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

package e2e

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	dpuservicev1 "github.com/nvidia/doca-platform/api/dpuservice/v1alpha1"
	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"
	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	argov1 "github.com/nvidia/doca-platform/internal/argocd/api/application/v1alpha1"
	kamajiv1 "github.com/nvidia/doca-platform/internal/kamaji/api/v1alpha1"
	nvipamv1 "github.com/nvidia/doca-platform/internal/nvipam/api/v1alpha1"
	"github.com/nvidia/doca-platform/test/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var (
	// testKubeconfig path to be used for this test.
	testKubeconfig string
)

func init() {
	flag.StringVar(&testKubeconfig, "e2e.testKubeconfig", "", "path to the testKubeconfig file")
	getEnvVariables()
}

// These variables can be set from the environment when running the DPF tests.
var (
	// numNodes can be overwritten by setting DPF_E2E_NUM_DPU_NODES in the environment.
	// This tells the test how many Kubernetes nodes to expect in the DPU Cluster.
	numNodes = 0
	// skipCleanup indicates whether to skip the cleanup of resources created during the e2e test run.
	// When set to true, resources will not be removed after the test completes.
	skipCleanup = false
	// bfbImageURL can be used to override the default BFB image URL used in the tests.
	bfbImageURL = ""
)

func getEnvVariables() {
	if nodes, found := os.LookupEnv("DPF_E2E_NUM_DPU_NODES"); found {
		var err error
		numNodes, err = strconv.Atoi(nodes)
		if err != nil {
			panic(err)
		}
	}
	if v, found := os.LookupEnv("E2E_SKIP_CLEANUP"); found {
		var err error
		skipCleanup, err = strconv.ParseBool(v)
		if err != nil {
			panic(fmt.Errorf("string must be a bool: %v", err))
		}
	}
	if url, found := os.LookupEnv("BFB_IMAGE_URL"); found {
		var err error
		bfbImageURL, err = utils.ResolveBFBImageURL(url)
		if err != nil {
			panic(err)
		}
	}
}

var (
	testClient client.Client
	restConfig *rest.Config
	clientset  *kubernetes.Clientset
	ctx        = ctrl.SetupSignalHandler()
)

// Run e2e tests using the Ginkgo runner.
func TestE2E(t *testing.T) {
	RegisterFailHandler(Fail)
	g := NewWithT(t)
	defer GinkgoRecover()
	var err error
	_, err = fmt.Fprintf(GinkgoWriter, "Starting dpf-operator suite\n")
	Expect(err).ToNot(HaveOccurred())
	ctrl.SetLogger(klog.Background())

	Expect(dpuservicev1.AddToScheme(scheme.Scheme)).To(Succeed())
	Expect(operatorv1.AddToScheme(scheme.Scheme)).To(Succeed())
	Expect(argov1.AddToScheme(scheme.Scheme)).To(Succeed())
	Expect(provisioningv1.AddToScheme(scheme.Scheme)).To(Succeed())
	Expect(nvipamv1.AddToScheme(scheme.Scheme)).To(Succeed())
	Expect(kamajiv1.AddToScheme(scheme.Scheme)).To(Succeed())
	s := scheme.Scheme

	// If testKubeconfig is not set default it to $HOME/.kube/config
	home, exists := os.LookupEnv("HOME")
	g.Expect(exists).To(BeTrue())
	if testKubeconfig == "" {
		testKubeconfig = filepath.Join(home, ".kube/config")
	}

	// Create a client to use throughout the test.
	restConfig, err = clientcmd.BuildConfigFromFlags("", testKubeconfig)
	g.Expect(err).NotTo(HaveOccurred())
	clientset, err = kubernetes.NewForConfig(restConfig)
	g.Expect(err).NotTo(HaveOccurred())
	testClient, err = client.New(restConfig, client.Options{Scheme: s})
	g.Expect(err).NotTo(HaveOccurred())

	RunSpecs(t, "e2e suite")
}
