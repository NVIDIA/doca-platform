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
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/nvidia/doca-platform/pkg/conditions"

	"github.com/fatih/color"
	"github.com/olekukonko/tablewriter"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/duration"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"
)

// Copied from: https://github.com/kubernetes-sigs/cluster-api/blob/release-1.9/cmd/clusterctl/cmd/describe_cluster.go#L48
const (
	firstElemPrefix = `├─`
	lastElemPrefix  = `└─`
	indent          = "  "
	pipe            = `│ `

	// lastObjectAnnotation defines the last object in the ObjectTree.
	// This is necessary to built the prefix for multiline condition messages.
	lastObjectAnnotation = "tree.cluster.x-k8s.io.io/last-object"
)

// Copied from: https://github.com/kubernetes-sigs/cluster-api/blob/release-1.9/cmd/clusterctl/cmd/describe_cluster.go#L55
var (
	gray   = color.New(color.FgHiBlack)
	red    = color.New(color.FgRed)
	green  = color.New(color.FgGreen)
	yellow = color.New(color.FgYellow)
	white  = color.New(color.FgWhite)
	cyan   = color.New(color.FgCyan)

	// setZeroConditionAge is a helper for the unit tests to set the LastTransitionTime of the conditions to time.Now().
	setZeroConditionAge bool
)

// PrintObjectTree prints the DPF status to stdout.
// Adopted from: https://github.com/kubernetes-sigs/cluster-api/blob/release-1.9/cmd/clusterctl/cmd/describe_cluster.go#L213
func PrintObjectTree(tree *ObjectTree) error {
	switch tree.options.Output {
	case "json", "yaml":
		return printObjectTreeFormatted(tree, tree.options.Output)
	case "table", "":
		printObjectTreeTable(tree)
	default:
		return fmt.Errorf("unsupported output format %s", tree.options.Output)
	}
	return nil
}

// printObjectTreeTable prints the ObjectTree in a table format.
func printObjectTreeTable(tree *ObjectTree) {
	// Creates the output table
	tbl := tablewriter.NewWriter(os.Stdout)
	tbl.SetHeader([]string{"NAME", "NAMESPACE", "STATUS", "REASON", "SINCE", "MESSAGE"})

	formatTableTree(tbl)
	// Add row for the root object, the DPFOperatorConfig, and recursively for all the resources representing the DPF status.
	addObjectRow("", tbl, tree, tree.GetRoot())

	// Prints the output table
	tbl.Render()
}

type jsonOutput struct {
	ObjectMeta metav1.ObjectMeta  `json:"metadata"`
	TypeMeta   metav1.TypeMeta    `json:"typeMeta"`
	Conditions []metav1.Condition `json:"conditions"`
}

// printObjectTreeFormatted prints the ObjectTree in JSON format. The Spec is removed, only NamespacedName and Conditions are printed.
func printObjectTreeFormatted(tree *ObjectTree, format string) error {
	j := make(map[string][]jsonOutput)

	rootKind := ensurePlural(tree.GetRoot().GetObjectKind().GroupVersionKind().Kind)
	j[rootKind] = []jsonOutput{jsonOutputObject(tree.GetRoot())}

	for _, item := range tree.items {
		if IsVirtualObject(item) {
			continue
		}
		kindName := ensurePlural(strings.ToLower(item.GetObjectKind().GroupVersionKind().Kind))
		j[kindName] = append(j[kindName], jsonOutputObject(item))
	}

	var output []byte
	var err error

	switch format {
	case "json":
		output, err = json.MarshalIndent(j, "", "  ")
		if err != nil {
			return err
		}
	case "yaml":
		output, err = yaml.Marshal(j)
		if err != nil {
			return err
		}
	default:
		return fmt.Errorf("unsupported format %s", format)
	}
	fmt.Println(string(output))

	return nil
}

func jsonOutputObject(item client.Object) jsonOutput {
	getter := objToGetSet(item)
	conds := []metav1.Condition{}
	if getter != nil {
		conds = getter.GetConditions()
	}

	// Delete the zOrder annotation from the output.
	// This annotation is only used for ordering the objects in the tree view.
	annotations := item.GetAnnotations()
	delete(annotations, ObjectZOrderAnnotation)

	return jsonOutput{
		ObjectMeta: metav1.ObjectMeta{
			Annotations:       annotations,
			CreationTimestamp: item.GetCreationTimestamp(),
			DeletionTimestamp: item.GetDeletionTimestamp(),
			Finalizers:        item.GetFinalizers(),
			Generation:        item.GetGeneration(),
			Labels:            item.GetLabels(),
			Name:              item.GetName(),
			Namespace:         item.GetNamespace(),
			OwnerReferences:   item.GetOwnerReferences(),
			ResourceVersion:   item.GetResourceVersion(),
			UID:               item.GetUID(),
		},
		TypeMeta: metav1.TypeMeta{
			APIVersion: item.GetObjectKind().GroupVersionKind().GroupVersion().String(),
			Kind:       item.GetObjectKind().GroupVersionKind().Kind,
		},
		Conditions: conds,
	}
}

// formats the table with required attributes.
// Copy from: https://github.com/kubernetes-sigs/cluster-api/blob/release-1.9/cmd/clusterctl/cmd/describe_cluster.go#L241
func formatTableTree(tbl *tablewriter.Table) {
	tbl.SetAutoWrapText(false)
	tbl.SetHeaderAlignment(tablewriter.ALIGN_LEFT)
	tbl.SetAlignment(tablewriter.ALIGN_LEFT)

	tbl.SetCenterSeparator("")
	tbl.SetColumnSeparator("")
	tbl.SetRowSeparator("")

	tbl.SetHeaderLine(false)
	tbl.SetBorder(false)
	tbl.SetTablePadding("  ")
	tbl.SetNoWhiteSpace(true)
}

// addConditions adds a row for each object condition except the ready condition,
// which is already represented on the object's main row.
// Adopted from:https://github.com/kubernetes-sigs/cluster-api/blob/release-1.9/cmd/clusterctl/cmd/describe_cluster.go#L470
func addConditions(prefix string, tbl *tablewriter.Table, objectTree *ObjectTree, obj client.Object) {
	// Add a row for each other condition, taking care of updating the tree view prefix.
	// In this case the tree prefix get a filler, to indent conditions from objects, and eventually
	// an additional pipe if the object has children that should be presented after the conditions.
	filler := strings.Repeat(" ", 10)
	childrenPipe := indent
	if objectTree.IsObjectWithChild(obj.GetUID()) {
		childrenPipe = pipe
	}

	otherConditions := GetConditions(obj)
	// Filter conditions based on whether we should show all conditions or just Ready and failed ones
	filteredConditions := []*metav1.Condition{}
	if objectTree.options.ShowOtherConditions == all {
		// Show all conditions when --show-conditions=all
		filteredConditions = otherConditions
	} else {
		// Otherwise only show Ready and failed conditions
		for _, c := range otherConditions {
			if c.Type == string(conditions.TypeReady) || c.Status != metav1.ConditionTrue {
				filteredConditions = append(filteredConditions, c)
			}
		}
	}

	for i := range filteredConditions {
		otherCondition := filteredConditions[i]
		otherDescriptor := newConditionDescriptor(otherCondition, false)
		childPrefix := getChildPrefix(prefix+childrenPipe+filler, i, len(filteredConditions))
		msg := formatParagraph(otherDescriptor.message, 100)

		msg0 := ""
		if len(msg) >= 1 {
			msg0 = msg[0]
		}

		tbl.Append([]string{
			fmt.Sprintf("%s%s", gray.Sprint(childPrefix), cyan.Sprint(otherCondition.Type)),
			"",
			otherDescriptor.readyColor.Sprint(otherDescriptor.status),
			otherDescriptor.readyColor.Sprint(otherDescriptor.reason),
			otherDescriptor.age,
			msg0,
		})
		for _, m := range msg[1:] {
			tbl.Append([]string{
				gray.Sprint(getMultilineConditionPrefix(childPrefix)),
				"",
				"",
				"",
				"",
				m,
			})
		}
	}
}

// formatParagraph takes a strings and splits it into n lines of maxWidth length.
// If the string contains line breaks, those are preserved.
// Adopted from: https://github.com/kubernetes-sigs/cluster-api/blob/release-1.9/cmd/clusterctl/cmd/describe_cluster.go#L772
func formatParagraph(text string, maxWidth int) []string {
	re := regexp.MustCompile(`[\s]+`)
	lines := []string{}
	for _, l := range strings.Split(text, "\n") {
		tmp := ""
		for _, c := range l {
			if c == ' ' {
				tmp += " "
				continue
			}
			break
		}
		indent := tmp
		if strings.HasPrefix(strings.TrimSpace(l), "* ") {
			indent += "  "
		}
		for _, w := range re.Split(l, -1) {
			if len(tmp)+len(w) < maxWidth {
				if strings.TrimSpace(tmp) != "" {
					tmp += " "
				}
				tmp += w
				continue
			}
			lines = append(lines, tmp)
			tmp = indent + w
		}
		lines = append(lines, tmp)
	}
	return lines
}

// getMultilineConditionPrefix return the tree view prefix for a multiline condition.
// Copy from: https://github.com/kubernetes-sigs/cluster-api/blob/release-1.9/cmd/clusterctl/cmd/describe_cluster.go#L513-L525
func getMultilineConditionPrefix(currentPrefix string) string {
	// All ├─ should be replaced by |, so all the existing hierarchic dependencies are carried on
	if strings.HasSuffix(currentPrefix, firstElemPrefix) {
		return strings.TrimSuffix(currentPrefix, firstElemPrefix) + pipe
	}
	// All └─ should be replaced by " " because we are under the last element of the tree (nothing to carry on)
	if strings.HasSuffix(currentPrefix, lastElemPrefix) {
		return strings.TrimSuffix(currentPrefix, lastElemPrefix)
	}
	return "?"
}

// getRootMultiLineObjectPrefix generates the multiline prefix for an object.
func getRootMultiLineObjectPrefix(obj client.Object, objectTree *ObjectTree) string {
	// If the object is the last one in the tree and has no conditions, no prefix is needed.
	if ensureLastObjectInTree(objectTree) == string(obj.GetUID()) && !HasConditions(obj) {
		return ""
	}

	// Determine the prefix for the current object.
	// If it is a leaf we don't have to add any prefix.
	prefix := indent
	if len(objectTree.GetObjectsByParent(obj.GetUID())) > 0 {
		prefix = pipe
	}

	// If the object has conditions, we have to add a filler with a pipe to the prefix.
	if HasConditions(obj) {
		filler := strings.Repeat(" ", 10)
		prefix += filler + pipe
	}

	// Traverse upward through the tree, calculating each parent's prefix.
	// The parent of the root object is nil, so we stop when we reach that point.
	previousUID := obj.GetUID()
	parent := objectTree.GetParent(obj.GetUID())
	for parent != nil {
		// We have to break the loop if the previous ID is the same as the current ID.
		// This should never happen as the root object has not set the parentship.
		if previousUID == parent.GetUID() {
			break
		}

		// Use pipe if the parent has children and the current node is not the last child.
		parentChildren := orderChildrenObjects(objectTree.GetObjectsByParent(parent.GetUID()))
		isLastChild := len(parentChildren) > 0 && parentChildren[len(parentChildren)-1].GetUID() == previousUID
		if objectTree.IsObjectWithChild(parent.GetUID()) && !isLastChild {
			prefix = pipe + prefix
		} else {
			prefix = indent + prefix
		}

		previousUID = parent.GetUID()
		parent = objectTree.GetParent(parent.GetUID())
	}
	return prefix
}

func ensureLastObjectInTree(objectTree *ObjectTree) string {
	// Compute last object in the tree and set it in the annotations.
	annotations := objectTree.GetRoot().GetAnnotations()
	if annotations == nil {
		annotations = map[string]string{}
	}

	// Return if last object is already set.
	if val, ok := annotations[lastObjectAnnotation]; ok {
		return val
	}

	lastObjectInTree := string(getLastObjectInTree(objectTree).GetUID())
	annotations[lastObjectAnnotation] = lastObjectInTree
	objectTree.GetRoot().SetAnnotations(annotations)
	return lastObjectInTree
}

func getLastObjectInTree(objectTree *ObjectTree) client.Object {
	var objs []client.Object

	var traverse func(obj client.Object)
	traverse = func(obj client.Object) {
		objs = append(objs, obj)
		children := orderChildrenObjects(objectTree.GetObjectsByParent(obj.GetUID()))
		for _, child := range children {
			traverse(child)
		}
	}

	traverse(objectTree.GetRoot())
	return objs[len(objs)-1]
}

// addObjectRow add a row for a given object, and recursively for all the object's children.
// NOTE: each row name gets a prefix, that generates a tree view like representation.
// Adopted from: https://github.com/kubernetes-sigs/cluster-api/blob/release-1.9/cmd/clusterctl/cmd/describe_cluster.go#L347
func addObjectRow(prefix string, tbl *tablewriter.Table, objectTree *ObjectTree, obj client.Object) {
	// Gets the descriptor for the object's ready condition, if any.
	readyDescriptor := conditionDescriptor{readyColor: gray}
	if ready := getReadyCondition(obj); ready != nil {
		readyDescriptor = newConditionDescriptor(ready, objectTree.options.Colors)
	}

	// If the object is a group object, override the condition message with the list of objects in the group. e.g dpu-1, dpu-2, ...
	if IsGroupObject(obj) {
		items := strings.Split(GetGroupItems(obj), GroupItemsSeparator)
		readyDescriptor.message = fmt.Sprintf("See %s", strings.Join(items, GroupItemsSeparator))
	}

	// If the object should show conditions or has failed conditions, we remove the Ready condition and set the message to empty.
	// The Ready condition will be printed in the conditions section.
	if IsShowConditionsObject(obj) || hasFailedConditions(obj) {
		readyDescriptor.message = ""
		readyDescriptor.reason = ""
		readyDescriptor.status = ""
		readyDescriptor.age = ""
	} else {
		if readyDescriptor.status != "" {
			readyDescriptor.status = fmt.Sprintf("%s: %s", conditions.TypeReady, readyDescriptor.status)
		}
	}

	// Add the row representing the object that includes
	// - The row name with the tree view prefix.
	// - Replica counters
	// - The object's Available, Ready, UpToDate conditions
	// - The condition picked in the rowDescriptor.
	// Note: if the condition has a multiline message, also add additional rows for each line.
	name := getRowName(obj)
	msg := formatParagraph(readyDescriptor.message, 100)
	msg0 := ""
	if len(msg) >= 1 {
		msg0 = msg[0]
	}
	tbl.Append([]string{
		fmt.Sprintf("%s%s", gray.Sprint(prefix), name),
		obj.GetNamespace(),
		readyDescriptor.readyColor.Sprint(readyDescriptor.status),
		readyDescriptor.readyColor.Sprint(readyDescriptor.reason),
		readyDescriptor.age,
		msg0,
	})

	multilinePrefix := getRootMultiLineObjectPrefix(obj, objectTree)
	for _, m := range msg[1:] {
		tbl.Append([]string{
			gray.Sprint(multilinePrefix),
			"",
			"",
			"",
			"",
			m,
		})
	}

	// If it is required to show all the conditions for the object or has failed conditions, add a row for each object's conditions.
	if IsShowConditionsObject(obj) || hasFailedConditions(obj) {
		addConditions(prefix, tbl, objectTree, obj)
	}

	// Add a row for each object's children, taking care of updating the tree view prefix.
	childrenObj := objectTree.GetObjectsByParent(obj.GetUID())
	childrenObj = orderChildrenObjects(childrenObj)

	for i, child := range childrenObj {
		addObjectRow(getChildPrefix(prefix, i, len(childrenObj)), tbl, objectTree, child)
	}
}

func orderChildrenObjects(childrenObj []client.Object) []client.Object {
	// printBefore returns true if children[i] should be printed before children[j]. Objects are sorted by z-order and
	// row name such that objects with higher z-order are printed first, and objects with the same z-order are
	// printed in alphabetical order.
	printBefore := func(i, j int) bool {
		if GetZOrder(childrenObj[i]) == GetZOrder(childrenObj[j]) {
			return getRowName(childrenObj[i]) < getRowName(childrenObj[j])
		}

		return GetZOrder(childrenObj[i]) > GetZOrder(childrenObj[j])
	}
	sort.Slice(childrenObj, printBefore)
	return childrenObj
}

// getChildPrefix return the tree view prefix for a row representing a child object.
// Copy from:https://github.com/kubernetes-sigs/cluster-api/blob/release-1.9/cmd/clusterctl/cmd/describe_cluster.go#L496
func getChildPrefix(currentPrefix string, childIndex, childCount int) string {
	nextPrefix := currentPrefix

	// Alter the current prefix for hosting the next child object:

	// All ├─ should be replaced by |, so all the existing hierarchic dependencies are carried on
	nextPrefix = strings.ReplaceAll(nextPrefix, firstElemPrefix, pipe)
	// All └─ should be replaced by " " because we are under the last element of the tree (nothing to carry on)
	nextPrefix = strings.ReplaceAll(nextPrefix, lastElemPrefix, strings.Repeat(" ", len([]rune(lastElemPrefix))))

	// Add the prefix for the new child object (├─ for the firsts children, └─ for the last children).
	if childIndex < childCount-1 {
		return nextPrefix + firstElemPrefix
	}
	return nextPrefix + lastElemPrefix
}

// getRowName returns the object name in the tree view according to following rules:
// - group objects are represented as objects kind, e.g. 3 DPUs...
// - other virtual objects are represented using the object name, e.g. Workers, or meta name if provided.
// - objects with a meta name are represented as meta name - (kind/name), e.g. DPUApplication - Application/flannel
// - other objects are represented as kind/name, e.g. DPU/worker1-0000-08-00
// - if the object is being deleted, a prefix will be added.
// Adopted from:https://github.com/kubernetes-sigs/cluster-api/blob/release-1.9/cmd/clusterctl/cmd/describe_cluster.go#L533
func getRowName(obj client.Object) string {
	if IsGroupObject(obj) {
		items := strings.Split(GetGroupItems(obj), GroupItemsSeparator)
		kind := strings.TrimSuffix(obj.GetObjectKind().GroupVersionKind().Kind, "Group")
		kind = ensurePlural(kind)
		return white.Add(color.Bold).Sprintf("%d %s...", len(items), kind)
	}

	if IsVirtualObject(obj) {
		if metaName := GetMetaName(obj); metaName != "" {
			return metaName
		}
		return obj.GetName()
	}

	objName := fmt.Sprintf("%s/%s",
		obj.GetObjectKind().GroupVersionKind().Kind,
		color.New(color.Bold).Sprint(obj.GetName()))

	name := objName
	if objectPrefix := GetMetaName(obj); objectPrefix != "" {
		name = fmt.Sprintf("%s - %s", objectPrefix, gray.Sprintf("%s", name))
	}

	if !obj.GetDeletionTimestamp().IsZero() {
		name = fmt.Sprintf("%s %s", red.Sprintf("!! IN DELETION !!"), name)
	}

	return name
}

// conditionDescriptor contains all the info for representing a condition.
// Copy from:https://github.com/kubernetes-sigs/cluster-api/blob/release-1.9/cmd/clusterctl/cmd/describe_cluster.go#L804
type conditionDescriptor struct {
	readyColor *color.Color
	age        string
	status     string
	reason     string
	message    string
}

// newConditionDescriptor returns a conditionDescriptor for the given condition.
// Adopted from:https://github.com/kubernetes-sigs/cluster-api/blob/release-1.9/cmd/clusterctl/cmd/describe_cluster.go#L813
func newConditionDescriptor(c *metav1.Condition, colors bool) conditionDescriptor {
	v := conditionDescriptor{}

	v.status = string(c.Status)
	v.reason = c.Reason
	v.message = c.Message

	// Compute the condition age.
	v.age = duration.HumanDuration(time.Since(c.LastTransitionTime.Time))
	if setZeroConditionAge {
		// If setZeroConditionAge is true, we set the age to 0s.
		// This is used for unit tests to ensure that the age is always zero.
		v.age = "0s"
	}

	// Determine the color to be used for showing the conditions according to Status and Severity in case Status is false.
	switch c.Status {
	case metav1.ConditionTrue:
		v.readyColor = green
	case metav1.ConditionFalse, metav1.ConditionUnknown:
		v.readyColor = white
		if strings.HasSuffix(c.Type, "Ready") {
			v.readyColor = red
		}
		if strings.HasSuffix(c.Type, "Reconciled") {
			v.readyColor = yellow
		}
	default:
		v.readyColor = gray
	}

	// We have to enable the color explicitly to make it work in the shell.
	if colors {
		v.readyColor.EnableColor()
	}

	return v
}

func showResource(opts ObjectTreeOptions, objKind string) bool {
	if opts.ShowResources == "" || opts.ShowResources == all || opts.ShowChildResources {
		return true
	}

	kinds := strings.Split(opts.ShowResources, ",")
	for _, kind := range kinds {
		testKind := kind
		kn := strings.Split(kind, "/")
		if len(kn) == 2 {
			testKind = kn[0]
		}
		if strings.EqualFold(ensurePlural(objKind), ensurePlural(testKind)) {
			return true
		}
	}
	return false
}

func isWorkloadKind(kind string) bool {
	validKinds := map[string]struct{}{
		"Deployment":  {},
		"StatefulSet": {},
		"DaemonSet":   {},
		"Job":         {},
		"CronJob":     {},
		"ReplicaSet":  {},
	}
	_, exists := validKinds[kind]
	return exists
}

// VirtualObjectForVisualization creates a virtual representation of an object to enable visualization.
// This is primarily used for objects like Argo Applications and BFBs that lack metav1.Conditions.
// The virtual object mimics the original, but strips specific annotations and optionally carries a deletion timestamp
// to reflect its lifecycle state in dpfctl.
func VirtualObjectForVisualization(obj client.Object, kind string) *unstructured.Unstructured {
	virtObj := VirtualObject(obj.GetNamespace(), kind, obj.GetName())
	// We have to remove the VirtualObjectAnnotation from the object to visualize it in dpfctl correctly.
	virtObj.SetAnnotations(nil)
	if !obj.GetDeletionTimestamp().IsZero() {
		virtObj.SetDeletionTimestamp(obj.GetDeletionTimestamp())
	}
	return virtObj
}

// hasFailedConditions returns true if the object has any conditions that are not True
func hasFailedConditions(obj client.Object) bool {
	getter := objToGetSet(obj)
	if getter == nil {
		return false
	}

	for _, c := range getter.GetConditions() {
		if c.Status != metav1.ConditionTrue {
			return true
		}
	}
	return false
}
