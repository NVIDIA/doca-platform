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

package nodeutils

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/cenkalti/backoff/v4"
	"github.com/go-logr/logr"
	dpuservicev1 "github.com/nvidia/doca-platform/api/dpuservice/v1alpha1"
	"github.com/nvidia/doca-platform/pkg/ovsmodel"
	"github.com/nvidia/doca-platform/pkg/ovsutils"
	"github.com/nvidia/doca-platform/pkg/utils/networkhelper"
	"github.com/ovn-org/libovsdb/client"
	"github.com/ovn-org/libovsdb/model"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/fields"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	// OVSDBSocketPath is the path to the OVS database socket
	OVSDBSocketPath = "unix:/var/run/openvswitch/db.sock"
	// IfaceIDKey is the key for the interface ID in the external IDs
	IfaceIDKey = "iface-id"
	// IfaceMacKey is the key for the interface MAC address in the external IDs
	IfaceMacKey = "iface-mac"
	// DpfIDKey is the key for the dpf ID in the external IDs
	DpfIDKey = "dpf-id"
	// IntegrationBridge is the name of the bridge that is used to connect the interfaces to the OVS
	IntegrationBridge = "br-int"
	// OVSConnectionTimeout defines the timeout duration for OVS client connection
	OVSConnectionTimeout = 15 * time.Second
	// OVSInactivityTimeout defines the inactivity timeout duration for OVS client
	OVSInactivityTimeout = 30 * time.Second
	// NetworkStatusAnnotationKey is the key for the network status annotation
	NetworkStatusAnnotationKey = "k8s.v1.cni.cncf.io/network-status"
	// PodNodeNameKey is the key for the node name in the pod spec
	PodNodeNameKey = "spec.nodeName"
)

// InitializeOVSClient sets up the OVS client connection and monitoring
func InitializeOVSClient(ctx context.Context) (*ovsutils.Client, error) {
	clientDBModel, err := createOVSDBModel()
	if err != nil {
		return nil, err
	}

	ovs, err := createAndConnectOVSClient(ctx, clientDBModel)
	if err != nil {
		return nil, err
	}

	if err := setupOVSMonitoring(ctx, ovs); err != nil {
		return nil, err
	}

	ovsClient := &ovsutils.Client{Client: ovs}
	return ovsClient, nil
}

// createOVSDBModel creates a new OVS database model with required tables
func createOVSDBModel() (*model.ClientDBModel, error) {
	ovsDBModel, err := model.NewClientDBModel("Open_vSwitch", map[string]model.Model{
		ovsmodel.BridgeTable:      &ovsmodel.Bridge{},
		ovsmodel.OpenvSwitchTable: &ovsmodel.OpenvSwitch{},
		ovsmodel.PortTable:        &ovsmodel.Port{},
		ovsmodel.InterfaceTable:   &ovsmodel.Interface{},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create ovsdb model: %v", err)
	}
	return &ovsDBModel, nil
}

// createAndConnectOVSClient creates and establishes connection to the OVS database
func createAndConnectOVSClient(ctx context.Context, clientDBModel *model.ClientDBModel) (client.Client, error) {
	discardLogger := logr.Discard()
	options := []client.Option{
		client.WithEndpoint(OVSDBSocketPath),
		client.WithInactivityCheck(OVSInactivityTimeout, OVSConnectionTimeout, &backoff.ZeroBackOff{}),
		client.WithLogger(&discardLogger),
	}

	ovs, err := client.NewOVSDBClient(*clientDBModel, options...)
	if err != nil {
		return nil, fmt.Errorf("failed to create ovsdb client: %v", err)
	}

	ctxCancel, cancel := context.WithTimeout(ctx, OVSConnectionTimeout)
	defer cancel()
	if err := ovs.Connect(ctxCancel); err != nil {
		return nil, fmt.Errorf("failed to connect to ovs: %v", err)
	}

	return ovs, nil
}

// setupOVSMonitoring initializes monitoring for OVS database tables
func setupOVSMonitoring(ctx context.Context, ovs client.Client) error {
	_, err := ovs.Monitor(
		ctx,
		ovs.NewMonitor(
			client.WithTable(&ovsmodel.OpenvSwitch{}),
			client.WithTable(&ovsmodel.Bridge{}),
			client.WithTable(&ovsmodel.Port{}),
			client.WithTable(&ovsmodel.Interface{}),
		),
	)
	if err != nil {
		return fmt.Errorf("failed to monitor ovs tables: %v", err)
	}
	return nil
}

// getPodWithLabelsOnNode returns pod in namespace that is scheduled on current node with given labels. if more than one or none matches, error out.
func getPodWithLabelsOnNode(ctx context.Context, client ctrlclient.Client, namespace string, lbls map[string]string, nodeName string) (*corev1.Pod, error) {
	log := ctrllog.FromContext(ctx)
	log.Info("Getting pod with labels on node", "namespace", namespace, "labels", lbls, "nodeName", nodeName)
	podList := &corev1.PodList{}
	listOpts := []ctrlclient.ListOption{}
	listOpts = append(listOpts, ctrlclient.MatchingLabels(lbls))
	listOpts = append(listOpts, ctrlclient.MatchingFieldsSelector{Selector: fields.OneTermEqualSelector(PodNodeNameKey, nodeName)})
	if namespace != "" {
		listOpts = append(listOpts, ctrlclient.InNamespace(namespace))
	}

	if err := client.List(ctx, podList, listOpts...); err != nil {
		return nil, err
	}

	if len(podList.Items) == 0 {
		return nil, fmt.Errorf("no pod in namespace(%s) matching labels(%v) on node(%s) found", namespace, lbls, nodeName)
	}

	if len(podList.Items) > 1 {
		return nil, fmt.Errorf("expected only one pod in namespace(%s) to match labels(%v) on node(%s). found %d",
			namespace, lbls, nodeName, len(podList.Items))
	}

	return &podList.Items[0], nil
}

func buildServiceInterfaceDPFIDValue(pod *corev1.Pod, interfaceName string) string {
	return fmt.Sprintf("%s/%s/%s", pod.Namespace, pod.Name, interfaceName)
}

func GetPortForServiceInterfaceTypeService(ctx context.Context, client ctrlclient.Client, ovsclient ovsutils.API, serviceInterface *dpuservicev1.ServiceInterface, nodeName string) (*ovsmodel.Interface, error) {
	log := ctrllog.FromContext(ctx)
	log.Info("Getting interface name for service interface", "ServiceInterface", serviceInterface)
	if serviceInterface.Spec.InterfaceType != dpuservicev1.InterfaceTypeService {
		return nil, fmt.Errorf("interface type is not service")
	}

	// get pod matching serviceID
	log.Info("Getting pod with labels", "namespace", serviceInterface.Namespace, "labels", map[string]string{dpuservicev1.DPFServiceIDLabelKey: serviceInterface.Spec.Service.ServiceID})
	pod, err := getPodWithLabelsOnNode(ctx, client, serviceInterface.Namespace, map[string]string{dpuservicev1.DPFServiceIDLabelKey: serviceInterface.Spec.Service.ServiceID}, nodeName)
	if err != nil {
		return nil, fmt.Errorf("failed to get pod with labels: %v", err)
	}
	// construct serviceInterfaceDPFIDValue which identifies ovs port for the interface that is associated
	// with service and serviceInterface.
	serviceInterfaceDPFIDValue := buildServiceInterfaceDPFIDValue(pod, serviceInterface.Spec.Service.InterfaceName)

	externalIDs := map[string]string{DpfIDKey: serviceInterfaceDPFIDValue}
	iface, err := ovsclient.GetIfaceWithExternalIDs(ctx, externalIDs)
	if err != nil {
		return nil, fmt.Errorf("failed to get interface with external_ids: %v", err)
	}

	return iface, nil
}

func getPortNameForServiceInterfaceTypeService(ctx context.Context, client ctrlclient.Client, ovsclient ovsutils.API, serviceInterface *dpuservicev1.ServiceInterface, nodeName string) (string, error) {
	port, err := GetPortForServiceInterfaceTypeService(ctx, client, ovsclient, serviceInterface, nodeName)
	if err != nil {
		return "", fmt.Errorf("failed to get port for service interface: %v", err)
	}
	return port.Name, nil
}

func GetPortNameForInterface(ctx context.Context, client ctrlclient.Client, ovsclient ovsutils.API, networkHelper networkhelper.NetworkHelper, serviceInterface *dpuservicev1.ServiceInterface, nodeName string) (string, error) {
	log := ctrllog.FromContext(ctx)

	// We only handle relevant interface types
	switch serviceInterface.Spec.InterfaceType {
	case dpuservicev1.InterfaceTypePF:
		interfaceName, err := networkHelper.GetPFRepresentorDPU(strconv.Itoa(serviceInterface.Spec.PF.ID))
		if err != nil {
			return "", fmt.Errorf("failed to get PF representor, PFID %d. %w", serviceInterface.Spec.PF.ID, err)
		}
		log.Info("Matched on interface type: PF", "interface name", interfaceName)
		return interfaceName, nil
	case dpuservicev1.InterfaceTypeVF:
		interfaceName, err := networkHelper.GetVFRepresentorDPU(
			strconv.Itoa(serviceInterface.Spec.VF.PFID), strconv.Itoa(serviceInterface.Spec.VF.VFID))
		if err != nil {
			return "", fmt.Errorf("failed to get VF representor, PFID %d, VFID %d. %w", serviceInterface.Spec.VF.PFID, serviceInterface.Spec.VF.VFID, err)
		}
		log.Info("Matched on interface type: VF", "interface name", interfaceName)
		return interfaceName, nil
	case dpuservicev1.InterfaceTypeService:
		log.Info("Matched on interface type: Service", "interface name", serviceInterface.Spec.Service.InterfaceName)
		return getPortNameForServiceInterfaceTypeService(ctx, client, ovsclient, serviceInterface, nodeName)
	default:
		return "", fmt.Errorf("unsupported interface type: %v", serviceInterface.Spec.InterfaceType)
	}
}
