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

package dms

import (
	"context"
	"testing"

	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"
	dnutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/dpunode/util"
	cutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/util"
	hostutil "github.com/nvidia/doca-platform/internal/provisioning/hostagent/util"

	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func newTestNode(name string) *corev1.Node {
	return &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: name}}
}

func createPod(t *testing.T, g *WithT, option dnutil.HostAgentPodOptions) *corev1.Pod {
	t.Helper()
	scheme := runtime.NewScheme()
	g.Expect(corev1.AddToScheme(scheme)).To(Succeed())
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()

	node := newTestNode("worker-1")
	ownerRef := &metav1.OwnerReference{APIVersion: "v1", Kind: "Pod", Name: "owner", UID: "uid"}
	g.Expect(CreateHostAgentPod(context.Background(), fakeClient, node, option, "dpf-operator-system", ownerRef)).To(Succeed())

	pod := &corev1.Pod{}
	g.Expect(fakeClient.Get(context.Background(),
		types.NamespacedName{Namespace: "dpf-operator-system", Name: cutil.GenerateHostAgentPodName(node)}, pod)).To(Succeed())
	return pod
}

func TestCreateHostAgentPod_CATrustBundleMount(t *testing.T) {
	baseOption := func() dnutil.HostAgentPodOptions {
		return dnutil.HostAgentPodOptions{
			HostAgentImageWithTag: "example.com/hostagent:v1",
			BFBRegistryAddress:    "https://10.0.0.1:30443",
		}
	}

	findVolume := func(pod *corev1.Pod, name string) *corev1.Volume {
		for i := range pod.Spec.Volumes {
			if pod.Spec.Volumes[i].Name == name {
				return &pod.Spec.Volumes[i]
			}
		}
		return nil
	}
	findContainer := func(pod *corev1.Pod, name string) *corev1.Container {
		for i := range pod.Spec.Containers {
			if pod.Spec.Containers[i].Name == name {
				return &pod.Spec.Containers[i]
			}
		}
		return nil
	}

	t.Run("mounts the CA trust bundle into the hostagent container", func(t *testing.T) {
		g := NewWithT(t)
		pod := createPod(t, g, baseOption())

		vol := findVolume(pod, "ca-trust-bundle")
		g.Expect(vol).NotTo(BeNil())
		g.Expect(vol.ConfigMap).NotTo(BeNil())
		g.Expect(vol.ConfigMap.Name).To(Equal(operatorv1.DefaultCATrustBundleConfigMapName))
		g.Expect(vol.ConfigMap.Optional).To(Equal(ptr.To(true)))

		hostagent := findContainer(pod, "hostagent")
		g.Expect(hostagent).NotTo(BeNil())
		var mount *corev1.VolumeMount
		for i := range hostagent.VolumeMounts {
			if hostagent.VolumeMounts[i].Name == "ca-trust-bundle" {
				mount = &hostagent.VolumeMounts[i]
			}
		}
		g.Expect(mount).NotTo(BeNil())
		g.Expect(mount.MountPath).To(Equal(hostutil.CATrustBundleDir))
		g.Expect(mount.ReadOnly).To(BeTrue())
	})

	t.Run("honors a custom CA trust bundle ConfigMap name", func(t *testing.T) {
		g := NewWithT(t)
		option := baseOption()
		option.CATrustBundleConfigMapName = "custom-ca-bundle"
		pod := createPod(t, g, option)

		vol := findVolume(pod, "ca-trust-bundle")
		g.Expect(vol).NotTo(BeNil())
		g.Expect(vol.ConfigMap).NotTo(BeNil())
		g.Expect(vol.ConfigMap.Name).To(Equal("custom-ca-bundle"))
	})
}
