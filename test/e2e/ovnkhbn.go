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
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strings"
	"time"

	dpuservicev1 "github.com/nvidia/doca-platform/api/dpuservice/v1alpha1"
	"github.com/nvidia/doca-platform/test/utils"
	"github.com/nvidia/doca-platform/test/utils/dpuservice"

	maintenancev1alpha1 "github.com/Mellanox/maintenance-operator/api/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	machineryruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func WaitForOVNKHBNDeploymentReady(ctx context.Context, input *SystemTestInput) {
	dpuservice.WaitForDPUDeploymentReady(ctx, input.Client, DPFOperatorSystemNamespace, []string{"ovn-hbn"}, 50*time.Minute)
}

// DeployOVNKHBNScenario creates the application-layer objects required for the HBN-OVN scenario:
// physical DPUServiceInterfaces (p0, p1, ovn), IPAM pools, HBN DPUServiceTemplate,
// DPUServiceConfiguration, OVN-K DPUServiceTemplate, DPUServiceConfiguration,
// and the ovn-hbn DPUDeployment.
// It must be called after applyConfig/applySDNConfig have run (to populate input fields).
func DeployOVNKHBNScenario(ctx context.Context, input *SystemTestInput) {
	for _, iface := range input.DPUServiceInterfacesHBN {
		By(fmt.Sprintf("Creating physical DPUServiceInterface %s for HBN uplink", iface.Name))
		Expect(client.IgnoreAlreadyExists(input.Client.Create(ctx,
			utils.GenerateDPUObj(iface.GetName(), input.Namespace, iface.DeepCopy(), CleanupScope.Suite),
		))).ToNot(HaveOccurred())
	}

	if input.DPUServiceInterfaceOVN != nil {
		By("Creating patch DPUServiceInterface for OVN-K connectivity")
		Expect(client.IgnoreAlreadyExists(input.Client.Create(ctx,
			utils.GenerateDPUObj(input.DPUServiceInterfaceOVN.GetName(), input.Namespace, input.DPUServiceInterfaceOVN.DeepCopy(), CleanupScope.Suite),
		))).ToNot(HaveOccurred())
	}

	if input.OVNCredentialRequest != nil {
		By("Creating DPUServiceCredentialRequest for OVN")
		Expect(client.IgnoreAlreadyExists(input.Client.Create(ctx,
			utils.GenerateDPUObj(input.OVNCredentialRequest.GetName(), input.Namespace, input.OVNCredentialRequest.DeepCopy(), CleanupScope.Suite),
		))).ToNot(HaveOccurred())
	}

	By("Creating CIDR pool DPUServiceIPAM for HBN")
	Expect(client.IgnoreAlreadyExists(input.Client.Create(ctx,
		utils.GenerateDPUObj(input.CIDRDPUServiceIPAM.GetName(), input.Namespace, input.CIDRDPUServiceIPAM.DeepCopy(), CleanupScope.Suite),
	))).ToNot(HaveOccurred())

	By("Creating subnet pool DPUServiceIPAM for HBN")
	Expect(client.IgnoreAlreadyExists(input.Client.Create(ctx,
		utils.GenerateDPUObj(input.IPPoolDPUServiceIPAM.GetName(), input.Namespace, input.IPPoolDPUServiceIPAM.DeepCopy(), CleanupScope.Suite),
	))).ToNot(HaveOccurred())

	if input.DPUServiceTemplateOVN != nil {
		By("Creating DPUServiceTemplate for OVN-K")
		Expect(client.IgnoreAlreadyExists(input.Client.Create(ctx,
			utils.GenerateDPUObj(input.DPUServiceTemplateOVN.GetName(), input.Namespace, input.DPUServiceTemplateOVN.DeepCopy(), CleanupScope.Suite),
		))).ToNot(HaveOccurred())
	}

	if input.DPUServiceTemplateHBN != nil {
		By("Creating DPUServiceTemplate for HBN")
		Expect(client.IgnoreAlreadyExists(input.Client.Create(ctx,
			utils.GenerateDPUObj(input.DPUServiceTemplateHBN.GetName(), input.Namespace, input.DPUServiceTemplateHBN.DeepCopy(), CleanupScope.Suite),
		))).ToNot(HaveOccurred())
	}

	if input.DPUServiceConfigurationHBN != nil {
		By("Creating DPUServiceConfiguration for HBN")
		Expect(client.IgnoreAlreadyExists(input.Client.Create(ctx,
			utils.GenerateDPUObj(input.DPUServiceConfigurationHBN.GetName(), input.Namespace, input.DPUServiceConfigurationHBN.DeepCopy(), CleanupScope.Suite),
		))).ToNot(HaveOccurred())
	}

	if input.DPUServiceConfigurationOVN != nil {
		By("Creating DPUServiceConfiguration for OVN-K")
		Expect(client.IgnoreAlreadyExists(input.Client.Create(ctx,
			utils.GenerateDPUObj(input.DPUServiceConfigurationOVN.GetName(), input.Namespace, applyOVNClusterValues(ctx, input), CleanupScope.Suite),
		))).ToNot(HaveOccurred())
	}

	By("Creating ovn-hbn DPUDeployment")
	Expect(client.IgnoreAlreadyExists(input.Client.Create(ctx,
		utils.GenerateDPUObj(input.DPUDeployment.GetName(), input.Namespace, input.DPUDeployment.DeepCopy(), CleanupScope.Suite),
	))).ToNot(HaveOccurred())
}

// applyOVNClusterValues returns a copy of the OVN DPUServiceConfiguration with network values
// extracted from the live cluster: k8sAPIServer from restConfig, pod/service CIDRs from
// kube-controller-manager, vtepCIDR and ipamPool from the cidr DPUServiceIPAM, and
// hostCIDR derived from the control plane node IP.
func applyOVNClusterValues(ctx context.Context, input *SystemTestInput) *dpuservicev1.DPUServiceConfiguration {
	result := input.DPUServiceConfigurationOVN.DeepCopy()

	if result.Spec.ServiceConfiguration.HelmChart.Values == nil {
		result.Spec.ServiceConfiguration.HelmChart.Values = &machineryruntime.RawExtension{}
	}

	values := make(map[string]interface{})
	if result.Spec.ServiceConfiguration.HelmChart.Values.Raw != nil {
		Expect(json.Unmarshal(result.Spec.ServiceConfiguration.HelmChart.Values.Raw, &values)).To(Succeed())
	}

	setVal := func(value string, path ...string) {
		if value == "" {
			return
		}
		current := values
		for _, k := range path[:len(path)-1] {
			if m, ok := current[k].(map[string]interface{}); ok {
				current = m
			} else {
				next := make(map[string]interface{})
				current[k] = next
				current = next
			}
		}
		current[path[len(path)-1]] = value
	}

	// k8sAPIServer from the kubeconfig used by the test
	setVal(input.RestConfig.Host, "k8sAPIServer")

	// pod and service CIDRs from kube-controller-manager
	podCIDR, serviceCIDR := extractClusterCIDRs(ctx, input.Client)
	setVal(podCIDR, "podNetwork")
	setVal(serviceCIDR, "serviceNetwork")

	// vtepCIDR and ipamPool from the cidr DPUServiceIPAM already loaded
	if input.CIDRDPUServiceIPAM != nil {
		setVal(input.CIDRDPUServiceIPAM.Name, "dpuManifests", "ipamPool")
		if input.CIDRDPUServiceIPAM.Spec.IPV4Network != nil {
			setVal(input.CIDRDPUServiceIPAM.Spec.IPV4Network.Network, "dpuManifests", "vtepCIDR")
		}
	}

	// hostCIDR derived from the control plane node IP
	controlPlaneIP := getClusterControlPlaneIP(ctx, input.Client)
	setVal(deriveHostCIDR(controlPlaneIP), "dpuManifests", "hostCIDR")

	raw, err := json.Marshal(values)
	Expect(err).ToNot(HaveOccurred())
	result.Spec.ServiceConfiguration.HelmChart.Values.Raw = raw

	return result
}

// extractClusterCIDRs returns the pod and service CIDRs from the kube-controller-manager pod args.
func extractClusterCIDRs(ctx context.Context, c client.Client) (podCIDR, serviceCIDR string) {
	pods := &corev1.PodList{}
	Expect(c.List(ctx, pods,
		client.InNamespace("kube-system"),
		client.MatchingLabels{"component": "kube-controller-manager"},
	)).To(Succeed())
	Expect(pods.Items).NotTo(BeEmpty(), "kube-controller-manager pod not found")
	Expect(pods.Items[0].Spec.Containers).NotTo(BeEmpty(), "kube-controller-manager pod has no containers")
	for _, arg := range pods.Items[0].Spec.Containers[0].Command {
		if strings.HasPrefix(arg, "--cluster-cidr=") {
			podCIDR = strings.TrimPrefix(arg, "--cluster-cidr=")
		}
		if strings.HasPrefix(arg, "--service-cluster-ip-range=") {
			serviceCIDR = strings.TrimPrefix(arg, "--service-cluster-ip-range=")
		}
	}
	return
}

// deriveHostCIDR returns the /24 CIDR containing the given IP (e.g. "10.0.110.1" → "10.0.110.0/24").
func deriveHostCIDR(ip string) string {
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return ""
	}
	mask := net.CIDRMask(24, 32)
	network := parsed.Mask(mask)
	if network == nil {
		return ""
	}
	return (&net.IPNet{IP: network, Mask: mask}).String()
}

// InstallOVNKResourceInjector installs the OVN-K resource injection webhook on the host cluster
// as a separate Helm release. The webhook mutates pods scheduled to DPU-enabled worker nodes,
// injecting VF resource requests so OVN-K can obtain the device ID for DPU offload.
// Reads OVN_KUBERNETES_REPO_URL and OVN_KUBERNETES_CHART_TAG env vars for the chart source.
func InstallOVNKResourceInjector(ctx context.Context, c client.Client) {
	const (
		ovnKubernetesNamespace      = "ovn-kubernetes"
		resourceInjectorReleaseName = "ovn-kubernetes-resource-injector"
	)

	repoURL, found := os.LookupEnv("OVN_KUBERNETES_REPO_URL")
	Expect(found).To(BeTrue(), "OVN_KUBERNETES_REPO_URL env var must be set")
	chartTag, found := os.LookupEnv("OVN_KUBERNETES_CHART_TAG")
	Expect(found).To(BeTrue(), "OVN_KUBERNETES_CHART_TAG env var must be set")

	By("Verifying ovn-kubernetes namespace exists before helm install")
	ns := &corev1.Namespace{}
	Expect(c.Get(ctx, client.ObjectKey{Name: ovnKubernetesNamespace}, ns)).To(Succeed(),
		"ovn-kubernetes namespace must exist before installing resource injector (created by dpf-bootstrap)")

	By("Installing OVN-K resource injector webhook on the host cluster")
	cmd := exec.Command("helm", "upgrade", "--install",
		"-n", ovnKubernetesNamespace,
		resourceInjectorReleaseName,
		repoURL+"/ovn-kubernetes-chart",
		"--version", chartTag,
		"--set", resourceInjectorReleaseName+".enabled=true",
	)
	cmd.Stdout = GinkgoWriter
	cmd.Stderr = GinkgoWriter
	Expect(cmd.Run()).To(Succeed(), "helm upgrade --install for %s failed", resourceInjectorReleaseName)

	By("Verifying OVN-K resource injector deployment is ready")
	Eventually(func(g Gomega) {
		deployment := &appsv1.Deployment{}
		g.Expect(c.Get(ctx, client.ObjectKey{
			Namespace: ovnKubernetesNamespace,
			Name:      resourceInjectorReleaseName,
		}, deployment)).To(Succeed())
		g.Expect(deployment.Status.ReadyReplicas).To(Equal(*deployment.Spec.Replicas))
	}).WithTimeout(2*time.Minute).Should(Succeed(), "%s deployment did not become ready", resourceInjectorReleaseName)
}

// SetMaintenanceOperatorMaxParallelOperations patches the MaintenanceOperatorConfig to allow
// up to value concurrent node drain operations. The default DPF value is 60% which limits
// parallelism to ~60% of cluster nodes. Returns a restore function that reverts to the original value.
func SetMaintenanceOperatorMaxParallelOperations(ctx context.Context, c client.Client, value int32) func() {
	cfg := &maintenancev1alpha1.MaintenanceOperatorConfig{}
	Expect(c.Get(ctx, client.ObjectKey{Name: "default", Namespace: DPFOperatorSystemNamespace}, cfg)).To(Succeed())

	original := cfg.Spec.MaxParallelOperations

	patch := client.MergeFrom(cfg.DeepCopy())
	v := intstr.FromInt32(value)
	cfg.Spec.MaxParallelOperations = &v
	Expect(c.Patch(ctx, cfg, patch)).To(Succeed())

	return func() {
		cfg2 := &maintenancev1alpha1.MaintenanceOperatorConfig{}
		Expect(c.Get(ctx, client.ObjectKey{Name: "default", Namespace: DPFOperatorSystemNamespace}, cfg2)).To(Succeed())
		patch2 := client.MergeFrom(cfg2.DeepCopy())
		cfg2.Spec.MaxParallelOperations = original
		Expect(c.Patch(ctx, cfg2, patch2)).To(Succeed())
	}
}
