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
	"sort"
	"strings"
	"time"

	"github.com/fatih/color"
	"github.com/olekukonko/tablewriter"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
)

// Copied from: https://github.com/kubernetes-sigs/cluster-api/blob/release-1.9/cmd/clusterctl/cmd/describe_cluster.go#L55
var (
	gray   = color.New(color.FgHiBlack)
	red    = color.New(color.FgRed)
	green  = color.New(color.FgGreen)
	yellow = color.New(color.FgYellow)
	white  = color.New(color.FgWhite)
	cyan   = color.New(color.FgCyan)
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
	tbl.SetHeader([]string{"NAME", "NAMESPACE", "READY", "REASON", "SINCE", "MESSAGE"})

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

// addOtherConditions adds a row for each object condition except the ready condition,
// which is already represented on the object's main row.
// Adopted from:https://github.com/kubernetes-sigs/cluster-api/blob/release-1.9/cmd/clusterctl/cmd/describe_cluster.go#L470
func addOtherConditions(prefix string, tbl *tablewriter.Table, objectTree *ObjectTree, obj client.Object) {
	// Add a row for each other condition, taking care of updating the tree view prefix.
	// In this case the tree prefix get a filler, to indent conditions from objects, and eventually
	// an additional pipe if the object has children that should be presented after the conditions.
	filler := strings.Repeat(" ", 10)
	childrenPipe := indent
	if objectTree.IsObjectWithChild(obj.GetUID()) {
		childrenPipe = pipe
	}

	otherConditions := GetOtherConditions(obj)
	for i := range otherConditions {
		otherCondition := otherConditions[i]
		otherDescriptor := newConditionDescriptor(otherCondition, false)
		childPrefix := getChildPrefix(prefix+childrenPipe+filler, i, len(otherConditions))
		msg, _ := tablewriter.WrapString(otherDescriptor.message, 100)

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
				gray.Sprint(getMultilinePrefix(childPrefix)),
				"",
				"",
				"",
				"",
				m,
			})
		}
	}
}

// getMultilinePrefix return the tree view prefix for a multiline condition.
// Copy from: https://github.com/kubernetes-sigs/cluster-api/blob/release-1.9/cmd/clusterctl/cmd/describe_cluster.go#L513-L525
func getMultilinePrefix(currentPrefix string) string {
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

	multilinePrefix := getMultilinePrefix(prefix)
	// If it is the top-level root object, set the multiline prefix to a pipe.
	if prefix == "" {
		multilinePrefix = pipe
	}
	// If the multiline prefix is empty, we have to ensure that even multiline conditions on this root object are indented.
	if strings.TrimSpace(multilinePrefix) == "" {
		filler := strings.Repeat(" ", 10)
		childrenPipe := indent
		if objectTree.IsObjectWithChild(obj.GetUID()) {
			childrenPipe = pipe
		}
		// We have to set the childCount to 3 as we want to fake the childPrefix for the root object.
		// We just want to indent the multiline message for the Ready condition.
		childPrefix := getChildPrefix(prefix+childrenPipe+filler, 0, 3)
		multilinePrefix = getMultilinePrefix(childPrefix)
	}

	// If it is the last object and last condition we can remove the prefix completely.
	objectsByParentLen := len(objectTree.GetObjectsByParent(obj.GetUID()))
	otherConditionsLen := len(GetOtherConditions(obj))
	if (!IsShowConditionsObject(obj) && objectsByParentLen == 0) || objectsByParentLen+otherConditionsLen == 0 {
		multilinePrefix = ""
	}

	// If it is the last object and last condition we can remove the prefix completely.
	if len(GetOtherConditions(obj))+len(objectTree.GetObjectsByParent(obj.GetUID())) == 0 {
		multilinePrefix = ""
	}

	// Add the row representing the object that includes
	// - The row name with the tree view prefix.
	// - Replica counters
	// - The object's Available, Ready, UpToDate conditions
	// - The condition picked in the rowDescriptor.
	// Note: if the condition has a multiline message, also add additional rows for each line.
	name := getRowName(obj)
	msg, _ := tablewriter.WrapString(readyDescriptor.message, 100)
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

	// If it is required to show all the conditions for the object, add a row for each object's conditions.
	if IsShowConditionsObject(obj) {
		addOtherConditions(prefix, tbl, objectTree, obj)
	}

	// Add a row for each object's children, taking care of updating the tree view prefix.
	childrenObj := objectTree.GetObjectsByParent(obj.GetUID())

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

	for i, child := range childrenObj {
		addObjectRow(getChildPrefix(prefix, i, len(childrenObj)), tbl, objectTree, child)
	}
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

func showResource(showResources string, objKind string) bool {
	if showResources == "" || showResources == "all" {
		return true
	}
	kinds := strings.Split(showResources, ",")
	for _, kind := range kinds {
		if strings.EqualFold(ensurePlural(objKind), ensurePlural(kind)) {
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
