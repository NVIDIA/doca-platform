/*
COPYRIGHT 2024 NVIDIA

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
	"math"
	"time"

	dpuservicev1 "github.com/nvidia/doca-platform/api/dpuservice/v1alpha1"
	"github.com/nvidia/doca-platform/internal/ovsmodel"
	"github.com/nvidia/doca-platform/internal/ovsutils"

	"antrea.io/antrea/pkg/ovs/openflow"
	"github.com/ovn-org/libovsdb/model"
	"github.com/ovn-org/libovsdb/ovsdb"
	"k8s.io/apimachinery/pkg/util/sets"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
)

type StaleObjectRemover struct {
	duration time.Duration
	client   client.Client
	OFBridge openflow.Bridge
	OVS      ovsutils.API
}

func NewStaleObjectRemover(duration time.Duration, client client.Client, ofb openflow.Bridge, ovs ovsutils.API) *StaleObjectRemover {
	return &StaleObjectRemover{
		duration: duration,
		client:   client,
		OFBridge: ofb,
		OVS:      ovs,
	}
}

func (r *StaleObjectRemover) Start(ctx context.Context) error {
	log := ctrllog.FromContext(ctx)
	log.Info("setup stale flows cleaner")
	ticker := time.NewTicker(r.duration)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			err := r.removeStaleFlows(ctx)
			if err != nil {
				log.Error(err, "failed to remove stale flows")
			}
			err = r.removeStalePorts(ctx)
			if err != nil {
				log.Error(err, "failed to remove stale ports")
			}
		}
	}
}

// removeStaleFlows compares the derived OpenFlow cookies from the current CRs
// and compares them against actual OpenFlow cookies in the OVS flows.
// The difference is treated as stale flows and removed.
func (r *StaleObjectRemover) removeStaleFlows(ctx context.Context) error {
	log := ctrllog.FromContext(ctx)

	flowsStatusMap, err := r.OFBridge.DumpFlows(0, 0)
	if err != nil {
		return fmt.Errorf("failed to get flow cookies: %w", err)
	}

	currentCookiesSet := sets.New[uint64]()
	for cookie := range flowsStatusMap {
		currentCookiesSet.Insert(cookie)
	}

	desiredCookiesSet := sets.New[uint64]()
	serviceChainList := &dpuservicev1.ServiceChainList{}
	if err = r.client.List(ctx, serviceChainList); err != nil {
		return fmt.Errorf("failed to list service chains: %w", err)
	}

	for _, serviceChain := range serviceChainList.Items {
		serviceChainNamespacedName := serviceChain.GetNamespace() + "/" + serviceChain.Name
		desiredCookiesSet.Insert(hash(serviceChainNamespacedName))
	}

	msg := fmt.Sprintf("lenCurrentCookies: %d lenDesiredCookies: %d currentCookies: %v desiredCookies: %v",
		len(currentCookiesSet), len(desiredCookiesSet),
		currentCookiesSet.UnsortedList(), desiredCookiesSet.UnsortedList())
	// TODO: Use a logger which has debug level. eg: logrus
	log.V(4).Info(msg) // debug level, info is level 0.

	unwantedCookiesSet := currentCookiesSet.Difference(desiredCookiesSet)

	for flowCookie := range unwantedCookiesSet {
		log.Info(fmt.Sprintf("remove cookie=%d/-1", flowCookie))
		flowErrors := r.OFBridge.DeleteFlowsByCookie(flowCookie, math.MaxUint64)
		if flowErrors != nil {
			return fmt.Errorf("failed to delete flow cookie %d : %w", flowCookie, flowErrors)
		}
	}
	return nil
}

// removeStalePorts
// Removes ports on br-sfc which are not defined by ServiceInterface CRs
// two type of set of ports will be skipped
//
//	physical ports added by CRs (this is to ensure we are not deleting the port backing the ESwitch manager)
//	patch ports added by DPF CNI
func (r *StaleObjectRemover) removeStalePorts(ctx context.Context) error {
	log := ctrllog.FromContext(ctx)
	currentPorts, err := getSfcOvsPorts(ctx, r.OVS)
	if err != nil {
		return fmt.Errorf("failed to list ports on br-sfc: %w", err)
	}

	desiredPortSet := sets.New[string]()
	serviceInterfaceList := &dpuservicev1.ServiceInterfaceList{}
	if err = r.client.List(ctx, serviceInterfaceList); err != nil {
		return fmt.Errorf("failed to list ServiceInterfaceList: %w", err)
	}

	for _, serviceInterface := range serviceInterfaceList.Items {
		if serviceInterface.Spec.InterfaceType == dpuservicev1.InterfaceTypeOVN {
			desiredPortSet.Insert(OvnPatchPeer)
		} else {
			portName := FigureOutName(ctx, &serviceInterface)
			desiredPortSet.Insert(portName)
		}
	}

	unwantedPortsSet := currentPorts.Difference(desiredPortSet)
	log.V(4).Info(fmt.Sprintf("found stale ports: %s", unwantedPortsSet.UnsortedList()))

	for ovsPortName := range unwantedPortsSet {
		deleteError := r.OVS.DelPort(ctx, SFCBridge, ovsPortName)
		if deleteError != nil {
			return fmt.Errorf("failed to delete port: %s, with error: %w", ovsPortName, deleteError)
		}
		log.Info(fmt.Sprintf("deleted OVS port with name: %s", ovsPortName))
	}

	return nil
}

func getSfcOvsPorts(ctx context.Context, ovs ovsutils.API) (sets.Set[string], error) {
	var err error
	brsfc := &ovsmodel.Bridge{
		Name: "br-sfc",
	}

	if err = ovs.Get(ctx, brsfc); err != nil {
		return nil, fmt.Errorf("failed to get br-sfc %v", err)
	}

	var port ovsmodel.Port
	setPorts := sets.New[string]()
	for _, portID := range brsfc.Ports {
		var ports []ovsmodel.Port
		// find all ports which were not added by dpf cni
		err = ovs.WhereAll(
			&port,
			model.Condition{
				Field:    &port.ExternalIDs,
				Function: ovsdb.ConditionExcludes,
				Value:    map[string]string{"owner": "ovs-cni.network.kubevirt.io"},
			},
			model.Condition{
				Field:    &port.ExternalIDs,
				Function: ovsdb.ConditionExcludes,
				Value:    map[string]string{"dpf-type": "physical"},
			},
			model.Condition{
				Field:    &port.UUID,
				Function: ovsdb.ConditionEqual,
				Value:    portID,
			},
			model.Condition{
				Field:    &port.Name,
				Function: ovsdb.ConditionNotEqual,
				Value:    "br-sfc",
			},
		).List(context.Background(), &ports)
		if err != nil {
			return nil, fmt.Errorf("failed to get port %s: %v", portID, err)
		}
		if len(ports) == 1 {
			setPorts.Insert(ports[0].Name)
		}
	}

	return setPorts, nil
}
