/*
Copyright 2026 NVIDIA

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Package config loads the single YAML file that drives one mock-dpuagent process.
package config

import (
	"crypto/sha256"
	"fmt"
	"os"
	"strings"
	"time"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"

	"k8s.io/apimachinery/pkg/util/validation"
	"sigs.k8s.io/yaml"
)

// DPUType selects the Redfish personality and the install flow the mock emulates.
type DPUType string

const (
	DPUTypeBF3 DPUType = "bf3"
	DPUTypeBF4 DPUType = "bf4"
)

// JoinMode selects how the mock joins the DPU cluster.
type JoinMode string

const (
	// JoinSimulated replays kubeadm join and kubelet node registration in-process with client-go.
	JoinSimulated JoinMode = "simulated"
	// JoinKubelet runs the real Start Kubelet operation (systemctl start kubelet) on a VM whose
	// kubelet is already configured to join the DPU cluster.
	JoinKubelet JoinMode = "kubelet"
)

const (
	// DefaultPath is where the process looks for its configuration when --config is not given.
	DefaultPath = "/etc/mock-dpuagent/config.yaml"
	// DefaultBMCPort is the Redfish listen port. The controller hard-codes https://<ip>/ without a
	// port for GenerateCSR, so anything other than 443 only works for unit tests.
	DefaultBMCPort = 443
	// DefaultPSID matches the CX9 image PSID of the trimmed PLDM bundle in testdata.
	DefaultPSID = "MT_0000001775"
	// BMCMinSupportedVersion is the BF3 BMC firmware version the DPUDevice controller requires
	// (dpudevice.BMCMinSupportedVersion). A lower initial value makes the controller run the BMC
	// firmware upgrade path; after Manager.Reset the mock reports exactly this value.
	BMCMinSupportedVersion = "BF-24.10-17"
	// PlaceholderVersion is the initial value of firmware versions that only matter for the BF4
	// PLDM comparison. It never equals a real bundle version, so a configured bundle is always applied.
	PlaceholderVersion = "mock-0.0"
	// DefaultRebootMethod makes the agent report NoAction on every run.
	DefaultRebootMethod = "NoAction"
)

// Config is the root of the YAML file.
type Config struct {
	DPUType DPUType `json:"dpuType"`
	BMC     BMC     `json:"bmc"`
	Agent   Agent   `json:"agent"`
}

// BMC configures the Redfish personality.
type BMC struct {
	Port         int      `json:"port"`
	SerialNumber string   `json:"serialNumber"`
	PSID         string   `json:"psid"`
	Firmware     Firmware `json:"firmware"`
	// ResponseDelay, when set, holds every Redfish response back for a random duration in the
	// range, to imitate a slow BMC. Unset means no extra latency.
	ResponseDelay *ResponseDelay `json:"responseDelay,omitempty"`
	// ResponseDelayOverrides give single Redfish operations their own range instead of
	// ResponseDelay. The redfish package checks the names and methods against its route table.
	ResponseDelayOverrides []ResponseDelayOverride `json:"responseDelayOverrides,omitempty"`
}

// ResponseDelay is a closed range of whole seconds; each Redfish response waits a uniformly random
// duration between MinSeconds and MaxSeconds before it is written.
type ResponseDelay struct {
	MinSeconds int `json:"minSeconds"`
	MaxSeconds int `json:"maxSeconds"`
}

// Range returns the delay bounds as durations.
func (d *ResponseDelay) Range() (minDelay, maxDelay time.Duration) {
	return time.Duration(d.MinSeconds) * time.Second, time.Duration(d.MaxSeconds) * time.Second
}

func (d *ResponseDelay) validate() error {
	if d.MinSeconds < 0 || d.MaxSeconds < d.MinSeconds {
		return fmt.Errorf("wants 0 <= minSeconds <= maxSeconds, got %d..%d", d.MinSeconds, d.MaxSeconds)
	}
	return nil
}

// ResponseDelayOverride is the delay range of one Redfish operation, see the operation table in
// the README. A zero range means the operation is never delayed.
type ResponseDelayOverride struct {
	// Name is the operation name from the route table, for example UpdateService.SimpleUpdate.
	Name string `json:"name"`
	// Methods are the HTTP methods the override covers. Empty or ["*"] means every method the
	// operation serves; "*" cannot be combined with other methods.
	Methods       []string `json:"methods,omitempty"`
	ResponseDelay `json:",inline"`
}

// AllMethods reports whether the override covers every method of its operation.
func (o *ResponseDelayOverride) AllMethods() bool {
	return len(o.Methods) == 0 || (len(o.Methods) == 1 && o.Methods[0] == "*")
}

func (o *ResponseDelayOverride) validate() error {
	if o.Name == "" {
		return fmt.Errorf("name is required")
	}
	seen := map[string]bool{}
	for _, m := range o.Methods {
		switch {
		case m == "":
			return fmt.Errorf("methods contains an empty element")
		case m == "*" && len(o.Methods) > 1:
			return fmt.Errorf("methods cannot combine \"*\" with other methods")
		case seen[m]:
			return fmt.Errorf("methods lists %s twice", m)
		}
		seen[m] = true
	}
	return o.ResponseDelay.validate()
}

// Firmware holds the versions reported at power-on. Versions reported after an install or an
// upgrade are parsed from the artifacts the controller sends, not configured.
type Firmware struct {
	BMC  string `json:"bmc"`
	ERoT string `json:"erot"`
	UEFI string `json:"uefi"`
	NIC  string `json:"nic"`
}

// Agent configures the simulated dpu-agent.
type Agent struct {
	// RebootMethod is a comma separated list of NoAction | PowerCycle | SLR. Each agent run reports
	// the next element; once the list is exhausted every run reports NoAction.
	RebootMethod string `json:"rebootMethod"`
	// DPUClusterJoin selects the join implementation, see JoinMode.
	DPUClusterJoin JoinMode `json:"dpuClusterJoin"`
}

// Load reads, defaults and validates the configuration file at path.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config %s: %w", path, err)
	}
	return Parse(data)
}

// Parse defaults and validates a configuration document.
func Parse(data []byte) (*Config, error) {
	cfg := &Config{}
	if err := yaml.UnmarshalStrict(data, cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	cfg.applyDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

func (c *Config) applyDefaults() {
	if c.BMC.Port == 0 {
		c.BMC.Port = DefaultBMCPort
	}
	if c.BMC.PSID == "" {
		c.BMC.PSID = DefaultPSID
	}
	if c.BMC.Firmware.BMC == "" {
		c.BMC.Firmware.BMC = BMCMinSupportedVersion
	}
	if c.BMC.Firmware.ERoT == "" {
		c.BMC.Firmware.ERoT = PlaceholderVersion
	}
	if c.BMC.Firmware.UEFI == "" {
		c.BMC.Firmware.UEFI = PlaceholderVersion
	}
	if c.BMC.Firmware.NIC == "" {
		c.BMC.Firmware.NIC = PlaceholderVersion
	}
	for i := range c.BMC.ResponseDelayOverrides {
		for j, m := range c.BMC.ResponseDelayOverrides[i].Methods {
			c.BMC.ResponseDelayOverrides[i].Methods[j] = strings.ToUpper(m)
		}
	}
	if c.Agent.RebootMethod == "" {
		c.Agent.RebootMethod = DefaultRebootMethod
	}
	if c.Agent.DPUClusterJoin == "" {
		c.Agent.DPUClusterJoin = JoinSimulated
	}
}

// Validate rejects values the rest of the process cannot act on.
func (c *Config) Validate() error {
	switch c.DPUType {
	case DPUTypeBF3, DPUTypeBF4:
	case "":
		return fmt.Errorf("dpuType is required (bf3 or bf4)")
	default:
		return fmt.Errorf("unsupported dpuType %q (want bf3 or bf4)", c.DPUType)
	}
	if c.BMC.Port <= 0 || c.BMC.Port > 65535 {
		return fmt.Errorf("bmc.port %d is out of range", c.BMC.Port)
	}
	if c.BMC.SerialNumber != "" && !ValidSerialNumber(c.BMC.SerialNumber) {
		return fmt.Errorf("bmc.serialNumber %q must be MT, four digits and six upper-case letters, digits or '-' (12 characters)", c.BMC.SerialNumber)
	}
	if d := c.BMC.ResponseDelay; d != nil {
		if err := d.validate(); err != nil {
			return fmt.Errorf("bmc.responseDelay %w", err)
		}
	}
	for i := range c.BMC.ResponseDelayOverrides {
		o := &c.BMC.ResponseDelayOverrides[i]
		if err := o.validate(); err != nil {
			return fmt.Errorf("bmc.responseDelayOverrides[%d]: %w", i, err)
		}
	}
	if _, err := c.RebootMethods(); err != nil {
		return err
	}
	switch c.Agent.DPUClusterJoin {
	case JoinSimulated, JoinKubelet:
	default:
		return fmt.Errorf("unsupported agent.dpuClusterJoin %q (want simulated or kubelet)", c.Agent.DPUClusterJoin)
	}
	return nil
}

// RebootMethods returns agent.rebootMethod as API values, in report order.
func (c *Config) RebootMethods() ([]provisioningv1.RebootMethodType, error) {
	parts := strings.Split(c.Agent.RebootMethod, ",")
	methods := make([]provisioningv1.RebootMethodType, 0, len(parts))
	for _, p := range parts {
		switch strings.TrimSpace(p) {
		case "NoAction":
			methods = append(methods, provisioningv1.RebootMethodNoAction)
		case "PowerCycle":
			methods = append(methods, provisioningv1.RebootMethodPowerCycle)
		case "SLR":
			methods = append(methods, provisioningv1.RebootMethodSystemLevelReset)
		case "":
			return nil, fmt.Errorf("agent.rebootMethod %q contains an empty element", c.Agent.RebootMethod)
		default:
			return nil, fmt.Errorf("agent.rebootMethod %q: unsupported value %q (want NoAction, PowerCycle or SLR)", c.Agent.RebootMethod, p)
		}
	}
	return methods, nil
}

// ValidSerialNumber reports whether s can serve as the serial number of a mock DPU: 12 characters,
// MT, two year digits, two ISO week digits, then six upper-case letters, digits, '-' or '.', and
// lower-cased a valid DNS-1123 subdomain, because the controller uses the lower-case serial as the
// DPUDevice name and the DPU Agent identity.
func ValidSerialNumber(s string) bool {
	if len(s) != 12 || !strings.HasPrefix(s, "MT") {
		return false
	}
	for i, r := range s[2:] {
		digit := r >= '0' && r <= '9'
		upper := r >= 'A' && r <= 'Z'
		if i < 4 && !digit {
			return false
		}
		if !digit && !upper && r != '-' && r != '.' {
			return false
		}
	}
	return len(validation.IsDNS1123Subdomain(strings.ToLower(s))) == 0
}

// serialTailLen is how many trailing characters of the host name go into the serial number.
// Deployment pods and KubeVirt VirtualMachineInstanceReplicaSet VMIs end in five random characters
// that are unique within the set, and VirtualMachinePool VMs end in their ordinal, so the tail
// alone tells the replicas apart while the serial stays readable next to the pod name.
const serialTailLen = 5

// DeriveSerialNumber builds a BlueField-shaped serial number from the host name: MT, the two-digit
// ISO year and ISO week of now, X, and the last serialTailLen characters of the host name in upper
// case, left-padded with X when the host name is shorter.
func DeriveSerialNumber(hostname string, now time.Time) string {
	year, week := now.ISOWeek()
	tail := strings.ToUpper(hostname)
	if len(tail) > serialTailLen {
		tail = tail[len(tail)-serialTailLen:]
	}
	tail = strings.Repeat("X", serialTailLen-len(tail)) + tail
	return fmt.Sprintf("MT%02d%02dX%s", year%100, week, tail)
}

// ResolveSerialNumber returns the configured serial number or derives one from the host name. A
// derived serial that cannot be used as a Kubernetes object name is an error: the process must not
// come up as a DPU the controller cannot represent.
func (c *Config) ResolveSerialNumber(hostname string, now time.Time) (string, error) {
	if c.BMC.SerialNumber != "" {
		return c.BMC.SerialNumber, nil
	}
	serial := DeriveSerialNumber(hostname, now)
	if !ValidSerialNumber(serial) {
		return "", fmt.Errorf("serial number %q derived from host name %q is not a valid DNS-1123 name once lower-cased; the last %d characters of the host name must be letters, digits or '-'", serial, hostname, serialTailLen)
	}
	return serial, nil
}

// PF0MAC derives a unique, Mellanox-OUI MAC address for PF0 from the serial number.
func PF0MAC(serial string) string {
	sum := sha256.Sum256([]byte(serial))
	return fmt.Sprintf("0C:42:A1:%02X:%02X:%02X", sum[0], sum[1], sum[2])
}
