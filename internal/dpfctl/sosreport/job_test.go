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
			wantInitContainers: 1,
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
			wantInitContainers: 2,
			wantOutputVolume: func(g Gomega, volumes []corev1.Volume) {
				v := findVolume(volumes, "output")
				g.Expect(v).NotTo(BeNil())
				g.Expect(v.VolumeSource.NFS).NotTo(BeNil())
			},
			wantOutputDirEnv: "/sos-output/sosreport-20260416-120000",
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
			g.Expect(job.Name).To(Equal(jobName(tt.opts.NodeName)))
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

			// Verify sosreport init container is always present (last init container).
			sosInit := spec.InitContainers[len(spec.InitContainers)-1]
			g.Expect(sosInit.Name).To(Equal("sosreport"))
			g.Expect(sosInit.Image).To(Equal(tt.opts.Image))
			g.Expect(*sosInit.SecurityContext.Privileged).To(BeTrue())

			// Verify mkdir init container when expected.
			if tt.wantInitContainers == 2 {
				g.Expect(spec.InitContainers[0].Name).To(Equal("mkdir"))
			}

			// Verify OUTPUT_DIR env override for subdir.
			if tt.wantOutputDirEnv != "" {
				g.Expect(findEnv(sosInit.Env, "OUTPUT_DIR")).To(Equal(tt.wantOutputDirEnv))
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
