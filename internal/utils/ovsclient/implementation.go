/*
Copyright 2024 NVIDIA

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

package ovsclient

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	kexec "k8s.io/utils/exec"
)

const ovsVsctl = "ovs-vsctl"
const ovsAppctl = "ovs-appctl"

type ovsClient struct {
	exec           kexec.Interface
	ovsVsctlPath   string
	ovsAppCtlPath  string
	fileSystemRoot string
}

// New creates an OVSClient and returns an error if the OVS util binaries can't be found.
func newOvsClient(exec kexec.Interface) (OVSClient, error) {
	var err error
	c := &ovsClient{}
	c.exec = exec
	c.ovsVsctlPath, err = exec.LookPath(ovsVsctl)
	if err != nil {
		return nil, err
	}
	c.ovsAppCtlPath, err = exec.LookPath(ovsAppctl)
	if err != nil {
		return nil, err
	}
	return c, err
}

func (c *ovsClient) runOVSVsctl(args ...string) (string, error) {
	cmd := c.exec.Command(c.ovsVsctlPath, args...)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.SetStdout(&stdout)
	cmd.SetStderr(&stderr)
	err := cmd.Run()
	if err != nil {
		return "", fmt.Errorf("error running ovs-vsctl command with args %v failed: err=%w stderr=%s", args, err, stderr.String())
	}
	return stdout.String(), nil
}

func (c *ovsClient) runOVSAppctl(args ...string) (string, error) {
	socketPath, err := getVSwitchDSocketPath(c.fileSystemRoot)
	if err != nil {
		return "", fmt.Errorf("failed to construct ovs-vswitchd socket path: %w", err)
	}
	finalArgs := make([]string, 0, len(args)+2)
	finalArgs = append(finalArgs, "-t", socketPath)
	finalArgs = append(finalArgs, args...)
	cmd := c.exec.Command(c.ovsAppCtlPath, finalArgs...)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.SetStdout(&stdout)
	cmd.SetStderr(&stderr)
	err = cmd.Run()
	if err != nil {
		return "", fmt.Errorf("error running ovs-appctl command with args %v failed: err=%w stderr=%s", args, err, stderr.String())
	}
	return stdout.String(), nil
}

// getVSwitchDSocketPath returns the active socket of the ovs-vswitchd process
func getVSwitchDSocketPath(fileSystemRoot string) (string, error) {
	pid, err := os.ReadFile(filepath.Join(fileSystemRoot, "/var/run/openvswitch/ovs-vswitchd.pid"))
	if err != nil {
		return "", fmt.Errorf("failed to get ovs-vswitch pid: %w", err)
	}

	pidTrimmed := strings.TrimSpace(string(pid))

	return filepath.Join(fileSystemRoot, fmt.Sprintf("/var/run/openvswitch/ovs-vswitchd.%s.ctl", pidTrimmed)), nil
}

// BridgeExists checks if a bridge exists
func (c *ovsClient) BridgeExists(name string) (bool, error) {
	_, err := c.runOVSVsctl("br-exists", name)
	if err != nil {
		var exitErr *kexec.CodeExitError
		if errors.As(err, &exitErr) {
			// https://github.com/openvswitch/ovs/blob/166ee41d282c506d100bc2185d60af277121b55b/utilities/ovs-vsctl.8.in#L203-L206
			if exitErr.Code == 2 {
				return false, nil
			}
		}
		return false, err
	}

	return true, nil
}

// AddBridgeIfNotExists adds a bridge if it doesn't exist
func (c *ovsClient) AddBridgeIfNotExists(name string) error {
	_, err := c.runOVSVsctl("--may-exist", "add-br", name)
	return err
}

// DeleteBridgeIfExists deletes a bridge if it exists
func (c *ovsClient) DeleteBridgeIfExists(name string) error {
	_, err := c.runOVSVsctl("--if-exists", "del-br", name)
	return err
}

// SetBridgeDataPathType sets the datapath type of a bridge
func (c *ovsClient) SetBridgeDataPathType(bridge string, bridgeType BridgeDataPathType) error {
	_, err := c.runOVSVsctl("set", "bridge", bridge, fmt.Sprintf("datapath_type=%s", bridgeType))
	return err
}

// SetBridgeMAC sets the MAC address for the bridge interface
func (c *ovsClient) SetBridgeMAC(bridge string, mac net.HardwareAddr) error {
	_, err := c.runOVSVsctl("set", "bridge", bridge, fmt.Sprintf("other-config:hwaddr=%s", mac.String()))
	return err
}

// SetBridgeUplink sets the bridge-uplink external ID of the bridge. It overrides if already exists.
func (c *ovsClient) SetBridgeUplinkPort(bridge string, port string) error {
	_, err := c.runOVSVsctl("br-set-external-id", bridge, "bridge-uplink", port)
	return err
}

// SetBridgeHostToServicePort sets the host-to-service external ID of the bridge. It overrides if already exists.
func (c *ovsClient) SetBridgeHostToServicePort(bridge string, port string) error {
	_, err := c.runOVSVsctl("br-set-external-id", bridge, "host-to-service-interface", port)
	return err
}

// SetBridgeController sets the controller for a bridge
func (c *ovsClient) SetBridgeController(bridge string, controller string) error {
	_, err := c.runOVSVsctl("set-controller", bridge, controller)
	return err
}

// AddPortIfNotExists adds a port to a bridge if it doesn't exist
func (c *ovsClient) AddPortIfNotExists(bridge string, port string) error {
	_, err := c.runOVSVsctl("--may-exist", "add-port", bridge, port)
	return err
}

// SetPortType adds a port to a bridge
func (c *ovsClient) SetPortType(port string, portType PortType) error {
	_, err := c.runOVSVsctl("set", "interface", port, fmt.Sprintf("type=%s", portType))
	return err
}

// SetPatchPortPeer sets the peer for a patch port
func (c *ovsClient) SetPatchPortPeer(port string, peer string) error {
	_, err := c.runOVSVsctl("set", "interface", port, fmt.Sprintf("options:peer=%s", peer))
	return err
}

// SetOVNEncapIP sets the ovn-encap-ip external ID in the Open_vSwitch table in OVS
func (c *ovsClient) SetOVNEncapIP(ip net.IP) error {
	_, err := c.runOVSVsctl("set", "Open_vSwitch", ".", fmt.Sprintf("external_ids:ovn-encap-ip=%s", ip.String()))
	return err
}

// SetDOCAInit sets the doca-init other_config in the Open_vSwitch table in OVS
func (c *ovsClient) SetDOCAInit(enable bool) error {
	_, err := c.runOVSVsctl("set", "Open_vSwitch", ".", fmt.Sprintf("other_config:doca-init=%t", enable))
	return err
}

// SetKubernetesHostNodeName sets the host-k8s-nodename external ID in the Open_vSwitch table in OVS
func (c *ovsClient) SetKubernetesHostNodeName(name string) error {
	_, err := c.runOVSVsctl("set", "Open_vSwitch", ".", fmt.Sprintf("external_ids:host-k8s-nodename=%s", name))
	return err
}

// InterfaceToBridge returns the bridge an interface exists in
func (c *ovsClient) InterfaceToBridge(iface string) (string, error) {
	out, err := c.runOVSVsctl("iface-to-br", iface)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// DeletePort deletes a port
func (c *ovsClient) DeletePort(port string) error {
	_, err := c.runOVSVsctl("del-port", port)
	return err
}

// GetInterfaceOfPort returns the ofport number of a port
func (c *ovsClient) GetInterfaceOfPort(port string) (int, error) {
	out, err := c.runOVSVsctl("--columns=ofport", "list", "int", port)
	if err != nil {
		return 0, err
	}

	_, ofportRaw, found := strings.Cut(out, ":")
	if !found {
		return 0, fmt.Errorf("error finding the ofport in command output: %s", out)
	}

	return strconv.Atoi(strings.TrimSpace(ofportRaw))
}

// GetPortExternalIDs returns the external_ids of an OVS port
func (c *ovsClient) GetPortExternalIDs(port string) (map[string]string, error) {
	out, err := c.runOVSVsctl("--columns=external_ids", "list", "port", port)
	if err != nil {
		return nil, err
	}

	_, rawIDs, found := strings.Cut(out, ":")
	if !found {
		return nil, fmt.Errorf("error finding the external IDs in command output: %s", out)
	}

	return getExternalIDsAsMap(rawIDs)
}

// getExternalIDsAsMap returns a map go struct for the given ids passed as string
func getExternalIDsAsMap(rawIDs string) (map[string]string, error) {
	// https://github.com/openvswitch/ovs/blob/ec2a950d7d70d541323c3a48a424df565370579e/vswitchd/vswitch.ovsschema#L550
	ids := make(map[string]string)

	rawIDs = strings.TrimSpace(rawIDs)

	if rawIDs[0] == '{' {
		rawIDs = rawIDs[1:]
	}

	if rawIDs[len(rawIDs)-1] == '}' {
		rawIDs = rawIDs[:len(rawIDs)-1]
	}

	if len(rawIDs) == 0 {
		return ids, nil
	}

	for _, externalID := range strings.Split(rawIDs, ", ") {
		kv := strings.Split(externalID, "=")
		if len(kv) > 2 {
			return nil, fmt.Errorf("more than 2 elements found when splitting '%s' at '='", kv)
		}
		key := strings.TrimSpace(kv[0])
		value := strings.TrimSpace(kv[1])

		ids[key] = value
	}

	return ids, nil
}

// GetInterfaceExternalIDs returns the external_ids of an OVS interface
func (c *ovsClient) GetInterfaceExternalIDs(iface string) (map[string]string, error) {
	out, err := c.runOVSVsctl("--columns=external_ids", "list", "interface", iface)
	if err != nil {
		return nil, err
	}

	_, rawIDs, found := strings.Cut(out, ":")
	if !found {
		return nil, fmt.Errorf("error finding the external IDs in command output: %s", out)
	}

	return getExternalIDsAsMap(rawIDs)
}

// AddPortWithMetadata adds a port to the given bridge with the specified external IDs and ofport request in a single
// transaction
func (c *ovsClient) AddPortWithMetadata(bridge string, port string, portType PortType, portExternalIDs map[string]string, interfaceExternalIDs map[string]string, ofport int) error {
	args := []string{}
	args = append(args, "add-port", bridge, port, "--")
	args = append(args, "set", "int", port, fmt.Sprintf("type=%s", portType), "--")
	args = append(args, "set", "int", port, fmt.Sprintf("ofport_request=%d", ofport), "--")
	for k, v := range portExternalIDs {
		args = append(args, "set", "port", port, fmt.Sprintf("external_ids:%s=%s", k, v), "--")
	}
	for k, v := range interfaceExternalIDs {
		args = append(args, "set", "int", port, fmt.Sprintf("external_ids:%s=%s", k, v), "--")
	}

	args = args[:len(args)-1]
	_, err := c.runOVSVsctl(args...)
	return err
}

// ListInterfaces lists all the interfaces that exist in OVS of a particular type
func (c *ovsClient) ListInterfaces(portType PortType) (map[string]interface{}, error) {
	out, err := c.runOVSVsctl("--columns=name", "find", "int", fmt.Sprintf("type=%s", portType))
	if err != nil {
		return nil, err
	}

	ports := make(map[string]interface{})
	outReader := strings.NewReader(out)
	s := bufio.NewScanner(outReader)
	for s.Scan() {
		line := s.Text()
		_, rawPort, found := strings.Cut(line, ":")
		if !found {
			continue
		}

		ports[strings.TrimSpace(rawPort)] = struct{}{}
	}

	if err := s.Err(); err != nil {
		return nil, err
	}

	return ports, nil
}

// GetInterfacesWithPMDRXQueue returns all the interfaces that have a PMD Rx queue
func (c *ovsClient) GetInterfacesWithPMDRXQueue() (map[string]interface{}, error) {
	out, err := c.runOVSAppctl("dpif-netdev/pmd-rxq-show")
	if err != nil {
		return nil, err
	}

	ports := make(map[string]interface{})
	outReader := strings.NewReader(out)
	s := bufio.NewScanner(outReader)
	for s.Scan() {
		line := s.Text()
		key, value, found := strings.Cut(line, ":")
		if !found {
			continue
		}

		if strings.TrimSpace(key) != "port" {
			continue
		}

		value = strings.TrimSpace(value)
		rawPort, _, found := strings.Cut(value, " ")
		if !found {
			return nil, fmt.Errorf("error while extracting port from string: %s", value)
		}

		ports[strings.TrimSpace(rawPort)] = struct{}{}
	}

	if err := s.Err(); err != nil {
		return nil, err
	}

	return ports, nil
}
