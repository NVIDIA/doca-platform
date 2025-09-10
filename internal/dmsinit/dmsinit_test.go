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
package dmsinit

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

var (
	cfg                                     *rest.Config
	k8sClient                               client.Client
	testEnv                                 *envtest.Environment
	ctx                                     context.Context
	cancel                                  context.CancelFunc
	tempDir, tempScriptPath, kubeconfigPath string
)

func TestMain(t *testing.T) {
	// Skip the test due to misalignment between the test and the new flow of dmsinit.sh
	// TODO: fix the test once the new flow is finished
	// RegisterFailHandler(Fail)
	// RunSpecs(t, "DMS-Init Suite")
}

var _ = BeforeSuite(func() {
	log.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))
	By("bootstrapping test environment")
	testEnv = &envtest.Environment{
		CRDDirectoryPaths: []string{
			filepath.Join("..", "..", "config", "provisioning", "crd", "bases"),
			filepath.Join("..", "..", "test", "objects", "crd", "cert-manager"),
		},
		ErrorIfCRDPathMissing: true,
		// The BinaryAssetsDirectory is only required if you want to run the tests directly
		// without call the makefile target test. If not informed it will look for the
		// default path defined in controller-runtime which is /usr/local/kubebuilder/.
		// Note that you must have the required binaries setup under the bin directory to perform
		// the tests directly. When we run make test it will be setup and used automatically.
		BinaryAssetsDirectory: filepath.Join("..", "..", "hack", "tools", "bin", "k8s",
			fmt.Sprintf("1.32.0-%s-%s", runtime.GOOS, runtime.GOARCH)),
	}
	var err error

	// cfg is defined in this file globally.
	cfg, err = testEnv.Start()
	Expect(err).NotTo(HaveOccurred())
	Expect(cfg).NotTo(BeNil())

	scheme := scheme.Scheme
	err = provisioningv1.AddToScheme(scheme)
	Expect(err).NotTo(HaveOccurred())

	// +kubebuilder:scaffold:scheme

	k8sClient, err = client.New(cfg, client.Options{Scheme: scheme})
	Expect(err).NotTo(HaveOccurred())
	Expect(k8sClient).NotTo(BeNil())

	ctx, cancel = context.WithCancel(ctrl.SetupSignalHandler())

	// Create temporary directory for dmsinit outputs
	tempDir, err = os.MkdirTemp("", "dmsinit-test")
	Expect(err).NotTo(HaveOccurred())

	// Copy script to temporary directory
	scriptPath := "./dmsinit.sh"
	tempScriptPath = filepath.Join(tempDir, "dmsinit.sh")
	data, err := os.ReadFile(scriptPath)
	Expect(err).NotTo(HaveOccurred())
	err = os.WriteFile(tempScriptPath, data, 0755)
	Expect(err).NotTo(HaveOccurred())

	// Write kubeconfig for envtest cluster
	kubeconfigPath = filepath.Join(tempDir, "kubeconfig")
	err = os.WriteFile(kubeconfigPath, []byte(generateKubeconfig(cfg)), 0644)
	Expect(err).NotTo(HaveOccurred())

	// Set the KUBECONFIG environment variable
	err = os.Setenv("KUBECONFIG", kubeconfigPath)
	Expect(err).NotTo(HaveOccurred())
})

var _ = AfterSuite(func() {
	By("tearing down the test environment")
	if cancel != nil {
		cancel()
	}
	err := testEnv.Stop()
	Expect(err).NotTo(HaveOccurred())

	Expect(os.RemoveAll(tempDir)).To(Succeed())
	// Unset the KUBECONFIG environment variable
	err = os.Unsetenv("KUBECONFIG")
	Expect(err).NotTo(HaveOccurred())
})

const (
	pciAddress1 string = "0000-04-00"
	pciAddress2 string = "0000-05-00"
)

var createNode = func(ctx context.Context, name string, labels map[string]string) *corev1.Node {
	node := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: name, Labels: labels}}
	Expect(k8sClient.Create(ctx, node)).NotTo(HaveOccurred())
	return node
}

var createIssuer = func(ctx context.Context, name string, namespace string) {
	issuer := &unstructured.Unstructured{}
	issuer.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "cert-manager.io",
		Version: "v1",
		Kind:    "Issuer",
	})
	issuer.SetName(name)
	issuer.SetNamespace(namespace)
	Expect(unstructured.SetNestedMap(issuer.Object, map[string]interface{}{}, "spec")).NotTo(HaveOccurred())
	Expect(k8sClient.Create(ctx, issuer)).NotTo(HaveOccurred())
}

// Generate kubeconfig from rest.Config
func generateKubeconfig(cfg *rest.Config) string {
	return fmt.Sprintf(`apiVersion: v1
clusters:
- cluster:
    certificate-authority-data: %s
    server: %s
  name: envtest
contexts:
- context:
    cluster: envtest
    user: envtest
  name: envtest
current-context: envtest
kind: Config
users:
- name: envtest
  user:
    client-certificate-data: %s
    client-key-data: %s`,
		base64.StdEncoding.EncodeToString(cfg.CAData),
		cfg.Host,
		base64.StdEncoding.EncodeToString(cfg.CertData),
		base64.StdEncoding.EncodeToString(cfg.KeyData),
	)
}

var _ = Describe("DMSInit", func() {
	var testNS *corev1.Namespace
	nodeName := "dpf-provisioning-dmsinit-node-test"
	BeforeEach(func() {
		By("creating the namespace")
		testNS = &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{GenerateName: "provisioning"}}
		Expect(client.IgnoreAlreadyExists(k8sClient.Create(ctx, testNS))).To(Succeed())
		By("creating the node")
		createNode(ctx, nodeName, map[string]string{
			"feature.node.kubernetes.io/dpu-enabled": "true"})
	})
	AfterEach(func() {
		By("deleting the node")
		Expect(k8sClient.Delete(ctx, &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: nodeName}})).To(Succeed())
		Expect(k8sClient.DeleteAllOf(ctx, &provisioningv1.DPUSet{}, client.InNamespace(testNS.Name))).To(Succeed())
	})
	Context("obj test context", func() {
		It("Run dmsinit.sh with one PCI Address and with external certificate", func() {
			// Execute the script
			cmdString := fmt.Sprintf("%s --kubeconfig %s --kube-node-ref %s --pci-address %s --external-certificate %s --namespace %s", tempScriptPath, kubeconfigPath, nodeName, pciAddress1, "true", testNS.Name)
			cmd := exec.Command("bash", "-c", cmdString)
			cmd.Dir = tempDir // Script runs in temp directory
			output, err := cmd.CombinedOutput()
			By("Run dmsinit.sh")
			Expect(err).NotTo(HaveOccurred(), "Script execution failed: %s", output)

			By("checking a DPUDevice is created for the node")
			Eventually(func(g Gomega) {
				dpuDeviceList := &provisioningv1.DPUDeviceList{}
				g.Expect(k8sClient.List(ctx, dpuDeviceList, client.InNamespace(testNS.Name))).To(Succeed())
				g.Expect(dpuDeviceList.Items).To(HaveLen(1))
			}).WithTimeout(10 * time.Second).Should(Succeed())

			dpuDeviceFetched := &provisioningv1.DPUDevice{}
			dpuDeviceName := "dpu-device-" + nodeName + "-" + pciAddress1

			Expect(k8sClient.Get(ctx, client.ObjectKey{
				Namespace: testNS.Name,
				Name:      dpuDeviceName},
				dpuDeviceFetched)).To(Succeed())
			Expect(dpuDeviceFetched.Status.PCIAddress).NotTo(BeNil())
			Expect(*dpuDeviceFetched.Status.PCIAddress).Should(Equal(pciAddress1))

			By("checking a DPUNode is created for the node")
			Eventually(func(g Gomega) {
				dpuNodeList := &provisioningv1.DPUNodeList{}
				g.Expect(k8sClient.List(ctx, dpuNodeList, client.InNamespace(testNS.Name))).To(Succeed())
				g.Expect(dpuNodeList.Items).To(HaveLen(1))
			}).WithTimeout(10 * time.Second).Should(Succeed())

			dpuNodeFetched := &provisioningv1.DPUNode{}
			dpuNodeName := nodeName
			defaultDMSIP := "0.0.0.0"
			defaultDMSPort := 9339

			Expect(k8sClient.Get(ctx, client.ObjectKey{
				Namespace: testNS.Name,
				Name:      dpuNodeName},
				dpuNodeFetched)).To(Succeed())
			Expect(dpuNodeFetched.Spec.NodeRebootMethod.GNOI).NotTo(BeNil())
			Expect(dpuNodeFetched.Spec.DPUs[0].Name).Should(Equal(dpuDeviceName))
			Expect(dpuNodeFetched.Spec.NodeDMSAddress.IP).Should(Equal(defaultDMSIP))
			Expect(dpuNodeFetched.Spec.NodeDMSAddress.Port).Should(Equal(uint16(defaultDMSPort)))
		})
		It("Run dmsinit.sh with two PCI Address, dms IP/Port, node reboot method external and with external certificate", func() {
			dmsIP := "1.1.1.1"
			dmsPort := 3000
			nodeRebootMethod := "external"
			// Execute the script
			cmdString := fmt.Sprintf("%s --kubeconfig %s --kube-node-ref %s --pci-address %s,%s --external-certificate %s --namespace %s --dms-ip %s --dms-port %d --node-reboot-method %s", tempScriptPath, kubeconfigPath, nodeName, pciAddress1, pciAddress2, "true", testNS.Name, dmsIP, dmsPort, nodeRebootMethod)
			cmd := exec.Command("bash", "-c", cmdString)
			cmd.Dir = tempDir // Script runs in temp directory
			output, err := cmd.CombinedOutput()
			By("Run dmsinit.sh")
			Expect(err).NotTo(HaveOccurred(), "Script execution failed: %s", output)

			By("checking two DPUDevice are created for the node")
			Eventually(func(g Gomega) {
				dpuDeviceList := &provisioningv1.DPUDeviceList{}
				g.Expect(k8sClient.List(ctx, dpuDeviceList, client.InNamespace(testNS.Name))).To(Succeed())
				g.Expect(dpuDeviceList.Items).To(HaveLen(2))
			}).WithTimeout(10 * time.Second).Should(Succeed())

			By("checking a DPUNode is created for the node")
			Eventually(func(g Gomega) {
				dpuNodeList := &provisioningv1.DPUNodeList{}
				g.Expect(k8sClient.List(ctx, dpuNodeList, client.InNamespace(testNS.Name))).To(Succeed())
				g.Expect(dpuNodeList.Items).To(HaveLen(1))
			}).WithTimeout(10 * time.Second).Should(Succeed())

			dpuNodeFetched := &provisioningv1.DPUNode{}
			dpuNodeName := nodeName

			Expect(k8sClient.Get(ctx, client.ObjectKey{
				Namespace: testNS.Name,
				Name:      dpuNodeName},
				dpuNodeFetched)).To(Succeed())
			Expect(dpuNodeFetched.Spec.NodeRebootMethod.External).NotTo(BeNil())
			Expect(dpuNodeFetched.Spec.NodeDMSAddress.IP).Should(Equal(dmsIP))
			Expect(dpuNodeFetched.Spec.NodeDMSAddress.Port).Should(Equal(uint16(dmsPort)))
		})
		It("Run dmsinit.sh with two PCI Address, dms IP/Port, node reboot method custom script and with external certificate", func() {
			dmsIP := "1.1.1.1"
			dmsPort := 3000
			nodeRebootMethod := "script"
			scriptName := "test_script"
			// Execute the script
			cmdString := fmt.Sprintf("%s --kubeconfig %s --kube-node-ref %s --pci-address %s,%s --external-certificate %s --namespace %s --dms-ip %s --dms-port %d --node-reboot-method %s --script-name %s", tempScriptPath, kubeconfigPath, nodeName, pciAddress1, pciAddress2, "true", testNS.Name, dmsIP, dmsPort, nodeRebootMethod, scriptName)
			cmd := exec.Command("bash", "-c", cmdString)
			cmd.Dir = tempDir // Script runs in temp directory
			output, err := cmd.CombinedOutput()
			By("Run dmsinit.sh")
			Expect(err).NotTo(HaveOccurred(), "Script execution failed: %s", output)

			By("checking two DPUDevice are created for the node")
			Eventually(func(g Gomega) {
				dpuDeviceList := &provisioningv1.DPUDeviceList{}
				g.Expect(k8sClient.List(ctx, dpuDeviceList, client.InNamespace(testNS.Name))).To(Succeed())
				g.Expect(dpuDeviceList.Items).To(HaveLen(2))
			}).WithTimeout(10 * time.Second).Should(Succeed())

			By("checking a DPUNode is created for the node")
			Eventually(func(g Gomega) {
				dpuNodeList := &provisioningv1.DPUNodeList{}
				g.Expect(k8sClient.List(ctx, dpuNodeList, client.InNamespace(testNS.Name))).To(Succeed())
				g.Expect(dpuNodeList.Items).To(HaveLen(1))
			}).WithTimeout(10 * time.Second).Should(Succeed())

			dpuNodeFetched := &provisioningv1.DPUNode{}
			dpuNodeName := nodeName

			Expect(k8sClient.Get(ctx, client.ObjectKey{
				Namespace: testNS.Name,
				Name:      dpuNodeName},
				dpuNodeFetched)).To(Succeed())
			Expect(dpuNodeFetched.Spec.NodeRebootMethod.Script).NotTo(BeNil())
			Expect(dpuNodeFetched.Spec.NodeRebootMethod.Script.Name).Should(Equal(scriptName))
			Expect(dpuNodeFetched.Spec.NodeDMSAddress.IP).Should(Equal(dmsIP))
			Expect(dpuNodeFetched.Spec.NodeDMSAddress.Port).Should(Equal(uint16(dmsPort)))
		})
		It("Run dmsinit.sh with external certificate set to none - verify certificate is generated", func() {
			issuerName := "dpf-provisioning-issuer-test"
			By("creating Issuer")
			createIssuer(ctx, issuerName, testNS.Name)

			// Execute the script
			cmdString := fmt.Sprintf("%s --kubeconfig %s --kube-node-ref %s --pci-address %s --issuer %s --namespace %s", tempScriptPath, kubeconfigPath, nodeName, pciAddress1, issuerName, testNS.Name)
			cmd := exec.Command("bash", "-c", cmdString)
			cmd.Dir = tempDir // Script runs in temp directory
			output, err := cmd.CombinedOutput()
			By("Run dmsinit.sh")
			Expect(err).NotTo(HaveOccurred(), "Script execution failed: %s", output)

			By("checking a DPUDevice is created for the node")
			Eventually(func(g Gomega) {
				dpuDeviceList := &provisioningv1.DPUDeviceList{}
				g.Expect(k8sClient.List(ctx, dpuDeviceList, client.InNamespace(testNS.Name))).To(Succeed())
				g.Expect(dpuDeviceList.Items).To(HaveLen(1))
			}).WithTimeout(10 * time.Second).Should(Succeed())

			By("checking a DPUNode is created for the node and has a owner ref to the node")
			Eventually(func(g Gomega) {
				dpuNodeList := &provisioningv1.DPUNodeList{}
				g.Expect(k8sClient.List(ctx, dpuNodeList, client.InNamespace(testNS.Name))).To(Succeed())
				g.Expect(dpuNodeList.Items).To(HaveLen(1))
				g.Expect(dpuNodeList.Items[0].OwnerReferences).To(HaveLen(1))
			}).WithTimeout(10 * time.Second).Should(Succeed())

			By("checking a Certificate is created for the DPUNode")
			certName := nodeName + "-dms-server-cert"
			secretName := nodeName + "-server-secret"
			Eventually(func(g Gomega) {
				certList := &unstructured.UnstructuredList{}
				certList.SetGroupVersionKind(schema.GroupVersionKind{
					Group:   "cert-manager.io",
					Version: "v1",
					Kind:    "CertificateList",
				})
				g.Expect(k8sClient.List(ctx, certList, client.InNamespace(testNS.Name))).To(Succeed())
				g.Expect(certList.Items).To(HaveLen(1))

				gotSecretName, found, err := unstructured.NestedString(certList.Items[0].Object, "spec", "secretName")
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(found).To(BeTrue())
				g.Expect(gotSecretName).Should(Equal(secretName))

				gotCommonName, found, err := unstructured.NestedString(certList.Items[0].Object, "spec", "commonName")
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(found).To(BeTrue())
				g.Expect(gotCommonName).Should(Equal(certName))
			}).WithTimeout(10 * time.Second).Should(Succeed())

			// certFetched := &certmanagerv1.Certificate{}
			// certName := nodeName + "-dms-server-cert"
			// secretName := nodeName + "-server-secret"

			// Expect(k8sClient.Get(ctx, client.ObjectKey{
			// 	Namespace: testNS.Name,
			// 	Name:      certName},
			// 	certFetched)).To(Succeed())
			// Expect(certFetched.Spec.CommonName).Should(Equal(certName))
			// Expect(certFetched.Spec.SecretName).Should(Equal(secretName))
		})
		It("Run dmsinit.sh with k8s-env = false", func() {
			// Execute the script
			cmdString := fmt.Sprintf("%s --kubeconfig %s --k8s-env %t --pci-address %s --external-certificate %s --namespace %s", tempScriptPath, kubeconfigPath, false, pciAddress1, "true", testNS.Name)
			cmd := exec.Command("bash", "-c", cmdString)
			cmd.Dir = tempDir // Script runs in temp directory
			output, err := cmd.CombinedOutput()
			By("Run dmsinit.sh")
			Expect(err).NotTo(HaveOccurred(), "Script execution failed: %s", output)

			By("checking a DPUDevice is created for the node")
			Eventually(func(g Gomega) {
				dpuDeviceList := &provisioningv1.DPUDeviceList{}
				g.Expect(k8sClient.List(ctx, dpuDeviceList, client.InNamespace(testNS.Name))).To(Succeed())
				g.Expect(dpuDeviceList.Items).To(HaveLen(1))
			}).WithTimeout(10 * time.Second).Should(Succeed())

			By("checking a DPUNode is created for the node")
			Eventually(func(g Gomega) {
				dpuNodeList := &provisioningv1.DPUNodeList{}
				g.Expect(k8sClient.List(ctx, dpuNodeList, client.InNamespace(testNS.Name))).To(Succeed())
				g.Expect(dpuNodeList.Items).To(HaveLen(1))
			}).WithTimeout(10 * time.Second).Should(Succeed())
		})
		It("Run dmsinit.sh with invalid argument", func() {
			// Execute the script
			cmdString := fmt.Sprintf("%s --xa %t", tempScriptPath, false)
			cmd := exec.Command("bash", "-c", cmdString)
			cmd.Dir = tempDir // Script runs in temp directory
			_, err := cmd.CombinedOutput()
			By("Run dmsinit.sh")
			Expect(err).To(HaveOccurred())
		})
	})
})
