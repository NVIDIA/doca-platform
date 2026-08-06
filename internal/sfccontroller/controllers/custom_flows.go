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

	kerrors "k8s.io/apimachinery/pkg/util/errors"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	CustomFlowsLabelKey     = "svc.dpu.nvidia.com/custom-flows"
	CustomFlowsFireflyValue = "firefly"
)

// addCustomFlowsForFireflyChain adds custom OpenFlow rules for ServiceChain that have
// the custom flow label set on the pod (firefly pod).
// It identifies service and physical interfaces in the chain,
// retrieves their corresponding OVS ofPorts, and configures specialized multicast flows.
// Key here is that the firefly pod is correctly labeled with the custom flow label.
// This function will fail if there are more than one service or physical interfaces per switch.
func (r *ServiceChainReconciler) addCustomFlowsForFireflyChain(ctx context.Context, sc *dpuservicev1.ServiceChain, nsi *dpuservicev1.NodeServiceInterfaces) error {
	var errs []error
	// don't fail immediately, operate on best effort basis to enable partial flows
	// to enable some of the traffic to pass
	for _, sw := range sc.Spec.Switches {
		if err := r.addCustomFlowsForFireflySwitch(ctx, sc, nsi, sw); err != nil {
			errs = append(errs, err)
		}
	}

	if len(errs) > 0 {
		return kerrors.NewAggregate(errs)
	}
	return nil
}

// addCustomFlowsForFireflySwitch adds the PTP-multicast flow pair between this switch's single Firefly service port and uplink port, if both exist.
func (r *ServiceChainReconciler) addCustomFlowsForFireflySwitch(ctx context.Context, sc *dpuservicev1.ServiceChain, nsi *dpuservicev1.NodeServiceInterfaces, sw dpuservicev1.Switch) error {
	log := ctrllog.FromContext(ctx)

	var errs []error
	serviceOfPort := ""
	var uplinkOfPorts []string
	for _, port := range sw.Ports {
		ofPort, ifaceType, err := r.resolveFireflyPort(ctx, sc, nsi, port)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if ofPort == "" {
			continue
		}

		switch ifaceType {
		case dpuservicev1.InterfaceTypeService:
			if serviceOfPort != "" {
				errs = append(errs, fmt.Errorf("[customflows] firefly chain found more than one service interface"))
				continue
			}
			serviceOfPort = ofPort
			log.V(1).Info("[customflows] Using service port", "servicePort", serviceOfPort)
		case dpuservicev1.InterfaceTypePhysical:
			uplinkOfPorts = append(uplinkOfPorts, ofPort)
		}
	}

	// Without a Firefly service port, custom flows do not apply to this switch.
	if serviceOfPort == "" {
		return kerrors.NewAggregate(errs)
	}

	// A Firefly switch requires exactly one physical uplink.
	if len(uplinkOfPorts) == 0 {
		errs = append(errs, fmt.Errorf("[customflows] firefly chain has no physical uplink interface"))
	} else if len(uplinkOfPorts) > 1 {
		errs = append(errs, fmt.Errorf("[customflows] firefly chain found more than one physical interface"))
	} else {
		log.V(1).Info("[customflows] Found uplink port", "uplinkPort", uplinkOfPorts[0])
		if err := r.ensurePTPMulticastFlows(ctx, sc.Namespace+"/"+sc.Name, serviceOfPort, uplinkOfPorts[0]); err != nil {
			errs = append(errs, err)
		}
	}

	return kerrors.NewAggregate(errs)
}

// resolveFireflyPort resolves port's OVS ofPort and type, or ("", "", nil) if it should be skipped.
func (r *ServiceChainReconciler) resolveFireflyPort(ctx context.Context, sc *dpuservicev1.ServiceChain, nsi *dpuservicev1.NodeServiceInterfaces, port dpuservicev1.Port) (string, string, error) {
	candidate, err := r.getSingleInterfaceCandidate(ctx, sc.Namespace, nsi, port.ServiceInterface.MatchLabels)
	if err != nil {
		return "", "", err
	}

	ifaceType := candidate.spec.GetInterfaceType()
	if ifaceType != dpuservicev1.InterfaceTypeService && ifaceType != dpuservicev1.InterfaceTypePhysical {
		return "", "", nil
	}
	if ifaceType == dpuservicev1.InterfaceTypeService {
		service := candidate.spec.GetService()
		if service == nil {
			return "", "", fmt.Errorf("[customflows] service interface missing Service definition")
		}
		isFirefly, err := r.isFireflyServicePod(ctx, sc.Namespace, service.ServiceID)
		if err != nil {
			return "", "", err
		}
		if !isFirefly {
			return "", "", nil
		}
	}

	if isValid, reason := candidate.ready(); !isValid {
		return "", "", fmt.Errorf("[customflows] invalid service interface: %s", reason)
	}

	ofPort, err := r.getPortNameForInterfaceEntry(ctx, sc.Namespace, candidate.spec, candidate.condition)
	if err != nil {
		return "", "", err
	}
	return ofPort, ifaceType, nil
}

// isFireflyServicePod reports whether the pod backing serviceID carries the Firefly label.
func (r *ServiceChainReconciler) isFireflyServicePod(ctx context.Context, namespace, serviceID string) (bool, error) {
	log := ctrllog.FromContext(ctx)

	searchLabels := map[string]string{dpuservicev1.DPFServiceIDLabelKey: serviceID}
	pod, err := r.getPodWithLabels(ctx, namespace, searchLabels)
	if err != nil {
		if errors.Is(err, errPodNotFound) {
			return false, nil
		}
		return false, err
	}
	if pod == nil || pod.Labels == nil {
		return false, nil
	}

	customFlowValue, hasCustomFlow := pod.Labels[CustomFlowsLabelKey]
	if !hasCustomFlow || customFlowValue != CustomFlowsFireflyValue {
		return false, nil
	}
	log.Info("[customflows] Found pod with Firefly custom flow label", "pod", pod.Name, "customflow label", CustomFlowsLabelKey, "customflow value", customFlowValue)
	return true, nil
}

func (r *ServiceChainReconciler) ensurePTPMulticastFlows(ctx context.Context, namespacedName string, servicePort string, uplinkPort string) error {

	log := ctrllog.FromContext(ctx)

	// Add explicit output rule for PTP multicast MAC address, for RX
	flow := fmt.Sprintf("cookie=%d, table=0, priority=%d, in_port=%s, dl_dst=%s, actions=output=%s",
		hash(namespacedName), PriorityCustomFlows, uplinkPort, NonForwardablePTPMulticastMac, servicePort)

	log.V(1).Info(fmt.Sprintf("[customflows] Flow lines generated for RX: %s", flow))

	err := r.OPFlow.AddFlows(ctx, flow, r.BridgeName)
	if err != nil {
		return err
	}
	// Add explicit output rule for PTP multicast MAC address, for TX
	flow = fmt.Sprintf("cookie=%d, table=0, priority=%d, in_port=%s, dl_dst=%s, actions=output=%s",
		hash(namespacedName), PriorityCustomFlows, servicePort, NonForwardablePTPMulticastMac, uplinkPort)
	log.V(1).Info(fmt.Sprintf("[customflows] Flow lines generated for TX: %s", flow))
	return r.OPFlow.AddFlows(ctx, flow, r.BridgeName)
}

func (r *ServiceChainReconciler) EnsureCustomFlowsForChain(ctx context.Context, sc *dpuservicev1.ServiceChain, nsi *dpuservicev1.NodeServiceInterfaces) error {

	if err := r.addCustomFlowsForFireflyChain(ctx, sc, nsi); err != nil {
		return err
	}
	return nil
}
