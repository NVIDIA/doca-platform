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

package main

import (
	"crypto/tls"
	"os"
	"time"

	vpcnodecontroller "gitlab-master.nvidia.com/doca-platform-foundation/ovn-vpc/ovn/node/controllers"
	"gitlab-master.nvidia.com/doca-platform-foundation/ovn-vpc/ovn/node/nodeutils"
	"gitlab-master.nvidia.com/doca-platform-foundation/ovn-vpc/vfmac"

	dpuservicev1 "github.com/nvidia/doca-platform/api/dpuservice/v1alpha1"
	"github.com/nvidia/doca-platform/pkg/health"
	"github.com/spf13/pflag"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/component-base/logs"
	logsv1 "k8s.io/component-base/logs/api/v1"
	_ "k8s.io/component-base/logs/json/register"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/config"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
)

var (
	scheme     = runtime.NewScheme()
	setupLog   = ctrl.Log.WithName("setup")
	logOptions = logs.NewOptions()
	fs         = pflag.CommandLine
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(dpuservicev1.AddToScheme(scheme))
	// +kubebuilder:scaffold:scheme
}

func main() {
	var pprofAddr string
	var enableLeaderElection bool
	var probeAddr string
	var insecureMetrics bool
	var enableHTTP2 bool
	var syncPeriod, stalePortsRemovalPeriod time.Duration
	var concurrency int
	fs.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	fs.StringVar(&pprofAddr, "pprof-bind-address", ":8082", "The address the pprof endpoint binds to.")
	fs.BoolVar(&enableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	fs.BoolVar(&insecureMetrics, "insecure-metrics", false,
		"If set the metrics endpoint is served insecure without AuthN/AuthZ.")
	fs.BoolVar(&enableHTTP2, "enable-http2", false,
		"If set, HTTP/2 will be enabled for the metrics and webhook servers")
	fs.DurationVar(&syncPeriod, "sync-period", 10*time.Minute,
		"The minimum interval at which watched resources are reconciled.")
	fs.DurationVar(&stalePortsRemovalPeriod, "stale-ports-removal-period", 30*time.Second,
		"The interval at which any stale ports on the bridge would be removed.")
	fs.IntVar(&concurrency, "concurrency", 1,
		"Number of objects to process simultaneously by each controller.")
	logsv1.AddFlags(logOptions, fs)

	pflag.Parse()
	if err := logsv1.ValidateAndApply(logOptions, nil); err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}
	ctrl.SetLogger(klog.Background())

	// validate required env variables
	nodeName := os.Getenv("NODE_NAME")
	if nodeName == "" {
		setupLog.Error(nil, "NODE_NAME environment variable must be set")
	}

	// if the enable-http2 flag is false (the default), http/2 should be disabled
	// due to its vulnerabilities. More specifically, disabling http/2 will
	// prevent from being vulnerable to the HTTP/2 Stream Cancelation and
	// Rapid Reset CVEs. For more information see:
	// - https://github.com/advisories/GHSA-qppj-fm5r-hxr3
	// - https://github.com/advisories/GHSA-4374-p667-p6c8
	disableHTTP2 := func(c *tls.Config) {
		setupLog.Info("disabling http/2")
		c.NextProtos = []string{"http/1.1"}
	}

	tlsOpts := []func(*tls.Config){}
	if !enableHTTP2 {
		tlsOpts = append(tlsOpts, disableHTTP2)
	}

	webhookServer := webhook.NewServer(webhook.Options{
		TLSOpts: tlsOpts,
	})

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                 scheme,
		WebhookServer:          webhookServer,
		HealthProbeBindAddress: probeAddr,
		Cache: cache.Options{
			SyncPeriod: &syncPeriod,
		},
		PprofBindAddress: pprofAddr,
		Controller: config.Controller{
			MaxConcurrentReconciles: concurrency,
		},
	})
	if err != nil {
		setupLog.Error(err, "unable to create manager")
		os.Exit(1)
	}

	ctx := ctrl.SetupSignalHandler()

	ovsClient, err := nodeutils.InitializeOVSClient(ctx)
	if err != nil {
		setupLog.Error(err, "failed to initialize OVS client")
		os.Exit(1)
	}

	// Create a new VFMAC instance with default configuration
	vfmacInstance := vfmac.NewVFMAC(nil, "", "")

	vfMapping, err := vfmacInstance.LoadIfaceMACAddressMapping()
	if err != nil {
		setupLog.Error(err, "failed to load VF MAC address mapping")
		os.Exit(1)
	}

	serviceInterfaceReconciler := &vpcnodecontroller.ServiceInterfaceReconciler{
		Client:    mgr.GetClient(),
		Scheme:    mgr.GetScheme(),
		NodeName:  nodeName,
		OVS:       ovsClient,
		VFMapping: vfMapping,
	}

	if err = serviceInterfaceReconciler.SetupWithManager(ctx, mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "ServiceInterface")
		os.Exit(1)
	}

	// +kubebuilder:scaffold:builder

	if err := mgr.AddHealthzCheck("healthz", health.APIConnectionCheck(ctx, mgr)); err != nil {
		setupLog.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", health.APIConnectionCheck(ctx, mgr)); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	stalePortsRemover := vpcnodecontroller.NewStaleObjectRemover(stalePortsRemovalPeriod, mgr.GetClient(), nodeName, ovsClient)
	if err = mgr.Add(stalePortsRemover); err != nil {
		setupLog.Error(err, "cannot add stalePortsRemover runnable to manager")
		os.Exit(1)
	}

	setupLog.Info("starting manager")
	if err := mgr.Start(ctx); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}
