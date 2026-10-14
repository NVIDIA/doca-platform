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
	"gitlab-master.nvidia.com/doca-platform-foundation/ovn-vpc/vfmac"

	dpuservicev1 "github.com/nvidia/doca-platform/api/dpuservice/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

func getTestVFMapping() *vfmac.VFMapping {
	return &vfmac.VFMapping{
		P0: map[string]vfmac.VFConfig{
			"vf1": {
				MAC: "00:00:00:00:00:01",
			},
			"vf2": {
				MAC: "00:00:00:00:00:02",
			},
			"pf": {
				MAC: "00:00:00:00:00:03",
			},
		},
	}
}

func getTestNode(name string) *corev1.Node {
	return &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
	}
}

func getServiceInterfaceObjectMeta(name string, namespace string) metav1.ObjectMeta {
	return metav1.ObjectMeta{
		Name:      name,
		Namespace: namespace,
		Annotations: map[string]string{
			common.LSPConnectedAnnotationKey: common.AnnotationValueTrue,
		},
	}
}

func getTestServiceInterfaceTypeVF(name string, namespace string, vn string, nodeName string, unknownMAC bool) *dpuservicev1.ServiceInterface {
	objectMeta := getServiceInterfaceObjectMeta(name, namespace)
	if unknownMAC {
		objectMeta.Annotations[common.LSPUnknownMACAnnotationKey] = common.AnnotationValueTrue
	}
	return &dpuservicev1.ServiceInterface{
		ObjectMeta: objectMeta,
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

func getTestServiceInterfaceTypePF(name string, namespace string, vn string, nodeName string, unknownMAC bool) *dpuservicev1.ServiceInterface {
	objectMeta := getServiceInterfaceObjectMeta(name, namespace)
	if unknownMAC {
		objectMeta.Annotations[common.LSPUnknownMACAnnotationKey] = common.AnnotationValueTrue
	}
	return &dpuservicev1.ServiceInterface{
		ObjectMeta: objectMeta,
		Spec: dpuservicev1.ServiceInterfaceSpec{
			Node:          &nodeName,
			InterfaceType: dpuservicev1.InterfaceTypePF,
			PF: &dpuservicev1.PF{
				ID:             0,
				VirtualNetwork: &vn,
			},
		},
	}
}

func getTestServiceInterfaceWithOutVirtualNetwork(name string, namespace string, nodeName string) *dpuservicev1.ServiceInterface {
	return &dpuservicev1.ServiceInterface{
		ObjectMeta: getServiceInterfaceObjectMeta(name, namespace),
		Spec: dpuservicev1.ServiceInterfaceSpec{
			Node:          &nodeName,
			InterfaceType: dpuservicev1.InterfaceTypeVF,
			VF: &dpuservicev1.VF{
				VFID: 1,
				PFID: 0,
			},
		},
	}
}

func getTestServiceInterfaceTypePhysical(name string, namespace string, nodeName string) *dpuservicev1.ServiceInterface {
	return &dpuservicev1.ServiceInterface{
		ObjectMeta: getServiceInterfaceObjectMeta(name, namespace),
		Spec: dpuservicev1.ServiceInterfaceSpec{
			Node:          &nodeName,
			InterfaceType: dpuservicev1.InterfaceTypePhysical,
			Physical: &dpuservicev1.Physical{
				InterfaceName: "p0",
			},
		},
	}
}

func getTestServiceInterfaceTypeService(name string, namespace string, vn string, nodeName string, unknownMAC bool) *dpuservicev1.ServiceInterface {
	objectMeta := getServiceInterfaceObjectMeta(name, namespace)
	if unknownMAC {
		objectMeta.Annotations[common.LSPUnknownMACAnnotationKey] = common.AnnotationValueTrue
	}
	return &dpuservicev1.ServiceInterface{
		ObjectMeta: objectMeta,
		Spec: dpuservicev1.ServiceInterfaceSpec{
			Node:          &nodeName,
			InterfaceType: dpuservicev1.InterfaceTypeService,
			Service: &dpuservicev1.ServiceDef{
				ServiceID:      "test",
				Network:        "test",
				InterfaceName:  "p0_sf",
				VirtualNetwork: &vn,
			},
		},
	}
}

func getPodWithLabels(namespace string, name string, nodeName string, labels map[string]string) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:                       name,
			Namespace:                  namespace,
			Annotations:                map[string]string{},
			Labels:                     labels,
			DeletionGracePeriodSeconds: ptr.To(int64(0)),
		},
		Spec: corev1.PodSpec{
			NodeName: nodeName,
			Containers: []corev1.Container{
				{
					Name:  "test",
					Image: "busybox",
				},
			},
		},
	}
}
