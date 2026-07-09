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

package dpuset

import (
	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

// driftFixture builds a minimally populated DPUSet/DPU pair from a single
// table-driven test row, then exercises computeDPUDrift / detectOutdated.
type driftFixture struct {
	// DPUSet template fields
	tplBFB             string
	tplFlavor          string
	tplFlavorTemplate  string // non-empty => template-mode DPUSet; DPUFlavor name comparison is bypassed
	tplSecureBoot      *bool
	tplBlueFieldSWName string  // empty => template.BlueFieldSoftware is nil
	tplClusterSelector *string // nil => template.Cluster is nil; non-nil string is a match-anything selector token
	tplStrategy        provisioningv1.StrategyType
	// DPU spec fields
	dpuBFB             string
	dpuFlavor          string
	dpuSecureBoot      *bool
	dpuBlueFieldSWName string
	dpuClusterName     string // used together with cluster list to simulate match/mismatch
	// Clusters provided to matchDPUClusterSelector
	dpuClusters []provisioningv1.DPUCluster
	// Precomputed template evaluation supplied to computeDPUDrift (template mode only).
	eval templateEval

	// Expected reasons (in fixed precedence order)
	expectReasons []dpuDriftReason
}

func buildDPUSetForDrift(f driftFixture) provisioningv1.DPUSet {
	tpl := provisioningv1.DPUTemplateSpec{
		BFB:        &provisioningv1.BFBReference{Name: f.tplBFB},
		SecureBoot: f.tplSecureBoot,
	}
	if f.tplFlavor != "" {
		tpl.DPUFlavor = &f.tplFlavor
	}
	if f.tplFlavorTemplate != "" {
		tpl.DPUFlavorTemplate = &f.tplFlavorTemplate
	}
	if f.tplBlueFieldSWName != "" {
		tpl.BlueFieldSoftware = &provisioningv1.BlueFieldSoftwareReference{Name: f.tplBlueFieldSWName}
	}
	if f.tplClusterSelector != nil {
		tpl.Cluster = &provisioningv1.ClusterSpec{
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"role": *f.tplClusterSelector}},
		}
	}
	return provisioningv1.DPUSet{
		Spec: provisioningv1.DPUSetSpec{
			Strategy:    provisioningv1.DPUSetStrategy{Type: f.tplStrategy},
			DPUTemplate: provisioningv1.DPUTemplate{Spec: tpl},
		},
	}
}

func buildDPUForDrift(f driftFixture) provisioningv1.DPU {
	dpu := provisioningv1.DPU{
		Spec: provisioningv1.DPUSpec{
			DPUFlavor:  f.dpuFlavor,
			SecureBoot: f.dpuSecureBoot,
		},
	}
	if f.dpuBFB != "" {
		dpu.Spec.BFB = ptr.To(f.dpuBFB)
	}
	if f.dpuBlueFieldSWName != "" {
		dpu.Spec.BlueFieldSoftware = ptr.To(f.dpuBlueFieldSWName)
	}
	if f.dpuClusterName != "" {
		dpu.Spec.Cluster = provisioningv1.K8sCluster{
			Name:      f.dpuClusterName,
			Namespace: "default",
		}
	}
	return dpu
}

var _ = Describe("computeDPUDrift", func() {
	var r *DPUSetReconciler
	BeforeEach(func() { r = &DPUSetReconciler{} })

	DescribeTable("returns reasons in fixed precedence order",
		func(f driftFixture) {
			dpuSet := buildDPUSetForDrift(f)
			dpu := buildDPUForDrift(f)
			drift := r.computeDPUDrift(dpuSet, dpu, f.dpuClusters, f.eval)
			Expect(drift.Reasons).To(Equal(f.expectReasons))
			Expect(drift.Diffs).To(HaveLen(len(f.expectReasons)))
		},
		Entry("no drift", driftFixture{
			tplBFB: "bfb-1", tplFlavor: "f-a", tplSecureBoot: ptr.To(true),
			dpuBFB: "bfb-1", dpuFlavor: "f-a", dpuSecureBoot: ptr.To(true),
			expectReasons: nil,
		}),
		Entry("only BFB", driftFixture{
			tplBFB: "bfb-2", tplFlavor: "f-a",
			dpuBFB: "bfb-1", dpuFlavor: "f-a",
			expectReasons: []dpuDriftReason{driftReasonBFB},
		}),
		Entry("only DPUFlavor", driftFixture{
			tplBFB: "bfb-1", tplFlavor: "f-b",
			dpuBFB: "bfb-1", dpuFlavor: "f-a",
			expectReasons: []dpuDriftReason{driftReasonDPUFlavor},
		}),
		Entry("only SecureBoot (nil -> set)", driftFixture{
			tplBFB: "bfb-1", tplFlavor: "f-a", tplSecureBoot: ptr.To(true),
			dpuBFB: "bfb-1", dpuFlavor: "f-a", dpuSecureBoot: nil,
			expectReasons: []dpuDriftReason{driftReasonSecureBoot},
		}),
		Entry("only SecureBoot (true -> false)", driftFixture{
			tplBFB: "bfb-1", tplFlavor: "f-a", tplSecureBoot: ptr.To(false),
			dpuBFB: "bfb-1", dpuFlavor: "f-a", dpuSecureBoot: ptr.To(true),
			expectReasons: []dpuDriftReason{driftReasonSecureBoot},
		}),
		Entry("only BlueFieldSoftware (template set, DPU different)", driftFixture{
			tplBFB: "bfb-1", tplFlavor: "f-a", tplBlueFieldSWName: "bfsw-2",
			dpuBFB: "bfb-1", dpuFlavor: "f-a", dpuBlueFieldSWName: "bfsw-1",
			expectReasons: []dpuDriftReason{driftReasonBlueFieldSoftware},
		}),
		Entry("BlueFieldSoftware template==nil even when DPU has value -> no drift", driftFixture{
			tplBFB: "bfb-1", tplFlavor: "f-a",
			dpuBFB: "bfb-1", dpuFlavor: "f-a", dpuBlueFieldSWName: "bfsw-old",
			expectReasons: nil,
		}),
		Entry("BFB + DPUFlavor + SecureBoot drift order", driftFixture{
			tplBFB: "bfb-2", tplFlavor: "f-b", tplSecureBoot: ptr.To(false),
			dpuBFB: "bfb-1", dpuFlavor: "f-a", dpuSecureBoot: ptr.To(true),
			expectReasons: []dpuDriftReason{driftReasonBFB, driftReasonDPUFlavor, driftReasonSecureBoot},
		}),
		Entry("all four non-cluster fields drift", driftFixture{
			tplBFB: "bfb-2", tplFlavor: "f-b", tplSecureBoot: ptr.To(false), tplBlueFieldSWName: "bfsw-2",
			dpuBFB: "bfb-1", dpuFlavor: "f-a", dpuSecureBoot: ptr.To(true), dpuBlueFieldSWName: "bfsw-1",
			expectReasons: []dpuDriftReason{driftReasonBFB, driftReasonDPUFlavor, driftReasonSecureBoot, driftReasonBlueFieldSoftware},
		}),
		// Template mode: dpuFlavor holds a per-DPU generated name, so the direct name
		// comparison is bypassed and the supplied templateEval decides DPUFlavor drift.
		Entry("template mode: eval.disrupt -> DPUFlavor drift", driftFixture{
			tplBFB: "bfb-1", tplFlavorTemplate: "tmpl",
			dpuBFB: "bfb-1", dpuFlavor: "generated-dpu-0",
			eval:          templateEval{disrupt: true},
			expectReasons: []dpuDriftReason{driftReasonDPUFlavor},
		}),
		Entry("template mode: no disrupt -> no DPUFlavor drift despite differing flavor names", driftFixture{
			tplBFB: "bfb-1", tplFlavor: "f-b", tplFlavorTemplate: "tmpl",
			dpuBFB: "bfb-1", dpuFlavor: "generated-dpu-0",
			eval:          templateEval{},
			expectReasons: nil,
		}),
	)

	It("includes ClusterSelector drift when DPU cluster does not match the selector", func() {
		f := driftFixture{
			tplBFB: "bfb-1", tplFlavor: "f-a",
			dpuBFB: "bfb-1", dpuFlavor: "f-a",
			tplClusterSelector: ptr.To("primary"),
			dpuClusterName:     "secondary-cluster",
			dpuClusters: []provisioningv1.DPUCluster{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "primary-cluster",
						Namespace: "default",
						Labels:    map[string]string{"role": "primary"},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "secondary-cluster",
						Namespace: "default",
						Labels:    map[string]string{"role": "secondary"},
					},
				},
			},
		}
		drift := r.computeDPUDrift(buildDPUSetForDrift(f), buildDPUForDrift(f), f.dpuClusters, templateEval{})
		Expect(drift.Reasons).To(Equal([]dpuDriftReason{driftReasonClusterSelector}))
	})

	It("treats unassigned cluster as a match (no ClusterSelector drift)", func() {
		f := driftFixture{
			tplBFB: "bfb-1", tplFlavor: "f-a",
			dpuBFB: "bfb-1", dpuFlavor: "f-a",
			tplClusterSelector: ptr.To("primary"),
			// dpuClusterName empty -> matchDPUClusterSelector returns true
		}
		drift := r.computeDPUDrift(buildDPUSetForDrift(f), buildDPUForDrift(f), nil, templateEval{})
		Expect(drift.Reasons).To(BeNil())
	})
})

var _ = Describe("dpuDrift.without", func() {
	It("returns the same drift when exclude is empty", func() {
		d := dpuDrift{
			Reasons: []dpuDriftReason{driftReasonBFB, driftReasonClusterSelector},
			Diffs:   []string{"BFB diff", "cluster diff"},
		}
		Expect(d.without().Reasons).To(Equal(d.Reasons))
	})
	It("removes specified reasons and keeps Diffs aligned", func() {
		d := dpuDrift{
			Reasons: []dpuDriftReason{driftReasonBFB, driftReasonClusterSelector, driftReasonDPUFlavor},
			Diffs:   []string{"BFB diff", "cluster diff", "flavor diff"},
		}
		out := d.without(driftReasonClusterSelector)
		Expect(out.Reasons).To(Equal([]dpuDriftReason{driftReasonBFB, driftReasonDPUFlavor}))
		Expect(out.Diffs).To(Equal([]string{"BFB diff", "flavor diff"}))
	})
	It("returns empty when all reasons are excluded", func() {
		d := dpuDrift{
			Reasons: []dpuDriftReason{driftReasonClusterSelector},
			Diffs:   []string{"cluster diff"},
		}
		out := d.without(driftReasonClusterSelector)
		Expect(out.empty()).To(BeTrue())
	})
})

var _ = Describe("detectOutdated", func() {
	var r *DPUSetReconciler
	BeforeEach(func() { r = &DPUSetReconciler{} })

	Context("when strategy is RollingUpdate", func() {
		It("returns true with BFB reason when BFB has diverged", func() {
			f := driftFixture{
				tplBFB: "bfb-2", tplFlavor: "f-a", tplStrategy: provisioningv1.RollingUpdateStrategyType,
				dpuBFB: "bfb-1", dpuFlavor: "f-a",
			}
			outdated, reason, msg := r.detectOutdated(buildDPUSetForDrift(f), buildDPUForDrift(f), nil, templateEval{})
			Expect(outdated).To(BeTrue())
			Expect(reason).To(Equal(provisioningv1.DPUOutdatedReasonBFB))
			Expect(msg).To(ContainSubstring("BFB: bfb-1 -> bfb-2"))
		})
	})

	Context("under OnDelete strategy", func() {
		baseOnDelete := func(f driftFixture) driftFixture {
			f.tplStrategy = provisioningv1.OnDeleteStrategyType
			return f
		}

		It("returns false when nothing has changed", func() {
			f := baseOnDelete(driftFixture{
				tplBFB: "bfb-1", tplFlavor: "f-a",
				dpuBFB: "bfb-1", dpuFlavor: "f-a",
			})
			outdated, reason, msg := r.detectOutdated(buildDPUSetForDrift(f), buildDPUForDrift(f), nil, templateEval{})
			Expect(outdated).To(BeFalse())
			Expect(reason).To(BeEmpty())
			Expect(msg).To(BeEmpty())
		})

		It("returns true with BFB reason when only BFB changed", func() {
			f := baseOnDelete(driftFixture{
				tplBFB: "bfb-2", tplFlavor: "f-a",
				dpuBFB: "bfb-1", dpuFlavor: "f-a",
			})
			outdated, reason, msg := r.detectOutdated(buildDPUSetForDrift(f), buildDPUForDrift(f), nil, templateEval{})
			Expect(outdated).To(BeTrue())
			Expect(reason).To(Equal(provisioningv1.DPUOutdatedReasonBFB))
			Expect(msg).To(ContainSubstring("BFB: bfb-1 -> bfb-2"))
		})

		It("returns true with DPUFlavor reason when only DPUFlavor changed", func() {
			f := baseOnDelete(driftFixture{
				tplBFB: "bfb-1", tplFlavor: "f-b",
				dpuBFB: "bfb-1", dpuFlavor: "f-a",
			})
			outdated, reason, msg := r.detectOutdated(buildDPUSetForDrift(f), buildDPUForDrift(f), nil, templateEval{})
			Expect(outdated).To(BeTrue())
			Expect(reason).To(Equal(provisioningv1.DPUOutdatedReasonDPUFlavor))
			Expect(msg).To(ContainSubstring("DPUFlavor: f-a -> f-b"))
		})

		It("returns true with SecureBoot reason for nil->set transition", func() {
			f := baseOnDelete(driftFixture{
				tplBFB: "bfb-1", tplFlavor: "f-a", tplSecureBoot: ptr.To(true),
				dpuBFB: "bfb-1", dpuFlavor: "f-a", dpuSecureBoot: nil,
			})
			outdated, reason, _ := r.detectOutdated(buildDPUSetForDrift(f), buildDPUForDrift(f), nil, templateEval{})
			Expect(outdated).To(BeTrue())
			Expect(reason).To(Equal(provisioningv1.DPUOutdatedReasonSecureBoot))
		})

		It("returns true with BlueFieldSoftware reason when template+DPU diverge", func() {
			f := baseOnDelete(driftFixture{
				tplBFB: "bfb-1", tplFlavor: "f-a", tplBlueFieldSWName: "bfsw-2",
				dpuBFB: "bfb-1", dpuFlavor: "f-a", dpuBlueFieldSWName: "bfsw-1",
			})
			outdated, reason, _ := r.detectOutdated(buildDPUSetForDrift(f), buildDPUForDrift(f), nil, templateEval{})
			Expect(outdated).To(BeTrue())
			Expect(reason).To(Equal(provisioningv1.DPUOutdatedReasonBlueFieldSoftware))
		})

		It("returns false when template.BlueFieldSoftware==nil even if DPU has one", func() {
			f := baseOnDelete(driftFixture{
				tplBFB: "bfb-1", tplFlavor: "f-a",
				dpuBFB: "bfb-1", dpuFlavor: "f-a", dpuBlueFieldSWName: "bfsw-old",
			})
			outdated, _, _ := r.detectOutdated(buildDPUSetForDrift(f), buildDPUForDrift(f), nil, templateEval{})
			Expect(outdated).To(BeFalse())
		})

		It("returns false when only the cluster selector has diverged", func() {
			f := baseOnDelete(driftFixture{
				tplBFB: "bfb-1", tplFlavor: "f-a",
				dpuBFB: "bfb-1", dpuFlavor: "f-a",
				tplClusterSelector: ptr.To("primary"),
				dpuClusterName:     "secondary-cluster",
				dpuClusters: []provisioningv1.DPUCluster{
					{ObjectMeta: metav1.ObjectMeta{Name: "primary-cluster", Namespace: "default", Labels: map[string]string{"role": "primary"}}},
					{ObjectMeta: metav1.ObjectMeta{Name: "secondary-cluster", Namespace: "default", Labels: map[string]string{"role": "secondary"}}},
				},
			})
			outdated, reason, msg := r.detectOutdated(buildDPUSetForDrift(f), buildDPUForDrift(f), f.dpuClusters, templateEval{})
			Expect(outdated).To(BeFalse())
			Expect(reason).To(BeEmpty())
			Expect(msg).To(BeEmpty())
		})

		It("reports first-precedence reason on multi-field drift; message lists every non-cluster diff", func() {
			f := baseOnDelete(driftFixture{
				tplBFB: "bfb-2", tplFlavor: "f-b", tplSecureBoot: ptr.To(true),
				dpuBFB: "bfb-1", dpuFlavor: "f-a", dpuSecureBoot: nil,
			})
			outdated, reason, msg := r.detectOutdated(buildDPUSetForDrift(f), buildDPUForDrift(f), nil, templateEval{})
			Expect(outdated).To(BeTrue())
			Expect(reason).To(Equal(provisioningv1.DPUOutdatedReasonBFB))
			Expect(msg).To(ContainSubstring("BFB: bfb-1 -> bfb-2"))
			Expect(msg).To(ContainSubstring("DPUFlavor: f-a -> f-b"))
			Expect(msg).To(ContainSubstring("SecureBoot: <nil> -> true"))
		})

		It("ignores ClusterSelector but reports BFB when both are out of sync", func() {
			f := baseOnDelete(driftFixture{
				tplBFB: "bfb-2", tplFlavor: "f-a",
				dpuBFB: "bfb-1", dpuFlavor: "f-a",
				tplClusterSelector: ptr.To("primary"),
				dpuClusterName:     "secondary-cluster",
				dpuClusters: []provisioningv1.DPUCluster{
					{ObjectMeta: metav1.ObjectMeta{Name: "primary-cluster", Namespace: "default", Labels: map[string]string{"role": "primary"}}},
					{ObjectMeta: metav1.ObjectMeta{Name: "secondary-cluster", Namespace: "default", Labels: map[string]string{"role": "secondary"}}},
				},
			})
			outdated, reason, msg := r.detectOutdated(buildDPUSetForDrift(f), buildDPUForDrift(f), f.dpuClusters, templateEval{})
			Expect(outdated).To(BeTrue())
			Expect(reason).To(Equal(provisioningv1.DPUOutdatedReasonBFB))
			Expect(msg).To(ContainSubstring("BFB: bfb-1 -> bfb-2"))
			Expect(msg).ToNot(ContainSubstring("ClusterSelector"))
		})
	})
})
