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

package manager

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"os"
	"time"

	"github.com/nvidia/doca-platform/internal/storage/snap/csi-plugin/config"
	"github.com/nvidia/doca-platform/internal/storage/snap/csi-plugin/controller/clusterhelper"
	"github.com/nvidia/doca-platform/internal/storage/snap/csi-plugin/grpc/middleware"
	"github.com/nvidia/doca-platform/internal/storage/snap/csi-plugin/handlers/controller"
	"github.com/nvidia/doca-platform/internal/storage/snap/csi-plugin/handlers/identity"
	"github.com/nvidia/doca-platform/internal/storage/snap/csi-plugin/handlers/node"
	"github.com/nvidia/doca-platform/internal/storage/snap/csi-plugin/node/preconfigure"
	utilsMount "github.com/nvidia/doca-platform/internal/storage/snap/csi-plugin/utils/mount"
	utilsNvme "github.com/nvidia/doca-platform/internal/storage/snap/csi-plugin/utils/nvme"
	utilsPci "github.com/nvidia/doca-platform/internal/storage/snap/csi-plugin/utils/pci"
	"github.com/nvidia/doca-platform/internal/storage/snap/csi-plugin/utils/runner"
	"github.com/nvidia/doca-platform/internal/storage/snap/csi-plugin/utils/sync"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"google.golang.org/grpc"
	"k8s.io/klog/v2"
	kmount "k8s.io/mount-utils"
	kexec "k8s.io/utils/exec"
)

const (
	// define how long manager will wait for dependencies to start
	defaultDependenciesWaitTimeout = time.Minute * 1
)

// New create and initialize new instance of ServiceManager
func New(pluginConfig config.PluginConfig, options ...Option) (*ServiceManager, error) {
	var opts managerOptions
	for _, o := range options {
		o.set(&opts)
	}
	var err error

	if opts.dependenciesWaitTimeout == nil {
		dependenciesWaitTimeout := defaultDependenciesWaitTimeout
		opts.dependenciesWaitTimeout = &dependenciesWaitTimeout
	}
	mgr := &ServiceManager{
		serverBindOptions:       pluginConfig.ListenOptions,
		dependenciesWaitTimeout: *opts.dependenciesWaitTimeout,
		runner:                  runner.New(),
	}

	mgr.grpcServer = getGRPCServer()

	klog.V(2).Info("register identity service")
	if opts.identityHandler == nil {
		opts.identityHandler, err = identity.New()
		if err != nil {
			return nil, fmt.Errorf("failed to initialize identity handler: %v", err)
		}
	}
	csi.RegisterIdentityServer(mgr.grpcServer, opts.identityHandler)

	switch pluginConfig.PluginMode {
	case config.PluginModeController:
		klog.V(2).Info("register clusterhelper")
		if opts.clusterhelper == nil {
			opts.clusterhelper = clusterhelper.New(pluginConfig.Controller)
		}
		mgr.runner.AddService("clusterhelper", opts.clusterhelper)

		klog.V(2).Info("register controller service")
		if opts.controllerHandler == nil {
			opts.controllerHandler = controller.New(pluginConfig.Controller, opts.clusterhelper)
		}
		csi.RegisterControllerServer(mgr.grpcServer, opts.controllerHandler)
	case config.PluginModeNode:
		pciUtils := utilsPci.New(pluginConfig.Node.HostRootFS, kexec.New())
		nodeRuntimeConfig := config.NewNodeRuntime()

		klog.V(2).Info("register preconfigure")
		if opts.preconfigure == nil {
			opts.preconfigure = preconfigure.New(pluginConfig.Node, nodeRuntimeConfig, pciUtils)
		}
		mgr.runner.AddService("preconfigure", opts.preconfigure)

		klog.V(2).Info("register node service")
		if opts.nodeHandler == nil {
			opts.nodeHandler = node.New(pluginConfig.Node, nodeRuntimeConfig,
				utilsMount.New(kmount.New(utilsMount.DefaultMounter)),
				utilsNvme.New(),
				pciUtils,
			)
		}
		csi.RegisterNodeServer(mgr.grpcServer, opts.nodeHandler)
	default:
		return nil, fmt.Errorf("unknown pluginMode: %s", config.PluginModeNode)
	}

	return mgr, nil
}

func getGRPCServer() *grpc.Server {
	return grpc.NewServer(grpc.ChainUnaryInterceptor(
		middleware.SetReqIDMiddleware,
		middleware.SetLoggerMiddleware,
		middleware.LogRequestMiddleware,
		middleware.NewSerializeVolumeRequestsMiddleware(sync.NewIDLocker()),
		middleware.SetDefaultErr,
		middleware.LogResponseMiddleware,
	))
}

// ServiceManager contains start logic for GRPC server and
// handlers for GRPC API endpoints
type ServiceManager struct {
	serverBindOptions       config.ServerBindOptions
	dependenciesWaitTimeout time.Duration

	grpcServer *grpc.Server
	runner     runner.Runner
}

type grpcServerWrapper struct {
	server            *grpc.Server
	serverBindOptions config.ServerBindOptions
	listener          net.Listener
}

func (g *grpcServerWrapper) Run(ctx context.Context) error {
	go func() {
		<-ctx.Done()
		klog.V(2).Info("stop GRPC server")
		g.server.Stop()
	}()
	klog.V(2).InfoS("start GRPC server", "network",
		g.serverBindOptions.Network, "address", g.serverBindOptions.Address)
	return g.server.Serve(g.listener)
}

func (g *grpcServerWrapper) Wait(ctx context.Context) error {
	return nil
}

// Start create listener and start GRPC server
func (m *ServiceManager) Start(ctx context.Context) error {
	if err := m.removeStaleUnixSocket(); err != nil {
		return err
	}

	innerCtx, innerCFunc := context.WithCancel(ctx)
	defer innerCFunc()

	listener, err := net.Listen(m.serverBindOptions.Network, m.serverBindOptions.Address)
	if err != nil {
		return err
	}

	depWaitTimeoutCtx, depWaitTimeoutCFunc := context.WithTimeout(ctx, m.dependenciesWaitTimeout)
	defer depWaitTimeoutCFunc()

	errCh := make(chan error, 1)
	go func() {
		errCh <- m.runner.Run(innerCtx)
		depWaitTimeoutCFunc()
	}()

	klog.V(2).Info("wait for dependencies to start")
	err = m.runner.Wait(depWaitTimeoutCtx)
	if err != nil {
		klog.V(2).ErrorS(nil, "timeout, dependencies not ready")
		innerCFunc()
		<-errCh
		return fmt.Errorf("manager dependencies are not ready: %v", err)
	}
	klog.V(2).Info("manager dependencies are ready, start GRPC server")
	m.runner.AddService("grpc server", &grpcServerWrapper{
		server:            m.grpcServer,
		serverBindOptions: m.serverBindOptions,
		listener:          listener,
	})
	return <-errCh
}

// this function detects and remove stale unix socket
func (m *ServiceManager) removeStaleUnixSocket() error {
	if m.serverBindOptions.Network != config.NetUnix {
		return nil
	}
	finfo, err := os.Stat(m.serverBindOptions.Address)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("failed to check unix socket path: %v", err)
	}
	if finfo.Mode().Type() != fs.ModeSocket {
		return fmt.Errorf("socket path already exist, but it is not a socket")
	}
	err = os.RemoveAll(m.serverBindOptions.Address)
	if err != nil {
		return fmt.Errorf("failed to remove socket file: %v", err)
	}
	return nil
}
