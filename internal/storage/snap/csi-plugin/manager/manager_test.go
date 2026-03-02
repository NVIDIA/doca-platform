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

package manager

import (
	"context"
	"fmt"
	"net"
	"os"
	"time"

	"github.com/nvidia/doca-platform/internal/storage/snap/csi-plugin/config"
	"github.com/nvidia/doca-platform/internal/storage/snap/csi-plugin/utils/runner"

	"github.com/container-storage-interface/spec/lib/go/csi"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

var _ = Describe("Manager", func() {
	var (
		listenOptions config.ServerBindOptions
	)
	BeforeEach(func() {
		tmpDir, err := os.MkdirTemp("", "csi-plugin-manager-test*")
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() {
			Expect(os.RemoveAll(tmpDir)).NotTo(HaveOccurred())
		})
		listenOptions = config.ServerBindOptions{
			Address: tmpDir + "/csi.sock",
			Network: "unix",
		}
	})
	It("controller", func(ctx SpecContext) {
		// create stale socket to make sure that manager will be able to handle this
		listener, err := net.Listen(listenOptions.Network, listenOptions.Address)
		Expect(err).NotTo(HaveOccurred())
		Expect(listener.Close()).NotTo(HaveOccurred())

		clusterHelper := &TestClusterHelper{FakeRunnable: runner.NewFakeRunnable()}
		m, err := New(config.PluginConfig{Common: config.Common{
			Name: config.DefaultPluginName, PluginMode: config.PluginModeController, ListenOptions: listenOptions},
			Controller: config.Controller{}},
			WithControllerHandler(&dummyGRPCHandler{}),
			WithClusterHelper(clusterHelper),
		)
		Expect(err).NotTo(HaveOccurred())
		runCtx, cancel := context.WithCancel(ctx)
		defer cancel()
		go func() {
			defer GinkgoRecover()
			Expect(m.Start(runCtx)).NotTo(HaveOccurred())
		}()
		Eventually(func(g Gomega, ctx context.Context) {
			conn, err := grpc.NewClient("unix://"+listenOptions.Address, grpc.WithTransportCredentials(insecure.NewCredentials()))
			g.Expect(err).NotTo(HaveOccurred())
			defer func() { _ = conn.Close() }()
			ctrlClient := csi.NewControllerClient(conn)
			identityClient := csi.NewIdentityClient(conn)

			callCtx, callCancel := context.WithTimeout(ctx, 5*time.Second)
			defer callCancel()
			controllerResp, err := ctrlClient.ControllerGetCapabilities(callCtx, &csi.ControllerGetCapabilitiesRequest{})
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(controllerResp).NotTo(BeNil())

			callCtx, callCancel = context.WithTimeout(ctx, 5*time.Second)
			defer callCancel()
			identityResp, err := identityClient.GetPluginInfo(callCtx, &csi.GetPluginInfoRequest{})
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(identityResp).NotTo(BeNil())
			g.Expect(identityResp.Name).To(Equal("csi.snap.nvidia.com"))
		}).WithContext(ctx).WithTimeout(time.Minute).WithPolling(time.Second).Should(Succeed())

		Expect(clusterHelper.Started()).To(BeTrue())
		Expect(clusterHelper.Stopped()).To(BeFalse())
		cancel()
		Eventually(func(g Gomega) {
			g.Expect(clusterHelper.Stopped()).To(BeTrue())
		}).WithTimeout(time.Minute).WithPolling(time.Second).Should(Succeed())
	})
	It("node", func(ctx SpecContext) {
		preconfigureService := runner.NewFakeRunnable()
		m, err := New(config.PluginConfig{Common: config.Common{
			Name: config.DefaultPluginName, PluginMode: config.PluginModeNode, ListenOptions: listenOptions},
			Node: config.Node{}},
			WithNodeHandler(&dummyGRPCHandler{}),
			WithPreconfigure(preconfigureService),
		)
		Expect(err).NotTo(HaveOccurred())
		runCtx, cancel := context.WithCancel(ctx)
		defer cancel()
		go func() {
			defer GinkgoRecover()
			Expect(m.Start(runCtx)).NotTo(HaveOccurred())
		}()
		Eventually(func(g Gomega, ctx context.Context) {
			conn, err := grpc.NewClient("unix://"+listenOptions.Address, grpc.WithTransportCredentials(insecure.NewCredentials()))
			g.Expect(err).NotTo(HaveOccurred())
			defer func() { _ = conn.Close() }()
			nodeClient := csi.NewNodeClient(conn)
			identityClient := csi.NewIdentityClient(conn)
			callCtx, callCancel := context.WithTimeout(ctx, 5*time.Second)
			defer callCancel()
			nodeResp, err := nodeClient.NodeGetInfo(callCtx, &csi.NodeGetInfoRequest{})
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(nodeResp).NotTo(BeNil())
			g.Expect(nodeResp.NodeId).To(Equal("test"))

			callCtx, callCancel = context.WithTimeout(ctx, 5*time.Second)
			defer callCancel()
			identityResp, err := identityClient.GetPluginInfo(callCtx, &csi.GetPluginInfoRequest{})
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(identityResp).NotTo(BeNil())
			g.Expect(identityResp.Name).To(Equal("csi.snap.nvidia.com"))
		}).WithContext(ctx).WithTimeout(time.Minute).WithPolling(time.Second).Should(Succeed())

		Expect(preconfigureService.Started()).To(BeTrue())
		Expect(preconfigureService.Stopped()).To(BeFalse())
		cancel()
		Eventually(func(g Gomega) {
			g.Expect(preconfigureService.Stopped()).To(BeTrue())
		}).WithTimeout(time.Minute).WithPolling(time.Second).Should(Succeed())
	})
	It("dependency timeout", func(ctx SpecContext) {
		// service will never become ready
		preconfigureService := runner.NewFakeRunnable()
		preconfigureService.SetFunction(func(ctx context.Context, _ func()) error {
			<-ctx.Done()
			return nil
		})
		m, err := New(config.PluginConfig{Common: config.Common{
			Name: config.DefaultPluginName, PluginMode: config.PluginModeNode, ListenOptions: listenOptions},
			Node: config.Node{}},
			WithNodeHandler(&dummyGRPCHandler{}),
			WithPreconfigure(preconfigureService),
			// dependency timeout is 1 second
			WithDependenciesWaitTimeout(time.Second),
		)
		Expect(err).NotTo(HaveOccurred())
		stop := make(chan struct{})
		go func() {
			defer GinkgoRecover()
			defer close(stop)
			Expect(m.Start(ctx)).To(MatchError(ContainSubstring("manager dependencies are not ready")))
		}()
		// the service should complete with an error
		Eventually(stop).WithTimeout(time.Minute).Should(BeClosed())
	})
	It("dependency failed", func(ctx SpecContext) {
		// service returns error
		preconfigureService := runner.NewFakeRunnable()
		preconfigureService.SetFunction(func(ctx context.Context, readyFunc func()) error {
			return fmt.Errorf("test error")
		})
		m, err := New(config.PluginConfig{Common: config.Common{
			PluginMode: config.PluginModeNode, ListenOptions: listenOptions},
			Node: config.Node{}},
			WithNodeHandler(&dummyGRPCHandler{}),
			WithPreconfigure(preconfigureService),
		)
		Expect(err).NotTo(HaveOccurred())
		stop := make(chan struct{})
		go func() {
			defer GinkgoRecover()
			defer close(stop)
			Expect(m.Start(ctx)).To(MatchError(ContainSubstring("manager dependencies are not ready")))
		}()
		// the service should complete with an error
		Eventually(stop).WithTimeout(time.Minute).Should(BeClosed())
	})
})
