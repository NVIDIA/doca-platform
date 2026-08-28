//go:build linux

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
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/cmd/dpuagent/opts"
	"github.com/nvidia/doca-platform/internal/provisioning/constants"
	"github.com/nvidia/doca-platform/internal/provisioning/dpuagent"
	"github.com/nvidia/doca-platform/internal/provisioning/dpuagent/operations"
	spiffeheartbeat "github.com/nvidia/doca-platform/internal/provisioning/dpuagent/spiffe"
	hostagenttypes "github.com/nvidia/doca-platform/internal/provisioning/hostagent/service/types"
	provcertificate "github.com/nvidia/doca-platform/internal/provisioning/utils/certificate"
	"github.com/nvidia/doca-platform/internal/provisioning/utils/certificate/bootstrap"
	providentity "github.com/nvidia/doca-platform/internal/provisioning/utils/certificate/identity"

	"github.com/spf13/pflag"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	clientset "k8s.io/client-go/kubernetes"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	restclient "k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	crclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"
)

const (
	dpuAgentPairName       = "dpu-agent-client"
	spiffeTokenWaitTimeout = 90 * time.Second

	// hostMgmtURL is the hostagent installation service on the DPU side of tmfifo. It speaks
	// plain HTTP, so it keeps answering while the DPU clock is wrong enough to break TLS, which is
	// the only situation the clock check exists for.
	hostMgmtURL = "http://[fe80::1%25tmfifo_net0]:11029"
	// hostMgmtTimeout bounds the hostagent call so a wedged tmfifo link cannot delay bootstrap.
	hostMgmtTimeout = 5 * time.Second
)

func main() {
	defer klog.Flush()
	ctrl.SetLogger(klog.Background())

	options := opts.Options{}
	pflag.BoolVar(&options.ZeroTrustMode, "zero-trust-mode", false, "Enable zero trust mode")
	pflag.BoolVar(&options.SpiffeMode, "spiffe-mode", false, "Enable SPIFFE JWT-SVID authentication via tokenFile kubeconfig")
	pflag.Int32Var(&options.ControlPlaneMTU, "control-plane-mtu", 1500, "Control plane MTU")
	pflag.StringVar(&options.Kubeconfig, "kubeconfig", opts.DefaultKubeconfig, "Path to the kubeconfig file")
	pflag.StringVar(&options.BootstrapKubeconfig, "bootstrap-kubeconfig", "", "Path to the bootstrap kubeconfig file (contains bootstrap token for TLS bootstrapping)")
	pflag.StringVar(&options.TokenFilePath, "token-file-path", constants.SpiffeTokenPath, "Path to the SPIFFE JWT token file (SPIFFE mode only)")
	pflag.StringVar(&options.CertDir, "cert-dir", opts.DefaultCertDir, "Directory to store client certificates")
	pflag.StringVar(&options.DPUName, "dpu-name", "", "Name of the DPU")
	pflag.StringVar(&options.DPUNamespace, "dpu-namespace", "", "Namespace of the DPU")
	pflag.StringVar(&options.DPUUID, "dpu-uid", "", "UID of the DPU object, used to reject stale agent status updates")
	pflag.StringVar(&options.DPUType, "dpu-type", string(provisioningv1.DPUTypeUnknown), "DPU hardware type")
	pflag.StringVar(&options.DPUFlavor, "dpuflavor", "", "Path to the DPU flavor YAML file")
	pflag.StringVar(&options.KubeadmSecretName, "kubeadm-secret-name", "", "Name of the Secret containing the Kubeadm join command")
	pflag.StringVar(&options.KubeadmSecretNamespace, "kubeadm-secret-namespace", "", "Namespace of the Secret containing the Kubeadm join command")
	pflag.StringVar(&options.BFBRegistryURL, "bfb-registry-url", "", "HTTP base URL of bfb-registry (scheme://host:port) for downloading files from the registry")
	pflag.StringVar(&options.NodeLabelScriptsDir, "node-label-scripts-dir", opts.DefaultNodeLabelScriptsDir, "Directory containing executable scripts that report DPU cluster Node labels")
	pflag.BoolVar(&options.AstraEnabled, "astra-enabled", false, "Enable astra-specific behavior")
	pflag.IntVar(&options.NICDeviceCount, "nic-device-count", opts.DefaultNICDeviceCount, "Expected NIC device count for provisioning validation")
	pflag.BoolVar(&options.SkipSysctl, "skip-sysctl", false, "Skip sysctl configuration")
	pflag.BoolVar(&options.SkipNetworkConfig, "skip-network-config", false, "Skip network configuration")
	pflag.BoolVar(&options.SkipDNSConfig, "skip-dns-config", false, "Skip DNS configuration")
	pflag.BoolVar(&options.SkipContainerdConfigration, "skip-containerd-config", false, "Skip containerd configuration")
	pflag.BoolVar(&options.SkipSFConfig, "skip-sf-config", false, "Skip SF configuration")
	pflag.BoolVar(&options.SkipVFMac, "skip-vf-mac", false, "Skip VF MAC configuration")
	pflag.BoolVar(&options.SkipOVSRawScript, "skip-ovs-raw-script", false, "Skip OVS raw script configuration")
	pflag.BoolVar(&options.SkipKernelCmdLine, "skip-kernel-cmd-line", false, "Skip kernel cmd line configuration")
	pflag.BoolVar(&options.SkipRemoveBuiltinKubelet, "skip-remove-builtin-kubelet", false, "Skip removing the built-in kubelet configuration")
	pflag.BoolVar(&options.SkipConfigureKubelet, "skip-configure-kubelet", false, "Skip kubelet configuration")
	pflag.BoolVar(&options.SkipStartKubelet, "skip-start-kubelet", false, "Skip starting kubelet")
	pflag.BoolVar(&options.SkipRebootMethodDiscovery, "skip-reboot-method-discovery", false, "Skip MFT-based reboot method discovery")
	pflag.BoolVar(&options.SkipNodeLabeling, "skip-node-labeling", false, "Skip reporting DPU cluster Node labels from scripts")
	pflag.BoolVar(&options.SkipAstra, "skip-astra", false, "Skip Astra-specific behavior")
	pflag.BoolVar(&options.SkipDPUMode, "skip-dpu-mode", false, "Skip DPU privilege mode enforcement (mlxprivhost)")
	pflag.BoolVar(&options.SkipNVConfig, "skip-nvconfig", false, "Skip NIC NVConfig application (mlxconfig)")
	pflag.BoolVar(&options.SkipReboot, "skip-reboot", false, "Report NoAction without performing host reboot")
	fs := flag.NewFlagSet(os.Args[0], flag.ExitOnError)
	klog.InitFlags(fs)
	pflag.VisitAll(func(f *pflag.Flag) {
		if fs.Lookup(f.Name) != nil {
			return
		}
		fs.Var(f.Value, f.Name, f.Usage)
	})
	if err := fs.Parse(os.Args[1:]); err != nil {
		klog.Fatalf("failed to parse flags: %v", err)
	}

	if err := options.Validate(); err != nil {
		klog.Errorf("failed to validate options: %v", err)
		os.Exit(1)
	}

	dpuFlavor := &provisioningv1.DPUFlavor{}
	parseFileOrDie(options.DPUFlavor, YamlParserFunc, dpuFlavor)

	execCtx := klog.NewContext(ctrl.SetupSignalHandler(), klog.Background())

	cfg, err := buildClientConfig(execCtx, &options)
	if err != nil {
		klog.Fatalf("failed to build client config: %v", err)
	}

	scheme := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(provisioningv1.AddToScheme(scheme))

	dpuClient, err := crclient.NewWithWatch(cfg, crclient.Options{Scheme: scheme})
	if err != nil {
		klog.Fatalf("failed to create controller-runtime client: %v", err)
	}
	k8sClient, err := clientset.NewForConfig(cfg)
	if err != nil {
		klog.Fatalf("failed to create kubernetes clientset: %v", err)
	}

	optCtx := &operations.Context{
		Client:      dpuClient,
		WatchClient: dpuClient,
		K8sClient:   k8sClient,
		DPUFlavor:   *dpuFlavor,
		Options:     options,
	}

	agent := dpuagent.NewDPUAgent(optCtx)
	if options.SpiffeMode {
		go spiffeheartbeat.Run(execCtx, spiffeheartbeat.Config{
			Client:       dpuClient,
			DPUName:      options.DPUName,
			DPUNamespace: options.DPUNamespace,
			DPUUID:       options.DPUUID,
		})
	}
	if err := agent.Run(execCtx); err != nil {
		if execCtx.Err() != nil {
			if shutdownErr := agent.Shutdown(); shutdownErr != nil {
				klog.ErrorS(shutdownErr, "failed to stop local DMS server after DPU agent error")
			}
			klog.Info("DPU agent stopped")
			return
		}
		if !dpuagent.IsBootstrapAbortErr(err) {
			klog.Fatalf("failed to run DPU agent bootstrap: %v", err)
		}
		klog.Info("Bootstrap aborted for reprovision")
	} else {
		klog.Info("DPUAgent successfully completed all operations")
	}
	klog.Info("DPUAgent successfully completed all operations")
	agent.StartCACertUpdateLoop(execCtx)
	agent.StartNICRuntimeConfigLoop(execCtx)
	agent.StartDPUReconcileLoop(execCtx)
	agent.StartSPIREAttestorLoop(execCtx)
	<-execCtx.Done()
	if err := agent.Shutdown(); err != nil {
		klog.ErrorS(err, "failed to stop local DMS server during DPU agent shutdown")
	}
	klog.Info("DPUAgent stop signal received")
}

func buildClientConfig(ctx context.Context, options *opts.Options) (*restclient.Config, error) {
	// Report on entry, so skew is recorded on the DPU object while it is blocking identity, and
	// again on return, so a clock corrected in between clears that report. Zero-trust DPUs are
	// skipped.
	if !options.ZeroTrustMode {
		reportClock(ctx, options)
		defer reportClock(ctx, options)
	}

	if options.SpiffeMode {
		if options.TokenFilePath == "" {
			return nil, fmt.Errorf("token-file-path is required in SPIFFE mode")
		}
		if err := waitForNonEmptyTokenFile(ctx, options.TokenFilePath, spiffeTokenWaitTimeout); err != nil {
			return nil, err
		}
		cfg, err := clientcmd.BuildConfigFromFlags("", options.Kubeconfig)
		if err != nil {
			return nil, fmt.Errorf("loading SPIFFE kubeconfig: %w", err)
		}
		return cfg, nil
	}

	if options.BootstrapKubeconfig == "" {
		return clientcmd.BuildConfigFromFlags("", options.Kubeconfig)
	}

	certConfig, clientConfig, err := bootstrap.LoadClientConfig(
		options.Kubeconfig, options.BootstrapKubeconfig, options.CertDir, dpuAgentPairName,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to load client config: %w", err)
	}

	commonName := providentity.DPUAgentUsername(options.DPUName)
	newClientsetFn := func(current *tls.Certificate) (clientset.Interface, error) {
		config := certConfig
		if current != nil {
			config = clientConfig
		}
		return clientset.NewForConfig(config)
	}
	clientCertificateManager, err := provcertificate.NewCertificateManager(
		options.CertDir,
		"",
		clientConfig.CertFile,
		clientConfig.KeyFile,
		newClientsetFn,
		dpuAgentPairName,
		commonName,
		[]string{providentity.DPUAgentOrganization},
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create certificate manager: %w", err)
	}

	transportConfig := restclient.AnonymousClientConfig(clientConfig)
	_, err = provcertificate.UpdateTransport(wait.NeverStop, transportConfig, clientCertificateManager, 0)
	if err != nil {
		return nil, fmt.Errorf("failed to update transport: %w", err)
	}

	klog.V(2).InfoS("Starting client certificate rotation")
	clientCertificateManager.Start()

	err = wait.PollUntilContextCancel(ctx, 10*time.Second, true, func(_ context.Context) (bool, error) {
		if clientCertificateManager.Current() != nil {
			return true, nil
		}
		klog.Info("Client certificate is not available yet, waiting...")
		return false, nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to wait for client certificate: %w", err)
	}
	klog.InfoS("TLS bootstrapping completed", "cn", commonName)

	return transportConfig, nil
}

// reportClock hands the DPU clock to the hostagent, which compares it against its own clock and
// records the result on the DPU object. The agent does neither itself: it has no trusted time
// reference, and no cluster credentials to write with, since obtaining them is exactly what a skewed
// clock prevents.
//
// This is trusted-host only. Zero-trust DPUs treat the host as untrusted, so neither its clock nor
// its writes to the DPU object are acceptable there; that mode needs its own time reference.
func reportClock(ctx context.Context, options *opts.Options) {
	err := postClockReport(ctx, hostMgmtURL, hostagenttypes.ReportClockRequest{
		DPUName:      options.DPUName,
		DPUNamespace: options.DPUNamespace,
		DPUUID:       options.DPUUID,
		DPUTime:      metav1.Now(),
	})
	if err != nil {
		// A DPU that cannot reach the hostagent at all has a louder problem than clock skew, and
		// this check must not become a second way for bootstrap to fail.
		klog.V(1).InfoS("Skipping DPU clock check", "err", err)
	}
}

// postClockReport sends the DPU clock to the hostagent, which judges it and records the result.
func postClockReport(ctx context.Context, baseURL string, request hostagenttypes.ReportClockRequest) error {
	body, err := json.Marshal(request)
	if err != nil {
		return err
	}

	reqCtx, cancel := context.WithTimeout(ctx, hostMgmtTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, baseURL+"/report-clock", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer func() {
		_ = resp.Body.Close()
	}()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("hostagent rejected the clock report: %s", resp.Status)
	}
	return nil
}

func waitForNonEmptyTokenFile(ctx context.Context, path string, timeout time.Duration) error {
	err := wait.PollUntilContextTimeout(ctx, time.Second, timeout, true, func(_ context.Context) (bool, error) {
		data, readErr := os.ReadFile(path)
		return readErr == nil && len(strings.TrimSpace(string(data))) > 0, nil
	})
	if err != nil {
		return fmt.Errorf("waiting for non-empty SPIFFE token file %s: %w", path, err)
	}
	return nil
}

type ParseFunc func(data []byte, obj interface{}) error

func YamlParserFunc(data []byte, obj interface{}) error { return yaml.Unmarshal(data, obj) }

func parseFile(name string, parse ParseFunc, obj interface{}) error {
	data, err := os.ReadFile(name)
	if err != nil {
		return fmt.Errorf("failed to read file %s: %w", name, err)
	}
	if err := parse(data, obj); err != nil {
		return fmt.Errorf("failed to parse file %s: %w", name, err)
	}
	return nil
}

func parseFileOrDie(name string, parse ParseFunc, obj interface{}) {
	err := parseFile(name, parse, obj)
	if err != nil {
		klog.Fatalf("failed to parse file %s: %v", name, err)
	}
}
