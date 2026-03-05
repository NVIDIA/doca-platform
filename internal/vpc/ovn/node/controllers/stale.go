/*
SPDX-FileCopyrightText: Copyright (c) 2025 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
SPDX-License-Identifier: LicenseRef-NvidiaProprietary

NVIDIA CORPORATION, its affiliates and licensors retain all intellectual
property and proprietary rights in and to this material, related
documentation and any modifications thereto. Any use, reproduction,
disclosure or distribution of this material and related documentation
 without an express license agreement from NVIDIA CORPORATION or
its affiliates is strictly prohibited.
*/

package controllers

import (
	"context"
	"fmt"
	"time"

	"gitlab-master.nvidia.com/doca-platform-foundation/ovn-vpc/ovn/node/nodeutils"

	dpuservicev1 "github.com/nvidia/doca-platform/api/dpuservice/v1alpha1"
	"github.com/nvidia/doca-platform/pkg/ovsmodel"
	"github.com/nvidia/doca-platform/pkg/ovsutils"
	"github.com/ovn-org/libovsdb/model"
	"github.com/ovn-org/libovsdb/ovsdb"
	"k8s.io/apimachinery/pkg/util/sets"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
)

type StaleObjectRemover struct {
	duration time.Duration
	client   client.Client
	nodeName string
	OVS      ovsutils.API
}

func NewStaleObjectRemover(duration time.Duration, client client.Client, nodeName string, ovs ovsutils.API) *StaleObjectRemover {
	return &StaleObjectRemover{
		duration: duration,
		client:   client,
		nodeName: nodeName,
		OVS:      ovs,
	}
}

func (r *StaleObjectRemover) Start(ctx context.Context) error {
	log := ctrllog.FromContext(ctx)
	log.Info("starting stale object remover", "duration", r.duration)
	ticker := time.NewTicker(r.duration)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			err := r.removeStalePorts(ctx)
			if err != nil {
				log.Error(err, "failed to remove stale ports")
			}
		}
	}
}

// removeStalePorts
// Removes stale ports from the OVN integration bridge (`br-int`)
//
// This cleanup function targets ports in `br-int` which are no longer referenced by a ServiceInterface CR and thus has become stale.
//
// These include:
// * Ports controlled by VPC:
//   - VF (Virtual Function) ports
//   - PF (Physical Function) ports
//
// Note: This function does not manage or remove ports of InterfaceType 'Service', which are handled by the ovs-cni plugin.
func (r *StaleObjectRemover) removeStalePorts(ctx context.Context) error {
	log := ctrllog.FromContext(ctx)
	log.V(4).Info("removing stale ports started")
	currentPorts, err := getBrIntOvsPorts(ctx, r.OVS)
	if err != nil {
		return fmt.Errorf("failed to list ports on br-int: %w", err)
	}

	if len(currentPorts) == 0 {
		// no relevant ports to remove
		log.V(4).Info("no ports to remove")
		return nil
	}

	desiredPortSet := sets.New[string]()
	serviceInterfaceList := &dpuservicev1.ServiceInterfaceList{}
	if err = r.client.List(ctx, serviceInterfaceList); err != nil {
		return fmt.Errorf("failed to list ServiceInterfaceList: %w", err)
	}

	skipServiceInterface := func(serviceInterface *dpuservicev1.ServiceInterface) bool {
		return !serviceInterface.HasVirtualNetwork() || *serviceInterface.Spec.Node != r.nodeName ||
			serviceInterface.Spec.InterfaceType == dpuservicev1.InterfaceTypeService
	}

	for _, serviceInterface := range serviceInterfaceList.Items {
		if skipServiceInterface(&serviceInterface) {
			continue
		}
		portName, err := nodeutils.GetPortNameForInterface(ctx, r.client, r.OVS, &serviceInterface, r.nodeName)
		if err != nil {
			return fmt.Errorf("failed to get port name for service interface: %v", err)
		}
		desiredPortSet.Insert(portName)
	}

	unwantedPortSet := currentPorts.Difference(desiredPortSet)
	if len(unwantedPortSet) == 0 {
		log.V(4).Info("no stale ports found")
		return nil
	}
	log.V(4).Info("found stale ports", "ports", unwantedPortSet.UnsortedList())
	for ovsPortName := range unwantedPortSet {
		deleteError := r.OVS.DelPort(ctx, nodeutils.IntegrationBridge, ovsPortName)
		if deleteError != nil {
			return fmt.Errorf("failed to delete stale port: %s, with error: %w", ovsPortName, deleteError)
		}
	}
	log.V(4).Info("removing stale ports completed", "ports", unwantedPortSet.UnsortedList())

	return nil
}

// returns ports on the br-int after filtering out:
// a) OVS cni ports
// b) ports without iface-id on their interface
// c) bridge internal port
func getBrIntOvsPorts(ctx context.Context, ovs ovsutils.API) (sets.Set[string], error) {
	log := ctrllog.FromContext(ctx)
	var err error
	brint := &ovsmodel.Bridge{
		Name: nodeutils.IntegrationBridge,
	}

	if err = ovs.Get(ctx, brint); err != nil {
		log.Error(err, "failed to get br-int")
		return nil, fmt.Errorf("failed to get %s %v", nodeutils.IntegrationBridge, err)
	}

	var port ovsmodel.Port
	portSet := sets.New[string]()
	for _, portID := range brint.Ports {
		var allPorts []ovsmodel.Port
		// find all ports which were not added by dpf cni
		err = ovs.WhereAll(
			&port,
			model.Condition{
				Field:    &port.ExternalIDs,
				Function: ovsdb.ConditionExcludes,
				Value:    map[string]string{"owner": "ovs-cni.network.kubevirt.io"},
			},
			model.Condition{
				Field:    &port.UUID,
				Function: ovsdb.ConditionEqual,
				Value:    portID,
			},
			model.Condition{
				Field:    &port.Name,
				Function: ovsdb.ConditionNotEqual,
				Value:    nodeutils.IntegrationBridge,
			},
		).List(ctx, &allPorts)
		if err != nil {
			return nil, fmt.Errorf("failed to get port %s: %v", portID, err)
		}

		var ovnPorts []ovsmodel.Port

		// Filter for ports with interfaces that have iface-id key
		// We are certain that all ports with iface-id key are OVN ports, and VPC OVN cannot coexist with other OVN Kubernetes.
		for _, p := range allPorts {
			iface, err := ovs.GetIfaceWithName(ctx, p.Name)
			if err != nil {
				return nil, fmt.Errorf("failed to get iface %s: %v", p.Name, err)
			}
			if _, hasIfaceID := iface.ExternalIDs[nodeutils.IfaceIDKey]; hasIfaceID {
				// Port has iface-id key
				ovnPorts = append(ovnPorts, p)
			}
		}

		if len(ovnPorts) == 1 {
			portSet.Insert(ovnPorts[0].Name)
		}
	}

	return portSet, nil
}
