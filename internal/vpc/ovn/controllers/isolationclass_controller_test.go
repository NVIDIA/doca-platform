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
	"fmt"
	"sync"
	"time"

	vpcv1 "github.com/nvidia/doca-platform/api/vpc/v1alpha1"
	testutils "github.com/nvidia/doca-platform/test/utils"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrlcfg "sigs.k8s.io/controller-runtime/pkg/config"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/metrics/server"
)

var _ = Describe("IsolationClassController", func() {
	var cleanupObjs []client.Object
	var testCtx context.Context
	var testCancelFunc context.CancelFunc
	var wg sync.WaitGroup

	BeforeEach(func() {
		cleanupObjs = make([]client.Object, 0)
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

		err = (&IsolationClassReconciler{
			Client: testManager.GetClient(),
			Scheme: testManager.GetScheme(),
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
		Expect(testutils.CleanupAndWait(suiteCtx, testClient, cleanupObjs...)).To(Succeed())
		testCancelFunc()
		wg.Wait()
	})

	It("should add finalizer to Isolation class", func() {
		By("creating an Isolation class")
		isoCls := getTestIsolationClass("test-isolation-class")
		Expect(testClient.Create(testCtx, isoCls)).To(Succeed())

		By("checking if finalizer is added")
		Eventually(func() bool {
			err := testClient.Get(testCtx, client.ObjectKey{Name: isoCls.Name}, isoCls)
			if err != nil {
				return false
			}
			return controllerutil.ContainsFinalizer(isoCls, isolationClassFinalizer)
		}).WithTimeout(30 * time.Second).WithPolling(500 * time.Millisecond).Should(BeTrue())

		By("deleting the Isolation class")
		Expect(testClient.Delete(testCtx, isoCls)).To(Succeed())
		Eventually(func() error {
			gotIsoCls := &vpcv1.IsolationClass{}
			err := testClient.Get(testCtx, client.ObjectKey{Name: isoCls.Name}, gotIsoCls)
			if err != nil {
				if apierrors.IsNotFound(err) {
					return nil
				}
				return err
			}
			return fmt.Errorf("Isolation class still exists")
		}).WithTimeout(30 * time.Second).WithPolling(500 * time.Millisecond).Should(Succeed())
	})

	It("should block deletion of Isolation class if DPUVPCs reference it", func() {
		By("creating DPUVPC")
		vpc := getTestVPC("test-vpc", "test-isolation-class", nil)
		Expect(testClient.Create(testCtx, vpc)).To(Succeed())
		cleanupObjs = append(cleanupObjs, vpc)

		By("creating an Isolation class")
		isoCls := getTestIsolationClass("test-isolation-class")
		Expect(testClient.Create(testCtx, isoCls)).To(Succeed())

		By("checking if finalizer is added")
		Eventually(func() bool {
			err := testClient.Get(testCtx, client.ObjectKey{Name: isoCls.Name}, isoCls)
			if err != nil {
				return false
			}
			return controllerutil.ContainsFinalizer(isoCls, isolationClassFinalizer)
		}).WithTimeout(30 * time.Second).WithPolling(500 * time.Millisecond).Should(BeTrue())

		By("deleting the Isolation class")
		Expect(testClient.Delete(testCtx, isoCls)).To(Succeed())

		By("checking if Isolation class is not deleted")
		Consistently(func() error {
			err := testClient.Get(testCtx, client.ObjectKey{Name: isoCls.Name}, isoCls)
			if err != nil {
				return err
			}
			return nil
		}).WithTimeout(5 * time.Second).WithPolling(500 * time.Millisecond).Should(Succeed())

		By("deleting the DPUVPC")
		Expect(testClient.Delete(testCtx, vpc)).To(Succeed())

		By("checking if Isolation class is deleted")
		Eventually(func() error {
			err := testClient.Get(testCtx, client.ObjectKey{Name: isoCls.Name}, isoCls)
			if err != nil {
				if apierrors.IsNotFound(err) {
					return nil
				}
				return err
			}
			return fmt.Errorf("Isolation class still exists")
		}).WithTimeout(60 * time.Second).WithPolling(500 * time.Millisecond).Should(Succeed())
	})
})
