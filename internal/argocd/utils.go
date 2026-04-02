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

//nolint:unused
package argocd

import (
	"encoding/json"
	"fmt"

	dpuservicev1 "github.com/nvidia/doca-platform/api/dpuservice/v1alpha1"
	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"
	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/internal/utils"
	"github.com/nvidia/doca-platform/pkg/dpucluster"
	argov1 "github.com/nvidia/doca-platform/third_party/forked/argoproj/argo-cd/pkg/apis/application/v1alpha1"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/clientcmd/api"
)

const (
	ArgoApplicationFinalizer = "resources-finalizer.argocd.argoproj.io"
	AppProjectNameDPU        = "doca-platform-project-dpu"
	AppProjectNameHost       = "doca-platform-project-host"
	ArgoCDSecretLabelKey     = "argocd.argoproj.io/secret-type"
	ArgoCDSecretLabelValue   = "cluster"
)

// GetApplicationName returns the name of an ArgoCD Application for a given cluster and DPUService.
func GetApplicationName(clusterName, dpuServiceName string) string {
	return fmt.Sprintf("%v-%v", clusterName, dpuServiceName)
}

func NewAppProject(namespace, name string, clusters []types.NamespacedName, sourceNamespace string) *argov1.AppProject {
	project := argov1.AppProject{
		TypeMeta: metav1.TypeMeta{
			Kind:       argov1.AppProjectSchemaGroupVersionKind.Kind,
			APIVersion: argov1.AppProjectSchemaGroupVersionKind.GroupVersion().Identifier(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			// TODO: Consider a common set of labels for all objects.
			Labels: map[string]string{
				operatorv1.DPFComponentLabelKey: "dpuservice-manager",
			},
			Annotations:     nil,
			OwnerReferences: nil,
		},
		Spec: argov1.AppProjectSpec{
			SourceRepos:  []string{"*"},
			Destinations: nil,
			Description:  "Installing DPU Services",
			Roles:        nil,
			ClusterResourceWhitelist: []argov1.ClusterResourceRestrictionItem{
				// Required to deploy Cluster-scoped resources to the DPU cluster.
				{Group: "*", Kind: "*"},
			},
			NamespaceResourceBlacklist: nil,
			OrphanedResources: &argov1.OrphanedResourcesMonitorSettings{
				Warn:   nil,
				Ignore: nil,
			},
			SyncWindows:                     nil,
			NamespaceResourceWhitelist:      nil,
			SignatureKeys:                   nil,
			ClusterResourceBlacklist:        nil,
			SourceNamespaces:                []string{sourceNamespace},
			PermitOnlyProjectScopedClusters: false,
		},
	}
	for _, cluster := range clusters {
		project.Spec.Destinations = append(project.Spec.Destinations, argov1.ApplicationDestination{
			Name:      cluster.Name,
			Namespace: "*",
		})
	}
	return &project
}

func NewApplication(namespace, projectName string, dpuService *dpuservicev1.DPUService, values *runtime.RawExtension, clusterName, clusterNamespace string) *argov1.Application {
	labels := map[string]string{
		provisioningv1.DPUClusterNameLabelKey:    clusterName,
		dpuservicev1.DPUServiceNameLabelKey:      dpuService.Name,
		dpuservicev1.DPUServiceNamespaceLabelKey: dpuService.Namespace,
		operatorv1.DPFComponentLabelKey:          "dpuservice-manager",
	}
	// clusterNamespace is not set for DPUService targeting the host cluster.
	if clusterNamespace != "" {
		labels[provisioningv1.DPUClusterNamespaceLabelKey] = clusterNamespace
	}
	return &argov1.Application{
		TypeMeta: metav1.TypeMeta{
			Kind:       argov1.ApplicationSchemaGroupVersionKind.Kind,
			APIVersion: argov1.ApplicationSchemaGroupVersionKind.GroupVersion().Identifier(),
		},
		ObjectMeta: metav1.ObjectMeta{
			// TODO: Revisit this naming to include the namespace of the cluster
			Name:      GetApplicationName(clusterName, dpuService.Name),
			Namespace: namespace,
			// TODO: Consider adding labels for the Application.
			Labels: labels,
			// This finalizer is what enables cascading deletion in ArgoCD.
			Finalizers:  []string{ArgoApplicationFinalizer},
			Annotations: nil,
		},
		Spec: argov1.ApplicationSpec{
			Source: &argov1.ApplicationSource{
				RepoURL:        dpuService.Spec.HelmChart.Source.GetArgoRepoURL(),
				Chart:          dpuService.Spec.HelmChart.Source.Chart,
				Path:           dpuService.Spec.HelmChart.Source.Path,
				TargetRevision: dpuService.Spec.HelmChart.Source.Version,
				Helm: &argov1.ApplicationSourceHelm{
					ReleaseName:  dpuService.Spec.HelmChart.Source.ReleaseName,
					ValuesObject: values,
				},
			},
			SyncPolicy: &argov1.SyncPolicy{
				Automated: &argov1.SyncPolicyAutomated{
					Prune:    true,
					SelfHeal: true,
				},
			},
			Destination: argov1.ApplicationDestination{
				// TODO: We should ensure cluster names are unique.
				Name: clusterName,
				// TODO: Either all resources have namespace defined or else they're deployed to the default namespace. Reconsider this.
				Namespace: dpuService.Namespace,
			},
			Project: projectName,
		},
	}
}

// NewArgoCDClusterSecret generates an ArgoCD cluster secret from the given kubeconfig.
func NewArgoCDClusterSecret(kubeconfig *api.Config, argoCDNamespace string, dpuClusterConfig *dpucluster.Config) (*corev1.Secret, error) {
	name, cluster := utils.GetRandomKVPair(kubeconfig.Clusters)
	if name == "" {
		return nil, fmt.Errorf("no clusters found in kubeconfig")
	}
	userName, user := utils.GetRandomKVPair(kubeconfig.AuthInfos)
	if userName == "" {
		return nil, fmt.Errorf("no users found in kubeconfig")
	}

	secretConfig, err := json.Marshal(
		// This is the format expected and defined by argov1.ClusterConfig, but we only want to marshal a subset.
		map[string]interface{}{
			"tlsClientConfig": map[string]interface{}{
				"caData":   cluster.CertificateAuthorityData,
				"keyData":  user.ClientKeyData,
				"certData": user.ClientCertificateData,
			},
		})
	if err != nil {
		return nil, fmt.Errorf("marshaling DPUCluster secret config for Argo CD: %w", err)
	}

	return &corev1.Secret{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Secret",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			// The secret name is the dpuCluster name. DPUClusters must have unique names.
			Name:      dpuClusterConfig.ClusterNamespaceName(),
			Namespace: argoCDNamespace,
			Labels: map[string]string{
				ArgoCDSecretLabelKey:                       ArgoCDSecretLabelValue,
				provisioningv1.DPUClusterNameLabelKey:      dpuClusterConfig.Cluster.Name,
				provisioningv1.DPUClusterNamespaceLabelKey: dpuClusterConfig.Cluster.Namespace,
			},
			OwnerReferences: nil,
			Annotations:     nil,
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{
			"name":   []byte(dpuClusterConfig.Cluster.Name),
			"server": []byte(cluster.Server),
			"config": secretConfig,
		},
	}, nil
}
