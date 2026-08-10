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

package util

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	cutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/util"
	"github.com/nvidia/doca-platform/pkg/dpucluster"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// NodeJoinCommandGenerator is an interface for generating join commands for DPU cluster nodes.
type NodeJoinCommandGenerator interface {
	GenerateJoinCommand(ctx context.Context, dc *provisioningv1.DPUCluster, dpu *provisioningv1.DPU) (string, error)
}

// KubeadmBootstrapTokenGenerator is a NodeJoinCommandGenerator that generates join commands following the kubeadm bootstrap token authentication method.
// It creates a bootstrap token secret and returns the join command.
// This join process is based on the kubeadm implementation.
// More details can be found in the kubeadm documentation:
// https://kubernetes.io/docs/reference/setup-tools/kubeadm/kubeadm-init/#bootstrap-token-authentication
type KubeadmBootstrapTokenGenerator struct {
	client.Client
}

// GenerateJoinCommand generates a join command for a DPU cluster node.
// The bootstrap token Secret created in the DPU cluster is labeled with the DPU
// identity so it can be revoked after the node has joined.
func (s *KubeadmBootstrapTokenGenerator) GenerateJoinCommand(ctx context.Context, dc *provisioningv1.DPUCluster, dpu *provisioningv1.DPU) (string, error) {
	// Generate a random 6 character string for the token ID.
	tokenID := make([]byte, 3)
	if _, err := rand.Read(tokenID); err != nil {
		return "", fmt.Errorf("failed to generate token ID: %v", err)
	}
	id := hex.EncodeToString(tokenID)

	// Generate a random 16 character string for the token secret.
	tokenSecret := make([]byte, 8)
	if _, err := rand.Read(tokenSecret); err != nil {
		return "", fmt.Errorf("failed to generate token secret: %v", err)
	}
	secret := hex.EncodeToString(tokenSecret)

	// Create the bootstrap token secret.
	bootstrapToken := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("bootstrap-token-%s", id),
			Namespace: "kube-system",
			Labels: map[string]string{
				cutil.LabelDPUName:      dpu.Name,
				cutil.LabelDPUNamespace: dpu.Namespace,
			},
		},
		Type: corev1.SecretTypeBootstrapToken,
		StringData: map[string]string{
			// This group is created by default when using kamaji clusters.
			"auth-extra-groups": "system:bootstrappers:kubeadm:default-node-token",
			// The bootstrap token will expire 2 hours after creation.
			"expiration":                     time.Now().Add(2 * time.Hour).Format(time.RFC3339),
			"usage-bootstrap-authentication": "true",
			"usage-bootstrap-signing":        "true",
			"description":                    "Bootstrap token for DPU cluster node join",
			"token-id":                       id,
			"token-secret":                   secret,
		},
	}

	clusterConfig := dpucluster.NewConfig(s, dc)
	dpuClusterClient, err := clusterConfig.Client(ctx)
	if err != nil {
		return "", err
	}
	err = dpuClusterClient.Create(ctx, bootstrapToken)
	if err != nil {
		return "", fmt.Errorf("failed to create bootstrap token secret: %v", err)
	}
	server, err := dpucluster.NewConfig(s, dc).Server(ctx)
	if err != nil {
		return "", err
	}
	// Strip the scheme (http:// or https://) from the host if present.
	// This is necessary because the join command expects a host without a scheme.
	server = strings.TrimPrefix(server, "https://")
	server = strings.TrimPrefix(server, "http://")

	// Construct the join command
	joinCommand := fmt.Sprintf("kubeadm join %s --token %s.%s --v=5",
		server,
		id,
		secret)

	// Add the CA certificate hash to the join command.
	caCertHashes, err := clusterConfig.CACertHashes(ctx)
	if err != nil {
		return "", err
	}
	for _, hash := range caCertHashes {
		joinCommand += fmt.Sprintf(" --discovery-token-ca-cert-hash %s", hash)
	}

	return joinCommand, nil
}

// DeleteNodeJoinBootstrapTokens deletes all node-join bootstrap token secrets in
// the DPU cluster kube-system that belong to the specified DPU (identified by labels).
// Only secrets of type SecretTypeBootstrapToken are deleted.
// dpuClusterClient must be a client for the DPU cluster, not the management cluster.
func DeleteNodeJoinBootstrapTokens(ctx context.Context, dpuClusterClient client.Client, dpuName, dpuNamespace string) error {
	selector := labels.SelectorFromSet(labels.Set{
		cutil.LabelDPUName:      dpuName,
		cutil.LabelDPUNamespace: dpuNamespace,
	})

	secretList := &corev1.SecretList{}
	if err := dpuClusterClient.List(ctx, secretList,
		client.InNamespace("kube-system"),
		client.MatchingLabelsSelector{Selector: selector},
	); err != nil {
		return fmt.Errorf("listing node-join bootstrap tokens for DPU %s/%s: %w", dpuNamespace, dpuName, err)
	}

	for i := range secretList.Items {
		s := &secretList.Items[i]
		if s.Type != corev1.SecretTypeBootstrapToken {
			continue
		}
		if err := dpuClusterClient.Delete(ctx, s); client.IgnoreNotFound(err) != nil {
			return fmt.Errorf("deleting node-join bootstrap token %s: %w", s.Name, err)
		}
	}
	return nil
}
