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

package controllers

import (
	"strings"

	noderesourcesv1 "github.com/nvidia/doca-platform/api/noderesources/v1alpha1"
	"github.com/nvidia/doca-platform/internal/nodesriovdeviceplugin/common"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
)

var _ = Describe("Pod", func() {
	Context("generatePodName", func() {
		It("should generate pod name with node name suffix", func() {
			Expect(generatePodName("my-node")).To(Equal("dpf-sriov-device-plugin-my-node"))
			Expect(generatePodName("my-other-node")).To(Equal("dpf-sriov-device-plugin-my-other-node"))
		})
		It("should generate hashed name when result exceeds 253 characters", func() {
			longNodeName := strings.Repeat("a", 254-len("dpf-sriov-device-plugin-"))
			Expect(len("dpf-sriov-device-plugin-" + longNodeName)).To(BeNumerically(">", 253))

			name := generatePodName(longNodeName)
			Expect(name).To(HavePrefix("dpf-sriov-device-plugin-"))
			Expect(len(name)).To(BeNumerically("<=", 253))
		})
		It("should generate different names for different node names", func() {
			longNodeName1 := strings.Repeat("a", 254-len("dpf-sriov-device-plugin-"))
			longNodeName2 := strings.Repeat("b", 254-len("dpf-sriov-device-plugin-"))
			Expect(generatePodName(longNodeName1)).NotTo(Equal(generatePodName(longNodeName2)))
		})
		It("should generate consistent name for the same node name", func() {
			longNodeName := strings.Repeat("a", 254-len("dpf-sriov-device-plugin-"))
			Expect(generatePodName(longNodeName)).To(Equal(generatePodName(longNodeName)))
		})
	})
	Context("getImagePullSecrets", func() {
		It("should return nil when no secrets configured", func() {
			config := &DevicePluginConfig{
				ImagePullSecrets: nil,
			}
			secrets := getImagePullSecrets(config)
			Expect(secrets).To(BeNil())
		})
		It("should return nil when secrets list is empty", func() {
			config := &DevicePluginConfig{
				ImagePullSecrets: []string{},
			}
			secrets := getImagePullSecrets(config)
			Expect(secrets).To(BeNil())
		})
		It("should return secrets when configured", func() {
			config := &DevicePluginConfig{
				ImagePullSecrets: []string{"secret1", "secret2"},
			}
			secrets := getImagePullSecrets(config)
			Expect(secrets).To(HaveLen(2))
			Expect(secrets[0].Name).To(Equal("secret1"))
			Expect(secrets[1].Name).To(Equal("secret2"))
		})
	})
	Context("buildDesiredPod", func() {
		var (
			nodeName           string
			namespace          string
			inputConfig        common.NodeInputConfig
			devicePluginConfig *DevicePluginConfig
		)
		BeforeEach(func() {
			nodeName = "test-node"
			namespace = "test-namespace"
			inputConfig = common.NodeInputConfig{
				"SN1234": {
					{
						Name: "pods_vf",
						Type: noderesourcesv1.DevicePluginResourceTypeVF,
						Ranges: []noderesourcesv1.VFRange{
							{PFIndex: 0, Start: ptr.To(int32(2)), End: ptr.To(int32(10))},
						},
					},
				},
			}
			devicePluginConfig = &DevicePluginConfig{
				Image:                 "device-plugin:latest",
				InitImage:             "init-image:latest",
				DefaultResourcePrefix: "nvidia.com",
			}
		})
		It("should create pod with correct labels and annotations", func() {
			pod, err := buildDesiredPod(nodeName, namespace, inputConfig, devicePluginConfig, nil)
			Expect(err).NotTo(HaveOccurred())
			Expect(pod.Name).To(Equal("dpf-sriov-device-plugin-test-node"))
			Expect(pod.Namespace).To(Equal(namespace))
			Expect(pod.Labels).To(HaveKeyWithValue(ManagedByLabelKey, ManagedByLabelValue))
			Expect(pod.Annotations).To(HaveKey(PodInputAnnotationKey))
			Expect(pod.Annotations).To(HaveKey(PodObjectHashAnnotationKey))
			Expect(pod.OwnerReferences).To(BeNil())
		})
		It("should set owner references when provided", func() {
			ownerRefs := []metav1.OwnerReference{
				{
					APIVersion: "v1",
					Kind:       "ConfigMap",
					Name:       "owner-config",
					UID:        types.UID("uid-123"),
				},
			}
			pod, err := buildDesiredPod(nodeName, namespace, inputConfig, devicePluginConfig, ownerRefs)
			Expect(err).NotTo(HaveOccurred())
			Expect(pod.OwnerReferences).To(HaveLen(1))
			Expect(pod.OwnerReferences[0]).To(Equal(ownerRefs[0]))
		})
		It("should set node affinity to target node", func() {
			pod, err := buildDesiredPod(nodeName, namespace, inputConfig, devicePluginConfig, nil)
			Expect(err).NotTo(HaveOccurred())
			Expect(pod.Spec.Affinity).NotTo(BeNil())
			Expect(pod.Spec.Affinity.NodeAffinity).NotTo(BeNil())
			nodeSelector := pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution
			Expect(nodeSelector).NotTo(BeNil())
			Expect(nodeSelector.NodeSelectorTerms).To(HaveLen(1))
			Expect(nodeSelector.NodeSelectorTerms[0].MatchFields).To(HaveLen(1))
			Expect(nodeSelector.NodeSelectorTerms[0].MatchFields[0].Key).To(Equal(metav1.ObjectNameField))
			Expect(nodeSelector.NodeSelectorTerms[0].MatchFields[0].Values).To(ConsistOf(nodeName))
		})
		It("should configure init container correctly", func() {
			pod, err := buildDesiredPod(nodeName, namespace, inputConfig, devicePluginConfig, nil)
			Expect(err).NotTo(HaveOccurred())
			Expect(pod.Spec.InitContainers).To(HaveLen(1))
			initContainer := pod.Spec.InitContainers[0]
			Expect(initContainer.Name).To(Equal(initContainerName))
			Expect(initContainer.Image).To(Equal(devicePluginConfig.InitImage))
			Expect(initContainer.Args).To(ContainElement(ContainSubstring("--input-path=")))
			Expect(initContainer.Args).To(ContainElement(ContainSubstring("--output-path=")))
			Expect(initContainer.Args).To(ContainElement(ContainSubstring("--default-resource-prefix=")))
			Expect(initContainer.SecurityContext).NotTo(BeNil())
			Expect(initContainer.SecurityContext.Privileged).To(Equal(ptr.To(true)))
		})
		It("should configure main container correctly", func() {
			pod, err := buildDesiredPod(nodeName, namespace, inputConfig, devicePluginConfig, nil)
			Expect(err).NotTo(HaveOccurred())
			Expect(pod.Spec.Containers).To(HaveLen(1))
			mainContainer := pod.Spec.Containers[0]
			Expect(mainContainer.Name).To(Equal(mainContainerName))
			Expect(mainContainer.Image).To(Equal(devicePluginConfig.Image))
			Expect(mainContainer.Args).To(ContainElement("--config-file=/etc/pcidp/config.json"))
			Expect(mainContainer.Args).To(ContainElement("--resource-prefix=nvidia.com"))
			Expect(mainContainer.SecurityContext).NotTo(BeNil())
			Expect(mainContainer.SecurityContext.Privileged).To(Equal(ptr.To(true)))
		})
		It("should configure volumes correctly", func() {
			pod, err := buildDesiredPod(nodeName, namespace, inputConfig, devicePluginConfig, nil)
			Expect(err).NotTo(HaveOccurred())
			Expect(pod.Spec.Volumes).To(HaveLen(4))
			volumeNames := make([]string, len(pod.Spec.Volumes))
			for i, v := range pod.Spec.Volumes {
				volumeNames[i] = v.Name
			}
			Expect(volumeNames).To(ContainElements(
				configVolumeName,
				downwardAPIVolumeName,
				devicePluginSocketVolumeName,
				sysVolumeName,
			))
		})
		It("should include image pull secrets when configured", func() {
			devicePluginConfig.ImagePullSecrets = []string{"my-secret"}
			pod, err := buildDesiredPod(nodeName, namespace, inputConfig, devicePluginConfig, nil)
			Expect(err).NotTo(HaveOccurred())
			Expect(pod.Spec.ImagePullSecrets).To(HaveLen(1))
			Expect(pod.Spec.ImagePullSecrets[0].Name).To(Equal("my-secret"))
		})
		It("should not include image pull secrets when not configured", func() {
			pod, err := buildDesiredPod(nodeName, namespace, inputConfig, devicePluginConfig, nil)
			Expect(err).NotTo(HaveOccurred())
			Expect(pod.Spec.ImagePullSecrets).To(BeNil())
		})
		It("should set host network and DNS policy", func() {
			pod, err := buildDesiredPod(nodeName, namespace, inputConfig, devicePluginConfig, nil)
			Expect(err).NotTo(HaveOccurred())
			Expect(pod.Spec.HostNetwork).To(BeTrue())
			Expect(pod.Spec.DNSPolicy).To(Equal(corev1.DNSClusterFirstWithHostNet))
		})
		It("should set priority class", func() {
			pod, err := buildDesiredPod(nodeName, namespace, inputConfig, devicePluginConfig, nil)
			Expect(err).NotTo(HaveOccurred())
			Expect(pod.Spec.PriorityClassName).To(Equal("system-node-critical"))
		})
		It("should configure tolerations", func() {
			pod, err := buildDesiredPod(nodeName, namespace, inputConfig, devicePluginConfig, nil)
			Expect(err).NotTo(HaveOccurred())
			Expect(pod.Spec.Tolerations).To(HaveLen(1))
			Expect(pod.Spec.Tolerations[0].Operator).To(Equal(corev1.TolerationOpExists))
		})
	})
	Context("isPodOutdated", func() {
		It("should return true when hash differs", func() {
			existing := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{PodObjectHashAnnotationKey: "hash1"},
				},
			}
			desired := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{PodObjectHashAnnotationKey: "hash2"},
				},
			}
			Expect(isPodOutdated(existing, desired)).To(BeTrue())
		})
		It("should return false when hash is same", func() {
			existing := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{PodObjectHashAnnotationKey: "hash1"},
				},
			}
			desired := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{PodObjectHashAnnotationKey: "hash1"},
				},
			}
			Expect(isPodOutdated(existing, desired)).To(BeFalse())
		})
	})
	Context("isPodInTerminalState", func() {
		It("should return true when pod phase is Succeeded", func() {
			pod := &corev1.Pod{
				Status: corev1.PodStatus{Phase: corev1.PodSucceeded},
			}
			Expect(isPodInTerminalState(pod)).To(BeTrue())
		})
		It("should return true when pod phase is Failed", func() {
			pod := &corev1.Pod{
				Status: corev1.PodStatus{Phase: corev1.PodFailed},
			}
			Expect(isPodInTerminalState(pod)).To(BeTrue())
		})
		It("should return false when pod phase is Running", func() {
			pod := &corev1.Pod{
				Status: corev1.PodStatus{Phase: corev1.PodRunning},
			}
			Expect(isPodInTerminalState(pod)).To(BeFalse())
		})
		It("should return false when pod phase is Pending", func() {
			pod := &corev1.Pod{
				Status: corev1.PodStatus{Phase: corev1.PodPending},
			}
			Expect(isPodInTerminalState(pod)).To(BeFalse())
		})
	})
	Context("computePodObjectHash", func() {
		It("should return consistent hash for same pod", func() {
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "test-ns",
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{Name: "test", Image: "test:latest"},
					},
				},
			}
			hash1 := computePodObjectHash(pod)
			hash2 := computePodObjectHash(pod)
			Expect(hash1).To(Equal(hash2))
		})
		It("should return different hash when pod spec changes", func() {
			pod1 := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "test-ns",
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{Name: "test", Image: "test:v1"},
					},
				},
			}
			pod2 := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "test-ns",
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{Name: "test", Image: "test:v2"},
					},
				},
			}
			hash1 := computePodObjectHash(pod1)
			hash2 := computePodObjectHash(pod2)
			Expect(hash1).NotTo(Equal(hash2))
		})
		It("should return hash of expected length", func() {
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{Name: "test-pod"},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{Name: "test", Image: "test:latest"},
					},
				},
			}
			hash := computePodObjectHash(pod)
			Expect(hash).To(HaveLen(10))
		})
	})
})
