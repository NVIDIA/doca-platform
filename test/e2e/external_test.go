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

package e2e

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("External DPF tests", Labels{Domain.ExternalTest}, func() {
	Context("Performance - OVNK HBN based", Ordered, func() {
		const nlasticSharedDir = "/tmp/nlastic/shared"

		type nlasticScenario struct {
			id                     string
			name                   string
			labels                 Labels
			bandwidthThresholdGbit float64
		}

		type nlasticProtocolSuite struct {
			name      string
			protocol  string
			scenarios []nlasticScenario
		}

		tcpScenario := func(id string, name string, thresholdGbit float64) nlasticScenario {
			return nlasticScenario{
				id:                     id,
				name:                   name,
				labels:                 Labels{Domain.TCP, Domain.OVNKHBN, Domain.RequiresNodes, Domain.Performance},
				bandwidthThresholdGbit: thresholdGbit,
			}
		}

		udpScenario := func(id string, name string, thresholdGbit float64) nlasticScenario {
			return nlasticScenario{
				id:                     id,
				name:                   name,
				labels:                 Labels{Domain.UDP, Domain.OVNKHBN, Domain.RequiresNodes, Domain.Performance},
				bandwidthThresholdGbit: thresholdGbit,
			}
		}

		tcpScenarios := []nlasticScenario{
			tcpScenario("1", "iperf pod to pod same host", 15),
			tcpScenario("3", "iperf pod to pod diff host", 230),
			tcpScenario("6", "iperf pod to pod via ClusterIP same host", 20),
			tcpScenario("7", "iperf pod to pod via ClusterIP diff host", 230),
			//  TODO: Scenario 8 is skipped until Nlastic handles NodePort same-host mapping correctly.
			// tcpScenario("8", "iperf pod to pod via NodePort same host", 10),
			//  TODO: Scenario 9 is skipped until Nlastic handles NodePort diff-host mapping correctly.
			// tcpScenario{id: "9", name: "iperf pod to pod via NodePort diff host", },
		}

		udpScenarios := []nlasticScenario{
			udpScenario("1", "testpmd pod to pod same host", 4),
			udpScenario("3", "testpmd pod to pod diff host", 10),
			udpScenario("6", "testpmd pod to pod via ClusterIP same host", 4),
			udpScenario("7", "testpmd pod to pod via ClusterIP diff host", 10),
			//  TODO: Scenario 8 is skipped until Nlastic handles NodePort same-host mapping correctly.
			// udpScenario("8", "testpmd pod to pod via NodePort same host", 10),
			//  TODO: Scenario 8 is skipped until Nlastic handles NodePort same-host mapping correctly.
			// udpScenario("9", "testpmd pod to pod via NodePort diff host", 8),
		}

		BeforeAll(func() {
			By("Wait for OVNK HBN deployment to be ready")
			WaitForOVNKHBNDeploymentReady(Ctx, input)
			By("Syncing image pull secrets for Nlastic workload pods")
			syncNlasticImagePullSecrets()
			By("Setup Nlastic environment")
			runExternalTestScript("setup")
			By("Configuring Nlastic testpmd image")
			configureNlasticTestpmdImage(fmt.Sprintf("%s:%s", netutilsImage, tag))
		})

		for _, suite := range []nlasticProtocolSuite{
			{name: "TCP", protocol: "tcp", scenarios: tcpScenarios},
			{name: "UDP", protocol: "udp", scenarios: udpScenarios},
		} {
			Context(fmt.Sprintf("%s performance tests via Nlastic", suite.name), func() {
				for _, s := range suite.scenarios {
					It(fmt.Sprintf("executes %s scenario %s: %s", suite.name, s.id, s.name),
						s.labels, func() {
							defer collectNlasticResults(nlasticSharedDir, fmt.Sprintf("%s/%s", suite.protocol, s.id))
							switch suite.protocol {
							case "tcp":
								By(fmt.Sprintf("Configuring TCP bandwidth threshold to %.2f Gbit/sec", s.bandwidthThresholdGbit))
								configureNlasticTCPBandwidthThreshold(s.bandwidthThresholdGbit)
							case "udp":
								By(fmt.Sprintf("Configuring UDP bandwidth threshold to %.2f Gbit/sec", s.bandwidthThresholdGbit))
								configureNlasticUDPBandwidthThreshold(s.bandwidthThresholdGbit)
							}
							runExternalTestScript(suite.protocol, s.id)
						})
				}
			})
		}
	})
})

func syncNlasticImagePullSecrets() {
	const nlasticPodNamespace = "default"
	for _, secretName := range []string{dpfPullSecretName, "pull-secret-extra"} {
		CopySecretToNamespace(Ctx, input.Client, secretName, dpfOperatorSystemNamespace, nlasticPodNamespace, CleanupScope.Suite)
	}
}

func runExternalTestScript(args ...string) {
	cmd := exec.Command(externalTest, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err := cmd.Run()
	Expect(err).NotTo(HaveOccurred(), "Script failed with error: %v", err)
}

func configureNlasticTestpmdImage(image string) {
	nlasticDir := os.Getenv("NLASTIC_DIR")
	if nlasticDir == "" {
		nlasticDir = "/tmp/nlastic"
	}
	testpmdTemplate := filepath.Join(nlasticDir, "nlastic_configs", "pod-dpf-testpmd-template.yaml")
	content, err := os.ReadFile(testpmdTemplate)
	Expect(err).NotTo(HaveOccurred(), "Failed to read %s", testpmdTemplate)
	rendered := strings.ReplaceAll(string(content), "$NETUTILS_IMAGE:$TAG", image)
	Expect(os.WriteFile(testpmdTemplate, []byte(rendered), 0644)).To(Succeed())
}

func configureNlasticTCPBandwidthThreshold(thresholdGbit float64) {
	Expect(thresholdGbit).To(BeNumerically(">", 0), "TCP bandwidth threshold must be positive")

	configureNlasticConfigValue(
		"test-dpf-ovn-hbn.yaml",
		"TCP_BW_THRESHOLD_GBIT",
		strconv.FormatFloat(thresholdGbit, 'f', -1, 64),
	)
}

func configureNlasticUDPBandwidthThreshold(thresholdGbit float64) {
	Expect(thresholdGbit).To(BeNumerically(">", 0), "UDP bandwidth threshold must be positive")

	thresholdBps := thresholdGbit * 1_000_000_000
	configureNlasticConfigValue(
		"test-dpf-udp-ovn-hbn.yaml",
		"UDP_BW_THRESHOLD_BPS",
		strconv.FormatFloat(thresholdBps, 'f', 0, 64),
	)
}

func configureNlasticConfigValue(configFile string, key string, value string) {
	nlasticDir := os.Getenv("NLASTIC_DIR")
	if nlasticDir == "" {
		nlasticDir = "/tmp/nlastic"
	}
	configPath := filepath.Join(nlasticDir, "nlastic_configs", configFile)
	content, err := os.ReadFile(configPath)
	Expect(err).NotTo(HaveOccurred(), "Failed to read %s", configPath)

	lines := strings.Split(string(content), "\n")
	found := false
	for i, line := range lines {
		if strings.HasPrefix(line, "  "+key+":") {
			lines[i] = fmt.Sprintf("  %s: %s", key, value)
			found = true
			break
		}
	}
	Expect(found).To(BeTrue(), "Failed to find %s in %s", key, configPath)
	Expect(os.WriteFile(configPath, []byte(strings.Join(lines, "\n")), 0644)).To(Succeed())
}

func collectNlasticResults(sharedDir, subDir string) {
	if _, err := os.Stat(sharedDir); err != nil {
		return
	}
	dest := filepath.Join(ArtifactsDir, "nlastic", subDir)
	if err := os.MkdirAll(dest, 0755); err != nil {
		_, _ = fmt.Fprintf(GinkgoWriter, "Failed to create nlastic artifacts dir %s: %v\n", dest, err)
		return
	}
	if out, err := exec.Command("cp", "-r", sharedDir+"/.", dest).CombinedOutput(); err != nil {
		_, _ = fmt.Fprintf(GinkgoWriter, "Failed to copy nlastic results to %s: %v: %s\n", dest, err, out)
		return
	}
	if err := os.RemoveAll(sharedDir); err != nil {
		_, _ = fmt.Fprintf(GinkgoWriter, "Failed to clean up %s: %v\n", sharedDir, err)
	}
	_, _ = fmt.Fprintf(GinkgoWriter, "nlastic results saved to %s\n", dest)
}
