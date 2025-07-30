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

package main

import (
	"crypto/tls"
	"fmt"
	"os"
	"time"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/test/mock/dms/pkg/certs"
	"github.com/nvidia/doca-platform/test/mock/dms/pkg/config"
	dpu "github.com/nvidia/doca-platform/test/mock/dms/pkg/controllers"
	dmsserver "github.com/nvidia/doca-platform/test/mock/dms/pkg/server"

	"github.com/spf13/pflag"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/component-base/logs"
	logsv1 "k8s.io/component-base/logs/api/v1"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/metrics/filters"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/yaml"
)

const downwardAPIPodName = "POD_NAME"

var (
	setupLog   = ctrl.Log.WithName("setup")
	logOptions = logs.NewOptions()
	fs         = pflag.CommandLine
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(clientgoscheme.Scheme))
	utilruntime.Must(provisioningv1.AddToScheme(clientgoscheme.Scheme))
	// +kubebuilder:scaffold:scheme
}

// Add RBAC for the metrics endpoint.
// +kubebuilder:rbac:groups=authentication.k8s.io,resources=tokenreviews,verbs=create
// +kubebuilder:rbac:groups=authorization.k8s.io,resources=subjectaccessreviews,verbs=create

// A controller which watches on DPU objects. When created add a new server listening on a new port. Add the address in an annotation on the DPU.
// One single DMS pod will mock the API for any number of DMS instances by using ports.
func main() {
	var metricsAddr string
	var enableLeaderElection bool
	var probeAddr string
	var insecureMetrics bool
	var enableHTTP2 bool
	var syncPeriod time.Duration
	var hostIP string
	var certificatePath string
	var configPath string

	fs.StringVar(&metricsAddr, "metrics-bind-address", ":35769", "The address the metric endpoint binds to.")
	fs.StringVar(&probeAddr, "health-probe-bind-address", ":35001", "The address the probe endpoint binds to.")
	fs.BoolVar(&enableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	fs.BoolVar(&insecureMetrics, "insecure-metrics", false,
		"If set the metrics endpoint is served insecure without AuthN/AuthZ.")
	fs.BoolVar(&enableHTTP2, "enable-http2", false,
		"If set, HTTP/2 will be enabled for the metrics and webhook servers")
	fs.DurationVar(&syncPeriod, "sync-period", 10*time.Minute,
		"The minimum interval at which watched resources are reconciled.")
	fs.StringVar(&hostIP, "host-ip", "0.0.0.0", "The IP on which the server will listen.")

	fs.StringVar(&certificatePath, "certificate-path", "/mock-dms-serving-certs/", "The directory containing the server 'tls.cert' cert and 'tls.key' private key.")
	fs.StringVar(&configPath, "config", "/etc/config/config.yaml", "The directory containing the configmap with the configuration for the server")

	logsv1.AddFlags(logOptions, fs)

	pflag.Parse()
	if err := logsv1.ValidateAndApply(logOptions, nil); err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}
	ctrl.SetLogger(klog.Background())

	// Get the pod name from the downward API.
	podName, ok := os.LookupEnv(downwardAPIPodName)
	if !ok {
		setupLog.Error(fmt.Errorf("could not find required environment variable %s", downwardAPIPodName), "unable to start manager")
		os.Exit(1)

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
		Scheme:                 clientgoscheme.Scheme,
		Metrics:                metricsOpts,
		WebhookServer:          webhookServer,
		HealthProbeBindAddress: probeAddr,
		Client: client.Options{
			Cache: &client.CacheOptions{
				Unstructured: true,
			},
		},
		Cache: cache.Options{
			SyncPeriod: &syncPeriod,
		},
		LeaderElection:   enableLeaderElection,
		LeaderElectionID: "e361zzcf.nvidia.com",
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
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	ctx := ctrl.SetupSignalHandler()

	cert, key, err := certs.ServerCertFromDirectory(certificatePath)
	if err != nil {
		setupLog.Error(err, "unable to get server certificate")
		os.Exit(1)
	}
	conf, err := getConfig(configPath)
	if err != nil {
		setupLog.Error(err, "unable to get config")
		os.Exit(1)
	}
	srv := dmsserver.NewDMSServerMux(hostIP, cert, key,
		&dmsserver.DPUNodeToPortListener{MinPort: 30000, MaxPort: 32000},
		&dmsserver.ConfigurableAPIHandler{Config: *conf},
	)
	if err = (&dpu.DMSServerReconciler{
		Client:  mgr.GetClient(),
		PodName: podName,
		// TODO: Fix certificate handling here.
		ListenerManager: srv,
		Server:          srv,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller")
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

	setupLog.Info("starting mock dms controller manager")
	if err := mgr.Start(ctx); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}

func getConfig(configPath string) (*config.Config, error) {
	conf := &config.Config{}
	file, err := os.ReadFile(configPath)
	if err != nil {
		return nil, err
	}
	if err := yaml.Unmarshal(file, conf); err != nil {
		return nil, err
	}
	return conf, nil
}
