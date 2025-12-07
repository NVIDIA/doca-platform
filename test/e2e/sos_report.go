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

package e2e

import (
	"context"
	"fmt"
	"sync"
	"time"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	testutils "github.com/nvidia/doca-platform/test/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	sosJobTimeout = 30 * time.Minute // sos job is expected to be completed within half an hour.
)

// collectSOSReports orchestrates the collection of SOS reports from all nodes in Host and DPU clusters using Kubernetes Jobs.
func collectSOSReports(ctx context.Context, input collectResourcesInput, dpuClient client.Client, dpuRestConfig *rest.Config) error {
	By("Collecting SOS reports from nodes using Jobs")
	var wg sync.WaitGroup
	var err error
	var hostKubeconfigData, dpuKubeconfigData []byte

	if dpuClient == nil || dpuRestConfig == nil {
		GinkgoLogr.Error(err, "DPU cluster or DPU REST config is not set")
		return err
	}

	hostKubeconfigData, err = createKubeconfigDataFromConfig(input.restConfig)
	if err != nil {
		GinkgoLogr.Error(err, "Failed to create host kubeconfig data")
		return err
	}
	dpuKubeconfigData, err = getDPUKubeconfigData(ctx, input.testClient)
	if err != nil {
		GinkgoLogr.Error(err, "Failed to get DPU kubeconfig data")
		return err
	}

	runOnClusterNodes(ctx, input.testClient, &wg, "host", hostKubeconfigData)
	runOnClusterNodes(ctx, dpuClient, &wg, "dpu", dpuKubeconfigData)

	// Wait for all jobs to finish (with a timeout)
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		By("Finished collecting SOS reports")
	case <-time.After(sosJobTimeout + 1*time.Minute): // Give extra minute for allowing Job to cleanup
		By("Timed out waiting for SOS reports collection")
	}
	return nil
}

// runSOSJob creates and monitors a Kubernetes Job to run sos report on a specific node.
func runSOSJob(ctx context.Context, c client.Client, nodeName string, kubeconfigData []byte) {
	jobName := fmt.Sprintf("sos-report-job-%s", nodeName)
	namespace := "default"
	secretName := "admin-config"
	caseID := fmt.Sprintf("dpf-%s", time.Now().Format("150405"))
	By(fmt.Sprintf("Running SOS report Job on node %s with caseID %s", nodeName, caseID))

	// Ensure admin-config secret exists for the pod to mount
	if err := createKubeconfigSecret(ctx, c, namespace, secretName, kubeconfigData); err != nil {
		GinkgoLogr.Error(err, "Failed to create admin-config secret", "node", nodeName)
		return
	}

	// Define Job, sos report will be written to the /tmp directory in the node filesystem.
	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      jobName,
			Namespace: namespace,
			Labels:    testutils.AfterEachCleanupLabels,
		},
		Spec: batchv1.JobSpec{
			Completions:             ptr.To(int32(1)),
			Parallelism:             ptr.To(int32(1)),
			BackoffLimit:            ptr.To(int32(3)),                       // Retry 3 times if it fails
			ActiveDeadlineSeconds:   ptr.To(int64(sosJobTimeout.Seconds())), // Timeout
			TTLSecondsAfterFinished: ptr.To(int32(20)),                      // Cleanup time
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"job-name": jobName},
				},
				Spec: corev1.PodSpec{
					NodeName:      nodeName,
					RestartPolicy: corev1.RestartPolicyNever,
					HostIPC:       true,
					HostNetwork:   true,
					HostPID:       true,
					Containers: []corev1.Container{
						{
							Name:            "sosreport",
							Image:           "ghcr.io/nvidia/sosreport:latest",
							ImagePullPolicy: corev1.PullIfNotPresent,
							Env: []corev1.EnvVar{
								{Name: "CASE_ID", Value: caseID},
							},
							SecurityContext: &corev1.SecurityContext{
								Privileged: ptr.To(true),
								RunAsUser:  ptr.To(int64(0)),
							},
							VolumeMounts: []corev1.VolumeMount{
								{MountPath: "/host", Name: "host"},
								{MountPath: "/run", Name: "run"},
								{MountPath: "/var/log", Name: "varlog"},
								{MountPath: "/etc/kubernetes/admin.conf", Name: "adminconf", SubPath: "kubeconfig"},
								{MountPath: "/etc/localtime", Name: "localtime"},
								{MountPath: "/etc/machine-id", Name: "machineid"},
								{MountPath: "/boot", Name: "boot"},
								{MountPath: "/usr/lib/modules/", Name: "modules"},
								{MountPath: "/tmp", Name: "tmp"}, // Mount host /tmp to container /tmp
							},
						},
					},
					Volumes: []corev1.Volume{
						{Name: "host", VolumeSource: corev1.VolumeSource{HostPath: &corev1.HostPathVolumeSource{Path: "/"}}},
						{Name: "run", VolumeSource: corev1.VolumeSource{HostPath: &corev1.HostPathVolumeSource{Path: "/run"}}},
						{Name: "boot", VolumeSource: corev1.VolumeSource{HostPath: &corev1.HostPathVolumeSource{Path: "/boot"}}},
						{Name: "modules", VolumeSource: corev1.VolumeSource{HostPath: &corev1.HostPathVolumeSource{Path: "/usr/lib/modules/"}}},
						{Name: "varlog", VolumeSource: corev1.VolumeSource{HostPath: &corev1.HostPathVolumeSource{Path: "/var/log"}}},
						{Name: "adminconf", VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{SecretName: secretName}}},
						{Name: "localtime", VolumeSource: corev1.VolumeSource{HostPath: &corev1.HostPathVolumeSource{Path: "/etc/localtime"}}},
						{Name: "machineid", VolumeSource: corev1.VolumeSource{HostPath: &corev1.HostPathVolumeSource{Path: "/etc/machine-id"}}},
						{Name: "tmp", VolumeSource: corev1.VolumeSource{HostPath: &corev1.HostPathVolumeSource{Path: "/tmp"}}},
					},
				},
			},
		},
	}

	// Cleanup existing job if any
	_ = testutils.CleanupAndWait(ctx, c, job)

	// Create a new Job and wrap with eventually to handle intermittent errors
	Eventually(func() error {
		return c.Create(ctx, job)
	}, 30*time.Second, 1*time.Second).Should(Succeed(), fmt.Sprintf("Failed to create SOS Job for node %s", nodeName))

	// Wait for Job completion with 30 seconds buffer
	timeOut := sosJobTimeout + 30*time.Second
	err := waitJobComplete(ctx, c, job, timeOut)
	if err != nil {
		GinkgoLogr.Error(err, "SOS Job failed or timed out", "node", nodeName, "timeout", sosJobTimeout)
	} else {
		By(fmt.Sprintf("SOS Job completed successfully on node %s", nodeName))
	}
}

func waitJobComplete(ctx context.Context, c client.Client, job *batchv1.Job, timeout time.Duration) error {
	return wait.PollUntilContextTimeout(ctx, 5*time.Second, timeout, true, func(ctx context.Context) (bool, error) {
		j := &batchv1.Job{}
		if err := c.Get(ctx, client.ObjectKeyFromObject(job), j); err != nil {
			return false, client.IgnoreNotFound(err)
		}

		// Check for success
		if j.Status.Succeeded > 0 {
			return true, nil
		}

		// Check for failure
		if j.Status.Failed == 0 {
			return false, nil
		}

		// Check conditions for DeadlineExceeded
		for _, cond := range j.Status.Conditions {
			if (cond.Type == batchv1.JobFailed || cond.Type == batchv1.JobFailureTarget) && cond.Status == corev1.ConditionTrue {
				if cond.Reason == "DeadlineExceeded" {
					GinkgoLogr.Error(fmt.Errorf("job deadline exceeded"), "SOS report job timed out", "reason", cond.Reason, "message", cond.Message)
				}
				return false, fmt.Errorf("job failed: %s - %s", cond.Reason, cond.Message)
			}
		}
		return false, fmt.Errorf("job failed with %d failed pods. Conditions: %+v", j.Status.Failed, j.Status.Conditions)
	})
}

// createKubeconfigSecret creates a secret containing the kubeconfig for the sos report to use.
func createKubeconfigSecret(ctx context.Context, c client.Client, namespace, name string, data []byte) error {
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels:    testutils.AfterEachCleanupLabels,
		},
		Data: map[string][]byte{
			"kubeconfig": data,
		},
	}
	if err := c.Create(ctx, secret); err != nil {
		if client.IgnoreAlreadyExists(err) != nil {
			GinkgoLogr.Error(err, "Failed to create secret", "name", name, "namespace", namespace)
			return err
		}
	}
	return nil
}

// getDPUKubeconfigData gets the DPU kubeconfig data from the DPU cluster from the kamaji cluster secret.
func getDPUKubeconfigData(ctx context.Context, c client.Client) ([]byte, error) {
	dpuClusters := &provisioningv1.DPUClusterList{}
	if err := c.List(ctx, dpuClusters); err == nil && len(dpuClusters.Items) > 0 {
		dpuCluster := dpuClusters.Items[0]
		secretName := dpuCluster.Spec.Kubeconfig
		secretNamespace := dpuCluster.Namespace
		secret := &corev1.Secret{}
		if err := c.Get(ctx, client.ObjectKey{Name: secretName, Namespace: secretNamespace}, secret); err == nil {
			if data, ok := secret.Data["admin.conf"]; ok {
				By(fmt.Sprintf("Found DPU Cluster kubeconfig secret %s in namespace %s", secretName, secretNamespace))
				return data, nil
			}
		} else {
			GinkgoLogr.Error(err, "Failed to get DPU Cluster kubeconfig secret", "name", secretName, "namespace", secretNamespace)
		}
	}
	return nil, fmt.Errorf("no DPU clusters found")
}

func createKubeconfigDataFromConfig(config *rest.Config) ([]byte, error) {
	apiConfig := clientcmdapi.NewConfig()
	clusterName := "cluster"
	userName := "user"
	contextName := "context"

	apiConfig.Clusters[clusterName] = &clientcmdapi.Cluster{
		Server:                   config.Host,
		CertificateAuthorityData: config.CAData,
		InsecureSkipTLSVerify:    config.Insecure,
	}
	apiConfig.AuthInfos[userName] = &clientcmdapi.AuthInfo{
		ClientCertificateData: config.CertData,
		ClientKeyData:         config.KeyData,
		Token:                 config.BearerToken,
		Username:              config.Username,
		Password:              config.Password,
	}
	apiConfig.Contexts[contextName] = &clientcmdapi.Context{
		Cluster:  clusterName,
		AuthInfo: userName,
	}
	apiConfig.CurrentContext = contextName

	data, err := clientcmd.Write(*apiConfig)
	if err != nil {
		return nil, err
	}
	return data, nil
}

func runOnClusterNodes(ctx context.Context, c client.Client, wg *sync.WaitGroup, clusterName string, kubeconfigData []byte) {
	nodes := &corev1.NodeList{}
	if err := c.List(ctx, nodes); err == nil {
		for _, node := range nodes.Items {
			wg.Add(1)
			go func(n string) {
				defer wg.Done()
				runSOSJob(ctx, c, n, kubeconfigData)
			}(node.Name)
		}
	} else {
		GinkgoLogr.Error(err, "Failed to list nodes for SOS report", "cluster", clusterName)
	}
}
