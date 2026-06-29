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

package dpudevice

import (
	"context"
	b64 "encoding/base64"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/internal/digest"
	rfclient "github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/state/redfish/client"
	dutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/util"
	cutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/util"
	"github.com/nvidia/doca-platform/internal/utils"
	"github.com/nvidia/doca-platform/pkg/conditions"

	"github.com/fluxcd/pkg/runtime/patch"
	"github.com/mcuadros/go-version"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	BMCMinSupportedVersion  = "BF-24.10-17"
	DPUDeviceControllerName = "dpudevice"
	hostlessDPUNodePrefix   = "hostless-"
	maxDPUNodeNameLength    = 48
	labelValueTrue          = "true"
)

// DPUDeviceReconciler reconciles a DPUDevice object
type DPUDeviceReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

//+kubebuilder:rbac:groups=provisioning.dpu.nvidia.com,resources=dpudevices,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=provisioning.dpu.nvidia.com,resources=dpudevices/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=provisioning.dpu.nvidia.com,resources=dpudevices/finalizers,verbs=update
//+kubebuilder:rbac:groups=provisioning.dpu.nvidia.com,resources=dpus,verbs=get;list;watch
//+kubebuilder:rbac:groups=provisioning.dpu.nvidia.com,resources=dpunodes,verbs=get;list;watch;create;update;patch
//+kubebuilder:rbac:groups="",resources=secrets,verbs=get;patch

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
func (r *DPUDeviceReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx)
	log.Info("Reconciling DPUDevice")

	// Fetch the DPUDevice instance
	dpuDevice := &provisioningv1.DPUDevice{}
	err := r.Get(ctx, req.NamespacedName, dpuDevice)
	if err != nil {
		if apierrors.IsNotFound(err) {
			// Request object not found, could have been deleted after reconcile request.
			// Return and don't requeue
			log.Info("DPUDevice not found, likely deleted")
			return ctrl.Result{}, nil
		}
		// Error reading the object - requeue the request.
		log.Error(err, "Failed to get DPUDevice")
		return ctrl.Result{}, err
	}

	if !dpuDevice.DeletionTimestamp.IsZero() {
		return r.reconcileDelete(ctx, dpuDevice)
	}

	return r.reconcile(ctx, dpuDevice)
}

// reconcileDelete handles cleanup when a DPUDevice is being deleted.
func (r *DPUDeviceReconciler) reconcileDelete(ctx context.Context, dpuDevice *provisioningv1.DPUDevice) (ctrl.Result, error) {
	log := log.FromContext(ctx)

	if !controllerutil.ContainsFinalizer(dpuDevice, provisioningv1.BMCCredentialFinalizer) {
		return ctrl.Result{}, nil
	}

	if err := r.cleanupCredentialFinalizer(ctx, dpuDevice); err != nil {
		log.Error(err, "Failed to clean up credential finalizer")
		return ctrl.Result{}, err
	}

	patch := client.MergeFrom(dpuDevice.DeepCopy())
	controllerutil.RemoveFinalizer(dpuDevice, provisioningv1.BMCCredentialFinalizer)
	if err := r.Client.Patch(ctx, dpuDevice, patch); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to remove BMCCredentialFinalizer from DPUDevice: %w", err)
	}

	return ctrl.Result{}, nil
}

// reconcile handles the main reconciliation logic for DPUDevice
func (r *DPUDeviceReconciler) reconcile(ctx context.Context, dpuDevice *provisioningv1.DPUDevice) (ctrl.Result, error) {
	log := log.FromContext(ctx)

	patcher := patch.NewSerialPatcher(dpuDevice, r.Client)
	// Defer a patch call to always patch the object when Reconcile exits.
	defer func() {
		log.Info("Patching")
		if err := patcher.Patch(ctx, dpuDevice,
			patch.WithFieldOwner(DPUDeviceControllerName),
			patch.WithStatusObservedGeneration{},
			patch.WithOwnedConditions{Conditions: conditions.TypesAsStrings(provisioningv1.DPUDeviceConditions)},
		); err != nil {
			log.Error(err, "Failed to patch DPUDevice")
		}
	}()

	if dpuDevice.Labels == nil {
		dpuDevice.Labels = make(map[string]string)
	}

	// 1. Check if the DPUDevice is attached to a DpuNode
	shouldContinue, result, err := r.checkDPUNodeAttachment(ctx, dpuDevice)
	if !shouldContinue {
		return result, err
	}

	// 2. For GNOI case skip reconsiliation for now
	dpfOperatorConfig, err := utils.GetDPFOperatorConfig(ctx, r.Client)
	if err != nil {
		log.Error(err, "Failed to get operator config")
		return ctrl.Result{}, err
	}

	dpuInstallInterface := dpfOperatorConfig.Spec.ProvisioningController.InstallInterface

	//nolint:staticcheck // SA1019: InstallViaGNOI is deprecated but still supported for backward compatibility
	if dpuInstallInterface == nil || dpuInstallInterface.InstallViaHostAgent != nil || dpuInstallInterface.InstallViaGNOI != nil {
		conditions.AddTrue(dpuDevice, provisioningv1.ConditionDpuDeviceDiscovered)
		conditions.AddTrue(dpuDevice, provisioningv1.ConditionDpuDeviceReady)
		setDPUDeviceLabels(dpuDevice)
		return ctrl.Result{}, nil
	}

	// Redfish install interface

	if dpuDevice.Spec.BMCIP != nil {
		dpuDevice.Status.BMCIP = dpuDevice.Spec.BMCIP
	}
	if dpuDevice.Spec.BMCPort != nil {
		dpuDevice.Status.BMCPort = dpuDevice.Spec.BMCPort
	}

	if dpuDevice.Labels[provisioningv1.DPUDeviceLabelSkipHWProvisioning] == labelValueTrue {
		// until redfish support is implemented, skip hardware provisioning
		log.Info("skip-hw-provisioning label set - skipping DPUDevice initialization and discovery")
		conditions.AddTrue(dpuDevice, provisioningv1.ConditionDpuDeviceInitialized)
		conditions.AddTrue(dpuDevice, provisioningv1.ConditionDpuDeviceDiscovered)
		conditions.AddTrue(dpuDevice, provisioningv1.ConditionDpuDeviceReady)
		setDPUDeviceLabels(dpuDevice)
		return ctrl.Result{}, nil
	}

	// Check if BMCIP and Port are set, provide defaults if not
	if dpuDevice.Status.BMCIP == nil {
		err := fmt.Errorf("BMCIP for DPUDevice %s is required but not set", dpuDevice.Name)
		conditions.AddTrue(dpuDevice, provisioningv1.ConditionDpuDeviceError)
		conditions.AddFalse(dpuDevice, provisioningv1.ConditionDpuDeviceReady,
			conditions.ReasonError,
			conditions.ConditionMessage(err.Error()))
		return ctrl.Result{}, err
	}

	// Check for disallowed mode switch (per-device → shared)
	if !r.isModeSwitchAllowed(dpuDevice) {
		conditions.AddFalse(dpuDevice, provisioningv1.ConditionBMCCredentialsReady,
			conditions.ConditionReason(provisioningv1.ReasonModeSwitchNotAllowed),
			conditions.ConditionMessage("Switching from per-device to shared credentials is not allowed. Delete and recreate the DPUDevice to use shared mode."))
		return ctrl.Result{}, nil
	}

	condition := conditions.Get(dpuDevice, provisioningv1.ConditionDpuDeviceInitialized)
	if condition == nil || condition.Status == metav1.ConditionFalse {
		if err := r.initializeDPUDevice(ctx, dpuDevice); err != nil {
			log.Error(err, "Failed to initialize DPUDevice")
			conditions.AddFalse(dpuDevice, provisioningv1.ConditionDpuDeviceInitialized,
				conditions.ReasonError,
				conditions.ConditionMessage(err.Error()))
			return ctrl.Result{}, err
		}

		condition = conditions.Get(dpuDevice, provisioningv1.ConditionDpuDeviceInitialized)
		if condition == nil || condition.Status == metav1.ConditionFalse {
			return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
		}
	} else if _, err := r.resolveAndAuthenticateBMC(ctx, dpuDevice, dpuDevice.BMCAddress(), true); err != nil {
		log.Error(err, "Failed to reconcile BMC credentials")
		return ctrl.Result{}, err
	}

	condition = conditions.Get(dpuDevice, provisioningv1.ConditionDpuDeviceDiscovered)
	if condition == nil || condition.Status == metav1.ConditionFalse {
		err = r.discoverDPUDevice(ctx, dpuDevice)
		if err != nil {
			log.Error(err, "Failed to discover DPUDevice")
			conditions.AddFalse(dpuDevice, provisioningv1.ConditionDpuDeviceDiscovered,
				conditions.ReasonError,
				conditions.ConditionMessage(err.Error()))
			return ctrl.Result{}, err
		}
	}

	conditions.AddTrue(dpuDevice, provisioningv1.ConditionDpuDeviceReady)

	log.Info("DPUDevice reconciled successfully", "dpuDevice", dpuDevice.Name)
	return ctrl.Result{}, nil
}

// checkDPUNodeAttachment checks if the DPUDevice is attached to a DPUNode. Returns whether the caller
// should continue reconciliation, and the result/error to return if not.
func (r *DPUDeviceReconciler) checkDPUNodeAttachment(ctx context.Context, dpuDevice *provisioningv1.DPUDevice) (bool, ctrl.Result, error) {
	log := log.FromContext(ctx)

	if isHostlessDPUDevice(dpuDevice) {
		dpuNodeName, err := r.ensureHostlessDPUNode(ctx, dpuDevice)
		if err != nil {
			log.Error(err, "Failed to ensure hostless DPUNode")
			conditions.AddFalse(dpuDevice, provisioningv1.ConditionDpuDeviceNodeAttached,
				conditions.ReasonError,
				conditions.ConditionMessage(err.Error()))
			return false, ctrl.Result{}, err
		}
		conditions.AddTrue(dpuDevice, provisioningv1.ConditionDpuDeviceNodeAttached)
		dpuDevice.Labels[provisioningv1.DPUNodeNameLabel] = dpuNodeName
		return true, ctrl.Result{}, nil
	}

	condition := conditions.Get(dpuDevice, provisioningv1.ConditionDpuDeviceNodeAttached)
	if condition != nil && condition.Status != metav1.ConditionFalse {
		return true, ctrl.Result{}, nil
	}

	dpuNodeList := &provisioningv1.DPUNodeList{}
	err := r.List(ctx, dpuNodeList, client.InNamespace(dpuDevice.Namespace))
	if err != nil {
		log.Error(err, "Failed to list DPUNode")
		return false, ctrl.Result{}, err
	}
	if len(dpuNodeList.Items) == 0 {
		log.Info("No DPUNode found, skipping reconciliation")
		conditions.AddFalse(dpuDevice, provisioningv1.ConditionDpuDeviceNodeAttached,
			conditions.ReasonPending,
			conditions.ConditionMessage("No DPUNode found"))
		return false, ctrl.Result{}, nil
	}

	dpuNodeFound := false
	for _, dpuNode := range dpuNodeList.Items {
		for _, dpu := range dpuNode.Spec.DPUs {
			if strings.EqualFold(dpu.Name, dpuDevice.Name) {
				conditions.AddTrue(dpuDevice, provisioningv1.ConditionDpuDeviceNodeAttached)
				dpuDevice.Labels[provisioningv1.DPUNodeNameLabel] = dpuNode.Name
				dpuNodeFound = true
				break
			}
		}
	}

	if !dpuNodeFound {
		conditions.AddFalse(dpuDevice, provisioningv1.ConditionDpuDeviceNodeAttached,
			conditions.ReasonPending,
			conditions.ConditionMessage("No DPUNode found"))
		return false, ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}

	return true, ctrl.Result{}, nil
}

func isHostlessDPUDevice(dpuDevice *provisioningv1.DPUDevice) bool {
	return dpuDevice.Labels[cutil.DPUDeviceHostlessLabel] == labelValueTrue
}

func (r *DPUDeviceReconciler) ensureHostlessDPUNode(ctx context.Context, dpuDevice *provisioningv1.DPUDevice) (string, error) {
	name := hostlessDPUNodeName(dpuDevice.Name)
	dpuNode := &provisioningv1.DPUNode{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: dpuDevice.Namespace,
		},
	}
	_, err := controllerutil.CreateOrPatch(ctx, r.Client, dpuNode, func() error {
		if controller := metav1.GetControllerOf(dpuNode); controller != nil && controller.UID != dpuDevice.UID {
			return fmt.Errorf("DPUNode %s/%s is already controlled by %s/%s", dpuNode.Namespace, dpuNode.Name, controller.Kind, controller.Name)
		}
		// The synthetic DPUNode is derived from this DPUDevice; keep its metadata
		// aligned with the DPUDevice instead of preserving external edits.
		dpuNode.Labels = copyStringMap(dpuDevice.Labels)
		if dpuNode.Labels == nil {
			dpuNode.Labels = map[string]string{}
		}
		dpuNode.Labels[cutil.NodeSelectorLabel] = labelValueTrue
		dpuNode.Annotations = copyStringMap(dpuDevice.Annotations)
		dpuNode.Spec.DPUs = []provisioningv1.DPURef{{Name: dpuDevice.Name}}
		dpuNode.Spec.NodeRebootMethod = &provisioningv1.NodeRebootMethod{None: &provisioningv1.None{}}
		dpuNode.OwnerReferences = []metav1.OwnerReference{*metav1.NewControllerRef(dpuDevice, provisioningv1.DPUDeviceGroupVersionKind)}
		return nil
	})
	return name, err
}

func hostlessDPUNodeName(dpuDeviceName string) string {
	name := hostlessDPUNodePrefix + dpuDeviceName
	if len(name) <= maxDPUNodeNameLength {
		return name
	}
	suffix := digest.Short(digest.FromObjects(dpuDeviceName), 8)
	prefixLen := maxDPUNodeNameLength - len(hostlessDPUNodePrefix) - 1 - len(suffix)
	return fmt.Sprintf("%s%s-%s", hostlessDPUNodePrefix, dpuDeviceName[:prefixLen], suffix)
}

func copyStringMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

// setDPUDeviceLabels sets device-specific labels on the DPUDevice from its spec and status fields.
func setDPUDeviceLabels(dpuDevice *provisioningv1.DPUDevice) {
	if dpuDevice.Status.PCIAddress != nil {
		dpuDevice.Labels[cutil.DPUDevicePCIAddressLabel] = *dpuDevice.Status.PCIAddress
	}
	//nolint:staticcheck
	if dpuDevice.Spec.PSID != nil {
		dpuDevice.Labels[cutil.DPUDevicePSIDLabel] = *dpuDevice.Spec.PSID
	}
	//nolint:staticcheck
	if dpuDevice.Spec.OPN != nil {
		dpuDevice.Labels[cutil.DPUDeviceOPNLabel] = *dpuDevice.Spec.OPN
	}
	if dpuDevice.Spec.NumberOfPFs != nil {
		dpuDevice.Labels[cutil.DPUDeviceNumOfPFsLabel] = fmt.Sprintf("%d", *dpuDevice.Spec.NumberOfPFs)
	}
	if dpuDevice.Status.PF0Name != nil {
		dpuDevice.Labels[cutil.DPUDevicePF0NameLabel] = *dpuDevice.Status.PF0Name
	} else if dpuDevice.Spec.PF0Name != nil { //nolint:staticcheck
		dpuDevice.Labels[cutil.DPUDevicePF0NameLabel] = *dpuDevice.Spec.PF0Name //nolint:staticcheck
	}
	if dpuDevice.Spec.BMCIP != nil {
		dpuDevice.Labels[cutil.DPUDeviceBMCIPLabel] = *dpuDevice.Spec.BMCIP
	}
}

// Updates BMC firmware if needed and returns the task ID, also sets up the TLS client
func (r *DPUDeviceReconciler) initializeDPUDevice(ctx context.Context, dpuDevice *provisioningv1.DPUDevice) error {
	log := log.FromContext(ctx)
	log.Info("Initializing DPUDevice")

	// Check if BMCIP and Port are set, provide defaults if not
	if dpuDevice.Status.BMCIP == nil {
		err := fmt.Errorf("BMCIP is required but not set")
		cutil.SetDPUDeviceCondition(dpuDevice, cutil.NewCondition(string(provisioningv1.ConditionDpuDeviceInitialized), err, "MissingBMCIP", err.Error()))
		return err
	}

	bmcAddress := dpuDevice.BMCAddress()

	// Resolve BMC credentials and handle rotation
	basicAuthClient, err := r.resolveAndAuthenticateBMC(ctx, dpuDevice, bmcAddress, false)
	if err != nil {
		err = fmt.Errorf("failed to initialize password: %w", err)
		cutil.SetDPUDeviceCondition(dpuDevice, cutil.NewCondition(string(provisioningv1.ConditionDpuDeviceInitialized), err, "FailedToInitializePassword", err.Error()))
		return err
	}

	if basicAuthClient.IsBF4 {
		dpuDevice.Status.DPUType = provisioningv1.DPUTypeBlueField4
	} else {
		dpuDevice.Status.DPUType = provisioningv1.DPUTypeBlueField3
		stop, err := r.checkAndUpdateBmcFw(ctx, dpuDevice, basicAuthClient)
		if err != nil {
			return err
		}
		if stop {
			return nil
		}
	}

	_, err = rfclient.NewTLSClient(ctx, bmcAddress, dpuDevice.Namespace, r.Client)
	if err != nil {
		log.Error(err, "failed to create tls client, setting up mTLS")
		if needBmcReset, err := r.setUpMTLS(ctx, dpuDevice, basicAuthClient); err != nil {
			condition := conditions.Get(dpuDevice, provisioningv1.ConditionDpuDeviceResettingBMC)
			err = fmt.Errorf("failed to set up mTLS: %w", err)
			if needBmcReset && (condition == nil || condition.Status == metav1.ConditionFalse) {
				log.Error(err, "resetting BMC to factory default")
				_, _, err = basicAuthClient.FactoryResetBMC()
				if err != nil {
					log.Error(err, "failed to factory reset BMC")
					return err
				}
				conditions.AddTrue(dpuDevice, provisioningv1.ConditionDpuDeviceResettingBMC)
				return nil
			} else {
				log.Error(err, "failed to set up mTLS")
				cutil.SetDPUDeviceCondition(dpuDevice, cutil.NewCondition(string(provisioningv1.ConditionDpuDeviceInitialized), err, "FailedToSetUpMTLS", err.Error()))
				return err
			}
		}
	}

	conditions.AddTrue(dpuDevice, provisioningv1.ConditionDpuDeviceInitialized)
	log.Info("DPUDevice initialized successfully", "dpuDevice", dpuDevice.Name)

	return nil
}

func (r *DPUDeviceReconciler) discoverDPUDevice(ctx context.Context, dpuDevice *provisioningv1.DPUDevice) error {
	log := log.FromContext(ctx)
	log.Info("Discovering DPUDevice", "dpuDevice", dpuDevice.Name)

	bmcAddress := dpuDevice.BMCAddress()
	client, err := rfclient.NewTLSClient(ctx, bmcAddress, dpuDevice.Namespace, r.Client)
	if err != nil {
		log.Error(err, "Failed to create TLS client")
		return err
	}

	resp, productDescription, err := client.GetProductDescription()
	if err != nil {
		log.Error(err, "Failed to get product description", "address", bmcAddress, "response", resp)
		return err
	}

	switch {
	case productDescription.Mode != nil && *productDescription.Mode == rfclient.NicMode:
		dpuDevice.Status.DPUMode = provisioningv1.NicMode
	default:
		dpuDevice.Status.DPUMode = provisioningv1.DpuMode
	}

	resp, chassisInfo, err := client.GetChassis()
	if err != nil {
		log.Error(err, "Failed to get chassis info", "address", bmcAddress, "response", resp)
		return err
	}

	dpuDevice.Status.DPUType = chassisInfo.GetBlueFieldVersion()
	if dpuDevice.Status.DPUMode == provisioningv1.DpuMode && dpuDevice.Status.DPUType == provisioningv1.DPUTypeUnknown {
		err = fmt.Errorf("unknown DPU type")
		log.Error(err, "Failed to get DPU type", "address", bmcAddress, "response", resp)
		return err
	}

	if chassisInfo.SerialNumber == "" {
		err = fmt.Errorf("serial number is empty")
		log.Error(err, "Failed to get chassis info", "address", bmcAddress, "response", resp)
		return err
	}

	if dpuDevice.Spec.SerialNumber != chassisInfo.SerialNumber {
		err = fmt.Errorf("serial number mismatch, expected: %s, actual: %s", dpuDevice.Spec.SerialNumber, chassisInfo.SerialNumber)
		log.Error(err, "Serial number mismatch", "expected", dpuDevice.Spec.SerialNumber, "actual", chassisInfo.SerialNumber)
		return err
	} else {
		dpuDevice.Status.SerialNumber = ptr.To(chassisInfo.SerialNumber)
		if chassisInfo.AssetTag != rfclient.ChassisAssetTagUnavailable {
			dpuDevice.Status.PSID = ptr.To(chassisInfo.AssetTag)
		}
	}

	dpuDevice.Status.OPN = ptr.To(chassisInfo.PartNumber)

	device := "eth0f0"
	if client.IsBF4 {
		device = "0"
	}
	resp, pf0, err := client.GetNetworkDeviceFunction(device)
	if err != nil {
		log.Error(err, "Failed to get network device function", "address", bmcAddress, "response", resp)
		return err
	}

	var mac string
	if client.IsBF4 {
		mac = pf0.Ethernet.PermanentMACAddress

	} else {
		mac = pf0.Ethernet.MACAddress
	}

	if mac != "" {
		dpuDevice.Status.PF0MAC = ptr.To(mac)
	} else {
		log.Info("No MAC address found for PF0", "address", bmcAddress, "response", resp)
	}

	if dpuDevice.Status.DPUType == provisioningv1.DPUTypeBlueField3 {
		resp, secureBootInfo, err := client.GetSecureBoot()
		if err != nil {
			log.Error(err, "Failed to get Secure Boot state", "address", bmcAddress, "response", resp)
			return err
		}

		if secureBootInfo != nil {
			enabled := secureBootInfo.IsCurrentlyActive()
			dpuDevice.Status.SecureBoot = &provisioningv1.SecureBootStatus{
				Enabled: ptr.To(enabled),
			}
			log.Info("Detected Secure Boot state", "address", bmcAddress, "enabled", enabled)
		}
	}
	// TODO: Get the PCI address once it will be available in the Redfish API

	if dpuDevice.Labels == nil {
		dpuDevice.Labels = make(map[string]string)
	}

	if dpuDevice.Status.PCIAddress != nil {
		dpuDevice.Labels[cutil.DPUDevicePCIAddressLabel] = *dpuDevice.Status.PCIAddress
	}
	if dpuDevice.Status.PSID != nil {
		dpuDevice.Labels[cutil.DPUDevicePSIDLabel] = *dpuDevice.Status.PSID
	}
	if dpuDevice.Status.OPN != nil {
		dpuDevice.Labels[cutil.DPUDeviceOPNLabel] = *dpuDevice.Status.OPN
	}
	dpuDevice.Labels[cutil.DPUDeviceBMCIPLabel] = *dpuDevice.Status.BMCIP

	// Labels and status will be updated by the deferred patch call in the reconcile function

	conditions.AddTrue(dpuDevice, provisioningv1.ConditionDpuDeviceDiscovered)
	log.Info("DPUDevice discovered successfully", "dpuDevice", dpuDevice.Name)
	return nil
}

// setUpMTLS sets up BMC mTLS in the same way as https://github.com/openbmc/bmcweb/blob/master/scripts/generate_auth_certificates.py
func (r *DPUDeviceReconciler) setUpMTLS(ctx context.Context, dpudevice *provisioningv1.DPUDevice, basicAuthClient *rfclient.Client) (bool, error) {
	caSecret := &corev1.Secret{}
	if err := r.Client.Get(ctx, types.NamespacedName{Name: rfclient.CASecret, Namespace: dpudevice.Namespace}, caSecret); err != nil {
		return false, fmt.Errorf("failed to get CA cert, err: %v", err)
	}
	caCert, ok := caSecret.Data["tls.crt"]
	if !ok {
		return false, fmt.Errorf("no CA cert in secret %s", caSecret.Name)
	}

	// step 1: install or replace CA certificate
	resp, _, err := basicAuthClient.InstallCert(string(caCert))
	if err != nil {
		return true, fmt.Errorf("failed to install CA cert, err: %v", err)
	} else if resp.StatusCode() == http.StatusInternalServerError {
		log.FromContext(ctx).Info("An existing CA certificate is likely already installed. Replacing...")
		resp, _, err = basicAuthClient.ReplaceCACert(string(caCert))
		if err != nil {
			return true, fmt.Errorf("failed to replace CA cert, err: %v", err)
		} else if resp.StatusCode() != http.StatusOK {
			return true, fmt.Errorf("failed to replace CA cert, unexpected response status: %s", resp.Status())
		}
		log.FromContext(ctx).Info("Successfully replaced CA certificate")
	} else if resp.StatusCode() != http.StatusOK {
		return true, fmt.Errorf("failed to install CA cert, unexpected response status: %s", resp.Status())
	}

	// step 2: replace server certificate
	log.FromContext(ctx).Info("Replace server certificate...")
	cr := &unstructured.Unstructured{}
	cr.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "cert-manager.io",
		Version: "v1",
		Kind:    "CertificateRequest",
	})
	err = r.Client.Get(ctx, types.NamespacedName{Name: dpudevice.Name, Namespace: dpudevice.Namespace}, cr)
	if err != nil {
		if apierrors.IsNotFound(err) {
			log.FromContext(ctx).Info("cert-manager CertificateRequest does not exist, try create one...")
			resp, csrInfo, err := basicAuthClient.GenerateCSR(*dpudevice.Status.BMCIP)
			if err != nil {
				return true, fmt.Errorf("failed to generate CSR, err: %v", err)
			} else if resp.StatusCode() != http.StatusOK {
				return true, fmt.Errorf("failed to generate CSR, unexpected response status: %s", resp.Status())
			}
			if err := r.createCR(ctx, dpudevice, csrInfo.CSRString); err != nil {
				return false, fmt.Errorf("failed to create cert-manager CertificateRequest, err: %v", err)
			}
			log.FromContext(ctx).Info("successfully created cert-manager CertificateRequest")
		} else {
			return false, fmt.Errorf("failed to get existing cert-manager CertificateRequest, err: %v", err)

		}
	}

	certificate, found, err := unstructured.NestedString(cr.Object, "status", "certificate")
	if err != nil {
		return false, fmt.Errorf("failed to extract certificate %w", err)
	}
	if !found {
		return false, fmt.Errorf("cert-manager CertificateRequest is not issued yet, retry later")
	}

	decodedCert, err := b64.StdEncoding.DecodeString(certificate)
	if err != nil {
		return false, fmt.Errorf("failed to base64 decode certificate %w", err)
	}

	resp, _, err = basicAuthClient.ReplaceServerCert(string(decodedCert))
	if err != nil {
		return true, fmt.Errorf("failed to replace server cert, err: %v", err)
	} else if resp.StatusCode() != http.StatusOK {
		return true, fmt.Errorf("failed to replace server cert, unexpected response status: %s", resp.Status())
	}
	log.FromContext(ctx).Info("Successfully replaced server certificate")

	// step 3: install client certificate
	log.FromContext(ctx).Info("Install client certificate...")
	clientSecret := &corev1.Secret{}
	name := rfclient.ClientCertSecret
	if basicAuthClient.IsBF4 {
		name = rfclient.ClientCertSecretBF4
	}
	if err := r.Client.Get(ctx, types.NamespacedName{Name: name, Namespace: dpudevice.Namespace}, clientSecret); err != nil {
		return false, fmt.Errorf("failed to get client cert, err: %v", err)
	}
	clientCert, ok := clientSecret.Data["tls.crt"]
	if !ok {
		return false, fmt.Errorf("no client cert in client secret %s", clientSecret.Name)
	}
	resp, _, err = basicAuthClient.InstallCert(string(clientCert))
	if err != nil {
		return true, fmt.Errorf("failed to install client cert, err: %v", err)
	} else if resp.StatusCode() == http.StatusInternalServerError {
		log.FromContext(ctx).Info("An existing client certificate is likely already installed. Skip installing client certificate")
	} else if resp.StatusCode() == http.StatusOK {
		log.FromContext(ctx).Info("Successfully installed client certificate")
	} else {
		return true, fmt.Errorf("failed to install client cert, unexpected response status: %s", resp.Status())
	}

	// step 4: enable mTLS
	log.FromContext(ctx).Info("enable mTLS...")
	resp, _, err = basicAuthClient.EnableMTLS()
	if err != nil {
		return true, fmt.Errorf("failed to enable mTLS, err: %v", err)
	} else if resp.StatusCode() != http.StatusOK {
		return true, fmt.Errorf("failed to enable mTLS, unexpected response status: %s", resp.Status())
	}
	log.FromContext(ctx).Info("Successfully enabled mTLS")
	return false, nil
}

func (r *DPUDeviceReconciler) generateCR(dpudevice *provisioningv1.DPUDevice, csr string) (*unstructured.Unstructured, error) {
	cr := &unstructured.Unstructured{}
	cr.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "cert-manager.io",
		Version: "v1",
		Kind:    "CertificateRequest",
	})
	cr.SetName(dpudevice.Name)
	cr.SetNamespace(dpudevice.Namespace)
	cr.SetOwnerReferences([]metav1.OwnerReference{*metav1.NewControllerRef(dpudevice, provisioningv1.DPUDeviceGroupVersionKind)})
	err := unstructured.SetNestedMap(cr.Object, map[string]interface{}{
		"request": b64.StdEncoding.EncodeToString([]byte(csr)),
		"isCA":    false,
		"usages": []interface{}{
			"server auth",
			"key encipherment",
			"digital signature",
		},
		"duration": metav1.Duration{
			// 365 days
			Duration: 8796 * time.Hour,
		}.ToUnstructured(),
		"issuerRef": map[string]interface{}{
			"name":  rfclient.Issuer,
			"kind":  "Issuer",
			"group": "cert-manager.io",
		},
	}, "spec")
	if err != nil {
		return nil, fmt.Errorf("failed to generate spec to CertificateRequest: %w", err)
	}
	return cr, nil
}

// createCR creates a cert-manager CertificateRequest for the given CSR
func (r *DPUDeviceReconciler) createCR(ctx context.Context, dpudevice *provisioningv1.DPUDevice, csr string) error {
	cr, err := r.generateCR(dpudevice, csr)
	if err != nil {
		return err
	}
	return r.Client.Create(ctx, cr)
}

// isModeSwitchAllowed checks whether the requested credential mode transition is permitted.
// Returns false if status shows per-device usage but spec requests shared mode.
func (r *DPUDeviceReconciler) isModeSwitchAllowed(dpuDevice *provisioningv1.DPUDevice) bool {
	if dpuDevice.Status.BMCCredentialSecretName == nil || *dpuDevice.Status.BMCCredentialSecretName == rfclient.BMCPasswordSecret {
		return true
	}
	// Status has a per-device value; check if spec is removing it or setting to shared
	return dpuDevice.Spec.BMCCredentialSecretName != nil &&
		*dpuDevice.Spec.BMCCredentialSecretName != "" &&
		*dpuDevice.Spec.BMCCredentialSecretName != rfclient.BMCPasswordSecret
}

// resolveAndAuthenticateBMC handles credential resolution, rotation, and BMC authentication.
// When alreadyInitialized is true the device's password has already been set, so first-use
// only verifies the credential instead of attempting to change the default BMC password.
// It returns a basic auth client authenticated against the BMC.
func (r *DPUDeviceReconciler) resolveAndAuthenticateBMC(ctx context.Context, dpuDevice *provisioningv1.DPUDevice, bmcAddress string, alreadyInitialized bool) (*rfclient.Client, error) {
	log := log.FromContext(ctx)

	specSecretName := rfclient.BMCPasswordSecret
	if dpuDevice.Spec.BMCCredentialSecretName != nil && *dpuDevice.Spec.BMCCredentialSecretName != "" {
		specSecretName = *dpuDevice.Spec.BMCCredentialSecretName
	}

	statusSecretName := ""
	if dpuDevice.Status.BMCCredentialSecretName != nil {
		statusSecretName = *dpuDevice.Status.BMCCredentialSecretName
	}

	if alreadyInitialized && specSecretName == statusSecretName {
		conditions.AddTrue(dpuDevice, provisioningv1.ConditionBMCCredentialsReady)
		return nil, nil
	}

	isRotation := statusSecretName != "" && statusSecretName != specSecretName

	var basicAuthClient *rfclient.Client
	if isRotation || alreadyInitialized {
		cred, err := rfclient.ResolveBMCCredential(ctx, dpuDevice.Namespace, dpuDevice.Spec.BMCCredentialSecretName, r.Client)
		if err != nil {
			r.setBMCCredentialsConditionFromError(dpuDevice, err)
			return nil, err
		}

		if isRotation {
			log.Info("Detected credential rotation", "from", statusSecretName, "to", specSecretName)

			oldCred, err := rfclient.ResolveBMCCredential(ctx, dpuDevice.Namespace, &statusSecretName, r.Client)
			if err != nil {
				r.setBMCCredentialsConditionFromError(dpuDevice, err)
				return nil, fmt.Errorf("failed to read old credential secret %q for rotation: %w", statusSecretName, err)
			}

			basicAuthClient, err = rfclient.RotatePassword(ctx, bmcAddress, cred.Password, oldCred.Password)
			if err != nil {
				r.setBMCCredentialsConditionFromError(dpuDevice, fmt.Errorf("password rotation failed: %w", err))
				return nil, fmt.Errorf("password rotation failed: %w", err)
			}

			if err := r.moveCredentialFinalizer(ctx, dpuDevice, statusSecretName, specSecretName); err != nil {
				return nil, fmt.Errorf("failed to move credential finalizer during rotation: %w", err)
			}
		} else {
			log.Info("Adopting per-device credential for an initialized device", "secret", specSecretName)
			basicAuthClient, _, err = rfclient.VerifyBMCCredential(bmcAddress, cred.Password)
			if err != nil {
				r.setBMCCredentialsConditionFromError(dpuDevice, err)
				return nil, fmt.Errorf("failed to verify per-device credential: %w", err)
			}
		}
	} else {
		var err error
		basicAuthClient, err = rfclient.InitPassword(ctx, bmcAddress, dpuDevice.Namespace, dpuDevice.Spec.BMCCredentialSecretName, r.Client)
		if err != nil {
			r.setBMCCredentialsConditionFromError(dpuDevice, err)
			return nil, err
		}
	}

	if !isRotation && dpuDevice.Spec.BMCCredentialSecretName != nil && *dpuDevice.Spec.BMCCredentialSecretName != "" {
		if err := r.ensureCredentialFinalizer(ctx, dpuDevice.Namespace, *dpuDevice.Spec.BMCCredentialSecretName); err != nil {
			return nil, fmt.Errorf("failed to add finalizer to credential secret: %w", err)
		}
	}

	// Update status to reflect the credential in use
	if specSecretName != rfclient.BMCPasswordSecret {
		controllerutil.AddFinalizer(dpuDevice, provisioningv1.BMCCredentialFinalizer)
	}
	dpuDevice.Status.BMCCredentialSecretName = ptr.To(specSecretName)
	conditions.AddTrue(dpuDevice, provisioningv1.ConditionBMCCredentialsReady)

	return basicAuthClient, nil
}

// setBMCCredentialsConditionFromError sets the BMCCredentialsReady condition based on the error type.
func (r *DPUDeviceReconciler) setBMCCredentialsConditionFromError(dpuDevice *provisioningv1.DPUDevice, err error) {
	errMsg := err.Error()
	switch {
	case strings.Contains(errMsg, "not found"):
		conditions.AddFalse(dpuDevice, provisioningv1.ConditionBMCCredentialsReady,
			conditions.ConditionReason(provisioningv1.ReasonCredentialsSecretNotFound),
			conditions.ConditionMessage(errMsg))
	case strings.Contains(errMsg, "empty or missing"):
		conditions.AddFalse(dpuDevice, provisioningv1.ConditionBMCCredentialsReady,
			conditions.ConditionReason(provisioningv1.ReasonCredentialsSecretInvalid),
			conditions.ConditionMessage(errMsg))
	case strings.Contains(errMsg, "password is wrong") || strings.Contains(errMsg, "unexpected BMC status"):
		conditions.AddFalse(dpuDevice, provisioningv1.ConditionBMCCredentialsReady,
			conditions.ConditionReason(provisioningv1.ReasonBMCAuthenticationFailed),
			conditions.ConditionMessage(errMsg))
	default:
		conditions.AddFalse(dpuDevice, provisioningv1.ConditionBMCCredentialsReady,
			conditions.ReasonError,
			conditions.ConditionMessage(errMsg))
	}
}

// ensureCredentialFinalizer adds the BMC credential finalizer to the referenced secret.
func (r *DPUDeviceReconciler) ensureCredentialFinalizer(ctx context.Context, namespace, secretName string) error {
	secret := &corev1.Secret{}
	if err := r.Client.Get(ctx, types.NamespacedName{Name: secretName, Namespace: namespace}, secret); err != nil {
		return err
	}
	if controllerutil.ContainsFinalizer(secret, provisioningv1.BMCCredentialFinalizer) {
		return nil
	}
	patch := client.MergeFrom(secret.DeepCopy())
	controllerutil.AddFinalizer(secret, provisioningv1.BMCCredentialFinalizer)

	// Ensure the secret is immutable
	if secret.Immutable == nil || !*secret.Immutable {
		immutable := true
		secret.Immutable = &immutable
	}

	return r.Client.Patch(ctx, secret, patch)
}

// removeCredentialFinalizer removes the BMC credential finalizer from the given secret.
func (r *DPUDeviceReconciler) removeCredentialFinalizer(ctx context.Context, namespace, secretName string) error {
	secret := &corev1.Secret{}
	if err := r.Client.Get(ctx, types.NamespacedName{Name: secretName, Namespace: namespace}, secret); err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return err
	}
	if !controllerutil.ContainsFinalizer(secret, provisioningv1.BMCCredentialFinalizer) {
		return nil
	}
	patch := client.MergeFrom(secret.DeepCopy())
	controllerutil.RemoveFinalizer(secret, provisioningv1.BMCCredentialFinalizer)
	return r.Client.Patch(ctx, secret, patch)
}

// moveCredentialFinalizer removes the finalizer from the old secret and adds it to the new one.
func (r *DPUDeviceReconciler) moveCredentialFinalizer(ctx context.Context, dpuDevice *provisioningv1.DPUDevice, oldSecretName, newSecretName string) error {
	// Skip finalizer operations for the shared secret
	if oldSecretName != rfclient.BMCPasswordSecret {
		if err := r.removeCredentialFinalizer(ctx, dpuDevice.Namespace, oldSecretName); err != nil {
			return fmt.Errorf("failed to remove finalizer from old secret %q: %w", oldSecretName, err)
		}
	}
	if newSecretName != rfclient.BMCPasswordSecret {
		if err := r.ensureCredentialFinalizer(ctx, dpuDevice.Namespace, newSecretName); err != nil {
			return fmt.Errorf("failed to add finalizer to new secret %q: %w", newSecretName, err)
		}
	}
	return nil
}

// cleanupCredentialFinalizer removes the BMC credential finalizer from the current credential secret
// during DPUDevice deletion.
func (r *DPUDeviceReconciler) cleanupCredentialFinalizer(ctx context.Context, dpuDevice *provisioningv1.DPUDevice) error {
	secretName := ""
	if dpuDevice.Status.BMCCredentialSecretName != nil {
		secretName = *dpuDevice.Status.BMCCredentialSecretName
	} else if dpuDevice.Spec.BMCCredentialSecretName != nil {
		secretName = *dpuDevice.Spec.BMCCredentialSecretName
	}

	if secretName == "" || secretName == rfclient.BMCPasswordSecret {
		return nil
	}

	return r.removeCredentialFinalizer(ctx, dpuDevice.Namespace, secretName)
}

// SetupWithManager sets up the controller with the Manager.
func (r *DPUDeviceReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&provisioningv1.DPUDevice{}).
		Watches(&provisioningv1.DPUNode{}, handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, obj client.Object) []ctrl.Request {
			dpuNode := obj.(*provisioningv1.DPUNode)
			var requests []ctrl.Request
			for _, dpu := range dpuNode.Spec.DPUs {
				requests = append(requests, ctrl.Request{
					NamespacedName: types.NamespacedName{
						Name:      dpu.Name,
						Namespace: dpuNode.Namespace,
					},
				})
			}
			return requests
		})).
		Complete(r)
}

// checkAndUpdateBmcFw checks BMC firmware version and updates it when below the minimum.
// Returns stop=true when reconciliation should pause (update in progress or BMC reset pending).
func (r *DPUDeviceReconciler) checkAndUpdateBmcFw(ctx context.Context, dpuDevice *provisioningv1.DPUDevice, basicAuthClient *rfclient.Client) (stop bool, err error) {
	log := log.FromContext(ctx)

	_, data, err := basicAuthClient.CheckBMCFirmware()
	if err != nil {
		err = fmt.Errorf("failed to get BMC firmware: %w", err)
		cutil.SetDPUDeviceCondition(dpuDevice, cutil.NewCondition(string(provisioningv1.ConditionDpuDeviceInitialized), err, "FailedToCheckBMCFW", err.Error()))
		return false, err
	}

	if version.Compare(data.Version, BMCMinSupportedVersion, "<") {
		taskName := fmt.Sprintf("%s-%s", dpuDevice.Name, dpuDevice.UID)

		if taskID, ok := dutil.BmcFwUpdateTaskMap.Load(taskName); ok {
			switch taskID := taskID.(type) {
			case string:
				// check progress
				resp, prog, err := basicAuthClient.CheckTaskProgress(taskID)
				if err != nil {
					err = fmt.Errorf("failed to check task progress: %w", err)
					log.Error(err, "Failed to check task progress")
					cutil.SetDPUDeviceCondition(dpuDevice, cutil.NewCondition(string(provisioningv1.ConditionDpuDeviceInitialized), err, "FailToCheckProgress", err.Error()))
					return false, err
				} else if resp.StatusCode() != http.StatusOK {
					err = fmt.Errorf("get status: %s is not OK", resp.Status())
					log.Error(err, "Failed to check task progress", "status", resp.Status())
					cutil.SetDPUDeviceCondition(dpuDevice, cutil.NewCondition(string(provisioningv1.ConditionDpuDeviceInitialized), err, "FailToCheckProgress", err.Error()))
					return false, err
				}
				log.Info(fmt.Sprintf("task: %+v", prog))
				switch prog.TaskState {
				case "Exception":
					err = fmt.Errorf("task %s failed: %v", taskID, prog.Messages)
					dutil.BmcFwUpdateTaskMap.Delete(taskName)
					cutil.SetDPUDeviceCondition(dpuDevice, cutil.NewCondition(string(provisioningv1.ConditionDpuDeviceInitialized), err, "FailToInstall", fmt.Sprintf("Task %s is in Exception state: %v", taskID, prog.Messages)))
					return false, err
				case "New", "Starting", "Running":
					log.Info(fmt.Sprintf("taskProgress: %+v", prog.PercentComplete))
					conditions.AddFalse(dpuDevice, provisioningv1.ConditionDpuDeviceInitialized,
						conditions.ReasonPending,
						conditions.ConditionMessage("BMC firmware update in progress"))
					return true, nil
				case "Completed":
					log.Info("Task completed. Resetting BMC")
					_, _, err := basicAuthClient.ResetBMC()
					if err != nil {
						err = fmt.Errorf("failed to reset BMC: %w", err)
						cutil.SetDPUDeviceCondition(dpuDevice, cutil.NewCondition(string(provisioningv1.ConditionDpuDeviceInitialized), err, "FailToResetBMC", err.Error()))
						return false, err
					}
					dutil.BmcFwUpdateTaskMap.Delete(taskName)
					conditions.AddFalse(dpuDevice, provisioningv1.ConditionDpuDeviceInitialized,
						conditions.ReasonPending,
						conditions.ConditionMessage("BMC firmware update completed, resetting BMC"))
					return true, nil
				default:
					err = fmt.Errorf("unknown task state: '%s'", prog.TaskState)
					log.Info(err.Error())
					cutil.SetDPUDeviceCondition(dpuDevice, cutil.NewCondition(string(provisioningv1.ConditionDpuDeviceInitialized), err, "UnknownSTate", err.Error()))
					return false, err
				}
			}

		} else {
			log.Info(fmt.Sprintf("Current BMC FW: %s is older than 24.10, update to 24.10-17", data.Version))
			bmcFwFile := os.Getenv("BMC_FW_FILE")
			if bmcFwFile == "" {
				bmcFwFile = "/bf3-bmc.fwpkg"
			}
			fwFile, err := os.Open(bmcFwFile)
			if err != nil {
				err = fmt.Errorf("failed to open BMC firmware file: %w", err)
				log.Error(err, "Failed to open BMC firmware file")
				cutil.SetDPUDeviceCondition(dpuDevice, cutil.NewCondition(string(provisioningv1.ConditionDpuDeviceInitialized), err, "FailedToOpenBMCFirmware", err.Error()))
				return false, err
			}
			defer func() {
				_ = fwFile.Close()
			}()
			resp, taskInfo, err := basicAuthClient.UpdateBMCFirmware(fwFile)
			if err != nil {
				err = fmt.Errorf("failed to update BMC firmware: %w", err)
				log.Error(err, "Failed to update BMC firmware")
				cutil.SetDPUDeviceCondition(dpuDevice, cutil.NewCondition(string(provisioningv1.ConditionDpuDeviceInitialized), err, "FailToUpdateBMCFW", err.Error()))
				return false, err
			} else if resp.StatusCode() != http.StatusAccepted {
				err = fmt.Errorf("get status: %s", resp.Status())
				log.Error(err, "Failed to update BMC firmware", "status", resp.Status())
				cutil.SetDPUDeviceCondition(dpuDevice, cutil.NewCondition(string(provisioningv1.ConditionDpuDeviceInitialized), err, "FailToUpdateBMCFW", err.Error()))
				return false, err
			}
			log.Info(fmt.Sprintf("new install task: %+v", *taskInfo))
			dutil.BmcFwUpdateTaskMap.Store(taskName, taskInfo.ID)
			conditions.AddFalse(dpuDevice, provisioningv1.ConditionDpuDeviceInitialized,
				conditions.ReasonPending,
				conditions.ConditionMessage(fmt.Sprintf("BMC firmware update started (current: %s, required: >= %s)", data.Version, BMCMinSupportedVersion)))
			return true, nil
		}
	}

	return false, nil
}
