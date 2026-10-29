/*
SPDX-FileCopyrightText: Copyright (c) 2025 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
SPDX-License-Identifier: LicenseRef-NvidiaProprietary

NVIDIA CORPORATION, its affiliates and licensors retain all intellectual
property and proprietary rights in and to this material, related
documentation and any modifications thereto. Any use, reproduction,
disclosure or distribution of this material and related documentation
 without an express license agreement from NVIDIA CORPORATION or
its affiliates is strictly prohibited.
*/

package provisioner

import (
	"gitlab-master.nvidia.com/doca-platform-foundation/ovn-vpc/ovn/common"

	"github.com/fluxcd/pkg/runtime/patch"
	"github.com/nvidia/doca-platform/pkg/ovsutils"
	mock_networkhelper "github.com/nvidia/doca-platform/pkg/utils/networkhelper/mock"
	testutils "github.com/nvidia/doca-platform/test/utils"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/mock/gomock"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("VPC OVN DPU Provisioner", Ordered, func() {
	var (
		mockCtrl          *gomock.Controller
		cleanupObjects    []client.Object
		ovsMock           *ovsutils.MockAPI
		networkHelper     *mock_networkhelper.MockNetworkHelper
		testNode          = "test-node"
		vpcDpuProvisioner *VPCOVNDPUProvisioner
		node              *corev1.Node
	)

	BeforeEach(func() {
		mockCtrl = gomock.NewController(GinkgoT())
		ovsMock = ovsutils.NewMockAPI(mockCtrl)
		networkHelper = mock_networkhelper.NewMockNetworkHelper(mockCtrl)

		dpuVPCProvisionerConfig := Config{NodeName: testNode}
		vpcDpuProvisioner = NewVPCOVNDPUProvisioner(ctx, &dpuVPCProvisionerConfig, networkHelper, k8sClient, ovsMock)

		// Create a test node
		node = &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name:        "test-node",
				Annotations: map[string]string{},
			},
		}
		Expect(k8sClient.Create(ctx, node)).To(Succeed())
		cleanupObjects = append(cleanupObjects, node)

		err := vpcDpuProvisioner.k8sClient.Get(ctx, client.ObjectKey{Name: testNode}, vpcDpuProvisioner.node)
		Expect(err).NotTo(HaveOccurred())

		vpcDpuProvisioner.patcher = patch.NewSerialPatcher(vpcDpuProvisioner.node, vpcDpuProvisioner.k8sClient)
		vpcDpuProvisioner.node.Annotations = map[string]string{}
	})

	AfterEach(func() {
		Expect(testutils.CleanupAndWait(ctx, k8sClient, cleanupObjects...)).To(Succeed())
		mockCtrl.Finish()
	})

	It("should create the OVS bridges", func() {
		ovsMock.EXPECT().AddBridge(ctx, defaultOVNExtBridgeName, defaultOVNBridgeDatapathType, internalBridgeInterfaceType).Return(nil)
		err := vpcDpuProvisioner.setupBridges()
		Expect(err).NotTo(HaveOccurred())
	})

	It("should add the Gateway config to the node annotation", func() {
		gatewayIP := "40.40.40.2/16"
		gatewayMAC := "00:11:22:33:44:55"
		gatewayNextHop := "40.40.40.1"

		err := vpcDpuProvisioner.addGatewayConfigToNodeAnnotation(common.GatewayConfig{
			IP: common.IPNetConfiguration{
				IPv4: gatewayIP,
			},
			MAC: gatewayMAC,
			NextHop: common.IPConfiguration{
				IPv4: gatewayNextHop,
			},
		})
		Expect(err).NotTo(HaveOccurred())

		err = vpcDpuProvisioner.patcher.Patch(ctx, vpcDpuProvisioner.node)
		Expect(err).NotTo(HaveOccurred())

		Eventually(func() common.GatewayConfig {
			err := k8sClient.Get(ctx, client.ObjectKeyFromObject(node), node)
			Expect(err).NotTo(HaveOccurred())
			savedConfig, err := common.GatewayConfigFromAnnotation(node.Annotations)
			Expect(err).NotTo(HaveOccurred())
			return *savedConfig
		}).Should(Equal(common.GatewayConfig{
			IP:  common.IPNetConfiguration{IPv4: gatewayIP},
			MAC: gatewayMAC,
			NextHop: common.IPConfiguration{
				IPv4: gatewayNextHop,
			},
		}))
	})

	It("should add the VTEP IP to the node annotation", func() {
		vtepIP := "20.0.0.2/16"

		err := vpcDpuProvisioner.addIPNetToNodeAnnotation(common.IPNetConfiguration{
			IPv4: vtepIP,
		}, common.OVNVtepIPAnnotationKey)
		Expect(err).NotTo(HaveOccurred())

		err = vpcDpuProvisioner.patcher.Patch(ctx, vpcDpuProvisioner.node)
		Expect(err).NotTo(HaveOccurred())

		Eventually(func() string {
			err := k8sClient.Get(ctx, client.ObjectKeyFromObject(node), node)
			Expect(err).NotTo(HaveOccurred())
			savedConfig, err := common.IPNetConfigurationFromAnnotation(node.Annotations, common.OVNVtepIPAnnotationKey)
			Expect(err).NotTo(HaveOccurred())
			return savedConfig.IPv4
		}).Should(Equal(vtepIP))
	})
})
