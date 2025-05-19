/*
COPYRIGHT 2025 NVIDIA

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

	dpuservicev1 "github.com/nvidia/doca-platform/api/dpuservice/v1alpha1"

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
func (r *ServiceChainReconciler) addCustomFlowsForFireflyChain(ctx context.Context, sc *dpuservicev1.ServiceChain) error {
	log := ctrllog.FromContext(ctx)

	for _, sw := range sc.Spec.Switches {
		// reset all counters and flags for each switch
		uplinkOfPort := ""
		serviceOfPort := ""
		for _, port := range sw.Ports {
			si, err := r.getServiceInterfaceWithLabels(ctx, sc.Namespace, port.ServiceInterface.MatchLabels)
			if err != nil {
				log.Error(err, "[customflows] failed to get service interface", "serviceinterface labels", port.ServiceInterface.MatchLabels)
				return err
			}

			switch si.Spec.InterfaceType {
			case dpuservicev1.InterfaceTypeService: // type `service`
				searchLabels := map[string]string{dpuservicev1.DPFServiceIDLabelKey: si.Spec.Service.ServiceID}
				pod, err := r.getPodWithLabels(ctx, sc.Namespace, searchLabels)
				if err != nil {
					log.Info("[customflows] failed to get pod for service interface", "searchLabels", searchLabels)
					continue
				}
				// Check if the pod has the custom flow labels
				if pod == nil || pod.Labels == nil {
					continue
				}

				customFlowValue, hasCustomFlow := pod.Labels[CustomFlowsLabelKey] // check for custom flow label
				if !hasCustomFlow || customFlowValue != CustomFlowsFireflyValue { // we do not have custom flow label or the value is not firefly
					continue
				}
				log.Info("[customflows] found pod with Firefly custom flow label", "pod", pod.Name, "customflow label", CustomFlowsLabelKey, "customflow value", customFlowValue)
				// Get the ofPort for this service interface
				ofPort, err := r.getPortNameForServiceInterface(ctx, sc.Namespace, &port.ServiceInterface)
				if err != nil {
					log.Error(err, "[customflows] failed to get ofPort for service interface", "serviceinterface labels", port.ServiceInterface.MatchLabels)
					return err
				}

				if ofPort != "" {
					serviceOfPort = ofPort
					log.Info("[customflows] using service port", "servicePort", serviceOfPort)
				}

			case dpuservicev1.InterfaceTypePhysical: // type `physical`
				// Get the ofPort for this uplink interface
				ofPort, err := r.getPortNameForServiceInterface(ctx, sc.Namespace, &port.ServiceInterface)
				if err != nil {
					log.Error(err, "[customflows] failed to get ofPort for uplink interface", "serviceinterface labels", port.ServiceInterface.MatchLabels)
					return err
				}

				if ofPort != "" {
					if uplinkOfPort != "" {
						return fmt.Errorf("[customflows] firefly chain found more than one physical interface")
					}
					uplinkOfPort = ofPort
					log.Info("[customflows] found uplink port", "uplinkPort", uplinkOfPort)
				}
			}
		}

		// We expect only one service port and one uplink per service chain switch
		if serviceOfPort != "" {
			if uplinkOfPort == "" {
				return fmt.Errorf("[customflows] Failed to add chain with no uplink port")
			}
			namespacedName := sc.Namespace + "/" + sc.Name
			// Add flow for each pair, but bailout in case of error
			err := r.ensurePTPMulticastFlows(ctx, namespacedName, serviceOfPort, uplinkOfPort)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func (r *ServiceChainReconciler) ensurePTPMulticastFlows(ctx context.Context, namespacedName string, servicePort string, uplinkPort string) error {

	log := ctrllog.FromContext(ctx)

	// Add explicit output rule for PTP multicast MAC address, for RX
	flow := fmt.Sprintf("cookie=%d, table=0, priority=%d, in_port=%s, dl_dst=%s, actions=output=%s",
		hash(namespacedName), PriorityCustomFlows, uplinkPort, NonForwardablePTPMulticastMac, servicePort)

	log.Info(fmt.Sprintf("[customflows] flow lines generated for RX: %s", flow))

	err := addFlows(ctx, r.Exec, flow)
	if err != nil {
		return err
	}
	// Add explicit output rule for PTP multicast MAC address, for TX
	flow = fmt.Sprintf("cookie=%d, table=0, priority=%d, in_port=%s, dl_dst=%s, actions=output=%s",
		hash(namespacedName), PriorityCustomFlows, servicePort, NonForwardablePTPMulticastMac, uplinkPort)
	log.Info(fmt.Sprintf("[customflows] flow lines generated for TX: %s", flow))
	return addFlows(ctx, r.Exec, flow)
}

func (r *ServiceChainReconciler) EnsureCustomFlowsForChain(ctx context.Context, sc *dpuservicev1.ServiceChain) error {

	if err := r.addCustomFlowsForFireflyChain(ctx, sc); err != nil {
		return err
	}
	return nil
}
