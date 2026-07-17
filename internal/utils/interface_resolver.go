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

package utils

import (
	"context"
	"fmt"

	dpuservicev1 "github.com/nvidia/doca-platform/api/dpuservice/v1alpha1"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ResolveServiceInterfaceByLabels resolves a single ServiceInterface for the given node
// and namespace matching matchLabels. It tries the NSI path first and falls back to the
// legacy ServiceInterface path if no match is found. Returns an error if more than one
// entry matches on either path.
//
// nsiTypes restricts which NSI shards are searched. Pass one or more type strings (e.g.
// "sfc") to limit the search, or pass nothing to search across all shards. The SFC
// controller should pass "sfc" since it only manages OVS ports for that shard; the
// pod-ipam-injector passes nothing because service interfaces can live in any shard.
//
// The caller's manager cache must have the NSI spec.node field index registered via
// SetupNSINodeIndexer.
func ResolveServiceInterfaceByLabels(
	ctx context.Context,
	c client.Client,
	nodeName, namespace string,
	matchLabels map[string]string,
	nsiTypes ...string,
) (*dpuservicev1.ServiceInterface, error) {
	sel := labels.SelectorFromSet(labels.Set(matchLabels))

	si, err := resolveFromNSI(ctx, c, nodeName, namespace, sel, nsiTypes...)
	if err != nil {
		return nil, err
	}
	if si != nil {
		return si, nil
	}

	return resolveFromLegacySI(ctx, c, nodeName, namespace, matchLabels, sel)
}

// ServiceInterfaceNodeFieldKey is the ServiceInterface spec.node field index key registered by ServiceInterfaceSetReconciler.
const ServiceInterfaceNodeFieldKey = "spec.node"

// ListInterfacesForNode returns every legacy and non-terminating NSI (restricted to nsiTypes) interface
// for nodeName as ServiceInterface objects. Interfaces are deduplicated by name, with NSI entries taking
// priority over a legacy ServiceInterface sharing the same name, mirroring the resolver's precedence.
func ListInterfacesForNode(
	ctx context.Context,
	c client.Client,
	nodeName, namespace string,
	nsiTypes ...string,
) ([]*dpuservicev1.ServiceInterface, error) {
	sil := &dpuservicev1.ServiceInterfaceList{}
	if err := c.List(ctx, sil,
		client.InNamespace(namespace),
		client.MatchingFields{ServiceInterfaceNodeFieldKey: nodeName},
	); err != nil {
		return nil, fmt.Errorf("list ServiceInterfaces for node %s: %w", nodeName, err)
	}

	order := make([]string, 0, len(sil.Items))
	byName := make(map[string]*dpuservicev1.ServiceInterface, len(sil.Items))
	add := func(si *dpuservicev1.ServiceInterface) {
		if _, seen := byName[si.Name]; !seen {
			order = append(order, si.Name)
		}
		byName[si.Name] = si
	}

	for i := range sil.Items {
		add(&sil.Items[i])
	}

	nsiList := &dpuservicev1.NodeServiceInterfacesList{}
	if err := c.List(ctx, nsiList,
		client.InNamespace(NSIObjectsNamespace),
		client.MatchingFields{NSINodeFieldKey: nodeName},
	); err != nil {
		return nil, fmt.Errorf("list NSI shards for node %s: %w", nodeName, err)
	}

	allowedTypes := make(map[string]bool, len(nsiTypes))
	for _, t := range nsiTypes {
		allowedTypes[t] = true
	}

	for _, nsi := range nsiList.Items {
		if len(allowedTypes) > 0 && !allowedTypes[nsi.Spec.Type] {
			continue
		}
		for i := range nsi.Spec.Interfaces {
			entry := &nsi.Spec.Interfaces[i]
			if entry.Terminating {
				continue
			}
			entryNS, _ := entry.GetNamespacedName()
			if entryNS != namespace {
				continue
			}
			add(entryToServiceInterface(entry, nodeName, namespace))
		}
	}

	interfaces := make([]*dpuservicev1.ServiceInterface, 0, len(order))
	for _, name := range order {
		interfaces = append(interfaces, byName[name])
	}
	return interfaces, nil
}

// resolveFromNSI searches NodeServiceInterfaces shards for this node, filtering by
// nsiTypes when provided. Returns (nil, nil) when no entry matches so the caller can
// fall through to the legacy path.
func resolveFromNSI(
	ctx context.Context,
	c client.Client,
	nodeName, namespace string,
	sel labels.Selector,
	nsiTypes ...string,
) (*dpuservicev1.ServiceInterface, error) {
	nsiList := &dpuservicev1.NodeServiceInterfacesList{}
	if err := c.List(ctx, nsiList,
		client.InNamespace(NSIObjectsNamespace),
		client.MatchingFields{NSINodeFieldKey: nodeName},
	); err != nil {
		return nil, fmt.Errorf("list NSI shards for node %s: %w", nodeName, err)
	}

	allowedTypes := make(map[string]bool, len(nsiTypes))
	for _, t := range nsiTypes {
		allowedTypes[t] = true
	}

	var matching []dpuservicev1.InterfaceEntry
	for _, nsi := range nsiList.Items {
		if len(allowedTypes) > 0 && !allowedTypes[nsi.Spec.Type] {
			continue
		}
		for _, entry := range nsi.Spec.Interfaces {
			// Skip entries marked for removal. A terminating entry lingers in
			// spec.Interfaces until ResourceReleased=True, and its interface data
			// may already be stale, so consumers must not resolve it.
			if entry.Terminating {
				continue
			}
			entryNS, _ := entry.GetNamespacedName()
			if entryNS != namespace {
				continue
			}
			if !sel.Matches(labels.Set(entry.Labels)) {
				continue
			}
			matching = append(matching, entry)
		}
	}

	switch len(matching) {
	case 1:
		return entryToServiceInterface(&matching[0], nodeName, namespace), nil
	case 0:
		return nil, nil
	default:
		return nil, fmt.Errorf("expected one NSI entry in namespace(%s) matching labels on node(%s), found %d",
			namespace, nodeName, len(matching))
	}
}

// resolveFromLegacySI lists ServiceInterface objects filtered by namespace, matchLabels
// and spec.node, returning the single match. Returns an error if zero or multiple match.
func resolveFromLegacySI(
	ctx context.Context,
	c client.Client,
	nodeName, namespace string,
	matchLabels map[string]string,
	sel labels.Selector,
) (*dpuservicev1.ServiceInterface, error) {
	sil := &dpuservicev1.ServiceInterfaceList{}
	if err := c.List(ctx, sil,
		client.MatchingLabelsSelector{Selector: sel},
		client.InNamespace(namespace),
		client.MatchingFields{ServiceInterfaceNodeFieldKey: nodeName},
	); err != nil {
		return nil, err
	}

	switch len(sil.Items) {
	case 1:
		return &sil.Items[0], nil
	case 0:
		return nil, fmt.Errorf("no serviceInterface in namespace(%s) matching labels(%v) on node(%s) found",
			namespace, matchLabels, nodeName)
	default:
		return nil, fmt.Errorf("expected only one serviceInterface in namespace(%s) to match labels(%v) on node(%s), found %d",
			namespace, matchLabels, nodeName, len(sil.Items))
	}
}

// entryToServiceInterface synthesizes a ServiceInterface from a NodeServiceInterfaces
// entry. The result carries the same fields that consumers (ServiceChain,
// pod-ipam-injector) need, making the NSI path transparent to callers.
func entryToServiceInterface(entry *dpuservicev1.InterfaceEntry, nodeName, namespace string) *dpuservicev1.ServiceInterface {
	_, setName := entry.GetNamespacedName()
	nodeCopy := nodeName
	return &dpuservicev1.ServiceInterface{
		ObjectMeta: metav1.ObjectMeta{
			Name:        setName,
			Namespace:   namespace,
			Labels:      entry.Labels,
			Annotations: entry.Annotations,
		},
		Spec: dpuservicev1.ServiceInterfaceSpec{
			Node:          &nodeCopy,
			InterfaceType: entry.InterfaceType,
			Physical:      entry.Physical,
			Vlan:          entry.Vlan,
			VF:            entry.VF,
			PF:            entry.PF,
			Service:       entry.Service,
			OVN:           entry.OVN,
			Patch:         entry.Patch,
		},
	}
}
