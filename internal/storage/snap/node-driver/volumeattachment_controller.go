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

package controllers

import (
	"context"
	"fmt"
	"time"

	pb "github.com/nvidia/doca-platform/api/grpc/nvidia/storage/plugins/v1"
	snapstoragev1 "github.com/nvidia/doca-platform/api/storage/v1alpha1"
	rpcClient "github.com/nvidia/doca-platform/internal/storage/snap/node-driver/snap-rpc"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

// VolumeAttachmentReconciler reconciles a VolumeAttachment object
type VolumeAttachmentReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	NodeName string
}

const dpuFinalizer = "storage.nvidia.com/dpu-attachment-protection"

//+kubebuilder:rbac:groups=storage.nvidia.com,resources=volumeattachments,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=storage.nvidia.com,resources=volumeattachments/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=storage.nvidia.com,resources=volumeattachments/finalizers,verbs=update

func (r *VolumeAttachmentReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	klog.InfoS("Starting Reconciliation for VolumeAttachment CR", "volumeAttachment", req.NamespacedName)

	// Fetch the VolumeAttachment instance
	volumeAttachment := &snapstoragev1.VolumeAttachment{}
	if err := r.Get(ctx, req.NamespacedName, volumeAttachment); err != nil {
		if apierrors.IsNotFound(err) {
			// CR not found, could have been deleted after reconcile request
			klog.InfoS("VolumeAttachment resource not found. It may have been deleted.")
			return ctrl.Result{}, nil
		}
		klog.ErrorS(err, "Failed to retrieve VolumeAttachment")
		return ctrl.Result{}, err
	}

	if volumeAttachment.Spec.NodeName != r.NodeName {
		return ctrl.Result{}, nil
	}

	// Log details about the fetched CR
	klog.InfoS("Fetched VolumeAttachment CR",
		"Namespace", volumeAttachment.Namespace,
		"Name", volumeAttachment.Name,
		"NodeName", volumeAttachment.Spec.NodeName,
		"VolumeRef", volumeAttachment.Spec.Source.VolumeRef,
		"StorageAttached", volumeAttachment.Status.StorageAttached,
	)

	// Check if marked for deletion
	if volumeAttachment.ObjectMeta.DeletionTimestamp != nil {
		result, err := r.handleDetachment(ctx, volumeAttachment)
		if finalizerErr := r.removeFinalizer(ctx, volumeAttachment); finalizerErr != nil {
			return result, finalizerErr
		}
		return result, err
	}

	// Wait until storageAttached is set to true before proceeding
	if !volumeAttachment.Status.StorageAttached {
		klog.InfoS("storageAttached is not true yet")
		return ctrl.Result{}, nil
	}

	// Add finalizer if not already added
	if !controllerutil.ContainsFinalizer(volumeAttachment, dpuFinalizer) {
		klog.InfoS("Adding finalizer",
			"Finalizer", dpuFinalizer,
			"VolumeAttachment", volumeAttachment.Name)
		controllerutil.AddFinalizer(volumeAttachment, dpuFinalizer)
		if err := r.Update(ctx, volumeAttachment); err != nil {
			klog.ErrorS(err, "Failed to add finalizer")
			return ctrl.Result{}, err
		}
	}

	// Handle attachment
	return r.handleAttachment(ctx, volumeAttachment)
}

// handleAttachment handles the attachment workflow
func (r *VolumeAttachmentReconciler) handleAttachment(ctx context.Context, volumeAttachment *snapstoragev1.VolumeAttachment) (ctrl.Result, error) {
	klog.InfoS("Handling attachment for VolumeAttachment", "Name", volumeAttachment.Name)

	// Fetch the referenced Volume
	if volumeAttachment.Spec.Source.VolumeRef.Name == "" {
		klog.InfoS("No VolumeRef specified in VolumeAttachment, skipping Volume lookup")
		return ctrl.Result{}, nil
	}

	volumeKey := client.ObjectKey{
		Namespace: volumeAttachment.Spec.Source.VolumeRef.Namespace,
		Name:      volumeAttachment.Spec.Source.VolumeRef.Name,
	}

	volume := &snapstoragev1.Volume{}
	if err := r.Get(ctx, volumeKey, volume); err != nil {
		if apierrors.IsNotFound(err) {
			klog.ErrorS(err, "Referenced Volume not found", "Volume", volumeKey)
			return ctrl.Result{}, nil
		}
		klog.ErrorS(err, "Failed to retrieve Volume", "Volume", volumeKey)
		return ctrl.Result{}, err
	}

	klog.InfoS("Fetched referenced Volume CR",
		"Namespace", volume.Namespace,
		"Name", volume.Name,
		"StorageVendorName", volume.Spec.VolumeSpecDPU.StorageVendorName,
		"StorageVendorPluginName", volume.Spec.VolumeSpecDPU.StorageVendorPluginName,
		"Status", volume.Status,
	)

	client, identityClient, cleanup, err := r.dialPluginClient(ctx, volume.Spec.VolumeSpecDPU.StorageVendorName)
	if err != nil {
		return ctrl.Result{}, err
	}
	defer cleanup()

	if volumeAttachment.Status.DPU.Attached && volumeAttachment.Status.DPU.DeviceName != "" {
		// Check if the device exists in SNAP
		req := &pb.GetDeviceRequest{
			VolumeId:   volume.Spec.VolumeSpecDPU.CSIReference.PVCRef.Name,
			DeviceName: volumeAttachment.Status.DPU.DeviceName,
		}

		_, err = client.GetDevice(ctx, req)
		if err != nil {
			klog.InfoS("Device does not exist in SNAP",
				"DeviceName", volumeAttachment.Status.DPU.DeviceName,
				"Error", err)

			// Update the status to reflect that the device is no longer attached
			klog.InfoS("Updating VolumeAttachment status to detached",
				"DeviceName", volumeAttachment.Status.DPU.DeviceName)
			volumeAttachment.Status.DPU.Attached = false
			if updateErr := r.Status().Update(ctx, volumeAttachment); updateErr != nil {
				klog.ErrorS(updateErr, "Failed to update VolumeAttachment status")
				return ctrl.Result{}, fmt.Errorf("failed to update status: %w", updateErr)
			}
			return ctrl.Result{Requeue: true, RequeueAfter: time.Second * 1}, nil
		}
		return ctrl.Result{Requeue: true, RequeueAfter: time.Second * 10}, nil
	}

	if err := validateVolumePlugin(ctx, volume.Spec.Request.VolumeMode, client, identityClient); err != nil {
		klog.ErrorS(err, "validateVolumePlugin failed")
		return ctrl.Result{}, err
	}

	snapProvResp, err := client.GetSNAPProvider(ctx, &pb.GetSNAPProviderRequest{})
	if err != nil {
		r.logGRPCError("GetSNAPProvider", err)
	} else {
		klog.InfoS("GetSNAPProvider success", "ProviderName", snapProvResp.GetProviderName())
	}

	deviceName, err := r.callCreateDeviceAPI(ctx, client, volumeAttachment, volume)
	if err != nil {
		klog.ErrorS(err, "Failed to create device via gRPC API")
		volumeAttachment.Status.Message = err.Error()
		if updateErr := r.Status().Update(ctx, volumeAttachment); updateErr != nil {
			klog.ErrorS(updateErr, "Failed to update VolumeAttachment status with error message")
			return ctrl.Result{}, updateErr
		}
		return ctrl.Result{}, err
	}
	volumeAttachment.Status.DPU.DeviceName = deviceName
	if err := r.Status().Update(ctx, volumeAttachment); err != nil {
		klog.ErrorS(err, "Failed to update VolumeAttachment DPU status")
		return ctrl.Result{}, err
	}

	// Use SNAP JSON-RPC APIs to expose device (placeholder)
	nsID, pciAddr, uuid, err := r.exposeDeviceOnSNAP(snapProvResp.GetProviderName(), deviceName, volumeAttachment)

	// Update PCIDeviceAddress and BdevAttrs
	if pciAddr != "" {
		volumeAttachment.Status.DPU.PCIDeviceAddress = pciAddr
	}

	volumeMode := volume.Spec.Request.VolumeMode
	if *volumeMode == corev1.PersistentVolumeBlock && nsID > 0 && uuid != "" {
		volumeAttachment.Status.DPU.BdevAttrs = snapstoragev1.BdevAttrs{NVMeNsID: int64(nsID), NVMeUUID: uuid}
	} else {
		fsTag := "Not Supported"
		volumeAttachment.Status.DPU.FSdevAttrs = snapstoragev1.FSdevAttrs{FilesystemTag: fsTag}
	}

	if err != nil {
		klog.ErrorS(err, "Failed to expose device on SNAP")
		volumeAttachment.Status.Message = err.Error()
		if updateErr := r.Status().Update(ctx, volumeAttachment); updateErr != nil {
			klog.ErrorS(updateErr, "Failed to update VolumeAttachment status with error message")
			return ctrl.Result{}, updateErr
		}
		return ctrl.Result{}, err
	}

	volumeAttachment.Status.DPU.Attached = true
	volumeAttachment.Status.Message = "" // Clear any error message when attachment is successful

	if *volumeMode == corev1.PersistentVolumeBlock {
		klog.InfoS("VolumeAttachment DPU attributes updated",
			"DeviceName", deviceName,
			"PCIDeviceAddress", pciAddr,
			"BdevAttrs.NsID", nsID,
			"BdevAttrs.UUID", uuid,
		)
	} else {
		fsTag := "Not Supported"
		klog.InfoS("VolumeAttachment DPU attributes updated",
			"DeviceName", deviceName,
			"PCIDeviceAddress", pciAddr,
			"FSdevAttrs", fsTag,
		)
	}

	if err := r.Status().Update(ctx, volumeAttachment); err != nil {
		klog.ErrorS(err, "Failed to update VolumeAttachment DPU status")
		return ctrl.Result{}, err
	}

	klog.InfoS("Attachment completed successfully for VolumeAttachment",
		"Namespace", volumeAttachment.Namespace,
		"Name", volumeAttachment.Name)
	return ctrl.Result{Requeue: true, RequeueAfter: time.Second * 10}, nil
}

// handleDetachment handles the detachment workflow once deletionTimestamp is set
func (r *VolumeAttachmentReconciler) handleDetachment(ctx context.Context, volumeAttachment *snapstoragev1.VolumeAttachment) (ctrl.Result, error) {
	klog.InfoS("Handling detachment for VolumeAttachment", "Name", volumeAttachment.Name)

	if volumeAttachment.Spec.Source.VolumeRef.Name == "" {
		// No VolumeRef specified, just proceed with finalizer removal
		klog.InfoS("No VolumeRef specified in VolumeAttachment, nothing to detach")
		return ctrl.Result{}, nil
	}

	volumeKey := client.ObjectKey{
		Namespace: volumeAttachment.Spec.Source.VolumeRef.Namespace,
		Name:      volumeAttachment.Spec.Source.VolumeRef.Name,
	}

	volume := &snapstoragev1.Volume{}
	if err := r.Get(ctx, volumeKey, volume); err != nil {
		if apierrors.IsNotFound(err) {
			klog.InfoS("Referenced Volume not found, no volume to detach from", "Volume", volumeKey)
			return ctrl.Result{}, nil
		}
		klog.ErrorS(err, "Failed to retrieve Volume during detachment", "Volume", volumeKey)
		return ctrl.Result{}, err
	}

	klog.InfoS("Fetched referenced Volume CR",
		"Namespace", volume.Namespace,
		"Name", volume.Name,
		"StorageVendorName", volume.Spec.VolumeSpecDPU.StorageVendorName,
		"StorageVendorPluginName", volume.Spec.VolumeSpecDPU.StorageVendorPluginName,
		"Status", volume.Status,
	)

	// Dial the plugin to call DeleteDevice
	pluginName := volume.Spec.VolumeSpecDPU.StorageVendorName
	client, _, cleanup, err := r.dialPluginClient(ctx, pluginName)
	if err != nil {
		klog.ErrorS(err, "Failed to dial plugin client", "pluginName", pluginName)
		return ctrl.Result{}, err
	}
	defer cleanup()

	// Retrieve SNAP provider
	snapProvResp, err := client.GetSNAPProvider(ctx, &pb.GetSNAPProviderRequest{})
	if err != nil {
		r.logGRPCError("GetSNAPProvider", err)
	} else {
		klog.InfoS("GetSNAPProvider success", "ProviderName", snapProvResp.GetProviderName())
	}

	// If SNAP provider is retrieved, proceed to detach from SNAP
	if snapProvResp.GetProviderName() != "" {
		if err := r.detachFromSNAP(snapProvResp.GetProviderName(), volumeAttachment.Status.DPU.BdevAttrs.NVMeNsID, volumeAttachment.Status.DPU.PCIDeviceAddress); err != nil {
			klog.ErrorS(err, "Failed to detach from SNAP")
			return ctrl.Result{}, err
		}
		klog.InfoS("Successfully detached from SNAP",
			"SNAPProvider", snapProvResp.GetProviderName(),
			"PCIDeviceAddress", volumeAttachment.Status.DPU.PCIDeviceAddress)
	}

	if err := r.callDeleteDeviceAPI(ctx, client, volume.Spec.VolumeSpecDPU.CSIReference.PVCRef.Name, volumeAttachment.Status.DPU.DeviceName); err != nil {
		klog.ErrorS(err, "Failed to delete device via gRPC API", "DeviceName", volumeAttachment.Status.DPU.DeviceName)
		return ctrl.Result{}, err
	}
	klog.InfoS("Successfully deleted device via gRPC API", "DeviceName", volumeAttachment.Status.DPU.DeviceName)

	// Update status to reflect detachment
	volumeAttachment.Status.DPU.Attached = false
	if err := r.Status().Update(ctx, volumeAttachment); err != nil {
		klog.ErrorS(err, "Failed to update VolumeAttachment status during detachment")
		return ctrl.Result{}, err
	}
	klog.InfoS("VolumeAttachment DPU attributes updated", "Attached", volumeAttachment.Status.DPU.Attached)

	klog.InfoS("Detachment completed successfully for VolumeAttachment",
		"Name", volumeAttachment.Name)

	return ctrl.Result{}, nil
}

// removeFinalizer removes the finalizer from the VolumeAttachment
func (r *VolumeAttachmentReconciler) removeFinalizer(ctx context.Context, volumeAttachment *snapstoragev1.VolumeAttachment) error {
	if controllerutil.ContainsFinalizer(volumeAttachment, dpuFinalizer) {
		klog.InfoS("Removing finalizer",
			"Finalizer", dpuFinalizer,
			"VolumeAttachment", volumeAttachment.Name)
		controllerutil.RemoveFinalizer(volumeAttachment, dpuFinalizer)
		if err := r.Update(ctx, volumeAttachment); err != nil {
			klog.ErrorS(err, "Failed to remove finalizer")
			return err
		}
	}

	return nil
}

//nolint:staticcheck
func (r *VolumeAttachmentReconciler) dialPluginClient(ctx context.Context, pluginName string) (pb.StoragePluginServiceClient, pb.IdentityServiceClient, func(), error) {
	socketRPCPath := fmt.Sprintf("/var/lib/nvidia/storage/snap/plugins/%s/dpu.sock", pluginName)

	ctxDial, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	conn, err := grpc.DialContext(
		ctxDial,
		"unix://"+socketRPCPath,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		klog.ErrorS(err, "Failed to connect to plugin", "pluginName", pluginName, "socketRPCPath", socketRPCPath)
		return nil, nil, nil, err
	}

	// Cleanup function to close the connection
	cleanup := func() {
		if cerr := conn.Close(); cerr != nil {
			klog.ErrorS(cerr, "Failed to close gRPC connection")
		}
	}

	client := pb.NewStoragePluginServiceClient(conn)
	identityClient := pb.NewIdentityServiceClient(conn)
	return client, identityClient, cleanup, nil
}

func (r *VolumeAttachmentReconciler) logGRPCError(methodName string, err error) {
	st, ok := status.FromError(err)
	if !ok {
		klog.ErrorS(err, fmt.Sprintf("%s call failed with non-gRPC error", methodName))
		return
	}

	switch st.Code() {
	case codes.FailedPrecondition:
		klog.ErrorS(err, fmt.Sprintf("%s call failed: plugin is unable to complete the call successfully", methodName))
	default:
		klog.ErrorS(err, fmt.Sprintf("%s call failed with code %s", methodName, st.Code()))
	}
}

// validateVolumePlugin validates the plugin for the given volume.
func validateVolumePlugin(ctx context.Context, volumeMode *corev1.PersistentVolumeMode, client pb.StoragePluginServiceClient, identityClient pb.IdentityServiceClient) error {
	// Call GetPluginInfo
	infoResp, err := identityClient.GetPluginInfo(ctx, &pb.GetPluginInfoRequest{})
	if err != nil {
		klog.ErrorS(err, "GetPluginInfo failed")
		return fmt.Errorf("GetPluginInfo failed: %w", err)
	}
	klog.InfoS("GetPluginInfo success",
		"Name", infoResp.GetName(),
		"VendorVersion", infoResp.GetVendorVersion(),
		"Manifest", infoResp.GetManifest(),
	)

	// Call Probe
	probeResp, err := identityClient.Probe(ctx, &pb.ProbeRequest{})
	if err != nil {
		klog.ErrorS(err, "Probe failed")
		return fmt.Errorf("probe failed: %w", err)
	}
	klog.InfoS("Probe success", "Ready", probeResp.GetReady().GetValue())
	if !probeResp.GetReady().GetValue() {
		return fmt.Errorf("plugin is not ready (Probe not ready)")
	}

	// Call StoragePluginGetCapabilities
	capResp, err := client.StoragePluginGetCapabilities(ctx, &pb.StoragePluginGetCapabilitiesRequest{})
	if err != nil {
		klog.ErrorS(err, "StoragePluginGetCapabilities failed")
		return fmt.Errorf("StoragePluginGetCapabilities failed: %w", err)
	}
	klog.InfoS("StoragePluginGetCapabilities success", "Capabilities", capResp.GetCapabilities())

	// Determine the required capability based on volumeMode
	var requiredCapability pb.StoragePluginServiceCapability_RPC_Type
	if volumeMode != nil && *volumeMode == corev1.PersistentVolumeBlock {
		requiredCapability = pb.StoragePluginServiceCapability_RPC_TYPE_CREATE_DELETE_BLOCK_DEVICE
	} else if volumeMode != nil && *volumeMode == corev1.PersistentVolumeFilesystem {
		requiredCapability = pb.StoragePluginServiceCapability_RPC_TYPE_CREATE_DELETE_FS_DEVICE
	} else {
		err := fmt.Errorf("unsupported volume mode: %v", volumeMode)
		klog.ErrorS(err, "Volume mode unsupported")
		return err
	}

	// Check if the required capability is present
	if !hasCapability(capResp.GetCapabilities(), requiredCapability) {
		err := status.Errorf(codes.FailedPrecondition,
			"plugin does not support required capability %v for volume mode %v",
			requiredCapability, *volumeMode)
		klog.ErrorS(err, "Required capability not present")
		return err
	}

	// All checks passed
	klog.InfoS("validateVolumePlugin successful",
		"VolumeMode", volumeMode, "RequiredCapability", requiredCapability)
	return nil
}

func hasCapability(caps []*pb.StoragePluginServiceCapability, required pb.StoragePluginServiceCapability_RPC_Type) bool {
	for _, c := range caps {
		if rpc := c.GetRpc(); rpc != nil && rpc.Type == required {
			return true
		}
	}
	return false
}

func (r *VolumeAttachmentReconciler) callCreateDeviceAPI(
	ctx context.Context,
	client pb.StoragePluginServiceClient,
	volumeAttachment *snapstoragev1.VolumeAttachment,
	volume *snapstoragev1.Volume,
) (deviceName string, err error) {
	volumeID := volume.Spec.VolumeSpecDPU.CSIReference.PVCRef.Name

	// If the device exists, get the device name and exit
	if volumeAttachment.Status.DPU.DeviceName != "" {
		req := &pb.GetDeviceRequest{
			VolumeId:   volumeID,
			DeviceName: volumeAttachment.Status.DPU.DeviceName,
		}
		_, err := client.GetDevice(ctx, req)
		if err != nil && status.Code(err) != codes.NotFound {
			return "", fmt.Errorf("failed to get device: %w", err)
		} else if err == nil {
			return volumeAttachment.Status.DPU.DeviceName, nil
		}
	}

	storageParameters := make(map[string]string)
	for k, v := range volume.Spec.StorageParameters {
		storageParameters[k] = v
	}
	for k, v := range volume.Spec.StoragePolicyParameters {
		storageParameters[k] = v
	}

	// 2. volumeContext
	// volumeContext should be the value of volume.volumeAttributes
	volumeContext := make(map[string]string)
	for k, v := range volume.Spec.VolumeSpecDPU.VolumeAttributes {
		volumeContext[k] = v
	}

	// 3. publishContext
	// publishContext should contain the parameters from the NV-VolumeAttachment object.
	// In addition, add nv-volumeName and nv-volumeAttachmentName keys.
	// If nv-volumeName or nv-volumeAttachmentName already exist in the parameters map, return error (13 INTERNAL).
	publishContext := make(map[string]string)
	for k, v := range volumeAttachment.Spec.Parameters {
		publishContext[k] = v
	}

	// Check if nv-volumeName or nv-volumeAttachmentName already exist
	if _, exists := publishContext["nv-volumeName"]; exists {
		return "", status.Errorf(codes.Internal, "nv-volumeName already exists in parameters")
	}
	if _, exists := publishContext["nv-volumeAttachmentName"]; exists {
		return "", status.Errorf(codes.Internal, "nv-volumeAttachmentName already exists in parameters")
	}

	// Add nv-volumeName and nv-volumeAttachmentName
	publishContext["nv-volumeName"] = volume.Name
	publishContext["nv-volumeAttachmentName"] = volumeAttachment.Name

	// Translate Kubernetes volume mode to a string recognized by the CreateDeviceRequest
	volumeMode := volume.Spec.Request.VolumeMode
	var mode string
	if volumeMode != nil && *volumeMode == corev1.PersistentVolumeBlock {
		mode = "Block"
	} else {
		mode = "Filesystem"
	}

	// Translate access modes from Kubernetes to the plugin's AccessMode enum
	var pbAccessModes []pb.AccessMode
	for _, m := range volume.Spec.VolumeSpecDPU.AccessModes {
		switch m {
		case corev1.ReadWriteOnce:
			pbAccessModes = append(pbAccessModes, pb.AccessMode_ACCESS_MODE_RWO)
		case corev1.ReadOnlyMany:
			pbAccessModes = append(pbAccessModes, pb.AccessMode_ACCESS_MODE_ROX)
		case corev1.ReadWriteMany:
			pbAccessModes = append(pbAccessModes, pb.AccessMode_ACCESS_MODE_RWX)
		case corev1.ReadWriteOncePod:
			pbAccessModes = append(pbAccessModes, pb.AccessMode_ACCESS_MODE_RWOP)
		default:
			return "", fmt.Errorf("unsupported access mode: %s", m)
		}
	}

	req := &pb.CreateDeviceRequest{
		VolumeId:          volumeID,
		AccessModes:       pbAccessModes,
		VolumeMode:        mode,
		PublishContext:    publishContext,
		VolumeContext:     volumeContext,
		StorageParameters: storageParameters,
	}

	resp, err := client.CreateDevice(ctx, req)
	if err != nil {
		return "", fmt.Errorf("failed to create device: %w", err)
	}

	return resp.DeviceName, nil
}

func (r *VolumeAttachmentReconciler) callDeleteDeviceAPI(
	ctx context.Context,
	client pb.StoragePluginServiceClient,
	volumeID, deviceName string,
) error {

	req := &pb.DeleteDeviceRequest{
		VolumeId:   volumeID,
		DeviceName: deviceName,
	}

	_, err := client.DeleteDevice(ctx, req)
	if err != nil {
		return fmt.Errorf("failed to delete device: %w", err)
	}
	return nil
}

func (r *VolumeAttachmentReconciler) exposeDeviceOnSNAP(snapProvider, deviceName string,
	volumeAttachment *snapstoragev1.VolumeAttachment) (int, string, string, error) {
	return rpcClient.ExposeDevice(snapProvider, deviceName, volumeAttachment.Status.DPU, volumeAttachment.Spec.Parameters)
}

func (r *VolumeAttachmentReconciler) detachFromSNAP(snapProvider string, nsID int64, pciAddr string) error {
	return rpcClient.DestroyDevice(snapProvider, int(nsID), pciAddr)
}

// SetupWithManager sets up the controller with the Manager.
func (r *VolumeAttachmentReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&snapstoragev1.VolumeAttachment{}).
		Complete(r)
}
