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

// bmcFactoryResetTimeout bounds the wait for a DPUDevice to finish its bootstrap factory reset and
// become Ready. The reset alone takes 3-4 minutes on a BlueField BMC, on top of the controller's
// settle delay and the password, firmware and mTLS initialization that follow it.
const bmcFactoryResetTimeout = 15 * time.Minute

// ValidateBMCFactoryResetCompletedOnBootstrap asserts the ZT bootstrap contract for BMC factory
// reset: the suite leaves discoveredDPUDeviceBMCFactoryResetPolicy unset, so discovery stamps the
// default (OnInitialization) on every DPUDevice, each BMC is reset to factory defaults exactly
// once and reports FactoryResetCompleted, and password hardening afterwards leaves no managed
// account on the factory default.
func ValidateBMCFactoryResetCompletedOnBootstrap(ctx context.Context, input *systemTestInput) {
	By("Asserting DPFOperatorConfig resolves discoveredDPUDeviceBMCFactoryResetPolicy to OnInitialization")
	cfg := &operatorv1.DPFOperatorConfig{}
	Expect(input.client.Get(ctx, client.ObjectKey{
		Namespace: dpfOperatorSystemNamespace,
		Name:      configName,
	}, cfg)).To(Succeed())
	Expect(cfg.Spec.ProvisioningController).NotTo(BeNil())
	Expect(cfg.Spec.ProvisioningController.InstallInterface).NotTo(BeNil())
	Expect(cfg.Spec.ProvisioningController.InstallInterface.InstallViaRedfish).NotTo(BeNil())
	Expect(provisioningv1.GetBMCFactoryResetPolicy(cfg.Spec.ProvisioningController.InstallInterface.InstallViaRedfish.
		DiscoveredDPUDeviceBMCFactoryResetPolicy)).To(Equal(provisioningv1.BMCFactoryResetPolicyOnInitialization),
		"e2e expects discovered DPUDevices to be factory-reset on initialization")

	By("Listing DPUDevices for BMC factory reset validation")
	dpuDevices := &provisioningv1.DPUDeviceList{}
	Eventually(func(g Gomega) {
		g.Expect(input.client.List(ctx, dpuDevices, client.InNamespace(dpfOperatorSystemNamespace))).To(Succeed())
		g.Expect(dpuDevices.Items).NotTo(BeEmpty(), "expected at least one DPUDevice")
	}).WithTimeout(3 * time.Minute).WithPolling(time.Second).Should(Succeed())

	By("Asserting bootstrap devices completed factory reset and hardened BMC passwords")
	for i := range dpuDevices.Items {
		assertFactoryResetCompletedAndHardened(ctx, input, &dpuDevices.Items[i])
	}
}

func assertFactoryResetCompletedAndHardened(ctx context.Context, input *systemTestInput, device *provisioningv1.DPUDevice) {
	key := client.ObjectKeyFromObject(device)
	By(fmt.Sprintf("Checking bootstrap factory reset completed on DPUDevice %s", key.Name))
	Eventually(func(g Gomega) {
		current := &provisioningv1.DPUDevice{}
		g.Expect(input.client.Get(ctx, key, current)).To(Succeed())
		g.Expect(provisioningv1.GetBMCFactoryResetPolicy(current.Spec.BMCFactoryResetPolicy)).To(
			Equal(provisioningv1.BMCFactoryResetPolicyOnInitialization),
			"bootstrap DPUDevice %s should carry OnInitialization from discovery", key.Name)
		resetCond := conditions.Get(current, provisioningv1.ConditionDpuDeviceBMCFactoryResetReady)
		g.Expect(resetCond).NotTo(BeNil(), "BMCFactoryResetReady not set on %s", key.Name)
		g.Expect(resetCond.Status).To(Equal(metav1.ConditionTrue),
			"BMCFactoryResetReady not True on %s: %s: %s", key.Name, resetCond.Reason, resetCond.Message)
		g.Expect(resetCond.Reason).To(Equal(provisioningv1.ReasonFactoryResetCompleted),
			"expected FactoryResetCompleted on bootstrap device %s, got %s: %s",
			key.Name, resetCond.Reason, resetCond.Message)
		g.Expect(current.Status.BMCFactoryResetRequestTime).NotTo(BeNil(),
			"DPUDevice %s reports a completed reset but never recorded submitting ResetToDefaults", key.Name)
		g.Expect(resetCond.LastTransitionTime.Time).NotTo(BeTemporally("<", current.Status.BMCFactoryResetRequestTime.Time),
			"BMCFactoryResetReady on %s turned True before the reset was requested", key.Name)
		g.Expect(conditions.IsTrue(current, provisioningv1.ConditionDpuDeviceReady)).To(BeTrue(),
			"DPUDevice %s not Ready", key.Name)
		g.Expect(conditions.IsTrue(current, provisioningv1.ConditionBMCCredentialsReady)).To(BeTrue(),
			"BMCCredentialsReady not True on %s", key.Name)
		g.Expect(current.BMCAddress()).NotTo(BeEmpty(), "DPUDevice %s has no BMC address", key.Name)
	}).WithTimeout(bmcFactoryResetTimeout).WithPolling(time.Second).Should(Succeed())

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
		_, user, err := rfclient.VerifyBMCCredential(ctx, current.BMCAddress(), cred.Password)
		g.Expect(err).NotTo(HaveOccurred(), "Secret password should authenticate to BMC of %s", deviceName)
		g.Expect(user).NotTo(BeEmpty())
		_, _, err = rfclient.VerifyBMCCredential(ctx, current.BMCAddress(), rfclient.BMCDefaultPassword)
		g.Expect(err).To(MatchError(rfclient.ErrBMCPasswordRejected),
			"factory default password should be rejected on Redfish user of %s", deviceName)
	}).WithTimeout(2 * time.Minute).WithPolling(time.Second).Should(Succeed())
}
