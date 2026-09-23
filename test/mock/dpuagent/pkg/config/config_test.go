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

package config

import (
	"testing"
	"time"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
)

func TestParseDefaults(t *testing.T) {
	cfg, err := Parse([]byte("dpuType: bf3\n"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if cfg.BMC.Port != DefaultBMCPort || cfg.BMC.PSID != DefaultPSID || cfg.BMC.Firmware.BMC != BMCMinSupportedVersion {
		t.Fatalf("defaults not applied: %+v", cfg.BMC)
	}
	if cfg.Agent.DPUClusterJoin != JoinSimulated {
		t.Fatalf("expected simulated join, got %q", cfg.Agent.DPUClusterJoin)
	}
	methods, err := cfg.RebootMethods()
	if err != nil || len(methods) != 1 || methods[0] != provisioningv1.RebootMethodNoAction {
		t.Fatalf("unexpected reboot methods %v, %v", methods, err)
	}
}

func TestParseRebootMethods(t *testing.T) {
	cfg, err := Parse([]byte("dpuType: bf4\nagent:\n  rebootMethod: \"SLR, PowerCycle,NoAction\"\n"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	methods, err := cfg.RebootMethods()
	if err != nil {
		t.Fatal(err)
	}
	want := []provisioningv1.RebootMethodType{
		provisioningv1.RebootMethodSystemLevelReset,
		provisioningv1.RebootMethodPowerCycle,
		provisioningv1.RebootMethodNoAction,
	}
	if len(methods) != len(want) {
		t.Fatalf("got %v want %v", methods, want)
	}
	for i := range want {
		if methods[i] != want[i] {
			t.Fatalf("got %v want %v", methods, want)
		}
	}
}

func TestParseRejectsInvalid(t *testing.T) {
	for _, doc := range []string{
		"",
		"dpuType: bf5\n",
		"dpuType: bf3\nagent:\n  rebootMethod: Reboot\n",
		"dpuType: bf3\nagent:\n  dpuClusterJoin: kwok\n",
		"dpuType: bf3\nbmc:\n  serialNumber: abc\n",
		"dpuType: bf3\nunknown: 1\n",
		"dpuType: bf3\nbmc:\n  responseDelay:\n    minSeconds: 3\n    maxSeconds: 1\n",
		"dpuType: bf3\nbmc:\n  responseDelay:\n    minSeconds: -1\n    maxSeconds: 1\n",
		"dpuType: bf3\nbmc:\n  responseDelayOverrides:\n    - methods: [GET]\n      maxSeconds: 1\n",
		"dpuType: bf3\nbmc:\n  responseDelayOverrides:\n    - name: Task\n      minSeconds: 2\n      maxSeconds: 1\n",
		"dpuType: bf3\nbmc:\n  responseDelayOverrides:\n    - name: Task\n      methods: [\"*\", GET]\n      maxSeconds: 1\n",
		"dpuType: bf3\nbmc:\n  responseDelayOverrides:\n    - name: Task\n      methods: [GET, get]\n      maxSeconds: 1\n",
		"dpuType: bf3\nbmc:\n  responseDelayOverrides:\n    - name: Task\n      methods: [\"\"]\n      maxSeconds: 1\n",
	} {
		if _, err := Parse([]byte(doc)); err == nil {
			t.Errorf("expected error for %q", doc)
		}
	}
}

func TestDeriveSerialNumber(t *testing.T) {
	now := time.Date(2026, 4, 15, 0, 0, 0, 0, time.UTC) // ISO week 16
	cases := map[string]string{
		"mock-dpuagent-7c9f8-x2k4q": "MT2616XX2K4Q", // Deployment pod: five random characters
		"mock-dpuagent-7c9f8-zzzzz": "MT2616XZZZZZ",
		"vmi-poolh8998":             "MT2616XH8998", // KubeVirt VirtualMachineInstanceReplicaSet
		"vm-pool-12":                "MT2616XOL-12", // KubeVirt VirtualMachinePool ordinal
		"vm1":                       "MT2616XXXVM1", // shorter than the tail: left-padded
	}
	for hostname, want := range cases {
		got := DeriveSerialNumber(hostname, now)
		if got != want {
			t.Errorf("%s: got %s, want %s", hostname, got, want)
		}
		if !ValidSerialNumber(got) {
			t.Errorf("%s: %s is not a valid serial", hostname, got)
		}
		resolved, err := (&Config{}).ResolveSerialNumber(hostname, now)
		if err != nil || resolved != got {
			t.Errorf("%s: resolve returned %q, %v", hostname, resolved, err)
		}
	}
	for _, hostname := range []string{"node-", "a..b"} {
		if _, err := (&Config{}).ResolveSerialNumber(hostname, now); err == nil {
			t.Errorf("%s: expected an error, serial %s", hostname, DeriveSerialNumber(hostname, now))
		}
	}
	if s, err := (&Config{BMC: BMC{SerialNumber: "MT2616ABCDEF"}}).ResolveSerialNumber("ignored", now); err != nil || s != "MT2616ABCDEF" {
		t.Fatalf("configured serial not returned: %q, %v", s, err)
	}
}

func TestParseResponseDelay(t *testing.T) {
	cfg, err := Parse([]byte("dpuType: bf3\n"))
	if err != nil || cfg.BMC.ResponseDelay != nil {
		t.Fatalf("expected no delay by default: %+v, %v", cfg.BMC.ResponseDelay, err)
	}
	cfg, err = Parse([]byte("dpuType: bf3\nbmc:\n  responseDelay:\n    minSeconds: 1\n    maxSeconds: 3\n"))
	if err != nil {
		t.Fatal(err)
	}
	lo, hi := cfg.BMC.ResponseDelay.Range()
	if lo != time.Second || hi != 3*time.Second {
		t.Fatalf("unexpected range %v..%v", lo, hi)
	}
	// A zero range is allowed and means no delay.
	if _, err := Parse([]byte("dpuType: bf3\nbmc:\n  responseDelay: {}\n")); err != nil {
		t.Fatal(err)
	}
}

func TestParseResponseDelayOverrides(t *testing.T) {
	cfg, err := Parse([]byte(`dpuType: bf3
bmc:
  responseDelayOverrides:
    - name: SecureBoot
      methods: [get, PATCH]
      minSeconds: 1
      maxSeconds: 2
    - name: Task
    - name: UpdateService.SimpleUpdate
      methods: ["*"]
      minSeconds: 20
      maxSeconds: 30
`))
	if err != nil {
		t.Fatal(err)
	}
	overrides := cfg.BMC.ResponseDelayOverrides
	if len(overrides) != 3 {
		t.Fatalf("got %d overrides", len(overrides))
	}
	if overrides[0].AllMethods() || len(overrides[0].Methods) != 2 || overrides[0].Methods[0] != "GET" || overrides[0].Methods[1] != "PATCH" {
		t.Fatalf("methods not normalized: %+v", overrides[0])
	}
	if lo, hi := overrides[0].Range(); lo != time.Second || hi != 2*time.Second {
		t.Fatalf("unexpected range %v..%v", lo, hi)
	}
	if !overrides[1].AllMethods() || !overrides[2].AllMethods() {
		t.Fatalf("empty methods and [*] must both mean all methods: %+v %+v", overrides[1], overrides[2])
	}
	if lo, hi := overrides[1].Range(); lo != 0 || hi != 0 {
		t.Fatalf("expected a zero range, got %v..%v", lo, hi)
	}
}
