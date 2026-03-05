/*
SPDX-FileCopyrightText: Copyright (c) 2025 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
SPDX-License-Identifier: LicenseRef-NvidiaProprietary

NVIDIA CORPORATION, its affiliates and licensors retain all intellectual
property and proprietary rights in and to this material, related
documentation and any modifications thereto. Any use, reproduction,
disclosure or distribution of this material and related documentation
 without an express license agreement from NVIDIA CORPORATION or
its affiliates is strictly prohibited.
*/

package controllers

import (
	"fmt"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/fluxcd/pkg/runtime/patch"
	dpuservicev1 "github.com/nvidia/doca-platform/api/dpuservice/v1alpha1"
	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	vpcv1 "github.com/nvidia/doca-platform/api/vpc/v1alpha1"
	"github.com/nvidia/doca-platform/pkg/dpucluster"
	testutils "github.com/nvidia/doca-platform/test/utils"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	apiruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

var (
	cfg                  *rest.Config
	testClient           client.Client
	dpuCluster           *provisioningv1.DPUCluster
	dpuClusterTestClient client.Client
	testEnv              *envtest.Environment
	testScheme           *apiruntime.Scheme = apiruntime.NewScheme()
	suiteCtx                                = ctrl.SetupSignalHandler()
	suiteCleanupObjs     []client.Object
)

func TestVPCtControllers(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecs(t, "Controller Suite")
}

var _ = BeforeSuite(func() {
	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))

	By("bootstrapping test environment")
	testEnv = &envtest.Environment{
		CRDDirectoryPaths: []string{
			filepath.Join("..", "..", "..", "..", "config", "vpc", "crd", "bases"),
			filepath.Join("..", "..", "..", "..", "config", "dpuservice", "crd", "bases"),
			filepath.Join("..", "..", "..", "..", "config", "provisioning", "crd", "bases"),
		},
		ErrorIfCRDPathMissing: true,

		// The BinaryAssetsDirectory is only required if you want to run the tests directly
		// without call the makefile target test. If not informed it will look for the
		// default path defined in controller-runtime which is /usr/local/kubebuilder/.
		// Note that you must have the required binaries setup under the bin directory to perform
		// the tests directly. When we run make test it will be setup and used automatically.
		BinaryAssetsDirectory: filepath.Join("..", "..", "..", "..", "hack", "tools", "bin", "k8s",
			fmt.Sprintf("1.33.0-%s-%s", runtime.GOOS, runtime.GOARCH)),
	}

	var err error
	// cfg is defined in this file globally.
	cfg, err = testEnv.Start()
	Expect(err).NotTo(HaveOccurred())
	Expect(cfg).NotTo(BeNil())

	err = scheme.AddToScheme(testScheme)
	Expect(err).NotTo(HaveOccurred())

	err = vpcv1.AddToScheme(testScheme)
	Expect(err).NotTo(HaveOccurred())

	err = dpuservicev1.AddToScheme(testScheme)
	Expect(err).NotTo(HaveOccurred())

	err = provisioningv1.AddToScheme(testScheme)
	Expect(err).NotTo(HaveOccurred())

	testClient, err = client.New(cfg, client.Options{Scheme: testScheme})
	Expect(err).NotTo(HaveOccurred())
	Expect(testClient).NotTo(BeNil())

	// Create dpucluster and secret pointing to envtest cluster for controllers that rely on dpucluster and cluster cache
	By("creating DPUCluster that use the envtest cluster")
	dpuCluster = ptr.To(testutils.GetTestDPUCluster("default", "envtest"))
	kamajiSecret, err := testutils.GetFakeKamajiClusterSecretFromEnvtest(*dpuCluster, cfg)
	Expect(err).NotTo(HaveOccurred())

	Expect(testClient.Create(suiteCtx, kamajiSecret)).To(Succeed())
	suiteCleanupObjs = append(suiteCleanupObjs, kamajiSecret)

	Expect(testClient.Create(suiteCtx, dpuCluster)).To(Succeed())
	suiteCleanupObjs = append(suiteCleanupObjs, dpuCluster)

	// mark the cluster as ready so that the remoteCache treats it as ready
	patcher := patch.NewSerialPatcher(dpuCluster, testClient)
	dpuCluster.Status.Phase = provisioningv1.PhaseReady
	Expect(patcher.Patch(suiteCtx, dpuCluster, patch.WithFieldOwner("cluster-controller"))).To(Succeed())

	// create a client for the DPUCluster
	dpuClusterTestClient, err = dpucluster.NewConfig(testClient, dpuCluster).Client(suiteCtx, dpucluster.ClientOptionScheme{Scheme: testScheme})
	Expect(err).ToNot(HaveOccurred())
})

var _ = AfterSuite(func() {
	Expect(testutils.CleanupAndWait(suiteCtx, testClient, suiteCleanupObjs...)).To(Succeed())
	err := testEnv.Stop()
	Expect(err).NotTo(HaveOccurred())
})
