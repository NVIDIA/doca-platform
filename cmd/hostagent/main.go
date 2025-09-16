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
	"flag"
	"os"

	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"
	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/cmd/hostagent/app"
	"github.com/nvidia/doca-platform/cmd/hostagent/options"
	"github.com/nvidia/doca-platform/internal/provisioning/hostagent"
	"github.com/nvidia/doca-platform/internal/provisioning/hostagent/networkmanager"
	"github.com/nvidia/doca-platform/internal/provisioning/hostagent/nodemanager"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/config"
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(provisioningv1.AddToScheme(scheme))
	utilruntime.Must(operatorv1.AddToScheme(scheme))
}

func main() {
	opts := &options.HostAgentFlags{}

	// define a new flag set to avoid flag redefinition error caused by package sigs.k8s.io/controller-runtime/pkg/client/config
	fs := flag.NewFlagSet(os.Args[0], flag.ExitOnError)
	fs.Usage = flag.Usage

	klog.InitFlags(fs)
	defer klog.Flush()

	fs.StringVar(&opts.BootstrapPath, "bootstrap-kubeconfig", "", "Path to kubeconfig file with authorization and master location information.")
	fs.StringVar(&opts.KubeconfigPath, "kubeconfig", "", "Path to kubeconfig file.")
	fs.StringVar(&opts.CertDir, "cert-dir", options.CertDir, "The directory where the TLS certs are located.")
	fs.IntVar(&opts.KubeAPIQPS, "kube-api-qps", 5, "The QPS to use while talking with kubernetes API servers.")
	fs.IntVar(&opts.KubeAPIBurst, "kube-api-burst", 10, "The burst to use while talking with kubernetes API servers.")
	fs.StringVar(&opts.BFBRegistryAddress, "bfb-registry-address", "", "The address of the BFB registry from which BFBs are downloaded.")
	if err := fs.Parse(os.Args[1:]); err != nil {
		klog.Fatalf("Failed to parse flags: %v", err)
	}

	clientCfg := app.GetConfigOrDie(opts)
	unCachedClient, err := client.New(clientCfg, client.Options{Scheme: scheme})
	if err != nil {
		klog.Fatalf("failed to create un-cached client: %v", err)
	}
	dpuNodeManager := nodemanager.NewNodeManager(unCachedClient)
	if err := dpuNodeManager.Start(); err != nil {
		klog.Fatalf("failed to start node manager: %v", err)
	}

	if err := networkmanager.ConvertVFConfigToNetworkRequest(unCachedClient); err != nil {
		klog.Fatalf("failed to convert VF config to network request: %v", err)
	}

	ctrl.SetLogger(klog.Background())
	mgr, err := ctrl.NewManager(clientCfg, ctrl.Options{
		Scheme: scheme,
		Client: client.Options{
			Cache: &client.CacheOptions{
				// Don't cache Secrets, ConfigMaps and DPUNodes. In general, the
				// controller-runtime client does a LIST and WATCH to cache
				// kinds you request (see https://github.com/kubernetes-sigs/controller-runtime/pull/1249),
				// and this can mean caching all secrets and configmaps; when
				// all that's required is the few that are referenced for
				// objects of interest to the controllers.
				DisableFor: []client.Object{&corev1.Secret{}, &corev1.ConfigMap{}, &provisioningv1.DPUNode{}},
			},
		},
		Controller: config.Controller{
			MaxConcurrentReconciles: 1,
		},
		LeaderElection:          true,
		LeaderElectionID:        "82182zs3.nvidia.com",
		LeaderElectionNamespace: "default",
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

	nm := networkmanager.NewNetworkManager(mgr.GetClient())
	if err := nm.Start(); err != nil {
		setupLog.Error(err, "unable to start network manager")
		os.Exit(1)
	}

	reconciler := hostagent.NewHostAgentReconciler(mgr.GetClient(), opts.BFBRegistryAddress, dpuNodeManager, nm)
	if err = reconciler.SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "DPU")
		os.Exit(1)
	}

	// Start the manager
	setupLog.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}
