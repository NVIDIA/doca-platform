/*
Copyright 2026 NVIDIA

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

// mock-reboot-controller is the "external system" of DPUNode nodeRebootMethod external for
// mock-dpuagent DPUs: it reboots the mock DPUs of an annotated DPUNode and clears the annotation.
// It is a singleton by design; leader election is not implemented.
package main

import (
	"os"
	"time"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/pkg/health"

	"github.com/spf13/pflag"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
)

var setupLog = ctrl.Log.WithName("setup")

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(clientgoscheme.Scheme))
	utilruntime.Must(provisioningv1.AddToScheme(clientgoscheme.Scheme))
}

func main() {
	var metricsAddr, probeAddr string
	var syncPeriod time.Duration
	var hostPort int
	fs := pflag.CommandLine
	fs.StringVar(&metricsAddr, "metrics-bind-address", "0", "The address the metric endpoint binds to; 0 disables metrics.")
	fs.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	fs.DurationVar(&syncPeriod, "sync-period", 10*time.Minute, "The minimum interval at which watched resources are reconciled.")
	fs.IntVar(&hostPort, "host-port", 80, "The mock-dpuagent host control port (POST /reboot).")
	pflag.Parse()
	ctrl.SetLogger(klog.Background())

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                 clientgoscheme.Scheme,
		Metrics:                metricsserver.Options{BindAddress: metricsAddr},
		HealthProbeBindAddress: probeAddr,
		Cache:                  cache.Options{SyncPeriod: &syncPeriod},
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}
	ctx := ctrl.SetupSignalHandler()
	if err := (&DPUNodeRebootReconciler{Client: mgr.GetClient(), HostPort: hostPort}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller")
		os.Exit(1)
	}
	if err := mgr.AddHealthzCheck("healthz", health.APIConnectionCheck(ctx, mgr)); err != nil {
		setupLog.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", health.APIConnectionCheck(ctx, mgr)); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}
	setupLog.Info("starting mock DPUNode reboot controller")
	if err := mgr.Start(ctx); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}
