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
	"net/url"
	"strings"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/cmd/dpuagent/opts"
	"github.com/nvidia/doca-platform/internal/provisioning/dpuagent/operations"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const (
	testProxyURL = "http://127.0.0.1:12345/"
)

var _ = Describe("Packages", func() {
	It("should skip if no packages are configured", func() {
		operation := &InstallPackages{}
		Expect(operation.ShouldSkip(&operations.Context{})).To(BeTrue())
	})

	It("should not skip if packages are configured", func() {
		operation := &InstallPackages{}
		Expect(operation.ShouldSkip(&operations.Context{
			DPUFlavor: provisioningv1.DPUFlavor{
				Spec: provisioningv1.DPUFlavorSpec{
					Packages: []provisioningv1.PackageSpec{{Name: "doca-extra"}},
				},
			},
		})).To(BeFalse())
	})

	It("should reconcile packages through the trusted-host proxy relay", func() {
		exactVersion := "1.2.3"
		apt := &fakeAPTManager{}
		var relayStarted, relayStopped bool
		operation := &InstallPackages{
			aptManager: apt,
			startRelay: func(ctx context.Context) (string, stopFunc, error) {
				relayStarted = true
				return testProxyURL, func() { relayStopped = true }, nil
			},
		}

		err := operation.Execute(ctx, &operations.Context{
			DPUFlavor: provisioningv1.DPUFlavor{
				Spec: provisioningv1.DPUFlavorSpec{
					Packages: []provisioningv1.PackageSpec{
						{
							Name: "doca-extra",
							Version: &provisioningv1.PackageVersionSpec{
								Value:       exactVersion,
								MatchPolicy: provisioningv1.PackageVersionMatchExact,
							},
							RepoFileRef: "/etc/apt/sources.list.d/doca.list",
						},
					},
				},
			},
		})

		Expect(err).NotTo(HaveOccurred())
		Expect(relayStarted).To(BeTrue())
		Expect(relayStopped).To(BeTrue())
		Expect(apt.calls).To(Equal([]aptCall{
			{method: "InstalledPackageVersion", name: "doca-extra"},
			{method: "Update", repoFileRef: "/etc/apt/sources.list.d/doca.list", proxyURL: testProxyURL},
			{method: "Install", installTarget: "doca-extra=" + exactVersion, repoFileRef: "/etc/apt/sources.list.d/doca.list", proxyURL: testProxyURL},
			{method: "InstalledPackageVersion", name: "doca-extra"},
			{method: "VersionCompare", version: exactVersion, op: "eq", requestedVersion: exactVersion},
		}))
	})

	It("should select the highest candidate that satisfies AtLeast", func() {
		minimumVersion := "2.0.0"
		bestCandidate := "2.1.0"
		apt := &fakeAPTManager{
			availableVersions: []string{"1.9.0", minimumVersion, bestCandidate},
		}
		operation := &InstallPackages{
			aptManager: apt,
			startRelay: func(ctx context.Context) (string, stopFunc, error) {
				return testProxyURL, func() {}, nil
			},
		}

		err := operation.Execute(ctx, &operations.Context{
			Options: opts.Options{ZeroTrustMode: true},
			DPUFlavor: provisioningv1.DPUFlavor{
				Spec: provisioningv1.DPUFlavorSpec{
					Packages: []provisioningv1.PackageSpec{
						{
							Name: "doca-extra",
							Version: &provisioningv1.PackageVersionSpec{
								Value: minimumVersion,
							},
						},
					},
				},
			},
		})

		Expect(err).NotTo(HaveOccurred())
		Expect(apt.calls).To(ContainElement(aptCall{method: "AvailableVersions", name: "doca-extra"}))
		Expect(apt.calls).To(ContainElement(aptCall{method: "Install", installTarget: "doca-extra=" + bestCandidate}))
	})

	It("should skip apt update and install when installed version satisfies the spec", func() {
		requestedVersion := "1.2.3"
		installedVersion := "1.2.4"
		apt := &fakeAPTManager{installedVersion: installedVersion}
		operation := &InstallPackages{
			aptManager: apt,
			startRelay: func(ctx context.Context) (string, stopFunc, error) {
				return testProxyURL, func() {}, nil
			},
		}

		err := operation.Execute(ctx, &operations.Context{
			Options: opts.Options{ZeroTrustMode: true},
			DPUFlavor: provisioningv1.DPUFlavor{
				Spec: provisioningv1.DPUFlavorSpec{
					Packages: []provisioningv1.PackageSpec{
						{
							Name: "doca-extra",
							Version: &provisioningv1.PackageVersionSpec{
								Value: requestedVersion,
							},
						},
					},
				},
			},
		})

		Expect(err).NotTo(HaveOccurred())
		Expect(apt.calls).To(Equal([]aptCall{
			{method: "InstalledPackageVersion", name: "doca-extra"},
			{method: "VersionCompare", version: installedVersion, op: "ge", requestedVersion: requestedVersion},
		}))
	})

	It("should use the current relay proxy URL on each execute retry", func() {
		requestedVersion := "4.5.6"
		firstProxyURL := "http://127.0.0.1:10001/"
		secondProxyURL := "http://127.0.0.1:10002/"
		relayURLs := []string{firstProxyURL, secondProxyURL}
		apt := &fakeAPTManager{}
		operation := &InstallPackages{
			aptManager: apt,
			startRelay: func(ctx context.Context) (string, stopFunc, error) {
				proxyURL := relayURLs[0]
				relayURLs = relayURLs[1:]
				return proxyURL, func() {}, nil
			},
		}
		optCtx := &operations.Context{
			DPUFlavor: provisioningv1.DPUFlavor{
				Spec: provisioningv1.DPUFlavorSpec{
					Packages: []provisioningv1.PackageSpec{
						{
							Name: "doca-extra",
							Version: &provisioningv1.PackageVersionSpec{
								Value:       requestedVersion,
								MatchPolicy: provisioningv1.PackageVersionMatchExact,
							},
						},
					},
				},
			},
		}

		Expect(operation.Execute(ctx, optCtx)).To(Succeed())
		Expect(apt.calls).To(ContainElements(
			aptCall{method: "Update", proxyURL: firstProxyURL},
			aptCall{method: "Install", installTarget: "doca-extra=" + requestedVersion, proxyURL: firstProxyURL},
		))

		apt.calls = nil
		apt.installedVersion = ""
		Expect(operation.Execute(ctx, optCtx)).To(Succeed())
		Expect(apt.calls).To(ContainElements(
			aptCall{method: "Update", proxyURL: secondProxyURL},
			aptCall{method: "Install", installTarget: "doca-extra=" + requestedVersion, proxyURL: secondProxyURL},
		))
		Expect(apt.proxyURLs).To(Equal([]string{firstProxyURL, secondProxyURL}))
	})

	It("should fail before install if no candidate satisfies AtLeast", func() {
		minimumVersion := "3.0.0"
		apt := &fakeAPTManager{availableVersions: []string{"1.9.0"}}
		operation := &InstallPackages{
			aptManager: apt,
			startRelay: func(ctx context.Context) (string, stopFunc, error) {
				return testProxyURL, func() {}, nil
			},
		}

		err := operation.Execute(ctx, &operations.Context{
			Options: opts.Options{ZeroTrustMode: true},
			DPUFlavor: provisioningv1.DPUFlavor{
				Spec: provisioningv1.DPUFlavorSpec{
					Packages: []provisioningv1.PackageSpec{
						{
							Name: "doca-extra",
							Version: &provisioningv1.PackageVersionSpec{
								Value: minimumVersion,
							},
						},
					},
				},
			},
		})

		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring(fmt.Sprintf("no available version satisfies >= %s", minimumVersion)))
		Expect(apt.calls).NotTo(ContainElement(aptCall{method: "Install"}))
	})

	It("should continue when apt update fails but a satisfying candidate is available", func() {
		minimumVersion := "2.2.0"
		bestCandidate := "2.3.0"
		apt := &fakeAPTManager{
			updateErr:         errors.New("some indexes failed"),
			availableVersions: []string{"2.1.0", minimumVersion, bestCandidate},
		}
		operation := &InstallPackages{
			aptManager: apt,
			startRelay: func(ctx context.Context) (string, stopFunc, error) {
				return testProxyURL, func() {}, nil
			},
		}

		err := operation.Execute(ctx, &operations.Context{
			Options: opts.Options{ZeroTrustMode: true},
			DPUFlavor: provisioningv1.DPUFlavor{
				Spec: provisioningv1.DPUFlavorSpec{
					Packages: []provisioningv1.PackageSpec{
						{
							Name: "doca-extra",
							Version: &provisioningv1.PackageVersionSpec{
								Value: minimumVersion,
							},
						},
					},
				},
			},
		})

		Expect(err).NotTo(HaveOccurred())
		Expect(apt.calls).To(ContainElement(aptCall{method: "Update"}))
		Expect(apt.calls).To(ContainElement(aptCall{method: "Install", installTarget: "doca-extra=" + bestCandidate}))
	})

	It("should return an error if the proxy relay fails to start", func() {
		operation := &InstallPackages{
			startRelay: func(ctx context.Context) (string, stopFunc, error) {
				return "", nil, errors.New("relay failure")
			},
		}

		err := operation.Execute(ctx, &operations.Context{
			DPUFlavor: provisioningv1.DPUFlavor{
				Spec: provisioningv1.DPUFlavorSpec{
					Packages: []provisioningv1.PackageSpec{{Name: "doca-extra"}},
				},
			},
		})

		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("failed to start APT proxy relay"))
	})

	It("should start the local proxy relay on an ephemeral loopback port", func() {
		proxyURL, stop, err := startLocalProxyRelay(ctx)
		Expect(err).NotTo(HaveOccurred())
		defer stop()

		parsed, err := url.Parse(proxyURL)
		Expect(err).NotTo(HaveOccurred())
		Expect(parsed.Scheme).To(Equal("http"))
		Expect(parsed.Hostname()).To(Equal(localProxyHost))
		Expect(parsed.Port()).NotTo(BeEmpty())
		Expect(parsed.Port()).NotTo(Equal("0"))
	})
})

type aptCall struct {
	method           string
	name             string
	proxyURL         string
	version          string
	op               string
	requestedVersion string
	repoFileRef      string
	installTarget    string
}

type fakeAPTManager struct {
	calls             []aptCall
	proxyURLs         []string
	currentProxyURL   string
	installedVersion  string
	availableVersions []string
	updateErr         error
}

func (f *fakeAPTManager) SetProxy(proxyURL string) {
	f.currentProxyURL = proxyURL
	f.proxyURLs = append(f.proxyURLs, proxyURL)
}

func (f *fakeAPTManager) InstalledPackageVersion(name string) (string, bool, error) {
	f.calls = append(f.calls, aptCall{method: "InstalledPackageVersion", name: name})
	if f.installedVersion == "" {
		return "", false, nil
	}
	return f.installedVersion, true, nil
}

func (f *fakeAPTManager) Update(repoFileRef string) error {
	f.calls = append(f.calls, aptCall{method: "Update", repoFileRef: repoFileRef, proxyURL: f.currentProxyURL})
	return f.updateErr
}

func (f *fakeAPTManager) Install(installTarget string, repoFileRef string) error {
	f.calls = append(f.calls, aptCall{method: "Install", installTarget: installTarget, repoFileRef: repoFileRef, proxyURL: f.currentProxyURL})
	if name, version, ok := strings.Cut(installTarget, "="); ok && name == "doca-extra" {
		f.installedVersion = version
	}
	return nil
}

func (f *fakeAPTManager) AvailableVersions(name string, repoFileRef string) ([]string, error) {
	f.calls = append(f.calls, aptCall{method: "AvailableVersions", name: name, repoFileRef: repoFileRef})
	return f.availableVersions, nil
}

func (f *fakeAPTManager) VersionCompare(version string, op string, requestedVersion string) bool {
	f.calls = append(f.calls, aptCall{method: "VersionCompare", version: version, op: op, requestedVersion: requestedVersion})
	// Test-only simplification. The production APT manager delegates comparison
	// to dpkg --compare-versions, which implements Debian version semantics.
	cmp := strings.Compare(version, requestedVersion)
	switch op {
	case "eq":
		return cmp == 0
	case "ge":
		return cmp >= 0
	case "gt":
		return cmp > 0
	}
	return false
}
