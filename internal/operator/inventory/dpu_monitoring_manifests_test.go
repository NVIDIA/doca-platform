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

package inventory

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	dpuservicev1 "github.com/nvidia/doca-platform/api/dpuservice/v1alpha1"
	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"
	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/internal/release"
	"github.com/nvidia/doca-platform/pkg/dpucluster"

	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/yaml"
)

// testDPUNetworkingHelmChart is the placeholder dpu-networking helm chart reference the inventory
// tests use in place of the one loaded from the release defaults.
const testDPUNetworkingHelmChart = "oci://example.com/dpu-networking:v0.1.0"

// dpuMonitoringTestYAML mirrors manifests/dpu-monitoring.yaml. The values the operator overwrites
// are placeholders, exactly as they are in the embedded manifest.
const dpuMonitoringTestYAML = `apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUService
metadata:
  name: dpu-monitoring
  namespace: dpf-operator-system
spec:
  security:
    privileged: false
  helmChart:
    source:
      repoURL: oci://example.com
      chart: dpu-monitoring-chart
      version: v0.1.0
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceCredentialRequest
metadata:
  name: dpu-monitoring
  namespace: dpf-operator-system
spec:
  duration: 24h
  serviceAccount:
    name: example-serviceaccount-name
    namespace: example-serviceaccount-namespace
  targetCluster:
    name: example-cluster-name
    namespace: example-cluster-namespace
  type: tokenFile
  secret:
    name: example-secret-name
    namespace: example-secret-namespace
`

func dpuMonitoringTestVariables(g *WithT, clusters ...*dpucluster.Config) Variables {
	defaults := &release.Defaults{}
	g.Expect(defaults.Parse()).To(Succeed())
	vars := newDefaultVariables(defaults)
	vars.Namespace = testNamespace
	vars.HelmCharts[operatorv1.DPUMonitoringName] = testDPUNetworkingHelmChart
	vars.DPUClusters = clusters
	return vars
}

func testDPUClusterConfig(name, namespace string) *dpucluster.Config {
	return &dpucluster.Config{
		Cluster: &provisioningv1.DPUCluster{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		},
	}
}

func Test_dpuMonitoringObjects_GenerateManifests(t *testing.T) {
	t.Run("generates only the RBAC DPUService when no DPUCluster exists", func(t *testing.T) {
		g := NewWithT(t)

		component := newDPUMonitoringObjects([]byte(dpuMonitoringTestYAML))
		g.Expect(component.Parse()).To(Succeed())

		objs, err := component.GenerateManifests(context.Background(), dpuMonitoringTestVariables(g))
		g.Expect(err).ToNot(HaveOccurred())

		g.Expect(objs).To(HaveLen(1))
		g.Expect(objs[0].GetObjectKind().GroupVersionKind().Kind).To(Equal(dpuservicev1.DPUServiceKind))
		g.Expect(objs[0].GetName()).To(Equal(operatorv1.DPUMonitoringName.String()))
	})

	t.Run("generates nothing when the component is disabled", func(t *testing.T) {
		g := NewWithT(t)

		vars := dpuMonitoringTestVariables(g, testDPUClusterConfig("cluster-one", "cluster-one-ns"))
		vars.DisableSystemComponents[operatorv1.DPUMonitoringName] = true

		component := newDPUMonitoringObjects([]byte(dpuMonitoringTestYAML))
		g.Expect(component.Parse()).To(Succeed())

		objs, err := component.GenerateManifests(context.Background(), vars)
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(objs).To(BeEmpty())
	})

	t.Run("generates one credential request per DPUCluster and a single RBAC DPUService", func(t *testing.T) {
		g := NewWithT(t)

		clusters := []*dpucluster.Config{
			testDPUClusterConfig("cluster-one", "cluster-one-ns"),
			testDPUClusterConfig("cluster-two", "cluster-two-ns"),
		}

		component := newDPUMonitoringObjects([]byte(dpuMonitoringTestYAML))
		g.Expect(component.Parse()).To(Succeed())

		objs, err := component.GenerateManifests(context.Background(), dpuMonitoringTestVariables(g, clusters...))
		g.Expect(err).ToNot(HaveOccurred())

		credRequestByCluster := map[string]*unstructured.Unstructured{}
		dpuServices := map[string]*unstructured.Unstructured{}
		for _, obj := range objs {
			u, ok := obj.(*unstructured.Unstructured)
			g.Expect(ok).To(BeTrue())
			switch u.GetKind() {
			case dpuservicev1.DPUServiceCredentialRequestKind:
				targetName, found, err := unstructured.NestedString(u.Object, "spec", "targetCluster", "name")
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(found).To(BeTrue())
				credRequestByCluster[targetName] = u
			case dpuservicev1.DPUServiceKind:
				dpuServices[u.GetName()] = u
			default:
				g.Expect(u.GetKind()).To(BeEmpty(), "unexpected object kind generated")
			}
		}

		// This component deploys no workload, so a single RBAC DPUService is generated in
		// total rather than one per DPUCluster.
		g.Expect(dpuServices).To(HaveLen(1))
		rbacService := dpuServices[operatorv1.DPUMonitoringName.String()]
		g.Expect(rbacService).ToNot(BeNil())

		deployInCluster, found, err := unstructured.NestedBool(rbacService.Object, "spec", "deployInCluster")
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(found).To(BeTrue())
		g.Expect(deployInCluster).To(BeFalse(), "the RBAC DPUService must be deployed to the DPU clusters")

		deployDPUManifests, found, err := unstructured.NestedBool(rbacService.Object,
			"spec", "helmChart", "values", operatorv1.DPUMonitoringName.String(), "deployDPUManifests")
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(found).To(BeTrue())
		g.Expect(deployDPUManifests).To(BeTrue())

		// The chart hardcodes the ServiceAccount name and derives its namespace from the
		// release, so the operator passes no subject to it.
		_, found, err = unstructured.NestedFieldNoCopy(rbacService.Object,
			"spec", "helmChart", "values", operatorv1.DPUMonitoringName.String(), "rbac")
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(found).To(BeFalse(), "the operator must not pass rbac values to the dpu-monitoring chart")

		g.Expect(credRequestByCluster).To(HaveLen(len(clusters)))
		for _, cluster := range clusters {
			credReq, ok := credRequestByCluster[cluster.Cluster.Name]
			g.Expect(ok).To(BeTrue(), "expected a DPUServiceCredentialRequest for cluster %s", cluster.Cluster.Name)

			// The token type and lifetime come from the manifest and are not edited.
			credType, _, err := unstructured.NestedString(credReq.Object, "spec", "type")
			g.Expect(err).ToNot(HaveOccurred())
			g.Expect(credType).To(Equal(string(dpuservicev1.SecretTypeTokenFile)))

			duration, _, err := unstructured.NestedString(credReq.Object, "spec", "duration")
			g.Expect(err).ToNot(HaveOccurred())
			g.Expect(duration).To(Equal("24h"))

			// The credential request itself, and the ServiceAccount it creates in the DPU
			// cluster, live in the DPF operator namespace.
			g.Expect(credReq.GetNamespace()).To(Equal(testNamespace))
			saNamespace, _, err := unstructured.NestedString(credReq.Object, "spec", "serviceAccount", "namespace")
			g.Expect(err).ToNot(HaveOccurred())
			g.Expect(saNamespace).To(Equal(testNamespace))

			// The ServiceAccount is created in the DPU cluster the request targets, so every
			// cluster uses the same unsuffixed name.
			saName, _, err := unstructured.NestedString(credReq.Object, "spec", "serviceAccount", "name")
			g.Expect(err).ToNot(HaveOccurred())
			g.Expect(saName).To(Equal(operatorv1.DPUMonitoringName.String()))

			// The token Secret is created in the DPUCluster namespace instead, because
			// Prometheus resolves the credentials of a ServiceMonitor in the namespace of
			// that ServiceMonitor, and the cluster manager creates the control plane
			// ServiceMonitors alongside the DPUCluster.
			secretNamespace, _, err := unstructured.NestedString(credReq.Object, "spec", "secret", "namespace")
			g.Expect(err).ToNot(HaveOccurred())
			g.Expect(secretNamespace).To(Equal(cluster.Cluster.Namespace))

			secretName, _, err := unstructured.NestedString(credReq.Object, "spec", "secret", "name")
			g.Expect(err).ToNot(HaveOccurred())
			g.Expect(secretName).To(Equal(DPUMonitoringSecretName(cluster.Cluster.Name, cluster.Cluster.Namespace)))

			targetNamespace, _, err := unstructured.NestedString(credReq.Object, "spec", "targetCluster", "namespace")
			g.Expect(err).ToNot(HaveOccurred())
			g.Expect(targetNamespace).To(Equal(cluster.Cluster.Namespace))
		}
	})
}

// TestDPUMonitoringChartServiceAccountName asserts the ServiceAccount name hardcoded in the
// dpu-monitoring chart matches the one the DPF Operator puts in the DPUServiceCredentialRequest.
// The chart binds that name to system:monitoring in every DPU cluster, so if the two drift apart
// Prometheus silently loses access to the DPU cluster control plane metrics.
func TestDPUMonitoringChartServiceAccountName(t *testing.T) {
	g := NewWithT(t)

	valuesPath := filepath.Join("..", "..", "..", "deploy", "charts", "dpu-networking", "charts", "dpu-monitoring", "values.yaml")
	data, err := os.ReadFile(valuesPath)
	g.Expect(err).ToNot(HaveOccurred())

	var chartValues struct {
		RBAC struct {
			ServiceAccountName string `json:"serviceAccountName"`
		} `json:"rbac"`
	}
	g.Expect(yaml.Unmarshal(data, &chartValues)).To(Succeed())

	g.Expect(chartValues.RBAC.ServiceAccountName).To(Equal(operatorv1.DPUMonitoringName.String()),
		"the ServiceAccount name in %s must match operatorv1.DPUMonitoringName", valuesPath)
}

// Test_DPUMonitoringSecretName asserts the Secret name is stable and distinct per DPUCluster.
// The cluster manager relies on this to reference the Secret from the ServiceMonitor it creates.
func Test_DPUMonitoringSecretName(t *testing.T) {
	g := NewWithT(t)

	name := DPUMonitoringSecretName("cluster-one", "cluster-one-ns")
	g.Expect(name).To(Equal(DPUMonitoringSecretName("cluster-one", "cluster-one-ns")))
	g.Expect(name).To(HavePrefix(operatorv1.DPUMonitoringName.String() + "-credentials-"))
	g.Expect(len(name)).To(BeNumerically("<=", 63))

	// The namespace participates in the hash, so identically named clusters in different
	// namespaces do not collide.
	g.Expect(name).ToNot(Equal(DPUMonitoringSecretName("cluster-one", "other-ns")))
	g.Expect(name).ToNot(Equal(DPUMonitoringSecretName("cluster-two", "cluster-one-ns")))
}
