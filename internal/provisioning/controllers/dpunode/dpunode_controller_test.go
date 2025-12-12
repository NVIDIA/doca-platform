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
	"testing"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestDPUNodeReconciler(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "DPUNode Reconciler Non exported Suite")
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
	})
})
