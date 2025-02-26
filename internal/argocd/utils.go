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
	"fmt"

	dpuservicev1 "github.com/nvidia/doca-platform/api/dpuservice/v1alpha1"
	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"
	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	argoapplication "github.com/nvidia/doca-platform/internal/argocd/api/application"
	argov1 "github.com/nvidia/doca-platform/internal/argocd/api/application/v1alpha1"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
)

const ArgoApplicationFinalizer = "resources-finalizer.argocd.argoproj.io"

func NewAppProject(namespace, name string, clusters []types.NamespacedName) *argov1.AppProject {
	project := argov1.AppProject{
		TypeMeta: metav1.TypeMeta{
			Kind:       argoapplication.AppProjectKind,
			APIVersion: fmt.Sprintf("%v/%v", argoapplication.Group, argoapplication.Version),
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
			ClusterResourceWhitelist: []metav1.GroupKind{
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
			SourceNamespaces:                nil,
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

func NewApplication(namespace, projectName string, dpuService *dpuservicev1.DPUService, values *runtime.RawExtension, clusterName string) *argov1.Application {
	return &argov1.Application{
		TypeMeta: metav1.TypeMeta{
			Kind:       argoapplication.ApplicationKind,
			APIVersion: fmt.Sprintf("%v/%v", argoapplication.Group, argoapplication.Version),
		},
		ObjectMeta: metav1.ObjectMeta{
			// TODO: Revisit this naming.
			Name:      fmt.Sprintf("%v-%v", clusterName, dpuService.Name),
			Namespace: namespace,
			// TODO: Consider adding labels for the Application.
			Labels: map[string]string{
				provisioningv1.DPUClusterLabelKey:        clusterName,
				dpuservicev1.DPUServiceNameLabelKey:      dpuService.Name,
				dpuservicev1.DPUServiceNamespaceLabelKey: dpuService.Namespace,
				operatorv1.DPFComponentLabelKey:          "dpuservice-manager",
			},
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
