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

package packages

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/internal/provisioning/dpuagent/operations"

	"k8s.io/klog/v2"
)

const (
	conditionType = "PackagesInstalled"

	hostProxyAddress = "fe80::1%tmfifo_net0"
	hostProxyPort    = "11030"
	localProxyHost   = "127.0.0.1"
)

type stopFunc func()
type startRelayFunc func(context.Context) (string, stopFunc, error)

// InstallPackages reconciles DPUFlavor package specs on the DPU.
type InstallPackages struct {
	aptManager APTPackageManager
	startRelay startRelayFunc
}

func (i *InstallPackages) Name() string {
	return "Install Packages"
}

func (i *InstallPackages) ConditionType() string {
	return conditionType
}

func (i *InstallPackages) ShouldSkip(ctx *operations.Context) bool {
	return len(ctx.DPUFlavor.Spec.Packages) == 0
}

func (i *InstallPackages) ShouldUpdateStatusBeforeContinue(ctx *operations.Context) bool {
	return true
}

func (i *InstallPackages) Execute(execCtx context.Context, optCtx *operations.Context) error {
	if i.startRelay == nil {
		i.startRelay = startLocalProxyRelay
	}
	if i.aptManager == nil {
		i.aptManager = newAPTPackageManager()
	}

	if !optCtx.Options.ZeroTrustMode {
		proxyURL, stopRelay, err := i.startRelay(execCtx)
		if err != nil {
			return fmt.Errorf("failed to start APT proxy relay: %w", err)
		}
		defer stopRelay()
		i.aptManager.SetProxy(proxyURL)
	}

	for _, pkg := range optCtx.DPUFlavor.Spec.Packages {
		if err := i.reconcilePackage(pkg); err != nil {
			return err
		}
	}
	return nil
}

func (i *InstallPackages) reconcilePackage(pkg provisioningv1.PackageSpec) error {
	installedVersion, installed, err := i.aptManager.InstalledPackageVersion(pkg.Name)
	if err != nil {
		return fmt.Errorf("failed to check installed package %s: %w", pkg.Name, err)
	}
	if installed {
		satisfied, err := i.isVersionSatisfied(pkg, installedVersion)
		if err != nil {
			return fmt.Errorf("failed to compare installed package %s version: %w", pkg.Name, err)
		}
		if satisfied {
			klog.Infof("Package %s is already installed with a satisfying version %s", pkg.Name, installedVersion)
			return nil
		}
		klog.Infof("Package %s is installed with version %s but does not satisfy requested version", pkg.Name, installedVersion)
	}

	updateErr := i.aptManager.Update(pkg.RepoFileRef)
	if updateErr != nil {
		klog.Warningf("Failed to update apt metadata before resolving package %s: %v", pkg.Name, updateErr)
	}
	installTarget, err := i.resolveInstallTarget(pkg)
	if err != nil {
		if updateErr != nil {
			return fmt.Errorf("failed to resolve install target for package %s, err: %w. apt metadata update failed; check dpu-agent logs for details", pkg.Name, err)
		}
		return fmt.Errorf("failed to resolve install target for package %s: %w", pkg.Name, err)
	}
	if err := i.aptManager.Install(installTarget, pkg.RepoFileRef); err != nil {
		return fmt.Errorf("failed to install package %s: %w", pkg.Name, err)
	}
	if err := i.verifyPackageVersion(pkg); err != nil {
		return fmt.Errorf("failed to verify package %s: %w", pkg.Name, err)
	}
	return nil
}

func (i *InstallPackages) resolveInstallTarget(pkg provisioningv1.PackageSpec) (string, error) {
	if pkg.Version == nil {
		return pkg.Name, nil
	}
	policy := pkg.Version.MatchPolicy
	if policy == "" {
		policy = provisioningv1.PackageVersionMatchAtLeast
	}
	switch policy {
	case provisioningv1.PackageVersionMatchExact:
		return pkg.Name + "=" + pkg.Version.Value, nil
	case provisioningv1.PackageVersionMatchAtLeast:
		version, err := i.bestAtLeastVersion(pkg.Name, pkg.Version.Value, pkg.RepoFileRef)
		if err != nil {
			return "", err
		}
		return pkg.Name + "=" + version, nil
	default:
		return "", fmt.Errorf("unsupported package version match policy %q", policy)
	}
}

func (i *InstallPackages) bestAtLeastVersion(name string, minimumVersion string, repoFileRef string) (string, error) {
	versions, err := i.aptManager.AvailableVersions(name, repoFileRef)
	if err != nil {
		return "", err
	}
	best := ""
	for _, version := range versions {
		if !i.aptManager.VersionCompare(version, "ge", minimumVersion) {
			continue
		}
		if best == "" || i.aptManager.VersionCompare(version, "gt", best) {
			best = version
		}
	}
	if best == "" {
		return "", fmt.Errorf("no available version satisfies >= %s", minimumVersion)
	}
	return best, nil
}

func (i *InstallPackages) verifyPackageVersion(pkg provisioningv1.PackageSpec) error {
	installedVersion, installed, err := i.aptManager.InstalledPackageVersion(pkg.Name)
	if err != nil {
		return err
	}
	if !installed {
		return fmt.Errorf("package is not installed")
	}
	satisfied, err := i.isVersionSatisfied(pkg, installedVersion)
	if err != nil {
		return err
	}
	if !satisfied {
		return fmt.Errorf("installed version %s does not satisfy requested version", installedVersion)
	}
	return nil
}

func (i *InstallPackages) isVersionSatisfied(pkg provisioningv1.PackageSpec, installedVersion string) (bool, error) {
	if pkg.Version == nil {
		return true, nil
	}
	policy := pkg.Version.MatchPolicy
	if policy == "" {
		policy = provisioningv1.PackageVersionMatchAtLeast
	}

	switch policy {
	case provisioningv1.PackageVersionMatchExact:
		return i.aptManager.VersionCompare(installedVersion, "eq", pkg.Version.Value), nil
	case provisioningv1.PackageVersionMatchAtLeast:
		return i.aptManager.VersionCompare(installedVersion, "ge", pkg.Version.Value), nil
	default:
		return false, fmt.Errorf("unsupported package version match policy %q", policy)
	}
}

func startLocalProxyRelay(ctx context.Context) (string, stopFunc, error) {
	listener, err := net.Listen("tcp4", net.JoinHostPort(localProxyHost, "0"))
	if err != nil {
		return "", nil, err
	}
	proxyURL := "http://" + listener.Addr().String() + "/"

	relayCtx, cancel := context.WithCancel(ctx)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		acceptLocalConnections(relayCtx, listener)
	}()

	stop := func() {
		cancel()
		_ = listener.Close()
		wg.Wait()
	}
	return proxyURL, stop, nil
}

func acceptLocalConnections(ctx context.Context, listener net.Listener) {
	for {
		client, err := listener.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) || ctx.Err() != nil {
				return
			}
			klog.Warningf("Failed to accept APT proxy relay connection: %v", err)
			continue
		}
		go relayConnection(ctx, client)
	}
}

func relayConnection(ctx context.Context, client net.Conn) {
	defer client.Close() //nolint:errcheck

	dialer := net.Dialer{}
	upstream, err := dialer.DialContext(ctx, "tcp6", net.JoinHostPort(hostProxyAddress, hostProxyPort))
	if err != nil {
		klog.Warningf("Failed to connect APT proxy relay upstream: %v", err)
		return
	}
	defer upstream.Close() //nolint:errcheck

	var wg sync.WaitGroup
	wg.Add(2)
	go proxyCopy(&wg, upstream, client)
	go proxyCopy(&wg, client, upstream)
	wg.Wait()
}

func proxyCopy(wg *sync.WaitGroup, dst net.Conn, src net.Conn) {
	defer wg.Done()
	_, _ = io.Copy(dst, src)
	_ = dst.Close()
	_ = src.Close()
}
