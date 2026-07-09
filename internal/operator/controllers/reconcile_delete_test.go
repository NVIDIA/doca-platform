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
	"strings"
	"testing"
	"time"

	dpuservicev1 "github.com/nvidia/doca-platform/api/dpuservice/v1alpha1"
	noderesourcesv1 "github.com/nvidia/doca-platform/api/noderesources/v1alpha1"
	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"
	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/internal/operator/inventory"
	"github.com/nvidia/doca-platform/internal/release"
	"github.com/nvidia/doca-platform/pkg/dpucluster"
	argov1 "github.com/nvidia/doca-platform/third_party/forked/argoproj/argo-cd/pkg/apis/application/v1alpha1"

	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
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
	g.Expect(noderesourcesv1.AddToScheme(scheme)).To(Succeed())
	g.Expect(provisioningv1.AddToScheme(scheme)).To(Succeed())
	g.Expect(argov1.AddToScheme(scheme)).To(Succeed())

	tests := []struct {
		name string
		// gvkWithFinalizer is the GVK of the objects that should have a finalizer which stops the flow of the deletion.
		// Note: This object is not counted in the `ObjectsExpected` numbers above.
		gvkWithFinalizer *schema.GroupVersionKind
		// nodeResourceObjsExpected is a boolean describing if resources from this group should exist.
		nodeResourceObjsExpected bool
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
			nodeResourceObjsExpected:  false,
			serviceChainObjsExpected:  false,
			dpuDeploymentObjsExpected: false,
			dpuServiceObjsExpected:    false,
			provisioningObjsExpected:  false,
		},
		{
			name:                      "Ensure DPUService, Provisioning, dpuDeployment is not deleted if ServiceChain has finalizer",
			gvkWithFinalizer:          &dpuservicev1.DPUServiceChainGroupVersionKind,
			nodeResourceObjsExpected:  false,
			serviceChainObjsExpected:  false,
			dpuDeploymentObjsExpected: true,
			dpuServiceObjsExpected:    true,
			provisioningObjsExpected:  true,
		},
		{
			name:                      "Ensure Provisioning, DPUService is not deleted if DPUDeployment has finalizer",
			gvkWithFinalizer:          &dpuservicev1.DPUDeploymentGroupVersionKind,
			nodeResourceObjsExpected:  false,
			serviceChainObjsExpected:  false,
			dpuDeploymentObjsExpected: false,
			dpuServiceObjsExpected:    true,
			provisioningObjsExpected:  true,
		},
		{
			name:                      "Ensure Provisioning is not deleted if DPUService has finalizer",
			gvkWithFinalizer:          &dpuservicev1.DPUServiceGroupVersionKind,
			nodeResourceObjsExpected:  false,
			serviceChainObjsExpected:  false,
			dpuDeploymentObjsExpected: false,
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
					DeploymentMode: operatorv1.DeploymentModeHostTrusted,
					ProvisioningController: &operatorv1.ProvisioningControllerConfiguration{
						BFBPersistentVolumeClaimName: ptr.To("oof"),
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
			objectsToCreate = append(objectsToCreate, generateObjectsByGVK(ns.Name, nodeResourceResources)...)
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

				_, _ = r.reconcileDelete(ctx, dpfOperatorConfig, []*dpucluster.Config{})
				// Expect the DPUDeployment objects to match the expected state.
				g.Expect(objectsInListStillExist(r.Client, dpuDeploymentResources)).To(Equal(tt.dpuDeploymentObjsExpected))
				// Expect the ServiceChain objects to match the expected state.
				g.Expect(objectsInListStillExist(r.Client, serviceChainResources)).To(Equal(tt.serviceChainObjsExpected))
				// Expect the DPUService objects to match the expected state.
				g.Expect(objectsInListStillExist(r.Client, dpuserviceResources)).To(Equal(tt.dpuServiceObjsExpected))
				// Expect the NodeResource objects to match the expected state.
				g.Expect(objectsInListStillExist(r.Client, nodeResourceResources)).To(Equal(tt.nodeResourceObjsExpected))
				// Expect the Provisioning objects to match the expected state.
				g.Expect(objectsInListStillExist(r.Client, provisioningResources)).To(Equal(tt.provisioningObjsExpected))
			}).WithTimeout(5 * time.Second).Should(Succeed())

			// Ensure the state does not change.
			g.Consistently(func(g Gomega) {
				_, _ = r.reconcileDelete(ctx, dpfOperatorConfig, []*dpucluster.Config{})
				// Expect the DPUDeployment list to have the right number of members.
				g.Expect(objectsInListStillExist(r.Client, dpuDeploymentResources)).To(Equal(tt.dpuDeploymentObjsExpected))
				// Expect the ServiceChain list to have the right number of members.
				g.Expect(objectsInListStillExist(r.Client, serviceChainResources)).To(Equal(tt.serviceChainObjsExpected))
				// Expect the DPUService list to have the right number of members.
				g.Expect(objectsInListStillExist(r.Client, dpuserviceResources)).To(Equal(tt.dpuServiceObjsExpected))
				// Expect the NodeResource list to have the right number of members.
				g.Expect(objectsInListStillExist(r.Client, nodeResourceResources)).To(Equal(tt.nodeResourceObjsExpected))
				// Expect the Provisioning list to have the right number of members.
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
				_, err := r.reconcileDelete(ctx, dpfOperatorConfig, []*dpucluster.Config{})
				g.Expect(err).NotTo(HaveOccurred())
				// Expect the DPFOperatorConfig to have no finalizer.
				g.Expect(dpfOperatorConfig.GetFinalizers()).To(BeEmpty())
			}).WithTimeout(50 * time.Second).Should(Succeed())

		})
	}
}

func TestMatchLabels(t *testing.T) {
	tests := []struct {
		name          string
		resources     []unstructured.Unstructured
		exclusionList []string
		expected      []unstructured.Unstructured
	}{
		{
			name: "Match all objects",
			resources: []unstructured.Unstructured{
				{
					Object: map[string]interface{}{
						"kind": dpuservicev1.DPUServiceChainGroupVersionKind,
						"metadata": map[string]interface{}{
							"labels": map[string]interface{}{
								dpuservicev1.ParentDPUDeploymentNameLabel: "some-chain",
							},
						},
					},
				},
				{
					Object: map[string]interface{}{
						"kind": dpuservicev1.DPUServiceGroupVersionKind,
						"metadata": map[string]interface{}{
							"labels": map[string]interface{}{
								dpuservicev1.ParentDPUDeploymentNameLabel: "some-service",
							},
						},
					},
				},
			},
			exclusionList: []string{dpuservicev1.ParentDPUDeploymentNameLabel},
			expected:      []unstructured.Unstructured{},
		},
		{
			name: "No match",
			resources: []unstructured.Unstructured{
				{
					Object: map[string]interface{}{
						"kind": dpuservicev1.DPUServiceGroupVersionKind,
						"metadata": map[string]interface{}{
							"labels": map[string]interface{}{
								"svc.dpu.nvidia.com/component": "dpf-operator",
							},
						},
					},
				},
			},
			exclusionList: []string{dpuservicev1.ParentDPUDeploymentNameLabel},
			expected: []unstructured.Unstructured{
				{
					Object: map[string]interface{}{
						"kind": dpuservicev1.DPUServiceGroupVersionKind,
						"metadata": map[string]interface{}{
							"labels": map[string]interface{}{
								"svc.dpu.nvidia.com/component": "dpf-operator",
							},
						},
					},
				},
			},
		},
		{
			name: "No labels",
			resources: []unstructured.Unstructured{
				{
					Object: map[string]interface{}{
						"kind": dpuservicev1.DPUServiceIPAMGroupVersionKind,
						"metadata": map[string]interface{}{
							"labels": nil,
						},
					},
				},
			},
			exclusionList: []string{dpuservicev1.ParentDPUDeploymentNameLabel},
			expected: []unstructured.Unstructured{
				{
					Object: map[string]interface{}{
						"kind": dpuservicev1.DPUServiceIPAMGroupVersionKind,
						"metadata": map[string]interface{}{
							"labels": nil,
						},
					},
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			res := make([]unstructured.Unstructured, 0)
			for _, obj := range tt.resources {
				if !matchLabelExclusionList(obj.GetLabels(), tt.exclusionList) {
					res = append(res, obj)
				}
				g.Expect(res).To(Equal(tt.expected))
			}
		})
	}
}

func TestDeleteDPUNodeMaintenanceResources(t *testing.T) {
	g := NewWithT(t)
	scheme := runtime.NewScheme()
	g.Expect(provisioningv1.AddToScheme(scheme)).To(Succeed())

	// A DPUNodeMaintenance with the protection finalizer and a non-empty requestor plus an
	// existing DPUNode simulates the case where the DPUNodeMaintenance controller would refuse to
	// release the finalizer, which would otherwise block DPFOperatorConfig teardown indefinitely.
	blocked := &provisioningv1.DPUNodeMaintenance{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "blocked",
			Namespace:  "test-ns",
			Finalizers: []string{provisioningv1.DPUNodeMaintenanceFinalizer},
		},
		Spec: provisioningv1.DPUNodeMaintenanceSpec{
			DPUNodeName: "node-1",
			Requestor:   []string{"some-dpu"},
		},
	}
	// A DPUNodeMaintenance without the protection finalizer should be deleted directly.
	unprotected := &provisioningv1.DPUNodeMaintenance{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "unprotected",
			Namespace: "test-ns",
		},
		Spec: provisioningv1.DPUNodeMaintenanceSpec{DPUNodeName: "node-2"},
	}

	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(blocked, unprotected).Build()

	// The first pass issues the deletes and strips the protection finalizer. It reports the objects
	// as still existing so the caller keeps requeueing, matching the deleteResources contract.
	g.Expect(deleteDPUNodeMaintenanceResources(context.Background(), c)).NotTo(Succeed())

	// After the finalizer is stripped the objects are garbage collected, so a subsequent pass finds
	// an empty list and succeeds.
	list := &provisioningv1.DPUNodeMaintenanceList{}
	g.Expect(c.List(context.Background(), list)).To(Succeed())
	g.Expect(list.Items).To(BeEmpty(), "all DPUNodeMaintenance objects should be force-deleted")

	g.Expect(deleteDPUNodeMaintenanceResources(context.Background(), c)).To(Succeed())
}

func TestDPFOperatorConfigReconciler_deleteSystemComponentViaInventory(t *testing.T) {
	const testNamespace = "test-ns"
	testComponentName := operatorv1.ComponentName("test-component")
	testComponent := inventory.StubComponentWithObjs(testComponentName, nil)

	newScheme := runtime.NewScheme()
	_ = corev1.AddToScheme(newScheme)

	// restMapper provides the GVK → REST mapping needed by getCurrentInventoryObjects.
	// The fake client defaults to an empty mapper so we must supply ConfigMap explicitly.
	restMapper := apimeta.NewDefaultRESTMapper([]schema.GroupVersion{{Group: "", Version: "v1"}})
	restMapper.Add(schema.GroupVersionKind{Group: "", Version: "v1", Kind: "ConfigMap"}, apimeta.RESTScopeNamespace)

	// makeApplySet creates an ApplySet Secret for testComponent in testNamespace with the given inventory annotation value.
	makeApplySet := func(inventoryAnnotation string) *corev1.Secret {
		return &corev1.Secret{
			TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "Secret"},
			ObjectMeta: metav1.ObjectMeta{
				Name:      inventory.ApplySetName(testComponent),
				Namespace: testNamespace,
				Labels: map[string]string{
					inventory.ApplySetParentIDLabel: inventory.ApplySetID(testNamespace, testComponent),
				},
				Annotations: map[string]string{
					inventory.ApplySetToolingAnnotation:      inventory.ApplySetToolingAnnotationValue,
					inventory.ApplySetInventoryAnnotationKey: inventoryAnnotation,
				},
			},
		}
	}

	// configMapGKNN returns the ApplySet GKNN string for a ConfigMap.
	configMapGKNN := func(ns, name string) string {
		return inventory.GroupKindNamespaceName{Kind: "ConfigMap", Group: "", Namespace: ns, Name: name}.String()
	}

	tests := []struct {
		name            string
		objectsToCreate []client.Object
		wantErr         bool
		// checkDeleted verifies the named ConfigMaps were removed from the cluster.
		checkDeleted []types.NamespacedName
		// checkRemaining verifies the named ConfigMaps still exist in the cluster.
		checkRemaining []types.NamespacedName
	}{
		{
			name:    "no applyset exists - no-op, returns nil",
			wantErr: false,
		},
		{
			name: "applyset with multiple objects - all deleted",
			objectsToCreate: []client.Object{
				makeApplySet(strings.Join([]string{
					configMapGKNN(testNamespace, "obj-1"),
					configMapGKNN(testNamespace, "obj-2"),
				}, ",")),
				&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "obj-1", Namespace: testNamespace}},
				&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "obj-2", Namespace: testNamespace}},
			},
			wantErr: false,
			checkDeleted: []types.NamespacedName{
				{Namespace: testNamespace, Name: "obj-1"},
				{Namespace: testNamespace, Name: "obj-2"},
			},
		},
		{
			name: "applyset references objects already absent - returns nil",
			objectsToCreate: []client.Object{
				makeApplySet(configMapGKNN(testNamespace, "obj-1")),
				// obj-1 intentionally not created
			},
			wantErr: false,
		},
		{
			name: "applyset references object with finalizer - returns pending deletion error",
			objectsToCreate: []client.Object{
				makeApplySet(configMapGKNN(testNamespace, "obj-1")),
				&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{
					Name:       "obj-1",
					Namespace:  testNamespace,
					Finalizers: []string{"test-finalizer"},
				}},
			},
			wantErr:        true,
			checkRemaining: []types.NamespacedName{{Namespace: testNamespace, Name: "obj-1"}},
		},
		{
			name: "applyset missing inventory annotation - returns error",
			objectsToCreate: []client.Object{
				&corev1.Secret{
					TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "Secret"},
					ObjectMeta: metav1.ObjectMeta{
						Name:      inventory.ApplySetName(testComponent),
						Namespace: testNamespace,
						Labels: map[string]string{
							inventory.ApplySetParentIDLabel: inventory.ApplySetID(testNamespace, testComponent),
						},
						Annotations: map[string]string{
							inventory.ApplySetToolingAnnotation: inventory.ApplySetToolingAnnotationValue,
							// ApplySetInventoryAnnotationKey deliberately omitted
						},
					},
				},
			},
			wantErr: true,
		},
		{
			name: "applyset references unregistered resource kind - returns error",
			objectsToCreate: []client.Object{
				makeApplySet(inventory.GroupKindNamespaceName{
					Kind:      "UnknownKind",
					Group:     "unknown.group.io",
					Namespace: testNamespace,
					Name:      "obj-1",
				}.String()),
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			c := fake.NewClientBuilder().WithScheme(newScheme).WithRESTMapper(restMapper).WithObjects(tt.objectsToCreate...).Build()
			r := &DPFOperatorConfigReconciler{Client: c}

			vars := inventory.Variables{Namespace: testNamespace}
			err := r.deleteSystemComponentViaInventory(context.Background(), testComponent, vars)

			if tt.wantErr {
				g.Expect(err).To(HaveOccurred())
			} else {
				g.Expect(err).NotTo(HaveOccurred())
			}

			for _, nn := range tt.checkDeleted {
				g.Expect(apierrors.IsNotFound(c.Get(context.Background(), nn, &corev1.ConfigMap{}))).To(BeTrue(),
					"expected %s to be deleted", nn)
			}
			for _, nn := range tt.checkRemaining {
				g.Expect(c.Get(context.Background(), nn, &corev1.ConfigMap{})).To(Succeed(),
					"expected %s to still exist", nn)
			}
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
