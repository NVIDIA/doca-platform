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

package nodelabels

import (
	"os"
	"path/filepath"
	"time"

	"github.com/nvidia/doca-platform/cmd/dpuagent/opts"
	"github.com/nvidia/doca-platform/internal/provisioning/dpuagent/operations"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

const testDPUNodeName = "dpu-node"

var _ = Describe("ReportNodeLabels", func() {
	var tempDir string

	BeforeEach(func() {
		tempDir = GinkgoT().TempDir()
	})

	It("should never be skipped and should update status before continuing", func() {
		operation := &ReportNodeLabels{}
		Expect(operation.ShouldSkip(&operations.Context{})).To(BeFalse())
		Expect(operation.ShouldUpdateStatusBeforeContinue(&operations.Context{})).To(BeTrue())
	})

	It("should be skipped if SkipNodeLabeling is true", func() {
		operation := &ReportNodeLabels{}
		Expect(operation.ShouldSkip(&operations.Context{
			Options: opts.Options{SkipNodeLabeling: true},
		})).To(BeTrue())
	})

	It("treats a missing directory as an empty label set and removes stale script labels", func() {
		node := nodeWithLabels(map[string]string{"scripts.dpu.nvidia.com/old": "value", "static": "keep"})
		nodeClient := fakeNodeClient(node)
		operation := &ReportNodeLabels{
			scriptsDir:        filepath.Join(tempDir, "missing"),
			newNodeClientFunc: fakeNodeClientFactory(nodeClient),
		}
		optCtx := &operations.Context{}
		optCtx.Options.DPUName = testDPUNodeName

		Expect(operation.Execute(ctx, optCtx)).To(Succeed())
		updatedNode := &corev1.Node{}
		Expect(nodeClient.Get(ctx, client.ObjectKey{Name: testDPUNodeName}, updatedNode)).To(Succeed())
		Expect(updatedNode.Labels).To(Not(HaveKey("scripts.dpu.nvidia.com/old")))
		Expect(updatedNode.Labels).To(HaveKeyWithValue("static", "keep"))
	})

	It("reports an empty label set for an empty directory", func() {
		nodeClient := fakeNodeClient(nodeWithLabels(nil))
		operation := &ReportNodeLabels{
			scriptsDir:        tempDir,
			newNodeClientFunc: fakeNodeClientFactory(nodeClient),
		}
		optCtx := &operations.Context{}
		optCtx.Options.DPUName = testDPUNodeName

		Expect(operation.Execute(ctx, optCtx)).To(Succeed())
		updatedNode := &corev1.Node{}
		Expect(nodeClient.Get(ctx, client.ObjectKey{Name: testDPUNodeName}, updatedNode)).To(Succeed())
		Expect(updatedNode.Labels).To(BeEmpty())
	})

	It("runs executable regular files and applies the labels they emit", func() {
		writeScript(tempDir, "network", "printf 'pf0.ip=192.0.2.10\\n'", 0755)
		writeScript(tempDir, "interface", "printf 'interface_eth0=eth0'", 0755)
		writeScript(tempDir, "ignored", "printf 'ignored=ignored'", 0644)
		Expect(os.Mkdir(filepath.Join(tempDir, "ignored-dir"), 0755)).To(Succeed())

		nodeClient := fakeNodeClient(nodeWithLabels(map[string]string{"static": "keep"}))
		operation := &ReportNodeLabels{
			scriptsDir:        tempDir,
			newNodeClientFunc: fakeNodeClientFactory(nodeClient),
		}
		optCtx := &operations.Context{}
		optCtx.Options.DPUName = testDPUNodeName

		Expect(operation.Execute(ctx, optCtx)).To(Succeed())
		updatedNode := &corev1.Node{}
		Expect(nodeClient.Get(ctx, client.ObjectKey{Name: testDPUNodeName}, updatedNode)).To(Succeed())
		Expect(updatedNode.Labels).To(HaveKeyWithValue("static", "keep"))
		Expect(updatedNode.Labels).NotTo(HaveKey("interface_eth0"))
		Expect(updatedNode.Labels).NotTo(HaveKey("pf0.ip"))
		Expect(updatedNode.Labels).NotTo(HaveKey("scripts.dpu.nvidia.com/ignored"))
		Expect(updatedNode.Labels).To(HaveKeyWithValue("scripts.dpu.nvidia.com/interface_eth0", "eth0"))
		Expect(updatedNode.Labels).To(HaveKeyWithValue("scripts.dpu.nvidia.com/pf0.ip", "192.0.2.10"))
	})

	It("applies multiple labels emitted by a single script and ignores blank lines", func() {
		writeScript(tempDir, "network-info", "printf 'pf0.ip=192.0.2.10\\n\\n  \\nlink.speed=200G\\n'", 0755)

		nodeClient := fakeNodeClient(nodeWithLabels(nil))
		operation := &ReportNodeLabels{
			scriptsDir:        tempDir,
			newNodeClientFunc: fakeNodeClientFactory(nodeClient),
		}
		optCtx := &operations.Context{}
		optCtx.Options.DPUName = testDPUNodeName

		Expect(operation.Execute(ctx, optCtx)).To(Succeed())
		updatedNode := &corev1.Node{}
		Expect(nodeClient.Get(ctx, client.ObjectKey{Name: testDPUNodeName}, updatedNode)).To(Succeed())
		Expect(updatedNode.Labels).To(Equal(map[string]string{
			"scripts.dpu.nvidia.com/pf0.ip":     "192.0.2.10",
			"scripts.dpu.nvidia.com/link.speed": "200G",
		}))
	})

	It("does not use the file name as a label key and accepts arbitrary file names", func() {
		writeScript(tempDir, "10-net info.sh", "printf 'pf0.ip=192.0.2.10\\n'", 0755)

		operation := &ReportNodeLabels{
			scriptsDir: tempDir,
		}

		labels, err := operation.collectLabels(ctx, &operations.Context{})

		Expect(err).NotTo(HaveOccurred())
		Expect(labels).To(Equal(map[string]string{
			"scripts.dpu.nvidia.com/pf0.ip": "192.0.2.10",
		}))
	})

	It("reports no labels for a script without output", func() {
		writeScript(tempDir, "silent", "printf ''", 0755)

		operation := &ReportNodeLabels{
			scriptsDir: tempDir,
		}

		labels, err := operation.collectLabels(ctx, &operations.Context{})

		Expect(err).NotTo(HaveOccurred())
		Expect(labels).To(BeEmpty())
	})

	It("lets the last script win for a label key emitted more than once", func() {
		writeScript(tempDir, "a-first", "printf 'pf0.ip=192.0.2.10\\n'", 0755)
		writeScript(tempDir, "b-second", "printf 'pf0.ip=192.0.2.20\\n'", 0755)

		operation := &ReportNodeLabels{
			scriptsDir: tempDir,
		}

		labels, err := operation.collectLabels(ctx, &operations.Context{})

		Expect(err).NotTo(HaveOccurred())
		Expect(labels).To(Equal(map[string]string{
			"scripts.dpu.nvidia.com/pf0.ip": "192.0.2.20",
		}))
	})

	It("lets the last line win for a label key emitted more than once by the same script", func() {
		writeScript(tempDir, "network", "printf 'pf0.ip=192.0.2.10\\npf0.ip=192.0.2.20\\n'", 0755)

		operation := &ReportNodeLabels{
			scriptsDir: tempDir,
		}

		labels, err := operation.collectLabels(ctx, &operations.Context{})

		Expect(err).NotTo(HaveOccurred())
		Expect(labels).To(Equal(map[string]string{
			"scripts.dpu.nvidia.com/pf0.ip": "192.0.2.20",
		}))
	})

	It("fails when a script emits a line without a key/value separator", func() {
		writeScript(tempDir, "network", "printf 'no-separator\\npf0.ip=192.0.2.10\\n'", 0755)

		operation := &ReportNodeLabels{
			scriptsDir: tempDir,
		}

		labels, err := operation.collectLabels(ctx, &operations.Context{})

		// Valid lines of the same script are still applied.
		Expect(labels).To(Equal(map[string]string{
			"scripts.dpu.nvidia.com/pf0.ip": "192.0.2.10",
		}))
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("invalid output from node label script \"network\""))
		Expect(err.Error()).To(ContainSubstring("line 1: expected <label-key-suffix>=<label-value>, got \"no-separator\""))
	})

	It("fails when a script emits an invalid Kubernetes label key", func() {
		writeScript(tempDir, "network", "printf 'bad key=value'", 0755)

		operation := &ReportNodeLabels{
			scriptsDir: tempDir,
		}
		optCtx := &operations.Context{}
		optCtx.Options.DPUName = testDPUNodeName
		err := operation.Execute(ctx, optCtx)

		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("invalid node label key"))
	})

	It("fails when a script emits an invalid Kubernetes label value", func() {
		writeScript(tempDir, "network", "printf 'valid-key=bad value'", 0755)

		operation := &ReportNodeLabels{
			scriptsDir: tempDir,
		}
		optCtx := &operations.Context{}
		optCtx.Options.DPUName = testDPUNodeName
		err := operation.Execute(ctx, optCtx)

		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("invalid node label value"))
	})

	It("fails when a script exits non-zero", func() {
		writeScript(tempDir, "network", "echo stderr-message >&2\nexit 2", 0755)

		operation := &ReportNodeLabels{
			scriptsDir: tempDir,
		}
		optCtx := &operations.Context{}
		optCtx.Options.DPUName = testDPUNodeName
		err := operation.Execute(ctx, optCtx)

		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("failed to run node label script"))
		Expect(err.Error()).To(ContainSubstring("stderr-message"))
	})

	It("collects valid labels while aggregating script errors", func() {
		writeScript(tempDir, "bad-value", "printf 'bad-value-key=bad value'", 0755)
		writeScript(tempDir, "failing-script", "echo stderr-message >&2\nexit 2", 0755)
		writeScript(tempDir, "valid-key", "printf 'valid-key=valid-value'", 0755)

		operation := &ReportNodeLabels{
			scriptsDir: tempDir,
		}

		labels, err := operation.collectLabels(ctx, &operations.Context{})

		Expect(labels).To(Equal(map[string]string{
			"scripts.dpu.nvidia.com/valid-key": "valid-value",
		}))
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("invalid output from node label script \"bad-value\""))
		Expect(err.Error()).To(ContainSubstring("invalid node label value \"bad value\""))
		Expect(err.Error()).To(ContainSubstring("failed to run node label script"))
		Expect(err.Error()).To(ContainSubstring("failing-script"))
		Expect(err.Error()).To(ContainSubstring("stderr-message"))
	})

	It("fails when a script times out", func() {
		writeScript(tempDir, "network", "sleep 1", 0755)

		operation := &ReportNodeLabels{
			scriptsDir:    tempDir,
			scriptTimeout: 10 * time.Millisecond,
		}
		optCtx := &operations.Context{}
		optCtx.Options.DPUName = testDPUNodeName
		err := operation.Execute(ctx, optCtx)

		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("timed out"))
	})

	It("overwrites existing script labels with current script output", func() {
		writeScript(tempDir, "network", "printf 'pf0.ip=192.0.2.10'", 0755)
		nodeClient := fakeNodeClient(nodeWithLabels(map[string]string{"scripts.dpu.nvidia.com/pf0.ip": "old"}))
		operation := &ReportNodeLabels{
			scriptsDir:        tempDir,
			newNodeClientFunc: fakeNodeClientFactory(nodeClient),
		}
		optCtx := &operations.Context{}
		optCtx.Options.DPUName = testDPUNodeName

		Expect(operation.Execute(ctx, optCtx)).To(Succeed())

		updatedNode := &corev1.Node{}
		Expect(nodeClient.Get(ctx, client.ObjectKey{Name: testDPUNodeName}, updatedNode)).To(Succeed())
		Expect(updatedNode.Labels).To(HaveKeyWithValue("scripts.dpu.nvidia.com/pf0.ip", "192.0.2.10"))
	})

	It("removes stale script labels while keeping current script labels", func() {
		writeScript(tempDir, "network", "printf 'pf0.ip=192.0.2.10'", 0755)
		nodeClient := fakeNodeClient(nodeWithLabels(map[string]string{
			"scripts.dpu.nvidia.com/old":    "value",
			"scripts.dpu.nvidia.com/pf0.ip": "old",
		}))
		operation := &ReportNodeLabels{
			scriptsDir:        tempDir,
			newNodeClientFunc: fakeNodeClientFactory(nodeClient),
		}
		optCtx := &operations.Context{}
		optCtx.Options.DPUName = testDPUNodeName

		Expect(operation.Execute(ctx, optCtx)).To(Succeed())

		updatedNode := &corev1.Node{}
		Expect(nodeClient.Get(ctx, client.ObjectKey{Name: testDPUNodeName}, updatedNode)).To(Succeed())
		Expect(updatedNode.Labels).To(HaveKeyWithValue("scripts.dpu.nvidia.com/pf0.ip", "192.0.2.10"))
		Expect(updatedNode.Labels).NotTo(HaveKey("scripts.dpu.nvidia.com/old"))
	})

	It("creates a node client for each Execute", func() {
		writeScript(tempDir, "network", "printf 'pf0.ip=192.0.2.10'", 0755)
		created := 0
		operation := &ReportNodeLabels{
			scriptsDir: tempDir,
			newNodeClientFunc: func() (client.Client, error) {
				created++
				return fakeNodeClient(nodeWithLabels(nil)), nil
			},
		}
		optCtx := &operations.Context{}
		optCtx.Options.DPUName = testDPUNodeName

		Expect(operation.Execute(ctx, optCtx)).To(Succeed())
		Expect(operation.Execute(ctx, optCtx)).To(Succeed())

		Expect(created).To(Equal(2))
	})
})

func writeScript(dir, name, body string, mode os.FileMode) {
	script := "#!/bin/sh\nset -eu\n" + body + "\n"
	ExpectWithOffset(1, os.WriteFile(filepath.Join(dir, name), []byte(script), mode)).To(Succeed())
}

func fakeNodeClient(objects ...client.Object) client.Client {
	scheme := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	return fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).Build()
}

func fakeNodeClientFactory(nodeClient client.Client) func() (client.Client, error) {
	return func() (client.Client, error) {
		return nodeClient, nil
	}
}

func nodeWithLabels(labels map[string]string) *corev1.Node {
	return &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:   testDPUNodeName,
			Labels: labels,
		},
	}
}
