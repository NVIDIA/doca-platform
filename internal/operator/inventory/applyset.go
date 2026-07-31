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

package inventory

import (
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"sort"
	"strings"

	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// Apply set implements the Kubernetes ApplySet spec to handle deletion of objects in the Kubernetes API.
// See https://github.com/kubernetes/enhancements/blob/master/keps/sig-cli/3659-kubectl-apply-prune/README.md
// kubectl's applyset file was used here as a reference implementation and some comments and code are directly copied from there.
// https://github.com/kubernetes/kubernetes/blob/ae945462fb2d12a4e38d074de8fe77267460624b/staging/src/k8s.io/kubectl/pkg/cmd/apply/applyset.go
const (
	// ApplySetToolingAnnotation is the key of the label that indicates which tool is used to manage this ApplySet.
	// Tooling should refuse to mutate ApplySets belonging to other tools.
	// The value must be in the format <toolname>/<semver>.
	// Example value: "kubectl/v1.27" or "helm/v3" or "kpt/v1.0.0"
	ApplySetToolingAnnotation = "applyset.kubernetes.io/tooling"

	// ApplySetToolingAnnotationValue signals that the DPF Operator controls the ApplySet.
	// TODO: consider having this version be dynamically populated.
	ApplySetToolingAnnotationValue = "dpf-operator/v0"

	// ApplySetParentIDLabel is the key of the label that makes object an ApplySet parent object.
	// Its value MUST use the format specified in v1ApplySetIDFormat below
	ApplySetParentIDLabel = "applyset.kubernetes.io/id"

	// applysetPartOfLabel is the key of the label which indicates that the object is a member of an ApplySet.
	// The value of the label MUST match the value of ApplySetParentIDLabel on the parent object.
	applysetPartOfLabel = "applyset.kubernetes.io/part-of"

	// ApplySetInventoryAnnotationKey is the key of the label which holds the inventory of the ApplySet in a
	// sorted list of Group Kind + Namespace Name.
	ApplySetInventoryAnnotationKey = "applyset.kubernetes.io/inventory"

	// v1ApplySetIDFormat is the format required for the value of ApplySetParentIDLabel (and applysetPartOfLabel).
	// The %s segment is the unique ID of the object itself, which MUST be the base64 encoding
	// (using the URL safe encoding of RFC4648) of the hash of the GKNN of the object it is on, in the form:
	// base64(sha256(<name>.<namespace>.<kind>.<group>)).
	v1ApplySetIDFormat = "applyset-%s-v1"
)

// GroupKindNamespaceName contains information required to uniquely identify an object as part of an ApplySet.
type GroupKindNamespaceName struct {
	Group     string
	Kind      string
	Namespace string
	Name      string
}

// String is the format for a GroupKindNamespaceName in the value for ApplySetInventoryAnnotationKey.
func (g GroupKindNamespaceName) String() string {
	return fmt.Sprintf("%s.%s/%s.%s", g.Kind, g.Group, g.Name, g.Namespace)
}

// ApplySetName returns the constant name for an ApplySet given a component.
func ApplySetName(component Component) string {
	return fmt.Sprintf("%s-%s", component.Name(), "applyset")
}

// ApplySetID is a unique ID for the ApplySet.
func ApplySetID(namespace string, component Component) string {
	// This ID must be in the format: base64(sha256(<name>.<namespace>.<kind>.<group>))
	unencoded := strings.Join([]string{ApplySetName(component), namespace, "Secret", ""}, ".")
	hashed := sha256.Sum256([]byte(unencoded))
	b64 := base64.RawURLEncoding.EncodeToString(hashed[:])
	// Label values must start and end with alphanumeric values, so add a known-safe prefix and suffix.
	return fmt.Sprintf(v1ApplySetIDFormat, b64)
}

// applySetParentForComponent returns a Secret object which is the parent for a given component.
func applySetParentForComponent(component Component, id string, vars Variables, inventory string) *unstructured.Unstructured {
	parent := &unstructured.Unstructured{}
	parent.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "",
		Kind:    "Secret",
		Version: "v1",
	})
	parent.SetName(ApplySetName(component))
	parent.SetNamespace(vars.Namespace)
	parent.SetLabels(map[string]string{
		ApplySetParentIDLabel:           id,
		operatorv1.DPFComponentLabelKey: component.Name(),
	})
	parent.SetAnnotations(map[string]string{
		ApplySetInventoryAnnotationKey: inventory,
		ApplySetToolingAnnotation:      ApplySetToolingAnnotationValue,
	})
	return parent
}

// applySetInventoryString returns a string to be used as the value for ApplySetInventoryAnnotationKey.
func applySetInventoryString(objs ...*unstructured.Unstructured) string {
	gknnStrings := []string{}
	for _, obj := range objs {
		gknnStrings = append(gknnStrings, GroupKindNamespaceName{
			Group:     obj.GetObjectKind().GroupVersionKind().Group,
			Kind:      obj.GetObjectKind().GroupVersionKind().Kind,
			Namespace: obj.GetNamespace(),
			Name:      obj.GetName(),
		}.String())
	}
	// items in the ApplySetInventoryAnnotation annotation are sorted alphabetically.
	sort.Strings(gknnStrings)
	return strings.Join(gknnStrings, ",")
}

// ParseGroupKindNamespaceName parses a string to a GroupKindNamespaceName.
func ParseGroupKindNamespaceName(s string) (GroupKindNamespaceName, error) {
	kindGroup, nameNamespace, ok := strings.Cut(s, "/")
	if !ok {
		return GroupKindNamespaceName{}, fmt.Errorf("GroupKindNameNamespace must have format <kind>.<group>/<name>.<namespace>. Input %v", s)
	}

	// A kind can not contain a "." but a group can - the first part of the cut string is the Kind and what follows is the Group.
	kind, group, ok := strings.Cut(kindGroup, ".")
	if !ok {
		return GroupKindNamespaceName{}, fmt.Errorf("GroupKindNameNamespace must have format <kind>.<group>/<name>.<namespace>. Input %v", s)
	}
	name, namespace, ok := strings.Cut(nameNamespace, ".")
	if !ok {
		return GroupKindNamespaceName{}, fmt.Errorf("GroupKindNameNamespace must have format <kind>.<group>/<name>.<namespace>. Input %v", s)
	}

	return GroupKindNamespaceName{
		Kind:      kind,
		Group:     group,
		Namespace: namespace,
		Name:      name,
	}, nil
}
