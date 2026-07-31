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
	"context"
	"sync"
	"time"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/pkg/dpucluster"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrlcfg "sigs.k8s.io/controller-runtime/pkg/config"
	"sigs.k8s.io/controller-runtime/pkg/metrics/server"
)

func newFakeWatchRegisterer() *fakeWatchRegisterer {
	return &fakeWatchRegisterer{
		mu: sync.Mutex{},
	}
}

type fakeWatchRegisterer struct {
	rc      *dpucluster.RemoteCache
	cluster *provisioningv1.DPUCluster

	mu sync.Mutex
}

// RegisterWatchesForCluster implements WatchRegisterer interface
func (f *fakeWatchRegisterer) RegisterWatchesForCluster(ctx context.Context, rc *dpucluster.RemoteCache, cluster *provisioningv1.DPUCluster) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rc = rc
	f.cluster = cluster
	return nil
}

// GetRemoteCache returns the remote cache
func (f *fakeWatchRegisterer) GetRemoteCache() *dpucluster.RemoteCache {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.rc
}

// GetCluster returns the cluster
func (f *fakeWatchRegisterer) GetCluster() *provisioningv1.DPUCluster {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.cluster
}

var _ = Describe("DPUClusterController", func() {
	var testCtx context.Context
	var testCancelFunc context.CancelFunc
	var wg sync.WaitGroup
	var fwr *fakeWatchRegisterer

	BeforeEach(func() {
		testCtx, testCancelFunc = context.WithCancel(suiteCtx)
		wg = sync.WaitGroup{}

		By("setting up and running the test manager")
		testManager, err := ctrl.NewManager(cfg,
			ctrl.Options{
				Scheme: testScheme,
				Client: client.Options{
					Cache: &client.CacheOptions{
						DisableFor: []client.Object{&corev1.Secret{}, &corev1.ConfigMap{}},
					},
				},
				// Set metrics server bind address to 0 to disable it.
				Metrics: server.Options{
					BindAddress: "0",
				},
				Controller: ctrlcfg.Controller{
					// this is needed since metrics are registered globally by controller runtime for each controller
					// and we want to allow multiple tests initializing the same controller name.
					SkipNameValidation: ptr.To(true),
				},
			},
		)
		Expect(err).ToNot(HaveOccurred())

		remoteCache, err := dpucluster.SetupRemoteCacheWithManager(testCtx, testManager,
			dpucluster.OptionHostClient{Client: testManager.GetClient()},
			dpucluster.OptionScheme{Scheme: testManager.GetScheme()},
			dpucluster.OptionUserAgent{UserAgent: "ovn-vpc-controller"},
			dpucluster.OptionDisableFor{DisableFor: []client.Object{
				&corev1.ConfigMap{},
				&corev1.Secret{},
			}})
		Expect(err).ToNot(HaveOccurred())

		fwr = newFakeWatchRegisterer()
		err = (&DPUClusterReconciler{
			Client:           testManager.GetClient(),
			Scheme:           testManager.GetScheme(),
			RemoteCache:      remoteCache,
			WatchRegisterers: []WatchRegisterer{fwr},
		}).SetupWithManager(testCtx, testManager)
		Expect(err).ToNot(HaveOccurred())

		wg.Add(1)
		go func() {
			defer wg.Done()
			defer GinkgoRecover()
			err = testManager.Start(testCtx)
			Expect(err).ToNot(HaveOccurred(), "failed to run manager")
		}()
	})

	AfterEach(func() {
		testCancelFunc()
		wg.Wait()
	})

	It("should call WatchRegister RegisterWatchesForCluster", func() {
		Eventually(func(g Gomega) {
			rc := fwr.GetRemoteCache()
			g.Expect(rc).NotTo(BeNil())
			cluster := fwr.GetCluster()
			g.Expect(cluster).ToNot(BeNil())
			g.Expect(cluster.Name).To(Equal("envtest"))
			g.Expect(cluster.Namespace).To(Equal("default"))
			c, err := rc.GetClient(client.ObjectKeyFromObject(fwr.cluster))
			g.Expect(err).ToNot(HaveOccurred())
			g.Expect(c.List(testCtx, &corev1.NamespaceList{})).To(Succeed())
		}).WithPolling(500 * time.Millisecond).WithTimeout(5 * time.Second).Should(Succeed())
	})
})
