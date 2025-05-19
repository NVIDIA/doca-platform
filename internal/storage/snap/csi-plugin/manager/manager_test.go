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
	It("controller", func() {
		// create stale socket to make sure that manager will be able to handle this
		_, err := net.Listen(listenOptions.Network, listenOptions.Address)
		Expect(err).NotTo(HaveOccurred())

		clusterHelper := &TestClusterHelper{FakeRunnable: runner.NewFakeRunnable()}
		m, err := New(config.PluginConfig{Common: config.Common{
			PluginMode: config.PluginModeController, ListenOptions: listenOptions},
			Controller: config.Controller{}},
			WithControllerHandler(&dummyGRPCHandler{}),
			WithClusterHelper(clusterHelper),
		)
		Expect(err).NotTo(HaveOccurred())
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go func() {
			defer GinkgoRecover()
			Expect(m.Start(ctx)).NotTo(HaveOccurred())
		}()
		Eventually(func(g Gomega) {
			conn, err := grpc.NewClient("unix://"+listenOptions.Address, grpc.WithTransportCredentials(insecure.NewCredentials()))
			g.Expect(err).NotTo(HaveOccurred())
			ctrlClient := csi.NewControllerClient(conn)
			identityClient := csi.NewIdentityClient(conn)
			controllerResp, err := ctrlClient.ControllerGetCapabilities(ctx, &csi.ControllerGetCapabilitiesRequest{})
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(controllerResp).NotTo(BeNil())

			identityResp, err := identityClient.GetPluginInfo(ctx, &csi.GetPluginInfoRequest{})
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(identityResp).NotTo(BeNil())
			g.Expect(identityResp.Name).To(Equal("csi.snap.nvidia.com"))
		}).WithTimeout(time.Second * 15).WithPolling(time.Second).Should(Succeed())

		Expect(clusterHelper.Started()).To(BeTrue())
		Expect(clusterHelper.Stopped()).To(BeFalse())
		cancel()
		Eventually(func(g Gomega) {
			g.Expect(clusterHelper.Stopped()).To(BeTrue())
		}).WithTimeout(time.Second * 15).WithPolling(time.Second).Should(Succeed())
	})
	It("node", func() {
		preconfigureService := runner.NewFakeRunnable()
		m, err := New(config.PluginConfig{Common: config.Common{
			PluginMode: config.PluginModeNode, ListenOptions: listenOptions},
			Node: config.Node{}},
			WithNodeHandler(&dummyGRPCHandler{}),
			WithPreconfigure(preconfigureService),
		)
		Expect(err).NotTo(HaveOccurred())
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go func() {
			defer GinkgoRecover()
			Expect(m.Start(ctx)).NotTo(HaveOccurred())
		}()
		Eventually(func(g Gomega) {
			conn, err := grpc.NewClient("unix://"+listenOptions.Address, grpc.WithTransportCredentials(insecure.NewCredentials()))
			g.Expect(err).NotTo(HaveOccurred())
			nodeClient := csi.NewNodeClient(conn)
			identityClient := csi.NewIdentityClient(conn)
			nodeResp, err := nodeClient.NodeGetInfo(ctx, &csi.NodeGetInfoRequest{})
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(nodeResp).NotTo(BeNil())
			g.Expect(nodeResp.NodeId).To(Equal("test"))

			identityResp, err := identityClient.GetPluginInfo(ctx, &csi.GetPluginInfoRequest{})
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(identityResp).NotTo(BeNil())
			g.Expect(identityResp.Name).To(Equal("csi.snap.nvidia.com"))
		}).WithTimeout(time.Second * 15).WithPolling(time.Second).Should(Succeed())

		Expect(preconfigureService.Started()).To(BeTrue())
		Expect(preconfigureService.Stopped()).To(BeFalse())
		cancel()
		Eventually(func(g Gomega) {
			g.Expect(preconfigureService.Stopped()).To(BeTrue())
		}).WithTimeout(time.Second * 15).WithPolling(time.Second).Should(Succeed())
	})
	It("dependency timeout", func() {
		// service will never become ready
		preconfigureService := runner.NewFakeRunnable()
		preconfigureService.SetFunction(func(ctx context.Context, _ func()) error {
			<-ctx.Done()
			return nil
		})
		m, err := New(config.PluginConfig{Common: config.Common{
			PluginMode: config.PluginModeNode, ListenOptions: listenOptions},
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
			Expect(m.Start(context.Background())).To(MatchError(ContainSubstring("manager dependencies are not ready")))
		}()
		// the service should complete with an error
		Eventually(stop).WithTimeout(time.Second * 15).Should(BeClosed())
	})
	It("dependency failed", func() {
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
			Expect(m.Start(context.Background())).To(MatchError(ContainSubstring("manager dependencies are not ready")))
		}()
		// the service should complete with an error
		Eventually(stop).WithTimeout(time.Second * 15).Should(BeClosed())
	})
})
