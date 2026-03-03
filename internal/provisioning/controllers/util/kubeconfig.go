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
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
	crclient "sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	DPUAgentTokenSecretName = "dpf-provisioning-dpuagent-zerotrust-token"
)

// GenerateKubeconfig creates a kubeconfig for the dpu-agent ServiceAccount.
// It reads the long-lived token and CA certificate from the pre-deployed
// service-account-token Secret and combines them with the API server URL.
func GenerateKubeconfig(ctx context.Context, client crclient.Client, apiServerAddress, namespace string) ([]byte, error) {
	tokenSecret := &corev1.Secret{}
	if err := client.Get(ctx, types.NamespacedName{Name: DPUAgentTokenSecretName, Namespace: namespace}, tokenSecret); err != nil {
		return nil, fmt.Errorf("getting token secret %s/%s: %w", namespace, DPUAgentTokenSecretName, err)
	}

	token, ok := tokenSecret.Data["token"]
	if !ok || len(token) == 0 {
		return nil, fmt.Errorf("token not found in secret %s/%s", namespace, DPUAgentTokenSecretName)
	}

	caData, ok := tokenSecret.Data["ca.crt"]
	if !ok || len(caData) == 0 {
		return nil, fmt.Errorf("ca.crt not found in secret %s/%s", namespace, DPUAgentTokenSecretName)
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
				Token: string(token),
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
		return nil, fmt.Errorf("marshaling kubeconfig: %w", err)
	}
	return data, nil
}
