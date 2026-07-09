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
	"sort"
	"time"

	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"
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
	"k8s.io/utils/ptr"
	crclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

const (
	ServiceAccountCAPath = "/var/run/secrets/kubernetes.io/serviceaccount/ca.crt"

	bootstrapTokenExpiration = 4 * time.Hour

	LabelDPUName      = DPUProvisioningPrefix + "dpu-name"
	LabelDPUNamespace = DPUProvisioningPrefix + "dpu-namespace"

	// DPUAgentBootstrapGroup is the extra group assigned to bootstrap tokens
	// for DPU agents. It must match the subject group in the ClusterRoleBinding
	// deployed by the operator (config/provisioning/rbac/dpuagent_bootstrap.yaml).
	DPUAgentBootstrapGroup = "system:bootstrappers:dpf:dpu-agent"
)

// CreateDPUAgentRole creates or updates a per-DPU Role that restricts the DPU
// agent to only its own DPU CR and required bootstrap resources.
func CreateDPUAgentRole(ctx context.Context, client crclient.Client, scheme *runtime.Scheme, dpu *provisioningv1.DPU, flavor *provisioningv1.DPUFlavor) error {
	roleName := providentity.DPUAgentUsername(dpu.Name)
	rules := []rbacv1.PolicyRule{
		{
			APIGroups:     []string{ProvisioningGroupName},
			Resources:     []string{"dpus"},
			ResourceNames: []string{dpu.Name},
			Verbs:         []string{"get"},
		},
		{
			APIGroups:     []string{ProvisioningGroupName},
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
		{
			APIGroups:     []string{ProvisioningGroupName},
			Resources:     []string{"bluefieldsoftwares"},
			ResourceNames: []string{ptr.Deref(dpu.Spec.BlueFieldSoftware, "")},
			Verbs:         []string{"get"},
		},
	}
	configMapNames := referencedConfigMapNames(flavor)
	if len(configMapNames) > 0 {
		rules = append(rules, rbacv1.PolicyRule{
			APIGroups:     []string{""},
			Resources:     []string{"configmaps"},
			ResourceNames: configMapNames,
			Verbs:         []string{"get"},
		})
	}

	key := crclient.ObjectKey{Name: roleName, Namespace: dpu.Namespace}
	role := &rbacv1.Role{}
	if err := client.Get(ctx, key, role); err != nil {
		if !apierrors.IsNotFound(err) {
			return fmt.Errorf("getting role %s: %w", roleName, err)
		}
		role = &rbacv1.Role{
			ObjectMeta: metav1.ObjectMeta{
				Name:      roleName,
				Namespace: dpu.Namespace,
			},
			Rules: rules,
		}
		if err := controllerutil.SetOwnerReference(dpu, role, scheme); err != nil {
			return fmt.Errorf("setting owner reference on role %s: %w", roleName, err)
		}
		if err := client.Create(ctx, role); err != nil {
			if !apierrors.IsAlreadyExists(err) {
				return fmt.Errorf("creating role %s: %w", roleName, err)
			}
			// Race: the role may appear between Get(NotFound) and Create.
			// Re-get and continue with update path below.
			if err := client.Get(ctx, key, role); err != nil {
				return fmt.Errorf("re-getting role %s after already exists: %w", roleName, err)
			}
		} else {
			return nil
		}
	}

	role.Rules = rules
	if err := controllerutil.SetOwnerReference(dpu, role, scheme); err != nil {
		return fmt.Errorf("setting owner reference on role %s: %w", roleName, err)
	}
	if err := client.Update(ctx, role); err != nil {
		return fmt.Errorf("updating role %s: %w", roleName, err)
	}
	return nil
}

func referencedConfigMapNames(flavor *provisioningv1.DPUFlavor) []string {
	if flavor == nil {
		return nil
	}
	seen := map[string]struct{}{}
	names := make([]string, 0, len(flavor.Spec.ConfigFiles))
	for _, file := range flavor.Spec.ConfigFiles {
		if file.Type == nil || *file.Type != provisioningv1.ConfigFileTypeAgentApplied {
			continue
		}
		if file.ContentFrom == nil || file.ContentFrom.ConfigMapKeyRef == nil {
			continue
		}
		name := file.ContentFrom.ConfigMapKeyRef.Name
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// CreateDPUAgentRoleBinding creates a per-DPU RoleBinding that binds the given
// subject username to the per-DPU Role. subjectName is the certificate username
// (da-{dpu.name}) for bootstrap-token DPUs or the literal SPIFFE-ID URI for
// SPIFFE-mode DPUs (the subject swap is the only RBAC delta for SPIFFE).
func CreateDPUAgentRoleBinding(ctx context.Context, client crclient.Client, scheme *runtime.Scheme, dpu *provisioningv1.DPU, subjectName string) error {
	bindingName := providentity.DPUAgentUsername(dpu.Name)
	rb := &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      bindingName,
			Namespace: dpu.Namespace,
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:     rbacv1.UserKind,
				Name:     subjectName,
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

// GenerateBootstrapKubeconfig returns a kubeconfig that authenticates with the
// provided bootstrap token. proxyURL, if non-empty, is written into the cluster
// stanza so clients route requests through that proxy.
func GenerateBootstrapKubeconfig(apiServerAddress, token string, caData []byte, proxyURL string) ([]byte, error) {
	cluster := &clientcmdapi.Cluster{
		Server:                   apiServerAddress,
		CertificateAuthorityData: caData,
	}
	if proxyURL != "" {
		cluster.ProxyURL = proxyURL
	}

	kubeconfig := clientcmdapi.Config{
		Clusters: map[string]*clientcmdapi.Cluster{
			"default": cluster,
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

// CreateDPUAgentBootstrapToken creates or reuses a short-lived bootstrap token
// secret in kube-system for the DPU agent. If a valid (non-expired) token
// already exists for this DPU (identified by labels), it reuses it.
func CreateDPUAgentBootstrapToken(ctx context.Context, client crclient.Client, dpu *provisioningv1.DPU) (string, error) {
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

// defaultKubernetesServicePort returns the KUBERNETES_SERVICE_PORT environment
// variable from the provisioning controller. Returns empty string if unset.
func defaultKubernetesServicePort() string {
	return os.Getenv("KUBERNETES_SERVICE_PORT")
}

// ResolveAPIServerAddress builds the API server address and optional proxy URL
// for the dpu-agent bootstrap kubeconfig.
//
// For zero-trust (redfish) mode both VIP and Port must be configured;
// otherwise an error is returned.
//
// For trusted-host mode the address is composed with fallbacks:
//   - host: VIP if configured, else "kubernetes.default.svc"
//   - port: Port if configured, else KUBERNETES_SERVICE_PORT env var
//
// The second return value is the proxy URL: for trusted-host mode it is always
// the hostagent forward proxy address; for zero-trust mode it is empty.
func ResolveAPIServerAddress(overrides *operatorv1.Overrides, isZeroTrust bool) (apiServerAddress, proxyURL string, err error) {
	const (
		defaultHost     = "kubernetes.default.svc"
		trustedProxyURL = "http://[fe80::1%25tmfifo_net0]:11030"
	)

	if isZeroTrust {
		if overrides == nil || overrides.KubernetesAPIServerVIP == nil || overrides.KubernetesAPIServerPort == nil {
			return "", "", fmt.Errorf("KubernetesAPIServerVIP and KubernetesAPIServerPort must be set in DPFOperatorConfig for zero-trust mode")
		}
		addr := fmt.Sprintf("https://%s:%d", *overrides.KubernetesAPIServerVIP, *overrides.KubernetesAPIServerPort)
		return addr, "", nil
	}

	host := defaultHost
	if overrides != nil && overrides.KubernetesAPIServerVIP != nil {
		host = *overrides.KubernetesAPIServerVIP
	}

	var port string
	if overrides != nil && overrides.KubernetesAPIServerPort != nil {
		port = fmt.Sprintf("%d", *overrides.KubernetesAPIServerPort)
	} else {
		port = defaultKubernetesServicePort()
	}
	if port == "" {
		port = "443"
	}

	addr := fmt.Sprintf("https://%s:%s", host, port)
	return addr, trustedProxyURL, nil
}
