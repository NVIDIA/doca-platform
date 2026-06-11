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

package inventory

import (
	"context"
	"fmt"
	"testing"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/internal/argocd"
	"github.com/nvidia/doca-platform/internal/features"
	"github.com/nvidia/doca-platform/internal/release"
	"github.com/nvidia/doca-platform/pkg/dpucluster"
	argov1 "github.com/nvidia/doca-platform/third_party/forked/argoproj/argo-cd/pkg/apis/application/v1alpha1"

	. "github.com/onsi/gomega"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
	featuregatetesting "k8s.io/component-base/featuregate/testing"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestDPUServiceControllerManifestsParse(t *testing.T) {
	g := NewWithT(t)
	dpuServiceMainfests := &dpuServiceControllerObjects{data: dpuServiceData}
	g.Expect(dpuServiceMainfests.Parse()).ToNot(HaveOccurred())

	// make sure we found expected objects
	foundByKind := map[ObjectKind]*unstructured.Unstructured{}
	for _, o := range dpuServiceMainfests.objects {
		foundByKind[ObjectKind(o.GetKind())] = o
	}

	g.Expect(foundByKind).To(HaveKey(DeploymentKind))
	g.Expect(foundByKind).To(HaveKey(RoleKind))
	g.Expect(foundByKind).To(HaveKey(RoleBindingKind))
	g.Expect(foundByKind).To(HaveKey(ServiceAccountKind))
	g.Expect(foundByKind).To(HaveKey(ClusterRoleKind))
	g.Expect(foundByKind).To(HaveKey(ClusterRoleBindingKind))
	g.Expect(foundByKind).To(HaveKey(ValidatingWebhookConfigurationKind))
	g.Expect(foundByKind).To(HaveKey(ServiceKind))
	g.Expect(foundByKind).To(HaveKey(CertificateKind))
	g.Expect(foundByKind).To(HaveKey(IssuerKind))

	// make sure no namespace and crd obj
	g.Expect(foundByKind).ToNot(HaveKey(CustomResourceDefinitionKind))
	g.Expect(foundByKind).ToNot(HaveKey(NamespaceKind))

	// ensure no additional object kinds
	g.Expect(foundByKind).To(HaveLen(10))

	// ensure objects parse to concrete type
	g.Expect(runtime.DefaultUnstructuredConverter.FromUnstructured(foundByKind[DeploymentKind].UnstructuredContent(), &appsv1.Deployment{})).ToNot(HaveOccurred())
	g.Expect(runtime.DefaultUnstructuredConverter.FromUnstructured(foundByKind[RoleKind].UnstructuredContent(), &rbacv1.Role{})).ToNot(HaveOccurred())
	g.Expect(runtime.DefaultUnstructuredConverter.FromUnstructured(foundByKind[RoleBindingKind].UnstructuredContent(), &rbacv1.RoleBinding{})).ToNot(HaveOccurred())
	g.Expect(runtime.DefaultUnstructuredConverter.FromUnstructured(foundByKind[ServiceAccountKind].UnstructuredContent(), &corev1.ServiceAccount{})).ToNot(HaveOccurred())
	g.Expect(runtime.DefaultUnstructuredConverter.FromUnstructured(foundByKind[ClusterRoleKind].UnstructuredContent(), &rbacv1.ClusterRole{})).ToNot(HaveOccurred())
	g.Expect(runtime.DefaultUnstructuredConverter.FromUnstructured(foundByKind[ClusterRoleBindingKind].UnstructuredContent(), &rbacv1.ClusterRoleBinding{})).ToNot(HaveOccurred())
	g.Expect(runtime.DefaultUnstructuredConverter.FromUnstructured(foundByKind[ValidatingWebhookConfigurationKind].UnstructuredContent(), &admissionregistrationv1.ValidatingWebhookConfiguration{})).ToNot(HaveOccurred())
	g.Expect(runtime.DefaultUnstructuredConverter.FromUnstructured(foundByKind[ServiceKind].UnstructuredContent(), &corev1.Service{})).ToNot(HaveOccurred())
}

func TestDPUServiceControllerManifestSetFlag(t *testing.T) {
	g := NewGomegaWithT(t)

	defaults := release.NewDefaults()
	g.Expect(defaults.Parse()).To(Succeed())

	dpuserviceCtrl := &dpuServiceControllerObjects{
		data: dpuServiceData,
	}
	g.Expect(dpuserviceCtrl.Parse()).To(Succeed())

	t.Run("test toggling DPUReady taints in DPUService controller", func(t *testing.T) {
		vars := newDefaultVariables(defaults)

		generatedObjs, err := dpuserviceCtrl.GenerateManifests(context.Background(), vars)
		g.Expect(err).NotTo(HaveOccurred())

		deployment := getDeploymentFromGeneratedObjs(g, generatedObjs)

		g.Expect(deployment).NotTo(BeNil())
		g.Expect(deployment.Spec.Template.Spec.Containers).To(HaveLen(1))
		g.Expect(deployment.Spec.Template.Spec.Containers[0].Args).To(ContainElement("--disable-dpu-ready-taints=false"))

		// Disable DPUReady taints and check the flag is set in the deployment.
		vars.DisableDPUReadyTaints = true
		generatedObjs, err = dpuserviceCtrl.GenerateManifests(context.Background(), vars)
		g.Expect(err).NotTo(HaveOccurred())

		deployment = getDeploymentFromGeneratedObjs(g, generatedObjs)
		g.Expect(deployment.Spec.Template.Spec.Containers[0].Args).To(ContainElement("--disable-dpu-ready-taints=true"))
	})

	t.Run("test propagating feature gates to DPUService controller", func(t *testing.T) {
		featuregatetesting.SetFeatureGateDuringTest(t, features.MutableGates, features.PrivilegedPodEnforcement, false)
		vars := newDefaultVariables(defaults)

		generatedObjs, err := dpuserviceCtrl.GenerateManifests(context.Background(), vars)
		g.Expect(err).NotTo(HaveOccurred())

		deployment := getDeploymentFromGeneratedObjs(g, generatedObjs)
		g.Expect(deployment).NotTo(BeNil())
		g.Expect(deployment.Spec.Template.Spec.Containers[0].Args).To(ContainElement("--feature-gates=PrivilegedPodEnforcement=false"))
	})
}

func getDeploymentFromGeneratedObjs(g Gomega, generatedObjs []client.Object) *appsv1.Deployment {
	var deployment *appsv1.Deployment
	for _, obj := range generatedObjs {
		if obj.GetObjectKind().GroupVersionKind().Kind == string(DeploymentKind) {
			deploy := &appsv1.Deployment{}
			unstructuredObj, ok := obj.(*unstructured.Unstructured)
			g.Expect(ok).To(BeTrue())
			err := runtime.DefaultUnstructuredConverter.FromUnstructured(unstructuredObj.UnstructuredContent(), deploy)
			g.Expect(err).NotTo(HaveOccurred())
			deployment = deploy
			break
		}
	}
	return deployment
}

// makeFakeKamajiSecret creates a Secret containing a syntactically-valid kubeconfig (with placeholder
// TLS data) in the format that the Kamaji controller produces and that dpucluster.Config.Kubeconfig
// expects.
func makeFakeKamajiSecret(cluster provisioningv1.DPUCluster) (*corev1.Secret, error) {
	config := &clientcmdapi.Config{
		Clusters: map[string]*clientcmdapi.Cluster{
			cluster.Name: {
				Server:                   "https://fake-server:6443",
				CertificateAuthorityData: []byte("fake-ca"),
			},
		},
		AuthInfos: map[string]*clientcmdapi.AuthInfo{
			"user": {
				ClientKeyData:         []byte("fake-key"),
				ClientCertificateData: []byte("fake-cert"),
			},
		},
	}
	confData, err := clientcmd.Write(*config)
	if err != nil {
		return nil, err
	}
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-admin-kubeconfig", cluster.Name),
			Namespace: cluster.Namespace,
		},
		Data: map[string][]byte{
			"super-admin.conf": confData,
		},
	}, nil
}

func TestGenerateArgoCDClusterSecrets(t *testing.T) {
	const argoCDNamespace = "argocd"

	tests := []struct {
		name           string
		clusterNames   []string
		missingSecret  string // cluster name for which no kamaji secret is created
		corruptSecret  string // cluster name whose kubeconfig is replaced with invalid data
		wantErr        bool
		wantCount      int
		wantArgoLabels bool
	}{
		{
			name:           "all clusters have valid secrets",
			clusterNames:   []string{"cluster-one", "cluster-two", "cluster-three"},
			wantErr:        false,
			wantCount:      3,
			wantArgoLabels: true,
		},
		{
			name:          "one cluster missing its kamaji secret",
			clusterNames:  []string{"cluster-four", "cluster-five", "cluster-six"},
			missingSecret: "cluster-six",
			wantErr:       false,
			wantCount:     2,
		},
		{
			name:          "one cluster has a malformed kubeconfig",
			clusterNames:  []string{"cluster-seven", "cluster-eight", "cluster-nine"},
			corruptSecret: "cluster-nine",
			wantErr:       true,
			wantCount:     2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			ctx := context.Background()
			ns := fmt.Sprintf("test-ns-%s", tt.name)

			clusters := make([]provisioningv1.DPUCluster, 0, len(tt.clusterNames))
			for _, name := range tt.clusterNames {
				clusters = append(clusters, provisioningv1.DPUCluster{
					ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
					Spec: provisioningv1.DPUClusterSpec{
						Type:       "kamaji",
						Kubeconfig: fmt.Sprintf("%s-admin-kubeconfig", name),
					}})
			}

			secrets := make([]client.Object, 0, len(clusters))
			for _, cluster := range clusters {
				if cluster.Name == tt.missingSecret {
					continue
				}
				s, err := makeFakeKamajiSecret(cluster)
				g.Expect(err).ToNot(HaveOccurred())
				if cluster.Name == tt.corruptSecret {
					s.Data["super-admin.conf"] = []byte("just-a-field")
				}
				secrets = append(secrets, s)
			}

			fakeClient := fake.NewClientBuilder().WithScheme(scheme.Scheme).WithObjects(secrets...).Build()
			configs := make([]*dpucluster.Config, 0, len(clusters))
			for i := range clusters {
				configs = append(configs, dpucluster.NewConfig(fakeClient, &clusters[i]))
			}

			d := &dpuServiceControllerObjects{}
			objs, err := d.generateArgoCDClusterSecrets(ctx, argoCDNamespace, nil, configs)
			if tt.wantErr {
				g.Expect(err).To(HaveOccurred())
			} else {
				g.Expect(err).ToNot(HaveOccurred())
			}
			g.Expect(objs).To(HaveLen(tt.wantCount))
			for _, obj := range objs {
				s, ok := obj.(*corev1.Secret)
				g.Expect(ok).To(BeTrue())
				g.Expect(s.Data).To(HaveKey("config"))
				g.Expect(s.Data).To(HaveKey("name"))
				g.Expect(s.Data).To(HaveKey("server"))
				if tt.wantArgoLabels {
					g.Expect(s.Labels).To(HaveKey(argocd.ArgoCDSecretLabelKey))
				}
			}
		})
	}
}

func TestGenerateArgoCDProjects(t *testing.T) {
	const argoCDNamespace = "argocd"
	const appNS = "dpf-operator-system"
	labelsToAdd := map[string]string{"test-label": "test-value"}

	makeDPUClusterConfig := func(ns, name string) *dpucluster.Config {
		cluster := provisioningv1.DPUCluster{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
		}
		return dpucluster.NewConfig(nil, &cluster)
	}

	t.Run("two clusters produce DPU and host AppProjects with correct destinations", func(t *testing.T) {
		g := NewWithT(t)
		configs := []*dpucluster.Config{
			makeDPUClusterConfig("ns-a", "cluster-a"),
			makeDPUClusterConfig("ns-b", "cluster-b"),
		}
		d := &dpuServiceControllerObjects{}
		objs := d.generateArgoCDProjects(argoCDNamespace, labelsToAdd, configs, appNS)

		g.Expect(objs).To(HaveLen(2))

		dpuProject, ok := objs[0].(*argov1.AppProject)
		g.Expect(ok).To(BeTrue())
		g.Expect(dpuProject.Name).To(Equal(argocd.AppProjectNameDPU))
		g.Expect(dpuProject.Namespace).To(Equal(argoCDNamespace))
		g.Expect(dpuProject.Spec.Destinations).To(HaveLen(2))
		destinationNames := []string{dpuProject.Spec.Destinations[0].Name, dpuProject.Spec.Destinations[1].Name}
		g.Expect(destinationNames).To(ConsistOf("cluster-a", "cluster-b"))

		hostProject, ok := objs[1].(*argov1.AppProject)
		g.Expect(ok).To(BeTrue())
		g.Expect(hostProject.Name).To(Equal(argocd.AppProjectNameHost))
		g.Expect(hostProject.Spec.Destinations).To(HaveLen(1))
		g.Expect(hostProject.Spec.Destinations[0].Name).To(Equal("in-cluster"))
	})

	t.Run("sourceNamespaces are propagated to both AppProjects", func(t *testing.T) {
		g := NewWithT(t)
		d := &dpuServiceControllerObjects{}
		objs := d.generateArgoCDProjects(argoCDNamespace, nil, nil, appNS)

		g.Expect(objs).To(HaveLen(2))
		for _, obj := range objs {
			proj, ok := obj.(*argov1.AppProject)
			g.Expect(ok).To(BeTrue())
			g.Expect(proj.Spec.SourceNamespaces).To(ConsistOf(appNS))
		}
	})

	t.Run("labels are applied to both AppProjects", func(t *testing.T) {
		g := NewWithT(t)
		d := &dpuServiceControllerObjects{}
		objs := d.generateArgoCDProjects(argoCDNamespace, labelsToAdd, nil, argoCDNamespace)

		g.Expect(objs).To(HaveLen(2))
		for _, obj := range objs {
			g.Expect(obj.GetLabels()).To(HaveKeyWithValue("test-label", "test-value"))
		}
	})

	t.Run("empty cluster list produces DPU AppProject with no destinations", func(t *testing.T) {
		g := NewWithT(t)
		d := &dpuServiceControllerObjects{}
		objs := d.generateArgoCDProjects(argoCDNamespace, nil, nil, argoCDNamespace)

		g.Expect(objs).To(HaveLen(2))
		dpuProject, ok := objs[0].(*argov1.AppProject)
		g.Expect(ok).To(BeTrue())
		g.Expect(dpuProject.Spec.Destinations).To(BeEmpty())
	})
}
