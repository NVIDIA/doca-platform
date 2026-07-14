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
	"time"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/pkg/conditions"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ValidateBMCServerCertificateRotation exercises the BMC mTLS server-certificate rotation feature
// end-to-end against real hardware: it waits for each DPUDevice to report a ready server certificate,
// requests a manual rotation via the annotation, and verifies the controller drives a fresh cert
// through the BMC (Redfish GenerateCSR -> cert-manager -> ReplaceServerCert) and reports it Ready.
//
// This requires the Redfish install interface with reachable BMCs, so it only runs in Zero-Trust
// runs with provisioned nodes.
func ValidateBMCServerCertificateRotation(ctx context.Context, input *systemTestInput) {
	By("Listing DPUDevices")
	dpuDevices := &provisioningv1.DPUDeviceList{}
	Eventually(func(g Gomega) {
		g.Expect(input.client.List(ctx, dpuDevices)).To(Succeed())
		g.Expect(dpuDevices.Items).NotTo(BeEmpty(), "expected at least one DPUDevice for BMC server certificate rotation")
	}).WithTimeout(3 * time.Minute).WithPolling(15 * time.Second).Should(Succeed())

	for i := range dpuDevices.Items {
		key := client.ObjectKeyFromObject(&dpuDevices.Items[i])

		By(fmt.Sprintf("Waiting for DPUDevice %s BMC server certificate to be ready", key.Name))
		var baselineRotation *metav1.Time
		Eventually(func(g Gomega) {
			device := &provisioningv1.DPUDevice{}
			g.Expect(input.client.Get(ctx, key, device)).To(Succeed())
			cond := conditions.Get(device, provisioningv1.ConditionDpuDeviceBMCServerCertificateReady)
			g.Expect(cond).NotTo(BeNil(), "BMCServerCertificateReady condition not set yet")
			g.Expect(cond.Status).To(Equal(metav1.ConditionTrue), "BMC server certificate not ready: %s", cond.Message)
			g.Expect(device.Status.BMCServerCertificate).NotTo(BeNil())
			g.Expect(device.Status.BMCServerCertificate.NotAfter).NotTo(BeNil())
			baselineRotation = device.Status.BMCServerCertificate.LastRotationTime
		}).WithTimeout(10 * time.Minute).WithPolling(15 * time.Second).Should(Succeed())

		triggerValue := fmt.Sprintf("e2e-%d", time.Now().UnixNano())
		By(fmt.Sprintf("Requesting manual rotation of DPUDevice %s via %s=%s",
			key.Name, provisioningv1.RotateBMCServerCertificateAnnotation, triggerValue))
		Eventually(func(g Gomega) {
			device := &provisioningv1.DPUDevice{}
			g.Expect(input.client.Get(ctx, key, device)).To(Succeed())
			original := device.DeepCopy()
			if device.Annotations == nil {
				device.Annotations = map[string]string{}
			}
			device.Annotations[provisioningv1.RotateBMCServerCertificateAnnotation] = triggerValue
			g.Expect(input.client.Patch(ctx, device, client.MergeFrom(original))).To(Succeed())
		}).WithTimeout(time.Minute).Should(Succeed())

		By(fmt.Sprintf("Verifying DPUDevice %s rotated the BMC server certificate", key.Name))
		Eventually(func(g Gomega) {
			device := &provisioningv1.DPUDevice{}
			g.Expect(input.client.Get(ctx, key, device)).To(Succeed())
			g.Expect(device.Status.BMCServerCertificate).NotTo(BeNil())
			// The controller records the honored trigger so the same request is not processed twice.
			g.Expect(device.Status.BMCServerCertificate.ObservedManualTrigger).To(Equal(ptr.To(triggerValue)),
				"manual rotation trigger not observed yet")
			cond := conditions.Get(device, provisioningv1.ConditionDpuDeviceBMCServerCertificateReady)
			g.Expect(cond).NotTo(BeNil())
			g.Expect(cond.Status).To(Equal(metav1.ConditionTrue), "BMC server certificate not ready after rotation: %s", cond.Message)
			newRotation := device.Status.BMCServerCertificate.LastRotationTime
			g.Expect(newRotation).NotTo(BeNil())
			if baselineRotation != nil {
				g.Expect(newRotation.After(baselineRotation.Time)).To(BeTrue(),
					"expected LastRotationTime to advance after manual rotation")
			}
		}).WithTimeout(10 * time.Minute).WithPolling(15 * time.Second).Should(Succeed())
	}
}
