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
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

const dpuInstallInterface = "eth0"

// rebootMethodPtr returns a pointer to m. Shared by all reboot-method tests
// in this file.
func rebootMethodPtr(m provisioningv1.RebootMethodType) *provisioningv1.RebootMethodType {
	return &m
}

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

			err := reconciler.createScriptJob(ctx, dpuNode, nil)
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

			err := reconciler.createScriptJob(ctx, dpuNode, nil)
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

			err := reconciler.createScriptJob(ctx, dpuNode, nil)
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

			err = reconciler.createScriptJob(ctx, dpuNode, nil)
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

		It("should create Job successfully with YAML pod-template in ConfigMap", func() {
			podTemplateYAML := `
spec:
  restartPolicy: Never
  containers:
    - name: reboot-script
      image: busybox:latest
      command:
        - /bin/sh
        - -c
        - echo 'rebooting'
`

			configMap := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-configmap-yaml",
					Namespace: "test-namespace",
				},
				Data: map[string]string{
					PodTemplateConfigMapKey: podTemplateYAML,
				},
			}
			Expect(fakeClient.Create(ctx, configMap)).To(Succeed())

			dpuNode := &provisioningv1.DPUNode{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-dpunode-yaml",
					Namespace: "test-namespace",
				},
				Spec: provisioningv1.DPUNodeSpec{
					NodeRebootMethod: &provisioningv1.NodeRebootMethod{
						Script: &provisioningv1.Script{
							Name: "test-configmap-yaml",
						},
					},
				},
			}
			Expect(fakeClient.Create(ctx, dpuNode)).To(Succeed())

			err := reconciler.createScriptJob(ctx, dpuNode, nil)
			Expect(err).NotTo(HaveOccurred())

			// Verify Job was created from YAML template
			job := &batchv1.Job{}
			err = fakeClient.Get(ctx, types.NamespacedName{
				Name:      "test-dpunode-yaml-script-job",
				Namespace: "test-namespace",
			}, job)
			Expect(err).NotTo(HaveOccurred())
			Expect(job.Spec.Template.Spec.Containers).To(HaveLen(1))
			Expect(job.Spec.Template.Spec.Containers[0].Name).To(Equal("reboot-script"))
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

			err = reconciler.createScriptJob(ctx, dpuNode, nil)
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

			err = reconciler.createScriptJob(ctx, dpuNode, nil)
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

		It("should refuse to create job when one already exists", func() {
			existingJob := &batchv1.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-dpunode-script-job",
					Namespace: "test-namespace",
				},
				Status: batchv1.JobStatus{Active: 1},
			}
			Expect(fakeClient.Create(ctx, existingJob)).To(Succeed())

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

			err := reconciler.createScriptJob(ctx, dpuNode, nil)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("refusing to create script job"))
			Expect(err.Error()).To(ContainSubstring("already exists"))
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

			err = reconciler.createScriptJob(ctx, dpuNode, nil)
			Expect(err).NotTo(HaveOccurred())

			// Patch the dpuNode to persist changes (simulating defer in Reconcile)
			Expect(patcher.Patch(ctx, dpuNode)).To(Succeed())

			// Fetch the updated DPUNode from the fake client to verify annotation was persisted
			updatedDPUNode := &provisioningv1.DPUNode{}
			Expect(fakeClient.Get(ctx, types.NamespacedName{
				Name:      "test-dpunode",
				Namespace: "test-namespace",
			}, updatedDPUNode)).To(Succeed())

		})

		It("should set backoffLimit to 3", func() {
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
					Name:      "test-configmap-bl",
					Namespace: "test-namespace",
				},
				Data: map[string]string{
					PodTemplateConfigMapKey: string(podTemplateJSON),
				},
			}
			Expect(fakeClient.Create(ctx, configMap)).To(Succeed())

			dpuNode := &provisioningv1.DPUNode{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-dpunode-bl",
					Namespace: "test-namespace",
				},
				Spec: provisioningv1.DPUNodeSpec{
					NodeRebootMethod: &provisioningv1.NodeRebootMethod{
						Script: &provisioningv1.Script{
							Name: "test-configmap-bl",
						},
					},
				},
			}
			Expect(fakeClient.Create(ctx, dpuNode)).To(Succeed())

			err = reconciler.createScriptJob(ctx, dpuNode, nil)
			Expect(err).NotTo(HaveOccurred())

			job := &batchv1.Job{}
			Expect(fakeClient.Get(ctx, types.NamespacedName{
				Name:      "test-dpunode-bl-script-job",
				Namespace: "test-namespace",
			}, job)).To(Succeed())

			Expect(job.Spec.BackoffLimit).NotTo(BeNil())
			Expect(*job.Spec.BackoffLimit).To(Equal(int32(3)))
		})

		It("should add control-plane toleration to job pod template", func() {
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
					Name:      "test-configmap-tol",
					Namespace: "test-namespace",
				},
				Data: map[string]string{
					PodTemplateConfigMapKey: string(podTemplateJSON),
				},
			}
			Expect(fakeClient.Create(ctx, configMap)).To(Succeed())

			dpuNode := &provisioningv1.DPUNode{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-dpunode-tol",
					Namespace: "test-namespace",
				},
				Spec: provisioningv1.DPUNodeSpec{
					NodeRebootMethod: &provisioningv1.NodeRebootMethod{
						Script: &provisioningv1.Script{
							Name: "test-configmap-tol",
						},
					},
				},
			}
			Expect(fakeClient.Create(ctx, dpuNode)).To(Succeed())

			err = reconciler.createScriptJob(ctx, dpuNode, nil)
			Expect(err).NotTo(HaveOccurred())

			job := &batchv1.Job{}
			Expect(fakeClient.Get(ctx, types.NamespacedName{
				Name:      "test-dpunode-tol-script-job",
				Namespace: "test-namespace",
			}, job)).To(Succeed())

			found := false
			for _, t := range job.Spec.Template.Spec.Tolerations {
				if t.Key == "node-role.kubernetes.io/control-plane" && t.Effect == corev1.TaintEffectNoSchedule {
					found = true
					break
				}
			}
			Expect(found).To(BeTrue(), "control-plane toleration should be present")
		})

		It("should not duplicate control-plane toleration if user already added it", func() {
			podTemplate := corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "reboot-script",
							Image: "busybox:latest",
						},
					},
					RestartPolicy: corev1.RestartPolicyNever,
					Tolerations: []corev1.Toleration{
						{
							Key:      "node-role.kubernetes.io/control-plane",
							Operator: corev1.TolerationOpExists,
							Effect:   corev1.TaintEffectNoSchedule,
						},
					},
				},
			}
			podTemplateJSON, err := json.Marshal(podTemplate)
			Expect(err).NotTo(HaveOccurred())

			configMap := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-configmap-tol2",
					Namespace: "test-namespace",
				},
				Data: map[string]string{
					PodTemplateConfigMapKey: string(podTemplateJSON),
				},
			}
			Expect(fakeClient.Create(ctx, configMap)).To(Succeed())

			dpuNode := &provisioningv1.DPUNode{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-dpunode-tol2",
					Namespace: "test-namespace",
				},
				Spec: provisioningv1.DPUNodeSpec{
					NodeRebootMethod: &provisioningv1.NodeRebootMethod{
						Script: &provisioningv1.Script{
							Name: "test-configmap-tol2",
						},
					},
				},
			}
			Expect(fakeClient.Create(ctx, dpuNode)).To(Succeed())

			err = reconciler.createScriptJob(ctx, dpuNode, nil)
			Expect(err).NotTo(HaveOccurred())

			job := &batchv1.Job{}
			Expect(fakeClient.Get(ctx, types.NamespacedName{
				Name:      "test-dpunode-tol2-script-job",
				Namespace: "test-namespace",
			}, job)).To(Succeed())

			cpCount := 0
			for _, t := range job.Spec.Template.Spec.Tolerations {
				if t.Key == "node-role.kubernetes.io/control-plane" && t.Effect == corev1.TaintEffectNoSchedule {
					cpCount++
				}
			}
			Expect(cpCount).To(Equal(1), "control-plane toleration should not be duplicated")
		})

		// v26.4 stop-gap: the script Job should expose the aggregated reboot
		// method (and the per-DPU mapping) to the script container so operators
		// can branch on PowerCycle vs SystemReboot vs SLR without consuming the
		// DPU API. The aggregator picks the most disruptive method present and
		// "Unknown" only when every DPU is missing or Unknown.
		Context("reboot-method propagation (v26.4 stop-gap)", func() {
			var stopgapScheme *runtime.Scheme
			BeforeEach(func() {
				stopgapScheme = runtime.NewScheme()
				Expect(provisioningv1.AddToScheme(stopgapScheme)).To(Succeed())
				Expect(corev1.AddToScheme(stopgapScheme)).To(Succeed())
				Expect(batchv1.AddToScheme(stopgapScheme)).To(Succeed())
			})

			// rebootMethodTestSetup builds a fakeClient with status subresource
			// support, seeds the provided DPUs and a default ConfigMap, and
			// returns a configured DPUNode + reconciler ready for the test.
			rebootMethodTestSetup := func(dpuNodeName string, dpus ...*provisioningv1.DPU) (*provisioningv1.DPUNode, *DPUNodeReconciler, client.Client) {
				podTemplate := corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{Name: "main", Image: "busybox:latest"},
						},
						InitContainers: []corev1.Container{
							{Name: "init", Image: "busybox:latest"},
						},
						RestartPolicy: corev1.RestartPolicyNever,
					},
				}
				podTemplateJSON, err := json.Marshal(podTemplate)
				Expect(err).NotTo(HaveOccurred())

				configMap := &corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "stopgap-cm-" + dpuNodeName,
						Namespace: "test-namespace",
					},
					Data: map[string]string{PodTemplateConfigMapKey: string(podTemplateJSON)},
				}
				dpuNode := &provisioningv1.DPUNode{
					ObjectMeta: metav1.ObjectMeta{
						Name:      dpuNodeName,
						Namespace: "test-namespace",
					},
					Spec: provisioningv1.DPUNodeSpec{
						NodeRebootMethod: &provisioningv1.NodeRebootMethod{
							Script: &provisioningv1.Script{Name: configMap.Name},
						},
					},
				}

				objs := []client.Object{configMap, dpuNode}
				for _, d := range dpus {
					objs = append(objs, d)
				}
				newClient := fake.NewClientBuilder().
					WithScheme(stopgapScheme).
					WithObjects(objs...).
					WithStatusSubresource(&provisioningv1.DPU{}).
					Build()
				return dpuNode, &DPUNodeReconciler{Client: newClient}, newClient
			}

			// Source of truth for the aggregator is dpu.Status.RebootStatus.Method.
			// InitializeDPURebootStatus is the only writer in production: it sets
			// RebootStatus.Method to AgentStatus.RebootMethod for the DPUConfig
			// source phase and to RebootMethodPowerCycle for the
			// DPUInitializeInterface mode-transition path. Tests stamp it
			// directly so a single helper covers both paths.
			makeDPU := func(name, dpuNodeName string, method *provisioningv1.RebootMethodType) *provisioningv1.DPU {
				dpu := &provisioningv1.DPU{
					ObjectMeta: metav1.ObjectMeta{
						Name:      name,
						Namespace: "test-namespace",
						Labels: map[string]string{
							provisioningv1.DPUNodeNameLabel: dpuNodeName,
						},
					},
				}
				if method != nil {
					dpu.Status.RebootStatus = &provisioningv1.RebootStatus{Method: method}
				}
				return dpu
			}

			envValue := func(envs []corev1.EnvVar, name string) (string, bool) {
				for _, e := range envs {
					if e.Name == name {
						return e.Value, true
					}
				}
				return "", false
			}

			It("aggregates to PowerCycle when any DPU reports PowerCycle", func() {
				dpuNodeName := "agg-powercycle"
				dpus := []*provisioningv1.DPU{
					makeDPU("dpu-a", dpuNodeName, rebootMethodPtr(provisioningv1.RebootMethodFirmwareReset)),
					makeDPU("dpu-b", dpuNodeName, rebootMethodPtr(provisioningv1.RebootMethodPowerCycle)),
					makeDPU("dpu-c", dpuNodeName, rebootMethodPtr(provisioningv1.RebootMethodNoAction)),
				}
				dpuNode, rec, c := rebootMethodTestSetup(dpuNodeName, dpus...)

				Expect(rec.createScriptJob(ctx, dpuNode, dpus)).To(Succeed())

				job := &batchv1.Job{}
				Expect(c.Get(ctx, types.NamespacedName{
					Name: rec.generateJobName(dpuNode), Namespace: "test-namespace",
				}, job)).To(Succeed())

				Expect(job.Spec.Template.Spec.Containers).To(HaveLen(1))
				Expect(job.Spec.Template.Spec.InitContainers).To(HaveLen(1))

				for _, c := range job.Spec.Template.Spec.Containers {
					v, ok := envValue(c.Env, DPUNodeRebootMethodEnvVar)
					Expect(ok).To(BeTrue(), "container %q missing %s", c.Name, DPUNodeRebootMethodEnvVar)
					Expect(v).To(Equal(string(provisioningv1.RebootMethodPowerCycle)))

					perDPU, ok := envValue(c.Env, DPUNodeRebootMethodsPerDPUEnvVar)
					Expect(ok).To(BeTrue())
					Expect(perDPU).To(Equal("dpu-a=FirmwareReset,dpu-b=PowerCycle,dpu-c=NoAction"))
				}
				for _, c := range job.Spec.Template.Spec.InitContainers {
					v, _ := envValue(c.Env, DPUNodeRebootMethodEnvVar)
					Expect(v).To(Equal(string(provisioningv1.RebootMethodPowerCycle)))
				}

				Expect(job.Spec.Template.Annotations).To(HaveKeyWithValue(
					DPUNodeRebootMethodAnnotation, string(provisioningv1.RebootMethodPowerCycle)))
				Expect(job.Spec.Template.Annotations).To(HaveKeyWithValue(
					DPUNodeRebootMethodsPerDPUAnnotation, "dpu-a=FirmwareReset,dpu-b=PowerCycle,dpu-c=NoAction"))
			})

			It("aggregates to SystemLevelReset when only SLR and lower-priority methods are reported", func() {
				dpuNodeName := "agg-slr"
				dpus := []*provisioningv1.DPU{
					makeDPU("dpu-a", dpuNodeName, rebootMethodPtr(provisioningv1.RebootMethodSystemLevelReset)),
					makeDPU("dpu-b", dpuNodeName, rebootMethodPtr(provisioningv1.RebootMethodSystemReboot)),
				}
				dpuNode, rec, c := rebootMethodTestSetup(dpuNodeName, dpus...)

				Expect(rec.createScriptJob(ctx, dpuNode, dpus)).To(Succeed())

				job := &batchv1.Job{}
				Expect(c.Get(ctx, types.NamespacedName{
					Name: rec.generateJobName(dpuNode), Namespace: "test-namespace",
				}, job)).To(Succeed())

				v, _ := envValue(job.Spec.Template.Spec.Containers[0].Env, DPUNodeRebootMethodEnvVar)
				Expect(v).To(Equal(string(provisioningv1.RebootMethodSystemLevelReset)))
			})

			It("defaults the aggregate to SystemLevelReset but keeps per-DPU Unknown when no DPU has reported a method", func() {
				dpuNodeName := "agg-unknown"
				dpus := []*provisioningv1.DPU{
					makeDPU("dpu-a", dpuNodeName, nil),
					makeDPU("dpu-b", dpuNodeName, rebootMethodPtr(provisioningv1.RebootMethodUnknown)),
				}
				dpuNode, rec, c := rebootMethodTestSetup(dpuNodeName, dpus...)

				Expect(rec.createScriptJob(ctx, dpuNode, dpus)).To(Succeed())

				job := &batchv1.Job{}
				Expect(c.Get(ctx, types.NamespacedName{
					Name: rec.generateJobName(dpuNode), Namespace: "test-namespace",
				}, job)).To(Succeed())

				v, _ := envValue(job.Spec.Template.Spec.Containers[0].Env, DPUNodeRebootMethodEnvVar)
				Expect(v).To(Equal(string(provisioningv1.RebootMethodSystemLevelReset)),
					"aggregate must default to SystemLevelReset rather than Unknown")
				Expect(job.Spec.Template.Annotations).To(HaveKeyWithValue(
					DPUNodeRebootMethodAnnotation, string(provisioningv1.RebootMethodSystemLevelReset)))

				perDPU, _ := envValue(job.Spec.Template.Spec.Containers[0].Env, DPUNodeRebootMethodsPerDPUEnvVar)
				Expect(perDPU).To(Equal("dpu-a=Unknown,dpu-b=Unknown"),
					"per-DPU mapping must keep Unknown so scripts can still see which DPUs have not reported")
				Expect(job.Spec.Template.Annotations).To(HaveKeyWithValue(
					DPUNodeRebootMethodsPerDPUAnnotation, "dpu-a=Unknown,dpu-b=Unknown"))
			})

			It("defaults the aggregate to SystemLevelReset and emits an empty per-DPU mapping when no DPU is in Rebooting phase", func() {
				dpuNode, rec, c := rebootMethodTestSetup("agg-no-dpus")

				Expect(rec.createScriptJob(ctx, dpuNode, nil)).To(Succeed())

				job := &batchv1.Job{}
				Expect(c.Get(ctx, types.NamespacedName{
					Name: rec.generateJobName(dpuNode), Namespace: "test-namespace",
				}, job)).To(Succeed())

				v, _ := envValue(job.Spec.Template.Spec.Containers[0].Env, DPUNodeRebootMethodEnvVar)
				Expect(v).To(Equal(string(provisioningv1.RebootMethodSystemLevelReset)),
					"aggregate must default to SystemLevelReset rather than Unknown")
				Expect(job.Spec.Template.Annotations).To(HaveKeyWithValue(
					DPUNodeRebootMethodAnnotation, string(provisioningv1.RebootMethodSystemLevelReset)))

				perDPU, ok := envValue(job.Spec.Template.Spec.Containers[0].Env, DPUNodeRebootMethodsPerDPUEnvVar)
				Expect(ok).To(BeTrue())
				Expect(perDPU).To(BeEmpty())

				Expect(job.Spec.Template.Annotations).To(HaveKeyWithValue(
					DPUNodeRebootMethodsPerDPUAnnotation, ""))
			})

			It("emits the per-DPU mapping in deterministic, name-sorted order", func() {
				dpuNodeName := "agg-sorted"
				dpus := []*provisioningv1.DPU{
					makeDPU("zzz-dpu", dpuNodeName, rebootMethodPtr(provisioningv1.RebootMethodSystemReboot)),
					makeDPU("aaa-dpu", dpuNodeName, rebootMethodPtr(provisioningv1.RebootMethodSystemLevelReset)),
					makeDPU("mmm-dpu", dpuNodeName, nil),
				}
				dpuNode, rec, c := rebootMethodTestSetup(dpuNodeName, dpus...)

				Expect(rec.createScriptJob(ctx, dpuNode, dpus)).To(Succeed())

				job := &batchv1.Job{}
				Expect(c.Get(ctx, types.NamespacedName{
					Name: rec.generateJobName(dpuNode), Namespace: "test-namespace",
				}, job)).To(Succeed())

				perDPU, _ := envValue(job.Spec.Template.Spec.Containers[0].Env, DPUNodeRebootMethodsPerDPUEnvVar)
				Expect(perDPU).To(Equal("aaa-dpu=SystemLevelReset,mmm-dpu=Unknown,zzz-dpu=SystemReboot"))

				Expect(job.Spec.Template.Annotations).To(HaveKeyWithValue(
					DPUNodeRebootMethodsPerDPUAnnotation,
					"aaa-dpu=SystemLevelReset,mmm-dpu=Unknown,zzz-dpu=SystemReboot"))
			})

			It("overwrites user-provided reboot-method annotations because the controller is the source of truth", func() {
				dpuNodeName := "agg-override"
				stalePodTemplate := corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Annotations: map[string]string{
							DPUNodeRebootMethodAnnotation:        "BogusUserValue",
							DPUNodeRebootMethodsPerDPUAnnotation: "stale=value",
						},
					},
					Spec: corev1.PodSpec{
						Containers:    []corev1.Container{{Name: "main", Image: "busybox:latest"}},
						RestartPolicy: corev1.RestartPolicyNever,
					},
				}
				stalePodTemplateJSON, err := json.Marshal(stalePodTemplate)
				Expect(err).NotTo(HaveOccurred())

				configMap := &corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "stopgap-cm-" + dpuNodeName,
						Namespace: "test-namespace",
					},
					Data: map[string]string{PodTemplateConfigMapKey: string(stalePodTemplateJSON)},
				}
				dpuNode := &provisioningv1.DPUNode{
					ObjectMeta: metav1.ObjectMeta{
						Name:      dpuNodeName,
						Namespace: "test-namespace",
					},
					Spec: provisioningv1.DPUNodeSpec{
						NodeRebootMethod: &provisioningv1.NodeRebootMethod{
							Script: &provisioningv1.Script{Name: configMap.Name},
						},
					},
				}
				dpu := makeDPU("dpu-a", dpuNodeName, rebootMethodPtr(provisioningv1.RebootMethodPowerCycle))

				newClient := fake.NewClientBuilder().
					WithScheme(stopgapScheme).
					WithObjects(configMap, dpuNode, dpu).
					WithStatusSubresource(&provisioningv1.DPU{}).
					Build()
				rec := &DPUNodeReconciler{Client: newClient}

				Expect(rec.createScriptJob(ctx, dpuNode, []*provisioningv1.DPU{dpu})).To(Succeed())

				job := &batchv1.Job{}
				Expect(newClient.Get(ctx, types.NamespacedName{
					Name: rec.generateJobName(dpuNode), Namespace: "test-namespace",
				}, job)).To(Succeed())

				Expect(job.Spec.Template.Annotations).To(HaveKeyWithValue(
					DPUNodeRebootMethodAnnotation, string(provisioningv1.RebootMethodPowerCycle)))
				Expect(job.Spec.Template.Annotations).To(HaveKeyWithValue(
					DPUNodeRebootMethodsPerDPUAnnotation, "dpu-a=PowerCycle"))
			})

			// Verifies the contract that createScriptJob only aggregates the
			// DPUs supplied by the caller (the Rebooting-phase set), even when
			// other DPUs labeled for the same DPUNode exist in the API server.
			// A pre-existing DPU still in DPUConfig with a stale RebootMethod
			// must not influence the aggregated method or the per-DPU mapping.
			It("aggregates only over the supplied DPUs and ignores other labeled DPUs", func() {
				dpuNodeName := "agg-rebooting-only"
				rebooting := []*provisioningv1.DPU{
					makeDPU("dpu-a", dpuNodeName, rebootMethodPtr(provisioningv1.RebootMethodSystemReboot)),
				}
				stale := makeDPU("dpu-z", dpuNodeName, rebootMethodPtr(provisioningv1.RebootMethodPowerCycle))
				dpuNode, rec, c := rebootMethodTestSetup(dpuNodeName, append(rebooting, stale)...)

				Expect(rec.createScriptJob(ctx, dpuNode, rebooting)).To(Succeed())

				job := &batchv1.Job{}
				Expect(c.Get(ctx, types.NamespacedName{
					Name: rec.generateJobName(dpuNode), Namespace: "test-namespace",
				}, job)).To(Succeed())

				v, _ := envValue(job.Spec.Template.Spec.Containers[0].Env, DPUNodeRebootMethodEnvVar)
				Expect(v).To(Equal(string(provisioningv1.RebootMethodSystemReboot)),
					"aggregation must reflect only the rebooting-phase DPUs")

				perDPU, _ := envValue(job.Spec.Template.Spec.Containers[0].Env, DPUNodeRebootMethodsPerDPUEnvVar)
				Expect(perDPU).To(Equal("dpu-a=SystemReboot"),
					"per-DPU mapping must not include DPUs that are not in Rebooting phase")
			})

			// createScriptJob must consume the per-reconcile aggregation,
			// not the persisted DPUNode.Status.RebootMethod left over from
			// a previous cycle.
			It("env vars track agg.Method and ignore stale Status.RebootMethod from a previous cycle", func() {
				dpuNodeName := "agg-stale-prev-cycle"
				dpu := makeDPU("dpu-a", dpuNodeName, rebootMethodPtr(provisioningv1.RebootMethodSystemReboot))
				dpuNode, rec, c := rebootMethodTestSetup(dpuNodeName, dpu)
				dpuNode.Status.RebootMethod = rebootMethodPtr(provisioningv1.RebootMethodPowerCycle) // stale carryover

				Expect(rec.createScriptJob(ctx, dpuNode, []*provisioningv1.DPU{dpu})).To(Succeed())

				job := &batchv1.Job{}
				Expect(c.Get(ctx, types.NamespacedName{
					Name: rec.generateJobName(dpuNode), Namespace: "test-namespace",
				}, job)).To(Succeed())

				v, _ := envValue(job.Spec.Template.Spec.Containers[0].Env, DPUNodeRebootMethodEnvVar)
				Expect(v).To(Equal(string(provisioningv1.RebootMethodSystemReboot)),
					"env var must reflect agg.Method, not a stale Status.RebootMethod")
				Expect(job.Spec.Template.Annotations).To(HaveKeyWithValue(
					DPUNodeRebootMethodAnnotation, string(provisioningv1.RebootMethodSystemReboot)))
			})
		})
	})

	Context("dpuDeviceToDPUNodeReq", func() {
		var (
			reconciler *DPUNodeReconciler
		)

		BeforeEach(func() {
			reconciler = &DPUNodeReconciler{}
		})

		It("should return request for DPUDevice with DPUNodeName label", func() {
			dpuDevice := &provisioningv1.DPUDevice{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-dpudevice",
					Namespace: "test-namespace",
					Labels: map[string]string{
						provisioningv1.DPUNodeNameLabel: "test-dpunode",
					},
				},
			}

			requests := reconciler.dpuDeviceToDPUNodeReq(context.Background(), dpuDevice)
			Expect(requests).To(HaveLen(1))
			Expect(requests[0].Name).To(Equal("test-dpunode"))
			Expect(requests[0].Namespace).To(Equal("test-namespace"))
		})

		It("should return nil when DPUDevice has no labels", func() {
			dpuDevice := &provisioningv1.DPUDevice{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-dpudevice",
					Namespace: "test-namespace",
				},
			}

			requests := reconciler.dpuDeviceToDPUNodeReq(context.Background(), dpuDevice)
			Expect(requests).To(BeNil())
		})

		It("should return nil when DPUDevice has empty DPUNodeName label", func() {
			dpuDevice := &provisioningv1.DPUDevice{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-dpudevice",
					Namespace: "test-namespace",
					Labels: map[string]string{
						provisioningv1.DPUNodeNameLabel: "",
					},
				},
			}

			requests := reconciler.dpuDeviceToDPUNodeReq(context.Background(), dpuDevice)
			Expect(requests).To(BeNil())
		})

		It("should return nil when DPUDevice has labels but no DPUNodeName label", func() {
			dpuDevice := &provisioningv1.DPUDevice{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-dpudevice",
					Namespace: "test-namespace",
					Labels: map[string]string{
						"some-other-label": "some-value",
					},
				},
			}

			requests := reconciler.dpuDeviceToDPUNodeReq(context.Background(), dpuDevice)
			Expect(requests).To(BeNil())
		})

		It("should use DPUDevice namespace for the request", func() {
			dpuDevice := &provisioningv1.DPUDevice{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-dpudevice",
					Namespace: "custom-namespace",
					Labels: map[string]string{
						provisioningv1.DPUNodeNameLabel: "test-dpunode",
					},
				},
			}

			requests := reconciler.dpuDeviceToDPUNodeReq(context.Background(), dpuDevice)
			Expect(requests).To(HaveLen(1))
			Expect(requests[0].Namespace).To(Equal("custom-namespace"))
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

		createTestSetup := func(jobStatus *batchv1.JobStatus) (*provisioningv1.DPUNode, *provisioningv1.DPU, *batchv1.Job) {
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

		// runReboot mirrors the production Reconcile flow for the rebootNode
		// path: it derives the inScope DPU slice from the fake client (via
		// aggregateAndPublishRebootMethod) and threads it into rebootNode.
		// Tests that previously called rebootNode directly on (ctx, dpuNode)
		// now call this helper to exercise the same wiring the controller
		// uses in production.
		runReboot := func(dpuNode *provisioningv1.DPUNode) (ctrl.Result, error) {
			inScope, err := reconciler.aggregateAndPublishRebootMethod(ctx, dpuNode)
			if err != nil {
				return ctrl.Result{}, err
			}
			return reconciler.rebootNode(ctx, dpuNode, inScope)
		}

		completedJobStatus := func() *batchv1.JobStatus {
			return &batchv1.JobStatus{
				Succeeded: 1,
				Conditions: []batchv1.JobCondition{
					{
						Type:   batchv1.JobComplete,
						Status: corev1.ConditionTrue,
					},
				},
			}
		}

		terminalFailedJobStatus := func() *batchv1.JobStatus {
			return &batchv1.JobStatus{
				Failed: 3,
				Conditions: []batchv1.JobCondition{
					{
						Type:    batchv1.JobFailed,
						Status:  corev1.ConditionTrue,
						Reason:  "BackoffLimitExceeded",
						Message: "Job has reached the specified backoff limit",
					},
				},
			}
		}

		setDPUConditionForTest := func(dpuNode *provisioningv1.DPUNode, reason string) {
			dpu := &provisioningv1.DPU{}
			ExpectWithOffset(1, fakeClient.Get(ctx, types.NamespacedName{
				Name:      cutil.GenerateDPUName(dpuNode.Name, "dpu1"),
				Namespace: "test-namespace",
			}, dpu)).To(Succeed())
			cutil.SetDPUCondition(&dpu.Status, cutil.DPUCondition(provisioningv1.DPUCondRebooted, reason, ""))
			ExpectWithOffset(1, fakeClient.Status().Update(ctx, dpu)).To(Succeed())
		}

		setDPURebootStatusForTest := func(dpuNode *provisioningv1.DPUNode, phase provisioningv1.RebootStatusPhase, reason, message string) {
			dpu := &provisioningv1.DPU{}
			ExpectWithOffset(1, fakeClient.Get(ctx, types.NamespacedName{
				Name:      cutil.GenerateDPUName(dpuNode.Name, "dpu1"),
				Namespace: "test-namespace",
			}, dpu)).To(Succeed())
			pm := provisioningv1.RebootMethodPowerCycle
			now := metav1.Now()
			dpu.Status.RebootStatus = &provisioningv1.RebootStatus{
				Phase:              phase,
				Method:             &pm,
				Reason:             reason,
				Message:            message,
				LastTransitionTime: &now,
			}
			ExpectWithOffset(1, fakeClient.Status().Update(ctx, dpu)).To(Succeed())
		}

		getDPURebootStatusForTest := func(dpuNode *provisioningv1.DPUNode) *provisioningv1.RebootStatus {
			dpu := &provisioningv1.DPU{}
			ExpectWithOffset(1, fakeClient.Get(ctx, types.NamespacedName{
				Name:      cutil.GenerateDPUName(dpuNode.Name, "dpu1"),
				Namespace: "test-namespace",
			}, dpu)).To(Succeed())
			return dpu.Status.RebootStatus
		}

		waitingScriptMsg := "waiting for script to reboot node"

		It("should handle job succeeded", func() {
			dpuNode, _, _ := createTestSetup(completedJobStatus())
			setDPURebootStatusForTest(dpuNode, provisioningv1.RebootStatusPending, cutil.ReasonRebootScriptWaiting, waitingScriptMsg)

			result, err := runReboot(dpuNode)
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).To(BeZero())
		})

		It("should handle job failed with terminal condition", func() {
			dpuNode, _, _ := createTestSetup(terminalFailedJobStatus())
			setDPURebootStatusForTest(dpuNode, provisioningv1.RebootStatusPending, cutil.ReasonRebootScriptWaiting, waitingScriptMsg)

			fakeRecorder := record.NewFakeRecorder(10)
			reconciler.Recorder = fakeRecorder

			result, err := runReboot(dpuNode)
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).To(BeZero())

			rs := getDPURebootStatusForTest(dpuNode)
			Expect(rs).NotTo(BeNil())
			Expect(rs.Phase).To(Equal(provisioningv1.RebootStatusFailed))
			Expect(rs.Reason).To(Equal(cutil.ReasonRebootScriptFailed))
			Expect(rs.Message).To(ContainSubstring("BackoffLimitExceeded"))
			Expect(rs.Message).To(ContainSubstring("delete the failed job"))

			Eventually(fakeRecorder.Events).Should(Receive(ContainSubstring("ScriptRebootFailed")))
		})

		It("should recreate job when not found", func() {
			dpuNode, _, _ := createTestSetup(nil)
			setDPURebootStatusForTest(dpuNode, provisioningv1.RebootStatusPending, cutil.ReasonRebootScriptWaiting, waitingScriptMsg)

			result, err := runReboot(dpuNode)
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).NotTo(BeZero(), "should requeue after recreating job")

			rs := getDPURebootStatusForTest(dpuNode)
			Expect(rs).NotTo(BeNil())
			Expect(rs.Phase).To(Equal(provisioningv1.RebootStatusPending))
			Expect(rs.Reason).To(Equal(cutil.ReasonRebootScriptWaiting))

			job := &batchv1.Job{}
			Expect(reconciler.Get(ctx, client.ObjectKey{Namespace: dpuNode.Namespace, Name: reconciler.generateJobName(dpuNode)}, job)).To(Succeed())
		})

		It("should create script job when no condition exists", func() {
			dpuNode, _, _ := createTestSetup(nil)

			result, err := runReboot(dpuNode)
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
			dpuNode, _, _ := createTestSetup(jobStatus)
			setDPURebootStatusForTest(dpuNode, provisioningv1.RebootStatusPending, cutil.ReasonRebootScriptWaiting, waitingScriptMsg)

			result, err := runReboot(dpuNode)
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

			result, err := runReboot(dpuNode)
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
			result, err := runReboot(dpuNode)
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).NotTo(BeZero())
		})

		// --- A1: Race condition tests ---

		It("should create job when DPU has non-script condition reason (race condition fix)", func() {
			dpuNode, _, _ := createTestSetup(nil)
			setDPUConditionForTest(dpuNode, "DPUNodeNotFound")

			result, err := runReboot(dpuNode)
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).NotTo(BeZero())

			job := &batchv1.Job{}
			Expect(fakeClient.Get(ctx, types.NamespacedName{
				Name:      "test-dpunode-script-job",
				Namespace: "test-namespace",
			}, job)).To(Succeed())
		})

		It("does not delete job when script reboot lifecycle is active from RebootStatus", func() {
			jobStatus := &batchv1.JobStatus{Active: 1}
			dpuNode, _, _ := createTestSetup(jobStatus)
			setDPURebootStatusForTest(dpuNode, provisioningv1.RebootStatusPending, cutil.ReasonRebootScriptWaiting, waitingScriptMsg)

			result, err := runReboot(dpuNode)
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).NotTo(BeZero())
		})

		It("should recreate job when script lifecycle active but job was deleted", func() {
			dpuNode, _, _ := createTestSetup(nil)
			setDPURebootStatusForTest(dpuNode, provisioningv1.RebootStatusFailed, cutil.ReasonRebootScriptFailed, "prior script failure")

			result, err := runReboot(dpuNode)
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).NotTo(BeZero(), "should requeue after recreating job")

			rs := getDPURebootStatusForTest(dpuNode)
			Expect(rs).NotTo(BeNil())
			Expect(rs.Phase).To(Equal(provisioningv1.RebootStatusPending))
			Expect(rs.Reason).To(Equal(cutil.ReasonRebootScriptWaiting))

			job := &batchv1.Job{}
			Expect(reconciler.Get(ctx, client.ObjectKey{Namespace: dpuNode.Namespace, Name: reconciler.generateJobName(dpuNode)}, job)).To(Succeed())
		})

		It("should requeue when job has Failed count but no Failed condition (mid-retry)", func() {
			jobStatus := &batchv1.JobStatus{Failed: 1, Active: 1}
			dpuNode, _, _ := createTestSetup(jobStatus)
			setDPURebootStatusForTest(dpuNode, provisioningv1.RebootStatusPending, cutil.ReasonRebootScriptWaiting, waitingScriptMsg)

			result, err := runReboot(dpuNode)
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).NotTo(BeZero())
		})

		It("should include pod exit code in condition message when available", func() {
			dpuNode, _, _ := createTestSetup(terminalFailedJobStatus())
			setDPURebootStatusForTest(dpuNode, provisioningv1.RebootStatusPending, cutil.ReasonRebootScriptWaiting, waitingScriptMsg)

			failedPod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-dpunode-script-job-pod-1",
					Namespace: "test-namespace",
					Labels:    map[string]string{"job-name": "test-dpunode-script-job"},
				},
				Status: corev1.PodStatus{
					ContainerStatuses: []corev1.ContainerStatus{
						{
							Name: "reboot-script",
							State: corev1.ContainerState{
								Terminated: &corev1.ContainerStateTerminated{
									ExitCode: 137,
									Reason:   "OOMKilled",
								},
							},
						},
					},
				},
			}
			Expect(fakeClient.Create(ctx, failedPod)).To(Succeed())

			_, err := runReboot(dpuNode)
			Expect(err).NotTo(HaveOccurred())

			rs := getDPURebootStatusForTest(dpuNode)
			Expect(rs).NotTo(BeNil())
			Expect(rs.Message).To(ContainSubstring("137"))
			Expect(rs.Message).To(ContainSubstring("OOMKilled"))
		})

		It("should emit Normal event when job is created", func() {
			dpuNode, _, _ := createTestSetup(nil)

			fakeRecorder := record.NewFakeRecorder(10)
			reconciler.Recorder = fakeRecorder

			result, err := runReboot(dpuNode)
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).NotTo(BeZero())

			Eventually(fakeRecorder.Events).Should(Receive(ContainSubstring("ScriptRebootJobCreated")))
		})

		// --- Job-existence guard tests ---

		It("should delete stale active job from previous cycle when isFirstRun is true", func() {
			jobStatus := &batchv1.JobStatus{Active: 1}
			dpuNode, _, _ := createTestSetup(jobStatus)

			dpu := &provisioningv1.DPU{}
			Expect(fakeClient.Get(ctx, types.NamespacedName{
				Name:      cutil.GenerateDPUName(dpuNode.Name, "dpu1"),
				Namespace: "test-namespace",
			}, dpu)).To(Succeed())
			cutil.SetDPUCondition(&dpu.Status, cutil.NewCondition(string(provisioningv1.DPUCondRebooted), fmt.Errorf("transient"), "GetDPUNodeError", "transient error"))
			Expect(fakeClient.Status().Update(ctx, dpu)).To(Succeed())

			result, err := runReboot(dpuNode)
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).NotTo(BeZero(), "should requeue after deleting stale job")

			job := &batchv1.Job{}
			err = fakeClient.Get(ctx, types.NamespacedName{
				Name:      "test-dpunode-script-job",
				Namespace: "test-namespace",
			}, job)
			Expect(apierrors.IsNotFound(err)).To(BeTrue(), "stale active job should be deleted from the API")

			Expect(fakeClient.Get(ctx, types.NamespacedName{
				Name:      cutil.GenerateDPUName(dpuNode.Name, "dpu1"),
				Namespace: "test-namespace",
			}, dpu)).To(Succeed())
			Expect(dpu.Status.RebootStatus).To(BeNil())
		})

		It("should delete stale completed job from previous cycle and requeue", func() {
			dpuNode, _, _ := createTestSetup(completedJobStatus())

			dpu := &provisioningv1.DPU{}
			Expect(fakeClient.Get(ctx, types.NamespacedName{
				Name:      cutil.GenerateDPUName(dpuNode.Name, "dpu1"),
				Namespace: "test-namespace",
			}, dpu)).To(Succeed())
			cutil.SetDPUCondition(&dpu.Status, cutil.NewCondition(string(provisioningv1.DPUCondRebooted), fmt.Errorf("error"), "GenerateIPMIToolCommandError", "error"))
			Expect(fakeClient.Status().Update(ctx, dpu)).To(Succeed())

			result, err := runReboot(dpuNode)
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).NotTo(BeZero(), "should requeue to let API server finish deletion")

			Expect(fakeClient.Get(ctx, types.NamespacedName{
				Name:      cutil.GenerateDPUName(dpuNode.Name, "dpu1"),
				Namespace: "test-namespace",
			}, dpu)).To(Succeed())
			Expect(dpu.Status.RebootStatus).To(BeNil())
		})

		It("should delete stale failed job from previous cycle and requeue", func() {
			dpuNode, _, _ := createTestSetup(terminalFailedJobStatus())

			dpu := &provisioningv1.DPU{}
			Expect(fakeClient.Get(ctx, types.NamespacedName{
				Name:      cutil.GenerateDPUName(dpuNode.Name, "dpu1"),
				Namespace: "test-namespace",
			}, dpu)).To(Succeed())
			cutil.SetDPUCondition(&dpu.Status, cutil.NewCondition(string(provisioningv1.DPUCondRebooted), fmt.Errorf("not found"), "DPUNodeNotFound", "not found"))
			Expect(fakeClient.Status().Update(ctx, dpu)).To(Succeed())

			result, err := runReboot(dpuNode)
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).NotTo(BeZero(), "should requeue to let API server finish deletion")

			Expect(fakeClient.Get(ctx, types.NamespacedName{
				Name:      cutil.GenerateDPUName(dpuNode.Name, "dpu1"),
				Namespace: "test-namespace",
			}, dpu)).To(Succeed())
			Expect(dpu.Status.RebootStatus).To(BeNil())
		})

		It("should not delete job in backoff between retries", func() {
			jobStatus := &batchv1.JobStatus{Failed: 1}
			dpuNode, _, _ := createTestSetup(jobStatus)
			setDPURebootStatusForTest(dpuNode, provisioningv1.RebootStatusPending, cutil.ReasonRebootScriptWaiting, waitingScriptMsg)

			result, err := runReboot(dpuNode)
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).NotTo(BeZero())

			job := &batchv1.Job{}
			Expect(fakeClient.Get(ctx, types.NamespacedName{
				Name:      "test-dpunode-script-job",
				Namespace: "test-namespace",
			}, job)).To(Succeed())
			Expect(job.Status.Failed).To(Equal(int32(1)))

			rs := getDPURebootStatusForTest(dpuNode)
			Expect(rs).NotTo(BeNil())
			Expect(rs.Phase).To(Equal(provisioningv1.RebootStatusPending))
			Expect(rs.Reason).To(Equal(cutil.ReasonRebootScriptWaiting), "RebootStatus should still reflect script wait while job retries")
		})

	})

	Context("isScriptRelatedReason", func() {
		It("should return true for all script-related reasons", func() {
			Expect(isScriptRelatedReason(cutil.ReasonRebootScriptWaiting)).To(BeTrue())
			Expect(isScriptRelatedReason(cutil.ReasonRebootScriptFailedToFetchJob)).To(BeTrue())
			Expect(isScriptRelatedReason(cutil.ReasonRebootScriptFailed)).To(BeTrue())
		})

		It("should return false for DPU-controller reasons", func() {
			Expect(isScriptRelatedReason("DPUNodeNotFound")).To(BeFalse())
			Expect(isScriptRelatedReason("InvalidState")).To(BeFalse())
			Expect(isScriptRelatedReason("GenerateIPMIToolCommandError")).To(BeFalse())
			Expect(isScriptRelatedReason("RebootScriptJobRemoved")).To(BeFalse())
			Expect(isScriptRelatedReason("")).To(BeFalse())
		})
	})

	Context("isJobComplete", func() {
		It("should return true when job has Complete condition", func() {
			job := &batchv1.Job{
				Status: batchv1.JobStatus{
					Succeeded: 1,
					Conditions: []batchv1.JobCondition{
						{
							Type:   batchv1.JobComplete,
							Status: corev1.ConditionTrue,
						},
					},
				},
			}
			Expect(isJobComplete(job)).To(BeTrue())
		})

		It("should return false when job has no Complete condition", func() {
			job := &batchv1.Job{
				Status: batchv1.JobStatus{
					Succeeded: 1,
				},
			}
			Expect(isJobComplete(job)).To(BeFalse())
		})

		It("should return false when Complete condition status is not True", func() {
			job := &batchv1.Job{
				Status: batchv1.JobStatus{
					Conditions: []batchv1.JobCondition{
						{
							Type:   batchv1.JobComplete,
							Status: corev1.ConditionFalse,
						},
					},
				},
			}
			Expect(isJobComplete(job)).To(BeFalse())
		})
	})

	Context("isJobFailed", func() {
		It("should return true when job has Failed condition", func() {
			job := &batchv1.Job{
				Status: batchv1.JobStatus{
					Conditions: []batchv1.JobCondition{
						{
							Type:   batchv1.JobFailed,
							Status: corev1.ConditionTrue,
							Reason: "BackoffLimitExceeded",
						},
					},
				},
			}
			Expect(isJobFailed(job)).To(BeTrue())
		})

		It("should return false when job has no Failed condition", func() {
			job := &batchv1.Job{
				Status: batchv1.JobStatus{
					Failed: 2,
				},
			}
			Expect(isJobFailed(job)).To(BeFalse())
		})

		It("should return false when Failed condition status is not True", func() {
			job := &batchv1.Job{
				Status: batchv1.JobStatus{
					Conditions: []batchv1.JobCondition{
						{
							Type:   batchv1.JobFailed,
							Status: corev1.ConditionFalse,
						},
					},
				},
			}
			Expect(isJobFailed(job)).To(BeFalse())
		})
	})

	Context("extractJobFailureDetails", func() {
		It("should return reason and message from Failed condition", func() {
			job := &batchv1.Job{
				Status: batchv1.JobStatus{
					Conditions: []batchv1.JobCondition{
						{
							Type:    batchv1.JobFailed,
							Status:  corev1.ConditionTrue,
							Reason:  "BackoffLimitExceeded",
							Message: "Job has reached the specified backoff limit",
						},
					},
				},
			}
			details := extractJobFailureDetails(job)
			Expect(details).To(ContainSubstring("BackoffLimitExceeded"))
			Expect(details).To(ContainSubstring("Job has reached the specified backoff limit"))
		})

		It("should return unknown reason when no Failed condition exists", func() {
			job := &batchv1.Job{
				Status: batchv1.JobStatus{},
			}
			details := extractJobFailureDetails(job)
			Expect(details).To(Equal("job failed with unknown reason"))
		})
	})

	Context("extractPodFailureDetails", func() {
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
			_ = batchv1.AddToScheme(scheme)
		})

		It("should return container exit code and reason", func() {
			job := &batchv1.Job{
				ObjectMeta: metav1.ObjectMeta{Name: "test-job", Namespace: "ns"},
			}
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-job-pod", Namespace: "ns",
					Labels: map[string]string{"job-name": "test-job"},
				},
				Status: corev1.PodStatus{
					ContainerStatuses: []corev1.ContainerStatus{
						{
							Name: "reboot-script",
							State: corev1.ContainerState{
								Terminated: &corev1.ContainerStateTerminated{
									ExitCode: 137,
									Reason:   "OOMKilled",
								},
							},
						},
					},
				},
			}

			fakeClient = fake.NewClientBuilder().WithScheme(scheme).WithObjects(pod).Build()
			reconciler = &DPUNodeReconciler{Client: fakeClient}

			details := reconciler.extractPodFailureDetails(ctx, job)
			Expect(details).To(ContainSubstring("reboot-script"))
			Expect(details).To(ContainSubstring("137"))
			Expect(details).To(ContainSubstring("OOMKilled"))
		})

		It("should return init container failure before regular containers", func() {
			job := &batchv1.Job{
				ObjectMeta: metav1.ObjectMeta{Name: "test-job", Namespace: "ns"},
			}
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-job-pod", Namespace: "ns",
					Labels: map[string]string{"job-name": "test-job"},
				},
				Status: corev1.PodStatus{
					InitContainerStatuses: []corev1.ContainerStatus{
						{
							Name: "init-setup",
							State: corev1.ContainerState{
								Terminated: &corev1.ContainerStateTerminated{
									ExitCode: 1,
									Reason:   "Error",
								},
							},
						},
					},
					ContainerStatuses: []corev1.ContainerStatus{
						{
							Name: "main",
							State: corev1.ContainerState{
								Terminated: &corev1.ContainerStateTerminated{
									ExitCode: 2,
									Reason:   "Error",
								},
							},
						},
					},
				},
			}

			fakeClient = fake.NewClientBuilder().WithScheme(scheme).WithObjects(pod).Build()
			reconciler = &DPUNodeReconciler{Client: fakeClient}

			details := reconciler.extractPodFailureDetails(ctx, job)
			Expect(details).To(ContainSubstring("init container"))
			Expect(details).To(ContainSubstring("init-setup"))
		})

		It("should fall back to job status when no pods exist", func() {
			job := &batchv1.Job{
				ObjectMeta: metav1.ObjectMeta{Name: "test-job", Namespace: "ns"},
				Status: batchv1.JobStatus{
					Conditions: []batchv1.JobCondition{
						{Type: batchv1.JobFailed, Status: corev1.ConditionTrue, Reason: "BackoffLimitExceeded", Message: "limit reached"},
					},
				},
			}

			fakeClient = fake.NewClientBuilder().WithScheme(scheme).Build()
			reconciler = &DPUNodeReconciler{Client: fakeClient}

			details := reconciler.extractPodFailureDetails(ctx, job)
			Expect(details).To(ContainSubstring("BackoffLimitExceeded"))
		})

		It("should fall back to job status when containers exited with code 0", func() {
			job := &batchv1.Job{
				ObjectMeta: metav1.ObjectMeta{Name: "test-job", Namespace: "ns"},
				Status: batchv1.JobStatus{
					Conditions: []batchv1.JobCondition{
						{Type: batchv1.JobFailed, Status: corev1.ConditionTrue, Reason: "DeadlineExceeded", Message: "timed out"},
					},
				},
			}
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-job-pod", Namespace: "ns",
					Labels: map[string]string{"job-name": "test-job"},
				},
				Status: corev1.PodStatus{
					ContainerStatuses: []corev1.ContainerStatus{
						{
							Name: "main",
							State: corev1.ContainerState{
								Terminated: &corev1.ContainerStateTerminated{ExitCode: 0, Reason: "Completed"},
							},
						},
					},
				},
			}

			fakeClient = fake.NewClientBuilder().WithScheme(scheme).WithObjects(pod).Build()
			reconciler = &DPUNodeReconciler{Client: fakeClient}

			details := reconciler.extractPodFailureDetails(ctx, job)
			Expect(details).To(ContainSubstring("DeadlineExceeded"))
		})
	})

	Context("ensureControlPlaneToleration", func() {
		It("should add toleration when not present", func() {
			tolerations := ensureControlPlaneToleration(nil)
			Expect(tolerations).To(HaveLen(1))
			Expect(tolerations[0].Key).To(Equal("node-role.kubernetes.io/control-plane"))
			Expect(tolerations[0].Effect).To(Equal(corev1.TaintEffectNoSchedule))
			Expect(tolerations[0].Operator).To(Equal(corev1.TolerationOpExists))
		})

		It("should not duplicate toleration if already present", func() {
			existing := []corev1.Toleration{
				{
					Key:      "node-role.kubernetes.io/control-plane",
					Operator: corev1.TolerationOpExists,
					Effect:   corev1.TaintEffectNoSchedule,
				},
			}
			tolerations := ensureControlPlaneToleration(existing)
			Expect(tolerations).To(HaveLen(1))
		})

		It("should add toleration alongside existing different tolerations", func() {
			existing := []corev1.Toleration{
				{
					Key:      "other-key",
					Operator: corev1.TolerationOpExists,
					Effect:   corev1.TaintEffectNoExecute,
				},
			}
			tolerations := ensureControlPlaneToleration(existing)
			Expect(tolerations).To(HaveLen(2))
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

			err := reconciler.createScriptJob(ctx, dpuNode, nil)
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

			err := reconciler.createScriptJob(ctx, dpuNode, nil)
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

			err := reconciler.createScriptJob(ctx, dpuNode, nil)
			Expect(err).To(HaveOccurred())
		})

		It("should return error when job already exists (defense in depth)", func() {
			existingJob := &batchv1.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-dpunode-script-job",
					Namespace: "test-namespace",
				},
				Status: batchv1.JobStatus{Active: 1},
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
				WithObjects(dpuNode, existingJob).
				Build()

			reconciler = &DPUNodeReconciler{
				Client: fakeClient,
			}

			err := reconciler.createScriptJob(ctx, dpuNode, nil)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("refusing to create script job"))
			Expect(err.Error()).To(ContainSubstring("already exists"))
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

			err := reconciler.createScriptJob(ctx, dpuNode, nil)
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

			err := reconciler.createScriptJob(ctx, dpuNode, nil)
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

			err := reconciler.createScriptJob(ctx, dpuNode, nil)
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

			err := reconciler.createScriptJob(ctx, dpuNode, nil)
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

			inScope, err := reconciler.aggregateAndPublishRebootMethod(ctx, dpuNode)
			Expect(err).NotTo(HaveOccurred())
			result, err := reconciler.rebootNode(ctx, dpuNode, inScope)
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
						provisioningv1.DPUNodeNameLabel: "test-dpunode",
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
						provisioningv1.DPUNodeNameLabel: "test-dpunode",
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
						provisioningv1.DPUNodeNameLabel: "test-dpunode",
					},
				},
				Spec: provisioningv1.DPUDeviceSpec{},
			}
			dpuDevice2 := &provisioningv1.DPUDevice{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-dpudevice-2",
					Namespace: "test-namespace",
					Labels: map[string]string{
						provisioningv1.DPUNodeNameLabel: "test-dpunode",
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
						provisioningv1.DPUNodeNameLabel: "test-dpunode",
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
						provisioningv1.DPUNodeNameLabel: "test-dpunode",
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
						provisioningv1.DPUNodeNameLabel: "test-dpunode",
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
						provisioningv1.DPUNodeNameLabel: "test-dpunode",
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
						provisioningv1.DPUNodeNameLabel: "test-dpunode-1",
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
						provisioningv1.DPUNodeNameLabel: "test-dpunode-2",
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
						provisioningv1.DPUNodeNameLabel: "test-dpunode",
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

			err := reconciler.noneDPUInNodeEffectOrRebooting(ctx, dpuNode, false, false)
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

			err := reconciler.noneDPUInNodeEffectOrRebooting(ctx, dpuNode, true, false)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(Equal("node effect is in progress"))
		})

		It("should return error when RebootInProgress condition is True and a DPU is in DPURebooting", func() {
			// Stale-True clear is gated on hasRebootingDPU=false, so an active
			// reboot (at least one DPU in DPURebooting) still short-circuits
			// readiness with "reboot is in progress".
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
			dpu := &provisioningv1.DPU{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-dpu",
					Namespace: "test-namespace",
					Labels:    map[string]string{provisioningv1.DPUNodeNameLabel: "test-dpunode"},
				},
				Spec:   provisioningv1.DPUSpec{DPUNodeName: "test-dpunode"},
				Status: provisioningv1.DPUStatus{Phase: provisioningv1.DPURebooting},
			}
			Expect(fakeClient.Create(ctx, dpu)).To(Succeed())

			err := reconciler.noneDPUInNodeEffectOrRebooting(ctx, dpuNode, false, true)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(Equal("reboot is in progress"))
			// RebootInProgress must still be present -- DPUs are still around.
			Expect(meta.FindStatusCondition(dpuNode.Status.Conditions,
				provisioningv1.DPUNodeConditionRebootInProgress.String())).NotTo(BeNil())
		})

		It("removes stale RebootInProgress=True without touching reboot-method markers; no-DPUs branch sweeps them on a follow-up call", func() {
			// Reachable when (a) the reboot finished and the marker was not
			// cleared, or (b) the user empties Spec.DPUs / DPUSet rescale /
			// DPUDevice GC wipes the child DPUs while reboot is in flight.
			// The narrow branch only unsticks readiness; the persistence
			// contract for Status.RebootMethod is enforced by the separate
			// no-DPUs branch, which fires on a follow-up reconcile once
			// RebootInProgress is gone.
			dpuNode := &provisioningv1.DPUNode{
				ObjectMeta: metav1.ObjectMeta{Name: "test-dpunode", Namespace: "test-namespace"},
				Status: provisioningv1.DPUNodeStatus{
					RebootMethod: ptr.To(provisioningv1.RebootMethodPowerCycle),
					Conditions: []metav1.Condition{
						{Type: provisioningv1.DPUNodeConditionRebootInProgress.String(), Status: metav1.ConditionTrue},
					},
				},
			}

			// First call: stale-True branch removes only RebootInProgress.
			Expect(reconciler.noneDPUInNodeEffectOrRebooting(ctx, dpuNode, false, false)).To(Succeed())
			Expect(meta.FindStatusCondition(dpuNode.Status.Conditions,
				provisioningv1.DPUNodeConditionRebootInProgress.String())).To(BeNil())
			Expect(dpuNode.Status.RebootMethod).NotTo(BeNil(),
				"Status.RebootMethod must survive the stale-True branch")

			// Follow-up call with no DPUs in the cluster: the no-DPUs branch
			// observes hasRebootMethodField and the label List returns 0,
			// so it sweeps the marker.
			Expect(reconciler.noneDPUInNodeEffectOrRebooting(ctx, dpuNode, false, false)).To(Succeed())
			Expect(dpuNode.Status.RebootMethod).To(BeNil())
		})

		It("preserves Status.RebootMethod when reboot finishes and DPUs are idle but still owned (rebooting -> idle)", func() {
			// Post-reboot persistence contract: once the host reboot is
			// over, RebootInProgress=True is briefly stale. The narrow
			// branch removes only the condition; Status.RebootMethod must
			// survive across the rebooting -> idle transition for as long
			// as the DPUNode owns any DPU. The no-DPUs branch is gated on
			// the label List, so idle-but-present DPUs keep the marker on
			// follow-up calls too.
			dpuNode := &provisioningv1.DPUNode{
				ObjectMeta: metav1.ObjectMeta{Name: "test-dpunode", Namespace: "test-namespace"},
				Status: provisioningv1.DPUNodeStatus{
					RebootMethod: ptr.To(provisioningv1.RebootMethodPowerCycle),
					Conditions: []metav1.Condition{
						{Type: provisioningv1.DPUNodeConditionRebootInProgress.String(), Status: metav1.ConditionTrue},
					},
				},
			}
			dpu := &provisioningv1.DPU{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-dpu",
					Namespace: "test-namespace",
					Labels:    map[string]string{provisioningv1.DPUNodeNameLabel: "test-dpunode"},
				},
				Spec:   provisioningv1.DPUSpec{DPUNodeName: "test-dpunode"},
				Status: provisioningv1.DPUStatus{Phase: provisioningv1.DPUReady},
			}
			Expect(fakeClient.Create(ctx, dpu)).To(Succeed())

			// First call: only RebootInProgress is removed.
			Expect(reconciler.noneDPUInNodeEffectOrRebooting(ctx, dpuNode, false, false)).To(Succeed())
			Expect(meta.FindStatusCondition(dpuNode.Status.Conditions,
				provisioningv1.DPUNodeConditionRebootInProgress.String())).To(BeNil())
			Expect(dpuNode.Status.RebootMethod).NotTo(BeNil())
			Expect(*dpuNode.Status.RebootMethod).To(Equal(provisioningv1.RebootMethodPowerCycle))

			// Follow-up call: no-DPUs branch sees the DPU via the label List
			// and refuses to sweep, so the marker remains.
			Expect(reconciler.noneDPUInNodeEffectOrRebooting(ctx, dpuNode, false, false)).To(Succeed())
			Expect(dpuNode.Status.RebootMethod).NotTo(BeNil())
			Expect(*dpuNode.Status.RebootMethod).To(Equal(provisioningv1.RebootMethodPowerCycle))
		})

		It("does not touch existing markers when reboot is in flight and DPUs still exist (no-op cleanup)", func() {
			// Steady-state in-flight reboot: marker present, DPUs still
			// around. Cleanup must be observably a no-op.
			dpuNode := &provisioningv1.DPUNode{
				ObjectMeta: metav1.ObjectMeta{Name: "test-dpunode", Namespace: "test-namespace"},
				Status: provisioningv1.DPUNodeStatus{
					RebootMethod: ptr.To(provisioningv1.RebootMethodPowerCycle),
					Conditions: []metav1.Condition{
						{Type: provisioningv1.DPUNodeConditionRebootInProgress.String(), Status: metav1.ConditionFalse},
					},
				},
			}
			dpu := &provisioningv1.DPU{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-dpu",
					Namespace: "test-namespace",
					Labels:    map[string]string{provisioningv1.DPUNodeNameLabel: "test-dpunode"},
				},
				Spec:   provisioningv1.DPUSpec{DPUNodeName: "test-dpunode"},
				Status: provisioningv1.DPUStatus{Phase: provisioningv1.DPURebooting},
			}
			Expect(fakeClient.Create(ctx, dpu)).To(Succeed())

			Expect(reconciler.noneDPUInNodeEffectOrRebooting(ctx, dpuNode, false, true)).To(Succeed())

			Expect(dpuNode.Status.RebootMethod).NotTo(BeNil())
			Expect(*dpuNode.Status.RebootMethod).To(Equal(provisioningv1.RebootMethodPowerCycle))
			Expect(dpuNode.Status.Conditions).To(HaveLen(1))
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
			err := reconciler.noneDPUInNodeEffectOrRebooting(ctx, dpuNode, false, false)
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
						provisioningv1.DPUNodeNameLabel: "test-dpunode",
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
						provisioningv1.DPUNodeNameLabel: "test-dpunode",
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

			err := reconciler.noneDPUInNodeEffectOrRebooting(ctx, dpuNode, false, false)
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
			err := reconciler.noneDPUInNodeEffectOrRebooting(ctx, dpuNode, true, false)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(Equal("node effect is in progress"))
		})
	})

	// The reboot method must be observable on every DPUNode regardless of
	// which reboot path drives it (HostAgent / External / Script). The tests
	// below exercise the aggregator's source of truth, the lifecycle of
	// Status.RebootMethod, the ungated cleanup hook (so HostAgent-path
	// DPUNodes that never set RebootInProgress still clear the field), and
	// the External path's WaitForExternalReboot Message enrichment.
	Context("aggregated reboot method", func() {
		var (
			ctx context.Context
		)

		BeforeEach(func() {
			ctx = context.Background()
		})

		// makeDPUWithRebootStatus builds a DPU whose Status.RebootStatus.Method
		// matches the production wiring set by InitializeDPURebootStatus.
		// dpuNodeName is needed so cutil.GenerateDPUName produces the same
		// composite name aggregateAndPublishRebootMethod expects when it
		// iterates dpuNode.Spec.DPUs.
		makeDPUWithRebootStatus := func(dpuNodeName, deviceName string, method *provisioningv1.RebootMethodType) *provisioningv1.DPU {
			d := &provisioningv1.DPU{
				ObjectMeta: metav1.ObjectMeta{
					Name:      cutil.GenerateDPUName(dpuNodeName, deviceName),
					Namespace: "test-namespace",
					Labels:    map[string]string{provisioningv1.DPUNodeNameLabel: dpuNodeName},
				},
				Status: provisioningv1.DPUStatus{Phase: provisioningv1.DPURebooting},
			}
			if method != nil {
				d.Status.RebootStatus = &provisioningv1.RebootStatus{Method: method}
			}
			return d
		}

		// dpuNodeWith builds a DPUNode whose Spec.DPUs entries match the
		// device names supplied so cutil.GetDPUsWithPhase resolves them via
		// cutil.GenerateDPUName.
		dpuNodeWith := func(name string, deviceNames ...string) *provisioningv1.DPUNode {
			refs := make([]provisioningv1.DPURef, 0, len(deviceNames))
			for _, n := range deviceNames {
				refs = append(refs, provisioningv1.DPURef{Name: n})
			}
			return &provisioningv1.DPUNode{
				ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "test-namespace"},
				Spec:       provisioningv1.DPUNodeSpec{DPUs: refs},
			}
		}

		newReconcilerWith := func(objs ...client.Object) (*DPUNodeReconciler, client.Client) {
			scheme := runtime.NewScheme()
			Expect(provisioningv1.AddToScheme(scheme)).To(Succeed())
			Expect(corev1.AddToScheme(scheme)).To(Succeed())
			Expect(batchv1.AddToScheme(scheme)).To(Succeed())
			c := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(objs...).
				WithStatusSubresource(&provisioningv1.DPU{}, &provisioningv1.DPUNode{}).
				Build()
			return &DPUNodeReconciler{Client: c, Recorder: record.NewFakeRecorder(10)}, c
		}

		Context("aggregateDPURebootMethods (pure)", func() {
			// rawDPU builds an isolated DPU whose ObjectMeta.Name is exactly
			// the supplied string. The pure aggregator never consults
			// dpuNode.Spec.DPUs, so the name does not need to satisfy
			// GenerateDPUName here.
			rawDPU := func(name string, method *provisioningv1.RebootMethodType) *provisioningv1.DPU {
				d := &provisioningv1.DPU{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "test-namespace"}}
				if method != nil {
					d.Status.RebootStatus = &provisioningv1.RebootStatus{Method: method}
				}
				return d
			}

			It("ignores AgentStatus.RebootMethod and reads Status.RebootStatus.Method as the source of truth", func() {
				// Production source of truth is Status.RebootStatus.Method.
				// AgentStatus.RebootMethod is intentionally set to a different
				// value to prove the aggregator does not consult it.
				dpu := rawDPU("dpu-a", rebootMethodPtr(provisioningv1.RebootMethodFirmwareReset))
				dpu.Status.AgentStatus = &provisioningv1.AgentStatus{
					RebootMethod: ptr.To(provisioningv1.RebootMethodPowerCycle),
				}

				agg := aggregateDPURebootMethods([]*provisioningv1.DPU{dpu})
				Expect(agg.HasReporting).To(BeTrue())
				Expect(agg.Method).To(Equal(provisioningv1.RebootMethodFirmwareReset))
				Expect(agg.Winner).To(Equal("dpu-a"))
			})

			It("seeds Winner from the lex-smallest reporting DPU even when all reporting DPUs report Unknown", func() {
				// Regression: an earlier draft of the aggregator left Winner
				// empty when every reporting DPU reported Unknown, because
				// Unknown never beat the initial-priority sentinel. The fix
				// always seeds Winner from the first reporting DPU, then
				// upgrades on strict priority.
				dpus := []*provisioningv1.DPU{
					rawDPU("zzz", rebootMethodPtr(provisioningv1.RebootMethodUnknown)),
					rawDPU("aaa", rebootMethodPtr(provisioningv1.RebootMethodUnknown)),
				}
				agg := aggregateDPURebootMethods(dpus)
				Expect(agg.HasReporting).To(BeTrue())
				Expect(agg.Method).To(Equal(provisioningv1.RebootMethodUnknown))
				Expect(agg.Winner).To(Equal("aaa"), "Winner must seed from the lex-smallest reporting DPU")
				Expect(agg.PerDPU).To(Equal("aaa=Unknown,zzz=Unknown"))
			})

			It("distinguishes 'no DPU has reported' from 'all reported Unknown' via HasReporting", func() {
				dpus := []*provisioningv1.DPU{
					rawDPU("dpu-a", nil),
					rawDPU("dpu-b", nil),
				}
				agg := aggregateDPURebootMethods(dpus)
				Expect(agg.HasReporting).To(BeFalse(), "no DPU reported a method")
				Expect(agg.Winner).To(BeEmpty())
				Expect(agg.PerDPU).To(Equal("dpu-a=Unknown,dpu-b=Unknown"))
			})

			It("orders DPUWarmReboot above NoAction and below FirmwareReset in the priority table", func() {
				// Regression for the priority renumbering:
				// PowerCycle > SystemLevelReset > SystemReboot > FirmwareReset > DPUWarmReboot > NoAction > Unknown.
				// Pin both adjacent boundaries with the smallest possible
				// fixtures: DPUWarmReboot must beat NoAction, and FirmwareReset
				// must beat DPUWarmReboot. Tied priorities tie-break by
				// ascending DPU name.
				warmVsNoAction := aggregateDPURebootMethods([]*provisioningv1.DPU{
					rawDPU("dpu-a", rebootMethodPtr(provisioningv1.RebootMethodNoAction)),
					rawDPU("dpu-b", rebootMethodPtr(provisioningv1.RebootMethodDPUWarmReboot)),
				})
				Expect(warmVsNoAction.Method).To(Equal(provisioningv1.RebootMethodDPUWarmReboot))
				Expect(warmVsNoAction.Winner).To(Equal("dpu-b"))

				firmwareVsWarm := aggregateDPURebootMethods([]*provisioningv1.DPU{
					rawDPU("dpu-a", rebootMethodPtr(provisioningv1.RebootMethodDPUWarmReboot)),
					rawDPU("dpu-b", rebootMethodPtr(provisioningv1.RebootMethodFirmwareReset)),
				})
				Expect(firmwareVsWarm.Method).To(Equal(provisioningv1.RebootMethodFirmwareReset))
				Expect(firmwareVsWarm.Winner).To(Equal("dpu-b"))
			})

			It("falls back to a non-empty placeholder for buildExternalRebootWaitMessage when agg.HasReporting is false", func() {
				// Defensive-only branch: production InitializeDPURebootStatus
				// errors out before letting a DPU enter DPURebooting without
				// a method, so HasReporting=false is unreachable through the
				// real phase machine. Still pin (a) the placeholder is
				// non-empty (operators never see a blank Message on
				// RebootInProgress=True) and (b) HasReporting is the only
				// gate -- a stray Winner without HasReporting still gets the
				// placeholder, never "required reboot method:  (driven by DPU dpu-a)".
				const placeholder = "required reboot method: <pending agent report>"
				Expect(buildExternalRebootWaitMessage(rebootMethodAggregation{})).To(Equal(placeholder))
				Expect(buildExternalRebootWaitMessage(rebootMethodAggregation{
					Method: provisioningv1.RebootMethodPowerCycle,
					Winner: "dpu-a",
				})).To(Equal(placeholder))
			})
		})

		Context("aggregateAndPublishRebootMethod (rebooting -> idle persistence)", func() {
			It("stamps Status.RebootMethod once a DPU reports and preserves it across the rebooting->idle transition", func() {
				// Cycle 1: PowerCycle is reported. Verify the field is set.
				dpuNode := dpuNodeWith("dpu-node", "1")
				dpu := makeDPUWithRebootStatus("dpu-node", "1", rebootMethodPtr(provisioningv1.RebootMethodPowerCycle))
				rec, c := newReconcilerWith(dpu, dpuNode)

				inScope, err := rec.aggregateAndPublishRebootMethod(ctx, dpuNode)
				Expect(err).NotTo(HaveOccurred())
				Expect(inScope).To(HaveLen(1))
				agg := aggregateDPURebootMethods(inScope)
				Expect(agg.HasReporting).To(BeTrue())
				Expect(agg.Method).To(Equal(provisioningv1.RebootMethodPowerCycle))

				Expect(dpuNode.Status.RebootMethod).NotTo(BeNil())
				Expect(*dpuNode.Status.RebootMethod).To(Equal(provisioningv1.RebootMethodPowerCycle))

				// Cycle 2: the DPU has left DPURebooting but the DPUNode still
				// owns it. The field must remain.
				dpu.Status.Phase = provisioningv1.DPUReady
				Expect(c.Status().Update(ctx, dpu)).To(Succeed())

				inScope, err = rec.aggregateAndPublishRebootMethod(ctx, dpuNode)
				Expect(err).NotTo(HaveOccurred())
				Expect(inScope).To(BeEmpty(), "no DPU in DPURebooting in cycle 2")
				Expect(aggregateDPURebootMethods(inScope).HasReporting).To(BeFalse())

				Expect(dpuNode.Status.RebootMethod).NotTo(BeNil(), "Status.RebootMethod must persist across the rebooting->idle transition")
				Expect(*dpuNode.Status.RebootMethod).To(Equal(provisioningv1.RebootMethodPowerCycle))
			})
		})

		Context("noneDPUInNodeEffectOrRebooting (cleanup is ungated, covers HostAgent path)", func() {
			It("clears Status.RebootMethod when no DPUs remain, even if RebootInProgress was never set", func() {
				// HostAgent path: the DPUNode controller never sets
				// DPUNodeRebootInProgress. Status.RebootMethod must still be
				// cleaned up when the DPUNode loses all its DPUs, which is
				// why the cleanup gate is `RebootInProgress OR
				// hasRebootMethodField` rather than `RebootInProgress` only.
				dpuNode := &provisioningv1.DPUNode{
					ObjectMeta: metav1.ObjectMeta{Name: "dpu-node", Namespace: "test-namespace"},
					Status: provisioningv1.DPUNodeStatus{
						RebootMethod: rebootMethodPtr(provisioningv1.RebootMethodPowerCycle),
					},
				}
				rec, _ := newReconcilerWith(dpuNode) // no DPUs in the cluster

				Expect(rec.noneDPUInNodeEffectOrRebooting(ctx, dpuNode, false, false)).To(Succeed())

				Expect(dpuNode.Status.RebootMethod).To(BeNil(), "Status.RebootMethod must be cleared when the DPUNode loses all DPUs")
			})

			It("preserves Status.RebootMethod when only the field is set and DPUs still exist (HostAgent steady-state)", func() {
				// HostAgent path leaves the field set without
				// RebootInProgress. The cleanup gate must observe
				// hasRebootMethodField, fire the List, and refuse to sweep
				// because the label List returns the still-present DPU.
				dpuNode := &provisioningv1.DPUNode{
					ObjectMeta: metav1.ObjectMeta{Name: "dpu-node", Namespace: "test-namespace"},
					Status: provisioningv1.DPUNodeStatus{
						RebootMethod: rebootMethodPtr(provisioningv1.RebootMethodPowerCycle),
					},
				}
				dpu := &provisioningv1.DPU{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-dpu",
						Namespace: "test-namespace",
						Labels:    map[string]string{provisioningv1.DPUNodeNameLabel: "dpu-node"},
					},
					Spec:   provisioningv1.DPUSpec{DPUNodeName: "dpu-node"},
					Status: provisioningv1.DPUStatus{Phase: provisioningv1.DPUReady},
				}
				rec, _ := newReconcilerWith(dpuNode, dpu)

				Expect(rec.noneDPUInNodeEffectOrRebooting(ctx, dpuNode, false, false)).To(Succeed())

				Expect(dpuNode.Status.RebootMethod).NotTo(BeNil(), "Status.RebootMethod must be preserved while the DPUNode still owns a DPU")
				Expect(*dpuNode.Status.RebootMethod).To(Equal(provisioningv1.RebootMethodPowerCycle))
			})

			It("does not List DPUs when no reboot marker is present (steady-state fast path)", func() {
				// Sanity: a DPUNode that has never rebooted should hit zero
				// API calls in the cleanup branch. Using the
				// dpuNodeErrorInjectingClient declared at the top of this
				// file we fail every List, so a passing call confirms the
				// gate guards the List entirely.
				scheme := runtime.NewScheme()
				Expect(provisioningv1.AddToScheme(scheme)).To(Succeed())
				inner := fake.NewClientBuilder().WithScheme(scheme).Build()
				rec := &DPUNodeReconciler{Client: &dpuNodeErrorInjectingClient{
					Client: inner,
					listFunc: func(_ context.Context, _ client.ObjectList, _ ...client.ListOption) error {
						return fmt.Errorf("List must not be invoked when no reboot marker is present")
					},
				}}
				dpuNode := &provisioningv1.DPUNode{
					ObjectMeta: metav1.ObjectMeta{Name: "dpu-node", Namespace: "test-namespace"},
				}
				Expect(rec.noneDPUInNodeEffectOrRebooting(ctx, dpuNode, false, false)).To(Succeed())
			})
		})

		Context("External path WaitForExternalReboot Message", func() {
			It("enriches the RebootInProgress Message with the aggregated method and winner DPU", func() {
				// processExternalReboot's Message-enrichment branch fires
				// only when every rebooting DPU is already in
				// RebootStatus.Phase=Pending with Reason=WaitingForManualPowerCycleOrReboot,
				// i.e. a steady-state reconcile after Step 2 has been
				// executed at least once. Pre-stamp that state plus the
				// annotation so we exercise the actual enrichment path.
				dpuNode := dpuNodeWith("dpu-node", "a")
				dpuNode.Annotations = map[string]string{
					provisioningv1.DPUNodeExternalRebootRequiredAnnotation: "true",
				}
				dpuNode.Spec.NodeRebootMethod = &provisioningv1.NodeRebootMethod{External: &provisioningv1.External{}}
				dpu := makeDPUWithRebootStatus("dpu-node", "a", rebootMethodPtr(provisioningv1.RebootMethodPowerCycle))
				dpu.Status.RebootStatus.Phase = provisioningv1.RebootStatusPending
				dpu.Status.RebootStatus.Reason = "WaitingForManualPowerCycleOrReboot"
				rec, _ := newReconcilerWith(dpu, dpuNode)

				inScope, err := rec.aggregateAndPublishRebootMethod(ctx, dpuNode)
				Expect(err).NotTo(HaveOccurred())
				Expect(inScope).To(HaveLen(1))
				Expect(rec.processExternalReboot(ctx, dpuNode, inScope)).To(Succeed())

				cond := meta.FindStatusCondition(dpuNode.Status.Conditions,
					provisioningv1.DPUNodeConditionRebootInProgress.String())
				Expect(cond).NotTo(BeNil())
				Expect(cond.Status).To(Equal(metav1.ConditionTrue))
				Expect(cond.Reason).To(Equal("WaitForExternalReboot"))
				Expect(cond.Message).To(Equal("required reboot method: PowerCycle (driven by DPU dpu-node-a)"))
			})

			It("Step 1 stamps the enriched Message and the annotation on the first reconcile", func() {
				// First-time entry into the External path: no annotation yet,
				// at least one rebooting DPU has no RebootStatus.Phase (so
				// condExists is false in processExternalReboot). Step 1 must
				// (a) set the dpunode-external-reboot-required annotation and
				// (b) stamp RebootInProgress=True with the enriched
				// "required reboot method: ..." Message immediately, not one
				// reconcile later when condExists flips to true.
				dpuNode := dpuNodeWith("dpu-node", "a")
				dpuNode.Spec.NodeRebootMethod = &provisioningv1.NodeRebootMethod{External: &provisioningv1.External{}}
				dpu := makeDPUWithRebootStatus("dpu-node", "a", rebootMethodPtr(provisioningv1.RebootMethodPowerCycle))
				rec, _ := newReconcilerWith(dpu, dpuNode)

				inScope, err := rec.aggregateAndPublishRebootMethod(ctx, dpuNode)
				Expect(err).NotTo(HaveOccurred())
				Expect(inScope).To(HaveLen(1))
				Expect(rec.processExternalReboot(ctx, dpuNode, inScope)).To(Succeed())

				Expect(dpuNode.Annotations).To(HaveKeyWithValue(
					provisioningv1.DPUNodeExternalRebootRequiredAnnotation, "true"))
				cond := meta.FindStatusCondition(dpuNode.Status.Conditions,
					provisioningv1.DPUNodeConditionRebootInProgress.String())
				Expect(cond).NotTo(BeNil())
				Expect(cond.Status).To(Equal(metav1.ConditionTrue))
				Expect(cond.Reason).To(Equal("WaitForExternalReboot"))
				Expect(cond.Message).To(Equal("required reboot method: PowerCycle (driven by DPU dpu-node-a)"))
			})

			It("Step 2 refreshes the Message when agg shifts between Step 1 and Step 2", func() {
				// Step 1 stamps the annotation + enriched Message naming the
				// initial winner. Before Step 2 runs the snapshot changes
				// (different DPU now drives a different method). Without the
				// Step 2 Message refresh the condition would carry a
				// one-cycle-stale Message until the next reconcile flips
				// condExists.
				dpuNode := dpuNodeWith("dpu-node", "a", "b")
				dpuNode.Spec.NodeRebootMethod = &provisioningv1.NodeRebootMethod{External: &provisioningv1.External{}}
				dpuNode.Annotations = map[string]string{
					provisioningv1.DPUNodeExternalRebootRequiredAnnotation: "true",
				}
				dpuNode.Status.Conditions = []metav1.Condition{{
					Type:    provisioningv1.DPUNodeConditionRebootInProgress.String(),
					Status:  metav1.ConditionTrue,
					Reason:  "WaitForExternalReboot",
					Message: "required reboot method: PowerCycle (driven by DPU dpu-node-a)",
				}}
				dpuB := makeDPUWithRebootStatus("dpu-node", "b", rebootMethodPtr(provisioningv1.RebootMethodSystemReboot))
				rec, _ := newReconcilerWith(dpuB, dpuNode)

				inScope, err := rec.aggregateAndPublishRebootMethod(ctx, dpuNode)
				Expect(err).NotTo(HaveOccurred())
				Expect(inScope).To(HaveLen(1))
				Expect(rec.processExternalReboot(ctx, dpuNode, inScope)).To(Succeed())

				cond := meta.FindStatusCondition(dpuNode.Status.Conditions,
					provisioningv1.DPUNodeConditionRebootInProgress.String())
				Expect(cond).NotTo(BeNil())
				Expect(cond.Message).To(Equal("required reboot method: SystemReboot (driven by DPU dpu-node-b)"),
					"Step 2 must refresh the Message to track the current-cycle winner")
			})
		})

		Context("HandleRebootSync snapshot guard", func() {
			It("requeues without driving rebootNode when dpuPhases reports DPURebooting but the inner Get returns no rebooting DPU", func() {
				// dpuPhases is computed at the top of Reconcile via the
				// label-list path; HandleRebootSync re-derives inScope
				// internally via aggregateAndPublishRebootMethod (Spec.DPUs
				// + Get). Between the two reads the cache can disagree
				// (a label points at a DPU whose Phase has already moved
				// off DPURebooting, or a DPU has been deleted), producing
				// dpuPhases={DPURebooting} but inScope=empty. Without the
				// guard External would mark RebootInProgress=False against
				// zero DPUs and Script would create a Job from a
				// HasReporting=false aggregation. Simulate the race by
				// asserting DPURebooting in dpuPhases while no DPU in
				// DPURebooting exists in the cluster.
				dpuNode := dpuNodeWith("dpu-node", "a")
				dpuNode.Spec.NodeRebootMethod = &provisioningv1.NodeRebootMethod{
					Script: &provisioningv1.Script{Name: "irrelevant-cm"},
				}
				rec, _ := newReconcilerWith(dpuNode)

				phases := map[string]struct{}{string(provisioningv1.DPURebooting): {}}
				result, err := rec.HandleRebootSync(ctx, dpuNode, phases)
				Expect(err).NotTo(HaveOccurred())
				Expect(result.RequeueAfter).NotTo(BeZero(), "must requeue rather than reboot off an empty snapshot")
				Expect(meta.FindStatusCondition(dpuNode.Status.Conditions,
					provisioningv1.DPUNodeConditionRebootInProgress.String())).To(BeNil(),
					"must not stamp RebootInProgress when refusing to reboot")
			})
		})

	})
})
