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
	"time"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	dutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/util"
	cutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/util"

	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	cmmeta "github.com/cert-manager/cert-manager/pkg/apis/meta/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
)

const (
	DMSImageFolder  string = "/bfb"
	DMSClientSecret string = "dpf-provisioning-client-secret"
)

func Address(ip string) string {
	return fmt.Sprintf("%s:%d", ip, dmsServerPort)
}

func createServerCertificate(ctx context.Context, client client.Client, name string, namespace string, secretName string, commonName string, issuerRef cmmeta.ObjectReference, owner *metav1.OwnerReference, ipAddresses []string) error {
	cert := &certmanagerv1.Certificate{
		ObjectMeta: metav1.ObjectMeta{
			Name:            name,
			Namespace:       namespace,
			OwnerReferences: []metav1.OwnerReference{*owner},
		},
		Spec: certmanagerv1.CertificateSpec{
			SecretName:  secretName,
			Duration:    &metav1.Duration{Duration: 24 * time.Hour},
			RenewBefore: &metav1.Duration{Duration: 8 * time.Hour},
			IssuerRef:   issuerRef,
			Usages: []certmanagerv1.KeyUsage{
				certmanagerv1.UsageServerAuth,
			},
			CommonName:  commonName,
			IPAddresses: ipAddresses,
		},
	}

	nn := types.NamespacedName{
		Namespace: namespace,
		Name:      name,
	}

	existingCert := &certmanagerv1.Certificate{}
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

func CreateDMSPod(ctx context.Context, client client.Client, dpu *provisioningv1.DPU, option dutil.DPUOptions) error {
	logger := log.FromContext(ctx)
	dmsPodName := cutil.GenerateDMSPodName(dpu.Name)

	owner := metav1.NewControllerRef(dpu,
		provisioningv1.GroupVersion.WithKind("DPU"))

	dmsServerSecretName := cutil.GenerateDMSServerSecretName(dpu.Name)
	dmsServerCertName := cutil.GenerateDMSServerCertName(dpu.Name)

	issuerRef := cmmeta.ObjectReference{
		Name: provisioningIssuerName,
		Kind: issuerKind,
	}

	nn := types.NamespacedName{
		Namespace: "",
		Name:      dpu.Spec.NodeName,
	}
	node := &corev1.Node{}
	if err := client.Get(ctx, nn, node); err != nil {
		return err
	}
	if len(node.Status.Addresses) == 0 {
		return fmt.Errorf("no IP addresses found in node %v status", node)
	}
	nodeInternalIP := node.Status.Addresses[0].Address

	// Create server certificate with Server Issuer
	if err := createServerCertificate(ctx, client, dmsServerCertName, dpu.Namespace, dmsServerSecretName, dmsServerCertName, issuerRef, owner, []string{nodeInternalIP}); err != nil {
		logger.Error(err, "Failed to create Server certificate", "dms", err)
		return err
	}

	removePrefix := true
	pciAddress, err := cutil.GetPCIAddrFromLabel(dpu.Labels, removePrefix)
	if err != nil {
		logger.Error(err, "Failed to get pci address from node label", "dms", err)
		return err
	}

	logger.V(3).Info(fmt.Sprintf("create %s DMS pod", dmsPodName))
	dmsCommand := fmt.Sprintf("./rshim.sh && %s -bind_address %s:%d -v 99 -auth cert -ca /etc/ssl/certs/server/ca.crt -tls_key_file /etc/ssl/certs/server/tls.key -tls_cert_file /etc/ssl/certs/server/tls.crt -password %s -username %s -image_folder %s -target_pci %s -exec_timeout %d -disable_unbind_at_activate -reboot_status_check none",
		dmsPath, dmsServerIP, dmsServerPort, password, username, DMSImageFolder, pciAddress, option.DMSTimeout)

	// Check if rshim is installed on the host for the target PCI address.
	// If rshim is installed, then exit with an error message to prevent DMS pod creation.
	// The error message will be used in a condition for the DPU resource.
	rshimPreflightCommand := fmt.Sprintf(`ls /dev | egrep 'rshim.*[0-9]+' | while read dev; do if echo 'DISPLAY_LEVEL 1' > /dev/$dev/misc && grep -q %s /dev/$dev/misc; then echo -n "%s" > /dev/termination-log; exit 1; fi; done`, pciAddress, dmsInitError)

	// THIS IS ONLY A TEMPORARY WORKAROUND AND WILL BE REMOVED WITH THE JANUARY RELEASE!
	// This workaround addresses a firmware protection mechanism issue where devices
	// are not automatically recovered after a fatal error.
	// Fatal errors can occur during a SW_RESET, which is triggered by the DMS Pod (e.g., bfb-install).
	//
	// Workflow:
	// 1. Set the `grace_period` to 0 to bypass the firmware protection mechanism and ensure devices are recovered.
	// 2. Explicitly trigger a recovery operation on each device before proceeding with BF installation.
	//
	// Notes:
	// - Physical Functions (PFs) must be handled before Virtual Functions (VFs) to avoid errors.
	//   Improved sorting ensures PFs are processed first based on their device names.
	// - We will only handle `p0` and `p1` PFs (representing the first two functions of the PCI device).
	//   This is consistent with the behavior of the hostnetwork Pod, which also limits its handling to these PFs.
	// - All VFs associated with these PFs will also be handled and recovered, but only after their corresponding PFs
	//   have been successfully processed to ensure a stable recovery sequence.
	//
	devlinkGracePeriodCommand := fmt.Sprintf(`readlink /sys/bus/pci/devices/0000:%s.[01] /sys/bus/pci/devices/0000:%s.[01]/virtfn* | xargs -n1 basename | sort -u | while read pci_device; do devlink health set pci/$pci_device reporter fw_fatal grace_period 0; devlink health recover pci/$pci_device reporter fw_fatal; done`, pciAddress, pciAddress)

	hostPathType := corev1.HostPathDirectory
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:            dmsPodName,
			Namespace:       dpu.Namespace,
			OwnerReferences: []metav1.OwnerReference{*owner},
		},
		Spec: corev1.PodSpec{
			HostNetwork: true,
			InitContainers: []corev1.Container{
				{
					Name:            "rshim-preflight",
					Image:           option.DMSImageWithTag,
					ImagePullPolicy: corev1.PullIfNotPresent,
					SecurityContext: &corev1.SecurityContext{
						Privileged: ptr.To(true),
					},
					Command: []string{"/bin/bash", "-c"},
					Args: []string{
						rshimPreflightCommand,
					},
				},
				{
					Name:            "devlink-grace-period",
					Image:           option.DMSImageWithTag,
					ImagePullPolicy: corev1.PullIfNotPresent,
					SecurityContext: &corev1.SecurityContext{
						Privileged: ptr.To(true),
					},
					Command: []string{"/bin/bash", "-xc"},
					Args: []string{
						devlinkGracePeriodCommand,
					},
				},
			},
			Containers: []corev1.Container{
				{
					Name:            "dms",
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
							MountPath: "/etc/ssl/certs/server",
							ReadOnly:  true,
						},
						{
							Name:      "bfb",
							MountPath: "/bfb",
						},
						{
							Name:      "dev",
							MountPath: "/dev",
						},
					},
					SecurityContext: &corev1.SecurityContext{
						Privileged: ptr.To(true),
					},
					Command: []string{"/bin/bash", "-c", "--"},
					Args: []string{
						dmsCommand,
					},
				},
			},
			ImagePullSecrets: option.ImagePullSecrets,
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
			},
		},
	}

	pod.Spec.Affinity = cutil.ReplaceDaemonSetPodNodeNameNodeAffinity(
		pod.Spec.Affinity, dpu.Spec.NodeName)
	pod.Spec.Tolerations = cutil.GeneratePodToleration(*dpu.Spec.NodeEffect)

	err = client.Create(ctx, pod)
	if err != nil {
		logger.Error(err, fmt.Sprintf("Failed to create %s DMS pod", dmsPodName))
		return err
	}
	logger.V(3).Info(fmt.Sprintf("%s DMS pod created", dmsPodName))
	return nil
}
