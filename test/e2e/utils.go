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

package e2e

import (
	"context"

	dpuservicev1 "github.com/nvidia/doca-platform/api/dpuservice/v1alpha1"
	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"
	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	kamajiv1 "github.com/nvidia/doca-platform/third_party/api/kamaji/api/v1alpha1"

	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var (

	// deployPrometheus indicates whether metrics are collected by Prometheus.
	// When set to true, Prometheus metrics will be verified.
	deployPrometheus = false
	// deployKSM indicates whether metrics are collected by KSM.
	// When set to true, KMS metrics will be verified for the resources.
	deployKSM        = false
	dpuClusterClient client.Client
	testRESTClient   *rest.RESTClient
	metricsURI       string
	cleanupLabels    = map[string]string{"dpf-operator-e2e-test-cleanup": "true"}
	labelSelector    = labels.SelectorFromSet(cleanupLabels)
	// helmRegistry holds the Helm registry in which the artifacts used in e2e are pushed
	helmRegistry = ""
	// tag holds the tag which the artifacts used in e2e are using
	tag = ""
	// Labels and resources targeted for cleanup before running our e2e tests.
	// This cleanup is typically handled by cleanupObjs, but if an e2e test fails, the standard cleanup may not be executed.
	resourcesToDelete = []client.ObjectList{
		&dpuservicev1.DPUDeploymentList{},
		&dpuservicev1.DPUServiceCredentialRequestList{},
		&dpuservicev1.DPUServiceList{},
		&dpuservicev1.DPUServiceConfigurationList{},
		&dpuservicev1.DPUServiceTemplateList{},
		&provisioningv1.DPUSetList{},
		&provisioningv1.DPUList{},
		&provisioningv1.BFBList{},
		&provisioningv1.DPUClusterList{},
		&dpuservicev1.DPUServiceIPAMList{},
		&dpuservicev1.DPUServiceChainList{},
		&dpuservicev1.DPUServiceInterfaceList{},
		&kamajiv1.TenantControlPlaneList{},
		&operatorv1.DPFOperatorConfigList{},
		&appsv1.DeploymentList{},
		&appsv1.DaemonSetList{},
		&corev1.PersistentVolumeClaimList{},
		&corev1.NamespaceList{},
		&corev1.NodeList{},
		&corev1.ServiceList{},
	}
)

const (
	configName                 = "dpfoperatorconfig"
	dpfOperatorSystemNamespace = "dpf-operator-system"
	argoCDInstanceLabel        = "argocd.argoproj.io/instance"
)

func createDPUObj[T client.Object](ctx context.Context, testClient client.Client, name, ns string, obj T) {
	obj.SetName(name)
	obj.SetNamespace(ns)
	obj.SetLabels(cleanupLabels)
	Expect(testClient.Create(ctx, obj)).To(Succeed())
}

func createTestNamespace(ctx context.Context, testClient client.Client, namespace string) {
	testNS := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}}
	testNS.SetLabels(cleanupLabels)
	Expect(testClient.Create(ctx, testNS)).To(Succeed())
}
