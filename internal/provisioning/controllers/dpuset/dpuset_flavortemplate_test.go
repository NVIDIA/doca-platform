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
	"context"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	cutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/util"
	"github.com/nvidia/doca-platform/internal/provisioning/dpuflavortemplate"
	"github.com/nvidia/doca-platform/pkg/conditions"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

const (
	tmplNS = "tmpl-ns"
	// okBody renders a valid DPUFlavor whose bfcfgParameters depend on a value.
	okBody = "spec:\n  bfcfgParameters:\n    - \"mtu={{ .mtu }}\"\n"
	// errBody references a missing key and fails under missingkey=error.
	errBody = "spec:\n  bfcfgParameters:\n    - \"{{ .missing }}\"\n"
)

func tmplScheme() *runtime.Scheme {
	s := runtime.NewScheme()
	Expect(provisioningv1.AddToScheme(s)).To(Succeed())
	return s
}

func tmplReconciler(scheme *runtime.Scheme, objs ...client.Object) *DPUSetReconciler {
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...).Build()
	return &DPUSetReconciler{Client: c, Scheme: scheme}
}

func newTemplate(body string) *provisioningv1.DPUFlavorTemplate {
	return &provisioningv1.DPUFlavorTemplate{
		ObjectMeta: metav1.ObjectMeta{Name: "tmpl", Namespace: tmplNS},
		Spec:       provisioningv1.DPUFlavorTemplateSpec{Template: body},
	}
}

func newDevice(valuesJSON string) *provisioningv1.DPUDevice {
	d := &provisioningv1.DPUDevice{ObjectMeta: metav1.ObjectMeta{Name: "dev", Namespace: tmplNS}}
	if valuesJSON != "" {
		d.Spec.Values = &runtime.RawExtension{Raw: []byte(valuesJSON)}
	}
	return d
}

func newTemplateDPUSet() *provisioningv1.DPUSet {
	ds := &provisioningv1.DPUSet{ObjectMeta: metav1.ObjectMeta{Name: "set", Namespace: tmplNS}}
	ds.Spec.DPUTemplate.Spec.DPUFlavorTemplate = "tmpl"
	return ds
}

var _ = Describe("DPUSet template mode", func() {
	var (
		ctx    context.Context
		scheme *runtime.Scheme
	)

	BeforeEach(func() {
		ctx = context.Background()
		scheme = tmplScheme()
	})

	Describe("createTemplateModeDPU", func() {
		It("creates the bare generated flavor and DPU, stamping hashes (ownership is the DPU controller's job)", func() {
			template := newTemplate(okBody)
			device := newDevice(`{"mtu":9000}`)
			dpuSet := newTemplateDPUSet()
			r := tmplReconciler(scheme, template, device)

			dpu := &provisioningv1.DPU{
				ObjectMeta: metav1.ObjectMeta{Name: "dpu-0", Namespace: tmplNS, Labels: map[string]string{}},
				Spec:       provisioningv1.DPUSpec{DPUDeviceName: "dev"},
			}
			Expect(r.createTemplateModeDPU(ctx, dpuSet, device, dpu)).To(Succeed())

			// DPU records the generated flavor name and input hashes.
			wantTH, wantVH, err := inputHashes(template.Spec, device.Spec.Values)
			Expect(err).NotTo(HaveOccurred())
			created := &provisioningv1.DPU{}
			Expect(r.Get(ctx, types.NamespacedName{Namespace: tmplNS, Name: "dpu-0"}, created)).To(Succeed())
			Expect(created.Spec.DPUFlavor).To(Equal("dpu-0"))
			Expect(created.Labels[cutil.DPUFlavorTemplateNameLabel]).To(Equal("tmpl"))
			Expect(created.Labels[cutil.DPUFlavorTemplateHashLabel]).To(Equal(wantTH))
			Expect(created.Labels[cutil.DPUDeviceValuesHashLabel]).To(Equal(wantVH))

			// Generated flavor exists and is labeled, but ownerRef + finalizer are NOT set here:
			// the DPU controller (adoptGeneratedFlavor) claims it on reconcile.
			flavor := &provisioningv1.DPUFlavor{}
			Expect(r.Get(ctx, types.NamespacedName{Namespace: tmplNS, Name: "dpu-0"}, flavor)).To(Succeed())
			Expect(flavor.Labels[cutil.GeneratedByLabel]).To(Equal(cutil.GeneratedByDPUFlavorTemplate))
			Expect(flavor.Labels[cutil.DPUFlavorTemplateNameLabel]).To(Equal("tmpl"))
			Expect(flavor.Spec.BFCfgParameters).To(Equal([]string{"mtu=9000"}))
			Expect(flavor.Finalizers).NotTo(ContainElement(cutil.GeneratedDPUFlavorFinalizer))
			Expect(flavor.OwnerReferences).To(BeEmpty())
		})

		It("creates the DPU with render-failed annotations and no flavor when rendering fails", func() {
			template := newTemplate(errBody)
			device := newDevice("")
			dpuSet := newTemplateDPUSet()
			r := tmplReconciler(scheme, template, device)

			dpu := &provisioningv1.DPU{
				ObjectMeta: metav1.ObjectMeta{Name: "dpu-0", Namespace: tmplNS, Labels: map[string]string{}},
				Spec:       provisioningv1.DPUSpec{DPUDeviceName: "dev"},
			}
			Expect(r.createTemplateModeDPU(ctx, dpuSet, device, dpu)).To(Succeed())

			created := &provisioningv1.DPU{}
			Expect(r.Get(ctx, types.NamespacedName{Namespace: tmplNS, Name: "dpu-0"}, created)).To(Succeed())
			Expect(created.Annotations[cutil.RenderFailedReasonAnnotation]).To(Equal(cutil.RenderFailedOnCreate))
			Expect(created.Annotations[cutil.RenderFailedMessageAnnotation]).NotTo(BeEmpty())

			flavor := &provisioningv1.DPUFlavor{}
			err := r.Get(ctx, types.NamespacedName{Namespace: tmplNS, Name: "dpu-0"}, flavor)
			Expect(err).To(HaveOccurred())
		})

		// reconcilerRejectingFlavor returns a reconciler whose DPUFlavor creates fail with createErr
		// (other creates pass through), so we can exercise admission handling without a real API server.
		reconcilerRejectingFlavor := func(createErr error, objs ...client.Object) *DPUSetReconciler {
			reject := interceptor.Funcs{
				Create: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.CreateOption) error {
					if _, ok := obj.(*provisioningv1.DPUFlavor); ok {
						return createErr
					}
					return c.Create(ctx, obj, opts...)
				},
			}
			c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...).
				WithInterceptorFuncs(reject).Build()
			return &DPUSetReconciler{Client: c, Scheme: scheme}
		}
		newTemplateDPU := func() *provisioningv1.DPU {
			return &provisioningv1.DPU{
				ObjectMeta: metav1.ObjectMeta{Name: "dpu-0", Namespace: tmplNS, Labels: map[string]string{}},
				Spec:       provisioningv1.DPUSpec{DPUDeviceName: "dev"},
			}
		}

		// invalidMTU mimics the API server rejecting an out-of-range MTU (CRD OpenAPI bound).
		invalidMTU := apierrors.NewInvalid(
			schema.GroupKind{Group: "provisioning.dpu.nvidia.com", Kind: "DPUFlavor"}, "dpu-0",
			field.ErrorList{field.Invalid(
				field.NewPath("spec", "hostNetworkInterfaceConfigs").Index(0).Child("mtu"),
				999999, "must be less than or equal to 9216")},
		)
		// badRequest mimics the DPUFlavor ValidateCreate webhook, which returns its content checks
		// (e.g. duplicate portNumber) as BadRequest rather than Invalid.
		badRequest := apierrors.NewBadRequest("host network interface configs are misconfigured: duplicate port number 0 found at index 1")

		DescribeTable("blocks the DPU on a deterministic, per-flavor admission rejection",
			func(createErr error, wantMsg string) {
				r := reconcilerRejectingFlavor(createErr, newTemplate(okBody), newDevice(`{"mtu":9000}`))
				// The blocked DPU is created and no error is returned to the reconcile loop.
				Expect(r.createTemplateModeDPU(ctx, newTemplateDPUSet(), newDevice(`{"mtu":9000}`), newTemplateDPU())).To(Succeed())

				created := &provisioningv1.DPU{}
				Expect(r.Get(ctx, types.NamespacedName{Namespace: tmplNS, Name: "dpu-0"}, created)).To(Succeed())
				Expect(created.Annotations[cutil.RenderFailedReasonAnnotation]).To(Equal(cutil.RenderFailedOnCreate))
				Expect(created.Annotations[cutil.RenderFailedMessageAnnotation]).To(ContainSubstring(wantMsg))
				// The blocked DPU still carries the expected generated flavor name (required field) and
				// the hash labels, so it is not re-rendered every reconcile - only when inputs change.
				Expect(created.Spec.DPUFlavor).To(Equal("dpu-0"))
				Expect(created.Labels[cutil.DPUFlavorTemplateNameLabel]).To(Equal("tmpl"))
				Expect(created.Labels[cutil.DPUFlavorTemplateHashLabel]).NotTo(BeEmpty())

				// No generated flavor was persisted (the create was rejected).
				flavor := &provisioningv1.DPUFlavor{}
				Expect(r.Get(ctx, types.NamespacedName{Namespace: tmplNS, Name: "dpu-0"}, flavor)).NotTo(Succeed())
			},
			Entry("Invalid: OpenAPI/CEL schema violation", invalidMTU, "mtu"),
			Entry("BadRequest: DPUFlavor ValidateCreate webhook", badRequest, "duplicate port number"),
		)

		DescribeTable("returns the error (no DPU) so the create is retried, not permanently blocked",
			func(createErr error) {
				r := reconcilerRejectingFlavor(createErr, newTemplate(okBody), newDevice(`{"mtu":9000}`))
				Expect(r.createTemplateModeDPU(ctx, newTemplateDPUSet(), newDevice(`{"mtu":9000}`), newTemplateDPU())).NotTo(Succeed())

				// The DPU was not created, so the next reconcile retries cleanly.
				created := &provisioningv1.DPU{}
				Expect(r.Get(ctx, types.NamespacedName{Namespace: tmplNS, Name: "dpu-0"}, created)).NotTo(Succeed())
			},
			// Transient: webhook endpoint unavailable.
			Entry("ServiceUnavailable", apierrors.NewServiceUnavailable("webhook temporarily unavailable")),
			// Controller-/namespace-level (RBAC, quota): identical for every flavor, fix once and retry.
			Entry("Forbidden", apierrors.NewForbidden(
				schema.GroupResource{Group: "provisioning.dpu.nvidia.com", Resource: "dpuflavors"}, "dpu-0",
				context.DeadlineExceeded)),
		)

		It("returns an error and creates nothing when the DPUFlavorTemplate is missing", func() {
			// No template is seeded: creation cannot proceed, so it errors and the reconcile retries
			// (the DPUFlavorTemplate watch also re-enqueues once the template appears).
			r := tmplReconciler(scheme, newDevice(`{"mtu":9000}`))

			Expect(r.createTemplateModeDPU(ctx, newTemplateDPUSet(), newDevice(`{"mtu":9000}`), newTemplateDPU())).NotTo(Succeed())

			// Neither the DPU nor a generated flavor was created.
			Expect(r.Get(ctx, types.NamespacedName{Namespace: tmplNS, Name: "dpu-0"}, &provisioningv1.DPU{})).NotTo(Succeed())
			Expect(r.Get(ctx, types.NamespacedName{Namespace: tmplNS, Name: "dpu-0"}, &provisioningv1.DPUFlavor{})).NotTo(Succeed())
		})
	})

	Describe("reconcileDPUFlavorTemplateCondition", func() {
		It("clears the condition for a static-flavor DPUSet", func() {
			r := tmplReconciler(scheme)
			dpuSet := newTemplateDPUSet()
			dpuSet.Spec.DPUTemplate.Spec.DPUFlavorTemplate = ""
			// A leftover condition from a prior template-mode configuration is cleared on revert.
			conditions.AddFalse(dpuSet, provisioningv1.ConditionDPUFlavorTemplateExists,
				conditions.ConditionReason(reasonDPUFlavorTemplateNotFound), conditions.ConditionMessage("stale"))

			Expect(r.reconcileDPUFlavorTemplateCondition(ctx, dpuSet)).To(Succeed())
			Expect(conditions.Get(dpuSet, provisioningv1.ConditionDPUFlavorTemplateExists)).To(BeNil())
		})

		It("sets the condition True when the template exists", func() {
			r := tmplReconciler(scheme, newTemplate(okBody))
			dpuSet := newTemplateDPUSet()

			Expect(r.reconcileDPUFlavorTemplateCondition(ctx, dpuSet)).To(Succeed())
			cond := conditions.Get(dpuSet, provisioningv1.ConditionDPUFlavorTemplateExists)
			Expect(cond).NotTo(BeNil())
			Expect(cond.Status).To(Equal(metav1.ConditionTrue))
		})

		It("sets the condition False when the template is missing", func() {
			// No template is seeded, so the referenced DPUFlavorTemplate is absent.
			r := tmplReconciler(scheme, newDevice(`{"mtu":9000}`))
			dpuSet := newTemplateDPUSet()

			Expect(r.reconcileDPUFlavorTemplateCondition(ctx, dpuSet)).To(Succeed())
			cond := conditions.Get(dpuSet, provisioningv1.ConditionDPUFlavorTemplateExists)
			Expect(cond).NotTo(BeNil())
			Expect(cond.Status).To(Equal(metav1.ConditionFalse))
			Expect(cond.Reason).To(Equal(reasonDPUFlavorTemplateNotFound))
			Expect(cond.Message).To(ContainSubstring("tmpl"))
		})

		It("self-heals to True once a missing template reappears", func() {
			r := tmplReconciler(scheme)
			dpuSet := newTemplateDPUSet()

			Expect(r.reconcileDPUFlavorTemplateCondition(ctx, dpuSet)).To(Succeed())

			Expect(r.Create(ctx, newTemplate(okBody))).To(Succeed())
			Expect(r.reconcileDPUFlavorTemplateCondition(ctx, dpuSet)).To(Succeed())
			cond := conditions.Get(dpuSet, provisioningv1.ConditionDPUFlavorTemplateExists)
			Expect(cond).NotTo(BeNil())
			Expect(cond.Status).To(Equal(metav1.ConditionTrue))
		})

		It("propagates a transient read error without marking the template missing", func() {
			// A non-NotFound read error must not be mistaken for a missing template: return the
			// error (so the reconcile retries) and leave the condition untouched.
			failGet := interceptor.Funcs{
				Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
					if _, ok := obj.(*provisioningv1.DPUFlavorTemplate); ok {
						return apierrors.NewServiceUnavailable("boom")
					}
					return c.Get(ctx, key, obj, opts...)
				},
			}
			c := fake.NewClientBuilder().WithScheme(scheme).WithInterceptorFuncs(failGet).Build()
			r := &DPUSetReconciler{Client: c, Scheme: scheme}
			dpuSet := newTemplateDPUSet()

			Expect(r.reconcileDPUFlavorTemplateCondition(ctx, dpuSet)).NotTo(Succeed())
			Expect(conditions.Get(dpuSet, provisioningv1.ConditionDPUFlavorTemplateExists)).To(BeNil())
		})
	})

	Describe("evalTemplateDPU", func() {
		// buildExisting returns a reconciler seeded with the template, device and (optionally)
		// the generated flavor, plus a DPU whose recorded hashes are deliberately stale.
		buildExisting := func(flavorSpec *provisioningv1.DPUFlavorSpec, body, values string) (*DPUSetReconciler, provisioningv1.DPUSet, provisioningv1.DPU) {
			template := newTemplate(body)
			device := newDevice(values)
			objs := []client.Object{template, device}
			if flavorSpec != nil {
				objs = append(objs, &provisioningv1.DPUFlavor{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "dpu-0",
						Namespace: tmplNS,
						Labels:    map[string]string{cutil.GeneratedByLabel: cutil.GeneratedByDPUFlavorTemplate},
					},
					Spec: *flavorSpec,
				})
			}
			r := tmplReconciler(scheme, objs...)
			dpuSet := newTemplateDPUSet()
			dpu := provisioningv1.DPU{
				ObjectMeta: metav1.ObjectMeta{
					Name: "dpu-0", Namespace: tmplNS,
					Labels: map[string]string{
						cutil.DPUFlavorTemplateNameLabel: "tmpl",
						cutil.DPUFlavorTemplateHashLabel: "stale",
						cutil.DPUDeviceValuesHashLabel:   "stale",
					},
				},
				Spec: provisioningv1.DPUSpec{DPUDeviceName: "dev", DPUFlavor: "dpu-0"},
			}
			return r, *dpuSet, dpu
		}

		It("is a no-op when recorded hashes already match the live inputs", func() {
			r, dpuSet, dpu := buildExisting(nil, okBody, `{"mtu":9000}`)
			// Stamp the DPU with the live hashes so the fast path returns early.
			th, vh, err := inputHashes(provisioningv1.DPUFlavorTemplateSpec{Template: okBody}, newDevice(`{"mtu":9000}`).Spec.Values)
			Expect(err).NotTo(HaveOccurred())
			dpu.Labels[cutil.DPUFlavorTemplateHashLabel] = th
			dpu.Labels[cutil.DPUDeviceValuesHashLabel] = vh

			eval := r.evalTemplateDPU(ctx, dpuSet, dpu)
			Expect(eval).To(Equal(templateEval{}))
		})

		It("disrupts when the referenced template name changed to an existing template", func() {
			r, dpuSet, dpu := buildExisting(nil, okBody, `{"mtu":9000}`)
			// The swap target must exist, otherwise the swap is held (see the next case).
			Expect(r.Create(ctx, &provisioningv1.DPUFlavorTemplate{
				ObjectMeta: metav1.ObjectMeta{Name: "other", Namespace: tmplNS},
				Spec:       provisioningv1.DPUFlavorTemplateSpec{Template: okBody},
			})).To(Succeed())
			dpuSet.Spec.DPUTemplate.Spec.DPUFlavorTemplate = "other"
			eval := r.evalTemplateDPU(ctx, dpuSet, dpu)
			Expect(eval.disrupt).To(BeTrue())
		})

		It("does not disrupt when the swap targets a missing template", func() {
			// Disrupting toward a missing template would delete a healthy DPU that then cannot be
			// recreated; hold instead until the target template exists.
			r, dpuSet, dpu := buildExisting(nil, okBody, `{"mtu":9000}`)
			dpuSet.Spec.DPUTemplate.Spec.DPUFlavorTemplate = "missing-template"
			eval := r.evalTemplateDPU(ctx, dpuSet, dpu)
			Expect(eval.disrupt).To(BeFalse())
		})

		It("reports equalButStale when inputs changed but the render matches the generated flavor", func() {
			rendered, err := dpuflavortemplate.Render(provisioningv1.DPUFlavorTemplateSpec{Template: okBody}, newDevice(`{"mtu":9000}`).Spec.Values)
			Expect(err).NotTo(HaveOccurred())
			r, dpuSet, dpu := buildExisting(&rendered.Spec, okBody, `{"mtu":9000}`)
			eval := r.evalTemplateDPU(ctx, dpuSet, dpu)
			Expect(eval.equalButStale).To(BeTrue())
			Expect(eval.disrupt).To(BeFalse())
			Expect(eval.liveTemplateHash).NotTo(BeEmpty())
		})

		It("disrupts when inputs changed and the generated flavor is missing", func() {
			r, dpuSet, dpu := buildExisting(nil, okBody, `{"mtu":9000}`)
			eval := r.evalTemplateDPU(ctx, dpuSet, dpu)
			Expect(eval.disrupt).To(BeTrue())
		})

		It("disrupts when the re-render differs from the generated flavor", func() {
			stale := &provisioningv1.DPUFlavorSpec{BFCfgParameters: []string{"mtu=1500"}}
			r, dpuSet, dpu := buildExisting(stale, okBody, `{"mtu":9000}`)
			eval := r.evalTemplateDPU(ctx, dpuSet, dpu)
			Expect(eval.disrupt).To(BeTrue())
		})

		It("surfaces a render error without disrupting", func() {
			r, dpuSet, dpu := buildExisting(nil, errBody, "")
			eval := r.evalTemplateDPU(ctx, dpuSet, dpu)
			Expect(eval.renderErr).To(HaveOccurred())
			Expect(eval.disrupt).To(BeFalse())
		})

		// buildDrift seeds the template, device and a matching generated flavor (so evaluation
		// reaches the dry-run normalization step) and installs a Create interceptor to drive the
		// dry-run outcome. The DPU carries stale hashes so the input-change branch is taken.
		buildDrift := func(funcs interceptor.Funcs) (*DPUSetReconciler, provisioningv1.DPUSet, provisioningv1.DPU) {
			rendered, err := dpuflavortemplate.Render(provisioningv1.DPUFlavorTemplateSpec{Template: okBody}, newDevice(`{"mtu":9000}`).Spec.Values)
			Expect(err).NotTo(HaveOccurred())
			flavor := &provisioningv1.DPUFlavor{
				ObjectMeta: metav1.ObjectMeta{Name: "dpu-0", Namespace: tmplNS,
					Labels: map[string]string{cutil.GeneratedByLabel: cutil.GeneratedByDPUFlavorTemplate}},
				Spec: rendered.Spec,
			}
			c := fake.NewClientBuilder().WithScheme(scheme).
				WithObjects(newTemplate(okBody), newDevice(`{"mtu":9000}`), flavor).
				WithInterceptorFuncs(funcs).Build()
			r := &DPUSetReconciler{Client: c, Scheme: scheme}
			dpuSet := newTemplateDPUSet()
			dpu := provisioningv1.DPU{
				ObjectMeta: metav1.ObjectMeta{Name: "dpu-0", Namespace: tmplNS,
					Labels: map[string]string{
						cutil.DPUFlavorTemplateNameLabel: "tmpl",
						cutil.DPUFlavorTemplateHashLabel: "stale",
						cutil.DPUDeviceValuesHashLabel:   "stale",
					}},
				Spec: provisioningv1.DPUSpec{DPUDeviceName: "dev", DPUFlavor: "dpu-0"},
			}
			return r, *dpuSet, dpu
		}

		It("surfaces renderErr (no disrupt) when the dry-run rejects the render as invalid", func() {
			// A rendered flavor the API server would reject must not trigger a destructive
			// reprovision: surface it like a render failure and keep the DPU running.
			r, dpuSet, dpu := buildDrift(interceptor.Funcs{
				Create: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.CreateOption) error {
					if _, ok := obj.(*provisioningv1.DPUFlavor); ok {
						return apierrors.NewInvalid(
							schema.GroupKind{Group: "provisioning.dpu.nvidia.com", Kind: "DPUFlavor"}, obj.GetName(),
							field.ErrorList{field.Invalid(field.NewPath("spec"), nil, "boom")})
					}
					return c.Create(ctx, obj, opts...)
				},
			})
			eval := r.evalTemplateDPU(ctx, dpuSet, dpu)
			Expect(eval.renderErr).To(HaveOccurred())
			Expect(eval.disrupt).To(BeFalse())
		})

		It("holds (no disrupt) on a transient dry-run error", func() {
			r, dpuSet, dpu := buildDrift(interceptor.Funcs{
				Create: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.CreateOption) error {
					if _, ok := obj.(*provisioningv1.DPUFlavor); ok {
						return apierrors.NewServiceUnavailable("boom")
					}
					return c.Create(ctx, obj, opts...)
				},
			})
			eval := r.evalTemplateDPU(ctx, dpuSet, dpu)
			Expect(eval).To(Equal(templateEval{}))
		})
	})

	Describe("evalTemplateDPUs", func() {
		It("evaluates label-less DPUs so a static->template switch migrates them", func() {
			// A DPUSet switched from a static dpuFlavor to a dpuFlavorTemplate leaves
			// pre-existing DPUs without the template-name label. They must still be
			// evaluated and flagged for migration (disrupt) rather than skipped - provided the
			// target template exists (a missing template is held, not disrupted).
			r := tmplReconciler(scheme, newTemplate(okBody))
			dpuSet := newTemplateDPUSet()
			dpu := provisioningv1.DPU{ObjectMeta: metav1.ObjectMeta{Name: "dpu-0", Namespace: tmplNS}}
			evals := r.evalTemplateDPUs(ctx, dpuSet, map[string]provisioningv1.DPU{dpu.Name: dpu})
			Expect(evals).To(HaveKey("dpu-0"))
			Expect(evals["dpu-0"].disrupt).To(BeTrue())
		})

		It("returns an empty map for a static-flavor DPUSet", func() {
			r := tmplReconciler(scheme)
			dpuSet := newTemplateDPUSet()
			dpuSet.Spec.DPUTemplate.Spec.DPUFlavorTemplate = ""
			dpu := provisioningv1.DPU{ObjectMeta: metav1.ObjectMeta{Name: "dpu-0", Namespace: tmplNS}}
			evals := r.evalTemplateDPUs(ctx, dpuSet, map[string]provisioningv1.DPU{dpu.Name: dpu})
			Expect(evals).To(BeEmpty())
		})
	})

	Describe("reconcileTemplateDPUs", func() {
		It("patches stale hash labels and clears render-failed annotations on an unchanged render", func() {
			rendered, err := dpuflavortemplate.Render(provisioningv1.DPUFlavorTemplateSpec{Template: okBody}, newDevice(`{"mtu":9000}`).Spec.Values)
			Expect(err).NotTo(HaveOccurred())
			template := newTemplate(okBody)
			device := newDevice(`{"mtu":9000}`)
			flavor := &provisioningv1.DPUFlavor{
				ObjectMeta: metav1.ObjectMeta{Name: "dpu-0", Namespace: tmplNS,
					Labels: map[string]string{cutil.GeneratedByLabel: cutil.GeneratedByDPUFlavorTemplate}},
				Spec: rendered.Spec,
			}
			dpu := &provisioningv1.DPU{
				ObjectMeta: metav1.ObjectMeta{Name: "dpu-0", Namespace: tmplNS,
					Labels: map[string]string{
						cutil.DPUFlavorTemplateNameLabel: "tmpl",
						cutil.DPUFlavorTemplateHashLabel: "stale",
						cutil.DPUDeviceValuesHashLabel:   "stale",
					},
					Annotations: map[string]string{
						cutil.RenderFailedReasonAnnotation:  cutil.RenderFailedOnUpdate,
						cutil.RenderFailedMessageAnnotation: "old failure",
					},
				},
				Spec: provisioningv1.DPUSpec{DPUDeviceName: "dev", DPUFlavor: "dpu-0"},
			}
			r := tmplReconciler(scheme, template, device, flavor, dpu)
			dpuSet := newTemplateDPUSet()

			dpuMap := map[string]provisioningv1.DPU{"dpu-0": *dpu}
			Expect(r.reconcileTemplateDPUs(ctx, dpuSet, dpuMap, r.evalTemplateDPUs(ctx, dpuSet, dpuMap))).To(Succeed())

			got := &provisioningv1.DPU{}
			Expect(r.Get(ctx, types.NamespacedName{Namespace: tmplNS, Name: "dpu-0"}, got)).To(Succeed())
			Expect(got.Labels[cutil.DPUFlavorTemplateHashLabel]).NotTo(Equal("stale"))
			Expect(got.Annotations).NotTo(HaveKey(cutil.RenderFailedReasonAnnotation))
		})

		It("records a render-failed annotation when an update render fails", func() {
			template := newTemplate(errBody)
			device := newDevice("")
			dpu := &provisioningv1.DPU{
				ObjectMeta: metav1.ObjectMeta{Name: "dpu-0", Namespace: tmplNS,
					Labels: map[string]string{
						cutil.DPUFlavorTemplateNameLabel: "tmpl",
						cutil.DPUFlavorTemplateHashLabel: "stale",
						cutil.DPUDeviceValuesHashLabel:   "stale",
					},
				},
				Spec: provisioningv1.DPUSpec{DPUDeviceName: "dev", DPUFlavor: "dpu-0"},
			}
			r := tmplReconciler(scheme, template, device, dpu)
			dpuSet := newTemplateDPUSet()

			dpuMap := map[string]provisioningv1.DPU{"dpu-0": *dpu}
			Expect(r.reconcileTemplateDPUs(ctx, dpuSet, dpuMap, r.evalTemplateDPUs(ctx, dpuSet, dpuMap))).To(Succeed())

			got := &provisioningv1.DPU{}
			Expect(r.Get(ctx, types.NamespacedName{Namespace: tmplNS, Name: "dpu-0"}, got)).To(Succeed())
			Expect(got.Annotations[cutil.RenderFailedReasonAnnotation]).To(Equal(cutil.RenderFailedOnUpdate))
		})
	})
})
