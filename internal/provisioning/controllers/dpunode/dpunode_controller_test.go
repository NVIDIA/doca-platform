/*
Copyright 2025 NVIDIA

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

package dpunode

import (
	"context"
	"encoding/json"
	"fmt"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	cutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/util"

	"github.com/fluxcd/pkg/runtime/patch"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

const dpuInstallInterface = "eth0"

// errorInjectingClient wraps a client and injects errors for specific operations
type dpuNodeErrorInjectingClient struct {
	client.Client
	listFunc   func(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error
	deleteFunc func(ctx context.Context, obj client.Object, opts ...client.DeleteOption) error
}

func (c *dpuNodeErrorInjectingClient) List(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
	if c.listFunc != nil {
		return c.listFunc(ctx, list, opts...)
	}
	return c.Client.List(ctx, list, opts...)
}

func (c *dpuNodeErrorInjectingClient) Delete(ctx context.Context, obj client.Object, opts ...client.DeleteOption) error {
	if c.deleteFunc != nil {
		return c.deleteFunc(ctx, obj, opts...)
	}
	return c.Client.Delete(ctx, obj, opts...)
}

// Helper function to create a DPUNode for testing
func createTestDPUNode(name string, finalizers []string) *provisioningv1.DPUNode {
	return &provisioningv1.DPUNode{
		ObjectMeta: metav1.ObjectMeta{
			Name:       name,
			Namespace:  "test-namespace",
			Finalizers: finalizers,
		},
		Spec: provisioningv1.DPUNodeSpec{
			NodeRebootMethod: &provisioningv1.NodeRebootMethod{},
		},
	}
}

var _ = Describe("DPUNodeReconciler Non exported", func() {
	Context("generateJobName", func() {
		var (
			reconciler *DPUNodeReconciler
		)

		BeforeEach(func() {
			reconciler = &DPUNodeReconciler{}
		})

		It("should generate job name with -script-job suffix", func() {
			dpuNode := &provisioningv1.DPUNode{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-dpunode",
					Namespace: "test-namespace",
				},
			}

			jobName := reconciler.generateJobName(dpuNode)
			Expect(jobName).To(Equal("test-dpunode-script-job"))
		})

		It("should handle different dpunode names", func() {
			testCases := []struct {
				dpuNodeName     string
				expectedJobName string
			}{
				{"node-1", "node-1-script-job"},
				{"my-dpu-node", "my-dpu-node-script-job"},
				{"dpunode-with-long-name", "dpunode-with-long-name-script-job"},
			}

			for _, tc := range testCases {
				dpuNode := &provisioningv1.DPUNode{
					ObjectMeta: metav1.ObjectMeta{
						Name: tc.dpuNodeName,
					},
				}
				jobName := reconciler.generateJobName(dpuNode)
				Expect(jobName).To(Equal(tc.expectedJobName))
			}
		})
	})

	Context("ensureEnv", func() {
		var (
			reconciler *DPUNodeReconciler
		)

		BeforeEach(func() {
			reconciler = &DPUNodeReconciler{}
		})

		It("should add env var when list is empty", func() {
			envs := []corev1.EnvVar{}
			result := reconciler.ensureEnv(envs, "TEST_VAR", "test-value")

			Expect(result).To(HaveLen(1))
			Expect(result[0].Name).To(Equal("TEST_VAR"))
			Expect(result[0].Value).To(Equal("test-value"))
		})

		It("should add env var when not present in list", func() {
			envs := []corev1.EnvVar{
				{Name: "EXISTING_VAR", Value: "existing-value"},
			}
			result := reconciler.ensureEnv(envs, "NEW_VAR", "new-value")

			Expect(result).To(HaveLen(2))
			Expect(result[0].Name).To(Equal("EXISTING_VAR"))
			Expect(result[1].Name).To(Equal("NEW_VAR"))
			Expect(result[1].Value).To(Equal("new-value"))
		})

		It("should not duplicate env var if already exists", func() {
			envs := []corev1.EnvVar{
				{Name: "TEST_VAR", Value: "original-value"},
			}
			result := reconciler.ensureEnv(envs, "TEST_VAR", "new-value")

			Expect(result).To(HaveLen(1))
			Expect(result[0].Name).To(Equal("TEST_VAR"))
			Expect(result[0].Value).To(Equal("original-value"))
		})

		It("should preserve original slice when env var exists", func() {
			original := []corev1.EnvVar{
				{Name: "VAR1", Value: "value1"},
				{Name: "VAR2", Value: "value2"},
			}
			result := reconciler.ensureEnv(original, "VAR1", "different-value")

			Expect(result).To(Equal(original))
		})

		It("should add DPUNODE_NAME env var correctly", func() {
			envs := []corev1.EnvVar{}
			result := reconciler.ensureEnv(envs, DPUNodeNameEnvVar, "my-dpunode")

			Expect(result).To(HaveLen(1))
			Expect(result[0].Name).To(Equal("DPUNODE_NAME"))
			Expect(result[0].Value).To(Equal("my-dpunode"))
		})
	})

	Context("ensureMount", func() {
		var (
			reconciler *DPUNodeReconciler
		)

		BeforeEach(func() {
			reconciler = &DPUNodeReconciler{}
		})

		It("should add volume mount when list is empty", func() {
			mounts := []corev1.VolumeMount{}
			result := reconciler.ensureMount(mounts, "test-volume", "/mnt/test")

			Expect(result).To(HaveLen(1))
			Expect(result[0].Name).To(Equal("test-volume"))
			Expect(result[0].MountPath).To(Equal("/mnt/test"))
		})

		It("should add volume mount when not present in list", func() {
			mounts := []corev1.VolumeMount{
				{Name: "existing-volume", MountPath: "/mnt/existing"},
			}
			result := reconciler.ensureMount(mounts, "new-volume", "/mnt/new")

			Expect(result).To(HaveLen(2))
			Expect(result[0].Name).To(Equal("existing-volume"))
			Expect(result[1].Name).To(Equal("new-volume"))
			Expect(result[1].MountPath).To(Equal("/mnt/new"))
		})

		It("should not duplicate volume mount if already exists", func() {
			mounts := []corev1.VolumeMount{
				{Name: "test-volume", MountPath: "/original/path"},
			}
			result := reconciler.ensureMount(mounts, "test-volume", "/new/path")

			Expect(result).To(HaveLen(1))
			Expect(result[0].Name).To(Equal("test-volume"))
			Expect(result[0].MountPath).To(Equal("/original/path"))
		})

		It("should preserve original slice when mount exists", func() {
			original := []corev1.VolumeMount{
				{Name: "vol1", MountPath: "/path1"},
				{Name: "vol2", MountPath: "/path2"},
			}
			result := reconciler.ensureMount(original, "vol1", "/different/path")

			Expect(result).To(Equal(original))
		})

		It("should add PodInfo volume mount correctly", func() {
			mounts := []corev1.VolumeMount{}
			result := reconciler.ensureMount(mounts, PodInfoVolumeName, PodInfoMountPath)

			Expect(result).To(HaveLen(1))
			Expect(result[0].Name).To(Equal("dpf-pod-info"))
			Expect(result[0].MountPath).To(Equal("/etc/dpf-pod-info"))
		})
	})

	Context("updateDPUCondition", func() {
		var (
			reconciler *DPUNodeReconciler
			fakeClient client.Client
			ctx        context.Context
		)

		BeforeEach(func() {
			ctx = context.Background()
			scheme := runtime.NewScheme()
			_ = provisioningv1.AddToScheme(scheme)

			fakeClient = fake.NewClientBuilder().
				WithScheme(scheme).
				WithStatusSubresource(&provisioningv1.DPU{}).
				Build()

			reconciler = &DPUNodeReconciler{
				Client: fakeClient,
			}
		})

		It("should update condition for single DPU", func() {
			dpu := &provisioningv1.DPU{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-dpu",
					Namespace: "test-namespace",
				},
			}
			Expect(fakeClient.Create(ctx, dpu)).To(Succeed())

			condition := &metav1.Condition{
				Type:    string(provisioningv1.DPUCondRebooted),
				Status:  metav1.ConditionTrue,
				Reason:  "RebootComplete",
				Message: "Node has been rebooted",
			}

			dpus := []*provisioningv1.DPU{dpu}
			err := reconciler.updateDPUCondition(ctx, dpus, condition)
			Expect(err).NotTo(HaveOccurred())

			// Verify the condition was set
			updatedDPU := &provisioningv1.DPU{}
			Expect(fakeClient.Get(ctx, types.NamespacedName{
				Name:      "test-dpu",
				Namespace: "test-namespace",
			}, updatedDPU)).To(Succeed())

			Expect(updatedDPU.Status.Conditions).NotTo(BeEmpty())
		})

		It("should update conditions for multiple DPUs", func() {
			dpu1 := &provisioningv1.DPU{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-dpu-1",
					Namespace: "test-namespace",
				},
			}
			dpu2 := &provisioningv1.DPU{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-dpu-2",
					Namespace: "test-namespace",
				},
			}
			Expect(fakeClient.Create(ctx, dpu1)).To(Succeed())
			Expect(fakeClient.Create(ctx, dpu2)).To(Succeed())

			condition := &metav1.Condition{
				Type:    string(provisioningv1.DPUCondRebooted),
				Status:  metav1.ConditionFalse,
				Reason:  "WaitingForReboot",
				Message: "Waiting for node reboot",
			}

			dpus := []*provisioningv1.DPU{dpu1, dpu2}
			err := reconciler.updateDPUCondition(ctx, dpus, condition)
			Expect(err).NotTo(HaveOccurred())

			// Verify both DPUs have the condition
			for _, name := range []string{"test-dpu-1", "test-dpu-2"} {
				updatedDPU := &provisioningv1.DPU{}
				Expect(fakeClient.Get(ctx, types.NamespacedName{
					Name:      name,
					Namespace: "test-namespace",
				}, updatedDPU)).To(Succeed())
				Expect(updatedDPU.Status.Conditions).NotTo(BeEmpty())
			}
		})

		It("should return nil for empty DPU list", func() {
			dpus := []*provisioningv1.DPU{}
			condition := &metav1.Condition{
				Type:   string(provisioningv1.DPUCondRebooted),
				Status: metav1.ConditionTrue,
				Reason: "RebootComplete",
			}

			err := reconciler.updateDPUCondition(ctx, dpus, condition)
			Expect(err).NotTo(HaveOccurred())
		})

		It("should return error when DPU does not exist", func() {
			nonExistentDPU := &provisioningv1.DPU{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "non-existent-dpu",
					Namespace: "test-namespace",
				},
			}

			condition := &metav1.Condition{
				Type:   string(provisioningv1.DPUCondRebooted),
				Status: metav1.ConditionTrue,
				Reason: "RebootComplete",
			}

			dpus := []*provisioningv1.DPU{nonExistentDPU}
			err := reconciler.updateDPUCondition(ctx, dpus, condition)
			Expect(err).To(HaveOccurred())
		})
	})

	Context("createScriptJob", func() {
		var (
			reconciler *DPUNodeReconciler
			fakeClient client.Client
			ctx        context.Context
		)

		BeforeEach(func() {
			ctx = context.Background()
			scheme := runtime.NewScheme()
			_ = provisioningv1.AddToScheme(scheme)
			_ = corev1.AddToScheme(scheme)
			_ = batchv1.AddToScheme(scheme)

			fakeClient = fake.NewClientBuilder().
				WithScheme(scheme).
				Build()

			reconciler = &DPUNodeReconciler{
				Client: fakeClient,
			}
		})

		It("should fail when ConfigMap does not exist", func() {
			dpuNode := &provisioningv1.DPUNode{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-dpunode",
					Namespace: "test-namespace",
				},
				Spec: provisioningv1.DPUNodeSpec{
					NodeRebootMethod: &provisioningv1.NodeRebootMethod{
						Script: &provisioningv1.Script{
							Name: "non-existent-configmap",
						},
					},
				},
			}

			err := reconciler.createScriptJob(ctx, dpuNode)
			Expect(err).To(HaveOccurred())
		})

		It("should fail when ConfigMap is missing pod-template key", func() {
			configMap := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-configmap",
					Namespace: "test-namespace",
				},
				Data: map[string]string{
					"wrong-key": "some-value",
				},
			}
			Expect(fakeClient.Create(ctx, configMap)).To(Succeed())

			dpuNode := &provisioningv1.DPUNode{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-dpunode",
					Namespace: "test-namespace",
				},
				Spec: provisioningv1.DPUNodeSpec{
					NodeRebootMethod: &provisioningv1.NodeRebootMethod{
						Script: &provisioningv1.Script{
							Name: "test-configmap",
						},
					},
				},
			}
			Expect(fakeClient.Create(ctx, dpuNode)).To(Succeed())

			err := reconciler.createScriptJob(ctx, dpuNode)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("pod-template not found in ConfigMap"))
		})

		It("should fail when pod-template is invalid JSON", func() {
			configMap := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-configmap",
					Namespace: "test-namespace",
				},
				Data: map[string]string{
					PodTemplateConfigMapKey: "invalid-json{",
				},
			}
			Expect(fakeClient.Create(ctx, configMap)).To(Succeed())

			dpuNode := &provisioningv1.DPUNode{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-dpunode",
					Namespace: "test-namespace",
				},
				Spec: provisioningv1.DPUNodeSpec{
					NodeRebootMethod: &provisioningv1.NodeRebootMethod{
						Script: &provisioningv1.Script{
							Name: "test-configmap",
						},
					},
				},
			}

			err := reconciler.createScriptJob(ctx, dpuNode)
			Expect(err).To(HaveOccurred())
		})

		It("should create Job successfully with valid ConfigMap", func() {
			podTemplate := corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "reboot-script",
							Image: "busybox:latest",
							Command: []string{
								"/bin/sh",
								"-c",
								"echo 'rebooting'",
							},
						},
					},
					RestartPolicy: corev1.RestartPolicyNever,
				},
			}
			podTemplateJSON, err := json.Marshal(podTemplate)
			Expect(err).NotTo(HaveOccurred())

			configMap := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-configmap",
					Namespace: "test-namespace",
				},
				Data: map[string]string{
					PodTemplateConfigMapKey: string(podTemplateJSON),
				},
			}
			Expect(fakeClient.Create(ctx, configMap)).To(Succeed())

			dpuNode := &provisioningv1.DPUNode{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-dpunode",
					Namespace: "test-namespace",
				},
				Spec: provisioningv1.DPUNodeSpec{
					NodeRebootMethod: &provisioningv1.NodeRebootMethod{
						Script: &provisioningv1.Script{
							Name: "test-configmap",
						},
					},
				},
			}
			Expect(fakeClient.Create(ctx, dpuNode)).To(Succeed())

			err = reconciler.createScriptJob(ctx, dpuNode)
			Expect(err).NotTo(HaveOccurred())

			// Verify Job was created
			job := &batchv1.Job{}
			err = fakeClient.Get(ctx, types.NamespacedName{
				Name:      "test-dpunode-script-job",
				Namespace: "test-namespace",
			}, job)
			Expect(err).NotTo(HaveOccurred())
			Expect(job.Name).To(Equal("test-dpunode-script-job"))
		})

		It("should add DPUNODE_NAME env var to containers", func() {
			podTemplate := corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "main-container",
							Image: "busybox:latest",
						},
					},
					RestartPolicy: corev1.RestartPolicyNever,
				},
			}
			podTemplateJSON, err := json.Marshal(podTemplate)
			Expect(err).NotTo(HaveOccurred())

			configMap := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-configmap",
					Namespace: "test-namespace",
				},
				Data: map[string]string{
					PodTemplateConfigMapKey: string(podTemplateJSON),
				},
			}
			Expect(fakeClient.Create(ctx, configMap)).To(Succeed())

			dpuNode := &provisioningv1.DPUNode{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "my-dpunode",
					Namespace: "test-namespace",
				},
				Spec: provisioningv1.DPUNodeSpec{
					NodeRebootMethod: &provisioningv1.NodeRebootMethod{
						Script: &provisioningv1.Script{
							Name: "test-configmap",
						},
					},
				},
			}
			Expect(fakeClient.Create(ctx, dpuNode)).To(Succeed())

			err = reconciler.createScriptJob(ctx, dpuNode)
			Expect(err).NotTo(HaveOccurred())

			// Verify Job has DPUNODE_NAME env var
			job := &batchv1.Job{}
			err = fakeClient.Get(ctx, types.NamespacedName{
				Name:      "my-dpunode-script-job",
				Namespace: "test-namespace",
			}, job)
			Expect(err).NotTo(HaveOccurred())

			// Check containers have DPUNODE_NAME env
			found := false
			for _, container := range job.Spec.Template.Spec.Containers {
				for _, env := range container.Env {
					if env.Name == DPUNodeNameEnvVar && env.Value == "my-dpunode" {
						found = true
						break
					}
				}
			}
			Expect(found).To(BeTrue(), "Expected DPUNODE_NAME env var to be set in containers")
		})

		It("should copy dpuNode labels to pod template", func() {
			podTemplate := corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "main-container",
							Image: "busybox:latest",
						},
					},
					RestartPolicy: corev1.RestartPolicyNever,
				},
			}
			podTemplateJSON, err := json.Marshal(podTemplate)
			Expect(err).NotTo(HaveOccurred())

			configMap := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-configmap",
					Namespace: "test-namespace",
				},
				Data: map[string]string{
					PodTemplateConfigMapKey: string(podTemplateJSON),
				},
			}
			Expect(fakeClient.Create(ctx, configMap)).To(Succeed())

			dpuNode := &provisioningv1.DPUNode{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-dpunode",
					Namespace: "test-namespace",
					Labels: map[string]string{
						"custom-label": "custom-value",
						"env":          "test",
					},
				},
				Spec: provisioningv1.DPUNodeSpec{
					NodeRebootMethod: &provisioningv1.NodeRebootMethod{
						Script: &provisioningv1.Script{
							Name: "test-configmap",
						},
					},
				},
			}
			Expect(fakeClient.Create(ctx, dpuNode)).To(Succeed())

			err = reconciler.createScriptJob(ctx, dpuNode)
			Expect(err).NotTo(HaveOccurred())

			// Verify Job has dpuNode labels
			job := &batchv1.Job{}
			err = fakeClient.Get(ctx, types.NamespacedName{
				Name:      "test-dpunode-script-job",
				Namespace: "test-namespace",
			}, job)
			Expect(err).NotTo(HaveOccurred())

			Expect(job.Spec.Template.Labels).To(HaveKeyWithValue("custom-label", "custom-value"))
			Expect(job.Spec.Template.Labels).To(HaveKeyWithValue("env", "test"))
		})

		It("should delete existing job before creating new one", func() {
			// Create existing job
			existingJob := &batchv1.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-dpunode-script-job",
					Namespace: "test-namespace",
				},
				Spec: batchv1.JobSpec{
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{Name: "old", Image: "old:image"},
							},
							RestartPolicy: corev1.RestartPolicyNever,
						},
					},
				},
			}
			Expect(fakeClient.Create(ctx, existingJob)).To(Succeed())

			// Create ConfigMap with new template
			podTemplate := corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "new-container",
							Image: "new:image",
						},
					},
					RestartPolicy: corev1.RestartPolicyNever,
				},
			}
			podTemplateJSON, err := json.Marshal(podTemplate)
			Expect(err).NotTo(HaveOccurred())

			configMap := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-configmap",
					Namespace: "test-namespace",
				},
				Data: map[string]string{
					PodTemplateConfigMapKey: string(podTemplateJSON),
				},
			}
			Expect(fakeClient.Create(ctx, configMap)).To(Succeed())

			dpuNode := &provisioningv1.DPUNode{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-dpunode",
					Namespace: "test-namespace",
				},
				Spec: provisioningv1.DPUNodeSpec{
					NodeRebootMethod: &provisioningv1.NodeRebootMethod{
						Script: &provisioningv1.Script{
							Name: "test-configmap",
						},
					},
				},
			}
			Expect(fakeClient.Create(ctx, dpuNode)).To(Succeed())

			err = reconciler.createScriptJob(ctx, dpuNode)
			Expect(err).NotTo(HaveOccurred())

			// Verify new Job was created with new container
			job := &batchv1.Job{}
			err = fakeClient.Get(ctx, types.NamespacedName{
				Name:      "test-dpunode-script-job",
				Namespace: "test-namespace",
			}, job)
			Expect(err).NotTo(HaveOccurred())
			Expect(job.Spec.Template.Spec.Containers[0].Name).To(Equal("new-container"))
		})

		It("should store ConfigMap ResourceVersion in DPUNode annotation", func() {
			podTemplate := corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "reboot-script",
							Image: "busybox:latest",
						},
					},
					RestartPolicy: corev1.RestartPolicyNever,
				},
			}
			podTemplateJSON, err := json.Marshal(podTemplate)
			Expect(err).NotTo(HaveOccurred())

			configMap := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-configmap",
					Namespace: "test-namespace",
				},
				Data: map[string]string{
					PodTemplateConfigMapKey: string(podTemplateJSON),
				},
			}
			Expect(fakeClient.Create(ctx, configMap)).To(Succeed())

			// Get the ConfigMap to retrieve the ResourceVersion set by fake client
			Expect(fakeClient.Get(ctx, types.NamespacedName{
				Name:      "test-configmap",
				Namespace: "test-namespace",
			}, configMap)).To(Succeed())

			dpuNode := &provisioningv1.DPUNode{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-dpunode",
					Namespace: "test-namespace",
				},
				Spec: provisioningv1.DPUNodeSpec{
					NodeRebootMethod: &provisioningv1.NodeRebootMethod{
						Script: &provisioningv1.Script{
							Name: "test-configmap",
						},
					},
				},
			}
			Expect(fakeClient.Create(ctx, dpuNode)).To(Succeed())

			// Create patcher to simulate defer behavior in Reconcile
			patcher := patch.NewSerialPatcher(dpuNode, fakeClient)

			err = reconciler.createScriptJob(ctx, dpuNode)
			Expect(err).NotTo(HaveOccurred())

			// Patch the dpuNode to persist changes (simulating defer in Reconcile)
			Expect(patcher.Patch(ctx, dpuNode)).To(Succeed())

			// Fetch the updated DPUNode from the fake client to verify annotation was persisted
			updatedDPUNode := &provisioningv1.DPUNode{}
			Expect(fakeClient.Get(ctx, types.NamespacedName{
				Name:      "test-dpunode",
				Namespace: "test-namespace",
			}, updatedDPUNode)).To(Succeed())

			// Verify the ConfigMap ResourceVersion is stored in DPUNode annotation
			Expect(updatedDPUNode.Annotations).NotTo(BeNil())
			Expect(updatedDPUNode.Annotations[provisioningv1.DPUNodeScriptConfigMapVersionAnnotation]).To(Equal(configMap.ResourceVersion))
		})
	})

	Context("shouldRetryScriptJob", func() {
		var (
			reconciler *DPUNodeReconciler
			fakeClient client.Client
			ctx        context.Context
		)

		BeforeEach(func() {
			ctx = context.Background()
			scheme := runtime.NewScheme()
			_ = provisioningv1.AddToScheme(scheme)
			_ = corev1.AddToScheme(scheme)

			fakeClient = fake.NewClientBuilder().
				WithScheme(scheme).
				Build()

			reconciler = &DPUNodeReconciler{
				Client: fakeClient,
			}
		})

		It("should return false when NodeRebootMethod is nil", func() {
			dpuNode := &provisioningv1.DPUNode{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-dpunode",
					Namespace: "test-namespace",
				},
				Spec: provisioningv1.DPUNodeSpec{
					NodeRebootMethod: nil,
				},
			}

			shouldRetry, err := reconciler.shouldRetryScriptJob(ctx, dpuNode)
			Expect(err).NotTo(HaveOccurred())
			Expect(shouldRetry).To(BeFalse())
		})

		It("should return false when Script is nil", func() {
			dpuNode := &provisioningv1.DPUNode{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-dpunode",
					Namespace: "test-namespace",
				},
				Spec: provisioningv1.DPUNodeSpec{
					NodeRebootMethod: &provisioningv1.NodeRebootMethod{
						External: &provisioningv1.External{},
					},
				},
			}

			shouldRetry, err := reconciler.shouldRetryScriptJob(ctx, dpuNode)
			Expect(err).NotTo(HaveOccurred())
			Expect(shouldRetry).To(BeFalse())
		})

		It("should return false when ConfigMap version annotation is not set", func() {
			dpuNode := &provisioningv1.DPUNode{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-dpunode",
					Namespace: "test-namespace",
				},
				Spec: provisioningv1.DPUNodeSpec{
					NodeRebootMethod: &provisioningv1.NodeRebootMethod{
						Script: &provisioningv1.Script{
							Name: "test-configmap",
						},
					},
				},
			}

			shouldRetry, err := reconciler.shouldRetryScriptJob(ctx, dpuNode)
			Expect(err).NotTo(HaveOccurred())
			Expect(shouldRetry).To(BeFalse())
		})

		It("should return error when ConfigMap does not exist", func() {
			dpuNode := &provisioningv1.DPUNode{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-dpunode",
					Namespace: "test-namespace",
					Annotations: map[string]string{
						provisioningv1.DPUNodeScriptConfigMapVersionAnnotation: "12345",
					},
				},
				Spec: provisioningv1.DPUNodeSpec{
					NodeRebootMethod: &provisioningv1.NodeRebootMethod{
						Script: &provisioningv1.Script{
							Name: "non-existent-configmap",
						},
					},
				},
			}

			shouldRetry, err := reconciler.shouldRetryScriptJob(ctx, dpuNode)
			Expect(err).To(HaveOccurred())
			Expect(shouldRetry).To(BeFalse())
		})

		It("should return false when ConfigMap ResourceVersion matches stored version", func() {
			configMap := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-configmap",
					Namespace: "test-namespace",
				},
				Data: map[string]string{
					"key": "value",
				},
			}
			Expect(fakeClient.Create(ctx, configMap)).To(Succeed())

			// Get the ConfigMap to retrieve the ResourceVersion
			Expect(fakeClient.Get(ctx, types.NamespacedName{
				Name:      "test-configmap",
				Namespace: "test-namespace",
			}, configMap)).To(Succeed())

			dpuNode := &provisioningv1.DPUNode{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-dpunode",
					Namespace: "test-namespace",
					Annotations: map[string]string{
						provisioningv1.DPUNodeScriptConfigMapVersionAnnotation: configMap.ResourceVersion,
					},
				},
				Spec: provisioningv1.DPUNodeSpec{
					NodeRebootMethod: &provisioningv1.NodeRebootMethod{
						Script: &provisioningv1.Script{
							Name: "test-configmap",
						},
					},
				},
			}

			shouldRetry, err := reconciler.shouldRetryScriptJob(ctx, dpuNode)
			Expect(err).NotTo(HaveOccurred())
			Expect(shouldRetry).To(BeFalse())
		})

		It("should return true when ConfigMap ResourceVersion differs from stored version", func() {
			configMap := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-configmap",
					Namespace: "test-namespace",
				},
				Data: map[string]string{
					"key": "value",
				},
			}
			Expect(fakeClient.Create(ctx, configMap)).To(Succeed())

			// Get the ConfigMap to retrieve the ResourceVersion
			Expect(fakeClient.Get(ctx, types.NamespacedName{
				Name:      "test-configmap",
				Namespace: "test-namespace",
			}, configMap)).To(Succeed())

			dpuNode := &provisioningv1.DPUNode{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-dpunode",
					Namespace: "test-namespace",
					Annotations: map[string]string{
						// Store an old/different version
						provisioningv1.DPUNodeScriptConfigMapVersionAnnotation: "old-version-12345",
					},
				},
				Spec: provisioningv1.DPUNodeSpec{
					NodeRebootMethod: &provisioningv1.NodeRebootMethod{
						Script: &provisioningv1.Script{
							Name: "test-configmap",
						},
					},
				},
			}

			shouldRetry, err := reconciler.shouldRetryScriptJob(ctx, dpuNode)
			Expect(err).NotTo(HaveOccurred())
			Expect(shouldRetry).To(BeTrue())
		})
	})

	Context("configMapToDPUNodeReq", func() {
		var (
			reconciler *DPUNodeReconciler
			fakeClient client.Client
			ctx        context.Context
		)

		BeforeEach(func() {
			ctx = context.Background()
			scheme := runtime.NewScheme()
			_ = provisioningv1.AddToScheme(scheme)
			_ = corev1.AddToScheme(scheme)

			fakeClient = fake.NewClientBuilder().
				WithScheme(scheme).
				Build()

			reconciler = &DPUNodeReconciler{
				Client: fakeClient,
			}
		})

		It("should return empty list when no DPUNodes exist", func() {
			configMap := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-configmap",
					Namespace: "test-namespace",
				},
			}

			requests := reconciler.configMapToDPUNodeReq(ctx, configMap)
			Expect(requests).To(BeEmpty())
		})

		It("should return empty list when no DPUNodes reference the ConfigMap", func() {
			dpuNode := &provisioningv1.DPUNode{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-dpunode",
					Namespace: "test-namespace",
				},
				Spec: provisioningv1.DPUNodeSpec{
					NodeRebootMethod: &provisioningv1.NodeRebootMethod{
						Script: &provisioningv1.Script{
							Name: "different-configmap",
						},
					},
				},
			}
			Expect(fakeClient.Create(ctx, dpuNode)).To(Succeed())

			configMap := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-configmap",
					Namespace: "test-namespace",
				},
			}

			requests := reconciler.configMapToDPUNodeReq(ctx, configMap)
			Expect(requests).To(BeEmpty())
		})

		It("should return request for DPUNode that references the ConfigMap", func() {
			dpuNode := &provisioningv1.DPUNode{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-dpunode",
					Namespace: "test-namespace",
				},
				Spec: provisioningv1.DPUNodeSpec{
					NodeRebootMethod: &provisioningv1.NodeRebootMethod{
						Script: &provisioningv1.Script{
							Name: "test-configmap",
						},
					},
				},
			}
			Expect(fakeClient.Create(ctx, dpuNode)).To(Succeed())

			configMap := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-configmap",
					Namespace: "test-namespace",
				},
			}

			requests := reconciler.configMapToDPUNodeReq(ctx, configMap)
			Expect(requests).To(HaveLen(1))
			Expect(requests[0].Name).To(Equal("test-dpunode"))
			Expect(requests[0].Namespace).To(Equal("test-namespace"))
		})

		It("should return requests for multiple DPUNodes that reference the same ConfigMap", func() {
			dpuNode1 := &provisioningv1.DPUNode{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "dpunode-1",
					Namespace: "test-namespace",
				},
				Spec: provisioningv1.DPUNodeSpec{
					NodeRebootMethod: &provisioningv1.NodeRebootMethod{
						Script: &provisioningv1.Script{
							Name: "shared-configmap",
						},
					},
				},
			}
			dpuNode2 := &provisioningv1.DPUNode{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "dpunode-2",
					Namespace: "test-namespace",
				},
				Spec: provisioningv1.DPUNodeSpec{
					NodeRebootMethod: &provisioningv1.NodeRebootMethod{
						Script: &provisioningv1.Script{
							Name: "shared-configmap",
						},
					},
				},
			}
			Expect(fakeClient.Create(ctx, dpuNode1)).To(Succeed())
			Expect(fakeClient.Create(ctx, dpuNode2)).To(Succeed())

			configMap := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "shared-configmap",
					Namespace: "test-namespace",
				},
			}

			requests := reconciler.configMapToDPUNodeReq(ctx, configMap)
			Expect(requests).To(HaveLen(2))

			names := []string{requests[0].Name, requests[1].Name}
			Expect(names).To(ContainElements("dpunode-1", "dpunode-2"))
		})

		It("should not return requests for DPUNodes in different namespace", func() {
			dpuNode := &provisioningv1.DPUNode{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-dpunode",
					Namespace: "other-namespace",
				},
				Spec: provisioningv1.DPUNodeSpec{
					NodeRebootMethod: &provisioningv1.NodeRebootMethod{
						Script: &provisioningv1.Script{
							Name: "test-configmap",
						},
					},
				},
			}
			Expect(fakeClient.Create(ctx, dpuNode)).To(Succeed())

			configMap := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-configmap",
					Namespace: "test-namespace",
				},
			}

			requests := reconciler.configMapToDPUNodeReq(ctx, configMap)
			Expect(requests).To(BeEmpty())
		})

		It("should not return requests for DPUNodes with nil NodeRebootMethod", func() {
			dpuNode := &provisioningv1.DPUNode{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-dpunode",
					Namespace: "test-namespace",
				},
				Spec: provisioningv1.DPUNodeSpec{
					NodeRebootMethod: nil,
				},
			}
			Expect(fakeClient.Create(ctx, dpuNode)).To(Succeed())

			configMap := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-configmap",
					Namespace: "test-namespace",
				},
			}

			requests := reconciler.configMapToDPUNodeReq(ctx, configMap)
			Expect(requests).To(BeEmpty())
		})

		It("should not return requests for DPUNodes with External reboot method", func() {
			dpuNode := &provisioningv1.DPUNode{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-dpunode",
					Namespace: "test-namespace",
				},
				Spec: provisioningv1.DPUNodeSpec{
					NodeRebootMethod: &provisioningv1.NodeRebootMethod{
						External: &provisioningv1.External{},
					},
				},
			}
			Expect(fakeClient.Create(ctx, dpuNode)).To(Succeed())

			configMap := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-configmap",
					Namespace: "test-namespace",
				},
			}

			requests := reconciler.configMapToDPUNodeReq(ctx, configMap)
			Expect(requests).To(BeEmpty())
		})
	})

	Context("clearDPURebootedConditions", func() {
		var (
			reconciler *DPUNodeReconciler
			fakeClient client.Client
			ctx        context.Context
		)

		BeforeEach(func() {
			ctx = context.Background()
			scheme := runtime.NewScheme()
			_ = provisioningv1.AddToScheme(scheme)

			fakeClient = fake.NewClientBuilder().
				WithScheme(scheme).
				WithStatusSubresource(&provisioningv1.DPU{}).
				Build()

			reconciler = &DPUNodeReconciler{
				Client: fakeClient,
			}
		})

		It("should return nil for empty DPU list", func() {
			dpus := []*provisioningv1.DPU{}
			err := reconciler.clearDPURebootedConditions(ctx, dpus)
			Expect(err).NotTo(HaveOccurred())
		})

		It("should remove Rebooted condition from single DPU", func() {
			dpu := &provisioningv1.DPU{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-dpu",
					Namespace: "test-namespace",
				},
				Status: provisioningv1.DPUStatus{
					Conditions: []metav1.Condition{
						{
							Type:   string(provisioningv1.DPUCondRebooted),
							Status: metav1.ConditionFalse,
							Reason: "RebootFailed",
						},
						{
							Type:   "OtherCondition",
							Status: metav1.ConditionTrue,
							Reason: "SomeReason",
						},
					},
				},
			}
			Expect(fakeClient.Create(ctx, dpu)).To(Succeed())
			Expect(fakeClient.Status().Update(ctx, dpu)).To(Succeed())

			dpus := []*provisioningv1.DPU{dpu}
			err := reconciler.clearDPURebootedConditions(ctx, dpus)
			Expect(err).NotTo(HaveOccurred())

			// Verify Rebooted condition was removed
			updatedDPU := &provisioningv1.DPU{}
			Expect(fakeClient.Get(ctx, types.NamespacedName{
				Name:      "test-dpu",
				Namespace: "test-namespace",
			}, updatedDPU)).To(Succeed())

			// Should only have OtherCondition, not Rebooted
			Expect(updatedDPU.Status.Conditions).To(HaveLen(1))
			Expect(updatedDPU.Status.Conditions[0].Type).To(Equal("OtherCondition"))
		})

		It("should remove Rebooted condition from multiple DPUs", func() {
			dpu1 := &provisioningv1.DPU{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-dpu-1",
					Namespace: "test-namespace",
				},
				Status: provisioningv1.DPUStatus{
					Conditions: []metav1.Condition{
						{
							Type:   string(provisioningv1.DPUCondRebooted),
							Status: metav1.ConditionFalse,
							Reason: "RebootFailed",
						},
					},
				},
			}
			dpu2 := &provisioningv1.DPU{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-dpu-2",
					Namespace: "test-namespace",
				},
				Status: provisioningv1.DPUStatus{
					Conditions: []metav1.Condition{
						{
							Type:   string(provisioningv1.DPUCondRebooted),
							Status: metav1.ConditionFalse,
							Reason: "RebootFailed",
						},
					},
				},
			}
			Expect(fakeClient.Create(ctx, dpu1)).To(Succeed())
			Expect(fakeClient.Status().Update(ctx, dpu1)).To(Succeed())
			Expect(fakeClient.Create(ctx, dpu2)).To(Succeed())
			Expect(fakeClient.Status().Update(ctx, dpu2)).To(Succeed())

			dpus := []*provisioningv1.DPU{dpu1, dpu2}
			err := reconciler.clearDPURebootedConditions(ctx, dpus)
			Expect(err).NotTo(HaveOccurred())

			// Verify both DPUs have Rebooted condition removed
			for _, name := range []string{"test-dpu-1", "test-dpu-2"} {
				updatedDPU := &provisioningv1.DPU{}
				Expect(fakeClient.Get(ctx, types.NamespacedName{
					Name:      name,
					Namespace: "test-namespace",
				}, updatedDPU)).To(Succeed())
				Expect(updatedDPU.Status.Conditions).To(BeEmpty())
			}
		})

		It("should preserve other conditions when removing Rebooted", func() {
			dpu := &provisioningv1.DPU{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-dpu",
					Namespace: "test-namespace",
				},
				Status: provisioningv1.DPUStatus{
					Conditions: []metav1.Condition{
						{
							Type:   string(provisioningv1.DPUCondRebooted),
							Status: metav1.ConditionFalse,
							Reason: "RebootFailed",
						},
						{
							Type:   "OSInstalled",
							Status: metav1.ConditionTrue,
							Reason: "Success",
						},
						{
							Type:   "InterfaceInitialized",
							Status: metav1.ConditionTrue,
							Reason: "Success",
						},
					},
				},
			}
			Expect(fakeClient.Create(ctx, dpu)).To(Succeed())
			Expect(fakeClient.Status().Update(ctx, dpu)).To(Succeed())

			dpus := []*provisioningv1.DPU{dpu}
			err := reconciler.clearDPURebootedConditions(ctx, dpus)
			Expect(err).NotTo(HaveOccurred())

			// Verify only Rebooted was removed
			updatedDPU := &provisioningv1.DPU{}
			Expect(fakeClient.Get(ctx, types.NamespacedName{
				Name:      "test-dpu",
				Namespace: "test-namespace",
			}, updatedDPU)).To(Succeed())

			Expect(updatedDPU.Status.Conditions).To(HaveLen(2))
			conditionTypes := []string{}
			for _, cond := range updatedDPU.Status.Conditions {
				conditionTypes = append(conditionTypes, cond.Type)
			}
			Expect(conditionTypes).To(ContainElements("OSInstalled", "InterfaceInitialized"))
			Expect(conditionTypes).NotTo(ContainElement(string(provisioningv1.DPUCondRebooted)))
		})

		It("should handle DPU without Rebooted condition", func() {
			dpu := &provisioningv1.DPU{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-dpu",
					Namespace: "test-namespace",
				},
				Status: provisioningv1.DPUStatus{
					Conditions: []metav1.Condition{
						{
							Type:   "OtherCondition",
							Status: metav1.ConditionTrue,
							Reason: "SomeReason",
						},
					},
				},
			}
			Expect(fakeClient.Create(ctx, dpu)).To(Succeed())
			Expect(fakeClient.Status().Update(ctx, dpu)).To(Succeed())

			dpus := []*provisioningv1.DPU{dpu}
			err := reconciler.clearDPURebootedConditions(ctx, dpus)
			Expect(err).NotTo(HaveOccurred())

			// Verify OtherCondition is still there
			updatedDPU := &provisioningv1.DPU{}
			Expect(fakeClient.Get(ctx, types.NamespacedName{
				Name:      "test-dpu",
				Namespace: "test-namespace",
			}, updatedDPU)).To(Succeed())

			Expect(updatedDPU.Status.Conditions).To(HaveLen(1))
			Expect(updatedDPU.Status.Conditions[0].Type).To(Equal("OtherCondition"))
		})

		It("should return error when DPU does not exist", func() {
			nonExistentDPU := &provisioningv1.DPU{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "non-existent-dpu",
					Namespace: "test-namespace",
				},
			}

			dpus := []*provisioningv1.DPU{nonExistentDPU}
			err := reconciler.clearDPURebootedConditions(ctx, dpus)
			Expect(err).To(HaveOccurred())
		})
	})

	Context("scriptRebootConfigMapPredicate", func() {
		// Helper function that mirrors the predicate logic for testing
		filterFunc := func(obj client.Object) bool {
			cm, ok := obj.(*corev1.ConfigMap)
			if !ok {
				return false
			}
			_, hasPodTemplate := cm.Data[PodTemplateConfigMapKey]
			return hasPodTemplate
		}

		It("should return true for ConfigMap with pod-template key", func() {
			configMap := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-configmap",
					Namespace: "test-namespace",
				},
				Data: map[string]string{
					PodTemplateConfigMapKey: `{"spec": {}}`,
				},
			}

			Expect(filterFunc(configMap)).To(BeTrue())
		})

		It("should return false for ConfigMap without pod-template key", func() {
			configMap := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-configmap",
					Namespace: "test-namespace",
				},
				Data: map[string]string{
					"other-key": "some-value",
				},
			}

			Expect(filterFunc(configMap)).To(BeFalse())
		})

		It("should return true for ConfigMap with pod-template key among other keys", func() {
			configMap := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-configmap",
					Namespace: "test-namespace",
				},
				Data: map[string]string{
					"other-key":             "some-value",
					PodTemplateConfigMapKey: `{"spec": {}}`,
					"another-key":           "another-value",
				},
			}

			Expect(filterFunc(configMap)).To(BeTrue())
		})

		It("should return false for ConfigMap with empty Data", func() {
			configMap := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-configmap",
					Namespace: "test-namespace",
				},
				Data: map[string]string{},
			}

			Expect(filterFunc(configMap)).To(BeFalse())
		})

		It("should return false for ConfigMap with nil Data", func() {
			configMap := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-configmap",
					Namespace: "test-namespace",
				},
				Data: nil,
			}

			Expect(filterFunc(configMap)).To(BeFalse())
		})

		It("should return false for non-ConfigMap objects", func() {
			// Test with a different object type (Pod)
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "test-namespace",
				},
			}

			Expect(filterFunc(pod)).To(BeFalse())
		})
	})

	Context("rebootNode", func() {
		var (
			reconciler *DPUNodeReconciler
			fakeClient client.Client
			ctx        context.Context
			scheme     *runtime.Scheme
		)

		BeforeEach(func() {
			ctx = context.Background()
			scheme = runtime.NewScheme()
			_ = provisioningv1.AddToScheme(scheme)
			_ = corev1.AddToScheme(scheme)
			_ = batchv1.AddToScheme(scheme)
		})

		createTestSetup := func(jobStatus *batchv1.JobStatus, configMapVersion string, storedVersion string) (*provisioningv1.DPUNode, *provisioningv1.DPU, *batchv1.Job) {
			podTemplate := corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "reboot-script",
							Image: "busybox:latest",
						},
					},
					RestartPolicy: corev1.RestartPolicyNever,
				},
			}
			podTemplateJSON, _ := json.Marshal(podTemplate)

			configMap := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:            "test-configmap",
					Namespace:       "test-namespace",
					ResourceVersion: configMapVersion,
				},
				Data: map[string]string{
					PodTemplateConfigMapKey: string(podTemplateJSON),
				},
			}

			annotations := map[string]string{}
			if storedVersion != "" {
				annotations[provisioningv1.DPUNodeScriptConfigMapVersionAnnotation] = storedVersion
			}

			dpuNode := &provisioningv1.DPUNode{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "test-dpunode",
					Namespace:   "test-namespace",
					Annotations: annotations,
				},
				Spec: provisioningv1.DPUNodeSpec{
					NodeRebootMethod: &provisioningv1.NodeRebootMethod{
						Script: &provisioningv1.Script{
							Name: "test-configmap",
						},
					},
					DPUs: []provisioningv1.DPURef{
						{Name: "dpu1"},
					},
				},
			}

			// DPU name follows the pattern: {dpuNodeName}-{dpuDeviceName}
			dpu := &provisioningv1.DPU{
				ObjectMeta: metav1.ObjectMeta{
					Name:      cutil.GenerateDPUName(dpuNode.Name, "dpu1"),
					Namespace: "test-namespace",
				},
				Status: provisioningv1.DPUStatus{
					Phase: provisioningv1.DPURebooting,
				},
			}

			var job *batchv1.Job
			if jobStatus != nil {
				job = &batchv1.Job{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-dpunode-script-job",
						Namespace: "test-namespace",
					},
					Spec: batchv1.JobSpec{
						Template: podTemplate,
					},
					Status: *jobStatus,
				}
			}

			objects := []client.Object{configMap, dpuNode, dpu}
			if job != nil {
				objects = append(objects, job)
			}

			fakeClient = fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(objects...).
				WithStatusSubresource(&provisioningv1.DPU{}).
				Build()

			reconciler = &DPUNodeReconciler{
				Client:   fakeClient,
				Recorder: record.NewFakeRecorder(10),
			}

			return dpuNode, dpu, job
		}

		It("should handle job succeeded and clean up annotation", func() {
			jobStatus := &batchv1.JobStatus{Succeeded: 1}
			dpuNode, _, _ := createTestSetup(jobStatus, "v2", "v1")

			// Set condition to indicate reboot was already triggered
			dpu := &provisioningv1.DPU{}
			Expect(fakeClient.Get(ctx, types.NamespacedName{
				Name:      cutil.GenerateDPUName(dpuNode.Name, "dpu1"),
				Namespace: "test-namespace",
			}, dpu)).To(Succeed())
			cutil.SetDPUCondition(&dpu.Status, cutil.DPUCondition(provisioningv1.DPUCondRebooted, "WaitingForScriptToRebootNode", ""))
			Expect(fakeClient.Status().Update(ctx, dpu)).To(Succeed())

			result, err := reconciler.rebootNode(ctx, dpuNode)
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).To(BeZero())

			// Verify annotation is removed
			Expect(dpuNode.Annotations[provisioningv1.DPUNodeScriptConfigMapVersionAnnotation]).To(BeEmpty())
		})

		It("should handle job failed without ConfigMap change (no retry)", func() {
			jobStatus := &batchv1.JobStatus{Failed: 1}
			// Same version - no retry expected
			dpuNode, _, _ := createTestSetup(jobStatus, "v1", "v1")

			// Set condition to indicate reboot was already triggered
			dpu := &provisioningv1.DPU{}
			Expect(fakeClient.Get(ctx, types.NamespacedName{
				Name:      cutil.GenerateDPUName(dpuNode.Name, "dpu1"),
				Namespace: "test-namespace",
			}, dpu)).To(Succeed())
			cutil.SetDPUCondition(&dpu.Status, cutil.DPUCondition(provisioningv1.DPUCondRebooted, "WaitingForScriptToRebootNode", ""))
			Expect(fakeClient.Status().Update(ctx, dpu)).To(Succeed())

			result, err := reconciler.rebootNode(ctx, dpuNode)
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).To(BeZero())

			// Verify DPU condition is set to failed
			Expect(fakeClient.Get(ctx, types.NamespacedName{
				Name:      cutil.GenerateDPUName(dpuNode.Name, "dpu1"),
				Namespace: "test-namespace",
			}, dpu)).To(Succeed())
			_, cond := cutil.GetDPUCondition(&dpu.Status, string(provisioningv1.DPUCondRebooted))
			Expect(cond).NotTo(BeNil())
			Expect(cond.Reason).To(Equal("RebootFailed"))
		})

		It("should handle job failed with ConfigMap change (auto-retry)", func() {
			jobStatus := &batchv1.JobStatus{Failed: 1}
			// Different versions - retry expected
			dpuNode, _, _ := createTestSetup(jobStatus, "v2", "v1")

			// Set condition to indicate reboot was already triggered
			dpu := &provisioningv1.DPU{}
			Expect(fakeClient.Get(ctx, types.NamespacedName{
				Name:      cutil.GenerateDPUName(dpuNode.Name, "dpu1"),
				Namespace: "test-namespace",
			}, dpu)).To(Succeed())
			cutil.SetDPUCondition(&dpu.Status, cutil.DPUCondition(provisioningv1.DPUCondRebooted, "WaitingForScriptToRebootNode", ""))
			Expect(fakeClient.Status().Update(ctx, dpu)).To(Succeed())

			result, err := reconciler.rebootNode(ctx, dpuNode)
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).NotTo(BeZero())

			// Verify failed job was deleted
			job := &batchv1.Job{}
			err = fakeClient.Get(ctx, types.NamespacedName{
				Name:      "test-dpunode-script-job",
				Namespace: "test-namespace",
			}, job)
			Expect(err).To(HaveOccurred())
		})

		It("should handle job not found with ConfigMap change (auto-retry)", func() {
			// No job (nil jobStatus), different versions - retry expected
			dpuNode, _, _ := createTestSetup(nil, "v2", "v1")

			// Set condition to indicate reboot was already triggered
			dpu := &provisioningv1.DPU{}
			Expect(fakeClient.Get(ctx, types.NamespacedName{
				Name:      cutil.GenerateDPUName(dpuNode.Name, "dpu1"),
				Namespace: "test-namespace",
			}, dpu)).To(Succeed())
			cutil.SetDPUCondition(&dpu.Status, cutil.DPUCondition(provisioningv1.DPUCondRebooted, "WaitingForScriptToRebootNode", ""))
			Expect(fakeClient.Status().Update(ctx, dpu)).To(Succeed())

			result, err := reconciler.rebootNode(ctx, dpuNode)
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).NotTo(BeZero())
		})

		It("should handle job not found without ConfigMap change (no retry)", func() {
			// No job, same versions - no retry, set JobNotFound condition
			dpuNode, _, _ := createTestSetup(nil, "v1", "v1")

			// Set condition to indicate reboot was already triggered
			dpu := &provisioningv1.DPU{}
			Expect(fakeClient.Get(ctx, types.NamespacedName{
				Name:      cutil.GenerateDPUName(dpuNode.Name, "dpu1"),
				Namespace: "test-namespace",
			}, dpu)).To(Succeed())
			cutil.SetDPUCondition(&dpu.Status, cutil.DPUCondition(provisioningv1.DPUCondRebooted, "WaitingForScriptToRebootNode", ""))
			Expect(fakeClient.Status().Update(ctx, dpu)).To(Succeed())

			_, err := reconciler.rebootNode(ctx, dpuNode)
			// Error is expected due to job not found
			Expect(err).To(HaveOccurred())
		})

		It("should create script job when no condition exists", func() {
			// Job exists but no DPUCondRebooted condition - should create new job
			dpuNode, _, _ := createTestSetup(nil, "v1", "")

			result, err := reconciler.rebootNode(ctx, dpuNode)
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).NotTo(BeZero())

			// Verify job was created
			job := &batchv1.Job{}
			err = fakeClient.Get(ctx, types.NamespacedName{
				Name:      "test-dpunode-script-job",
				Namespace: "test-namespace",
			}, job)
			Expect(err).NotTo(HaveOccurred())
		})

		It("should requeue when job is still running", func() {
			jobStatus := &batchv1.JobStatus{Active: 1}
			dpuNode, _, _ := createTestSetup(jobStatus, "v1", "v1")

			// Set condition to indicate reboot was already triggered
			dpu := &provisioningv1.DPU{}
			Expect(fakeClient.Get(ctx, types.NamespacedName{
				Name:      cutil.GenerateDPUName(dpuNode.Name, "dpu1"),
				Namespace: "test-namespace",
			}, dpu)).To(Succeed())
			cutil.SetDPUCondition(&dpu.Status, cutil.DPUCondition(provisioningv1.DPUCondRebooted, "WaitingForScriptToRebootNode", ""))
			Expect(fakeClient.Status().Update(ctx, dpu)).To(Succeed())

			result, err := reconciler.rebootNode(ctx, dpuNode)
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).NotTo(BeZero())
		})

		It("should handle external reboot method", func() {
			// Create setup with external reboot method
			dpuNode := &provisioningv1.DPUNode{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-dpunode",
					Namespace: "test-namespace",
					Annotations: map[string]string{
						provisioningv1.DPUNodeExternalRebootRequiredAnnotation: "true",
					},
				},
				Spec: provisioningv1.DPUNodeSpec{
					NodeRebootMethod: &provisioningv1.NodeRebootMethod{
						External: &provisioningv1.External{},
					},
					DPUs: []provisioningv1.DPURef{
						{Name: "dpu1"},
					},
				},
			}

			dpu := &provisioningv1.DPU{
				ObjectMeta: metav1.ObjectMeta{
					Name:      cutil.GenerateDPUName(dpuNode.Name, "dpu1"),
					Namespace: "test-namespace",
				},
				Status: provisioningv1.DPUStatus{
					Phase: provisioningv1.DPURebooting,
				},
			}

			fakeClient = fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(dpuNode, dpu).
				WithStatusSubresource(&provisioningv1.DPU{}).
				Build()

			reconciler = &DPUNodeReconciler{
				Client:   fakeClient,
				Recorder: record.NewFakeRecorder(10),
			}

			result, err := reconciler.rebootNode(ctx, dpuNode)
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).To(BeZero())
		})

		It("should handle no DPUs in rebooting phase", func() {
			podTemplate := corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "reboot-script",
							Image: "busybox:latest",
						},
					},
					RestartPolicy: corev1.RestartPolicyNever,
				},
			}
			podTemplateJSON, _ := json.Marshal(podTemplate)

			configMap := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-configmap",
					Namespace: "test-namespace",
				},
				Data: map[string]string{
					PodTemplateConfigMapKey: string(podTemplateJSON),
				},
			}

			dpuNode := &provisioningv1.DPUNode{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-dpunode",
					Namespace: "test-namespace",
				},
				Spec: provisioningv1.DPUNodeSpec{
					NodeRebootMethod: &provisioningv1.NodeRebootMethod{
						Script: &provisioningv1.Script{
							Name: "test-configmap",
						},
					},
					DPUs: []provisioningv1.DPURef{
						{Name: "dpu1"},
					},
				},
			}

			// DPU is NOT in rebooting phase
			dpu := &provisioningv1.DPU{
				ObjectMeta: metav1.ObjectMeta{
					Name:      cutil.GenerateDPUName(dpuNode.Name, "dpu1"),
					Namespace: "test-namespace",
				},
				Status: provisioningv1.DPUStatus{
					Phase: provisioningv1.DPUReady, // Not rebooting
				},
			}

			fakeClient = fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(configMap, dpuNode, dpu).
				WithStatusSubresource(&provisioningv1.DPU{}).
				Build()

			reconciler = &DPUNodeReconciler{
				Client:   fakeClient,
				Recorder: record.NewFakeRecorder(10),
			}

			// Should create job even with no DPUs in rebooting phase
			result, err := reconciler.rebootNode(ctx, dpuNode)
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).NotTo(BeZero())
		})
	})

	Context("createScriptJob error handling", func() {
		var (
			reconciler *DPUNodeReconciler
			fakeClient client.Client
			ctx        context.Context
			scheme     *runtime.Scheme
		)

		BeforeEach(func() {
			ctx = context.Background()
			scheme = runtime.NewScheme()
			_ = provisioningv1.AddToScheme(scheme)
			_ = corev1.AddToScheme(scheme)
			_ = batchv1.AddToScheme(scheme)
		})

		It("should return error when ConfigMap is not found", func() {
			dpuNode := &provisioningv1.DPUNode{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-dpunode",
					Namespace: "test-namespace",
				},
				Spec: provisioningv1.DPUNodeSpec{
					NodeRebootMethod: &provisioningv1.NodeRebootMethod{
						Script: &provisioningv1.Script{
							Name: "non-existent-configmap",
						},
					},
				},
			}

			fakeClient = fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(dpuNode).
				Build()

			reconciler = &DPUNodeReconciler{
				Client: fakeClient,
			}

			err := reconciler.createScriptJob(ctx, dpuNode)
			Expect(err).To(HaveOccurred())
		})

		It("should return error when ConfigMap is missing pod-template key", func() {
			configMap := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-configmap",
					Namespace: "test-namespace",
				},
				Data: map[string]string{
					"wrong-key": "some-value",
				},
			}

			dpuNode := &provisioningv1.DPUNode{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-dpunode",
					Namespace: "test-namespace",
				},
				Spec: provisioningv1.DPUNodeSpec{
					NodeRebootMethod: &provisioningv1.NodeRebootMethod{
						Script: &provisioningv1.Script{
							Name: "test-configmap",
						},
					},
				},
			}

			fakeClient = fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(configMap, dpuNode).
				Build()

			reconciler = &DPUNodeReconciler{
				Client: fakeClient,
			}

			err := reconciler.createScriptJob(ctx, dpuNode)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("not found in ConfigMap"))
		})

		It("should return error when pod template JSON is invalid", func() {
			configMap := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-configmap",
					Namespace: "test-namespace",
				},
				Data: map[string]string{
					PodTemplateConfigMapKey: "invalid-json{{{",
				},
			}

			dpuNode := &provisioningv1.DPUNode{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-dpunode",
					Namespace: "test-namespace",
				},
				Spec: provisioningv1.DPUNodeSpec{
					NodeRebootMethod: &provisioningv1.NodeRebootMethod{
						Script: &provisioningv1.Script{
							Name: "test-configmap",
						},
					},
				},
			}

			fakeClient = fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(configMap, dpuNode).
				Build()

			reconciler = &DPUNodeReconciler{
				Client: fakeClient,
			}

			err := reconciler.createScriptJob(ctx, dpuNode)
			Expect(err).To(HaveOccurred())
		})

		It("should delete existing job before creating new one", func() {
			podTemplate := corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "new-container",
							Image: "new:image",
						},
					},
					RestartPolicy: corev1.RestartPolicyNever,
				},
			}
			podTemplateJSON, _ := json.Marshal(podTemplate)

			configMap := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-configmap",
					Namespace: "test-namespace",
				},
				Data: map[string]string{
					PodTemplateConfigMapKey: string(podTemplateJSON),
				},
			}

			existingJob := &batchv1.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-dpunode-script-job",
					Namespace: "test-namespace",
				},
				Spec: batchv1.JobSpec{
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{Name: "old-container", Image: "old:image"},
							},
							RestartPolicy: corev1.RestartPolicyNever,
						},
					},
				},
			}

			dpuNode := &provisioningv1.DPUNode{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-dpunode",
					Namespace: "test-namespace",
				},
				Spec: provisioningv1.DPUNodeSpec{
					NodeRebootMethod: &provisioningv1.NodeRebootMethod{
						Script: &provisioningv1.Script{
							Name: "test-configmap",
						},
					},
				},
			}

			fakeClient = fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(configMap, dpuNode, existingJob).
				Build()

			reconciler = &DPUNodeReconciler{
				Client: fakeClient,
			}

			err := reconciler.createScriptJob(ctx, dpuNode)
			Expect(err).NotTo(HaveOccurred())

			// Verify new job was created
			job := &batchv1.Job{}
			err = fakeClient.Get(ctx, types.NamespacedName{
				Name:      "test-dpunode-script-job",
				Namespace: "test-namespace",
			}, job)
			Expect(err).NotTo(HaveOccurred())
			Expect(job.Spec.Template.Spec.Containers[0].Name).To(Equal("new-container"))
		})

		It("should add init container volume mounts when volume exists", func() {
			podTemplate := corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					InitContainers: []corev1.Container{
						{
							Name:  "init-container",
							Image: "init:image",
						},
					},
					Containers: []corev1.Container{
						{
							Name:  "main-container",
							Image: "main:image",
						},
					},
					RestartPolicy: corev1.RestartPolicyNever,
				},
			}
			podTemplateJSON, _ := json.Marshal(podTemplate)

			configMap := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-configmap",
					Namespace: "test-namespace",
				},
				Data: map[string]string{
					PodTemplateConfigMapKey: string(podTemplateJSON),
				},
			}

			dpuNode := &provisioningv1.DPUNode{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-dpunode",
					Namespace: "test-namespace",
					Labels: map[string]string{
						"test-label": "test-value",
					},
				},
				Spec: provisioningv1.DPUNodeSpec{
					NodeRebootMethod: &provisioningv1.NodeRebootMethod{
						Script: &provisioningv1.Script{
							Name: "test-configmap",
						},
					},
				},
			}

			fakeClient = fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(configMap, dpuNode).
				Build()

			reconciler = &DPUNodeReconciler{
				Client: fakeClient,
			}

			err := reconciler.createScriptJob(ctx, dpuNode)
			Expect(err).NotTo(HaveOccurred())

			// Verify job was created with init container volume mounts
			job := &batchv1.Job{}
			err = fakeClient.Get(ctx, types.NamespacedName{
				Name:      "test-dpunode-script-job",
				Namespace: "test-namespace",
			}, job)
			Expect(err).NotTo(HaveOccurred())
			Expect(job.Spec.Template.Spec.InitContainers).To(HaveLen(1))
			// Verify volume mount was added
			Expect(job.Spec.Template.Spec.InitContainers[0].VolumeMounts).NotTo(BeEmpty())
		})

		It("should not duplicate volume when podinfo volume already exists", func() {
			podTemplate := corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "main-container",
							Image: "main:image",
						},
					},
					Volumes: []corev1.Volume{
						{
							Name: PodInfoVolumeName,
							VolumeSource: corev1.VolumeSource{
								EmptyDir: &corev1.EmptyDirVolumeSource{},
							},
						},
					},
					RestartPolicy: corev1.RestartPolicyNever,
				},
			}
			podTemplateJSON, _ := json.Marshal(podTemplate)

			configMap := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-configmap",
					Namespace: "test-namespace",
				},
				Data: map[string]string{
					PodTemplateConfigMapKey: string(podTemplateJSON),
				},
			}

			dpuNode := &provisioningv1.DPUNode{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-dpunode",
					Namespace: "test-namespace",
				},
				Spec: provisioningv1.DPUNodeSpec{
					NodeRebootMethod: &provisioningv1.NodeRebootMethod{
						Script: &provisioningv1.Script{
							Name: "test-configmap",
						},
					},
				},
			}

			fakeClient = fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(configMap, dpuNode).
				Build()

			reconciler = &DPUNodeReconciler{
				Client: fakeClient,
			}

			err := reconciler.createScriptJob(ctx, dpuNode)
			Expect(err).NotTo(HaveOccurred())

			// Verify job was created without duplicating the volume
			job := &batchv1.Job{}
			err = fakeClient.Get(ctx, types.NamespacedName{
				Name:      "test-dpunode-script-job",
				Namespace: "test-namespace",
			}, job)
			Expect(err).NotTo(HaveOccurred())
			// Should have exactly 1 volume (the existing one, not duplicated)
			Expect(job.Spec.Template.Spec.Volumes).To(HaveLen(1))
		})

		It("should copy dpuNode labels to pod template", func() {
			podTemplate := corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "main-container",
							Image: "main:image",
						},
					},
					RestartPolicy: corev1.RestartPolicyNever,
				},
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"existing-label": "existing-value",
					},
				},
			}
			podTemplateJSON, _ := json.Marshal(podTemplate)

			configMap := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-configmap",
					Namespace: "test-namespace",
				},
				Data: map[string]string{
					PodTemplateConfigMapKey: string(podTemplateJSON),
				},
			}

			dpuNode := &provisioningv1.DPUNode{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-dpunode",
					Namespace: "test-namespace",
					Labels: map[string]string{
						"dpunode-label":  "dpunode-value",
						"existing-label": "should-not-override", // Should not override existing
					},
				},
				Spec: provisioningv1.DPUNodeSpec{
					NodeRebootMethod: &provisioningv1.NodeRebootMethod{
						Script: &provisioningv1.Script{
							Name: "test-configmap",
						},
					},
				},
			}

			fakeClient = fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(configMap, dpuNode).
				Build()

			reconciler = &DPUNodeReconciler{
				Client: fakeClient,
			}

			err := reconciler.createScriptJob(ctx, dpuNode)
			Expect(err).NotTo(HaveOccurred())

			job := &batchv1.Job{}
			err = fakeClient.Get(ctx, types.NamespacedName{
				Name:      "test-dpunode-script-job",
				Namespace: "test-namespace",
			}, job)
			Expect(err).NotTo(HaveOccurred())
			// Verify dpuNode label was added
			Expect(job.Spec.Template.Labels["dpunode-label"]).To(Equal("dpunode-value"))
			// Verify existing label was NOT overridden
			Expect(job.Spec.Template.Labels["existing-label"]).To(Equal("existing-value"))
		})

		It("should not override existing annotations in pod template", func() {
			podTemplate := corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "main-container",
							Image: "main:image",
						},
					},
					RestartPolicy: corev1.RestartPolicyNever,
				},
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						"existing-annotation": "existing-value",
					},
				},
			}
			podTemplateJSON, _ := json.Marshal(podTemplate)

			configMap := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-configmap",
					Namespace: "test-namespace",
				},
				Data: map[string]string{
					PodTemplateConfigMapKey: string(podTemplateJSON),
				},
			}

			dpuNode := &provisioningv1.DPUNode{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-dpunode",
					Namespace: "test-namespace",
					Annotations: map[string]string{
						"dpunode-annotation":  "dpunode-value",
						"existing-annotation": "should-not-override", // Should not override existing
					},
				},
				Spec: provisioningv1.DPUNodeSpec{
					NodeRebootMethod: &provisioningv1.NodeRebootMethod{
						Script: &provisioningv1.Script{
							Name: "test-configmap",
						},
					},
				},
			}

			fakeClient = fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(configMap, dpuNode).
				Build()

			reconciler = &DPUNodeReconciler{
				Client: fakeClient,
			}

			err := reconciler.createScriptJob(ctx, dpuNode)
			Expect(err).NotTo(HaveOccurred())

			job := &batchv1.Job{}
			err = fakeClient.Get(ctx, types.NamespacedName{
				Name:      "test-dpunode-script-job",
				Namespace: "test-namespace",
			}, job)
			Expect(err).NotTo(HaveOccurred())
			// Verify dpuNode annotation was added
			Expect(job.Spec.Template.Annotations["dpunode-annotation"]).To(Equal("dpunode-value"))
			// Verify existing annotation was NOT overridden
			Expect(job.Spec.Template.Annotations["existing-annotation"]).To(Equal("existing-value"))
		})

		It("should handle external reboot completion after annotation removal", func() {
			dpuNode := &provisioningv1.DPUNode{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-dpunode",
					Namespace: "test-namespace",
					// No external reboot annotation - simulates user removed it
				},
				Spec: provisioningv1.DPUNodeSpec{
					NodeRebootMethod: &provisioningv1.NodeRebootMethod{
						External: &provisioningv1.External{},
					},
					DPUs: []provisioningv1.DPURef{
						{Name: "dpu1"},
					},
				},
			}

			dpu := &provisioningv1.DPU{
				ObjectMeta: metav1.ObjectMeta{
					Name:      cutil.GenerateDPUName(dpuNode.Name, "dpu1"),
					Namespace: "test-namespace",
				},
				Status: provisioningv1.DPUStatus{
					Phase: provisioningv1.DPURebooting,
				},
			}
			// Set condition to indicate waiting for external reboot
			cutil.SetDPUCondition(&dpu.Status, cutil.DPUCondition(provisioningv1.DPUCondRebooted, "WaitingForManualPowerCycleOrReboot", ""))

			fakeClient = fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(dpuNode, dpu).
				WithStatusSubresource(&provisioningv1.DPU{}).
				Build()

			reconciler = &DPUNodeReconciler{
				Client:   fakeClient,
				Recorder: record.NewFakeRecorder(10),
			}

			result, err := reconciler.rebootNode(ctx, dpuNode)
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).To(BeZero())

			// Verify DPU condition was updated
			updatedDPU := &provisioningv1.DPU{}
			Expect(fakeClient.Get(ctx, types.NamespacedName{
				Name:      cutil.GenerateDPUName(dpuNode.Name, "dpu1"),
				Namespace: "test-namespace",
			}, updatedDPU)).To(Succeed())
			_, cond := cutil.GetDPUCondition(&updatedDPU.Status, string(provisioningv1.DPUCondRebooted))
			Expect(cond).NotTo(BeNil())
		})
	})

	Context("Reconcile - DPUNode Deletion", func() {
		var (
			reconciler *DPUNodeReconciler
			fakeClient client.Client
			ctx        context.Context
			scheme     *runtime.Scheme
		)

		BeforeEach(func() {
			ctx = context.Background()
			scheme = runtime.NewScheme()
			_ = corev1.AddToScheme(scheme)
			_ = provisioningv1.AddToScheme(scheme)

			fakeClient = fake.NewClientBuilder().
				WithScheme(scheme).
				WithStatusSubresource(&provisioningv1.DPUNode{}, &provisioningv1.DPU{}, &provisioningv1.DPUDevice{}).
				Build()

			dpuInstallInterface := dpuInstallInterface
			reconciler = &DPUNodeReconciler{
				Client:              fakeClient,
				DPUInstallInterface: &dpuInstallInterface,
				Recorder:            record.NewFakeRecorder(32),
			}
		})

		It("should return without error when DPUNode does not exist", func() {
			result, err := reconciler.Reconcile(ctx, ctrl.Request{
				NamespacedName: types.NamespacedName{
					Name:      "non-existent-dpunode",
					Namespace: "test-namespace",
				},
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(Equal(ctrl.Result{}))
		})

		It("should add finalizer to DPUNode without finalizer", func() {
			dpuNode := createTestDPUNode("test-dpunode", nil)
			Expect(fakeClient.Create(ctx, dpuNode)).To(Succeed())

			result, err := reconciler.Reconcile(ctx, ctrl.Request{
				NamespacedName: types.NamespacedName{
					Name:      "test-dpunode",
					Namespace: "test-namespace",
				},
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(Equal(ctrl.Result{}))

			// Verify finalizer was added
			updatedDPUNode := &provisioningv1.DPUNode{}
			Expect(fakeClient.Get(ctx, types.NamespacedName{
				Name:      "test-dpunode",
				Namespace: "test-namespace",
			}, updatedDPUNode)).To(Succeed())
			Expect(updatedDPUNode.Finalizers).To(ContainElement(provisioningv1.DPUNodeFinalizer))
		})

		It("should delete associated DPUs when DPUNode is being deleted", func() {
			// Create DPUNode with finalizer
			dpuNode := createTestDPUNode("test-dpunode", []string{provisioningv1.DPUNodeFinalizer})
			Expect(fakeClient.Create(ctx, dpuNode)).To(Succeed())

			// Create associated DPUs with the dpunode label
			dpu1 := &provisioningv1.DPU{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-dpu-1",
					Namespace: "test-namespace",
					Labels: map[string]string{
						cutil.DPUNodeNameLabel: "test-dpunode",
					},
				},
				Spec: provisioningv1.DPUSpec{
					DPUNodeName: "test-dpunode",
				},
			}
			dpu2 := &provisioningv1.DPU{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-dpu-2",
					Namespace: "test-namespace",
					Labels: map[string]string{
						cutil.DPUNodeNameLabel: "test-dpunode",
					},
				},
				Spec: provisioningv1.DPUSpec{
					DPUNodeName: "test-dpunode",
				},
			}
			Expect(fakeClient.Create(ctx, dpu1)).To(Succeed())
			Expect(fakeClient.Create(ctx, dpu2)).To(Succeed())

			// Delete the DPUNode
			Expect(fakeClient.Delete(ctx, dpuNode)).To(Succeed())

			// Reconcile should delete the associated DPUs
			result, err := reconciler.Reconcile(ctx, ctrl.Request{
				NamespacedName: types.NamespacedName{
					Name:      "test-dpunode",
					Namespace: "test-namespace",
				},
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(Equal(ctrl.Result{}))

			// Verify DPUs have deletion timestamp (marked for deletion)
			updatedDPU1 := &provisioningv1.DPU{}
			err = fakeClient.Get(ctx, types.NamespacedName{
				Name:      "test-dpu-1",
				Namespace: "test-namespace",
			}, updatedDPU1)
			// DPU should either be deleted or have deletion timestamp
			if err == nil {
				Expect(updatedDPU1.DeletionTimestamp).NotTo(BeNil())
			}

			updatedDPU2 := &provisioningv1.DPU{}
			err = fakeClient.Get(ctx, types.NamespacedName{
				Name:      "test-dpu-2",
				Namespace: "test-namespace",
			}, updatedDPU2)
			// DPU should either be deleted or have deletion timestamp
			if err == nil {
				Expect(updatedDPU2.DeletionTimestamp).NotTo(BeNil())
			}
		})

		It("should delete associated DPUDevices when DPUNode is being deleted", func() {
			// Create DPUNode with finalizer
			dpuNode := createTestDPUNode("test-dpunode", []string{provisioningv1.DPUNodeFinalizer})
			Expect(fakeClient.Create(ctx, dpuNode)).To(Succeed())

			// Create associated DPUDevices with the dpunode label
			dpuDevice1 := &provisioningv1.DPUDevice{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-dpudevice-1",
					Namespace: "test-namespace",
					Labels: map[string]string{
						cutil.DPUNodeNameLabel: "test-dpunode",
					},
				},
				Spec: provisioningv1.DPUDeviceSpec{},
			}
			dpuDevice2 := &provisioningv1.DPUDevice{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-dpudevice-2",
					Namespace: "test-namespace",
					Labels: map[string]string{
						cutil.DPUNodeNameLabel: "test-dpunode",
					},
				},
				Spec: provisioningv1.DPUDeviceSpec{},
			}
			Expect(fakeClient.Create(ctx, dpuDevice1)).To(Succeed())
			Expect(fakeClient.Create(ctx, dpuDevice2)).To(Succeed())

			// Delete the DPUNode
			Expect(fakeClient.Delete(ctx, dpuNode)).To(Succeed())

			// Reconcile should delete the associated DPUDevices
			result, err := reconciler.Reconcile(ctx, ctrl.Request{
				NamespacedName: types.NamespacedName{
					Name:      "test-dpunode",
					Namespace: "test-namespace",
				},
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(Equal(ctrl.Result{}))

			// Verify DPUDevices have deletion timestamp (marked for deletion)
			updatedDPUDevice1 := &provisioningv1.DPUDevice{}
			err = fakeClient.Get(ctx, types.NamespacedName{
				Name:      "test-dpudevice-1",
				Namespace: "test-namespace",
			}, updatedDPUDevice1)
			// DPUDevice should either be deleted or have deletion timestamp
			if err == nil {
				Expect(updatedDPUDevice1.DeletionTimestamp).NotTo(BeNil())
			}

			updatedDPUDevice2 := &provisioningv1.DPUDevice{}
			err = fakeClient.Get(ctx, types.NamespacedName{
				Name:      "test-dpudevice-2",
				Namespace: "test-namespace",
			}, updatedDPUDevice2)
			// DPUDevice should either be deleted or have deletion timestamp
			if err == nil {
				Expect(updatedDPUDevice2.DeletionTimestamp).NotTo(BeNil())
			}
		})

		It("should remove finalizer when all DPUDevices are deleted and no DPUs exist", func() {
			// Create DPUNode with finalizer
			dpuNode := createTestDPUNode("test-dpunode", []string{provisioningv1.DPUNodeFinalizer})
			Expect(fakeClient.Create(ctx, dpuNode)).To(Succeed())

			// Delete the DPUNode (no associated DPUs or DPUDevices)
			Expect(fakeClient.Delete(ctx, dpuNode)).To(Succeed())

			// Reconcile should remove the finalizer
			result, err := reconciler.Reconcile(ctx, ctrl.Request{
				NamespacedName: types.NamespacedName{
					Name:      "test-dpunode",
					Namespace: "test-namespace",
				},
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(Equal(ctrl.Result{}))

			// Verify finalizer was removed - DPUNode may be deleted or still exist without finalizer
			updatedDPUNode := &provisioningv1.DPUNode{}
			err = fakeClient.Get(ctx, types.NamespacedName{
				Name:      "test-dpunode",
				Namespace: "test-namespace",
			}, updatedDPUNode)
			if err == nil {
				// If DPUNode still exists, verify finalizer was removed
				Expect(updatedDPUNode.Finalizers).NotTo(ContainElement(provisioningv1.DPUNodeFinalizer))
			}
		})

		It("should not remove finalizer when DPUDevices still exist", func() {
			// Create DPUNode with finalizer
			dpuNode := createTestDPUNode("test-dpunode", []string{provisioningv1.DPUNodeFinalizer})
			Expect(fakeClient.Create(ctx, dpuNode)).To(Succeed())

			// Create associated DPUDevice that won't be deleted yet
			dpuDevice := &provisioningv1.DPUDevice{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-dpudevice",
					Namespace:  "test-namespace",
					Finalizers: []string{"some-other-finalizer"}, // This prevents immediate deletion
					Labels: map[string]string{
						cutil.DPUNodeNameLabel: "test-dpunode",
					},
				},
				Spec: provisioningv1.DPUDeviceSpec{},
			}
			Expect(fakeClient.Create(ctx, dpuDevice)).To(Succeed())

			// Delete the DPUNode
			Expect(fakeClient.Delete(ctx, dpuNode)).To(Succeed())

			// Reconcile should NOT remove the finalizer because DPUDevice still exists
			result, err := reconciler.Reconcile(ctx, ctrl.Request{
				NamespacedName: types.NamespacedName{
					Name:      "test-dpunode",
					Namespace: "test-namespace",
				},
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(Equal(ctrl.Result{}))

			// Verify finalizer is still present
			updatedDPUNode := &provisioningv1.DPUNode{}
			Expect(fakeClient.Get(ctx, types.NamespacedName{
				Name:      "test-dpunode",
				Namespace: "test-namespace",
			}, updatedDPUNode)).To(Succeed())
			Expect(updatedDPUNode.Finalizers).To(ContainElement(provisioningv1.DPUNodeFinalizer))
			Expect(updatedDPUNode.DeletionTimestamp).NotTo(BeNil())
		})

		It("should not remove finalizer when DPUs still exist", func() {
			// Create DPUNode with finalizer
			dpuNode := createTestDPUNode("test-dpunode", []string{provisioningv1.DPUNodeFinalizer})
			Expect(fakeClient.Create(ctx, dpuNode)).To(Succeed())

			// Create associated DPU with a finalizer to prevent immediate deletion
			dpu := &provisioningv1.DPU{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-dpu",
					Namespace:  "test-namespace",
					Finalizers: []string{"test-finalizer"}, // Prevent immediate deletion in fake client
					Labels: map[string]string{
						cutil.DPUNodeNameLabel: "test-dpunode",
					},
				},
				Spec: provisioningv1.DPUSpec{
					DPUNodeName: "test-dpunode",
				},
			}
			Expect(fakeClient.Create(ctx, dpu)).To(Succeed())

			// Delete the DPUNode
			Expect(fakeClient.Delete(ctx, dpuNode)).To(Succeed())

			// First reconcile - should trigger DPU deletion but not remove finalizer yet
			result, err := reconciler.Reconcile(ctx, ctrl.Request{
				NamespacedName: types.NamespacedName{
					Name:      "test-dpunode",
					Namespace: "test-namespace",
				},
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(Equal(ctrl.Result{}))

			// Verify finalizer is still present
			updatedDPUNode := &provisioningv1.DPUNode{}
			Expect(fakeClient.Get(ctx, types.NamespacedName{
				Name:      "test-dpunode",
				Namespace: "test-namespace",
			}, updatedDPUNode)).To(Succeed())
			Expect(updatedDPUNode.Finalizers).To(ContainElement(provisioningv1.DPUNodeFinalizer))
			Expect(updatedDPUNode.DeletionTimestamp).NotTo(BeNil())
		})

		It("should handle deletion of DPUNode with multiple finalizers", func() {
			// Create DPUNode with multiple finalizers
			dpuNode := createTestDPUNode("test-dpunode", []string{
				provisioningv1.DPUNodeFinalizer,
				"other.finalizer.io/test",
			})
			Expect(fakeClient.Create(ctx, dpuNode)).To(Succeed())

			// Delete the DPUNode (no associated DPUs or DPUDevices)
			Expect(fakeClient.Delete(ctx, dpuNode)).To(Succeed())

			// Reconcile should remove only the DPUNode finalizer
			result, err := reconciler.Reconcile(ctx, ctrl.Request{
				NamespacedName: types.NamespacedName{
					Name:      "test-dpunode",
					Namespace: "test-namespace",
				},
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(Equal(ctrl.Result{}))

			// Verify DPUNode finalizer was removed but other finalizers remain
			updatedDPUNode := &provisioningv1.DPUNode{}
			Expect(fakeClient.Get(ctx, types.NamespacedName{
				Name:      "test-dpunode",
				Namespace: "test-namespace",
			}, updatedDPUNode)).To(Succeed())
			Expect(updatedDPUNode.Finalizers).NotTo(ContainElement(provisioningv1.DPUNodeFinalizer))
			Expect(updatedDPUNode.Finalizers).To(ContainElement("other.finalizer.io/test"))
			Expect(updatedDPUNode.DeletionTimestamp).NotTo(BeNil())
		})

		It("should handle cascading deletion - DPUs and DPUDevices together", func() {
			// Create DPUNode with finalizer
			dpuNode := createTestDPUNode("test-dpunode", []string{provisioningv1.DPUNodeFinalizer})
			Expect(fakeClient.Create(ctx, dpuNode)).To(Succeed())

			// Create associated DPU
			dpu := &provisioningv1.DPU{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-dpu",
					Namespace: "test-namespace",
					Labels: map[string]string{
						cutil.DPUNodeNameLabel: "test-dpunode",
					},
				},
				Spec: provisioningv1.DPUSpec{
					DPUNodeName: "test-dpunode",
				},
			}
			Expect(fakeClient.Create(ctx, dpu)).To(Succeed())

			// Create associated DPUDevice
			dpuDevice := &provisioningv1.DPUDevice{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-dpudevice",
					Namespace: "test-namespace",
					Labels: map[string]string{
						cutil.DPUNodeNameLabel: "test-dpunode",
					},
				},
				Spec: provisioningv1.DPUDeviceSpec{},
			}
			Expect(fakeClient.Create(ctx, dpuDevice)).To(Succeed())

			// Delete the DPUNode
			Expect(fakeClient.Delete(ctx, dpuNode)).To(Succeed())

			// Reconcile should delete both DPU and DPUDevice
			result, err := reconciler.Reconcile(ctx, ctrl.Request{
				NamespacedName: types.NamespacedName{
					Name:      "test-dpunode",
					Namespace: "test-namespace",
				},
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(Equal(ctrl.Result{}))

			// Verify both DPU and DPUDevice are marked for deletion
			updatedDPU := &provisioningv1.DPU{}
			err = fakeClient.Get(ctx, types.NamespacedName{
				Name:      "test-dpu",
				Namespace: "test-namespace",
			}, updatedDPU)
			if err == nil {
				Expect(updatedDPU.DeletionTimestamp).NotTo(BeNil())
			}

			updatedDPUDevice := &provisioningv1.DPUDevice{}
			err = fakeClient.Get(ctx, types.NamespacedName{
				Name:      "test-dpudevice",
				Namespace: "test-namespace",
			}, updatedDPUDevice)
			if err == nil {
				Expect(updatedDPUDevice.DeletionTimestamp).NotTo(BeNil())
			}
		})

		It("should not affect DPUs and DPUDevices from other DPUNodes", func() {
			// Create two DPUNodes
			dpuNode1 := createTestDPUNode("test-dpunode-1", []string{provisioningv1.DPUNodeFinalizer})
			dpuNode2 := createTestDPUNode("test-dpunode-2", []string{provisioningv1.DPUNodeFinalizer})
			Expect(fakeClient.Create(ctx, dpuNode1)).To(Succeed())
			Expect(fakeClient.Create(ctx, dpuNode2)).To(Succeed())

			// Create DPUs for both nodes
			dpu1 := &provisioningv1.DPU{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-dpu-1",
					Namespace: "test-namespace",
					Labels: map[string]string{
						cutil.DPUNodeNameLabel: "test-dpunode-1",
					},
				},
				Spec: provisioningv1.DPUSpec{
					DPUNodeName: "test-dpunode-1",
				},
			}
			dpu2 := &provisioningv1.DPU{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-dpu-2",
					Namespace: "test-namespace",
					Labels: map[string]string{
						cutil.DPUNodeNameLabel: "test-dpunode-2",
					},
				},
				Spec: provisioningv1.DPUSpec{
					DPUNodeName: "test-dpunode-2",
				},
			}
			Expect(fakeClient.Create(ctx, dpu1)).To(Succeed())
			Expect(fakeClient.Create(ctx, dpu2)).To(Succeed())

			// Delete only dpuNode1
			Expect(fakeClient.Delete(ctx, dpuNode1)).To(Succeed())

			// Reconcile dpuNode1
			result, err := reconciler.Reconcile(ctx, ctrl.Request{
				NamespacedName: types.NamespacedName{
					Name:      "test-dpunode-1",
					Namespace: "test-namespace",
				},
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(Equal(ctrl.Result{}))

			// Verify dpu1 is marked for deletion
			updatedDPU1 := &provisioningv1.DPU{}
			err = fakeClient.Get(ctx, types.NamespacedName{
				Name:      "test-dpu-1",
				Namespace: "test-namespace",
			}, updatedDPU1)
			if err == nil {
				Expect(updatedDPU1.DeletionTimestamp).NotTo(BeNil())
			}

			// Verify dpu2 is NOT affected
			updatedDPU2 := &provisioningv1.DPU{}
			Expect(fakeClient.Get(ctx, types.NamespacedName{
				Name:      "test-dpu-2",
				Namespace: "test-namespace",
			}, updatedDPU2)).To(Succeed())
			Expect(updatedDPU2.DeletionTimestamp).To(BeNil())
		})

		It("should return error when listing DPU fails during DPUNode deletion", func() {
			// Create DPUNode with finalizer
			dpuNode := createTestDPUNode("test-dpunode", []string{provisioningv1.DPUNodeFinalizer})
			Expect(fakeClient.Create(ctx, dpuNode)).To(Succeed())

			// Delete the DPUNode (this sets DeletionTimestamp)
			Expect(fakeClient.Delete(ctx, dpuNode)).To(Succeed())

			// Create error injecting client that fails on DPUList List operations
			errorClient := &dpuNodeErrorInjectingClient{
				Client: fakeClient,
				listFunc: func(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
					// Return error only for DPUList List operations
					if _, ok := list.(*provisioningv1.DPUList); ok {
						return fmt.Errorf("simulated DPU list error")
					}
					// Pass through for other List operations
					return fakeClient.List(ctx, list, opts...)
				},
			}

			dpuInstallInterface := dpuInstallInterface

			errorReconciler := &DPUNodeReconciler{
				Client:              errorClient,
				DPUInstallInterface: &dpuInstallInterface,
				Recorder:            record.NewFakeRecorder(32),
			}

			// Reconcile should return error
			result, err := errorReconciler.Reconcile(ctx, ctrl.Request{
				NamespacedName: types.NamespacedName{
					Name:      "test-dpunode",
					Namespace: "test-namespace",
				},
			})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("simulated DPU list error"))
			Expect(result).To(Equal(ctrl.Result{}))

			// Verify DPUNode still exists with finalizer (deletion not completed due to list error)
			updatedDPUNode := &provisioningv1.DPUNode{}
			Expect(fakeClient.Get(ctx, types.NamespacedName{
				Name:      "test-dpunode",
				Namespace: "test-namespace",
			}, updatedDPUNode)).To(Succeed())
			Expect(updatedDPUNode.Finalizers).To(ContainElement(provisioningv1.DPUNodeFinalizer))
			Expect(updatedDPUNode.DeletionTimestamp.IsZero()).To(BeFalse())
		})

		It("should return error when listing DPUDevice fails during DPUNode deletion", func() {
			// Create DPUNode with finalizer
			dpuNode := createTestDPUNode("test-dpunode", []string{provisioningv1.DPUNodeFinalizer})
			Expect(fakeClient.Create(ctx, dpuNode)).To(Succeed())

			// Delete the DPUNode (this sets DeletionTimestamp)
			Expect(fakeClient.Delete(ctx, dpuNode)).To(Succeed())

			// Create error injecting client that fails on DPUDeviceList List operations
			errorClient := &dpuNodeErrorInjectingClient{
				Client: fakeClient,
				listFunc: func(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
					// Return error only for DPUDeviceList List operations
					if _, ok := list.(*provisioningv1.DPUDeviceList); ok {
						return fmt.Errorf("simulated DPUDevice list error")
					}
					// Pass through for other List operations
					return fakeClient.List(ctx, list, opts...)
				},
			}

			dpuInstallInterface := dpuInstallInterface
			errorReconciler := &DPUNodeReconciler{
				Client:              errorClient,
				DPUInstallInterface: &dpuInstallInterface,
				Recorder:            record.NewFakeRecorder(32),
			}

			// Reconcile should return error
			result, err := errorReconciler.Reconcile(ctx, ctrl.Request{
				NamespacedName: types.NamespacedName{
					Name:      "test-dpunode",
					Namespace: "test-namespace",
				},
			})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("simulated DPUDevice list error"))
			Expect(result).To(Equal(ctrl.Result{}))

			// Verify DPUNode still exists with finalizer (deletion not completed due to list error)
			updatedDPUNode := &provisioningv1.DPUNode{}
			Expect(fakeClient.Get(ctx, types.NamespacedName{
				Name:      "test-dpunode",
				Namespace: "test-namespace",
			}, updatedDPUNode)).To(Succeed())
			Expect(updatedDPUNode.Finalizers).To(ContainElement(provisioningv1.DPUNodeFinalizer))
			Expect(updatedDPUNode.DeletionTimestamp.IsZero()).To(BeFalse())
		})

		It("should return error when deleting DPUDevice fails during DPUNode deletion", func() {
			// Create DPUNode with finalizer
			dpuNode := createTestDPUNode("test-dpunode", []string{provisioningv1.DPUNodeFinalizer})
			Expect(fakeClient.Create(ctx, dpuNode)).To(Succeed())

			// Create associated DPUDevice
			dpuDevice := &provisioningv1.DPUDevice{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-dpudevice",
					Namespace: "test-namespace",
					Labels: map[string]string{
						cutil.DPUNodeNameLabel: "test-dpunode",
					},
				},
				Spec: provisioningv1.DPUDeviceSpec{},
			}
			Expect(fakeClient.Create(ctx, dpuDevice)).To(Succeed())

			// Delete the DPUNode (this sets DeletionTimestamp)
			Expect(fakeClient.Delete(ctx, dpuNode)).To(Succeed())

			// Create error injecting client that fails on DPUDevice Delete operations
			errorClient := &dpuNodeErrorInjectingClient{
				Client: fakeClient,
				deleteFunc: func(ctx context.Context, obj client.Object, opts ...client.DeleteOption) error {
					// Return error only for DPUDevice Delete operations
					if _, ok := obj.(*provisioningv1.DPUDevice); ok {
						return fmt.Errorf("simulated DPUDevice delete error")
					}
					// Pass through for other Delete operations
					return fakeClient.Delete(ctx, obj, opts...)
				},
			}

			dpuInstallInterface := dpuInstallInterface
			errorReconciler := &DPUNodeReconciler{
				Client:              errorClient,
				DPUInstallInterface: &dpuInstallInterface,
				Recorder:            record.NewFakeRecorder(32),
			}

			// Reconcile should return error
			result, err := errorReconciler.Reconcile(ctx, ctrl.Request{
				NamespacedName: types.NamespacedName{
					Name:      "test-dpunode",
					Namespace: "test-namespace",
				},
			})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("simulated DPUDevice delete error"))
			Expect(result).To(Equal(ctrl.Result{}))

			// Verify DPUNode still exists with finalizer (deletion not completed due to delete error)
			updatedDPUNode := &provisioningv1.DPUNode{}
			Expect(fakeClient.Get(ctx, types.NamespacedName{
				Name:      "test-dpunode",
				Namespace: "test-namespace",
			}, updatedDPUNode)).To(Succeed())
			Expect(updatedDPUNode.Finalizers).To(ContainElement(provisioningv1.DPUNodeFinalizer))
			Expect(updatedDPUNode.DeletionTimestamp.IsZero()).To(BeFalse())

			// Verify DPUDevice still exists (not deleted due to error)
			updatedDPUDevice := &provisioningv1.DPUDevice{}
			Expect(fakeClient.Get(ctx, types.NamespacedName{
				Name:      "test-dpudevice",
				Namespace: "test-namespace",
			}, updatedDPUDevice)).To(Succeed())
		})
	})

	Context("noneDPUInNodeEffectOrRebooting", func() {
		var (
			reconciler *DPUNodeReconciler
			fakeClient client.Client
			ctx        context.Context
		)

		BeforeEach(func() {
			ctx = context.Background()
			scheme := runtime.NewScheme()
			_ = provisioningv1.AddToScheme(scheme)

			fakeClient = fake.NewClientBuilder().
				WithScheme(scheme).
				WithStatusSubresource(&provisioningv1.DPU{}, &provisioningv1.DPUNode{}).
				Build()

			reconciler = &DPUNodeReconciler{
				Client: fakeClient,
			}
		})

		It("should return nil when no DPUNodeMaintenance exists and no RebootInProgress condition", func() {
			dpuNode := &provisioningv1.DPUNode{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-dpunode",
					Namespace: "test-namespace",
				},
				Status: provisioningv1.DPUNodeStatus{
					Conditions: []metav1.Condition{},
				},
			}

			err := reconciler.noneDPUInNodeEffectOrRebooting(ctx, dpuNode, false)
			Expect(err).NotTo(HaveOccurred())
		})

		It("should return error when NodeEffectInProgress condition is True", func() {
			dpuNode := &provisioningv1.DPUNode{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-dpunode",
					Namespace: "test-namespace",
				},
				Status: provisioningv1.DPUNodeStatus{
					Conditions: []metav1.Condition{
						{
							Type:   provisioningv1.DPUNodeConditionNodeEffectInProgress.String(),
							Status: metav1.ConditionTrue,
						},
					},
				},
			}

			err := reconciler.noneDPUInNodeEffectOrRebooting(ctx, dpuNode, true)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(Equal("node effect is in progress"))
		})

		It("should return error when RebootInProgress condition is True", func() {
			dpuNode := &provisioningv1.DPUNode{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-dpunode",
					Namespace: "test-namespace",
				},
				Status: provisioningv1.DPUNodeStatus{
					Conditions: []metav1.Condition{
						{
							Type:   provisioningv1.DPUNodeConditionRebootInProgress.String(),
							Status: metav1.ConditionTrue,
						},
					},
				},
			}

			err := reconciler.noneDPUInNodeEffectOrRebooting(ctx, dpuNode, false)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(Equal("reboot is in progress"))
		})

		It("should remove RebootInProgress condition when it exists but no DPUs exist", func() {
			dpuNode := &provisioningv1.DPUNode{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-dpunode",
					Namespace: "test-namespace",
				},
				Status: provisioningv1.DPUNodeStatus{
					Conditions: []metav1.Condition{
						{
							Type:   provisioningv1.DPUNodeConditionRebootInProgress.String(),
							Status: metav1.ConditionFalse,
						},
					},
				},
			}

			// No DPUs exist for this DPUNode
			err := reconciler.noneDPUInNodeEffectOrRebooting(ctx, dpuNode, false)
			Expect(err).NotTo(HaveOccurred())

			// Verify RebootInProgress condition was removed
			Expect(dpuNode.Status.Conditions).To(BeEmpty())
		})

		It("should keep RebootInProgress condition when DPUs exist", func() {
			dpuNode := &provisioningv1.DPUNode{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-dpunode",
					Namespace: "test-namespace",
					Labels: map[string]string{
						cutil.DPUNodeNameLabel: "test-dpunode",
					},
				},
				Status: provisioningv1.DPUNodeStatus{
					Conditions: []metav1.Condition{
						{
							Type:   provisioningv1.DPUNodeConditionRebootInProgress.String(),
							Status: metav1.ConditionFalse,
						},
					},
				},
			}

			// Create a DPU for this DPUNode
			dpu := &provisioningv1.DPU{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-dpu",
					Namespace: "test-namespace",
					Labels: map[string]string{
						cutil.DPUNodeNameLabel: "test-dpunode",
					},
				},
				Spec: provisioningv1.DPUSpec{
					DPUNodeName: "test-dpunode",
				},
				Status: provisioningv1.DPUStatus{
					Phase: provisioningv1.DPUReady,
				},
			}
			Expect(fakeClient.Create(ctx, dpu)).To(Succeed())

			err := reconciler.noneDPUInNodeEffectOrRebooting(ctx, dpuNode, false)
			Expect(err).NotTo(HaveOccurred())

			// Verify RebootInProgress condition still exists
			Expect(dpuNode.Status.Conditions).To(HaveLen(1))
			Expect(dpuNode.Status.Conditions[0].Type).To(Equal(provisioningv1.DPUNodeConditionRebootInProgress.String()))
		})

		It("should handle both NodeEffectInProgress and RebootInProgress conditions", func() {
			dpuNode := &provisioningv1.DPUNode{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-dpunode",
					Namespace: "test-namespace",
				},
				Status: provisioningv1.DPUNodeStatus{
					Conditions: []metav1.Condition{
						{
							Type:   provisioningv1.DPUNodeConditionNodeEffectInProgress.String(),
							Status: metav1.ConditionTrue,
						},
						{
							Type:   provisioningv1.DPUNodeConditionRebootInProgress.String(),
							Status: metav1.ConditionFalse,
						},
					},
				},
			}

			// NodeEffect takes precedence, should return error before checking RebootInProgress
			err := reconciler.noneDPUInNodeEffectOrRebooting(ctx, dpuNode, true)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(Equal("node effect is in progress"))
		})
	})
})
