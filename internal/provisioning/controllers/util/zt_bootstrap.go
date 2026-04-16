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

package util

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"time"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	providentity "github.com/nvidia/doca-platform/internal/provisioning/utils/certificate/identity"

	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
	crclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

const (
	ServiceAccountCAPath = "/var/run/secrets/kubernetes.io/serviceaccount/ca.crt"

	bootstrapTokenExpiration = 4 * time.Hour

	LabelDPUName      = "provisioning.dpu.nvidia.com/dpu-name"
	LabelDPUNamespace = "provisioning.dpu.nvidia.com/dpu-namespace"

	// DPUAgentBootstrapGroup is the extra group assigned to bootstrap tokens
	// for DPU agents. It must match the subject group in the ClusterRoleBinding
	// deployed by the operator (config/provisioning/rbac/dpuagent_bootstrap.yaml).
	DPUAgentBootstrapGroup = "system:bootstrappers:dpf:dpu-agent"
)

// CreateDPUAgentRole creates a per-DPU Role that restricts the DPU agent to
// only its own DPU CR and kubeadm join Secret.
func CreateDPUAgentRole(ctx context.Context, client crclient.Client, scheme *runtime.Scheme, dpu *provisioningv1.DPU) error {
	roleName := providentity.DPUAgentUsername(dpu.Name)
	role := &rbacv1.Role{
		ObjectMeta: metav1.ObjectMeta{
			Name:      roleName,
			Namespace: dpu.Namespace,
		},
		Rules: []rbacv1.PolicyRule{
			{
				APIGroups:     []string{"provisioning.dpu.nvidia.com"},
				Resources:     []string{"dpus"},
				ResourceNames: []string{dpu.Name},
				Verbs:         []string{"get"},
			},
			{
				APIGroups:     []string{"provisioning.dpu.nvidia.com"},
				Resources:     []string{"dpus/status"},
				ResourceNames: []string{dpu.Name},
				Verbs:         []string{"patch"},
			},
			{
				APIGroups:     []string{""},
				Resources:     []string{"secrets"},
				ResourceNames: []string{KubeadmJoinSecretName(dpu.Name)},
				Verbs:         []string{"get"},
			},
		},
	}
	if err := controllerutil.SetOwnerReference(dpu, role, scheme); err != nil {
		return fmt.Errorf("setting owner reference on role %s: %w", roleName, err)
	}
	if err := client.Create(ctx, role); err != nil {
		if apierrors.IsAlreadyExists(err) {
			return nil
		}
		return fmt.Errorf("creating role %s: %w", roleName, err)
	}
	return nil
}

// CreateDPUAgentRoleBinding creates a per-DPU RoleBinding that binds the
// certificate username (da-{dpu.name}) to the per-DPU Role.
func CreateDPUAgentRoleBinding(ctx context.Context, client crclient.Client, scheme *runtime.Scheme, dpu *provisioningv1.DPU) error {
	bindingName := providentity.DPUAgentUsername(dpu.Name)
	rb := &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      bindingName,
			Namespace: dpu.Namespace,
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:     rbacv1.UserKind,
				Name:     providentity.DPUAgentUsername(dpu.Name),
				APIGroup: rbacv1.GroupName,
			},
		},
		RoleRef: rbacv1.RoleRef{
			Kind:     "Role",
			Name:     providentity.DPUAgentUsername(dpu.Name),
			APIGroup: rbacv1.GroupName,
		},
	}
	if err := controllerutil.SetOwnerReference(dpu, rb, scheme); err != nil {
		return fmt.Errorf("setting owner reference on rolebinding %s: %w", bindingName, err)
	}
	if err := client.Create(ctx, rb); err != nil {
		if apierrors.IsAlreadyExists(err) {
			return nil
		}
		return fmt.Errorf("creating rolebinding %s: %w", bindingName, err)
	}
	return nil
}

// CreateDPUAgentBootstrapKubeconfig creates a short-lived bootstrap token secret
// in kube-system for the DPU agent and returns a kubeconfig that authenticates
// with that token. If a valid (non-expired) token already exists for this DPU
// (identified by labels), it reuses the existing token.
// caPath is the path to the CA certificate file (typically ServiceAccountCAPath).
func CreateDPUAgentBootstrapKubeconfig(ctx context.Context, client crclient.Client, dpu *provisioningv1.DPU, apiServerAddress, caPath string) ([]byte, error) {
	token, err := createDPUAgentBootstrapToken(ctx, client, dpu)
	if err != nil {
		return nil, err
	}
	return generateBootstrapKubeconfig(apiServerAddress, token, caPath)
}

func createDPUAgentBootstrapToken(ctx context.Context, client crclient.Client, dpu *provisioningv1.DPU) (string, error) {
	selector := labels.SelectorFromSet(labels.Set{
		LabelDPUName:      dpu.Name,
		LabelDPUNamespace: dpu.Namespace,
	})

	secretList := &corev1.SecretList{}
	if err := client.List(ctx, secretList,
		crclient.InNamespace("kube-system"),
		crclient.MatchingLabelsSelector{Selector: selector},
	); err != nil {
		return "", fmt.Errorf("listing bootstrap tokens for DPU %s/%s: %w", dpu.Namespace, dpu.Name, err)
	}

	now := time.Now()
	for i := range secretList.Items {
		s := &secretList.Items[i]
		if s.Type != corev1.SecretTypeBootstrapToken {
			continue
		}
		expStr := string(s.Data["expiration"])
		if expStr == "" {
			continue
		}
		expTime, err := time.Parse(time.RFC3339, expStr)
		if err != nil {
			continue
		}
		if expTime.After(now) {
			id := string(s.Data["token-id"])
			secret := string(s.Data["token-secret"])
			if id != "" && secret != "" {
				return fmt.Sprintf("%s.%s", id, secret), nil
			}
		}
	}

	tokenID, err := generateRandomHex(3)
	if err != nil {
		return "", fmt.Errorf("generating token ID: %w", err)
	}
	tokenSecret, err := generateRandomHex(8)
	if err != nil {
		return "", fmt.Errorf("generating token secret: %w", err)
	}

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("bootstrap-token-%s", tokenID),
			Namespace: "kube-system",
			Labels: map[string]string{
				LabelDPUName:      dpu.Name,
				LabelDPUNamespace: dpu.Namespace,
			},
		},
		Type: corev1.SecretTypeBootstrapToken,
		StringData: map[string]string{
			"token-id":                       tokenID,
			"token-secret":                   tokenSecret,
			"usage-bootstrap-authentication": "true",
			"auth-extra-groups":              DPUAgentBootstrapGroup,
			"expiration":                     now.Add(bootstrapTokenExpiration).Format(time.RFC3339),
		},
	}

	if err := client.Create(ctx, secret); err != nil {
		if apierrors.IsAlreadyExists(err) {
			existing := &corev1.Secret{}
			if getErr := client.Get(ctx, crclient.ObjectKeyFromObject(secret), existing); getErr != nil {
				return "", fmt.Errorf("getting existing bootstrap token: %w", getErr)
			}
			id := string(existing.Data["token-id"])
			sec := string(existing.Data["token-secret"])
			return fmt.Sprintf("%s.%s", id, sec), nil
		}
		return "", fmt.Errorf("creating bootstrap token for DPU %s/%s: %w", dpu.Namespace, dpu.Name, err)
	}

	return fmt.Sprintf("%s.%s", tokenID, tokenSecret), nil
}

// DeleteDPUAgentBootstrapTokens deletes all bootstrap token secrets in
// kube-system that belong to the specified DPU (identified by labels).
func DeleteDPUAgentBootstrapTokens(ctx context.Context, client crclient.Client, dpuName, dpuNamespace string) error {
	selector := labels.SelectorFromSet(labels.Set{
		LabelDPUName:      dpuName,
		LabelDPUNamespace: dpuNamespace,
	})

	secretList := &corev1.SecretList{}
	if err := client.List(ctx, secretList,
		crclient.InNamespace("kube-system"),
		crclient.MatchingLabelsSelector{Selector: selector},
	); err != nil {
		return fmt.Errorf("listing bootstrap tokens for DPU %s/%s: %w", dpuNamespace, dpuName, err)
	}

	for i := range secretList.Items {
		if err := client.Delete(ctx, &secretList.Items[i]); crclient.IgnoreNotFound(err) != nil {
			return fmt.Errorf("deleting bootstrap token %s: %w", secretList.Items[i].Name, err)
		}
	}
	return nil
}

func generateRandomHex(nBytes int) (string, error) {
	b := make([]byte, nBytes)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func generateBootstrapKubeconfig(apiServerAddress, token, caPath string) ([]byte, error) {
	caData, err := os.ReadFile(caPath)
	if err != nil {
		return nil, fmt.Errorf("reading CA certificate from %s: %w", caPath, err)
	}

	kubeconfig := clientcmdapi.Config{
		Clusters: map[string]*clientcmdapi.Cluster{
			"default": {
				Server:                   apiServerAddress,
				CertificateAuthorityData: caData,
			},
		},
		AuthInfos: map[string]*clientcmdapi.AuthInfo{
			"default": {
				Token: token,
			},
		},
		Contexts: map[string]*clientcmdapi.Context{
			"default": {
				Cluster:  "default",
				AuthInfo: "default",
			},
		},
		CurrentContext: "default",
	}

	data, err := clientcmd.Write(kubeconfig)
	if err != nil {
		return nil, fmt.Errorf("marshaling bootstrap kubeconfig: %w", err)
	}
	return data, nil
}
