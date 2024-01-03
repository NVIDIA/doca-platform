/*
Copyright 2024 NVIDIA

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

package hostnetwork

import (
	"context"
	"fmt"
	"regexp"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	dutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/util"
	cutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/util"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	NumofVFDefaultValue        = "16"
	ParprouterdRefreshInterval = 30
)

func getNumOfVFsFromFlavor(flavor *provisioningv1.DPUFlavor) (string, bool) {
	regex := regexp.MustCompile(`^NUM_OF_VFS=([0-9]+)`)
	for _, nvconfig := range flavor.Spec.NVConfig {
		for _, parmeter := range nvconfig.Parameters {
			matches := regex.FindStringSubmatch(parmeter)
			if len(matches) == 2 {
				return matches[1], true
			}
		}
	}
	return "", false
}

func CreateHostNetworkSetupPod(ctx context.Context, client client.Client, dpu *provisioningv1.DPU, option dutil.DPUOptions) error {
	logger := log.FromContext(ctx)
	hostnetworkPodName := cutil.GenerateHostnetworkPodName(dpu.Name)

	numVFs := NumofVFDefaultValue
	flavor := &provisioningv1.DPUFlavor{}
	if err := client.Get(ctx, types.NamespacedName{
		Namespace: dpu.Namespace,
		Name:      dpu.Spec.DPUFlavor,
	}, flavor); err != nil {
		return err
	}

	if num, ok := getNumOfVFsFromFlavor(flavor); ok {
		numVFs = num
	}

	removePrefix := false
	pciAddress, err := cutil.GetPCIAddrFromLabel(dpu.Labels, removePrefix)
	if err != nil {
		logger.Error(err, "Failed to get pci address from node label", "dms", err)
		return err
	}

	hostPathType := corev1.HostPathDirectory
	owner := metav1.NewControllerRef(dpu,
		provisioningv1.GroupVersion.WithKind("DPU"))
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:            hostnetworkPodName,
			Namespace:       dpu.Namespace,
			OwnerReferences: []metav1.OwnerReference{*owner},
		},
		Spec: corev1.PodSpec{
			HostNetwork: true,
			Containers: []corev1.Container{
				{
					Name:            "hostnetwork",
					Image:           option.HostnetworkImageWithTag,
					ImagePullPolicy: corev1.PullIfNotPresent,
					VolumeMounts: []corev1.VolumeMount{
						{
							Name:      "dev",
							MountPath: "/dev",
						},
					},
					SecurityContext: &corev1.SecurityContext{
						Privileged: ptr.To(true),
					},
					Env: []corev1.EnvVar{
						{
							Name:  "device_pci_address",
							Value: pciAddress,
						},
						{
							Name:  "num_of_vfs",
							Value: numVFs,
						},
					},

					Command: []string{"/bin/bash", "-c", "--"},
					Args: []string{
						"hostnetwork.sh",
					},
					ReadinessProbe: &corev1.Probe{
						ProbeHandler: corev1.ProbeHandler{
							Exec: &corev1.ExecAction{
								Command: []string{
									"ls",
									"/tmp/hostnetwork_succeed",
								},
							},
						},
						PeriodSeconds: 5,
					},
				},
			},
			ImagePullSecrets: option.ImagePullSecrets,
			Volumes: []corev1.Volume{
				{
					Name: "dev",
					VolumeSource: corev1.VolumeSource{
						HostPath: &corev1.HostPathVolumeSource{
							Path: "/dev",
							Type: &hostPathType,
						},
					},
				},
			},
		},
	}
	pod.Spec.Affinity = cutil.ReplaceDaemonSetPodNodeNameNodeAffinity(
		pod.Spec.Affinity, dpu.Spec.NodeName)
	pod.Spec.Tolerations = cutil.GeneratePodToleration(*dpu.Spec.NodeEffect)
	err = client.Create(ctx, pod)
	if err != nil {
		logger.Error(err, fmt.Sprintf("Failed to create %s hostnetwork pod", hostnetworkPodName))
		return err
	}
	logger.V(3).Info(fmt.Sprintf("%s Hostnetwork pod created", hostnetworkPodName))
	return nil
}
