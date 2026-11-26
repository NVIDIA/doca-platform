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

package cmd

import (
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/internal/dpfctl/bmcdump"

	"github.com/spf13/cobra"
	"k8s.io/apimachinery/pkg/api/resource"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type dumpBMCFlags struct {
	namespace      string
	outputDir      string
	devices        []string
	username       string
	caCertFile     string
	timeout        time.Duration
	requestTimeout time.Duration
	clearExisting  bool
	bmcAddress     string
	passwordFile   string
	stopAt         string
	archiveDir     string
	archiveMinFree string
	printLogs      bool
	header         map[string]string
	serial         string
	dpuUID         string
}

var dumpBMCOpts dumpBMCFlags

var dumpBMCCmd = &cobra.Command{
	Use:   "bmc",
	Short: "Create and download BlueField BMC diagnostic dumps",
	Long: `Create and download BlueField BMC diagnostic dumps for DPUDevices.

The command discovers DPUDevices in the host cluster, reads each device's
BMC endpoint and credential Secret, then collects the Manager diagnostic dump
and, on BlueField-4, the System CPU diagnostics dump. Each dump runs as a
Redfish task and is downloaded into its own directory under local artifacts.

The BMC generation is detected at runtime, and the Redfish user and resource
paths follow from it, so the same invocation works on BlueField-3 and
BlueField-4.

By default, this command preserves existing BMC dump entries. Use
--clear-existing to delete existing entries before creating a fresh dump.

Each BMC server certificate is verified against the CA that signed it and the
BMC identity is pinned to the address being dialed. The CA is read from the
trust bundle the DPF Operator publishes, found through the DPFOperatorConfig
(--namespace is used only if it cannot be listed), so no certificate input is
needed. Point --ca-cert-file at a PEM file when the cluster is anchored to a certificate authority of your own.

With --bmc-address the command collects from that one BMC and does not contact
the Kubernetes API: the password is read from --password-file and the CA from
--ca-cert-file. --archive-dir then stores the dumps per serial and keeps the
newest three.`,
	Example: `  # Collect dumps for all discovered DPUDevices
  dpfctl dump bmc --output-dir /tmp/bmc-dumps

  # Collect dumps for selected DPUDevices
  dpfctl dump bmc --devices mt2610604vmk,mt2610604vnc

  # Clear existing BMC dump entries before creating a fresh dump
  dpfctl dump bmc --clear-existing

  # Verify against a certificate authority of your own
  dpfctl dump bmc --ca-cert-file /etc/pki/my-bmc-ca.crt

  # Collect from one BMC without a Kubernetes cluster
  dpfctl dump bmc --bmc-address 10.0.0.10 --password-file ./bmc-password --ca-cert-file ./ca.crt`,
	Args: cobra.NoArgs,
	// Don't print usage on runtime errors; it hides the actual error message.
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runDumpBMC(cmd)
	},
}

func init() {
	dumpCmd.AddCommand(dumpBMCCmd)
	addDumpBMCFlags(dumpBMCCmd)
}

func addDumpBMCFlags(cmd *cobra.Command) {
	f := cmd.Flags()
	f.StringVar(&dumpBMCOpts.namespace, "namespace", bmcdump.DefaultNamespace, "Namespace containing DPUDevices and BMC credential Secrets")
	must(cmd.RegisterFlagCompletionFunc("namespace", completeNamespaces))
	f.StringVar(&dumpBMCOpts.username, "username", "", "BlueField BMC Redfish username (default: auto-detected, root on BF3 and admin on BF4)")
	f.DurationVar(&dumpBMCOpts.timeout, "timeout", 30*time.Minute, "Timeout for each BMC diagnostic dump task")
	must(cmd.RegisterFlagCompletionFunc("timeout", cobra.FixedCompletions([]string{"5m", "15m", "30m", "1h"}, cobra.ShellCompDirectiveNoFileComp)))
	f.DurationVar(&dumpBMCOpts.requestTimeout, "request-timeout", 30*time.Second, "Timeout for each Redfish HTTP request")
	must(cmd.RegisterFlagCompletionFunc("request-timeout", cobra.FixedCompletions([]string{"10s", "30s", "1m"}, cobra.ShellCompDirectiveNoFileComp)))
	f.StringVar(&dumpBMCOpts.outputDir, "output-dir", "", "Local directory for downloaded dumps (default: bmcdump-<timestamp>)")
	must(cmd.RegisterFlagCompletionFunc("output-dir", func(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
		return nil, cobra.ShellCompDirectiveFilterDirs
	}))
	f.StringSliceVar(&dumpBMCOpts.devices, "devices", nil, "DPUDevice names to collect from (comma-separated; default: all)")
	must(cmd.RegisterFlagCompletionFunc("devices", completeDPUDevices))
	f.BoolVar(&dumpBMCOpts.clearExisting, "clear-existing", false, "Clear existing BMC dump entries before creating a new dump")
	must(cmd.RegisterFlagCompletionFunc("clear-existing", cobra.FixedCompletions([]string{"true", "false"}, cobra.ShellCompDirectiveNoFileComp)))
	f.StringVar(&dumpBMCOpts.caCertFile, "ca-cert-file", "", "PEM file with the CA that signed the BMC server certificates (default: the DPF Operator's trust bundle, named by the DPFOperatorConfig and read from its namespace; --namespace is used only when no DPFOperatorConfig can be read)")
	must(cmd.RegisterFlagCompletionFunc("ca-cert-file", func(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
		return []string{"crt", "pem"}, cobra.ShellCompDirectiveFilterFileExt
	}))
	f.StringVar(&dumpBMCOpts.bmcAddress, "bmc-address", "", "Collect from this one BMC (host or host:port) without contacting the Kubernetes API; requires --password-file and --ca-cert-file")
	f.StringVar(&dumpBMCOpts.passwordFile, "password-file", "", "File holding the BMC password, used with --bmc-address")
	f.StringVar(&dumpBMCOpts.stopAt, "stop-at", "", "Time (RFC 3339) at which to stop; until then, failed read requests to the BMC are retried")
	f.StringVar(&dumpBMCOpts.archiveDir, "archive-dir", "", "Store the dumps under <dir>/<serial>/<timestamp>-<dpu-uid>/ and keep the newest three per serial; used with --bmc-address and --serial instead of --output-dir")
	f.StringVar(&dumpBMCOpts.archiveMinFree, "archive-min-free", "2Gi", "Free space in --archive-dir below which nothing is collected")
	f.BoolVar(&dumpBMCOpts.printLogs, "print-logs", false, "Print the collection as JSON lines (a header, the useful logs of the dumps, a result) and nothing else; used with --bmc-address")
	f.StringToStringVar(&dumpBMCOpts.header, "header", nil, "Extra key=value fields for the header line, used with --print-logs")
	f.StringVar(&dumpBMCOpts.serial, "serial", "", "DPU serial number that names the archive directory")
	f.StringVar(&dumpBMCOpts.dpuUID, "dpu-uid", "", "DPU UID appended to the archive directory name")
	// Registered but never read, so invocations written against earlier releases keep working
	// instead of failing on an unknown flag.
	f.Bool("insecure-skip-tls-verify", false, "Skip BMC TLS certificate verification (insecure)")
	must(f.MarkDeprecated("insecure-skip-tls-verify", "it is ignored; BMC server certificates are always verified. Use --ca-cert-file to supply a CA of your own"))
}

func runDumpBMC(cmd *cobra.Command) error {
	ctx, cancel := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	opts, err := dumpBMCCollectOptions()
	if err != nil {
		return err
	}

	// A single BMC given by address needs nothing from the cluster, so no client is created and
	// the command works without a kubeconfig.
	var c client.Client
	if opts.BMCAddress == "" {
		if c, err = newClient(); err != nil {
			return err
		}
	}

	return bmcdump.Collect(ctx, c, opts)
}

// dumpBMCCollectOptions builds the collection options from the command flags.
func dumpBMCCollectOptions() (bmcdump.CollectOptions, error) {
	minFree, err := resource.ParseQuantity(dumpBMCOpts.archiveMinFree)
	if err != nil {
		return bmcdump.CollectOptions{}, fmt.Errorf("parsing --archive-min-free: %w", err)
	}
	var stopAt time.Time
	if dumpBMCOpts.stopAt != "" {
		if stopAt, err = time.Parse(time.RFC3339, dumpBMCOpts.stopAt); err != nil {
			return bmcdump.CollectOptions{}, fmt.Errorf("parsing --stop-at: %w", err)
		}
	}
	// Assigned only when asked for: a nil *os.File stored in the interface would not be nil.
	var printLogs io.Writer
	if dumpBMCOpts.printLogs {
		printLogs = os.Stdout
	}
	return bmcdump.CollectOptions{
		PrintLogs:           printLogs,
		Header:              dumpBMCOpts.header,
		BMCAddress:          dumpBMCOpts.bmcAddress,
		PasswordFile:        dumpBMCOpts.passwordFile,
		StopAt:              stopAt,
		ArchiveDir:          dumpBMCOpts.archiveDir,
		ArchiveMinFreeBytes: minFree.Value(),
		Serial:              dumpBMCOpts.serial,
		DPUUID:              dumpBMCOpts.dpuUID,
		Namespace:           dumpBMCOpts.namespace,
		OutputDir:           dumpBMCOpts.outputDir,
		Devices:             dumpBMCOpts.devices,
		Username:            dumpBMCOpts.username,
		CACertFile:          dumpBMCOpts.caCertFile,
		RequestTimeout:      dumpBMCOpts.requestTimeout,
		TaskTimeout:         dumpBMCOpts.timeout,
		ClearExisting:       dumpBMCOpts.clearExisting,
	}, nil
}

func completeDPUDevices(cmd *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
	c, err := newClient()
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	devices := &provisioningv1.DPUDeviceList{}
	if err := c.List(cmd.Context(), devices, client.InNamespace(dumpBMCOpts.namespace)); err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	names := make([]string, 0, len(devices.Items))
	for _, device := range devices.Items {
		names = append(names, device.Name)
	}
	return names, cobra.ShellCompDirectiveNoFileComp
}
