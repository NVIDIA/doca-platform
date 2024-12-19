/*
Copyright 2024 NVIDIA

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

package controller

import (
	"context"
	"fmt"
	"testing"
	"time"

	dpuservicev1 "github.com/nvidia/doca-platform/api/dpuservice/v1alpha1"
	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"
	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/internal/operator/inventory"
	"github.com/nvidia/doca-platform/internal/release"

	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

var testFinalizer = "object-with-finalizer"

func TestDPFOperatorConfigReconciler_reconcileDelete(t *testing.T) {
	g := NewWithT(t)
	scheme := runtime.NewScheme()
	g.Expect(corev1.AddToScheme(scheme)).To(Succeed())
	g.Expect(appsv1.AddToScheme(scheme)).To(Succeed())

	g.Expect(operatorv1.AddToScheme(scheme)).To(Succeed())
	g.Expect(dpuservicev1.AddToScheme(scheme)).To(Succeed())
	g.Expect(provisioningv1.AddToScheme(scheme)).To(Succeed())

	tests := []struct {
		name string
		// gvkWithFinalizer is the GVK of the objects that should have a finalizer which stops the flow of the deletion.
		// Note: This object is not counted in the `ObjectsExpected` numbers above.
		gvkWithFinalizer *schema.GroupVersionKind
		// dpuDeploymentObjsExpected is a boolean describing if resources from this group should exist after running deletion for this test case.
		dpuDeploymentObjsExpected bool
		// serviceChainObjsExpected is a boolean describing if resources from this group should exist.
		serviceChainObjsExpected bool
		// provisioningObjsExpected is a boolean describing if resources from this group should exist.
		provisioningObjsExpected bool
		// dpuServiceObjsExpected is a boolean describing if resources from this group should exist.
		dpuServiceObjsExpected bool
	}{
		{
			name:                      "Deletion works with no finalizers set",
			dpuDeploymentObjsExpected: false,
			serviceChainObjsExpected:  false,
			dpuServiceObjsExpected:    false,
			provisioningObjsExpected:  false,
		},
		{
			name:                      "Ensure DPUService, Provisioning and ServiceChain is not deleted if DPUDeployment has finalizer",
			gvkWithFinalizer:          &dpuservicev1.DPUDeploymentGroupVersionKind,
			dpuDeploymentObjsExpected: false,
			serviceChainObjsExpected:  true,
			dpuServiceObjsExpected:    true,
			provisioningObjsExpected:  true,
		},
		{
			name:                      "Ensure DPUService, Provisioning is not deleted if ServiceChain has finalizer",
			gvkWithFinalizer:          &dpuservicev1.DPUServiceChainGroupVersionKind,
			dpuDeploymentObjsExpected: false,
			serviceChainObjsExpected:  false,
			dpuServiceObjsExpected:    true,
			provisioningObjsExpected:  true,
		},
		{
			name:                      "Ensure Provisioning is not deleted if DPUSet has finalizer",
			gvkWithFinalizer:          &dpuservicev1.DPUServiceGroupVersionKind,
			dpuDeploymentObjsExpected: false,
			serviceChainObjsExpected:  false,
			dpuServiceObjsExpected:    false,
			provisioningObjsExpected:  true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "test-ns"}}

			dpfOperatorConfig := &operatorv1.DPFOperatorConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test",
					Namespace: ns.Name,
					// Adding the finalizer here to simulate a normal DPFOperatorConfig with deletion protection.
					Finalizers: []string{operatorv1.DPFOperatorConfigFinalizer},
				},
				Spec: operatorv1.DPFOperatorConfigSpec{
					ProvisioningController: operatorv1.ProvisioningControllerConfiguration{
						BFBPersistentVolumeClaimName: "oof",
					},
				},
			}
			objectsToCreate := []client.Object{dpfOperatorConfig}
			// Create an object with a finalizer. This object will never be deleted in the test and will stop other objects from being deleted.
			objectWithFinalizer := &unstructured.Unstructured{}
			if tt.gvkWithFinalizer != nil {
				objectWithFinalizer.SetName("object-with-finalizer")
				objectWithFinalizer.SetNamespace(ns.Name)
				objectWithFinalizer.SetGroupVersionKind(*tt.gvkWithFinalizer)
				objectWithFinalizer.SetFinalizers([]string{testFinalizer})
				objectsToCreate = append(objectsToCreate, objectWithFinalizer)
			}
			// Generate the objects for the test.
			objectsToCreate = append(objectsToCreate, generateObjectsByGVK(ns.Name, dpuDeploymentResources)...)
			objectsToCreate = append(objectsToCreate, generateObjectsByGVK(ns.Name, serviceChainResources)...)
			objectsToCreate = append(objectsToCreate, generateObjectsByGVK(ns.Name, dpuserviceResources)...)
			objectsToCreate = append(objectsToCreate, generateObjectsByGVK(ns.Name, provisioningResources)...)

			c := fake.NewClientBuilder().WithObjects(objectsToCreate...).WithScheme(scheme).Build()
			g.Expect(c.Create(context.Background(), ns)).To(Succeed())

			inv := inventory.New()
			g.Expect(inv.ParseAll()).To(Succeed())
			defaults := release.NewDefaults()
			g.Expect(defaults.Parse()).To(Succeed())

			r := &DPFOperatorConfigReconciler{
				Client:    c,
				Defaults:  defaults,
				Inventory: inv,
			}

			g.Expect(c.Delete(context.Background(), dpfOperatorConfig)).To(Succeed())
			// Wait for the existing objects to eventually get into the correct state.
			g.Eventually(func(g Gomega) {

				_ = r.reconcileDelete(ctx, dpfOperatorConfig)
				// 1) Expect the DPUDeployment objects to match the expected state.
				g.Expect(objectsInListStillExist(r.Client, dpuDeploymentResources)).To(Equal(tt.dpuDeploymentObjsExpected))
				// 2) Expect the ServiceChain objects to match the expected state.
				g.Expect(objectsInListStillExist(r.Client, serviceChainResources)).To(Equal(tt.serviceChainObjsExpected))
				// 3) Expect the DPUService objects to match the expected state.
				g.Expect(objectsInListStillExist(r.Client, dpuserviceResources)).To(Equal(tt.dpuServiceObjsExpected))
				// 4) Expect the Provisioning objects to match the expected state.
				g.Expect(objectsInListStillExist(r.Client, provisioningResources)).To(Equal(tt.provisioningObjsExpected))
			}).WithTimeout(5 * time.Second).Should(Succeed())

			// Ensure the state does not change.
			g.Consistently(func(g Gomega) {
				_ = r.reconcileDelete(ctx, dpfOperatorConfig)
				// 1) Expect the DPUDeployment list to have the right number of members.
				g.Expect(objectsInListStillExist(r.Client, dpuDeploymentResources)).To(Equal(tt.dpuDeploymentObjsExpected))
				// 2) Expect the ServiceChain list to have the right number of members.
				g.Expect(objectsInListStillExist(r.Client, serviceChainResources)).To(Equal(tt.serviceChainObjsExpected))
				// 3) Expect the DPUService list to have the right number of members.
				g.Expect(objectsInListStillExist(r.Client, dpuserviceResources)).To(Equal(tt.dpuServiceObjsExpected))
				// 4) Expect the Provisioning list to have the right number of members.
				g.Expect(objectsInListStillExist(r.Client, provisioningResources)).To(Equal(tt.provisioningObjsExpected))
			}).WithTimeout(5 * time.Second).Should(Succeed())

			// Ensure the DPFOperatorConfig has its finalizer removed once all its dependent objects are deleted.
			g.Eventually(func(g Gomega) {
				// Remove the finalizer from the blocking object if it was set.
				if tt.gvkWithFinalizer != nil {
					err := c.Patch(context.Background(), objectWithFinalizer, client.RawPatch(types.MergePatchType, []byte(`{"metadata":{"finalizers":[]}}`)))
					g.Expect(client.IgnoreNotFound(err)).NotTo(HaveOccurred())
				}
				// Expect the reconcileDelete method to finish with no errors.
				g.Expect(r.reconcileDelete(ctx, dpfOperatorConfig)).To(Succeed())
				// Expect the DPFOperatorConfig to have no finalizer.
				g.Expect(dpfOperatorConfig.GetFinalizers()).To(BeEmpty())
			}).WithTimeout(50 * time.Second).Should(Succeed())

		})
	}
}

func objectsInListStillExist(c client.Client, gvks []schema.GroupVersionKind) bool {
	objs := []unstructured.Unstructured{}
	for _, gvk := range gvks {
		list := &unstructured.UnstructuredList{}
		list.SetGroupVersionKind(gvk.GroupVersion().WithKind(gvk.Kind))
		if err := c.List(ctx, list); err != nil {
			panic(err)
		}
		for _, obj := range list.Items {
			// Only add the item to the list if it doesn't have the test finalizer.
			if len(obj.GetFinalizers()) == 1 && obj.GetFinalizers()[0] == testFinalizer {
				continue
			}
			objs = append(objs, obj)
		}
	}
	return len(objs) != 0
}

func generateObjectsByGVK(ns string, gvks []schema.GroupVersionKind) []client.Object {
	objs := []client.Object{}
	for i, gvk := range gvks {
		object := &unstructured.Unstructured{}
		object.SetGroupVersionKind(gvk)
		object.SetNamespace(ns)
		object.SetName(fmt.Sprintf("%s-%d", object.GetName(), i))
		objs = append(objs, object)
	}
	return objs
}
