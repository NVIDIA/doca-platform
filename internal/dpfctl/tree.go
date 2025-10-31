/*
Copyright 2025 NVIDIA

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

package dpfctl

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ObjectTreeOptions defines the options for an ObjectTree.
type ObjectTreeOptions struct {
	// ShowResources is a list of comma separated kind or kind/name that should be shown in the output.
	ShowResources string

	// ShowChildResources can be set to true if all child resources should be shown.
	ShowChildResources bool

	// ShowOtherConditions is a list of comma separated kind or kind/name for which we should add the ShowObjectConditionsAnnotation
	// to signal to the presentation layer to show all the conditions for the objects.
	ShowOtherConditions string

	// ExpandResources is a list of comma separated kind or kind/name that should be expanded in the output.
	ExpandResources string

	// Grouping groups sibling object in case the ready conditions
	// have the same Status, Severity and Reason
	Grouping bool

	// Colors enables color output
	// Default is true
	Colors bool

	// Output is the output format
	// Default is "table"
	Output string

	// ShowStorage enables showing storage resources (DPUVolume, DPUVolumeAttachment, DPUStoragePolicy, DPUStorageVendor)
	// Default is false
	ShowStorage bool
}

// ObjectTree defines an object tree representing the status of DPF resources.
// Copy from: https://github.com/kubernetes-sigs/cluster-api/blob/release-1.9/cmd/clusterctl/client/tree/tree.go#L65
type ObjectTree struct {
	root       client.Object
	options    ObjectTreeOptions
	items      map[types.UID]client.Object
	ownership  map[types.UID]map[types.UID]bool
	parentship map[types.UID]types.UID
}

// NewObjectTree creates a new object tree with the given root and options.
// Copy from: https://github.com/kubernetes-sigs/cluster-api/blob/release-1.9/cmd/clusterctl/client/tree/tree.go#L73
func NewObjectTree(root client.Object, options ObjectTreeOptions) *ObjectTree {
	// If it is requested to show all the conditions for the root, add
	// the ShowObjectConditionsAnnotation to signal this to the presentation layer.
	if isObjDebug(root, options.ShowOtherConditions) {
		addAnnotation(root, ShowObjectConditionsAnnotation, "True")
	}

	return &ObjectTree{
		root:       root,
		options:    options,
		items:      make(map[types.UID]client.Object),
		ownership:  make(map[types.UID]map[types.UID]bool),
		parentship: make(map[types.UID]types.UID),
	}
}

func (od ObjectTree) AddMultipleWithHeader(parent client.Object, objs []client.Object, headerName string, opts ...AddObjectOption) {
	// Return early if there are no objects to add. This prevents the creation of a virtual object with no children.
	if len(objs) == 0 {
		return
	}

	headerName = ensurePlural(headerName)
	virtualObj := VirtualObject("", headerName, headerName)
	od.Add(parent, virtualObj, opts...)
	for _, obj := range objs {
		od.Add(virtualObj, obj, opts...)
	}
}

// hasNotReadyChildren checks if an object has any children that are not ready.
// This is used to prevent grouping of nodes that have failing or in-progress children.
func (od ObjectTree) hasNotReadyChildren(obj client.Object) bool {
	children := od.GetObjectsByParent(obj.GetUID())
	for _, child := range children {
		childReady := getReadyCondition(child)

		// If child has no Ready condition, check if it has ANY non-True conditions
		// (e.g., DPUNodeMaintenance with requestor conditions showing False)
		if childReady == nil {
			// Check if the child has any failed conditions
			if hasFailedConditions(child) {
				return true
			}

			// If it has no failed conditions, it might be a virtual/organizational object
			// Recursively check its children instead
			if !hasFailedConditions(child) && len(GetConditions(child)) == 0 {
				if od.hasNotReadyChildren(child) {
					return true
				}
			}
			continue
		}

		// If child has a Ready condition but it's not True, consider it not ready
		if childReady.Status != metav1.ConditionTrue {
			return true
		}
	}
	return false
}

// Add a object to the object tree.
// Adoption from: https://github.com/kubernetes-sigs/cluster-api/blob/release-1.9/cmd/clusterctl/client/tree/tree.go#L89
func (od ObjectTree) Add(parent, obj client.Object, opts ...AddObjectOption) (added bool, visible bool) {
	if parent == nil || obj == nil {
		return false, false
	}
	addOpts := &addObjectOptions{}
	addOpts.ApplyOptions(opts)

	objReady := getReadyCondition(obj)

	// If it is requested to show all the conditions for the object, add
	// the ShowObjectConditionsAnnotation to signal this to the presentation layer.
	if isObjDebug(obj, od.options.ShowOtherConditions) {
		addAnnotation(obj, ShowObjectConditionsAnnotation, "True")
	}

	// If it is requested to use a meta name for the object in the presentation layer, add
	// the ObjectMetaNameAnnotation to signal this to the presentation layer.
	if addOpts.MetaName != "" {
		addAnnotation(obj, ObjectMetaNameAnnotation, addOpts.MetaName)
	}

	// Add the ObjectZOrderAnnotation to signal this to the presentation layer.
	addAnnotation(obj, ObjectZOrderAnnotation, strconv.Itoa(addOpts.ZOrder))

	// If it is requested that this object and its sibling should be grouped in case the ready condition
	// has the same Status and Reason, process all the sibling nodes that are Ready and group them. All
	// other siblings that are not Ready will be printed ungrouped.
	// Also don't group if this object has children that aren't ready.
	if IsGroupingObject(parent) && objReady != nil && objReady.Status == metav1.ConditionTrue && !od.hasNotReadyChildren(obj) {
		siblings := od.GetObjectsByParent(parent.GetUID())

		// The loop below will process the next node and decide if it belongs in a group. Since objects in the same group
		// must have the same Kind, we sort by Kind so objects of the same Kind will be together in the list.
		sort.Slice(siblings, func(i, j int) bool {
			return siblings[i].GetObjectKind().GroupVersionKind().Kind < siblings[j].GetObjectKind().GroupVersionKind().Kind
		})

		for i := range siblings {
			s := siblings[i]
			sReady := getReadyCondition(s)

			// If the object's ready condition has a different Status, Severity and Reason than the sibling object,
			// move on (they should not be grouped).
			if !hasSameStatusAndReason(objReady, sReady) {
				continue
			}

			// Don't group if the sibling has children that aren't ready (e.g., DPUNodeMaintenances in progress)
			// This ensures that nodes with active maintenance or failing children are shown individually
			if od.hasNotReadyChildren(s) {
				continue
			}

			// If the sibling node is already a group object
			if IsGroupObject(s) {
				// Check to see if the group object kind matches the object, i.e. group is DPUGroup and object is DPU.
				// If so, upgrade it with the current object.
				if s.GetObjectKind().GroupVersionKind().Kind == obj.GetObjectKind().GroupVersionKind().Kind+"Group" {
					updateGroupNode(s, sReady, obj, objReady)
					return true, false
				}
			} else if s.GetObjectKind().GroupVersionKind().Kind != obj.GetObjectKind().GroupVersionKind().Kind {
				// If the sibling is not a group object, check if the sibling and the object are of the same kind. If not, move on.
				continue
			}

			// Otherwise the object and the current sibling should be merged in a group.

			// Create virtual object for the group and add it to the object tree.
			groupNode := createGroupNode(s, sReady, obj, objReady)
			// By default, grouping objects should be sorted last.
			addAnnotation(groupNode, ObjectZOrderAnnotation, strconv.Itoa(GetZOrder(obj)))

			od.addInner(parent, groupNode)

			// Remove the current sibling (now merged in the group).
			od.remove(parent, s)
			return true, false
		}
	}

	// If it is requested that the child of this node should be grouped in case the ready condition
	// has the same Status, Severity and Reason, add the GroupingObjectAnnotation to signal
	// this to the presentation layer.
	if addOpts.GroupingObject && od.options.Grouping {
		addAnnotation(obj, GroupingObjectAnnotation, "True")
	}

	// Add the object to the object tree.
	od.addInner(parent, obj)

	return true, true
}

// Copy from: https://github.com/kubernetes-sigs/cluster-api/blob/release-1.9/cmd/clusterctl/client/tree/tree.go#L231
func (od ObjectTree) remove(parent client.Object, s client.Object) {
	for _, child := range od.GetObjectsByParent(s.GetUID()) {
		od.remove(s, child)
	}
	delete(od.items, s.GetUID())
	delete(od.ownership[parent.GetUID()], s.GetUID())
	delete(od.parentship, s.GetUID())
}

// Copy from: https://github.com/kubernetes-sigs/cluster-api/blob/release-1.9/cmd/clusterctl/client/tree/tree.go#L239
func (od ObjectTree) addInner(parent client.Object, obj client.Object) {
	od.items[obj.GetUID()] = obj
	if od.ownership[parent.GetUID()] == nil {
		od.ownership[parent.GetUID()] = make(map[types.UID]bool)
	}
	od.ownership[parent.GetUID()][obj.GetUID()] = true
	od.parentship[obj.GetUID()] = parent.GetUID()
}

// GetRoot returns the root of the tree.
// Copy from: https://github.com/kubernetes-sigs/cluster-api/blob/release-1.9/cmd/clusterctl/client/tree/tree.go#L248
func (od ObjectTree) GetRoot() client.Object { return od.root }

// GetObject returns the object with the given uid.
// Copy from: https://github.com/kubernetes-sigs/cluster-api/blob/release-1.9/cmd/clusterctl/client/tree/tree.go#L250
func (od ObjectTree) GetObject(id types.UID) client.Object { return od.items[id] }

// IsObjectWithChild determines if an object has dependants.
// Copy from: https://github.com/kubernetes-sigs/cluster-api/blob/release-1.9/cmd/clusterctl/client/tree/tree.go#L253
func (od ObjectTree) IsObjectWithChild(id types.UID) bool {
	return len(od.ownership[id]) > 0
}

// GetObjectsByParent returns all the dependant objects for the given uid.
// Copy from: https://github.com/kubernetes-sigs/cluster-api/blob/release-1.9/cmd/clusterctl/client/tree/tree.go#L259
func (od ObjectTree) GetObjectsByParent(id types.UID) []client.Object {
	out := make([]client.Object, 0, len(od.ownership[id]))
	for k := range od.ownership[id] {
		out = append(out, od.GetObject(k))
	}
	return out
}

// GetParent returns parent of an object.
func (od ObjectTree) GetParent(id types.UID) client.Object {
	parentID, ok := od.parentship[id]
	if !ok {
		return nil
	}
	if parentID == od.root.GetUID() {
		return od.root
	}
	return od.items[parentID]
}

// Copy from: https://github.com/kubernetes-sigs/cluster-api/blob/release-1.9/cmd/clusterctl/client/tree/tree.go#L280
func hasSameStatusAndReason(a, b *metav1.Condition) bool {
	if ((a == nil) != (b == nil)) || ((a != nil && b != nil) && (a.Status != b.Status || a.Reason != b.Reason)) {
		return false
	}
	return true
}

// Adoption from: https://github.com/kubernetes-sigs/cluster-api/blob/release-1.9/cmd/clusterctl/client/tree/tree.go#L380
func createGroupNode(sibling client.Object, siblingReady *metav1.Condition, obj client.Object, objReady *metav1.Condition) *unstructured.Unstructured {
	kind := fmt.Sprintf("%sGroup", obj.GetObjectKind().GroupVersionKind().Kind)

	// Create a new group node and add the GroupObjectAnnotation to signal
	// this to the presentation layer.
	// NB. The group nodes gets a unique ID to avoid conflicts.
	groupNode := VirtualObject(obj.GetNamespace(), kind, readyStatusAndReasonUID(obj))
	addAnnotation(groupNode, GroupObjectAnnotation, "True")

	// Update the list of items included in the group and store it in the GroupItemsAnnotation.
	items := []string{obj.GetName(), sibling.GetName()}
	sort.Strings(items)
	addAnnotation(groupNode, GroupItemsAnnotation, strings.Join(items, GroupItemsSeparator))

	// Update the group's ready condition.
	if objReady != nil {
		objReady.LastTransitionTime = minLastTransitionTime(objReady, siblingReady)
		objReady.Message = ""
		setReadyCondition(groupNode, objReady)
	}
	return groupNode
}

// Adoption from: https://github.com/kubernetes-sigs/cluster-api/blob/release-1.9/cmd/clusterctl/client/tree/tree.go#L403
func readyStatusAndReasonUID(obj client.Object) string {
	ready := getReadyCondition(obj)
	if ready == nil {
		return fmt.Sprintf("zzz_%s", randomString(6))
	}
	return fmt.Sprintf("zz_%s_%s_%s", ready.Status, ready.Reason, randomString(6))
}

// Adoption from: https://github.com/kubernetes-sigs/cluster-api/blob/release-1.9/cmd/clusterctl/client/tree/tree.go#L411
func minLastTransitionTime(a, b *metav1.Condition) metav1.Time {
	if a == nil && b == nil {
		return metav1.Time{}
	}
	if (a != nil) && (b == nil) {
		return a.LastTransitionTime
	}
	if a == nil {
		return b.LastTransitionTime
	}
	if a.LastTransitionTime.Time.After(b.LastTransitionTime.Time) {
		return b.LastTransitionTime
	}
	return a.LastTransitionTime
}

// Adoption from: https://github.com/kubernetes-sigs/cluster-api/blob/release-1.9/cmd/clusterctl/client/tree/tree.go#L463
func updateGroupNode(groupObj client.Object, groupReady *metav1.Condition, obj client.Object, objReady *metav1.Condition) {
	// Update the list of items included in the group and store it in the GroupItemsAnnotation.
	items := strings.Split(GetGroupItems(groupObj), GroupItemsSeparator)
	items = append(items, obj.GetName())
	sort.Strings(items)
	addAnnotation(groupObj, GroupItemsAnnotation, strings.Join(items, GroupItemsSeparator))

	// Update the group's ready condition.
	if groupReady != nil {
		groupReady.LastTransitionTime = minLastTransitionTime(objReady, groupReady)
		groupReady.Message = ""
		setReadyCondition(groupObj, groupReady)
	}
}

// Copy from: https://github.com/kubernetes-sigs/cluster-api/blob/release-1.9/cmd/clusterctl/client/tree/tree.go#L478
func isObjDebug(obj client.Object, debugFilter string) bool {
	if debugFilter == "" {
		return false
	}

	kind := obj.GetObjectKind().GroupVersionKind().Kind
	for _, filter := range strings.Split(strings.ToLower(debugFilter), ",") {
		filter = strings.TrimSpace(filter)
		if filter == "" {
			continue
		}
		if strings.EqualFold(filter, all) {
			return true
		}
		kn := strings.Split(filter, "/")
		if len(kn) == 2 {
			if strings.EqualFold(ensurePlural(kind), ensurePlural(kn[0])) && obj.GetName() == kn[1] {
				return true
			}
			continue
		}
		if strings.EqualFold(ensurePlural(kind), ensurePlural(kn[0])) {
			return true
		}
	}
	return false
}
