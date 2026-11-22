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

package bfbregistry

import (
	"context"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestBFBRegistryCreator(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "BFB Registry Creator Suite")
}

var _ = Describe("EnsureBFBRegistry", func() {
	const testNamespace = "bfb-registry-test"

	var (
		ctx       context.Context
		scheme    *runtime.Scheme
		leaderPod *corev1.Pod
	)

	BeforeEach(func() {
		ctx = context.Background()
		scheme = runtime.NewScheme()
		Expect(corev1.AddToScheme(scheme)).To(Succeed())
		leaderPod = &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "leader-pod",
				Namespace: testNamespace,
				UID:       "leader-uid",
			},
		}
	})

	It("returns error when leader pod does not exist", func() {
		c := fake.NewClientBuilder().WithScheme(scheme).Build()
		_, err := EnsureBFBRegistry(ctx, EnsureBFBRegistryDeps{Client: c}, testNamespace, "leader-pod", "node-1", "img")
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("get leader pod"))
	})

	It("creates the bfb-registry pod, and neither a Service nor a Certificate", func() {
		c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(leaderPod).Build()

		_, err := EnsureBFBRegistry(ctx, EnsureBFBRegistryDeps{Client: c}, testNamespace, "leader-pod", "node-1", "registry:8082")
		Expect(err).NotTo(HaveOccurred())

		pod := &corev1.Pod{}
		Expect(c.Get(ctx, client.ObjectKey{Namespace: testNamespace, Name: PodName}, pod)).To(Succeed())
		Expect(pod.Spec.NodeName).To(Equal("node-1"))
		Expect(pod.Labels[LabelDPUComponent]).To(Equal(LabelValue))
		Expect(podOwnedByLeaderPod(pod, leaderPod)).To(BeTrue())

		// The DPF Operator owns the Service and the server Certificate.
		svcList := &corev1.ServiceList{}
		Expect(c.List(ctx, svcList, client.InNamespace(testNamespace))).To(Succeed())
		Expect(svcList.Items).To(BeEmpty())
		certList := &unstructured.UnstructuredList{}
		certList.SetGroupVersionKind(schema.GroupVersionKind{Group: "cert-manager.io", Version: "v1", Kind: "CertificateList"})
		Expect(c.List(ctx, certList, client.InNamespace(testNamespace))).To(Succeed())
		Expect(certList.Items).To(BeEmpty())
	})

	It("succeeds when the pod already exists (no duplicate create)", func() {
		ref := leaderControllerRef(leaderPod)
		existingPod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:            PodName,
				Namespace:       testNamespace,
				OwnerReferences: []metav1.OwnerReference{*ref},
			},
			Spec: corev1.PodSpec{
				NodeName: "node-1",
				Containers: []corev1.Container{
					{Name: "bfb-registry", Image: "registry:8082"},
				},
			},
		}
		c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(leaderPod, existingPod).Build()

		_, err := EnsureBFBRegistry(ctx, EnsureBFBRegistryDeps{Client: c}, testNamespace, "leader-pod", "node-1", "registry:8082")
		Expect(err).NotTo(HaveOccurred())

		podList := &corev1.PodList{}
		Expect(c.List(ctx, podList, client.InNamespace(testNamespace))).To(Succeed())
		Expect(podList.Items).To(HaveLen(2)) // leader + bfb-registry
	})

	It("replaces the bfb-registry pod when owned by a different leader", func() {
		oldLeader := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "leader-old",
				Namespace: testNamespace,
				UID:       "old-uid",
			},
		}
		existingPod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:            PodName,
				Namespace:       testNamespace,
				OwnerReferences: []metav1.OwnerReference{*leaderControllerRef(oldLeader)},
			},
			Spec: corev1.PodSpec{
				NodeName: "node-1",
				Containers: []corev1.Container{
					{Name: "bfb-registry", Image: "registry:old"},
				},
			},
		}
		c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(leaderPod, oldLeader, existingPod).Build()

		_, err := EnsureBFBRegistry(ctx, EnsureBFBRegistryDeps{Client: c}, testNamespace, "leader-pod", "node-2", "registry:new")
		Expect(err).NotTo(HaveOccurred())

		pod := &corev1.Pod{}
		Expect(c.Get(ctx, client.ObjectKey{Namespace: testNamespace, Name: PodName}, pod)).To(Succeed())
		Expect(podOwnedByLeaderPod(pod, leaderPod)).To(BeTrue())
		Expect(pod.Spec.NodeName).To(Equal("node-2"))
		Expect(pod.Spec.Containers[0].Image).To(Equal("registry:new"))
	})
})

var _ = Describe("ensurePod with a terminated bfb-registry pod", func() {
	const testNamespace = "bfb-registry-test"

	var (
		ctx      context.Context
		scheme   *runtime.Scheme
		leader   *corev1.Pod
		fixedNow time.Time
	)

	BeforeEach(func() {
		ctx = context.Background()
		scheme = runtime.NewScheme()
		Expect(corev1.AddToScheme(scheme)).To(Succeed())
		leader = &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "leader-pod",
				Namespace: testNamespace,
				UID:       "leader-uid",
			},
		}
		fixedNow = time.Date(2026, 11, 3, 15, 0, 0, 0, time.UTC)
		origNow := now
		now = func() time.Time { return fixedNow }
		DeferCleanup(func() { now = origNow })
	})

	failedPod := func(owner *corev1.Pod, createdAgo time.Duration) *corev1.Pod {
		return &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:              PodName,
				Namespace:         testNamespace,
				OwnerReferences:   []metav1.OwnerReference{*leaderControllerRef(owner)},
				CreationTimestamp: metav1.Time{Time: fixedNow.Add(-createdAgo)},
			},
			Status: corev1.PodStatus{Phase: corev1.PodFailed},
		}
	}

	getRegistryPod := func(c client.Client) *corev1.Pod {
		p := &corev1.Pod{}
		Expect(c.Get(ctx, client.ObjectKey{Namespace: testNamespace, Name: PodName}, p)).To(Succeed())
		return p
	}

	It("replaces a failed pod once the backoff has passed", func() {
		c := fake.NewClientBuilder().WithScheme(scheme).
			WithObjects(leader, failedPod(leader, failedPodRecreateBackoff+time.Second)).Build()

		requeueAfter, err := (&BFBRegistryRunnable{Client: c}).ensurePod(ctx, testNamespace, "node-1", "img", leader)
		Expect(err).NotTo(HaveOccurred())
		Expect(requeueAfter).To(BeZero())
		p := getRegistryPod(c)
		Expect(p.Status.Phase).To(BeEmpty())
		Expect(podOwnedByLeaderPod(p, leader)).To(BeTrue())
	})

	It("keeps a failed pod inside the backoff and returns the rest of it to the caller", func() {
		c := fake.NewClientBuilder().WithScheme(scheme).
			WithObjects(leader, failedPod(leader, 10*time.Second)).Build()

		requeueAfter, err := EnsureBFBRegistry(ctx, EnsureBFBRegistryDeps{Client: c}, testNamespace, "leader-pod", "node-1", "img")
		Expect(err).NotTo(HaveOccurred())
		Expect(requeueAfter).To(Equal(failedPodRecreateBackoff - 10*time.Second))
		Expect(getRegistryPod(c).Status.Phase).To(Equal(corev1.PodFailed))
	})

	It("replaces a failed pod of another leader without waiting for the backoff", func() {
		oldLeader := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "leader-old",
				Namespace: testNamespace,
				UID:       "old-uid",
			},
		}
		c := fake.NewClientBuilder().WithScheme(scheme).
			WithObjects(leader, failedPod(oldLeader, 5*time.Second)).Build()

		requeueAfter, err := (&BFBRegistryRunnable{Client: c}).ensurePod(ctx, testNamespace, "node-1", "img", leader)
		Expect(err).NotTo(HaveOccurred())
		Expect(requeueAfter).To(BeZero())
		Expect(podOwnedByLeaderPod(getRegistryPod(c), leader)).To(BeTrue())
	})

	It("leaves a terminating pod for its delete event to handle", func() {
		terminating := failedPod(leader, time.Hour)
		terminating.Finalizers = []string{"test.dpu.nvidia.com/hold"}
		terminating.DeletionTimestamp = &metav1.Time{Time: fixedNow}
		c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(leader, terminating).Build()

		requeueAfter, err := (&BFBRegistryRunnable{Client: c}).ensurePod(ctx, testNamespace, "node-1", "img", leader)
		Expect(err).NotTo(HaveOccurred())
		Expect(requeueAfter).To(BeZero())
		Expect(getRegistryPod(c).DeletionTimestamp).NotTo(BeNil())
	})
})

var _ = Describe("desiredPod HTTPS wiring", func() {
	const ns = "dpf-provisioning"

	var pod *corev1.Pod

	BeforeEach(func() {
		leader := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "leader", Namespace: ns, UID: "leader-uid"}}
		r := &BFBRegistryRunnable{}
		pod = r.desiredPod(ns, "node-1", "registry:1.0", leaderControllerRef(leader))
	})

	It("shares the PID namespace for the reloader sidecar", func() {
		Expect(pod.Spec.ShareProcessNamespace).NotTo(BeNil())
		Expect(*pod.Spec.ShareProcessNamespace).To(BeTrue())
	})

	It("runs nginx and the cert-reloader sidecar", func() {
		names := make([]string, 0, len(pod.Spec.Containers))
		for _, c := range pod.Spec.Containers {
			names = append(names, c.Name)
		}
		Expect(names).To(ConsistOf("bfb-registry", "cert-reloader"))
	})

	It("exposes only the HTTPS port and feeds NGINX_HTTPS_PORT", func() {
		nginx := containerByName(pod, "bfb-registry")
		Expect(nginx.Ports).To(HaveLen(1))
		Expect(nginx.Ports[0].Name).To(Equal("https"))
		Expect(nginx.Ports[0].ContainerPort).To(Equal(int32(HTTPSContainerPort)))
		Expect(nginx.Env).To(ContainElement(corev1.EnvVar{Name: "NGINX_HTTPS_PORT", Value: "8443"}))
	})

	It("mounts the server cert into both containers without subPath", func() {
		for _, name := range []string{"bfb-registry", "cert-reloader"} {
			c := containerByName(pod, name)
			mount := volumeMountByName(c, "server-cert")
			Expect(mount).NotTo(BeNil(), "container %s should mount server-cert", name)
			Expect(mount.MountPath).To(Equal(ServerCertMountPath))
			Expect(mount.SubPath).To(BeEmpty())
			Expect(mount.ReadOnly).To(BeTrue())
		}
	})

	It("backs the server-cert volume with the cert Secret (required, mode 0440)", func() {
		vol := volumeByName(pod, "server-cert")
		Expect(vol).NotTo(BeNil())
		Expect(vol.Secret).NotTo(BeNil())
		Expect(vol.Secret.SecretName).To(Equal(ServerCertSecretName))
		Expect(vol.Secret.Optional).NotTo(BeNil())
		Expect(*vol.Secret.Optional).To(BeFalse())
		Expect(vol.Secret.DefaultMode).NotTo(BeNil())
		Expect(*vol.Secret.DefaultMode).To(Equal(int32(0o440)))
	})
})

var _ = Describe("APIServerVIPFromDMSPodEnvs", func() {
	It("returns the KUBERNETES_SERVICE_HOST value when present", func() {
		Expect(APIServerVIPFromDMSPodEnvs([]string{
			"KUBERNETES_SERVICE_HOST=10.0.0.10",
			"KUBERNETES_SERVICE_PORT=6443",
		})).To(Equal("10.0.0.10"))
	})

	It("returns empty when the variable is absent", func() {
		Expect(APIServerVIPFromDMSPodEnvs([]string{"FOO=bar"})).To(BeEmpty())
		Expect(APIServerVIPFromDMSPodEnvs(nil)).To(BeEmpty())
	})
})

func containerByName(pod *corev1.Pod, name string) *corev1.Container {
	for i := range pod.Spec.Containers {
		if pod.Spec.Containers[i].Name == name {
			return &pod.Spec.Containers[i]
		}
	}
	return nil
}

func volumeByName(pod *corev1.Pod, name string) *corev1.Volume {
	for i := range pod.Spec.Volumes {
		if pod.Spec.Volumes[i].Name == name {
			return &pod.Spec.Volumes[i]
		}
	}
	return nil
}

func volumeMountByName(c *corev1.Container, name string) *corev1.VolumeMount {
	for i := range c.VolumeMounts {
		if c.VolumeMounts[i].Name == name {
			return &c.VolumeMounts[i]
		}
	}
	return nil
}
