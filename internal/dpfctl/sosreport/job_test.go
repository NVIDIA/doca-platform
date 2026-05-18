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

package sosreport

import (
	"context"
	"testing"
	"time"

	. "github.com/onsi/gomega"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/validation"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestCreateJob(t *testing.T) {
	tests := []struct {
		name                  string
		opts                  JobOptions
		wantContainerName     string
		wantInitContainers    int
		wantOutputVolume      func(g Gomega, volumes []corev1.Volume)
		wantOutputDirEnv      string
		wantGeneratePrefix    string
		wantGenerateHashInput string
	}{
		{
			name: "local mode uses emptyDir and sleep container",
			opts: JobOptions{
				Namespace:   "test-ns",
				NodeName:    "worker-1",
				CaseID:      "case-123",
				Image:       "ghcr.io/nvidia/sosreport:latest",
				ClusterName: "host",
				Timeout:     30 * time.Minute,
				Output:      OutputLocal,
			},
			wantContainerName:  "sleep",
			wantInitContainers: 1,
			wantOutputVolume: func(g Gomega, volumes []corev1.Volume) {
				v := findVolume(volumes, "output")
				g.Expect(v).NotTo(BeNil())
				g.Expect(v.VolumeSource.EmptyDir).NotTo(BeNil())
			},
		},
		{
			name: "local mode uses short node name in generated name for FQDN node",
			opts: JobOptions{
				Namespace:   "test-ns",
				NodeName:    "nvd-srv-00.nvidia.eng.abc1.dc.example.com",
				CaseID:      "dpf-20260517-081148",
				Image:       "ghcr.io/nvidia/sosreport:latest",
				ClusterName: "host",
				Timeout:     30 * time.Minute,
				Output:      OutputLocal,
			},
			wantContainerName:     "sleep",
			wantInitContainers:    1,
			wantGeneratePrefix:    "sos-dpf-20260517-081148-nvd-srv-00-",
			wantGenerateHashInput: "sos-dpf-20260517-081148-nvd-srv-00.nvidia.eng.abc1.dc.example.com",
		},
		{
			name: "local mode hashes generated name when case ID and short node exceed limit",
			opts: JobOptions{
				Namespace:   "test-ns",
				NodeName:    "worker-1.example.com",
				CaseID:      "case-with-a-very-long-identifier-that-fits-label",
				Image:       "ghcr.io/nvidia/sosreport:latest",
				ClusterName: "host",
				Timeout:     30 * time.Minute,
				Output:      OutputLocal,
			},
			wantContainerName:     "sleep",
			wantInitContainers:    1,
			wantGenerateHashInput: "sos-case-with-a-very-long-identifier-that-fits-label-worker-1.example.com",
		},
		{
			name: "local mode hashes full node name for FQDN node",
			opts: JobOptions{
				Namespace:   "test-ns",
				NodeName:    "foo.bar.com",
				CaseID:      "case-same-short",
				Image:       "ghcr.io/nvidia/sosreport:latest",
				ClusterName: "host",
				Timeout:     30 * time.Minute,
				Output:      OutputLocal,
			},
			wantContainerName:     "sleep",
			wantInitContainers:    1,
			wantGeneratePrefix:    "sos-case-same-short-foo-",
			wantGenerateHashInput: "sos-case-same-short-foo.bar.com",
		},
		{
			name: "local mode uses different hash for same short node name",
			opts: JobOptions{
				Namespace:   "test-ns",
				NodeName:    "foo.foobar.com",
				CaseID:      "case-same-short",
				Image:       "ghcr.io/nvidia/sosreport:latest",
				ClusterName: "host",
				Timeout:     30 * time.Minute,
				Output:      OutputLocal,
			},
			wantContainerName:     "sleep",
			wantInitContainers:    1,
			wantGeneratePrefix:    "sos-case-same-short-foo-",
			wantGenerateHashInput: "sos-case-same-short-foo.foobar.com",
		},
		{
			name: "local mode stores long node name in annotation",
			opts: JobOptions{
				Namespace:   "test-ns",
				NodeName:    "node-with-a-long-but-valid-label-value-that-exceeds-sixty-three-bytes.example.com",
				CaseID:      "dpf-20260517-081148",
				Image:       "ghcr.io/nvidia/sosreport:latest",
				ClusterName: "host",
				Timeout:     30 * time.Minute,
				Output:      OutputLocal,
			},
			wantContainerName:  "sleep",
			wantInitContainers: 1,
		},
		{
			name: "NFS mode uses NFS volume and done container",
			opts: JobOptions{
				Namespace:   "test-ns",
				NodeName:    "worker-1",
				CaseID:      "case-nfs",
				Image:       "ghcr.io/nvidia/sosreport:latest",
				ClusterName: "host",
				Timeout:     30 * time.Minute,
				Output:      OutputNFS,
				NFSServer:   "10.0.0.1",
				NFSPath:     "/exports/sos",
			},
			wantContainerName:  "done",
			wantInitContainers: 2, // sosreport + copy-to-nfs
			wantOutputVolume: func(g Gomega, volumes []corev1.Volume) {
				v := findVolume(volumes, "output")
				g.Expect(v).NotTo(BeNil())
				g.Expect(v.VolumeSource.NFS).NotTo(BeNil())
				g.Expect(v.VolumeSource.NFS.Server).To(Equal("10.0.0.1"))
				g.Expect(v.VolumeSource.NFS.Path).To(Equal("/exports/sos"))
			},
		},
		{
			name: "NFS with subdir prepends mkdir init container",
			opts: JobOptions{
				Namespace:   "test-ns",
				NodeName:    "worker-1",
				CaseID:      "case-sub",
				Image:       "ghcr.io/nvidia/sosreport:latest",
				ClusterName: "host",
				Timeout:     30 * time.Minute,
				Output:      OutputNFS,
				NFSServer:   "10.0.0.1",
				NFSPath:     "/exports/sos",
				NFSSubDir:   "sosreport-20260416-120000",
			},
			wantContainerName:  "done",
			wantInitContainers: 3, // mkdir + sosreport + copy-to-nfs
			wantOutputVolume: func(g Gomega, volumes []corev1.Volume) {
				v := findVolume(volumes, "output")
				g.Expect(v).NotTo(BeNil())
				g.Expect(v.VolumeSource.NFS).NotTo(BeNil())
			},
			wantOutputDirEnv: "/sos-staging",
		},
		{
			name: "NFS with subdir and archive adds archive init container",
			opts: JobOptions{
				Namespace:   "test-ns",
				NodeName:    "worker-1",
				CaseID:      "case-arc",
				Image:       "ghcr.io/nvidia/sosreport:latest",
				ClusterName: "host",
				Timeout:     30 * time.Minute,
				Output:      OutputNFS,
				NFSServer:   "10.0.0.1",
				NFSPath:     "/exports/sos",
				NFSSubDir:   "sosreport-20260416-120000",
				Archive:     true,
			},
			wantContainerName:  "done",
			wantInitContainers: 4, // mkdir + sosreport + copy-to-nfs + archive
			wantOutputDirEnv:   "/sos-staging",
		},
		{
			name: "NFS archive-only skips mkdir and copy",
			opts: JobOptions{
				Namespace:   "test-ns",
				NodeName:    "worker-1",
				CaseID:      "case-ao",
				Image:       "ghcr.io/nvidia/sosreport:latest",
				ClusterName: "host",
				Timeout:     30 * time.Minute,
				Output:      OutputNFS,
				NFSServer:   "10.0.0.1",
				NFSPath:     "/exports/sos",
				NFSSubDir:   "sosreport-20260416-120000",
				Archive:     true,
				ArchiveOnly: true,
			},
			wantContainerName:  "done",
			wantInitContainers: 2, // sosreport + archive (no mkdir, no copy)
			wantOutputDirEnv:   "/sos-staging",
		},
		{
			name: "NFS with custom UID sets RunAsUser on mkdir",
			opts: JobOptions{
				Namespace:   "test-ns",
				NodeName:    "worker-1",
				CaseID:      "case-uid",
				Image:       "ghcr.io/nvidia/sosreport:latest",
				ClusterName: "host",
				Timeout:     30 * time.Minute,
				Output:      OutputNFS,
				NFSServer:   "10.0.0.1",
				NFSPath:     "/exports/sos",
				NFSSubDir:   "sosreport-20260416-120000",
				NFSUID:      1000,
			},
			wantContainerName:  "done",
			wantInitContainers: 3, // mkdir + sosreport + copy-to-nfs
			wantOutputDirEnv:   "/sos-staging",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)

			scheme := runtime.NewScheme()
			_ = batchv1.AddToScheme(scheme)
			_ = corev1.AddToScheme(scheme)
			c := fake.NewClientBuilder().WithScheme(scheme).Build()

			job, err := CreateJob(context.Background(), c, tt.opts)
			g.Expect(err).NotTo(HaveOccurred())

			// Verify metadata.
			nodeLabel := nodeLabelValue(tt.opts.NodeName)
			g.Expect(job.GenerateName).To(Equal(jobGenerateName(tt.opts.CaseID, tt.opts.NodeName)))
			if tt.wantGeneratePrefix != "" {
				g.Expect(job.GenerateName).To(HavePrefix(tt.wantGeneratePrefix))
			}
			if tt.wantGenerateHashInput != "" {
				g.Expect(len(job.GenerateName)).To(BeNumerically("<=", maxJobGenerateNamePrefixSize))
				g.Expect(job.GenerateName).To(ContainSubstring(shortHash(tt.wantGenerateHashInput)))
			}
			g.Expect(job.Name).To(HavePrefix(job.GenerateName))
			g.Expect(len(job.Name)).To(BeNumerically("<=", validation.LabelValueMaxLength))
			g.Expect(validation.IsValidLabelValue(job.Name)).To(BeEmpty())
			g.Expect(job.Namespace).To(Equal(tt.opts.Namespace))
			g.Expect(job.Labels).To(HaveKeyWithValue(labelCaseID, tt.opts.CaseID))
			g.Expect(job.Labels).To(HaveKeyWithValue(labelNodeID, nodeLabel))
			g.Expect(job.Spec.Template.Labels).To(HaveKeyWithValue(labelNodeID, nodeLabel))
			g.Expect(len(nodeLabel)).To(BeNumerically("<=", validation.LabelValueMaxLength))
			g.Expect(validation.IsValidLabelValue(nodeLabel)).To(BeEmpty())
			g.Expect(nodeLabel).To(HaveSuffix(shortHash(tt.opts.NodeName)))
			g.Expect(job.Labels).NotTo(HaveKey(annotationNode))
			g.Expect(job.Labels).NotTo(HaveKey(annotationCluster))
			g.Expect(job.Annotations).To(HaveKeyWithValue(annotationNode, tt.opts.NodeName))
			g.Expect(job.Annotations).To(HaveKeyWithValue(annotationCluster, tt.opts.ClusterName))
			g.Expect(job.Spec.Template.Annotations).To(HaveKeyWithValue(annotationNode, tt.opts.NodeName))
			g.Expect(job.Spec.Template.Annotations).To(HaveKeyWithValue(annotationCluster, tt.opts.ClusterName))

			// Verify Job spec.
			g.Expect(job.Spec.ActiveDeadlineSeconds).NotTo(BeNil())
			g.Expect(*job.Spec.ActiveDeadlineSeconds).To(BeEquivalentTo(int64(tt.opts.Timeout.Seconds())))

			spec := job.Spec.Template.Spec
			g.Expect(spec.NodeName).To(Equal(tt.opts.NodeName))
			g.Expect(spec.HostPID).To(BeTrue())
			g.Expect(spec.HostNetwork).To(BeTrue())
			g.Expect(spec.HostIPC).To(BeTrue())

			// Verify containers.
			g.Expect(spec.InitContainers).To(HaveLen(tt.wantInitContainers))
			g.Expect(spec.Containers).To(HaveLen(1))
			g.Expect(spec.Containers[0].Name).To(Equal(tt.wantContainerName))

			// Find sosreport init container by name.
			var sosInit *corev1.Container
			for i := range spec.InitContainers {
				if spec.InitContainers[i].Name == "sosreport" {
					sosInit = &spec.InitContainers[i]
					break
				}
			}
			g.Expect(sosInit).NotTo(BeNil(), "sosreport init container not found")
			g.Expect(sosInit.Image).To(Equal(tt.opts.Image))
			g.Expect(*sosInit.SecurityContext.Privileged).To(BeTrue())

			// Verify OUTPUT_DIR env override for subdir.
			if tt.wantOutputDirEnv != "" {
				g.Expect(findEnv(sosInit.Env, "OUTPUT_DIR")).To(Equal(tt.wantOutputDirEnv))
			}

			// Verify NFS UID is set on mkdir/copy/archive containers.
			if tt.opts.Output == OutputNFS {
				for _, ic := range spec.InitContainers {
					switch ic.Name {
					case "mkdir", "copy-to-nfs", "archive":
						g.Expect(ic.SecurityContext).NotTo(BeNil(), "expected SecurityContext on %s", ic.Name)
						g.Expect(*ic.SecurityContext.RunAsUser).To(Equal(tt.opts.NFSUID), "wrong RunAsUser on %s", ic.Name)
					}
				}
			}

			// Verify archive-only has no mkdir or copy containers.
			if tt.opts.ArchiveOnly {
				g.Expect(findInitContainer(spec.InitContainers, "mkdir")).To(BeNil(), "archive-only should not have mkdir")
				g.Expect(findInitContainer(spec.InitContainers, "copy-to-nfs")).To(BeNil(), "archive-only should not have copy-to-nfs")
				g.Expect(findInitContainer(spec.InitContainers, "archive")).NotTo(BeNil(), "archive-only should have archive")
			}

			// Verify --archive adds archive container alongside copy.
			if tt.opts.Archive && !tt.opts.ArchiveOnly && tt.opts.NFSSubDir != "" {
				g.Expect(findInitContainer(spec.InitContainers, "archive")).NotTo(BeNil(), "archive should be present")
				g.Expect(findInitContainer(spec.InitContainers, "copy-to-nfs")).NotTo(BeNil(), "copy-to-nfs should be present")
			}

			// Verify output volume.
			if tt.wantOutputVolume != nil {
				tt.wantOutputVolume(g, spec.Volumes)
			}
		})
	}
}

func TestOutputMode(t *testing.T) {
	g := NewWithT(t)
	g.Expect(string(OutputLocal)).To(Equal("local"))
	g.Expect(string(OutputNFS)).To(Equal("nfs"))
}

// findInitContainer returns the init container with the given name, or nil.
func findInitContainer(containers []corev1.Container, name string) *corev1.Container {
	for i := range containers {
		if containers[i].Name == name {
			return &containers[i]
		}
	}
	return nil
}

// findVolume returns the volume with the given name, or nil.
func findVolume(volumes []corev1.Volume, name string) *corev1.Volume {
	for i := range volumes {
		if volumes[i].Name == name {
			return &volumes[i]
		}
	}
	return nil
}

// findEnv returns the value of the env var with the given name.
func findEnv(envs []corev1.EnvVar, name string) string {
	for _, e := range envs {
		if e.Name == name {
			return e.Value
		}
	}
	return ""
}
