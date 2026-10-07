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
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/nvidia/doca-platform/hack/tools/dpfdev/pkg/config"
	"github.com/nvidia/doca-platform/hack/tools/dpfdev/pkg/gitlab"

	"github.com/spf13/cobra"
)

const (
	dpuHealthPoolAll      = "all"
	dpuHealthPoolPhysical = "physical"
	dpuHealthPoolNICCloud = "nic-cloud"

	defaultNICCloudRunnerTag = "managed/nic-cloud"
	defaultNICCloudSetupInfo = "/workspace/cloud_tools/.setup_info"

	dpuHealthColorAuto   = "auto"
	dpuHealthColorAlways = "always"
	dpuHealthColorNever  = "never"

	// Default SSH users per environment. Physical and NIC Cloud setups use
	// different users and (crucially) different passwords, so each set is
	// resolved independently and never falls back to the other.
	defaultPhysicalUser = "depuser"
	defaultCloudUser    = "root"

	// Environment variables used to override users and supply passwords.
	// Passwords have no defaults and must be provided by the user.
	envPhysicalUser     = "PHYSICAL_USER"
	envPhysicalPassword = "PHYSICAL_PASSWORD"
	envCloudUser        = "CLOUD_SSH_USER"
	envCloudPassword    = "VM_PASSWORD"
	envCloudPasswordAlt = "CLOUD_SSH_PASSWORD"

	// ansiReset, ansiRed, ansiGreen, ansiYellow and ansiCyan are declared in
	// runner_list.go (same package); only the bold/dim variants are added here.
	ansiBoldGreen  = "\033[1;32m"
	ansiBoldRed    = "\033[1;31m"
	ansiBoldYellow = "\033[1;33m"
	ansiBoldCyan   = "\033[1;36m"
	ansiDim        = "\033[2m"
	ansiBold       = "\033[1m"
)

var (
	dpuHealthPhysicalHosts  []string
	dpuHealthPhysicalUser   string
	dpuHealthCloudUser      string
	dpuHealthNICHosts       []string
	dpuHealthNICHostsFile   string
	dpuHealthRunnerTag      string
	dpuHealthSetupInfo      string
	dpuHealthMasterHost     string
	dpuHealthWorkers        []string
	dpuHealthConnectTimeout time.Duration
	dpuHealthCommandTimeout time.Duration
	dpuHealthRequireUp      bool
	dpuHealthNoGitLab       bool
	dpuHealthSecretsFile    string
	dpuHealthColorMode      string
	dpuHealthVerbose        bool
)

var defaultDPUHealthPhysicalHosts = []string{
	"cloud-dev-ci01",
	"cloud-dev-ci02",
	"cloud-dev-ci03",
	"cloud-dev-ci04",
	"setup28-dpf",
	"setup34-dpf",
	"setup35-dpf",
}

func init() {
	runnerCmd.AddCommand(runnerDPUHealthCmd)

	runnerDPUHealthCmd.Flags().StringSliceVar(&dpuHealthPhysicalHosts, "physical-host", nil, "Physical CI host short name to probe; repeat or comma-separate to override defaults")
	runnerDPUHealthCmd.Flags().StringVar(&dpuHealthPhysicalUser, "physical-user", defaultPhysicalUser, "SSH user for physical CI setups (env "+envPhysicalUser+"); password comes from env "+envPhysicalPassword)
	runnerDPUHealthCmd.Flags().StringVar(&dpuHealthCloudUser, "cloud-user", defaultCloudUser, "SSH user for NIC Cloud runners (env "+envCloudUser+"); password comes from env "+envCloudPassword)
	runnerDPUHealthCmd.Flags().StringSliceVar(&dpuHealthNICHosts, "nic-cloud-host", nil, "NIC Cloud runner host/IP to probe; repeat or comma-separate to add explicit targets")
	runnerDPUHealthCmd.Flags().StringVar(&dpuHealthNICHostsFile, "nic-cloud-hosts-file", "", "Optional file with NIC Cloud runner hosts/IPs, used only when no explicit hosts and no GitLab discovery")
	runnerDPUHealthCmd.Flags().StringVar(&dpuHealthRunnerTag, "nic-cloud-runner-tag", defaultNICCloudRunnerTag, "GitLab tag used to discover NIC Cloud runners")
	runnerDPUHealthCmd.Flags().StringVar(&dpuHealthSetupInfo, "nic-cloud-setup-info", defaultNICCloudSetupInfo, "Path to NIC Cloud .setup_info on runner hosts")
	runnerDPUHealthCmd.Flags().StringVar(&dpuHealthMasterHost, "master-host", "master1", "Master host to query for DPU phases when local kubectl is unavailable")
	runnerDPUHealthCmd.Flags().StringSliceVar(&dpuHealthWorkers, "worker", []string{"worker1", "worker2"}, "Physical worker names to probe from each CI host")
	runnerDPUHealthCmd.Flags().DurationVar(&dpuHealthConnectTimeout, "ssh-connect-timeout", 20*time.Second, "SSH connection timeout")
	runnerDPUHealthCmd.Flags().DurationVar(&dpuHealthCommandTimeout, "ssh-command-timeout", 5*time.Minute, "Timeout for each runner/host probe")
	runnerDPUHealthCmd.Flags().BoolVar(&dpuHealthRequireUp, "require-up", true, "Require detected physical DPU PF netdev operstate to be up; NIC Cloud checks netdev presence only")
	runnerDPUHealthCmd.Flags().BoolVar(&dpuHealthNoGitLab, "no-gitlab", false, "Skip GitLab NIC Cloud runner discovery and use explicit hosts or host file only")
	runnerDPUHealthCmd.Flags().StringVar(&dpuHealthSecretsFile, "secrets-file", "", "Optional env file to load SSH passwords and tokens from before probing")
	runnerDPUHealthCmd.Flags().StringVar(&dpuHealthColorMode, "color", "auto", "Colorize output: auto, always, or never")
	runnerDPUHealthCmd.Flags().BoolVar(&dpuHealthVerbose, "verbose", false, "Show per-host probe progress before the summary")
}

var runnerDPUHealthCmd = &cobra.Command{
	Use:   "dpu-health [physical|cloud|nic-cloud|all]",
	Short: "Check DPU PF interface health on CI runner workers",
	Long: `SSH into physical and/or NIC Cloud runner environments and summarize BlueField DPU PF interface health.

Run without arguments to check both physical and NIC Cloud runners. Use "physical"
for lab physical setups, or "cloud" / "nic-cloud" for NIC Cloud runners.

Physical mode SSHs to each CI runner host, then probes worker1/worker2 from there.
NIC Cloud mode discovers active managed/nic-cloud runners from GitLab, SSHs to each
runner, reads /workspace/cloud_tools/.setup_info, and probes CLOUD_PLAYER_1_IP and
CLOUD_PLAYER_2_IP from the runner.

Physical checks require DPU PF interfaces to exist and be up. NIC Cloud checks only
require the interfaces to exist, because links can be down when no test owns the setup.

Physical and NIC Cloud setups use different SSH credentials, resolved independently.
The physical user defaults to "depuser" (--physical-user or PHYSICAL_USER) with password
from PHYSICAL_PASSWORD; the NIC Cloud user defaults to "root" (--cloud-user or
CLOUD_SSH_USER) with password from VM_PASSWORD (or CLOUD_SSH_PASSWORD). Passwords are read
from the environment only; a selected pool with no password is skipped with a warning.
The default output is summary-only; use --verbose for per-host probe progress.`,
	Example: `  # Check physical and NIC Cloud workers
  dpfdev runner dpu-health

  # Check physical only
  dpfdev runner dpu-health physical

  # Check NIC Cloud only using GitLab discovery (cloud is an alias for nic-cloud)
  dpfdev runner dpu-health cloud

  # Check one explicit NIC Cloud runner without GitLab discovery
  dpfdev runner dpu-health cloud --no-gitlab --nic-cloud-host 10.10.20.30

  # Show per-host probe progress before the summary
  dpfdev runner dpu-health physical --verbose`,
	Args: cobra.ArbitraryArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		pools, err := parseDPUHealthPools(args)
		if err != nil {
			return err
		}
		if err := loadDPUHealthSecrets(); err != nil {
			return err
		}
		color, err := resolveDPUHealthColorMode(dpuHealthColorMode, os.Stdout)
		if err != nil {
			return err
		}
		masterHost := dpuHealthMasterHost
		if !cmd.Flags().Changed("master-host") {
			masterHost = envOrDefault("MASTER_HOST", dpuHealthMasterHost)
		}

		// Resolve per-environment credentials independently. Users default to
		// depuser/root but may be overridden by flag or env; passwords have no
		// default and are read from the environment only.
		physicalUser := dpuHealthPhysicalUser
		if !cmd.Flags().Changed("physical-user") {
			physicalUser = envOrDefault(envPhysicalUser, dpuHealthPhysicalUser)
		}
		cloudUser := dpuHealthCloudUser
		if !cmd.Flags().Changed("cloud-user") {
			cloudUser = envOrDefault(envCloudUser, dpuHealthCloudUser)
		}

		options := dpuHealthOptions{
			pools:            pools,
			physicalHosts:    dpuHealthResolvedPhysicalHosts(),
			physicalUser:     physicalUser,
			physicalPassword: os.Getenv(envPhysicalPassword),
			cloudUser:        cloudUser,
			cloudPassword:    firstNonEmptyEnv(envCloudPassword, envCloudPasswordAlt),
			nicHosts:         dpuHealthNICHosts,
			nicHostsFile:     os.ExpandEnv(dpuHealthNICHostsFile),
			nicRunnerTag:     dpuHealthRunnerTag,
			nicSetupInfo:     dpuHealthSetupInfo,
			masterHost:       masterHost,
			workers:          dpuHealthWorkers,
			connectTimeout:   dpuHealthConnectTimeout,
			commandTimeout:   dpuHealthCommandTimeout,
			requireUp:        dpuHealthRequireUp,
			noGitLab:         dpuHealthNoGitLab,
			color:            color,
			verbose:          dpuHealthVerbose,
			ssh:              realSSHExecutor{},
		}

		return runDPUHealth(cmd.Context(), os.Stdout, options)
	},
}

type dpuHealthOptions struct {
	pools            map[string]bool
	physicalHosts    []string
	physicalUser     string
	physicalPassword string
	cloudUser        string
	cloudPassword    string
	nicHosts         []string
	nicHostsFile     string
	nicRunnerTag     string
	nicSetupInfo     string
	masterHost       string
	workers          []string
	connectTimeout   time.Duration
	commandTimeout   time.Duration
	requireUp        bool
	noGitLab         bool
	color            bool
	verbose          bool
	ssh              sshExecutor
}

type sshExecutor interface {
	Run(ctx context.Context, target, user, password, script string, connectTimeout, commandTimeout time.Duration) (string, error)
}

type realSSHExecutor struct{}

type dpuHealthResult struct {
	Kind   string
	Setup  string
	Worker string
	IP     string
	Reach  string
	DPU    string
	Phase  string
	Detail string
}

type dpuHealthRunnerTarget struct {
	ID          int
	Description string
	Host        string
	Active      bool
	Online      bool
	Status      string
}

func parseDPUHealthPools(args []string) (map[string]bool, error) {
	pools := map[string]bool{}
	for _, arg := range args {
		switch strings.ToLower(arg) {
		case dpuHealthPoolAll:
			pools[dpuHealthPoolPhysical] = true
			pools[dpuHealthPoolNICCloud] = true
		case dpuHealthPoolPhysical:
			pools[dpuHealthPoolPhysical] = true
		case dpuHealthPoolNICCloud, "cloud":
			pools[dpuHealthPoolNICCloud] = true
		default:
			return nil, fmt.Errorf("unknown pool %q: expected physical, nic-cloud, cloud, or all", arg)
		}
	}
	if len(pools) == 0 {
		pools[dpuHealthPoolPhysical] = true
		pools[dpuHealthPoolNICCloud] = true
	}
	return pools, nil
}

func loadDPUHealthSecrets() error {
	secretsFile := os.ExpandEnv(dpuHealthSecretsFile)
	if secretsFile == "" {
		return nil
	}
	if _, err := os.Stat(secretsFile); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return fmt.Errorf("checking secrets file: %w", err)
	}
	values, err := parseEnvFile(secretsFile)
	if err != nil {
		return err
	}
	for key, value := range values {
		if os.Getenv(key) == "" {
			if err := os.Setenv(key, value); err != nil {
				return fmt.Errorf("setting %s from secrets file: %w", key, err)
			}
		}
	}
	return nil
}

func parseEnvFile(path string) (map[string]string, error) {
	file, err := os.Open(filepath.Clean(path))
	if err != nil {
		return nil, fmt.Errorf("opening secrets file: %w", err)
	}
	defer func() {
		_ = file.Close()
	}()

	values := map[string]string{}
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if key == "" {
			continue
		}
		values[key] = trimEnvValue(value)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("reading secrets file: %w", err)
	}
	return values, nil
}

func trimEnvValue(value string) string {
	if len(value) >= 2 {
		if (value[0] == '"' && value[len(value)-1] == '"') || (value[0] == '\'' && value[len(value)-1] == '\'') {
			return value[1 : len(value)-1]
		}
	}
	return value
}

func dpuHealthResolvedPhysicalHosts() []string {
	if len(dpuHealthPhysicalHosts) > 0 {
		return uniqueNonEmpty(dpuHealthPhysicalHosts)
	}
	return append([]string{}, defaultDPUHealthPhysicalHosts...)
}

func resolveDPUHealthColorMode(mode string, w io.Writer) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "", dpuHealthColorAuto:
		if os.Getenv("NO_COLOR") != "" {
			return false, nil
		}
		file, ok := w.(*os.File)
		if !ok {
			return false, nil
		}
		stat, err := file.Stat()
		if err != nil {
			return false, nil
		}
		return stat.Mode()&os.ModeCharDevice != 0, nil
	case dpuHealthColorAlways:
		return true, nil
	case dpuHealthColorNever:
		return false, nil
	default:
		return false, fmt.Errorf("--color must be auto, always, or never")
	}
}

func runDPUHealth(ctx context.Context, w io.Writer, options dpuHealthOptions) error {
	if err := validateDPUHealthOptions(options); err != nil {
		return err
	}
	if err := checkDPUHealthRequirements(); err != nil {
		return err
	}

	var results []dpuHealthResult
	var warnings []string
	if options.verbose {
		fmt.Fprintf(w, "DPU runner health status (%s)\n", time.Now().Format(time.RFC3339))
	}

	if options.pools[dpuHealthPoolPhysical] {
		if options.physicalPassword == "" {
			warnings = append(warnings, fmt.Sprintf("physical checks skipped: set %s (SSH user defaults to %q, override with --physical-user or %s)", envPhysicalPassword, defaultPhysicalUser, envPhysicalUser))
		} else {
			results = append(results, checkDPUHealthPhysical(ctx, w, options)...)
		}
	}

	if options.pools[dpuHealthPoolNICCloud] {
		if options.cloudPassword == "" {
			warnings = append(warnings, fmt.Sprintf("NIC Cloud checks skipped: set %s or %s (SSH user defaults to %q, override with --cloud-user or %s)", envCloudPassword, envCloudPasswordAlt, defaultCloudUser, envCloudUser))
		} else {
			nicTargets, nicWarnings, err := discoverDPUHealthNICTargets(options)
			if err != nil {
				warnings = append(warnings, err.Error())
			}
			warnings = append(warnings, nicWarnings...)
			results = append(results, checkDPUHealthNICCloud(ctx, w, options, nicTargets)...)
		}
	}

	printDPUHealthSummary(w, results, warnings, options.color)
	if issues := countDPUHealthIssues(results); issues > 0 {
		return fmt.Errorf("%d DPU health issue(s) found", issues)
	}
	if len(results) == 0 {
		return fmt.Errorf("no DPU health checks were run")
	}
	return nil
}

func validateDPUHealthOptions(options dpuHealthOptions) error {
	if options.connectTimeout <= 0 {
		return fmt.Errorf("--ssh-connect-timeout must be greater than zero")
	}
	if options.commandTimeout <= 0 {
		return fmt.Errorf("--ssh-command-timeout must be greater than zero")
	}
	if len(options.workers) == 0 && options.pools[dpuHealthPoolPhysical] {
		return fmt.Errorf("at least one --worker is required for physical checks")
	}
	return nil
}

func checkDPUHealthRequirements() error {
	if _, err := exec.LookPath("sshpass"); err != nil {
		return fmt.Errorf("sshpass not found in PATH")
	}
	if _, err := exec.LookPath("ssh"); err != nil {
		return fmt.Errorf("ssh not found in PATH")
	}
	return nil
}

func checkDPUHealthPhysical(ctx context.Context, w io.Writer, options dpuHealthOptions) []dpuHealthResult {
	results := []dpuHealthResult{}
	// Physical setups use a single credential set for both hops: SSH to the CI
	// runner host, then from there to each worker.
	physicalUser := options.physicalUser
	physicalPassword := options.physicalPassword

	if options.verbose {
		fmt.Fprintf(w, "\nPhysical runners (%d hosts)\n", len(options.physicalHosts))
	}
	for _, host := range options.physicalHosts {
		fqdn := dpuHealthPhysicalFQDN(host)
		if options.verbose {
			fmt.Fprintf(w, "  probing %s (%s)\n", host, fqdn)
		}
		script := physicalDPUHealthScript(physicalUser, options.masterHost, options.workers, options.connectTimeout, options.requireUp)
		out, err := options.ssh.Run(ctx, fqdn, physicalUser, physicalPassword, script, options.connectTimeout, options.commandTimeout)
		if err != nil {
			if options.verbose {
				fmt.Fprintf(w, "    ssh failed: %v\n", err)
			}
			results = append(results, dpuHealthResult{
				Kind:   dpuHealthPoolPhysical,
				Setup:  host,
				Worker: "-",
				IP:     "-",
				Reach:  "ssh_fail",
				DPU:    "-",
				Phase:  "-",
				Detail: err.Error(),
			})
			continue
		}
		hostResults := parseDPUHealthResults(dpuHealthPoolPhysical, host, out)
		if len(hostResults) == 0 {
			hostResults = noDPUHealthResults(dpuHealthPoolPhysical, host, "probe produced no RESULT lines")
		}
		if options.verbose {
			printDPUHealthProbeResults(w, hostResults, options.color)
		}
		results = append(results, hostResults...)
	}
	return results
}

func checkDPUHealthNICCloud(ctx context.Context, w io.Writer, options dpuHealthOptions, targets []dpuHealthRunnerTarget) []dpuHealthResult {
	results := []dpuHealthResult{}
	if len(targets) == 0 {
		if options.verbose {
			fmt.Fprintln(w, "\nNIC Cloud runners: no targets to probe")
		}
		return results
	}

	runnerUser := options.cloudUser
	runnerPassword := options.cloudPassword

	if options.verbose {
		fmt.Fprintf(w, "\nNIC Cloud runners (%d hosts)\n", len(targets))
	}
	for _, target := range targets {
		if options.verbose {
			fmt.Fprintf(w, "  probing %s\n", target.Host)
		}
		script := nicCloudDPUHealthScript(runnerUser, options.nicSetupInfo, options.masterHost, options.connectTimeout)
		out, err := options.ssh.Run(ctx, target.Host, runnerUser, runnerPassword, script, options.connectTimeout, options.commandTimeout)
		if err != nil {
			if options.verbose {
				fmt.Fprintf(w, "    ssh failed: %v\n", err)
			}
			results = append(results, dpuHealthResult{
				Kind:   dpuHealthPoolNICCloud,
				Setup:  target.Host,
				Worker: "-",
				IP:     "-",
				Reach:  "ssh_fail",
				DPU:    "-",
				Phase:  "-",
				Detail: err.Error(),
			})
			continue
		}
		hostResults := parseDPUHealthResults(dpuHealthPoolNICCloud, target.Host, out)
		if len(hostResults) == 0 {
			hostResults = noDPUHealthResults(dpuHealthPoolNICCloud, target.Host, "probe produced no RESULT lines")
		}
		if options.verbose {
			printDPUHealthProbeResults(w, hostResults, options.color)
		}
		results = append(results, hostResults...)
	}
	return results
}

func discoverDPUHealthNICTargets(options dpuHealthOptions) ([]dpuHealthRunnerTarget, []string, error) {
	targets := map[string]dpuHealthRunnerTarget{}
	warnings := []string{}

	for _, host := range options.nicHosts {
		host = strings.TrimSpace(host)
		if host == "" {
			continue
		}
		targets[host] = dpuHealthRunnerTarget{Host: host, Active: true}
	}

	if !options.noGitLab {
		gitlabTargets, err := discoverDPUHealthGitLabTargets(options.nicRunnerTag)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("GitLab NIC Cloud discovery skipped: %v", err))
		} else {
			for _, target := range gitlabTargets {
				targets[target.Host] = target
			}
		}
	}

	if len(targets) == 0 {
		fileTargets, err := readDPUHealthNICHostsFile(options.nicHostsFile)
		if err != nil {
			if !errors.Is(err, os.ErrNotExist) {
				return nil, warnings, err
			}
		}
		for _, target := range fileTargets {
			targets[target.Host] = target
		}
	}

	result := make([]dpuHealthRunnerTarget, 0, len(targets))
	for _, target := range targets {
		result = append(result, target)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Host < result[j].Host
	})
	return result, warnings, nil
}

func discoverDPUHealthGitLabTargets(tag string) ([]dpuHealthRunnerTarget, error) {
	cfg, err := config.LoadConfig(configFile)
	if err != nil {
		return nil, fmt.Errorf("loading config: %w", err)
	}
	token := firstNonEmptyEnv("GITLAB_TOKEN", "GITLAB_API_TOKEN", "GITLAB_RUNNER_GROUP_MAINTAINER_TOKEN")
	if token == "" {
		return nil, fmt.Errorf("GITLAB_TOKEN, GITLAB_API_TOKEN, or GITLAB_RUNNER_GROUP_MAINTAINER_TOKEN is required")
	}
	client := gitlab.NewClient(cfg.GitLab.Endpoint, token, cfg.GitLab.ProjectID)
	runners, err := client.ListRunners(tag, "")
	if err != nil {
		return nil, fmt.Errorf("listing runners: %w", err)
	}

	targets := []dpuHealthRunnerTarget{}
	for _, runner := range runners {
		if !runner.Active {
			continue
		}
		host := connectableRunnerHost(runner)
		if host == "" {
			continue
		}
		targets = append(targets, dpuHealthRunnerTarget{
			ID:          runner.ID,
			Description: runner.Description,
			Host:        host,
			Active:      runner.Active,
			Online:      runner.Online,
			Status:      runner.Status,
		})
	}
	return targets, nil
}

func readDPUHealthNICHostsFile(path string) ([]dpuHealthRunnerTarget, error) {
	path = os.ExpandEnv(path)
	file, err := os.Open(filepath.Clean(path))
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = file.Close()
	}()

	targets := []dpuHealthRunnerTarget{}
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		targets = append(targets, dpuHealthRunnerTarget{Host: line, Active: true})
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("reading NIC Cloud hosts file: %w", err)
	}
	return targets, nil
}

func connectableRunnerHost(runner gitlab.Runner) string {
	for _, candidate := range []string{runner.IPAddress, runner.Name, runner.Description} {
		if host := extractConnectableHost(candidate); host != "" {
			return host
		}
	}
	return ""
}

func extractConnectableHost(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if ip := net.ParseIP(value); ip != nil && ip.To4() != nil {
		return value
	}
	if match := ipv4FromRunnerDescription(value); match != "" {
		return match
	}
	if !strings.ContainsAny(value, " \t") {
		return value
	}
	return ""
}

// reIPv4FromRunnerDescription matches a trailing IPv4 address in a runner
// description (optionally after a " - " separator). Compiled once at package
// load so it is not rebuilt for every runner description checked.
var reIPv4FromRunnerDescription = regexp.MustCompile(`(?:^|\s+-\s+)([0-9]{1,3}(?:\.[0-9]{1,3}){3})\s*$`)

func ipv4FromRunnerDescription(value string) string {
	match := reIPv4FromRunnerDescription.FindStringSubmatch(value)
	if len(match) != 2 {
		return ""
	}
	if ip := net.ParseIP(match[1]); ip == nil || ip.To4() == nil {
		return ""
	}
	return match[1]
}

func dpuHealthPhysicalFQDN(host string) string {
	if strings.Contains(host, ".") {
		return host
	}
	if strings.HasPrefix(host, "cloud-dev-ci") {
		return host + ".mtl.labs.mlnx"
	}
	return host + ".lab.mtl.com"
}

func (realSSHExecutor) Run(ctx context.Context, target, user, password, script string, connectTimeout, commandTimeout time.Duration) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, commandTimeout)
	defer cancel()

	args := []string{
		"-e",
		"ssh",
		"-o", "StrictHostKeyChecking=no",
		"-o", "UserKnownHostsFile=/dev/null",
		"-o", "LogLevel=ERROR",
		"-o", fmt.Sprintf("ConnectTimeout=%d", int(connectTimeout.Round(time.Second).Seconds())),
		fmt.Sprintf("%s@%s", user, target),
		"bash", "-s",
	}
	cmd := exec.CommandContext(ctx, "sshpass", args...)
	// Deliver the password to the remote shell through its environment rather
	// than writing it into the script body. The first line reads one line from
	// stdin into SSHPASS and exports it; the line immediately after carries the
	// value and is consumed by `read`, so it is never executed and never appears
	// as literal text in the script (keeping it out of `set -x` traces and any
	// tooling that captures the script body). Nested sshpass calls in the script
	// then authenticate with `sshpass -e`, reading SSHPASS from the environment —
	// the same pattern used for the outer hop's SSHPASS below.
	remoteStdin := "IFS= read -r SSHPASS; export SSHPASS\n" + password + "\n" + script
	cmd.Stdin = strings.NewReader(remoteStdin)
	cmd.Env = append(os.Environ(), "SSHPASS="+password)
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	err := cmd.Run()
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return output.String(), fmt.Errorf("probe timed out after %s", commandTimeout)
	}
	if err != nil {
		return output.String(), fmt.Errorf("%w: %s", err, strings.TrimSpace(output.String()))
	}
	return output.String(), nil
}

func physicalDPUHealthScript(workerUser, masterHost string, workers []string, connectTimeout time.Duration, requireUp bool) string {
	// The worker password is not embedded here; realSSHExecutor.Run exports it as
	// SSHPASS in the remote environment, and ssh_worker uses `sshpass -e`.
	return fmt.Sprintf(`set -u
WORKER_USER=%s
MASTER_HOST=%s
WORKERS=%s
CONNECT_TIMEOUT=%d
REQUIRE_UP=%s
%s
for worker in ${WORKERS}; do
	probe_target "${WORKER_USER}@${worker}" "${worker}"
done
`, shellQuote(workerUser), shellQuote(masterHost), shellQuote(strings.Join(workers, " ")), int(connectTimeout.Round(time.Second).Seconds()), shellBool(requireUp), dpuHealthRemoteProbeLibrary())
}

func nicCloudDPUHealthScript(workerUser, setupInfo, masterHost string, connectTimeout time.Duration) string {
	// The worker password is not embedded here; realSSHExecutor.Run exports it as
	// SSHPASS in the remote environment, and ssh_worker uses `sshpass -e`.
	return fmt.Sprintf(`set -u
WORKER_USER=%s
SETUP_INFO=%s
MASTER_HOST=%s
CONNECT_TIMEOUT=%d
REQUIRE_UP=false
%s
if [[ ! -f "${SETUP_INFO}" ]]; then
	emit "__setup_info__" "-" "missing" "-" "${SETUP_INFO}" "-"
	exit 0
fi
set -a
# shellcheck source=/dev/null
source "${SETUP_INFO}"
set +a
found=0
if [[ -n "${CLOUD_PLAYER_1_IP:-}" ]]; then
	found=1
	probe_target "${WORKER_USER}@${CLOUD_PLAYER_1_IP}" "player1 (${CLOUD_PLAYER_1_IP})"
fi
if [[ -n "${CLOUD_PLAYER_2_IP:-}" ]]; then
	found=1
	probe_target "${WORKER_USER}@${CLOUD_PLAYER_2_IP}" "player2 (${CLOUD_PLAYER_2_IP})"
fi
if [[ "${found}" -eq 0 ]]; then
	emit "__workers__" "-" "no_cloud_ips" "-" "-" "-"
fi
`, shellQuote(workerUser), shellQuote(setupInfo), shellQuote(masterHost), int(connectTimeout.Round(time.Second).Seconds()), dpuHealthRemoteProbeLibrary())
}

func dpuHealthRemoteProbeLibrary() string {
	return `emit() { printf 'RESULT\t%s\t%s\t%s\t%s\t%s\t%s\n' "$1" "$2" "$3" "$4" "$5" "$6"; }
resolve() { getent ahosts "$1" 2>/dev/null | awk '{print $1; exit}'; }
ssh_worker() {
	local target="$1" command="$2"
	sshpass -e ssh -n -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -o LogLevel=ERROR -o ConnectTimeout="${CONNECT_TIMEOUT}" "${target}" "${command}"
}
kubectl_dpu_rows() {
	if [[ "${DPU_ROWS_LOADED:-false}" == "true" ]]; then
		printf '%s\n' "${DPU_ROWS_CACHE:-}"
		return
	fi
	DPU_ROWS_LOADED=true
	local jsonpath='{range .items[*]}{.metadata.namespace}{"\t"}{.metadata.name}{"\t"}{.spec.dpuNodeName}{"\t"}{.status.phase}{"\n"}{end}'
	DPU_ROWS_CACHE="$(kubectl get dpus -A -o "jsonpath=${jsonpath}" 2>/dev/null || true)"
	if [[ -z "${DPU_ROWS_CACHE}" && -n "${MASTER_HOST:-}" ]]; then
		DPU_ROWS_CACHE="$(ssh_worker "${WORKER_USER}@${MASTER_HOST}" "sudo KUBECONFIG=/etc/kubernetes/admin.conf kubectl get dpus -A -o jsonpath='${jsonpath}'" 2>/dev/null || true)"
	fi
	printf '%s\n' "${DPU_ROWS_CACHE:-}"
}
dpu_phase_summary() {
	local worker_host="$1" label="$2" rows line ns name node phase item found all
	rows="$(kubectl_dpu_rows)"
	if [[ -z "${rows// }" ]]; then
		echo "kubectl:unavailable"
		return
	fi
	found=""
	all=""
	while IFS=$'\t' read -r ns name node phase; do
		[[ -z "${name}" ]] && continue
		item="${name}:${phase:-unknown}"
		all+="${item},"
		if [[ "${node}" == "${worker_host}" || "${node}" == "${label}" || "${name}" == *"${worker_host}"* ]]; then
			found+="${item},"
		fi
	done <<< "${rows}"
	found="${found%,}"
	all="${all%,}"
	if [[ -n "${found}" ]]; then
		echo "${found}"
	elif [[ -n "${all}" ]]; then
		echo "all=${all}"
	else
		echo "kubectl:no_dpus"
	fi
}
pf_detail() {
	local target="$1" pci="$2" pf="$3" ifaces iface state detail ok
	ifaces="$(ssh_worker "${target}" "ls /sys/bus/pci/devices/${pci}.${pf}/net/ 2>/dev/null" 2>/dev/null | tr '\n' ' ' || true)"
	if [[ -z "${ifaces// }" ]]; then
		echo "pf${pf}=missing:bad"
		return
	fi
	ok=1
	detail="pf${pf}="
	for iface in ${ifaces}; do
		state="$(ssh_worker "${target}" "cat /sys/class/net/${iface}/operstate 2>/dev/null" 2>/dev/null || true)"
		state="${state//$'\r'/}"
		state="${state//$'\n'/}"
		if [[ "${REQUIRE_UP}" == "true" && "${state}" != "up" ]]; then
			ok=0
		fi
		detail+="${iface}:${state:-unknown},"
	done
	detail="${detail%,}"
	if [[ "${ok}" -eq 1 ]]; then
		echo "${detail}:ok"
	else
		echo "${detail}:bad"
	fi
}
probe_target() {
	local target="$1" label="$2" host ip dpus pci pf0 pf1 bad detail phase
	host="${target#*@}"
	ip="$(resolve "${host}")"
	[[ -z "${ip}" ]] && ip="-"
	if ! ssh_worker "${target}" "true" 2>/dev/null; then
		emit "${label}" "${ip}" "unreachable" "-" "-" "-"
		return
	fi
	dpus="$(ssh_worker "${target}" "lspci -D 2>/dev/null | awk '/BlueField/ {print \$1}' | cut -d. -f1 | sort -u" 2>/dev/null || true)"
	[[ -z "${dpus}" ]] && { emit "${label}" "${ip}" "reachable" "no_bluefield" "-" "$(dpu_phase_summary "${host}" "${label}")"; return; }
	bad=0
	detail=""
	while read -r pci; do
		[[ -z "${pci}" ]] && continue
		pf0="$(pf_detail "${target}" "${pci}" "0")"
		pf1="$(pf_detail "${target}" "${pci}" "1")"
		[[ "${pf0}" == *:bad || "${pf1}" == *:bad ]] && bad=1
		detail+="${pci}:${pf0%:*};${pf1%:*};"
	done <<< "${dpus}"
	detail="${detail%;}"
	if [[ "${bad}" -eq 0 ]]; then
		emit "${label}" "${ip}" "reachable" "ifaces_ok" "${detail}" "-"
	else
		phase="$(dpu_phase_summary "${host}" "${label}")"
		emit "${label}" "${ip}" "reachable" "ifaces_bad" "${detail}" "${phase}"
	fi
}`
}

func parseDPUHealthResults(kind, setup, output string) []dpuHealthResult {
	results := []dpuHealthResult{}
	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "RESULT\t") {
			continue
		}
		fields := strings.SplitN(line, "\t", 7)
		if len(fields) < 6 {
			continue
		}
		phase := "-"
		if len(fields) == 7 && strings.TrimSpace(fields[6]) != "" {
			phase = summarizeDPUPhases(fields[6])
		}
		results = append(results, dpuHealthResult{
			Kind:   kind,
			Setup:  setup,
			Worker: fields[1],
			IP:     fields[2],
			Reach:  fields[3],
			DPU:    fields[4],
			Detail: fields[5],
			Phase:  phase,
		})
	}
	return results
}

func summarizeDPUPhases(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || value == "-" || strings.HasPrefix(value, "kubectl:") {
		return value
	}
	value = strings.TrimPrefix(value, "all=")
	seen := map[string]bool{}
	phases := []string{}
	for _, item := range strings.Split(value, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		phase := item
		if index := strings.LastIndex(item, ":"); index >= 0 && index < len(item)-1 {
			phase = item[index+1:]
		}
		phase = strings.TrimSpace(phase)
		if phase == "" || seen[phase] {
			continue
		}
		seen[phase] = true
		phases = append(phases, phase)
	}
	if len(phases) == 0 {
		return value
	}
	return strings.Join(phases, " / ")
}

func noDPUHealthResults(kind, setup, detail string) []dpuHealthResult {
	return []dpuHealthResult{
		{
			Kind:   kind,
			Setup:  setup,
			Worker: "-",
			IP:     "-",
			Reach:  "no_results",
			DPU:    "-",
			Phase:  "-",
			Detail: detail,
		},
	}
}

func printDPUHealthProbeResults(w io.Writer, results []dpuHealthResult, color bool) {
	for _, result := range results {
		fmt.Fprintf(w, "    %s %s %s %s %s %s\n",
			dpuHealthColoredStatus(result, color, 5),
			padRight(result.Worker, 24),
			padRight(result.IP, 14),
			dpuHealthColoredDPU(result, color, 12),
			dpuHealthColoredPhase(result, color, 28),
			dpuHealthColoredDetail(result, color),
		)
	}
}

func printDPUHealthSummary(w io.Writer, results []dpuHealthResult, warnings []string, color bool) {
	if len(warnings) > 0 {
		fmt.Fprintf(w, "\n%s\n", colorize(color, ansiBoldYellow, "Warnings"))
		for _, warning := range warnings {
			fmt.Fprintf(w, "  - %s\n", colorize(color, ansiYellow, warning))
		}
	}

	fmt.Fprintf(w, "\n%s\n", colorize(color, ansiBold, "Summary"))
	fmt.Fprintf(w, "%s %s %s %s %s %s %s %s %s\n",
		padRight("STATUS", 7),
		padRight("KIND", 9),
		padRight("SETUP", 16),
		padRight("WORKER", 24),
		padRight("IP", 14),
		padRight("REACH", 14),
		padRight("DPU_NET", 12),
		padRight("DPU_PHASE", 28),
		"DETAIL",
	)
	fmt.Fprintf(w, "%s %s %s %s %s %s %s %s %s\n",
		padRight("------", 7),
		padRight("----", 9),
		padRight("-----", 16),
		padRight("------", 24),
		padRight("--", 14),
		padRight("-----", 14),
		padRight("-------", 12),
		padRight("---------", 28),
		"------",
	)
	for _, result := range results {
		fmt.Fprintf(w, "%s %s %s %s %s %s %s %s %s\n",
			dpuHealthColoredStatus(result, color, 7),
			padRight(result.Kind, 9),
			padRight(result.Setup, 16),
			padRight(result.Worker, 24),
			padRight(result.IP, 14),
			dpuHealthColoredReach(result, color, 14),
			dpuHealthColoredDPU(result, color, 12),
			dpuHealthColoredPhase(result, color, 28),
			dpuHealthColoredDetail(result, color),
		)
	}
	total := len(results)
	issues := countDPUHealthIssues(results)
	passingText := colorize(color, ansiBoldGreen, fmt.Sprintf("%d", total-issues))
	issuesColor := ansiBoldGreen
	if issues > 0 {
		issuesColor = ansiBoldRed
	}
	issuesText := colorize(color, issuesColor, fmt.Sprintf("%d", issues))
	fmt.Fprintf(w, "\nTotal checks: %d, passing: %s, issues: %s\n", total, passingText, issuesText)
}

func (r dpuHealthResult) OK() bool {
	if r.Reach != "reachable" {
		return false
	}
	if r.DPU == "ifaces_ok" {
		return true
	}
	return r.DPU == "ifaces_bad" && r.HasDPUPhase()
}

func (r dpuHealthResult) HasDPUPhase() bool {
	phase := strings.TrimSpace(r.Phase)
	return phase != "" && phase != "-" && !strings.HasPrefix(phase, "kubectl:")
}

func (r dpuHealthResult) Status() string {
	if r.OK() {
		return "OK"
	}
	return "ISSUE"
}

func countDPUHealthIssues(results []dpuHealthResult) int {
	issues := 0
	for _, result := range results {
		if !result.OK() {
			issues++
		}
	}
	return issues
}

func dpuHealthColoredStatus(result dpuHealthResult, color bool, width int) string {
	colorCode := ansiBoldRed
	if result.OK() {
		colorCode = ansiBoldGreen
	}
	return colorize(color, colorCode, padRight(result.Status(), width))
}

func dpuHealthColoredReach(result dpuHealthResult, color bool, width int) string {
	colorCode := ansiBoldRed
	if result.Reach == "reachable" {
		colorCode = ansiGreen
	}
	return colorize(color, colorCode, padRight(result.Reach, width))
}

func dpuHealthColoredDPU(result dpuHealthResult, color bool, width int) string {
	colorCode := ansiBoldRed
	switch result.DPU {
	case "ifaces_ok":
		colorCode = ansiGreen
	case "ifaces_bad":
		if result.HasDPUPhase() {
			colorCode = ansiYellow
		}
	case "-":
		colorCode = ansiDim
	}
	return colorize(color, colorCode, padRight(result.DPU, width))
}

func dpuHealthColoredPhase(result dpuHealthResult, color bool, width int) string {
	phase := result.Phase
	if strings.TrimSpace(phase) == "" {
		phase = "-"
	}
	colorCode := ansiYellow
	if phase == "-" {
		colorCode = ansiDim
	} else if strings.Contains(phase, "kubectl:") {
		colorCode = ansiBoldRed
	} else if allDPUPhasesReady(phase) {
		colorCode = ansiGreen
	}
	return colorize(color, colorCode, padRight(phase, width))
}

func allDPUPhasesReady(value string) bool {
	value = summarizeDPUPhases(value)
	if value == "" || value == "-" {
		return false
	}
	parts := strings.Split(value, " / ")
	for _, part := range parts {
		if strings.TrimSpace(part) != "Ready" {
			return false
		}
	}
	return true
}

func dpuHealthColoredDetail(result dpuHealthResult, color bool) string {
	if result.Detail == "" || result.Detail == "-" {
		return colorize(color, ansiDim, result.Detail)
	}
	if result.OK() {
		return colorize(color, ansiDim, result.Detail)
	}
	return colorize(color, ansiYellow, result.Detail)
}

func colorize(enabled bool, colorCode, value string) string {
	if !enabled || value == "" {
		return value
	}
	return colorCode + value + ansiReset
}

func padRight(value string, width int) string {
	if len(value) >= width {
		return value
	}
	return value + strings.Repeat(" ", width-len(value))
}

func shellQuote(value string) string {
	if value == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}

func shellBool(value bool) string {
	if value {
		return "true"
	}
	return "false"
}

func envOrDefault(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func firstNonEmptyEnv(keys ...string) string {
	for _, key := range keys {
		if value := os.Getenv(key); value != "" {
			return value
		}
	}
	return ""
}

func uniqueNonEmpty(values []string) []string {
	seen := map[string]bool{}
	result := []string{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		result = append(result, value)
	}
	return result
}
