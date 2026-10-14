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

package controllers

import (
	"gitlab-master.nvidia.com/doca-platform-foundation/ovn-vpc/ovn/common"

	dpuservicev1 "github.com/nvidia/doca-platform/api/dpuservice/v1alpha1"
	vpcv1 "github.com/nvidia/doca-platform/api/vpc/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// this file contains helper function used for controller tests
//
//nolint:unparam
func getTestServiceInterface(name string, vn string, nodeName string) *dpuservicev1.ServiceInterface {
	return &dpuservicev1.ServiceInterface{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "default",
			Annotations: map[string]string{
				common.LSPMACAddressAnnotationKey: "00:00:00:00:00:01",
			},
		},
		Spec: dpuservicev1.ServiceInterfaceSpec{
			Node:          &nodeName,
			InterfaceType: dpuservicev1.InterfaceTypeVF,
			VF: &dpuservicev1.VF{
				VFID:           1,
				PFID:           0,
				VirtualNetwork: &vn,
			},
		},
	}
}

func getTestNode(name string, labels map[string]string) *corev1.Node {
	GinkgoHelper()

	annotations := make(map[string]string)
	// set gateway config
	gwConfig := common.GatewayConfig{
		IP: common.IPNetConfiguration{
			IPv4: "10.0.0.2/24",
		},
		MAC: "00:ae:ff:ff:01:02",
		NextHop: common.IPConfiguration{
			IPv4: "10.0.0.1",
		},
	}
	Expect(common.GatewayConfigToAnnotation(gwConfig, annotations)).To(Succeed())

	// set vtep IP
	vtepIP := common.IPNetConfiguration{
		IPv4: "20.0.0.2/24",
	}
	Expect(common.IPNetConfigurationToAnnotation(vtepIP, annotations, common.OVNVtepIPAnnotationKey)).To(Succeed())

	// set chassis ID
	annotations[common.OVNChassisIDAnnotationKey] = name

	return &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Labels:      labels,
			Annotations: annotations,
		},
	}
}

func getTestIsolationClass(name string) *vpcv1.IsolationClass {
	return &vpcv1.IsolationClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: vpcv1.IsolationClassSpec{
			Provisioner: OVNProvisionerName,
			Parameters:  map[string]string{"foo": "bar"},
		},
	}
}

func getTestVPC(name, isoClsName string, nodeSelectorLabels map[string]string) *vpcv1.DPUVPC {
	var nodeSelector *metav1.LabelSelector
	if nodeSelectorLabels != nil {
		nodeSelector = &metav1.LabelSelector{
			MatchLabels: nodeSelectorLabels,
		}
	}

	return &vpcv1.DPUVPC{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "default",
		},
		Spec: vpcv1.DPUVPCSpec{
			Tenant:             "test-tenant",
			NodeSelector:       nodeSelector,
			IsolationClassName: isoClsName,
			InterNetworkAccess: true,
		},
	}
}

func getTestDPUVirtualNetwork(name string, vpcName string, subnet string, annotations map[string]string) *vpcv1.DPUVirtualNetwork {
	return &vpcv1.DPUVirtualNetwork{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Namespace:   "default",
			Annotations: annotations,
		},
		Spec: vpcv1.DPUVirtualNetworkSpec{
			VPCName: vpcName,
			Type:    vpcv1.BridgedVirtualNetworkType,
			BridgedNetwork: &vpcv1.BridgedNetworkSpec{
				IPAM: &vpcv1.BridgedNetworkIPAMSpec{
					IPv4: &vpcv1.BridgedNetworkIPAMIPv4Spec{
						DHCP:   true,
						Subnet: subnet,
					},
				},
			},
		},
	}
}

func getTestDPUServiceInterface(name string, vn string) *dpuservicev1.DPUServiceInterface {
	return &dpuservicev1.DPUServiceInterface{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "default",
		},
		Spec: dpuservicev1.DPUServiceInterfaceSpec{
			Template: dpuservicev1.ServiceInterfaceSetSpecTemplate{
				Spec: dpuservicev1.ServiceInterfaceSetSpec{
					Template: dpuservicev1.ServiceInterfaceSpecTemplate{
						Spec: dpuservicev1.ServiceInterfaceSpec{
							InterfaceType: dpuservicev1.InterfaceTypeVF,
							VF: &dpuservicev1.VF{
								VFID:           1,
								PFID:           0,
								VirtualNetwork: &vn,
							},
						},
					},
				},
			},
		},
	}
}
