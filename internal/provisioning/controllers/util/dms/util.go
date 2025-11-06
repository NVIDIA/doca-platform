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

package dms

import (
	"context"
	"fmt"
	"strings"

	dnutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/dpunode/util"
	cutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/util"
	hostutil "github.com/nvidia/doca-platform/internal/provisioning/hostagent/util"
	"github.com/nvidia/doca-platform/internal/release"

	"github.com/openconfig/gnmi/proto/gnmi"
	"google.golang.org/grpc"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	DMSImageFolder        string = "/tmp/dms"
	DMSServiceAccountName string = "dpf-provisioning-hostagent-service-account"
	DMSContainerName      string = "dms"
	// DPFLocalDir is mounted to the containers.
	// For backwards compatibility, /var/lib/dpf is used, which is the parent directory of /var/lib/dpf/hostagent and the old /var/lib/dpf/dms.
	DPFLocalDir                     string = "/var/lib/dpf"
	SystemNodeCriticalPriorityClass string = "system-node-critical"
)

func CreateHostAgentPod(ctx context.Context, client client.Client, node *corev1.Node, option dnutil.HostAgentPodOptions, namespace string, dpfOperatorConfigOwnerRef *metav1.OwnerReference) error {
	logger := log.FromContext(ctx)
	dmsPodName := cutil.GenerateHostAgentPodName(node)

	rebootParams := ""
	if node.Labels != nil {
		if v, ok := node.Labels[cutil.DPUNodeRebootMethodLabel]; ok {
			rebootParams = fmt.Sprintf("--reboot-method %s", v)
		}
		if v, ok := node.Labels[cutil.DPUNodeScriptNameLabel]; ok {
			rebootParams = fmt.Sprintf("%s --custom-script-name %s", rebootParams, v)
		}
	}

	extraEnvs := []corev1.EnvVar{}
	for _, env := range option.DMSPodEnvs {
		parts := strings.Split(env, "=")
		if len(parts) != 2 {
			return fmt.Errorf("invalid environment variable: %s", env)
		}
		extraEnvs = append(extraEnvs, corev1.EnvVar{
			Name:  parts[0],
			Value: parts[1],
		})
	}
	extraEnvs = append(extraEnvs, corev1.EnvVar{
		Name: hostutil.K8sNodeNameEnv,
		ValueFrom: &corev1.EnvVarSource{
			FieldRef: &corev1.ObjectFieldSelector{
				FieldPath: "spec.nodeName",
			},
		},
	})

	hostPathType := corev1.HostPathDirectory
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      dmsPodName,
			Namespace: namespace,
			Labels: map[string]string{
				cutil.ProvisioningComponentLabelKey: "hostagent",
				release.DPFVersionLabelKey:          release.DPFVersion(),
			},
			OwnerReferences: []metav1.OwnerReference{*dpfOperatorConfigOwnerRef},
		},
		Spec: corev1.PodSpec{
			ServiceAccountName: DMSServiceAccountName,
			HostNetwork:        true,
			DNSPolicy:          corev1.DNSClusterFirstWithHostNet,
			PriorityClassName:  SystemNodeCriticalPriorityClass,
			Containers: []corev1.Container{
				{
					Name:            DMSContainerName,
					Image:           option.HostAgentImageWithTag,
					ImagePullPolicy: corev1.PullIfNotPresent,
					VolumeMounts: []corev1.VolumeMount{
						{
							Name:      "dev",
							MountPath: "/dev",
						},
						{
							Name:      "sys",
							MountPath: "/sys",
						},
						{
							Name:      "lib-modules",
							MountPath: "/lib/modules",
							ReadOnly:  true,
						},
						{
							Name:      "dms-image",
							MountPath: DMSImageFolder,
						},
						{
							Name:      "systemd-dbus",
							MountPath: "/var/run/dbus/system_bus_socket",
						},
					},
					SecurityContext: &corev1.SecurityContext{
						Privileged: ptr.To(true),
					},
					Env:     extraEnvs,
					Command: []string{"/bin/bash", "-c", "--"},
					Args: []string{
						"rshim --force && /hostagent rundms",
					},
				},
				{
					Name:            "hostagent",
					Image:           option.HostAgentImageWithTag,
					ImagePullPolicy: corev1.PullIfNotPresent,
					SecurityContext: &corev1.SecurityContext{
						Privileged: ptr.To(true),
					},
					Env:     extraEnvs,
					Command: []string{"/bin/bash", "-c", "--"},
					Args: []string{
						fmt.Sprintf("/hostagent serve --bfb-registry-address=%s %s",
							option.BFBRegistryAddress, rebootParams)},
					VolumeMounts: []corev1.VolumeMount{
						{
							Name:      "dev",
							MountPath: "/dev",
						},
						{
							Name:      "sys",
							MountPath: "/sys",
						},
						{
							Name:      "lib-modules",
							MountPath: "/lib/modules",
							ReadOnly:  true,
						},
						{
							Name:      "dms-image",
							MountPath: DMSImageFolder,
						},
						{
							Name:      "dpf-local-dir",
							MountPath: DPFLocalDir,
						},
						{
							Name:      "systemd-dbus",
							MountPath: "/var/run/dbus/system_bus_socket",
						},
						{
							Name:      "run-systemd",
							MountPath: "/run/systemd",
						},
						{
							Name:      "etc-netplan",
							MountPath: "/etc/netplan",
						},
						{
							Name:      "run-udev",
							MountPath: "/run/udev",
						},
						{
							Name:      "systemd-network",
							MountPath: "/usr/lib/systemd/network",
						},
					},
				},
			},
			ImagePullSecrets: option.ImagePullSecrets,
			// TODO: add a Volume for DMS Server certificate that will be populated by the DMS Init container and shared with the DMS container
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
				{
					Name: "dms-image",
					VolumeSource: corev1.VolumeSource{
						EmptyDir: &corev1.EmptyDirVolumeSource{},
					},
				},
				{
					Name: "sys",
					VolumeSource: corev1.VolumeSource{
						HostPath: &corev1.HostPathVolumeSource{
							Path: "/sys",
							Type: &hostPathType,
						},
					},
				},
				{
					Name: "lib-modules",
					VolumeSource: corev1.VolumeSource{
						HostPath: &corev1.HostPathVolumeSource{
							Path: "/lib/modules",
							Type: &hostPathType,
						},
					},
				},
				{
					Name: "dpf-local-dir",
					VolumeSource: corev1.VolumeSource{
						HostPath: &corev1.HostPathVolumeSource{
							Path: DPFLocalDir,
							Type: ptr.To(corev1.HostPathDirectoryOrCreate),
						},
					},
				},
				{
					Name: "systemd-dbus",
					VolumeSource: corev1.VolumeSource{
						HostPath: &corev1.HostPathVolumeSource{
							Path: "/var/run/dbus/system_bus_socket",
							Type: ptr.To(corev1.HostPathSocket),
						},
					},
				},
				{
					Name: "run-systemd",
					VolumeSource: corev1.VolumeSource{
						HostPath: &corev1.HostPathVolumeSource{
							Path: "/run/systemd",
							Type: ptr.To(corev1.HostPathDirectory),
						},
					},
				},
				{
					Name: "etc-netplan",
					VolumeSource: corev1.VolumeSource{
						HostPath: &corev1.HostPathVolumeSource{
							Path: "/etc/netplan",
							Type: ptr.To(corev1.HostPathDirectoryOrCreate),
						},
					},
				},
				{
					Name: "run-udev",
					VolumeSource: corev1.VolumeSource{
						HostPath: &corev1.HostPathVolumeSource{
							Path: "/run/udev",
							Type: ptr.To(corev1.HostPathDirectory),
						},
					},
				},
				{
					Name: "systemd-network",
					VolumeSource: corev1.VolumeSource{
						HostPath: &corev1.HostPathVolumeSource{
							Path: "/usr/lib/systemd/network",
							Type: ptr.To(corev1.HostPathDirectory),
						},
					},
				},
			},
			Tolerations: []corev1.Toleration{
				{
					Operator: corev1.TolerationOpExists,
					Effect:   corev1.TaintEffectNoExecute,
				},
				{
					Operator: corev1.TolerationOpExists,
					Effect:   corev1.TaintEffectNoSchedule,
				},
			},
		},
	}

	pod.Spec.Affinity = cutil.ReplaceDaemonSetPodNodeNameNodeAffinity(pod.Spec.Affinity, node.GetName())

	err := client.Create(ctx, pod)
	if err != nil {
		logger.Error(err, fmt.Sprintf("Failed to create %s DMS pod", dmsPodName))
		return err
	}
	logger.V(3).Info(fmt.Sprintf("%s DMS pod created", dmsPodName))
	return nil
}

func ExecuteDMSDebugCmd(ctx context.Context, conn *grpc.ClientConn, command string) (string, error) {
	gnmiClient := gnmi.NewGNMIClient(conn)
	path := &gnmi.Path{
		Elem: []*gnmi.PathElem{
			{Name: "nvidia"},
			{Name: "command", Key: map[string]string{"run": command}},
			{Name: "run"},
		},
	}
	req := &gnmi.GetRequest{
		Path: []*gnmi.Path{path},
	}
	resp, err := gnmiClient.Get(ctx, req)
	if err != nil {
		return "", err
	}

	return resp.GetNotification()[0].GetUpdate()[0].GetVal().GetStringVal(), nil
}

func GenerateDMSTaskName(namespace string, name string) string {
	return fmt.Sprintf("%s/%s", namespace, name)
}
