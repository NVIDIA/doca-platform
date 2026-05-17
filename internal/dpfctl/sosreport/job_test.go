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
	"regexp"
	"testing"
	"time"

	. "github.com/onsi/gomega"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestCreateJob(t *testing.T) {
	tests := []struct {
		name               string
		opts               JobOptions
		wantContainerName  string
		wantInitContainers int
		wantOutputVolume   func(g Gomega, volumes []corev1.Volume)
		wantOutputDirEnv   string
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
			g.Expect(job.Name).To(Equal(jobName(tt.opts.CaseID, tt.opts.NodeName)))
			g.Expect(job.Namespace).To(Equal(tt.opts.Namespace))
			g.Expect(job.Labels).To(HaveKeyWithValue(labelCaseID, tt.opts.CaseID))
			g.Expect(job.Labels).To(HaveKeyWithValue(labelNode, tt.opts.NodeName))
			g.Expect(job.Labels).To(HaveKeyWithValue(labelCluster, tt.opts.ClusterName))

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

// rfc1123Re matches valid Kubernetes resource names (RFC 1123 subdomain).
var rfc1123Re = regexp.MustCompile(`^[a-z0-9]([a-z0-9\-.]*[a-z0-9])?$`)

func TestJobName(t *testing.T) {
	tests := []struct {
		name     string
		caseID   string
		nodeName string
	}{
		{
			name:     "short name is returned unchanged",
			caseID:   "dpf-20260517-120000",
			nodeName: "worker-1",
		},
		{
			name:     "exactly 63 bytes is returned unchanged",
			caseID:   "dpf-20260517-120000",
			nodeName: "node-123456789012345678", // "sos-dpf-20260517-120000-node-123456789012345678" = 63 bytes
		},
		{
			// Total name = 64 bytes — just over the limit; slice boundary lands on '.'.
			name:     "long FQDN node name is truncated with hash",
			caseID:   "dpf-20260517-081215",
			nodeName: "worker-01.zone-a.internal.example.cluster.test",
		},
		{
			// DPU node names are host FQDN + DPU suffix, making them even longer.
			name:     "long DPU node name is truncated with hash",
			caseID:   "dpf-20260517-081215",
			nodeName: "worker-01.zone-a.internal.example.cluster.test-dpu0",
		},
		{
			// The 54-byte slice boundary lands on '.' — TrimRight must strip it.
			name:     "truncation strips trailing dot before hash",
			caseID:   "dpf-20260517-081215",
			nodeName: "host-01.rack-a.internal.example.dc.cluster.corp.test",
		},
		{
			// The 54-byte slice boundary lands on '-' — TrimRight must strip it.
			name:     "truncation strips trailing dash before hash",
			caseID:   "dpf-20260517-081215",
			nodeName: "node-aaaaaaaaaaaaaaaaaaaaaaaa-bbbbbbbbbbbbbbbbbbbbb",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			result := jobName(tt.caseID, tt.nodeName)

			g.Expect(len(result)).To(BeNumerically("<=", 63),
				"job name must be at most 63 bytes, got %d: %q", len(result), result)

			g.Expect(rfc1123Re.MatchString(result)).To(BeTrue(),
				"job name must be a valid RFC 1123 subdomain, got %q", result)
		})
	}
}

func TestJobNameUniqueness(t *testing.T) {
	g := NewWithT(t)
	caseID := "dpf-20260517-081215"

	// Two nodes whose names share the same 54-char prefix — hash must distinguish them.
	name1 := jobName(caseID, "worker-01.zone-a.internal.example.cluster.test")
	name2 := jobName(caseID, "worker-01.zone-a.internal.example.cluster.test-dpu0")
	g.Expect(name1).NotTo(Equal(name2), "different long node names must produce different job names")
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
