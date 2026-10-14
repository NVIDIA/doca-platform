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
	"os"
	"path/filepath"
	"testing"

	. "github.com/onsi/gomega"
	kexec "k8s.io/utils/exec"
	kexecTesting "k8s.io/utils/exec/testing"
)

func TestGetExternalIDsAsMap(t *testing.T) {
	g := NewWithT(t)
	cases := []struct {
		msg            string
		input          string
		expectedOutput map[string]string
		expectedError  bool
	}{
		{
			msg:            "empty",
			input:          "{}",
			expectedOutput: make(map[string]string),
			expectedError:  false,
		},
		{
			msg:   "one external id",
			input: "{owner=ovs-cni.network.kubevirt.io}",
			expectedOutput: map[string]string{
				"owner": "ovs-cni.network.kubevirt.io",
			},
			expectedError: false,
		},
		{
			msg:   "one external id with extra space at the beginning and end",
			input: " {owner=ovs-cni.network.kubevirt.io} ",
			expectedOutput: map[string]string{
				"owner": "ovs-cni.network.kubevirt.io",
			},
			expectedError: false,
		},
		{
			msg:   "multiple external ids",
			input: `{dpf-id="dpf-operator-system/dpu-cplane-tenant1-doca-hbn-ds-98svc/p1_if", iface-id=""}`,
			expectedOutput: map[string]string{
				"dpf-id":   `"dpf-operator-system/dpu-cplane-tenant1-doca-hbn-ds-98svc/p1_if"`,
				"iface-id": `""`,
			},
			expectedError: false,
		},
		{
			msg:   "malformed input due to no closing bracket",
			input: `{dpf-id="dpf-operator-system/dpu-cplane-tenant1-doca-hbn-ds-98svc/p1_if", iface-id=""`,
			expectedOutput: map[string]string{
				"dpf-id":   `"dpf-operator-system/dpu-cplane-tenant1-doca-hbn-ds-98svc/p1_if"`,
				"iface-id": `""`,
			},
			expectedError: false,
		},
		{
			msg:            "malformed input due to multiple equals",
			input:          `{dpf-id=="dpf-operator-system/dpu-cplane-tenant1-doca-hbn-ds-98svc/p1_if", iface-id=""}`,
			expectedOutput: make(map[string]string),
			expectedError:  true,
		},
	}

	for _, tt := range cases {
		t.Run(tt.msg, func(t *testing.T) {
			output, err := getExternalIDsAsMap(tt.input)
			if tt.expectedError {
				g.Expect(err).To(HaveOccurred())
				return
			}

			g.Expect(err).ToNot(HaveOccurred())
			g.Expect(output).To(BeComparableTo(tt.expectedOutput))
		})
	}
}

func TestGetInterfaceOfPort(t *testing.T) {
	g := NewWithT(t)
	cases := []struct {
		msg               string
		input             string
		fakeCommandOutput string
		expectedOutput    int
		expectedError     bool
	}{
		{
			msg:               "usual command output",
			input:             "pf0hpf",
			fakeCommandOutput: "ofport              : 4",
			expectedOutput:    4,
			expectedError:     false,
		},
		{
			msg:               "empty command output",
			input:             "pf0hpf",
			fakeCommandOutput: "",
			expectedError:     true,
		},
		{
			msg:               "malformed command output - missing column",
			input:             "pf0hpf",
			fakeCommandOutput: "ofport                4",
			expectedError:     true,
		},
	}

	for _, tt := range cases {
		t.Run(tt.msg, func(t *testing.T) {
			fakeExec := &kexecTesting.FakeExec{LookPathFunc: func(s string) (string, error) { return s, nil }}
			c, err := newOvsClient(fakeExec)
			g.Expect(err).ToNot(HaveOccurred())

			fakeExec.CommandScript = append(fakeExec.CommandScript, kexecTesting.FakeCommandAction(func(cmd string, args ...string) kexec.Cmd {
				g.Expect(cmd).To(Equal("ovs-vsctl"))
				g.Expect(args).To(Equal([]string{
					"--columns=ofport",
					"list",
					"int",
					tt.input,
				}))
				return kexec.New().Command("echo", tt.fakeCommandOutput)
			}))

			output, err := c.GetInterfaceOfPort(tt.input)
			if tt.expectedError {
				g.Expect(err).To(HaveOccurred())
				return
			}

			g.Expect(err).ToNot(HaveOccurred())
			g.Expect(output).To(BeComparableTo(tt.expectedOutput))
		})
	}
}

func TestGetPortExternalIDs(t *testing.T) {
	g := NewWithT(t)
	cases := []struct {
		msg               string
		input             string
		fakeCommandOutput string
		expectedOutput    map[string]string
		expectedError     bool
	}{
		{
			msg:               "usual command output",
			input:             "pf0hpf",
			fakeCommandOutput: `external_ids        : {dpf-id="dpf-operator-system/dpu-cplane-tenant1-doca-hbn-ds-98svc/p1_if", iface-id=""}`,
			expectedOutput: map[string]string{
				"dpf-id":   `"dpf-operator-system/dpu-cplane-tenant1-doca-hbn-ds-98svc/p1_if"`,
				"iface-id": `""`,
			},
			expectedError: false,
		},
		{
			msg:               "empty external ids",
			input:             "pf0hpf",
			fakeCommandOutput: "external_ids        : {}",
			expectedOutput:    make(map[string]string),
			expectedError:     false,
		},
		{
			msg:               "empty command output",
			input:             "pf0hpf",
			fakeCommandOutput: "",
			expectedError:     true,
		},
		{
			msg:               "malformed command output - missing column",
			input:             "pf0hpf",
			fakeCommandOutput: `external_ids          {dpf-id="dpf-operator-system/dpu-cplane-tenant1-doca-hbn-ds-98svc/p1_if", iface-id=""}`,
			expectedError:     true,
		},
	}

	for _, tt := range cases {
		t.Run(tt.msg, func(t *testing.T) {
			fakeExec := &kexecTesting.FakeExec{LookPathFunc: func(s string) (string, error) { return s, nil }}
			c, err := newOvsClient(fakeExec)
			g.Expect(err).ToNot(HaveOccurred())

			fakeExec.CommandScript = append(fakeExec.CommandScript, kexecTesting.FakeCommandAction(func(cmd string, args ...string) kexec.Cmd {
				g.Expect(cmd).To(Equal("ovs-vsctl"))
				g.Expect(args).To(Equal([]string{
					"--columns=external_ids",
					"list",
					"port",
					tt.input,
				}))
				return kexec.New().Command("echo", tt.fakeCommandOutput)
			}))

			output, err := c.GetPortExternalIDs(tt.input)
			if tt.expectedError {
				g.Expect(err).To(HaveOccurred())
				return
			}

			g.Expect(err).ToNot(HaveOccurred())
			g.Expect(output).To(BeComparableTo(tt.expectedOutput))
		})
	}
}

func TestGetInterfaceExternalIDs(t *testing.T) {
	g := NewWithT(t)
	cases := []struct {
		msg               string
		input             string
		fakeCommandOutput string
		expectedOutput    map[string]string
		expectedError     bool
	}{
		{
			msg:               "usual command output",
			input:             "pf0hpf",
			fakeCommandOutput: `external_ids        : {dpf-id="dpf-operator-system/dpu-cplane-tenant1-doca-hbn-ds-98svc/p1_if", iface-id=""}`,
			expectedOutput: map[string]string{
				"dpf-id":   `"dpf-operator-system/dpu-cplane-tenant1-doca-hbn-ds-98svc/p1_if"`,
				"iface-id": `""`,
			},
			expectedError: false,
		},
		{
			msg:               "empty external ids",
			input:             "pf0hpf",
			fakeCommandOutput: "external_ids        : {}",
			expectedOutput:    make(map[string]string),
			expectedError:     false,
		},
		{
			msg:               "empty command output",
			input:             "pf0hpf",
			fakeCommandOutput: "",
			expectedError:     true,
		},
		{
			msg:               "malformed command output - missing column",
			input:             "pf0hpf",
			fakeCommandOutput: `external_ids          {dpf-id="dpf-operator-system/dpu-cplane-tenant1-doca-hbn-ds-98svc/p1_if", iface-id=""}`,
			expectedError:     true,
		},
	}

	for _, tt := range cases {
		t.Run(tt.msg, func(t *testing.T) {
			fakeExec := &kexecTesting.FakeExec{LookPathFunc: func(s string) (string, error) { return s, nil }}
			c, err := newOvsClient(fakeExec)
			g.Expect(err).ToNot(HaveOccurred())

			fakeExec.CommandScript = append(fakeExec.CommandScript, kexecTesting.FakeCommandAction(func(cmd string, args ...string) kexec.Cmd {
				g.Expect(cmd).To(Equal("ovs-vsctl"))
				g.Expect(args).To(Equal([]string{
					"--columns=external_ids",
					"list",
					"interface",
					tt.input,
				}))
				return kexec.New().Command("echo", tt.fakeCommandOutput)
			}))

			output, err := c.GetInterfaceExternalIDs(tt.input)
			if tt.expectedError {
				g.Expect(err).To(HaveOccurred())
				return
			}

			g.Expect(err).ToNot(HaveOccurred())
			g.Expect(output).To(BeComparableTo(tt.expectedOutput))
		})
	}
}

func TestAddPortWithMetadata(t *testing.T) {
	g := NewWithT(t)
	cases := []struct {
		msg                       string
		inputBridge               string
		inputPort                 string
		inputPortType             PortType
		inputPortExternalIDs      map[string]string
		inputInterfaceExternalIDs map[string]string
		inputOfport               int
		expectedCommandArgs       []string
		expectedError             bool
	}{
		{
			msg:           "full",
			inputBridge:   "br-ovn",
			inputPort:     "pf0hpf",
			inputPortType: DPDK,
			inputPortExternalIDs: map[string]string{
				"port-id-key-1": "port-id-value-1",
				"port-id-key-2": "port-id-value-2",
			},
			inputInterfaceExternalIDs: map[string]string{
				"iface-id-key-1": "iface-id-value-1",
				"iface-id-key-2": "iface-id-value-2",
			},
			inputOfport: 4,
			expectedCommandArgs: []string{
				"add-port", "br-ovn", "pf0hpf",
				"--", "set", "int", "pf0hpf", "type=dpdk",
				"--", "set", "int", "pf0hpf", "ofport_request=4",
				"--", "set", "port", "pf0hpf", "external_ids:port-id-key-1=port-id-value-1",
				"--", "set", "port", "pf0hpf", "external_ids:port-id-key-2=port-id-value-2",
				"--", "set", "int", "pf0hpf", "external_ids:iface-id-key-1=iface-id-value-1",
				"--", "set", "int", "pf0hpf", "external_ids:iface-id-key-2=iface-id-value-2",
			},
			expectedError: false,
		},
		{
			msg:                  "no port external ids",
			inputBridge:          "br-ovn",
			inputPort:            "pf0hpf",
			inputPortType:        DPDK,
			inputPortExternalIDs: make(map[string]string),
			inputInterfaceExternalIDs: map[string]string{
				"iface-id-key-1": "iface-id-value-1",
				"iface-id-key-2": "iface-id-value-2",
			},
			inputOfport: 4,
			expectedCommandArgs: []string{
				"add-port", "br-ovn", "pf0hpf",
				"--", "set", "int", "pf0hpf", "type=dpdk",
				"--", "set", "int", "pf0hpf", "ofport_request=4",
				"--", "set", "int", "pf0hpf", "external_ids:iface-id-key-1=iface-id-value-1",
				"--", "set", "int", "pf0hpf", "external_ids:iface-id-key-2=iface-id-value-2",
			},
			expectedError: false,
		},
		{
			msg:           "no interface external ids",
			inputBridge:   "br-ovn",
			inputPort:     "pf0hpf",
			inputPortType: DPDK,
			inputPortExternalIDs: map[string]string{
				"port-id-key-1": "port-id-value-1",
				"port-id-key-2": "port-id-value-2",
			},
			inputInterfaceExternalIDs: make(map[string]string),
			inputOfport:               4,
			expectedCommandArgs: []string{
				"add-port", "br-ovn", "pf0hpf",
				"--", "set", "int", "pf0hpf", "type=dpdk",
				"--", "set", "int", "pf0hpf", "ofport_request=4",
				"--", "set", "port", "pf0hpf", "external_ids:port-id-key-1=port-id-value-1",
				"--", "set", "port", "pf0hpf", "external_ids:port-id-key-2=port-id-value-2",
			},
			expectedError: false,
		},
		{
			msg:                       "no port and interface external ids",
			inputBridge:               "br-ovn",
			inputPort:                 "pf0hpf",
			inputPortType:             DPDK,
			inputPortExternalIDs:      make(map[string]string),
			inputInterfaceExternalIDs: make(map[string]string),
			inputOfport:               4,
			expectedCommandArgs: []string{
				"add-port", "br-ovn", "pf0hpf",
				"--", "set", "int", "pf0hpf", "type=dpdk",
				"--", "set", "int", "pf0hpf", "ofport_request=4",
			},
			expectedError: false,
		},
	}

	for _, tt := range cases {
		t.Run(tt.msg, func(t *testing.T) {
			fakeExec := &kexecTesting.FakeExec{LookPathFunc: func(s string) (string, error) { return s, nil }}
			c, err := newOvsClient(fakeExec)
			g.Expect(err).ToNot(HaveOccurred())

			fakeExec.CommandScript = append(fakeExec.CommandScript, kexecTesting.FakeCommandAction(func(cmd string, args ...string) kexec.Cmd {
				g.Expect(cmd).To(Equal("ovs-vsctl"))
				g.Expect(args).To(ContainElements(tt.expectedCommandArgs))
				return kexec.New().Command("echo")
			}))

			err = c.AddPortWithMetadata(
				tt.inputBridge,
				tt.inputPort,
				tt.inputPortType,
				tt.inputPortExternalIDs,
				tt.inputInterfaceExternalIDs,
				tt.inputOfport)
			if tt.expectedError {
				g.Expect(err).To(HaveOccurred())
				return
			}
			g.Expect(err).ToNot(HaveOccurred())
		})
	}
}

func TestListInterfaces(t *testing.T) {
	g := NewWithT(t)
	cases := []struct {
		msg               string
		fakeCommandOutput string
		expectedOutput    map[string]interface{}
		expectedError     bool
	}{
		{
			msg: "usual command output",
			fakeCommandOutput: `
name                : pf0hpf

name                : p0

name                : p1

name                : ovn-k8s-mp0

name                : en3f0pf0sf15

name                : en3f0pf0sf12

name                : en3f0pf0sf1`,
			expectedOutput: map[string]interface{}{
				"pf0hpf":       struct{}{},
				"p0":           struct{}{},
				"p1":           struct{}{},
				"ovn-k8s-mp0":  struct{}{},
				"en3f0pf0sf15": struct{}{},
				"en3f0pf0sf12": struct{}{},
				"en3f0pf0sf1":  struct{}{},
			},
			expectedError: false,
		},
		{
			msg:               "no interfaces",
			fakeCommandOutput: "",
			expectedOutput:    make(map[string]interface{}),
			expectedError:     false,
		},
		{
			msg: "malformed command output - missing column in one of the lines",
			fakeCommandOutput: `
name                : pf0hpf

name                : p0

name                : p1

name                : ovn-k8s-mp0

name                 en3f0pf0sf15

name                : en3f0pf0sf12

name                : en3f0pf0sf1`,
			expectedOutput: map[string]interface{}{
				"pf0hpf":       struct{}{},
				"p0":           struct{}{},
				"p1":           struct{}{},
				"ovn-k8s-mp0":  struct{}{},
				"en3f0pf0sf12": struct{}{},
				"en3f0pf0sf1":  struct{}{},
			},
			expectedError: false,
		},
	}

	for _, tt := range cases {
		t.Run(tt.msg, func(t *testing.T) {
			fakeExec := &kexecTesting.FakeExec{LookPathFunc: func(s string) (string, error) { return s, nil }}
			c, err := newOvsClient(fakeExec)
			g.Expect(err).ToNot(HaveOccurred())

			fakeExec.CommandScript = append(fakeExec.CommandScript, kexecTesting.FakeCommandAction(func(cmd string, args ...string) kexec.Cmd {
				g.Expect(cmd).To(Equal("ovs-vsctl"))
				g.Expect(args).To(Equal([]string{
					"--columns=name",
					"find",
					"int",
					"type=dpdk",
				}))
				return kexec.New().Command("echo", tt.fakeCommandOutput)
			}))

			output, err := c.ListInterfaces(DPDK)
			if tt.expectedError {
				g.Expect(err).To(HaveOccurred())
				return
			}

			g.Expect(err).ToNot(HaveOccurred())
			g.Expect(output).To(BeComparableTo(tt.expectedOutput))
		})
	}
}

func TestGetInterfacesWithPMDRXQueue(t *testing.T) {
	g := NewWithT(t)
	cases := []struct {
		msg               string
		fakeCommandOutput string
		expectedOutput    map[string]interface{}
		expectedError     bool
	}{
		{
			msg: "usual command output",
			fakeCommandOutput: `
Displaying last 60 seconds pmd usage %
pmd thread numa_id 0 core_id 11:
  isolated : true
  port: en3f0pf0sf12      queue-id:  0 (enabled)   pmd usage:  0 %
  port: en3f0pf0sf14      queue-id:  0 (enabled)   pmd usage:  0 %
  port: en3f0pf0sf19      queue-id:  0 (enabled)   pmd usage:  0 %
  port: ovn-k8s-mp0       queue-id:  0 (enabled)   pmd usage:  0 %
  port: p0                queue-id:  0 (enabled)   pmd usage:  0 %
  port: p1                queue-id:  0 (enabled)   pmd usage:  0 %
  port: pf0hpf            queue-id:  0 (enabled)   pmd usage:  0 %
  port: pf0vf39           queue-id:  0 (enabled)   pmd usage:  0 %
  overhead:  0 %`,
			expectedOutput: map[string]interface{}{
				"en3f0pf0sf12": struct{}{},
				"en3f0pf0sf14": struct{}{},
				"en3f0pf0sf19": struct{}{},
				"ovn-k8s-mp0":  struct{}{},
				"p0":           struct{}{},
				"p1":           struct{}{},
				"pf0hpf":       struct{}{},
				"pf0vf39":      struct{}{},
			},
			expectedError: false,
		},
		{
			msg: "no interfaces",
			fakeCommandOutput: `
Displaying last 60 seconds pmd usage %
pmd thread numa_id 0 core_id 11:
  isolated : true
  overhead:  0 %`,
			expectedOutput: make(map[string]interface{}),
			expectedError:  false,
		},
		{
			msg: "malformed input due to missing column",
			fakeCommandOutput: `
Displaying last 60 seconds pmd usage %
pmd thread numa_id 0 core_id 11:
  isolated : true
  port: en3f0pf0sf12      queue-id:  0 (enabled)   pmd usage:  0 %
  port: en3f0pf0sf14      queue-id:  0 (enabled)   pmd usage:  0 %
  port: en3f0pf0sf19      queue-id:  0 (enabled)   pmd usage:  0 %
  port ovn-k8s-mp0       queue-id:  0 (enabled)   pmd usage:  0 %
  port p0                queue-id:  0 (enabled)   pmd usage:  0 %
  port: p1                queue-id:  0 (enabled)   pmd usage:  0 %
  port: pf0hpf            queue-id:  0 (enabled)   pmd usage:  0 %
  port: pf0vf39           queue-id:  0 (enabled)   pmd usage:  0 %
  overhead:  0 %`,
			expectedOutput: map[string]interface{}{
				"en3f0pf0sf12": struct{}{},
				"en3f0pf0sf14": struct{}{},
				"en3f0pf0sf19": struct{}{},
				"p1":           struct{}{},
				"pf0hpf":       struct{}{},
				"pf0vf39":      struct{}{},
			},
			expectedError: false,
		},
	}

	for _, tt := range cases {
		t.Run(tt.msg, func(t *testing.T) {
			fakeExec := &kexecTesting.FakeExec{LookPathFunc: func(s string) (string, error) { return s, nil }}
			c, err := newOvsClient(fakeExec)
			g.Expect(err).ToNot(HaveOccurred())
			cImpl := c.(*ovsClient)

			tmpDir, err := os.MkdirTemp("", "ovsclient")
			defer func() {
				err := os.RemoveAll(tmpDir)
				g.Expect(err).ToNot(HaveOccurred())
			}()
			g.Expect(err).NotTo(HaveOccurred())
			cImpl.fileSystemRoot = tmpDir
			ovsSocketDir := filepath.Join(tmpDir, "/var/run/openvswitch")
			g.Expect(os.MkdirAll(ovsSocketDir, 0755)).To(Succeed())
			ovsPidFile := filepath.Join(ovsSocketDir, "ovs-vswitchd.pid")
			g.Expect(os.WriteFile(ovsPidFile, []byte("12345"), 0644)).To(Succeed())
			ovsSocketPath := filepath.Join(ovsSocketDir, "ovs-vswitchd.12345.ctl")

			fakeExec.CommandScript = append(fakeExec.CommandScript, kexecTesting.FakeCommandAction(func(cmd string, args ...string) kexec.Cmd {
				g.Expect(cmd).To(Equal("ovs-appctl"))
				g.Expect(args).To(Equal([]string{
					"-t",
					ovsSocketPath,
					"dpif-netdev/pmd-rxq-show",
				}))
				return kexec.New().Command("echo", tt.fakeCommandOutput)
			}))

			output, err := cImpl.GetInterfacesWithPMDRXQueue()
			if tt.expectedError {
				g.Expect(err).To(HaveOccurred())
				return
			}

			g.Expect(err).ToNot(HaveOccurred())
			g.Expect(output).To(BeComparableTo(tt.expectedOutput))
		})
	}
}
