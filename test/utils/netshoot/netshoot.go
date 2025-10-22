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

package netshoot

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	utilrand "k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	// throughputThreshold is the minimum throughput required for the test to pass in Gbit/sec (10 Gbit/sec)
	throughputThreshold = 10e9
)

// TestPodConfig represents the configuration for a test pod
type TestPodConfig struct {
	Name          string
	Namespace     string
	NADName       string
	InterfaceName string
	IP            string
	DST           string
	GW            string
	NodeName      string
	VFIndex       int
	CIDR          string
	Labels        map[string]string
	CommandArgs   []string
}

// IperfResult is used to parse the traffic test result of an iperf3 command
type IperfResult struct {
	Start struct {
		Connected []struct {
			LocalHost  string `json:"local_host"`
			RemoteHost string `json:"remote_host"`
		} `json:"connected"`
	} `json:"start"`
	Intervals []struct {
		Sum struct {
			BitsPerSecond float64 `json:"bits_per_second"`
		} `json:"sum"`
	} `json:"intervals"`
	End struct {
		SumSent struct {
			BitsPerSecond float64 `json:"bits_per_second"`
		} `json:"sum_sent"`
	} `json:"end"`
}

// IBWriteBWResult is used to parse the traffic test result of an ib_write_bw command
// Example output:
/*
{
  "test_info": {
    "test": "RDMA_Write_BW_Test",
    "Dual_port": "OFF",
    "Device": "mlx5_11",
    "Number_of_qps": 1,
    "Transport_type": "IB",
    "Connection_type": "RC",
    "Using_SRQ": "OFF",
    "PCIe_relax_order": "ON",
    "ibv_wr_API": "ON",
    "TX_depth": 128,
    "CQ_Moderation": 1,
    "Mtu": 1024,
    "Link_type": "Ethernet",
    "GID_index": 1,
    "Max_inline_data": 0,
    "rdma_cm_QPs": "OFF",
    "Use_ROCm_memory": "OFF",
    "Data_ex_method": "Ethernet"
  },
  "results": {
    "MsgSize": 65536,
    "n_iterations": 5000,
    "BW_peak": 5293.52,
    "BW_average": 2229.27,
    "MsgRate": 0.035668
  }
}
*/
type IBWriteBWResult struct {
	TestInfo *struct {
		Device *string `json:"Device,omitempty"`
	} `json:"test_info,omitempty"`
	Results *struct {
		BWPeak    *float32 `json:"BW_peak,omitempty"`
		BWAverage *float32 `json:"BW_average,omitempty"`
	} `json:"results,omitempty"`
}

const (
	networkAnnotationKey = "k8s.v1.cni.cncf.io/networks"
	netshootImage        = "mirror.gcr.io/nicolaka/netshoot:v0.13"
	ExecTimeout          = 2 * time.Minute
)

var (
	ErrCreateExecutor = errors.New("failed to create executor")
	ErrExecFailed     = errors.New("exec failed")
)

// ErrorParserFunc defines a function type for parsing command output errors.
type ErrorParserFunc func(stdout, stderr string) string

// DefaultErrorParser is the default error parsing function that truncates output.
func DefaultErrorParser(stdout, stderr string) string {
	errorOutput := ""
	if stderr != "" {
		errorOutput += fmt.Sprintf("Stderr: %s\n", stderr)
	}

	if stdout != "" {
		// Truncate output to 200 characters to avoid overwhelming the log
		output := stdout
		if len(stdout) > 200 {
			output = stdout[:200] + "..."
		}
		errorOutput += fmt.Sprintf("Output: %s\n", output)
	}
	return errorOutput
}

// IperfErrorParser is an error parsing function that extracts errors from Iperf3 JSON output.
func IperfErrorParser(stdout, stderr string) string {
	errorOutput := ""
	if stderr != "" {
		errorOutput += fmt.Sprintf("Stderr: %s\n", stderr)
	}

	if stdout != "" {
		// Try to extract error from iperf3 JSON output
		var jsonData map[string]interface{}
		err := json.Unmarshal([]byte(stdout), &jsonData)
		if err == nil {
			errorMsg, exists := jsonData["error"]
			if exists {
				errorStr, ok := errorMsg.(string)
				if ok {
					errorOutput += fmt.Sprintf("Error: %s\n", errorStr)
				}
			}
		}
	}

	return errorOutput
}

func CreateAndWaitForPods(ctx context.Context, client client.Client, configs []*TestPodConfig) {
	CreatePods(ctx, client, configs)
	WaitForPodsReady(ctx, client, configs, 10*time.Minute)
}

func CreatePods(ctx context.Context, client client.Client, configs []*TestPodConfig) {
	for _, podConfig := range configs {
		createNetshootPod(ctx, client, *podConfig)
	}
}

// WaitForPodsReady waits for multiple pods to be running
func WaitForPodsReady(ctx context.Context, client client.Client, configs []*TestPodConfig, timeout time.Duration) {
	Eventually(func(g Gomega) {
		for _, podConfig := range configs {
			g.Expect(isPodRunning(ctx, g, client, podConfig.Namespace, podConfig.Name)).To(BeTrue())
		}
	}, timeout).Should(Succeed())
}

func CreateNadsFromConfig(ctx context.Context, client client.Client, configs []*TestPodConfig) {
	for _, podConfig := range configs {
		addIPCIDR := func(ip string) string {
			return fmt.Sprintf("%s/%s", ip, podConfig.CIDR)
		}

		podConfig.NADName = createNetworkAttachmentDefinition(ctx, client, podConfig.Namespace, podConfig.Name,
			podConfig.VFIndex, addIPCIDR(podConfig.IP), addIPCIDR(podConfig.DST), podConfig.GW)
	}
}

func RunTrafficTest(restClient *rest.RESTClient, restConfig *rest.Config, hostNamespace string, podName1, podName2, pod2IP string) {
	startIperf3Server(restClient, restConfig, hostNamespace, podName2)
	defer stopIperf3Server(restClient, restConfig, hostNamespace, podName2)

	netshootOutput := runIperf3Client(restClient, restConfig, hostNamespace, podName1, pod2IP)
	analyzeIperfResults(netshootOutput, false)

	reverseNetshootOutput := runIperf3ClientReverse(restClient, restConfig, hostNamespace, podName1, pod2IP)
	analyzeIperfResults(reverseNetshootOutput, true)
}

func RunRDMATrafficTest(restClient *rest.RESTClient, restConfig *rest.Config, hostNamespace string, podName1, podName2, pod2IP string) {
	startRDMAServer(restClient, restConfig, hostNamespace, podName2)
	defer stopRDMAServer(restClient, restConfig, hostNamespace, podName2)

	output := runRDMAClient(restClient, restConfig, hostNamespace, podName1, pod2IP)
	analyzeIBWriteBWResult(output)
}

func createNetshootPod(ctx context.Context, testClient client.Client, config TestPodConfig) {
	// Add tail -F /dev/null to the command args to block the pod from terminating
	additionalArgs := []string{"tail -F /dev/null"}
	finalArgs := append(config.CommandArgs, additionalArgs...)
	script := strings.Join(finalArgs, "\n")

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      config.Name,
			Namespace: config.Namespace,
			Annotations: map[string]string{
				networkAnnotationKey: config.NADName,
			},
			Labels: config.Labels,
		},
		Spec: corev1.PodSpec{
			NodeName: config.NodeName,
			Containers: []corev1.Container{
				{
					Name:    "netshoot",
					Image:   netshootImage,
					Command: []string{"/bin/sh", "-c"},
					Args:    []string{script},
					SecurityContext: &corev1.SecurityContext{
						Capabilities: &corev1.Capabilities{
							Add: []corev1.Capability{"NET_ADMIN"},
						},
					},
				},
			},
		},
	}
	Expect(client.IgnoreAlreadyExists(testClient.Create(ctx, pod))).To(Succeed())
}

func createNetworkAttachmentDefinition(ctx context.Context, testClient client.Client, namespace string, podName string, vfIndex int, ipAddress string, dst string, gw string) string {
	nadName := fmt.Sprintf("nad-%s", podName)
	name := fmt.Sprintf("hostpf0vf%d", vfIndex)
	hostDevice := fmt.Sprintf("enp8s0f0v%d", vfIndex)
	optionalRoutes := ""
	if dst != "" && gw != "" {
		optionalRoutes = fmt.Sprintf(`,"routes": [{"dst": "%s","gw": "%s"}]`, dst, gw)
	}

	nad := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "k8s.cni.cncf.io/v1",
			"kind":       "NetworkAttachmentDefinition",
			"metadata": map[string]interface{}{
				"name":      nadName,
				"namespace": namespace,
			},
			"spec": map[string]interface{}{
				"config": fmt.Sprintf(`{
					"cniVersion": "0.3.1",
					"name": "%s",
					"type": "host-device",
					"device": "%s",
					"ipam": {
						"type": "static",
						"addresses": [
							{
								"address": "%s"
							}
						]%s
					}
				}`, name, hostDevice, ipAddress, optionalRoutes),
			},
		},
	}

	Expect(testClient.Create(ctx, nad)).To(Succeed())
	return nadName
}

func isPodRunning(ctx context.Context, g Gomega, testClient client.Client, namespace, podName string) bool {
	pod := &corev1.Pod{}
	g.Expect(testClient.Get(ctx, client.ObjectKey{Namespace: namespace, Name: podName}, pod)).To(Succeed())
	if pod.Status.Phase != corev1.PodRunning {
		return false
	}
	for _, cs := range pod.Status.ContainerStatuses {
		if !cs.Ready {
			return false
		}
	}
	return true
}

func GetPodIP(ctx context.Context, testClient client.Client, namespace, podName string) string {
	pod := &corev1.Pod{}
	Expect(testClient.Get(ctx, client.ObjectKey{Namespace: namespace, Name: podName}, pod)).To(Succeed())
	return pod.Status.PodIP
}

func startIperf3Server(restClient *rest.RESTClient, restConfig *rest.Config, namespace string, podName string) {
	execCommandEventually(restClient, restConfig, namespace, podName, []string{"iperf3", "-s", "-D"}, 30*time.Second, DefaultErrorParser)
}

func stopIperf3Server(restClient *rest.RESTClient, restConfig *rest.Config, namespace string, podName string) {
	execCommandEventually(restClient, restConfig, namespace, podName, []string{"pkill", "iperf3"}, 30*time.Second, DefaultErrorParser)
}

func startRDMAServer(restClient *rest.RESTClient, restConfig *rest.Config, namespace string, podName string) {
	// This complex command is needed so that we can run ib_write_bw in background. ib_write_bw doesn't have a flag to
	// keep the process running after the first client has finished the test.
	execCommandEventually(restClient, restConfig, namespace, podName, []string{"bash", "-c", "nohup bash -c 'while true; do ib_write_bw; done' >/dev/null 2>&1 < /dev/null & exit"}, 30*time.Second, DefaultErrorParser)
}

func stopRDMAServer(restClient *rest.RESTClient, restConfig *rest.Config, namespace string, podName string) {
	execCommandEventually(restClient, restConfig, namespace, podName, []string{"pkill", "bash"}, 30*time.Second, DefaultErrorParser)
	execCommandEventually(restClient, restConfig, namespace, podName, []string{"pkill", "ib_write_bw"}, 30*time.Second, DefaultErrorParser)
}

func runRDMAClient(restClient *rest.RESTClient, restConfig *rest.Config, namespace string, podName string, serverIP string) string {
	fileName := fmt.Sprintf("ib_write_bw-result-%s", utilrand.String(6))
	execCommandEventually(restClient, restConfig, namespace, podName, []string{"ib_write_bw", serverIP, "--out_json", fmt.Sprintf("--out_json_file=%s", fileName)}, 30*time.Second, DefaultErrorParser)
	output := execCommandEventually(restClient, restConfig, namespace, podName, []string{"cat", fileName}, 30*time.Second, DefaultErrorParser)
	return output
}

func runIperf3Client(restClient *rest.RESTClient, restConfig *rest.Config, namespace string, podName string, iperf3ServerIP string) string {
	return execCommandEventually(restClient, restConfig, namespace, podName, []string{"iperf3", "-c", iperf3ServerIP, "-J"}, 500*time.Second, IperfErrorParser)
}

func runIperf3ClientReverse(restClient *rest.RESTClient, restConfig *rest.Config, namespace string, podName string, iperf3ServerIP string) string {
	return execCommandEventually(restClient, restConfig, namespace, podName, []string{"iperf3", "-c", iperf3ServerIP, "-R", "-J"}, 500*time.Second, IperfErrorParser)
}

func analyzeIperfResults(output string, reverse bool) {
	var result IperfResult
	err := json.Unmarshal([]byte(output), &result)
	Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("failed to parse iperf3 output: %v", err))

	Expect(result.Start.Connected).ShouldNot(BeEmpty(), "no connection information found. output: %s", output)
	localIP := result.Start.Connected[0].LocalHost
	remoteIP := result.Start.Connected[0].RemoteHost

	if reverse {
		fmt.Printf("Reverse traffic sent from %s to %s\n", remoteIP, localIP)
	} else {
		fmt.Printf("Traffic sent from %s to %s\n", localIP, remoteIP)
	}

	bitrate := result.End.SumSent.BitsPerSecond
	intervalCount := len(result.Intervals)
	fmt.Printf("Bitrate: %.2f Gbit/sec over %d intervals\n", bitrate/1e9, intervalCount)
	Expect(bitrate).Should(BeNumerically(">", throughputThreshold), "bitrate is below %d Gbit/sec", throughputThreshold/1e9)
}

func analyzeIBWriteBWResult(output string) {
	var result IBWriteBWResult
	err := json.Unmarshal([]byte(output), &result)
	Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("failed to parse ib_write_bw output: %v", err))

	Expect(result.Results).ToNot(BeNil(), "no results found. output: %s", output)
	Expect(result.Results.BWPeak).ToNot(BeNil())
	Expect(result.Results.BWAverage).ToNot(BeNil())
	fmt.Printf("Bandwidth peak %.2f MB/sec\t Bandwidth average %.2f MB/sec\n", *result.Results.BWPeak, *result.Results.BWAverage)
	Expect(*result.Results.BWPeak).Should(BeNumerically(">", 0))
	Expect(*result.Results.BWAverage).Should(BeNumerically(">", 0))
}

// execCommandEventually executes a command on a pod repeatedly until it succeeds or the timeout is reached
func execCommandEventually(restClient *rest.RESTClient, config *rest.Config, namespace string, podName string, command []string, timeout time.Duration, errorParser ErrorParserFunc) string {
	fmt.Printf("Executing command %v on pod '%s' in namespace '%s' (timeout: %v)\n", command, podName, namespace, timeout)

	var output string
	var attemptCount int
	var err error

	Eventually(func(g Gomega) {
		attemptCount++
		output, err = executeCommandOnce(restClient, config, namespace, podName, command, errorParser)
		if err != nil {
			fmt.Printf("Attempt %d failed, retrying in 5 seconds...\n", attemptCount)
		}
		g.Expect(err).NotTo(HaveOccurred(), "command %v execution failed: %v", command, output)
	}, timeout, 5*time.Second).Should(Succeed())

	return output
}

// execCommandFailConsistently executes a command on a pod repeatedly, expecting it to fail, until it unexpectly succeeds or the timeout is reached
func execCommandFailConsistently(restClient *rest.RESTClient, config *rest.Config, namespace string, podName string, command []string, expectFailure error, timeout time.Duration, errorParser ErrorParserFunc) {
	fmt.Printf("Executing command %v on pod '%s' in namespace '%s' (timeout: %v) - expecting failure\n", command, podName, namespace, timeout)

	Consistently(func(g Gomega) {
		_, err := executeCommandOnce(restClient, config, namespace, podName, command, errorParser)
		g.Expect(err).To(HaveOccurred(), "command %v should consistently fail", command)
		Expect(errors.Is(err, expectFailure)).To(BeTrue(), "command %v should fail with %v, but failed with %v", command, expectFailure, err)
	}, timeout, 5*time.Second).Should(Succeed())
}

// executeCommandOnce executes a command on a pod once and returns the output and error
func executeCommandOnce(restClient *rest.RESTClient, config *rest.Config, namespace string, podName string, command []string, errorParser ErrorParserFunc) (string, error) {
	req := restClient.Post().
		Resource("pods").
		Name(podName).
		Namespace(namespace).
		SubResource("exec").
		VersionedParams(&corev1.PodExecOptions{
			Command: command,
			Stdout:  true,
			Stderr:  true,
		}, scheme.ParameterCodec)

	exec, err := remotecommand.NewSPDYExecutor(config, "POST", req.URL())
	if err != nil {
		fmt.Printf("failed to create executor: %v", err)
		return "", ErrCreateExecutor
	}

	var stdout, stderr bytes.Buffer

	ctx, cancel := context.WithTimeout(context.Background(), ExecTimeout)
	defer cancel()

	err = exec.StreamWithContext(ctx, remotecommand.StreamOptions{
		Stdout: &stdout,
		Stderr: &stderr,
	})
	stdoutStr := stdout.String()
	stderrStr := stderr.String()

	if err != nil {
		fmt.Print(errorParser(stdoutStr, stderrStr))
		return fmt.Sprintf("%v (stderr: %s, stdout: %s)", err, stderrStr, stdoutStr), ErrExecFailed
	}

	return stdoutStr, nil
}

// AssertPingSuccess asserts that ping between pods succeeds
func AssertPingSuccess(restClient *rest.RESTClient, config *rest.Config, namespace, fromPod, toPodIP string) {
	execCommandEventually(restClient, config, namespace, fromPod, []string{"ping", "-c", "2", toPodIP}, 30*time.Second, DefaultErrorParser)
}

// AssertPingFailure asserts that ping between pods fails with ErrExecFailed error
func AssertPingFailure(restClient *rest.RESTClient, config *rest.Config, namespace, fromPod, toPodIP string) {
	execCommandFailConsistently(restClient, config, namespace, fromPod, []string{"ping", "-c", "2", toPodIP}, ErrExecFailed, 30*time.Second, DefaultErrorParser)
}
