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

package gnoi_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"path/filepath"
	"runtime"
	"strconv"
	"testing"
	"time"

	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"
	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	cutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/util"
	"github.com/nvidia/doca-platform/internal/provisioning/controllers/util/dms"
	dmsserver "github.com/nvidia/doca-platform/test/mock/dms/pkg/server"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/openconfig/gnmi/proto/gnmi"
	"github.com/openconfig/gnoi/os"
	"github.com/openconfig/gnoi/system"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

var (
	cfg         *rest.Config
	k8sClient   client.Client
	testEnv     *envtest.Environment
	ctx         context.Context
	cancel      context.CancelFunc
	testNS      *corev1.Namespace
	testDPUNode *provisioningv1.DPUNode
	testObjects []client.Object
	dmsServer   *dmsserver.DMSServerMux

	defaultDPUNodeName       = "test-dpu-node"
	defaultDPFOperatorConfig = "test-dpf-operator-config"
)

func TestGNOIPhases(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "gNOI Phases Suite")
}

var _ = BeforeSuite(func() {
	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))

	By("bootstrapping test environment")
	testEnv = &envtest.Environment{
		CRDDirectoryPaths: []string{
			// filepath.Join("..", "..", "..", "..", "..", "..", "config", "operator-crds"),
			filepath.Join("..", "..", "..", "..", "..", "..", "config", "provisioning", "crd", "bases"),
			filepath.Join("..", "..", "..", "..", "..", "..", "deploy", "charts", "dpf-operator", "templates", "crds"),
			filepath.Join("..", "..", "..", "..", "..", "..", "test", "objects", "crd", "cert-manager"),
			filepath.Join("..", "..", "..", "..", "..", "..", "test", "objects", "crd", "nodemaintenances"),
		},
		ErrorIfCRDPathMissing: true,

		// The BinaryAssetsDirectory is only required if you want to run the tests directly
		// without call the makefile target test. If not informed it will look for the
		// default path defined in controller-runtime which is /usr/local/kubebuilder/.
		// Note that you must have the required binaries setup under the bin directory to perform
		// the tests directly. When we run make test it will be setup and used automatically.
		BinaryAssetsDirectory: filepath.Join("..", "..", "..", "..", "..", "..", "hack", "tools", "bin", "k8s",
			fmt.Sprintf("1.32.0-%s-%s", runtime.GOOS, runtime.GOARCH)),
	}

	// cfg is defined in this file globally.
	var err error
	cfg, err = testEnv.Start()
	Expect(err).NotTo(HaveOccurred())
	Expect(cfg).NotTo(BeNil())

	scheme := scheme.Scheme
	err = provisioningv1.AddToScheme(scheme)
	Expect(err).NotTo(HaveOccurred())
	err = operatorv1.AddToScheme(scheme)
	Expect(err).NotTo(HaveOccurred())

	ctx, cancel = context.WithCancel(context.TODO())
	k8sClient, err = client.New(cfg, client.Options{Scheme: scheme})
	Expect(err).NotTo(HaveOccurred())
	Expect(k8sClient).NotTo(BeNil())

})

var _ = AfterSuite(func() {
	By("tearing down the test environment")
	err := (func() (err error) {
		// Need to sleep if the first stop fails due to a bug:
		// https://github.com/kubernetes-sigs/controller-runtime/issues/1571
		sleepTime := 1 * time.Millisecond
		for i := 0; i < 12; i++ { // Exponentially sleep up to ~4s
			if err = testEnv.Stop(); err == nil {
				return
			}
			sleepTime *= 2
			time.Sleep(sleepTime)
		}
		return
	})()
	Expect(err).NotTo(HaveOccurred())
})

var _ = BeforeEach(func() {
	testNS = &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{GenerateName: "gnoi-test"}}
	Eventually(func() error {
		return k8sClient.Create(ctx, testNS)
	}).WithTimeout(10 * time.Second).Should(Succeed())
})

var _ = AfterEach(func() {
	if dmsServer != nil {
		dmsServer.Stop()
	}
	for _, obj := range testObjects {
		Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, obj))).To(Succeed())
	}
})

func createObject(obj client.Object) {
	Expect(k8sClient.Create(ctx, obj)).To(Succeed())
	testObjects = append(testObjects, obj)
}

func dpuObj(name, dpuNodeName, flavorName string) *provisioningv1.DPU {
	dpu := &provisioningv1.DPU{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: name,
			Namespace:    testNS.Name,
			Labels:       make(map[string]string),
		},
		Spec: provisioningv1.DPUSpec{
			SerialNumber:  "MT25066004C7",
			PCIAddress:    ptr.To("0000-00-00"),
			DPUFlavor:     flavorName,
			DPUNodeName:   dpuNodeName,
			DPUDeviceName: name,
		},
	}
	return dpu
}

func flavorObj(name string) *provisioningv1.DPUFlavor {
	flavor := &provisioningv1.DPUFlavor{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: testNS.Name,
		},
		Spec: provisioningv1.DPUFlavorSpec{
			DPUResources: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("1"),
				corev1.ResourceMemory: resource.MustParse("1Gi"),
			},
		},
	}
	return flavor
}

func dpuNodeObj(name string, listener net.Listener) *provisioningv1.DPUNode {
	ipStr, portStr, err := net.SplitHostPort(listener.Addr().String())
	Expect(err).To(Succeed())
	port, err := strconv.Atoi(portStr)
	Expect(err).To(Succeed())
	dpuNode := &provisioningv1.DPUNode{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: testNS.Name,
			Labels: map[string]string{
				cutil.NodeFeatureDiscoveryLabelPrefix + cutil.DPUOOBBridgeConfiguredLabel: "true",
			},
		},
		Spec: provisioningv1.DPUNodeSpec{
			NodeRebootMethod: &provisioningv1.NodeRebootMethod{
				GNOI: &provisioningv1.GNOI{},
			},
			NodeDMSAddress: &provisioningv1.DMSAddress{IP: ipStr, Port: uint16(port)},
		},
	}
	return dpuNode
}

func dpuDeviceObj(name string) *provisioningv1.DPUDevice {
	dpuDevice := &provisioningv1.DPUDevice{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: testNS.Name,
		},
		Spec: provisioningv1.DPUDeviceSpec{
			SerialNumber: "MT25066004C7",
			NumberOfPFs:  ptr.To(2),
		},
	}
	return dpuDevice
}

func bfbObj(name string) *provisioningv1.BFB {
	bfb := &provisioningv1.BFB{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: testNS.Name,
		},
		Spec: provisioningv1.BFBSpec{
			URL: "https://test.com/bfb-file.bfb",
		},
	}
	return bfb
}

func dpuClusterObj(name string) *provisioningv1.DPUCluster {
	dpuCluster := &provisioningv1.DPUCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: testNS.Name,
		},
		Spec: provisioningv1.DPUClusterSpec{
			Type:     string(provisioningv1.StaticCluster),
			MaxNodes: 1,
		},
	}
	return dpuCluster
}

func dpfOperatorConfigObj(name string) *operatorv1.DPFOperatorConfig {
	dpfOperatorConfig := &operatorv1.DPFOperatorConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: testNS.Name,
		},
		Spec: operatorv1.DPFOperatorConfigSpec{
			Networking: &operatorv1.Networking{
				ControlPlaneMTU: ptr.To(1500),
			},
			ProvisioningController: &operatorv1.ProvisioningControllerConfiguration{
				BFBPersistentVolumeClaimName: "test-bfb-pvc",
			},
		},
	}
	return dpfOperatorConfig
}

func createMTLSCerts(dmsIP string) (caCrtBytes, clientCrtBytes, clientKeyBytes, srvCrtBytes, srvKeyBytes []byte) {
	// CA Private Key
	caPrivKey, err := rsa.GenerateKey(rand.Reader, 2048)
	Expect(err).NotTo(HaveOccurred())

	// CA Certificate Template
	caTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(2024),
		Subject: pkix.Name{
			Organization: []string{"Test CA Org"},
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().AddDate(1, 0, 0), // Valid for 1 year
		IsCA:                  true,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth, x509.ExtKeyUsageServerAuth},
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		BasicConstraintsValid: true,
	}

	// Create CA Certificate
	caCertBytes, err := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, &caPrivKey.PublicKey, caPrivKey)
	Expect(err).NotTo(HaveOccurred())
	caCert, err := x509.ParseCertificate(caCertBytes)
	Expect(err).NotTo(HaveOccurred())

	// Server Private Key
	serverPrivKey, err := rsa.GenerateKey(rand.Reader, 2048)
	Expect(err).NotTo(HaveOccurred())

	// Server Certificate Template
	serverTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(2025),
		Subject: pkix.Name{
			CommonName: dmsIP,
		},
		IPAddresses: []net.IP{net.ParseIP(dmsIP)},
		NotBefore:   time.Now(),
		NotAfter:    time.Now().AddDate(1, 0, 0),
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		KeyUsage:    x509.KeyUsageDigitalSignature,
	}

	// Create Server Certificate
	serverCertBytes, err := x509.CreateCertificate(rand.Reader, serverTemplate, caCert, &serverPrivKey.PublicKey, caPrivKey)
	Expect(err).NotTo(HaveOccurred())

	// Client Private Key
	clientPrivKey, err := rsa.GenerateKey(rand.Reader, 2048)
	Expect(err).NotTo(HaveOccurred())

	// Client Certificate Template
	clientTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(2026),
		Subject: pkix.Name{
			CommonName: "client",
		},
		NotBefore:   time.Now(),
		NotAfter:    time.Now().AddDate(1, 0, 0),
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
		KeyUsage:    x509.KeyUsageDigitalSignature,
	}

	// Create Client Certificate
	clientCertBytes, err := x509.CreateCertificate(rand.Reader, clientTemplate, caCert, &clientPrivKey.PublicKey, caPrivKey)
	Expect(err).NotTo(HaveOccurred())

	// PEM Encode
	caCertPEM := new(bytes.Buffer)
	Expect(pem.Encode(caCertPEM, &pem.Block{Type: "CERTIFICATE", Bytes: caCertBytes})).To(Succeed())

	serverCertPEM := new(bytes.Buffer)
	Expect(pem.Encode(serverCertPEM, &pem.Block{Type: "CERTIFICATE", Bytes: serverCertBytes})).To(Succeed())
	serverPrivKeyPEM := new(bytes.Buffer)
	Expect(pem.Encode(serverPrivKeyPEM, &pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(serverPrivKey)})).To(Succeed())

	clientCertPEM := new(bytes.Buffer)
	Expect(pem.Encode(clientCertPEM, &pem.Block{Type: "CERTIFICATE", Bytes: clientCertBytes})).To(Succeed())
	clientPrivKeyPEM := new(bytes.Buffer)
	Expect(pem.Encode(clientPrivKeyPEM, &pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(clientPrivKey)})).To(Succeed())

	return caCertPEM.Bytes(), clientCertPEM.Bytes(), clientPrivKeyPEM.Bytes(), serverCertPEM.Bytes(), serverPrivKeyPEM.Bytes()
}

func createSecret(dpuNodeName string, caCrtBytes, clientCrtBytes, clientKeyBytes, srvCrtBytes, srvKeyBytes []byte) {
	clientCert := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      dms.DMSClientSecret,
			Namespace: testNS.Name,
		},
		Data: map[string][]byte{
			"ca.crt":  caCrtBytes,
			"tls.crt": clientCrtBytes,
			"tls.key": clientKeyBytes,
		},
	}

	serverCert := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cutil.GenerateDMSServerSecretName(dpuNodeName),
			Namespace: testNS.Name,
		},
		Data: map[string][]byte{
			"ca.crt":  caCrtBytes,
			"tls.crt": srvCrtBytes,
			"tls.key": srvKeyBytes,
		},
	}
	createObject(clientCert)
	createObject(serverCert)
}

func parseCertificate(certContent []byte) (*x509.Certificate, error) {
	certBlock, _ := pem.Decode(certContent)
	if certBlock == nil {
		return nil, fmt.Errorf("unable to decode PEM data for cert")
	}

	cert, err := x509.ParseCertificate(certBlock.Bytes)
	if err != nil {
		return nil, fmt.Errorf("unable to parse certificate: %w", err)
	}
	return cert, nil
}

func parseKey(keyContent []byte) (*rsa.PrivateKey, error) {
	keyBlock, _ := pem.Decode(keyContent)
	if keyBlock == nil {
		return nil, fmt.Errorf("unable to decode PEM data for key")
	}

	key, err := x509.ParsePKCS1PrivateKey(keyBlock.Bytes)
	if err != nil {
		return nil, fmt.Errorf("unable to parse private key: %w", err)
	}
	// Decode the key from the file
	return key, nil
}

type localDMS struct {
	os.UnimplementedOSServer
	system.UnimplementedSystemServer
	gnmi.UnimplementedGNMIServer
	get          func(ctx context.Context, req *gnmi.GetRequest) (*gnmi.GetResponse, error)
	install      func(req os.OS_InstallServer) error
	activate     func(ctx context.Context, req *os.ActivateRequest) (*os.ActivateResponse, error)
	rebootStatus func(ctx context.Context, req *system.RebootStatusRequest) (*system.RebootStatusResponse, error)
	set          func(ctx context.Context, req *gnmi.SetRequest) (*gnmi.SetResponse, error)
}

func (d *localDMS) Set(ctx context.Context, req *gnmi.SetRequest) (*gnmi.SetResponse, error) {
	return d.set(ctx, req)
}

func (d *localDMS) Get(ctx context.Context, req *gnmi.GetRequest) (*gnmi.GetResponse, error) {
	return d.get(ctx, req)
}

func (d *localDMS) Install(req os.OS_InstallServer) error {
	return d.install(req)
}

func (d *localDMS) Activate(ctx context.Context, req *os.ActivateRequest) (*os.ActivateResponse, error) {
	return d.activate(ctx, req)
}

func (d *localDMS) RebootStatus(ctx context.Context, req *system.RebootStatusRequest) (*system.RebootStatusResponse, error) {
	return d.rebootStatus(ctx, req)
}

func setupDMS(handler dmsserver.APIHandler) {
	// create local DMS
	// step 1: find a free port
	dmsIP := "127.0.0.1"
	listener, err := net.Listen("tcp", fmt.Sprintf("%s:0", dmsIP))
	Expect(err).To(Succeed())
	testDPUNode = dpuNodeObj(defaultDPUNodeName, listener)
	createObject(testDPUNode)

	// step 2: create mTLS certs
	caCrtBytes, clientCrtBytes, clientKeyBytes, srvCrtBytes, srvKeyBytes := createMTLSCerts(dmsIP)
	cert, err := parseCertificate(srvCrtBytes)
	Expect(err).To(Succeed())
	key, err := parseKey(srvKeyBytes)
	Expect(err).To(Succeed())
	createSecret(testDPUNode.Name, caCrtBytes, clientCrtBytes, clientKeyBytes, srvCrtBytes, srvKeyBytes)

	// step 3: start the local DMSserver
	dmsServer = dmsserver.NewDMSServerMux(dmsIP, cert, key, nil, handler)
	Expect(dmsServer.ServeForDPUNode(testDPUNode, listener)).To(Succeed())
	Eventually(func() error {
		// Test connection to the listener to ensure it's ready
		address := net.JoinHostPort(testDPUNode.Spec.NodeDMSAddress.IP, strconv.Itoa(int(testDPUNode.Spec.NodeDMSAddress.Port)))
		conn, err := net.Dial("tcp", address)
		if err != nil {
			return fmt.Errorf("failed to connect to listener: %v", err)
		}
		defer conn.Close() // nolint:errcheck
		By(fmt.Sprintf("local DMS server is ready to serve: %s", conn.RemoteAddr().String()))
		return nil
	}).WithTimeout(5 * time.Second).Should(Succeed())
}
