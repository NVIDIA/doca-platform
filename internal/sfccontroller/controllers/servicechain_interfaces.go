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

package controller

import (
	"context"
	"errors"
	"fmt"

	dpuservicev1 "github.com/nvidia/doca-platform/api/dpuservice/v1alpha1"
	"github.com/nvidia/doca-platform/internal/utils"
	"github.com/nvidia/doca-platform/pkg/conditions"

	"k8s.io/apimachinery/pkg/labels"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// resolvedInterface bundles a single matched legacy or NSI interface behind interfaceEntrySpec, plus what's needed to validate and locate it.
type resolvedInterface struct {
	spec      interfaceEntrySpec
	condition string
	ready     func() (bool, string)
}

// isSFCNodeShard reports whether nsi is the SFC-typed NodeServiceInterfaces shard owned by node in the central NSI namespace.
func isSFCNodeShard(nsi *dpuservicev1.NodeServiceInterfaces, node string) bool {
	return nsi.Namespace == utils.NSIObjectsNamespace &&
		nsi.Spec.Node == node &&
		nsi.Spec.Type == dpuservicev1.NSITypeSFC
}

// getSFCNodeServiceInterfaces returns this node's "sfc"-typed NodeServiceInterfaces object, or nil if none exists.
func (r *ServiceChainReconciler) getSFCNodeServiceInterfaces(ctx context.Context) (*dpuservicev1.NodeServiceInterfaces, error) {
	nsiList := &dpuservicev1.NodeServiceInterfacesList{}
	if err := r.List(ctx, nsiList,
		client.InNamespace(utils.NSIObjectsNamespace),
		client.MatchingFields{
			utils.NSINodeFieldKey: r.NodeName,
			utils.NSITypeFieldKey: dpuservicev1.NSITypeSFC,
		},
	); err != nil {
		return nil, err
	}

	switch len(nsiList.Items) {
	case 0:
		return nil, nil
	case 1:
		return &nsiList.Items[0], nil
	default:
		return nil, fmt.Errorf("multiple SFC NodeServiceInterfaces objects found for node %s", r.NodeName)
	}
}

// isNSIEntryReady mirrors isValidateServiceInterface, but reads the single Ready condition InterfaceEntryStatus carries.
func isNSIEntryReady(nsi *dpuservicev1.NodeServiceInterfaces, entry *dpuservicev1.InterfaceEntry) (bool, string) {
	status := nsi.GetEntryStatus(entry.Name)
	if status != nil && conditions.IsTrue(status, conditions.TypeReady) {
		return true, ""
	}
	var errorMessage string
	if status != nil {
		if ready := conditions.Get(status, conditions.TypeReady); ready != nil {
			errorMessage = ready.Message
		}
	}
	return false, fmt.Sprintf(
		"NodeServiceInterfaces entry %s in namespace (%s) is not ready: %s",
		entry.Name, nsi.Namespace, errorMessage,
	)
}

// getInterfaceCandidates returns every interface on this node matching lbls; NSI matches take priority over legacy ones with the same labels.
func (r *ServiceChainReconciler) getInterfaceCandidates(ctx context.Context, namespace string, nsi *dpuservicev1.NodeServiceInterfaces, lbls map[string]string) ([]resolvedInterface, error) {
	if nsi != nil {
		selector := labels.SelectorFromSet(lbls)
		var nsiCandidates []resolvedInterface
		matchedNSI := false
		for i := range nsi.Spec.Interfaces {
			entry := &nsi.Spec.Interfaces[i]
			entryNamespace, _ := entry.GetNamespacedName()
			if entryNamespace != namespace || !selector.Matches(labels.Set(entry.Labels)) {
				continue
			}
			// Terminating entries are being drained: don't treat them as an NSI match, so a still-active
			// legacy ServiceInterface with the same labels can keep the chain up during the drain window.
			if entry.Terminating {
				continue
			}
			matchedNSI = true
			nsiCandidates = append(nsiCandidates, resolvedInterface{
				spec:      entry,
				condition: entry.Name,
				ready:     func() (bool, string) { return isNSIEntryReady(nsi, entry) },
			})
		}
		if matchedNSI {
			return nsiCandidates, nil
		}
	}

	sil, err := r.getServiceInterfaceListWithLabels(ctx, namespace, lbls)
	if err != nil {
		return nil, err
	}

	candidates := make([]resolvedInterface, 0, len(sil))
	for _, si := range sil {
		candidates = append(candidates, resolvedInterface{
			spec:      si,
			condition: si.Namespace + "/" + si.Name,
			ready:     func() (bool, string) { return isValidateServiceInterface(si) },
		})
	}

	return candidates, nil
}

// getSingleInterfaceCandidate resolves exactly one matching interface, erroring if none or more than one match.
func (r *ServiceChainReconciler) getSingleInterfaceCandidate(ctx context.Context, namespace string, nsi *dpuservicev1.NodeServiceInterfaces, lbls map[string]string) (*resolvedInterface, error) {
	candidates, err := r.getInterfaceCandidates(ctx, namespace, nsi, lbls)
	if err != nil {
		return nil, err
	}

	if len(candidates) == 0 {
		return nil, fmt.Errorf("no serviceInterface in namespace(%s) matching labels(%v) on node(%s) found", namespace, lbls, r.NodeName)
	}
	if len(candidates) > 1 {
		return nil, fmt.Errorf("expected only one serviceInterface in namespace(%s) to match labels(%v) on node(%s). found %d",
			namespace, lbls, r.NodeName, len(candidates))
	}

	return &candidates[0], nil
}

// getPortNameForInterfaceEntry returns the ovs port name matching the given interface entry.
func (r *ServiceChainReconciler) getPortNameForInterfaceEntry(ctx context.Context, namespace string, spec interfaceEntrySpec, condition string) (string, error) {
	if spec.GetInterfaceType() == dpuservicev1.InterfaceTypeService {
		svc := spec.GetService()
		if svc == nil {
			return "", errors.New("service definition missing for serviceInterface of type service")
		}
		// get pod matching serviceID
		pod, err := r.getPodWithLabels(ctx, namespace, map[string]string{dpuservicev1.DPFServiceIDLabelKey: svc.ServiceID})
		if err != nil {
			return "", err
		}
		// Identify the OVS port via the pod and interface name instead, since it's Service-typed.
		condition = pod.Namespace + "/" + pod.Name + "/" + svc.InterfaceName
	}

	port, err := findInterface(ctx, r.OVS, condition)
	if err != nil {
		return "", err
	}

	if port == "" {
		return "", fmt.Errorf("port with condition %s not found", condition)
	}

	return port, nil
}
