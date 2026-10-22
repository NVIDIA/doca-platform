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

package controllers

import (
	"context"
	"testing"

	dpuservicev1 "github.com/nvidia/doca-platform/api/dpuservice/v1alpha1"
	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"

	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// TestTracingRevision checks that a recreated input and the DPUService inputs are part of the revision.
func TestTracingRevision(t *testing.T) {
	g := NewWithT(t)
	ctx := context.Background()
	deployment := revisionDeployment(false)
	withServices := revisionDeployment(true)

	original := tracingRevision(ctx, revisionClient(t, revisionBFB("uid-1", 1)), deployment)
	recreated := tracingRevision(ctx, revisionClient(t, revisionBFB("uid-2", 1)), deployment)
	bumped := tracingRevision(ctx, revisionClient(t, revisionBFB("uid-1", 2)), deployment)
	services := tracingRevision(ctx, revisionClient(t,
		revisionBFB("uid-1", 1),
		&dpuservicev1.DPUServiceTemplate{ObjectMeta: metav1.ObjectMeta{Name: "tmpl", Namespace: "ns", UID: "tmpl-uid", Generation: 3}},
		&dpuservicev1.DPUServiceConfiguration{ObjectMeta: metav1.ObjectMeta{Name: "cfg", Namespace: "ns", UID: "cfg-uid", Generation: 4}},
	), withServices)

	g.Expect(recreated).NotTo(Equal(original), "a recreated input with the same generation starts a new revision")
	g.Expect(bumped).NotTo(Equal(original), "a new generation of an input starts a new revision")
	g.Expect(services).NotTo(Equal(original), "DPUService inputs are part of the revision")
}

// revisionClient returns a client containing objs, keeping the UID and generation set on them.
func revisionClient(t *testing.T, objs ...client.Object) client.Client {
	t.Helper()
	g := NewWithT(t)
	scheme := runtime.NewScheme()
	g.Expect(dpuservicev1.AddToScheme(scheme)).To(Succeed())
	g.Expect(provisioningv1.AddToScheme(scheme)).To(Succeed())
	return fake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...).Build()
}

// revisionDeployment returns a DPUDeployment whose only provisioning input is the BFB named bfb.
// withServices adds a DPUServiceTemplate and DPUServiceConfiguration reference.
func revisionDeployment(withServices bool) *dpuservicev1.DPUDeployment {
	deployment := &dpuservicev1.DPUDeployment{
		ObjectMeta: metav1.ObjectMeta{Name: "dep", Namespace: "ns"},
		Spec: dpuservicev1.DPUDeploymentSpec{
			DPUs: dpuservicev1.DPUs{BFB: ptr.To("bfb")},
		},
	}
	if withServices {
		deployment.Spec.Services = map[string]dpuservicev1.DPUDeploymentServiceConfiguration{
			"svc": {ServiceTemplate: "tmpl", ServiceConfiguration: "cfg"},
		}
	}
	return deployment
}

// revisionBFB returns a BFB named bfb with the given UID and generation.
func revisionBFB(uid types.UID, generation int64) *provisioningv1.BFB {
	return &provisioningv1.BFB{ObjectMeta: metav1.ObjectMeta{
		Name: "bfb", Namespace: "ns", UID: uid, Generation: generation,
	}}
}
