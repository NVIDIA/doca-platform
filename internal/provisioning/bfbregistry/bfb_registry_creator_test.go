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
	"sync/atomic"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

func TestBFBRegistryCreator(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "BFB Registry Creator Suite")
}

var _ = Describe("EnsureBFBRegistry", func() {
	const testNamespace = "bfb-registry-test"

	var (
		ctx    context.Context
		scheme *runtime.Scheme
	)

	BeforeEach(func() {
		ctx = context.Background()
		scheme = runtime.NewScheme()
		Expect(corev1.AddToScheme(scheme)).To(Succeed())
	})

	It("returns error when leader pod does not exist", func() {
		c := fake.NewClientBuilder().WithScheme(scheme).Build()
		err := EnsureBFBRegistry(ctx, EnsureBFBRegistryDeps{Client: c}, testNamespace, "leader-pod", "node-1", "10.0.0.1", "img")
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("get leader pod"))
	})

	It("creates bfb-registry pod and service when both not exist", func() {
		leaderPod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "leader-pod",
				Namespace: testNamespace,
				UID:       "leader-uid",
			},
		}
		c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(leaderPod).Build()

		err := EnsureBFBRegistry(ctx, EnsureBFBRegistryDeps{Client: c}, testNamespace, "leader-pod", "node-1", "10.0.0.1", "registry:8082")
		Expect(err).NotTo(HaveOccurred())

		pod := &corev1.Pod{}
		Expect(c.Get(ctx, client.ObjectKey{Namespace: testNamespace, Name: PodName}, pod)).To(Succeed())
		Expect(pod.Spec.NodeName).To(Equal("node-1"))
		Expect(pod.Labels[LabelDPUComponent]).To(Equal(LabelValue))

		svc := &corev1.Service{}
		Expect(c.Get(ctx, client.ObjectKey{Namespace: testNamespace, Name: PodName}, svc)).To(Succeed())
		Expect(svc.Spec.Type).To(Equal(corev1.ServiceTypeNodePort))
		// HTTPS-only: exactly one port named "https" on the HTTPS container port.
		Expect(svc.Spec.Ports).To(HaveLen(1))
		Expect(svc.Spec.Ports[0].Name).To(Equal("https"))
		Expect(svc.Spec.Ports[0].Port).To(Equal(int32(HTTPSContainerPort)))
		Expect(svc.Spec.Ports[0].TargetPort.IntValue()).To(Equal(HTTPSContainerPort))
	})

	It("succeeds when pod and service already exist (no duplicate create)", func() {
		leaderPod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "leader-pod",
				Namespace: testNamespace,
				UID:       "leader-uid",
			},
		}
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
		existingSvc := &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:            PodName,
				Namespace:       testNamespace,
				OwnerReferences: []metav1.OwnerReference{*ref},
			},
			Spec: corev1.ServiceSpec{Type: corev1.ServiceTypeNodePort},
		}
		c := fake.NewClientBuilder().WithScheme(scheme).
			WithObjects(leaderPod, existingPod, existingSvc).
			Build()

		err := EnsureBFBRegistry(ctx, EnsureBFBRegistryDeps{Client: c}, testNamespace, "leader-pod", "node-1", "10.0.0.1", "registry:8082")
		Expect(err).NotTo(HaveOccurred())

		// Should still have exactly one pod and one service (no duplicates)
		podList := &corev1.PodList{}
		Expect(c.List(ctx, podList, client.InNamespace(testNamespace))).To(Succeed())
		Expect(podList.Items).To(HaveLen(2)) // leader + bfb-registry
		svcList := &corev1.ServiceList{}
		Expect(c.List(ctx, svcList, client.InNamespace(testNamespace))).To(Succeed())
		Expect(svcList.Items).To(HaveLen(1))
	})

	It("replaces bfb-registry pod and re-parents service when owned by a different leader", func() {
		oldLeader := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "leader-old",
				Namespace: testNamespace,
				UID:       "old-uid",
			},
		}
		newLeader := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "leader-new",
				Namespace: testNamespace,
				UID:       "new-uid",
			},
		}
		refOld := leaderControllerRef(oldLeader)
		existingPod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:            PodName,
				Namespace:       testNamespace,
				OwnerReferences: []metav1.OwnerReference{*refOld},
			},
			Spec: corev1.PodSpec{
				NodeName: "node-1",
				Containers: []corev1.Container{
					{Name: "bfb-registry", Image: "registry:8082"},
				},
			},
		}
		existingSvc := &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:            PodName,
				Namespace:       testNamespace,
				OwnerReferences: []metav1.OwnerReference{*refOld},
			},
			Spec: corev1.ServiceSpec{Type: corev1.ServiceTypeNodePort},
		}
		c := fake.NewClientBuilder().WithScheme(scheme).
			WithObjects(oldLeader, newLeader, existingPod, existingSvc).
			Build()

		err := EnsureBFBRegistry(ctx, EnsureBFBRegistryDeps{Client: c}, testNamespace, "leader-new", "node-2", "10.0.0.2", "registry:8082")
		Expect(err).NotTo(HaveOccurred())

		p := &corev1.Pod{}
		Expect(c.Get(ctx, client.ObjectKey{Namespace: testNamespace, Name: PodName}, p)).To(Succeed())
		Expect(podOwnedByLeaderPod(p, newLeader)).To(BeTrue())
		Expect(p.Spec.NodeName).To(Equal("node-2"))

		svc := &corev1.Service{}
		Expect(c.Get(ctx, client.ObjectKey{Namespace: testNamespace, Name: PodName}, svc)).To(Succeed())
		Expect(serviceOwnedByLeaderPod(svc, newLeader)).To(BeTrue())
	})

	It("does not fail when service create races with an existing service", func() {
		leaderPod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "leader-pod",
				Namespace: testNamespace,
				UID:       "leader-uid",
			},
		}
		ref := leaderControllerRef(leaderPod)
		existingSvc := &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:            PodName,
				Namespace:       testNamespace,
				OwnerReferences: []metav1.OwnerReference{*ref},
			},
			Spec: corev1.ServiceSpec{Type: corev1.ServiceTypeNodePort},
		}

		var staleReadServed atomic.Bool
		c := fake.NewClientBuilder().WithScheme(scheme).
			WithObjects(leaderPod, existingSvc).
			WithInterceptorFuncs(interceptor.Funcs{
				Get: func(ctx context.Context, client client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
					if _, ok := obj.(*corev1.Service); ok && key.Namespace == testNamespace && key.Name == PodName && !staleReadServed.Load() {
						staleReadServed.Store(true)
						return apierrors.NewNotFound(schema.GroupResource{Group: "", Resource: "services"}, key.Name)
					}
					return client.Get(ctx, key, obj, opts...)
				},
			}).Build()

		run := &BFBRegistryRunnable{Client: c}
		err := run.ensureService(ctx, testNamespace, leaderPod)
		Expect(err).NotTo(HaveOccurred())
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
		var names []string
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

var _ = Describe("ensureServerCertificate", func() {
	const ns = "dpf-provisioning"

	var (
		ctx    context.Context
		scheme *runtime.Scheme
		leader *corev1.Pod
	)

	BeforeEach(func() {
		ctx = context.Background()
		scheme = runtime.NewScheme()
		Expect(corev1.AddToScheme(scheme)).To(Succeed())
		leader = &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "leader", Namespace: ns, UID: "leader-uid"}}
	})

	getCert := func(c client.Client) *unstructured.Unstructured {
		u := &unstructured.Unstructured{}
		u.SetGroupVersionKind(certificateGVK)
		Expect(c.Get(ctx, client.ObjectKey{Namespace: ns, Name: ServerCertSecretName}, u)).To(Succeed())
		return u
	}

	It("creates a Certificate with the expected spec, SANs and owner", func() {
		c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(leader).Build()
		r := &BFBRegistryRunnable{Client: c}
		Expect(r.ensureServerCertificate(ctx, ns, "10.0.0.5", leader)).To(Succeed())

		u := getCert(c)
		Expect(u.GetOwnerReferences()).To(HaveLen(1))
		Expect(u.GetOwnerReferences()[0].Name).To(Equal("leader"))

		secretName, _, _ := unstructured.NestedString(u.Object, "spec", "secretName")
		Expect(secretName).To(Equal(ServerCertSecretName))
		cn, _, _ := unstructured.NestedString(u.Object, "spec", "commonName")
		Expect(cn).To(Equal(PodName))
		issuer, _, _ := unstructured.NestedString(u.Object, "spec", "issuerRef", "name")
		Expect(issuer).To(Equal(ServerCertIssuerName))
		alg, _, _ := unstructured.NestedString(u.Object, "spec", "privateKey", "algorithm")
		Expect(alg).To(Equal("ECDSA"))

		dns, _, _ := unstructured.NestedStringSlice(u.Object, "spec", "dnsNames")
		Expect(dns).To(ConsistOf(
			"bfb-registry",
			"bfb-registry."+ns,
			"bfb-registry."+ns+".svc",
			"bfb-registry."+ns+".svc.cluster.local",
		))
		ips, _, _ := unstructured.NestedStringSlice(u.Object, "spec", "ipAddresses")
		Expect(ips).To(ConsistOf("10.0.0.5"))
	})

	It("is idempotent and updates the IP SAN on node failover", func() {
		c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(leader).Build()
		r := &BFBRegistryRunnable{Client: c}
		Expect(r.ensureServerCertificate(ctx, ns, "10.0.0.5", leader)).To(Succeed())
		Expect(r.ensureServerCertificate(ctx, ns, "10.0.0.5", leader)).To(Succeed())

		// Leader fails over to a new node: the IP SAN must follow the new NODE_IP.
		Expect(r.ensureServerCertificate(ctx, ns, "10.0.0.9", leader)).To(Succeed())
		ips, _, _ := unstructured.NestedStringSlice(getCert(c).Object, "spec", "ipAddresses")
		Expect(ips).To(ConsistOf("10.0.0.9"))
	})

	It("returns an error when NODE_IP is empty", func() {
		c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(leader).Build()
		r := &BFBRegistryRunnable{Client: c}
		err := r.ensureServerCertificate(ctx, ns, "", leader)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("NODE_IP is empty"))
	})
})

var _ = Describe("serverCertSANs", func() {
	It("includes the node IP and the service DNS names", func() {
		dns, ips := serverCertSANs("dpf-provisioning", "192.168.1.10")
		Expect(dns).To(ConsistOf(
			"bfb-registry",
			"bfb-registry.dpf-provisioning",
			"bfb-registry.dpf-provisioning.svc",
			"bfb-registry.dpf-provisioning.svc.cluster.local",
		))
		Expect(ips).To(ConsistOf("192.168.1.10"))
	})

	It("omits the IP SAN when the node IP is not a valid IP", func() {
		_, ips := serverCertSANs("dpf-provisioning", "not-an-ip")
		Expect(ips).To(BeEmpty())
	})

	It("includes the kube-vip VIP from KUBERNETES_SERVICE_HOST when it differs from the node IP", func() {
		GinkgoT().Setenv("KUBERNETES_SERVICE_HOST", "10.0.0.10")
		_, ips := serverCertSANs("dpf-provisioning", "10.0.0.3")
		Expect(ips).To(ConsistOf("10.0.0.3", "10.0.0.10"))
	})

	It("does not duplicate the node IP when KUBERNETES_SERVICE_HOST matches it", func() {
		GinkgoT().Setenv("KUBERNETES_SERVICE_HOST", "10.0.0.3")
		_, ips := serverCertSANs("dpf-provisioning", "10.0.0.3")
		Expect(ips).To(ConsistOf("10.0.0.3"))
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
