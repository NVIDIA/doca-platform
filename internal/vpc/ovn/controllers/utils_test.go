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
	"context"

	vpcv1 "github.com/nvidia/doca-platform/api/vpc/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	fakeClient "sigs.k8s.io/controller-runtime/pkg/client/fake"
)

var _ = Describe("Controllers utils", func() {
	Context("NodeInVPC", func() {
		It("should return true when node matches vpc selector", func() {
			node := &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"tenant": "foo"},
				},
			}
			vpc := &vpcv1.DPUVPC{
				Spec: vpcv1.DPUVPCSpec{
					NodeSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"tenant": "foo"},
					},
				},
			}
			result, err := NodeInVPC(node, vpc)
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(BeTrue())
		})

		It("should return false when node does not match vpc selector", func() {
			node := &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"tenant": "foo"},
				},
			}
			vpc := &vpcv1.DPUVPC{
				Spec: vpcv1.DPUVPCSpec{
					NodeSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"tenant": "bar"},
					},
				},
			}
			result, err := NodeInVPC(node, vpc)
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(BeFalse())
		})
	})

	Context("VPCForNode", func() {
		It("should return nil when there are no VPCs", func() {
			node := &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"tenant": "foo"},
				},
			}
			vpcs := []vpcv1.DPUVPC{}
			result, err := VPCForNode(node, vpcs)
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(BeNil())
		})

		It("should return nil when node belongs to no VPC", func() {
			node := &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"tenant": "foo"},
				},
			}
			vpcs := []vpcv1.DPUVPC{
				{
					Spec: vpcv1.DPUVPCSpec{
						NodeSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{"tenant": "bar"},
						},
					},
				},
			}
			result, err := VPCForNode(node, vpcs)
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(BeNil())
		})

		It("should error when node belongs to multiple VPCs", func() {
			node := &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"tenant": "foo", "zone": "A"},
				},
			}
			vpcs := []vpcv1.DPUVPC{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "vpc1"},
					Spec: vpcv1.DPUVPCSpec{
						NodeSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{"tenant": "foo"},
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{Name: "vpc2"},
					Spec: vpcv1.DPUVPCSpec{
						NodeSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{"zone": "A"},
						},
					},
				},
			}
			_, err := VPCForNode(node, vpcs)
			Expect(err).To(HaveOccurred())
		})
	})

	Context("IsolationClassForVPC", func() {
		It("should return matching isolationClass", func() {
			isoCls := &vpcv1.IsolationClass{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-iso",
				},
				Spec: vpcv1.IsolationClassSpec{
					Provisioner: OVNProvisionerName,
				},
			}
			fakec := fakeClient.NewClientBuilder().WithScheme(testScheme).WithObjects(isoCls).Build()
			ctx := context.Background()
			vpc := &vpcv1.DPUVPC{
				Spec: vpcv1.DPUVPCSpec{
					IsolationClassName: "test-iso",
				},
			}
			iso, err := IsolationClassForVPC(ctx, fakec, vpc)
			Expect(err).ToNot(HaveOccurred())
			Expect(iso.Name).To(Equal(isoCls.Name))
		})

		It("should return error if no matching isolationClass", func() {
			fakec := fakeClient.NewClientBuilder().WithScheme(testScheme).Build()
			ctx := context.Background()
			vpc := &vpcv1.DPUVPC{
				Spec: vpcv1.DPUVPCSpec{
					IsolationClassName: "test-iso",
				},
			}
			iso, err := IsolationClassForVPC(ctx, fakec, vpc)
			Expect(err).To(HaveOccurred())
			Expect(iso).To(BeNil())
		})

		It("should return error if isolationClass provisioner is not OVN", func() {
			isoCls := &vpcv1.IsolationClass{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-iso",
				},
				Spec: vpcv1.IsolationClassSpec{
					Provisioner: "some.provisioner",
				},
			}
			fakec := fakeClient.NewClientBuilder().WithScheme(testScheme).WithObjects(isoCls).Build()
			ctx := context.Background()
			vpc := &vpcv1.DPUVPC{
				Spec: vpcv1.DPUVPCSpec{
					IsolationClassName: "test-iso",
				},
			}
			iso, err := IsolationClassForVPC(ctx, fakec, vpc)
			Expect(err).To(HaveOccurred())
			Expect(iso).To(BeNil())
		})
	})

	Context("OVNSBClientFromIsolationClass", func() {
		It("should return error if ovn-sb-endpoint parameter is not set in isolation class", func() {
			isoCls := getTestIsolationClass("test")
			_, err := OVNSBClientFromIsolationClass(context.TODO(), isoCls)
			Expect(err).To(HaveOccurred())
			Expect(err).To(MatchError(ErrIsolationClassMissingParameter))
		})
	})

	Context("OVNClientFromIsolationClass", func() {
		It("should return error if ovn-nb-endpoint parameter is not set in isolation class", func() {
			isoCls := getTestIsolationClass("test")
			_, err := OVNClientFromIsolationClass(context.TODO(), isoCls)
			Expect(err).To(HaveOccurred())
			Expect(err).To(MatchError(ErrIsolationClassMissingParameter))
		})
	})
})
