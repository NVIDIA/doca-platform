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
	"time"

	dnutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/dpunode/util"
	cutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/util"
	"github.com/nvidia/doca-platform/internal/release"

	"github.com/openconfig/gnmi/proto/gnmi"
	"google.golang.org/grpc"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	dmsPath                string = "/opt/mellanox/doca/services/dms/dmsd"
	username               string = "admin"
	password               string = "admin"
	issuerKind             string = "Issuer"
	provisioningIssuerName string = "dpf-provisioning-issuer" // this issuer is created by provisioning manifest
	dmsServerIP            string = "0.0.0.0"
	dmsInitError           string = "rshim is installed on host which is not supported. Please remove the rshim package from the host"
	dmsServerPort          int32  = 9339
	dmsConfDir             string = "/opt/dpf/dms"
	dmsCertDir             string = dmsConfDir + "/certs"
	dmsLibDir              string = "/var/lib/dpf/dms"
)

const (
	DMSImageFolder        string = "/bfb"
	DMSClientSecret       string = "dpf-provisioning-client-secret"
	DMSInitScript         string = "/opt/dpf/dmsinit.sh"
	HostNetworkScript     string = "/opt/dpf/hostnetwork.sh"
	DMSServiceAccountName string = "dpf-provisioning-dms-service-account"
	DMSContainerName      string = "dms"
)

func createServerCertificate(ctx context.Context, client client.Client, name string, namespace string, secretName string, commonName string, issuerRef map[string]interface{}, ipAddresses []interface{}) error {
	cert := &unstructured.Unstructured{}
	cert.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "cert-manager.io",
		Version: "v1",
		Kind:    "Certificate",
	})
	cert.SetName(name)
	cert.SetNamespace(namespace)
	err := unstructured.SetNestedMap(cert.Object, map[string]interface{}{
		"secretName":  secretName,
		"duration":    metav1.Duration{Duration: 365 * 24 * time.Hour}.ToUnstructured(),
		"renewBefore": metav1.Duration{Duration: 365 * 12 * time.Hour}.ToUnstructured(),
		"issuerRef":   issuerRef,
		"usages": []interface{}{
			"server auth",
		},
		"commonName":  commonName,
		"ipAddresses": ipAddresses,
	}, "spec")
	if err != nil {
		return fmt.Errorf("failed to set spec to Certificate: %w", err)
	}

	nn := types.NamespacedName{
		Namespace: namespace,
		Name:      name,
	}

	existingCert := &unstructured.Unstructured{}
	existingCert.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "cert-manager.io",
		Version: "v1",
		Kind:    "Certificate",
	})
	if err := client.Get(ctx, nn, existingCert); err != nil {
		if apierrors.IsNotFound(err) {
			err = client.Create(ctx, cert)
			if err != nil {
				return err
			}
			return nil
		}
		return fmt.Errorf("failed to get Certificate: %v", err)
	}
	return nil
}

func CreateDMSPod(ctx context.Context, client client.Client, node *corev1.Node, option dnutil.DMSPodOptions, namespace string, dpfOperatorConfigOwnerRef *metav1.OwnerReference) error {
	logger := log.FromContext(ctx)
	dmsPodName := cutil.GenerateDMSPodName(node)

	dmsServerSecretName := cutil.GenerateDMSServerSecretName(node.Name)
	dmsServerCertName := cutil.GenerateDMSServerCertName(node.Name)

	issuerRef := map[string]interface{}{
		"name": provisioningIssuerName,
		"kind": issuerKind,
	}

	if len(node.Status.Addresses) == 0 {
		return fmt.Errorf("no IP addresses found in node %v status", node)
	}
	nodeInternalIP := node.Status.Addresses[0].Address

	// Create server certificate with Server Issuer
	if err := createServerCertificate(ctx, client, dmsServerCertName, namespace, dmsServerSecretName, dmsServerCertName, issuerRef, []interface{}{nodeInternalIP}); err != nil {
		logger.Error(err, "Failed to create Server certificate", "dms", err)
		return err
	}

	rebootParams := ""
	if node.Labels != nil {
		if v, ok := node.Labels[cutil.DPUNodeRebootMethodLabel]; ok {
			rebootParams = fmt.Sprintf("--node-reboot-method %s", v)
		}
		if v, ok := node.Labels[cutil.DPUNodeScriptNameLabel]; ok {
			rebootParams = fmt.Sprintf("%s --script-name %s", rebootParams, v)
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

	hostPathType := corev1.HostPathDirectory
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      dmsPodName,
			Namespace: namespace,
			Labels: map[string]string{
				cutil.ProvisioningComponentLabelKey: "dms",
				release.DPFVersionLabelKey:          release.DPFVersion(),
			},
			OwnerReferences: []metav1.OwnerReference{*dpfOperatorConfigOwnerRef},
		},
		Spec: corev1.PodSpec{
			ServiceAccountName: DMSServiceAccountName,
			HostNetwork:        true,
			InitContainers: []corev1.Container{
				{
					Name:            "rshim-preflight",
					Image:           option.DMSImageWithTag,
					ImagePullPolicy: corev1.PullIfNotPresent,
					SecurityContext: &corev1.SecurityContext{
						Privileged: ptr.To(true),
					},
					Env:     extraEnvs,
					Command: []string{"/bin/bash", "-c", "--"},
					Args: []string{
						fmt.Sprintf("%s --cmd check-rshim-not-occupied", DMSInitScript),
					},
					VolumeMounts: []corev1.VolumeMount{
						{
							Name:      "sys",
							MountPath: "/sys",
						},
						{
							Name:      "dev",
							MountPath: "/dev",
						},
						{
							Name:      "lib-modules",
							MountPath: "/lib/modules",
							ReadOnly:  true,
						},
					},
				},
				{
					Name:            "dms-init",
					Image:           option.DMSImageWithTag,
					ImagePullPolicy: corev1.PullIfNotPresent,
					SecurityContext: &corev1.SecurityContext{
						Privileged: ptr.To(true),
					},
					Env:     extraEnvs,
					Command: []string{"/bin/bash", "-c", "--"},
					Args: []string{
						fmt.Sprintf("%s --cmd register --dms-conf-dir %s --dms-image-dir %s --kube-node-ref %s --dms-port %d --dms-ip %s --external-certificate TODO %s",
							DMSInitScript, dmsConfDir, DMSImageFolder, node.GetName(), dmsServerPort, nodeInternalIP, rebootParams),
					},
					VolumeMounts: []corev1.VolumeMount{
						{
							Name:      "sys",
							MountPath: "/sys",
						},
						{
							Name:      "dev",
							MountPath: "/dev",
						},
						{
							Name:      "dms-config",
							MountPath: dmsConfDir,
						},
						{
							Name:      "lib-modules",
							MountPath: "/lib/modules",
							ReadOnly:  true,
						},
					},
				},
			},
			Containers: []corev1.Container{
				{
					Name:            DMSContainerName,
					Image:           option.DMSImageWithTag,
					ImagePullPolicy: corev1.PullIfNotPresent,
					Ports: []corev1.ContainerPort{
						{
							ContainerPort: dmsServerPort,
						},
					},
					VolumeMounts: []corev1.VolumeMount{
						{
							Name:      "server-volume",
							MountPath: dmsCertDir,
							ReadOnly:  true,
						},
						{
							Name:      "bfb",
							MountPath: DMSImageFolder,
						},
						{
							Name:      "dev",
							MountPath: "/dev",
						},
						{
							Name:      "dms-config",
							MountPath: dmsConfDir,
						},
						{
							Name:      "dms-lib",
							MountPath: dmsLibDir,
						},
						{
							Name:      "lib-modules",
							MountPath: "/lib/modules",
							ReadOnly:  true,
						},
					},
					SecurityContext: &corev1.SecurityContext{
						Privileged: ptr.To(true),
					},
					Env:     extraEnvs,
					Command: []string{"/bin/bash", "-c", "--"},
					Args: []string{
						fmt.Sprintf("./rshim.sh && %s $(cat %s/dms.conf)", dmsPath, dmsConfDir),
					},
					StartupProbe: &corev1.Probe{
						ProbeHandler: corev1.ProbeHandler{
							Exec: &corev1.ExecAction{
								Command: []string{"/bin/bash", "-c", "df -h | grep /bfb"}, // Verify NFS server is available
							},
						},
						TimeoutSeconds:   1,
						FailureThreshold: 30,
						PeriodSeconds:    10,
					},
				},
				{
					Name:            "configure-vf",
					Image:           option.DMSImageWithTag,
					ImagePullPolicy: corev1.PullIfNotPresent,
					SecurityContext: &corev1.SecurityContext{
						Privileged: ptr.To(true),
					},
					Env:     extraEnvs,
					Command: []string{"/bin/bash", "-c", "--"},
					//Args:    []string{fmt.Sprintf("%s --restore-vf", HostNetworkScript)},
					Args: []string{fmt.Sprintf(
						`while true; do
							%s --restore-vf;
							sleep 5;
						done`, HostNetworkScript),
					},
					VolumeMounts: []corev1.VolumeMount{
						{
							Name:      "dms-lib",
							MountPath: dmsLibDir,
						},
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
					},
				},
			},
			ImagePullSecrets: option.ImagePullSecrets,
			// TODO: add a Volume for DMS Server certificate that will be populated by the DMS Init container and shared with the DMS container
			Volumes: []corev1.Volume{
				{
					Name: "server-volume",
					VolumeSource: corev1.VolumeSource{
						Secret: &corev1.SecretVolumeSource{
							SecretName: dmsServerSecretName,
						},
					},
				},
				{
					Name: "bfb",
					VolumeSource: corev1.VolumeSource{
						PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
							ClaimName: option.BFBPVC,
						},
					},
				},
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
					Name: "dms-config",
					VolumeSource: corev1.VolumeSource{
						EmptyDir: &corev1.EmptyDirVolumeSource{},
					},
				},
				{
					Name: "dms-lib",
					VolumeSource: corev1.VolumeSource{
						HostPath: &corev1.HostPathVolumeSource{
							Path: dmsLibDir,
							Type: ptr.To(corev1.HostPathDirectoryOrCreate),
						},
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
