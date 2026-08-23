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

	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"
	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	rfclient "github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/state/redfish/client"
	"github.com/nvidia/doca-platform/pkg/conditions"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ValidateBMCFactoryResetSkippedOnBootstrap asserts the ZT bootstrap contract for BMC factory
// reset: the suite explicitly sets discoveredDPUDeviceBMCFactoryResetPolicy=Never (there is no
// CRD default for that OperatorConfig field), so every discovered DPUDevice reports
// FactoryResetSkipped while password hardening still runs and leaves no managed account on the
// factory default.
func ValidateBMCFactoryResetSkippedOnBootstrap(ctx context.Context, input *systemTestInput) {
	By("Asserting DPFOperatorConfig still has discoveredDPUDeviceBMCFactoryResetPolicy=Never")
	cfg := &operatorv1.DPFOperatorConfig{}
	Expect(input.client.Get(ctx, client.ObjectKey{
		Namespace: dpfOperatorSystemNamespace,
		Name:      configName,
	}, cfg)).To(Succeed())
	Expect(cfg.Spec.ProvisioningController).NotTo(BeNil())
	Expect(cfg.Spec.ProvisioningController.InstallInterface).NotTo(BeNil())
	Expect(cfg.Spec.ProvisioningController.InstallInterface.InstallViaRedfish).NotTo(BeNil())
	Expect(cfg.Spec.ProvisioningController.InstallInterface.InstallViaRedfish.
		DiscoveredDPUDeviceBMCFactoryResetPolicy).To(Equal(provisioningv1.BMCFactoryResetPolicyNever),
		"e2e must keep Never on OperatorConfig; discovery has no CRD default and would otherwise stamp OnInitialization")

	By("Listing DPUDevices for BMC factory reset validation")
	dpuDevices := &provisioningv1.DPUDeviceList{}
	Eventually(func(g Gomega) {
		g.Expect(input.client.List(ctx, dpuDevices, client.InNamespace(dpfOperatorSystemNamespace))).To(Succeed())
		g.Expect(dpuDevices.Items).NotTo(BeEmpty(), "expected at least one DPUDevice")
	}).WithTimeout(3 * time.Minute).WithPolling(time.Second).Should(Succeed())

	By("Asserting bootstrap devices skipped factory reset and hardened BMC passwords")
	for i := range dpuDevices.Items {
		assertFactoryResetSkippedAndHardened(ctx, input, &dpuDevices.Items[i])
	}
}

func assertFactoryResetSkippedAndHardened(ctx context.Context, input *systemTestInput, device *provisioningv1.DPUDevice) {
	key := client.ObjectKeyFromObject(device)
	By(fmt.Sprintf("Checking bootstrap factory-reset skip on DPUDevice %s", key.Name))
	Eventually(func(g Gomega) {
		current := &provisioningv1.DPUDevice{}
		g.Expect(input.client.Get(ctx, key, current)).To(Succeed())
		g.Expect(conditions.IsTrue(current, provisioningv1.ConditionDpuDeviceReady)).To(BeTrue(),
			"DPUDevice %s not Ready", key.Name)
		resetCond := conditions.Get(current, provisioningv1.ConditionDpuDeviceBMCFactoryResetReady)
		g.Expect(resetCond).NotTo(BeNil(), "BMCFactoryResetReady not set on %s", key.Name)
		g.Expect(resetCond.Status).To(Equal(metav1.ConditionTrue),
			"BMCFactoryResetReady not True on %s: %s", key.Name, resetCond.Message)
		g.Expect(resetCond.Reason).To(Equal(provisioningv1.ReasonFactoryResetSkipped),
			"expected FactoryResetSkipped on bootstrap device %s (suite sets Never), got %s: %s",
			key.Name, resetCond.Reason, resetCond.Message)
		g.Expect(provisioningv1.GetBMCFactoryResetPolicy(current.Spec.BMCFactoryResetPolicy)).To(
			Equal(provisioningv1.BMCFactoryResetPolicyNever),
			"bootstrap DPUDevice %s should carry Never from discovery", key.Name)
		g.Expect(conditions.IsTrue(current, provisioningv1.ConditionBMCCredentialsReady)).To(BeTrue(),
			"BMCCredentialsReady not True on %s", key.Name)
		g.Expect(current.BMCAddress()).NotTo(BeEmpty(), "DPUDevice %s has no BMC address", key.Name)
	}).WithTimeout(5 * time.Minute).WithPolling(time.Second).Should(Succeed())

	assertPasswordHardened(ctx, input, key.Name)
}

func assertPasswordHardened(ctx context.Context, input *systemTestInput, deviceName string) {
	key := client.ObjectKey{Namespace: dpfOperatorSystemNamespace, Name: deviceName}
	device := &provisioningv1.DPUDevice{}
	Expect(input.client.Get(ctx, key, device)).To(Succeed())
	Expect(device.BMCAddress()).NotTo(BeEmpty(), "DPUDevice %s has no BMC address", deviceName)

	cred, err := rfclient.ResolveBMCCredential(ctx, device.Namespace, device.Status.BMCCredentialSecretName, input.client)
	Expect(err).NotTo(HaveOccurred(), "resolving BMC credential for %s", deviceName)
	Expect(cred.Password).NotTo(Equal(rfclient.BMCDefaultPassword),
		"credential Secret for %s still holds the factory default password", deviceName)

	By(fmt.Sprintf("Asserting Redfish user on %s accepts the Secret password and rejects %s",
		deviceName, rfclient.BMCDefaultPassword))
	Eventually(func(g Gomega) {
		current := &provisioningv1.DPUDevice{}
		g.Expect(input.client.Get(ctx, key, current)).To(Succeed())
		g.Expect(current.BMCAddress()).NotTo(BeEmpty())
		_, user, err := rfclient.VerifyBMCCredential(current.BMCAddress(), cred.Password)
		g.Expect(err).NotTo(HaveOccurred(), "Secret password should authenticate to BMC of %s", deviceName)
		g.Expect(user).NotTo(BeEmpty())
		_, _, err = rfclient.VerifyBMCCredential(current.BMCAddress(), rfclient.BMCDefaultPassword)
		g.Expect(err).To(MatchError(rfclient.ErrBMCPasswordRejected),
			"factory default password should be rejected on Redfish user of %s", deviceName)
	}).WithTimeout(2 * time.Minute).WithPolling(time.Second).Should(Succeed())
}
