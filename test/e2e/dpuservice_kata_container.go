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

package e2e

import (
	"context"
	"fmt"
	"strings"
	"time"

	dpuservicev1 "github.com/nvidia/doca-platform/api/dpuservice/v1alpha1"
	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"
	"github.com/nvidia/doca-platform/test/utils/dpuservice"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	nodev1 "k8s.io/api/node/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	machineryruntime "k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	kataRuntimeClassName       = "kata-qemu"
	kataDPUServiceName         = "dummydpuservice-kata"
	kataDPUServiceSFResource   = "nvidia.com/bf_sf"
	kataDPUServiceSFResourceID = corev1.ResourceName(kataDPUServiceSFResource)
)

func ValidateDPUServiceKataRuntimeClass(ctx context.Context, input *SystemTestInput) {
	if !input.HasDpuNodes() {
		Skip("Skip DPUService Kata RuntimeClass test as there are no DPU nodes")
	}

	By("Waiting for kata-containers DPUService to be ready")
	dpuservice.WaitForDPUServices(ctx, input.Client, dpfOperatorSystemNamespace, []string{operatorv1.KataContainersName.String()})

	By("Waiting for kata-qemu RuntimeClass to be created in the DPU cluster")
	Eventually(func(g Gomega) {
		g.Expect(dpuClusterClient[0].Get(ctx, client.ObjectKey{Name: kataRuntimeClassName}, &nodev1.RuntimeClass{})).To(Succeed())
	}).WithTimeout(10 * time.Minute).WithPolling(time.Second).Should(Succeed())

	By("Creating a dummy DPUService that uses kata-qemu and requests an SF")
	dpuService := input.DPUService.DeepCopy()
	dpuService.Name = kataDPUServiceName
	dpuService.Namespace = dpfOperatorSystemNamespace
	dpuService.SetLabels(CleanupScope.It)
	dpuService.Spec.HelmChart.Source = dpuservicev1.ApplicationSource{
		Chart:   "dummydpuservice-chart",
		Version: tag,
		RepoURL: helmRegistry,
	}
	dpuService.Spec.HelmChart.Values = &machineryruntime.RawExtension{
		Raw: []byte(fmt.Sprintf(`{"runtimeClassName": %q, "imagePullSecrets": [{"name": %q}]}`, kataRuntimeClassName, dpfPullSecretName)),
	}
	dpuService.Spec.ServiceDaemonSet = &dpuservicev1.ServiceDaemonSetValues{
		Resources: corev1.ResourceList{
			kataDPUServiceSFResourceID: resource.MustParse("1"),
		},
	}
	Expect(input.Client.Create(ctx, dpuService)).To(Succeed())

	By("Waiting for the kata dummy DPUService pods to be running")
	VerifyClusterPods(ctx, dpuClusterClient[0], []string{kataDPUServiceName})

	By("Verifying the running kata dummy DPUService pods use kata-qemu and an SF")
	Eventually(func(g Gomega) {
		podList := kataRunningPods(ctx, g)
		for _, pod := range podList {
			g.Expect(pod.Spec.RuntimeClassName).NotTo(BeNil())
			g.Expect(*pod.Spec.RuntimeClassName).To(Equal(kataRuntimeClassName))
			g.Expect(pod.Spec.Containers).NotTo(BeEmpty())
			g.Expect(pod.Spec.Containers[0].Resources.Requests).To(HaveKeyWithValue(kataDPUServiceSFResourceID, resource.MustParse("1")))
			g.Expect(pod.Spec.Containers[0].Resources.Limits).To(HaveKeyWithValue(kataDPUServiceSFResourceID, resource.MustParse("1")))
		}
	}).WithTimeout(5 * time.Minute).WithPolling(time.Second).Should(Succeed())
}

// kataRunningPods returns all Running pods belonging to the kata dummy DPUService DaemonSet.
func kataRunningPods(ctx context.Context, g Gomega) []corev1.Pod {
	daemonSetList := &appsv1.DaemonSetList{}
	g.Expect(dpuClusterClient[0].List(ctx, daemonSetList, client.InNamespace(dpfOperatorSystemNamespace))).To(Succeed())

	var matchLabels map[string]string
	for _, ds := range daemonSetList.Items {
		if strings.Contains(ds.Name, kataDPUServiceName) {
			g.Expect(ds.Spec.Selector).NotTo(BeNil())
			matchLabels = ds.Spec.Selector.MatchLabels
			break
		}
	}
	g.Expect(matchLabels).NotTo(BeEmpty(), "expected DaemonSet containing %q to exist", kataDPUServiceName)

	podList := &corev1.PodList{}
	g.Expect(dpuClusterClient[0].List(
		ctx,
		podList,
		client.InNamespace(dpfOperatorSystemNamespace),
		client.MatchingLabels(matchLabels),
		client.MatchingFields{"status.phase": string(corev1.PodRunning)},
	)).To(Succeed())
	g.Expect(podList.Items).NotTo(BeEmpty())
	return podList.Items
}
