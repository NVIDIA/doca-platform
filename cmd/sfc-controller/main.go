/*
COPYRIGHT 2024 NVIDIA

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

package main

import (
	"crypto/tls"
	"os"
	"time"

	dpuservicev1 "github.com/nvidia/doca-platform/api/dpuservice/v1alpha1"
	"github.com/nvidia/doca-platform/internal/ovsmodel"
	"github.com/nvidia/doca-platform/internal/ovsutils"
	sfccontroller "github.com/nvidia/doca-platform/internal/sfccontroller/controllers"

	"antrea.io/antrea/pkg/ovs/openflow"
	"github.com/ovn-org/libovsdb/client"
	"github.com/ovn-org/libovsdb/model"
	"github.com/spf13/pflag"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/component-base/logs"
	logsv1 "k8s.io/component-base/logs/api/v1"
	_ "k8s.io/component-base/logs/json/register"
	"k8s.io/klog/v2"
	kexec "k8s.io/utils/exec"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/metrics/filters"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
)

var (
	scheme                  = runtime.NewScheme()
	setupLog                = ctrl.Log.WithName("setup")
	logOptions              = logs.NewOptions()
	fs                      = pflag.CommandLine
	sfcBridgeConenctionAddr = "/var/run/openvswitch/br-sfc.mgmt"
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))

	utilruntime.Must(dpuservicev1.AddToScheme(scheme))
	// +kubebuilder:scaffold:scheme
}

// Add RBAC for the metrics endpoint.
// +kubebuilder:rbac:groups=authentication.k8s.io,resources=tokenreviews,verbs=create
// +kubebuilder:rbac:groups=authorization.k8s.io,resources=subjectaccessreviews,verbs=create

func main() {
	var metricsAddr string
	var enableLeaderElection bool
	var probeAddr string
	var insecureMetrics bool
	var enableHTTP2 bool
	var syncPeriod, staleFlowsRemovalPeriod time.Duration

	fs.StringVar(&metricsAddr, "metrics-bind-address", ":8080", "The address the metric endpoint binds to.")
	fs.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	fs.BoolVar(&enableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	fs.BoolVar(&insecureMetrics, "insecure-metrics", false,
		"If set the metrics endpoint is served insecure without AuthN/AuthZ.")
	fs.BoolVar(&enableHTTP2, "enable-http2", false,
		"If set, HTTP/2 will be enabled for the metrics and webhook servers")
	fs.DurationVar(&syncPeriod, "sync-period", 10*time.Minute,
		"The minimum interval at which watched resources are reconciled.")
	fs.DurationVar(&staleFlowsRemovalPeriod, "stale-flows-removal-period", 1*time.Minute,
		"The interval at which any stale flows on the bridge would be removed.")
	logsv1.AddFlags(logOptions, fs)

	pflag.Parse()
	if err := logsv1.ValidateAndApply(logOptions, nil); err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}
	ctrl.SetLogger(klog.Background())

	// validate required env variables
	nodeName := os.Getenv("SFC_NODE_NAME")
	if nodeName == "" {
		setupLog.Error(nil, "SFC_NODE_NAME environment variable must be set")
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

	metricsOpts := metricsserver.Options{
		BindAddress:    metricsAddr,
		SecureServing:  true,
		FilterProvider: filters.WithAuthenticationAndAuthorization,
	}
	if insecureMetrics {
		metricsOpts.SecureServing = false
		metricsOpts.FilterProvider = nil
	}

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                 scheme,
		Metrics:                metricsOpts,
		WebhookServer:          webhookServer,
		HealthProbeBindAddress: probeAddr,
		Cache: cache.Options{
			SyncPeriod: &syncPeriod,
		},
		// LeaderElectionReleaseOnCancel defines if the leader should step down voluntarily
		// when the Manager ends. This requires the binary to immediately end when the
		// Manager is stopped, otherwise, this setting is unsafe. Setting this significantly
		// speeds up voluntary leader transitions as the new leader don't have to wait
		// LeaseDuration time first.
		//
		// In the default scaffold provided, the program ends immediately after
		// the manager stops, so would be fine to enable this option. However,
		// if you are doing or is intended to do any operation such as perform cleanups
		// after the manager stops then its usage might be unsafe.
		// LeaderElectionReleaseOnCancel: true,
	})
	if err != nil {
		setupLog.Error(err, "unable to create manager")
		os.Exit(1)
	}

	ofb := openflow.NewOFBridge(sfccontroller.SFCBridge, sfcBridgeConenctionAddr)
	if err := ofb.Connect(5, nil); err != nil {
		setupLog.Error(err, "failed to connect to ovs")
		os.Exit(1)
	}
	defer func() {
		_ = ofb.Disconnect()
	}()

	// used to build the flows, it's not creating the bridge in ovs
	oftable := openflow.NewOFTable(0, sfccontroller.SFCBridge, 0, 0, openflow.TableMissActionNormal)

	ofb.NewTable(oftable, 0, openflow.TableMissActionNormal)
	// it's a must other wise getting a panic when building flows
	ofb.Initialize()

	clientDBModel, err := model.NewClientDBModel("Open_vSwitch",
		map[string]model.Model{
			ovsmodel.BridgeTable:      &ovsmodel.Bridge{},
			ovsmodel.OpenvSwitchTable: &ovsmodel.OpenvSwitch{},
			ovsmodel.PortTable:        &ovsmodel.Port{},
			ovsmodel.InterfaceTable:   &ovsmodel.Interface{},
		})
	if err != nil {
		setupLog.Error(err, "failed to create ovs db model client")
		os.Exit(1)
	}

	ctx := ctrl.SetupSignalHandler()

	ovs, err := client.NewOVSDBClient(clientDBModel, client.WithEndpoint("unix:/var/run/openvswitch/db.sock"))
	if err != nil {
		setupLog.Error(err, "failed to create ovsdb client")
		os.Exit(1)
	}

	err = ovs.Connect(ctx)
	if err != nil {
		setupLog.Error(err, "failed to connect to ovs")
		os.Exit(1)
	}
	defer ovs.Disconnect()

	_, err = ovs.Monitor(
		ctx,
		ovs.NewMonitor(
			client.WithTable(&ovsmodel.OpenvSwitch{}),
			client.WithTable(&ovsmodel.Bridge{}),
			client.WithTable(&ovsmodel.Port{}),
			client.WithTable(&ovsmodel.Interface{}),
		),
	)
	if err != nil {
		setupLog.Error(err, "failed to monitor ovs tables")
		os.Exit(1)
	}

	ovsClient := &ovsutils.Client{Client: ovs}

	if err = (&sfccontroller.ServiceInterfaceReconciler{
		Client:   mgr.GetClient(),
		Scheme:   mgr.GetScheme(),
		NodeName: nodeName,
		OVS:      ovsClient,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "ServiceInterface")
		os.Exit(1)
	}
	if err = (&sfccontroller.ServiceChainReconciler{
		Client:   mgr.GetClient(),
		Scheme:   mgr.GetScheme(),
		NodeName: nodeName,
		OFTable:  oftable,
		OFBridge: ofb,
		OVS:      ovsClient,
		Exec:     kexec.New(),
	}).SetupWithManager(ctx, mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "ServiceChain")
		os.Exit(1)
	}
	// +kubebuilder:scaffold:builder

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	staleFlowsRemover := sfccontroller.NewStaleObjectRemover(staleFlowsRemovalPeriod, mgr.GetClient(), ofb, ovsClient)
	if err = mgr.Add(staleFlowsRemover); err != nil {
		setupLog.Error(err, "cannot add runnable to manager")
	}

	setupLog.Info("starting manager")
	if err := mgr.Start(ctx); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}
