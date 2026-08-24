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
	"crypto/sha256"
	"crypto/x509"
	b64 "encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"
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
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	ctrlbuilder "sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

const (
	BMCMinSupportedVersion  = "BF-24.10-17"
	DPUDeviceControllerName = "dpudevice"
	hostlessDPUNodePrefix   = "hostless-"
	maxDPUNodeNameLength    = 48
	labelValueTrue          = "true"

	// dpuByDPUDeviceNameField indexes DPUs by spec.dpuDeviceName so the SPIFFE entry reconciler
	// can look up the owning DPU for a DPUDevice without listing every DPU in the namespace.
	dpuByDPUDeviceNameField = "spec.dpuDeviceName"

	// certManagerGroup is the API group of the cert-manager resources DPF creates.
	certManagerGroup = "cert-manager.io"

	// serverCertDuration is the validity period requested for the BMC mTLS server certificate.
	serverCertDuration = 8760 * time.Hour // 365 days
	// defaultBMCServerCertRenewBefore is used when DPFOperatorConfig does not set BMCServerCertRenewBefore field.
	defaultBMCServerCertRenewBefore = 720 * time.Hour // 30 days
	// maxServerCertRequeue caps the rotation requeue so long-lived certs are still re-checked periodically.
	maxServerCertRequeue = 24 * time.Hour
	// minServerCertRequeue is the minimum time to wait before the next rotation check.
	minServerCertRequeue = time.Minute
	// serverCertIssuanceRequeue is how long to wait for cert-manager to issue a CertificateRequest
	// when the Owns watch does not fire first.
	serverCertIssuanceRequeue = 30 * time.Second
	// serverCertRotationBackoff is used when the mTLS client cannot be opened for rotation.
	serverCertRotationBackoff = 5 * time.Minute

	CATrustBundleConfigMap          = "dpf-ca-trust-bundle"
	CATrustBundleDataKey            = "ca.crt"
	BundleHashDataKey               = "bundle-hash"
	bmcManagerDateTimeSyncThreshold = 1 * time.Minute
)

// errCertRequestPending indicates the server-cert CertificateRequest is not yet issued and the
// reconcile should requeue without treating the rotation as failed.
var errCertRequestPending = errors.New("cert-manager CertificateRequest is not issued yet")

// DPUDeviceReconciler reconciles a DPUDevice object
type DPUDeviceReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
}

//+kubebuilder:rbac:groups=provisioning.dpu.nvidia.com,resources=dpudevices,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=provisioning.dpu.nvidia.com,resources=dpudevices/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=provisioning.dpu.nvidia.com,resources=dpudevices/finalizers,verbs=update
//+kubebuilder:rbac:groups=provisioning.dpu.nvidia.com,resources=dpus,verbs=get;list;watch
//+kubebuilder:rbac:groups=provisioning.dpu.nvidia.com,resources=dpunodes,verbs=get;list;watch;create;update;patch
//+kubebuilder:rbac:groups="",resources=secrets,verbs=get;patch
//+kubebuilder:rbac:groups=spire.spiffe.io,resources=clusterstaticentries,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch

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
//
// Deletion is ordered: the per-DPU SPIRE ClusterStaticEntry is deregistered (and observed gone
// from the K8s API) before the BMC credential finalizer is released, so a reflashed DPU cannot
// race a stale identity entry.
func (r *DPUDeviceReconciler) reconcileDelete(ctx context.Context, dpuDevice *provisioningv1.DPUDevice) (ctrl.Result, error) {
	log := log.FromContext(ctx)

	if controllerutil.ContainsFinalizer(dpuDevice, provisioningv1.SPIFFEDeregistrationFinalizer) {
		done, err := r.deleteSPIFFEEntry(ctx, dpuDevice)
		if err != nil {
			log.Error(err, "Failed to deregister SPIFFE ClusterStaticEntry")
			return ctrl.Result{}, err
		}
		if !done {
			// CR still present; requeue until K8s GC removes it, then release the finalizer.
			return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
		}
		patch := client.MergeFrom(dpuDevice.DeepCopy())
		controllerutil.RemoveFinalizer(dpuDevice, provisioningv1.SPIFFEDeregistrationFinalizer)
		if err := r.Client.Patch(ctx, dpuDevice, patch); err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to remove SPIFFEDeregistrationFinalizer from DPUDevice: %w", err)
		}
	}

	if controllerutil.ContainsFinalizer(dpuDevice, provisioningv1.BMCCredentialFinalizer) {
		if err := r.cleanupCredentialFinalizer(ctx, dpuDevice); err != nil {
			log.Error(err, "Failed to clean up credential finalizer")
			return ctrl.Result{}, err
		}
		patch := client.MergeFrom(dpuDevice.DeepCopy())
		controllerutil.RemoveFinalizer(dpuDevice, provisioningv1.BMCCredentialFinalizer)
		if err := r.Client.Patch(ctx, dpuDevice, patch); err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to remove BMCCredentialFinalizer from DPUDevice: %w", err)
		}
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

	dpfOperatorConfig, err := utils.GetDPFOperatorConfig(ctx, r.Client)
	if err != nil {
		log.Error(err, "Failed to get operator config")
		return ctrl.Result{}, err
	}

	// Reconcile the per-DPU SPIRE ClusterStaticEntry for SPIFFE-mode DPUs. No-op otherwise.
	if err := r.reconcileSPIFFEEntry(ctx, dpuDevice, dpfOperatorConfig); err != nil {
		log.Error(err, "Failed to reconcile SPIFFE ClusterStaticEntry")
		return ctrl.Result{}, err
	}

	// 2. For GNOI case skip reconsiliation for now
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

	if result, stop, err := r.checkBMCCredentialSecretUniqueness(ctx, dpuDevice); stop || err != nil {
		return result, err
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
	} else {
		if _, err := r.resolveAndAuthenticateBMC(ctx, dpuDevice, dpuDevice.BMCAddress(), true); err != nil {
			log.Error(err, "Failed to reconcile BMC credentials")
			return ctrl.Result{}, err
		}
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

	// Rotate the BMC mTLS server certificate before it expires (time-based) or on manual request.
	result, err = r.reconcileServerCertRotation(ctx, dpuDevice, bmcServerCertRenewBefore(dpfOperatorConfig))
	if err != nil {
		log.Error(err, "Failed to reconcile BMC server certificate rotation")
		return result, err
	}

	// Skip the success log when the server-certificate rotation is still in progress or has failed
	// (without a returned error, e.g. a backoff requeue).
	if cond := conditions.Get(dpuDevice, provisioningv1.ConditionDpuDeviceBMCServerCertificateReady); cond != nil && cond.Status != metav1.ConditionTrue {
		return result, nil
	}

	caResult, err := r.reconcileCATrustBundle(ctx, dpuDevice)
	if err != nil {
		return caResult, err
	}
	result = soonestRequeue(result, caResult)

	// Runs last so that a BMC that is only reachable after certificate recovery is not blocked by it.
	if err := r.reconcileDynamicFields(ctx, dpuDevice); err != nil {
		log.Error(err, "Failed to reconcile dynamic fields")
		return result, err
	}

	log.Info("DPUDevice reconciled successfully", "dpuDevice", dpuDevice.Name)
	return result, nil
}

// soonestRequeue returns the result requesting the earliest requeue. A zero RequeueAfter means no
// periodic requeue was requested, so a non-zero value always takes precedence over zero.
func soonestRequeue(a, b ctrl.Result) ctrl.Result {
	switch {
	case a.RequeueAfter == 0:
		return b
	case b.RequeueAfter == 0:
		return a
	case b.RequeueAfter < a.RequeueAfter:
		return b
	default:
		return a
	}
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

	if err := checkBMCManagerDateTimeSync(ctx, basicAuthClient); err != nil {
		cutil.SetDPUDeviceCondition(dpuDevice, cutil.NewCondition(string(provisioningv1.ConditionDpuDeviceInitialized), err, "BMCManagerDateTimeUnsynchronized", err.Error()))
		return err
	}

	_, err = rfclient.NewTLSClient(ctx, bmcAddress, dpuDevice.Namespace, r.Client)
	if err != nil {
		// Stale controller client leaf (CA re-issued without client renewal) cannot be healed by
		// setUpMTLS; force cert-manager to reissue and retry. Do not fall through to BMC bootstrap.
		if rfclient.IsRedfishClientCertStale(err) {
			log.Info("Redfish client certificate does not chain to current DPF CA; forcing reissue", "err", err.Error())
			if reissueErr := r.forceReissueRedfishClientCerts(ctx, dpuDevice.Namespace); reissueErr != nil {
				err = fmt.Errorf("failed to reissue Redfish client certificate: %w", reissueErr)
				cutil.SetDPUDeviceCondition(dpuDevice, cutil.NewCondition(string(provisioningv1.ConditionDpuDeviceInitialized), err, provisioningv1.ReasonRedfishClientCertStale, err.Error()))
				return err
			}
			err = fmt.Errorf("waiting for Redfish client certificate reissue: %w", err)
			cutil.SetDPUDeviceCondition(dpuDevice, cutil.NewCondition(string(provisioningv1.ConditionDpuDeviceInitialized), err, provisioningv1.ReasonRedfishClientCertStale, err.Error()))
			return err
		}
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

type desiredCATrustCert struct {
	pem         string
	fingerprint string
}

func (r *DPUDeviceReconciler) reconcileCATrustBundle(ctx context.Context, dpuDevice *provisioningv1.DPUDevice) (ctrl.Result, error) {
	log := log.FromContext(ctx)
	log.Info("Reconciling BMC CA trust bundle", "dpudevice", dpuDevice.Name, "namespace", dpuDevice.Namespace)
	configMap := &corev1.ConfigMap{}
	if err := r.Client.Get(ctx, types.NamespacedName{Name: CATrustBundleConfigMap, Namespace: dpuDevice.Namespace}, configMap); err != nil {
		setCATrustBundleCondition(dpuDevice, metav1.ConditionFalse, provisioningv1.ReasonCATrustBundleUnavailable, fmt.Sprintf("failed to get ConfigMap %s: %v", CATrustBundleConfigMap, err))
		return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
	}

	bundlePEM := configMap.Data[CATrustBundleDataKey]
	if bundlePEM == "" {
		err := fmt.Errorf("ConfigMap %s is missing %q", CATrustBundleConfigMap, CATrustBundleDataKey)
		setCATrustBundleCondition(dpuDevice, metav1.ConditionFalse, provisioningv1.ReasonCATrustBundleUnavailable, err.Error())
		return ctrl.Result{}, err
	}

	desiredCerts, err := parseDesiredCATrustBundle(bundlePEM)
	if err != nil {
		setCATrustBundleCondition(dpuDevice, metav1.ConditionFalse, provisioningv1.ReasonCATrustBundleUnavailable, err.Error())
		return ctrl.Result{}, err
	}

	bundleHash := configMap.Data[BundleHashDataKey]
	if bundleHash == "" {
		setCATrustBundleCondition(dpuDevice, metav1.ConditionFalse, provisioningv1.ReasonCATrustBundleUnavailable, fmt.Sprintf("ConfigMap %s is missing %q", CATrustBundleConfigMap, BundleHashDataKey))
		return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
	}

	if dpuDevice.Status.CATrustBundle == nil {
		dpuDevice.Status.CATrustBundle = &provisioningv1.TrustBundleStatus{}
	}

	observedHash := ptr.Deref(dpuDevice.Status.CATrustBundle.ObservedBundleHash, "")
	shouldSync := bundleHash != "" && observedHash != bundleHash
	if !shouldSync {
		// If desired and observed bundle hashes already match, keep the readiness condition stable.
		if bundleHash != "" && observedHash == bundleHash {
			setCATrustBundleCondition(dpuDevice, metav1.ConditionTrue, provisioningv1.ReasonCATrustBundleSynced, "")
		}
		log.Info("Skipping BMC CA trust bundle sync",
			"dpudevice", dpuDevice.Name,
			"desired_hash", bundleHash,
			"observed_hash", observedHash,
			"desired_cert_count", len(desiredCerts),
		)
		// No periodic requeue: the controller watches the CA trust bundle ConfigMap, so a bundle
		// change re-triggers reconciliation.
		return ctrl.Result{}, nil
	}
	setCATrustBundleCondition(dpuDevice, metav1.ConditionFalse, provisioningv1.ReasonCATrustBundleSyncing, "syncing BMC truststore with desired CA bundle")
	log.Info("Starting BMC CA trust bundle sync",
		"dpudevice", dpuDevice.Name,
		"desired_hash", bundleHash,
		"observed_hash", observedHash,
		"desired_cert_count", len(desiredCerts),
	)

	tlsClient, err := rfclient.NewTLSClient(ctx, dpuDevice.BMCAddress(), dpuDevice.Namespace, r.Client)
	if err != nil {
		setCATrustBundleCondition(dpuDevice, metav1.ConditionFalse, provisioningv1.ReasonCATrustBundleSyncFailed, fmt.Sprintf("failed to create mTLS redfish client: %v", err))
		return ctrl.Result{}, err
	}

	installedCerts, err := tlsClient.ListTruststoreCerts()
	if err != nil {
		setCATrustBundleCondition(dpuDevice, metav1.ConditionFalse, provisioningv1.ReasonCATrustBundleSyncFailed, fmt.Sprintf("failed to list truststore certificates: %v", err))
		return ctrl.Result{}, err
	}

	desiredByFingerprint := make(map[string]desiredCATrustCert, len(desiredCerts))
	for _, cert := range desiredCerts {
		desiredByFingerprint[cert.fingerprint] = cert
	}
	installedByFingerprint := make(map[string]rfclient.TruststoreCert, len(installedCerts))
	for _, cert := range installedCerts {
		installedByFingerprint[cert.Fingerprint] = cert
	}

	log.Info("Calculated BMC truststore diff",
		"dpudevice", dpuDevice.Name,
		"desired_cert_count", len(desiredByFingerprint),
		"installed_cert_count", len(installedByFingerprint),
	)

	installedCount := 0
	for fingerprint, cert := range desiredByFingerprint {
		if _, found := installedByFingerprint[fingerprint]; found {
			continue
		}
		resp, _, err := tlsClient.InstallCert(cert.pem)
		if err != nil {
			log.Error(err, "Failed to install truststore certificate", "dpudevice", dpuDevice.Name, "fingerprint", fingerprint)
			setCATrustBundleCondition(dpuDevice, metav1.ConditionFalse, provisioningv1.ReasonCATrustBundleSyncFailed, fmt.Sprintf("failed to install truststore cert %s: %v", fingerprint, err))
			return ctrl.Result{}, fmt.Errorf("install truststore cert %s: %w", fingerprint, err)
		}
		if resp.StatusCode() != http.StatusOK {
			log.Info("Unexpected status while installing truststore certificate", "dpudevice", dpuDevice.Name, "fingerprint", fingerprint, "status", resp.Status())
			setCATrustBundleCondition(dpuDevice, metav1.ConditionFalse, provisioningv1.ReasonCATrustBundleSyncFailed, fmt.Sprintf("install truststore cert %s: unexpected status %s", fingerprint, resp.Status()))
			return ctrl.Result{}, fmt.Errorf("install truststore cert %s: unexpected status %s", fingerprint, resp.Status())
		}
		log.Info("Installed missing truststore certificate", "fingerprint", fingerprint)
		installedCount++
	}

	removedCount := 0
	for fingerprint, cert := range installedByFingerprint {
		if _, keep := desiredByFingerprint[fingerprint]; keep {
			continue
		}
		resp, _, err := tlsClient.DeleteTruststoreCert(cert.URI)
		if err != nil {
			log.Error(err, "Failed to delete truststore certificate", "dpudevice", dpuDevice.Name, "fingerprint", fingerprint, "uri", cert.URI)
			setCATrustBundleCondition(dpuDevice, metav1.ConditionFalse, provisioningv1.ReasonCATrustBundleSyncFailed, fmt.Sprintf("failed to delete truststore cert %s (%s): %v", fingerprint, cert.URI, err))
			return ctrl.Result{}, fmt.Errorf("delete truststore cert %s (%s): %w", fingerprint, cert.URI, err)
		}
		if resp.StatusCode() != http.StatusOK &&
			resp.StatusCode() != http.StatusAccepted &&
			resp.StatusCode() != http.StatusNoContent &&
			resp.StatusCode() != http.StatusNotFound {
			log.Info("Unexpected status while deleting truststore certificate", "dpudevice", dpuDevice.Name, "fingerprint", fingerprint, "uri", cert.URI, "status", resp.Status())
			setCATrustBundleCondition(dpuDevice, metav1.ConditionFalse, provisioningv1.ReasonCATrustBundleSyncFailed, fmt.Sprintf("delete truststore cert %s (%s): unexpected status %s", fingerprint, cert.URI, resp.Status()))
			return ctrl.Result{}, fmt.Errorf("delete truststore cert %s (%s): unexpected status %s", fingerprint, cert.URI, resp.Status())
		}
		log.Info("Removed stale truststore certificate", "fingerprint", fingerprint, "uri", cert.URI)
		removedCount++
	}

	if bundleHash != "" {
		dpuDevice.Status.CATrustBundle.ObservedBundleHash = ptr.To(bundleHash)
	}
	now := metav1.Now()
	dpuDevice.Status.CATrustBundle.LastUpdateTime = &now

	setCATrustBundleCondition(dpuDevice, metav1.ConditionTrue, provisioningv1.ReasonCATrustBundleSynced, "")
	log.Info("Completed BMC CA trust bundle sync",
		"dpudevice", dpuDevice.Name,
		"desired_hash", bundleHash,
		"installed", installedCount,
		"removed", removedCount,
	)
	// No periodic requeue: the controller watches the CA trust bundle ConfigMap, so a bundle change
	// re-triggers reconciliation.
	return ctrl.Result{}, nil
}

func setCATrustBundleCondition(dpuDevice *provisioningv1.DPUDevice, status metav1.ConditionStatus, reason, message string) {
	conditionsList := dpuDevice.GetConditions()
	meta.SetStatusCondition(&conditionsList, metav1.Condition{
		Type:               string(provisioningv1.ConditionDpuDeviceCATrustBundleReady),
		Status:             status,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: dpuDevice.GetGeneration(),
	})
	dpuDevice.SetConditions(conditionsList)
}

func parseDesiredCATrustBundle(bundlePEM string) ([]desiredCATrustCert, error) {
	remaining := []byte(bundlePEM)
	seen := map[string]struct{}{}
	ret := []desiredCATrustCert{}

	for {
		block, rest := pem.Decode(remaining)
		if block == nil {
			break
		}
		remaining = rest
		if block.Type != "CERTIFICATE" {
			continue
		}

		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("failed parsing certificate in bundle: %w", err)
		}
		fingerprintRaw := sha256.Sum256(cert.Raw)
		fingerprint := fmt.Sprintf("%x", fingerprintRaw)
		if _, found := seen[fingerprint]; found {
			continue
		}
		seen[fingerprint] = struct{}{}
		ret = append(ret, desiredCATrustCert{
			pem:         string(pem.EncodeToMemory(block)),
			fingerprint: fingerprint,
		})
	}
	if len(ret) == 0 {
		return nil, fmt.Errorf("trust bundle does not contain any PEM certificates")
	}
	return ret, nil
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

	// The mode is needed before the chassis check below, which tolerates an unknown DPU type only in NIC mode.
	if err := r.refreshDPUMode(ctx, dpuDevice, client); err != nil {
		return err
	}

	resp, chassisInfo, err := client.GetChassis()
	if err != nil {
		log.Error(err, "Failed to get chassis info", "address", bmcAddress, "response", rfclient.RespBody(resp))
		return err
	}

	dpuDevice.Status.DPUType = chassisInfo.GetBlueFieldVersion()
	if dpuDevice.Status.DPUMode == provisioningv1.DpuMode && dpuDevice.Status.DPUType == provisioningv1.DPUTypeUnknown {
		err = fmt.Errorf("unknown DPU type")
		log.Error(err, "Failed to get DPU type", "address", bmcAddress, "response", rfclient.RespBody(resp))
		return err
	}

	if chassisInfo.SerialNumber == "" {
		err = fmt.Errorf("serial number is empty")
		log.Error(err, "Failed to get chassis info", "address", bmcAddress, "response", rfclient.RespBody(resp))
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
		log.Error(err, "Failed to get network device function", "address", bmcAddress, "response", rfclient.RespBody(resp))
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
		log.Info("No MAC address found for PF0", "address", bmcAddress, "response", rfclient.RespBody(resp))
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

// refreshDPUMode records the mode (DPU or NIC) the BMC currently reports.
func (r *DPUDeviceReconciler) refreshDPUMode(ctx context.Context, dpuDevice *provisioningv1.DPUDevice, client *rfclient.Client) error {
	resp, productDescription, err := client.GetProductDescription()
	if err != nil {
		log.FromContext(ctx).Error(err, "Failed to get product description", "address", dpuDevice.BMCAddress(), "response", rfclient.RespBody(resp))
		return err
	}

	switch {
	case productDescription.Mode != nil && *productDescription.Mode == rfclient.NicMode:
		dpuDevice.Status.DPUMode = provisioningv1.NicMode
	default:
		dpuDevice.Status.DPUMode = provisioningv1.DpuMode
	}

	return nil
}

func checkBMCManagerDateTimeSync(ctx context.Context, client *rfclient.Client) error {
	log := log.FromContext(ctx)
	_, bmcManager, err := client.GetBmcManager()
	if err != nil {
		log.Error(err, "Failed to get BMC manager")
		return err
	}
	bmcManagerDateTime, err := time.Parse(time.RFC3339, bmcManager.DateTime)
	if err != nil {
		log.Error(err, "Failed to parse BMC manager date time")
		return err
	}
	if time.Since(bmcManagerDateTime).Abs() > bmcManagerDateTimeSyncThreshold {
		err = fmt.Errorf("BMC manager date time is unsynchronized with system time")
		log.Error(err, "BMC manager date time is unsynchronized with system time", "bmcManagerDateTime", bmcManagerDateTime, "time", time.Now())
		return err
	}
	return nil
}

// reconcileDynamicFields refreshes the status fields that can change on the device after discovery,
// so that the DPUDevice keeps reflecting the current state of the hardware.
func (r *DPUDeviceReconciler) reconcileDynamicFields(ctx context.Context, dpuDevice *provisioningv1.DPUDevice) error {
	log := log.FromContext(ctx)
	bmcAddress := dpuDevice.BMCAddress()
	client, err := rfclient.NewTLSClient(ctx, bmcAddress, dpuDevice.Namespace, r.Client)
	if err != nil {
		log.Error(err, "Failed to create TLS client")
		return err
	}

	if err := r.refreshDPUMode(ctx, dpuDevice, client); err != nil {
		return err
	}

	resp, secureBootInfo, err := client.GetSecureBoot()
	if err != nil {
		log.Error(err, "Failed to get Secure Boot state", "address", bmcAddress, "response", rfclient.RespBody(resp))
		return err
	}

	if secureBootInfo != nil {
		enabled := secureBootInfo.IsCurrentlyActive()
		dpuDevice.Status.SecureBoot = &provisioningv1.SecureBootStatus{
			Enabled: ptr.To(enabled),
		}
		log.Info("Detected Secure Boot state", "address", bmcAddress, "enabled", enabled)
	}

	if err := checkBMCManagerDateTimeSync(ctx, client); err != nil {
		return err
	}

	return nil
}

// setUpMTLS sets up BMC mTLS in the same way as https://github.com/openbmc/bmcweb/blob/master/scripts/generate_auth_certificates.py
func (r *DPUDeviceReconciler) setUpMTLS(ctx context.Context, dpudevice *provisioningv1.DPUDevice, basicAuthClient *rfclient.Client) (bool, error) {
	caCert, err := r.getBootstrapCACert(ctx, dpudevice.Namespace)
	if err != nil {
		return false, fmt.Errorf("failed to get bootstrap CA cert: %w", err)
	}

	// step 1: reconcile the DPF CA in the BMC truststore by fingerprint (no hardcoded /1).
	if err := r.installBootstrapCA(ctx, basicAuthClient, string(caCert)); err != nil {
		return true, err
	}

	// step 2: replace server certificate
	log.FromContext(ctx).Info("Replace server certificate...")
	cr := newServerCertRequest(dpudevice)
	err = r.Client.Get(ctx, types.NamespacedName{Name: cr.GetName(), Namespace: cr.GetNamespace()}, cr)
	if err != nil {
		if apierrors.IsNotFound(err) {
			log.FromContext(ctx).Info("cert-manager CertificateRequest does not exist, try create one...")
			return r.createServerCertFromCSR(ctx, dpudevice, basicAuthClient)
		}
		return false, fmt.Errorf("failed to get existing cert-manager CertificateRequest, err: %v", err)
	}

	certificate, found, err := unstructured.NestedString(cr.Object, "status", "certificate")
	if err != nil {
		return false, fmt.Errorf("failed to extract certificate %w", err)
	}
	if !found {
		if failErr := certRequestFailure(cr); failErr != nil {
			return false, failErr
		}
		return false, fmt.Errorf("cert-manager CertificateRequest is not issued yet, retry later")
	}

	decodedCert, err := b64.StdEncoding.DecodeString(certificate)
	if err != nil {
		return false, fmt.Errorf("failed to base64 decode certificate %w", err)
	}

	// An existing CertificateRequest is immutable: if it no longer verifies for the current BMC
	// (wrong CA after upgrade, expired leaf, or identity mismatch after BMC IP change),
	// reinstalling it leaves mTLS broken forever. Drop it and regenerate against the current BMC.
	expectedHost := ""
	if dpudevice.Status.BMCIP != nil {
		expectedHost = *dpudevice.Status.BMCIP
	}
	if err := issuedServerCertUsableForBMC(decodedCert, caCert, expectedHost); err != nil {
		log.FromContext(ctx).Info("stale server CertificateRequest is not usable for current BMC; regenerating", "err", err)
		if delErr := r.Client.Delete(ctx, cr); delErr != nil && !apierrors.IsNotFound(delErr) {
			return false, fmt.Errorf("failed to delete stale server CertificateRequest: %w", delErr)
		}
		return r.createServerCertFromCSR(ctx, dpudevice, basicAuthClient)
	}

	resp, _, err := basicAuthClient.ReplaceServerCert(string(decodedCert))
	if err != nil {
		return true, fmt.Errorf("failed to replace server cert, err: %v", err)
	} else if resp.StatusCode() != http.StatusOK {
		// The BMC rejected the issued certificate. The most common cause is a key mismatch: GenerateCSR
		// creates a fresh pending key pair on the BMC, and that key is dropped if the BMC reboots (e.g.
		// during provisioning) before the certificate is installed. Delete the stale CertificateRequest
		// so the next pass regenerates a CSR against the BMC's current key instead of retrying the same
		// orphaned certificate forever.
		if delErr := r.Client.Delete(ctx, cr); delErr != nil && !apierrors.IsNotFound(delErr) {
			log.FromContext(ctx).Error(delErr, "failed to delete stale server CertificateRequest after install failure")
		}
		// Retryable: delete the CR so the next pass regenerates a CSR against the BMC's current
		// key. Do not request a BMC factory reset — key mismatch is recovered by re-issuing.
		return false, fmt.Errorf("failed to replace server cert, unexpected response status: %s", resp.Status())
	}
	log.FromContext(ctx).Info("Successfully replaced server certificate")

	// The BMC validates the controller's client certificate by chaining it to the CA already in
	// its truststore, so the client leaf does not need to be installed on the BMC.

	// step 3: enable mTLS
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

// installBootstrapCA reconciles the BMC truststore to hold the desired DPF CA bundle over the
// basic-auth channel used during mTLS bootstrap and recovery. It diffs installed vs desired certs by
// SHA-256 fingerprint, deletes truststore certs that are no longer desired, and then installs any
// desired CA that is missing. This avoids the old InstallCert→ReplaceCACert(.../Certificates/1)
// sequence, which fails with HTTP 500 when the desired CA is already present or lives at a different
// index. Stale certs are removed before new ones are installed because the BMC truststore accepts at
// most 10 certificates; deleting first frees slots so the desired CA install cannot be rejected when
// the truststore is already full.
func (r *DPUDeviceReconciler) installBootstrapCA(ctx context.Context, basicAuthClient *rfclient.Client, caBundlePEM string) error {
	log := log.FromContext(ctx)

	desiredCerts, err := parseDesiredCATrustBundle(caBundlePEM)
	if err != nil {
		return fmt.Errorf("failed to parse bootstrap CA bundle: %w", err)
	}

	installedCerts, err := basicAuthClient.ListTruststoreCerts()
	if err != nil {
		return fmt.Errorf("failed to list BMC truststore certificates: %w", err)
	}

	desiredByFingerprint := make(map[string]desiredCATrustCert, len(desiredCerts))
	for _, cert := range desiredCerts {
		desiredByFingerprint[cert.fingerprint] = cert
	}
	installedByFingerprint := make(map[string]rfclient.TruststoreCert, len(installedCerts))
	for _, cert := range installedCerts {
		installedByFingerprint[cert.Fingerprint] = cert
	}

	// Delete stale certs first: the BMC truststore accepts at most 10 certificates, so freeing
	// slots before installing the desired CA prevents the install from being rejected when the
	// truststore is already at capacity.
	for fingerprint, cert := range installedByFingerprint {
		if _, keep := desiredByFingerprint[fingerprint]; keep {
			continue
		}
		resp, _, err := basicAuthClient.DeleteTruststoreCert(cert.URI)
		if err != nil {
			return fmt.Errorf("failed to delete stale CA cert %s (%s) from BMC truststore: %w", fingerprint, cert.URI, err)
		}
		if resp.StatusCode() != http.StatusOK &&
			resp.StatusCode() != http.StatusAccepted &&
			resp.StatusCode() != http.StatusNoContent &&
			resp.StatusCode() != http.StatusNotFound {
			return fmt.Errorf("failed to delete stale CA cert %s (%s) from BMC truststore, unexpected response status: %s", fingerprint, cert.URI, resp.Status())
		}
		log.Info("Removed stale CA certificate from BMC truststore", "fingerprint", fingerprint, "uri", cert.URI)
	}

	for fingerprint, cert := range desiredByFingerprint {
		if _, found := installedByFingerprint[fingerprint]; found {
			continue
		}
		resp, _, err := basicAuthClient.InstallCert(cert.pem)
		if err != nil {
			return fmt.Errorf("failed to install CA cert %s in BMC truststore: %w", fingerprint, err)
		}
		if resp.StatusCode() != http.StatusOK {
			return fmt.Errorf("failed to install CA cert %s in BMC truststore, unexpected response status: %s", fingerprint, resp.Status())
		}
		log.Info("Installed DPF CA certificate in BMC truststore", "fingerprint", fingerprint)
	}

	return nil
}

// createServerCertFromCSR asks the BMC for a fresh CSR and creates the fixed-name CertificateRequest.
// Issuance is asynchronous, so it always returns a retryable "not issued yet" error on success.
func (r *DPUDeviceReconciler) createServerCertFromCSR(ctx context.Context, dpudevice *provisioningv1.DPUDevice, basicAuthClient *rfclient.Client) (bool, error) {
	if dpudevice.Status.BMCIP == nil {
		return false, fmt.Errorf("cannot generate CSR: DPUDevice %s has no BMCIP set", dpudevice.Name)
	}
	resp, csrInfo, err := basicAuthClient.GenerateCSR(*dpudevice.Status.BMCIP)
	if err != nil {
		return true, fmt.Errorf("failed to generate CSR, err: %v", err)
	} else if resp.StatusCode() != http.StatusOK {
		return true, fmt.Errorf("failed to generate CSR, unexpected response status: %s", resp.Status())
	}
	if err := r.createServerCertCR(ctx, dpudevice, csrInfo.CSRString); err != nil {
		return false, fmt.Errorf("failed to create cert-manager CertificateRequest, err: %v", err)
	}
	log.FromContext(ctx).Info("successfully created cert-manager CertificateRequest")
	return false, fmt.Errorf("cert-manager CertificateRequest is not issued yet, retry later")
}

// issuedServerCertUsableForBMC reports whether certPEM (the leaf from a CertificateRequest status)
// can be installed for the current BMC: it must chain to the current DPF CA trust bundle and pin
// expectedHost (BMC IP) as an IP SAN, DNS SAN, or Common Name. A failure means the
// CertificateRequest must be regenerated.
//
// x509.Verify also rejects expired certificates. That is intentional here: setUpMTLS only runs when
// NewTLSClient already failed (wrong CA, expired leaf, or identity mismatch), and
// reconcileServerCertRotation cannot renew without a working mTLS connection, so bootstrap must
// reissue in all of those cases.
func issuedServerCertUsableForBMC(certPEM, caBundlePEM []byte, expectedHost string) error {
	block, _ := pem.Decode(certPEM)
	if block == nil {
		return fmt.Errorf("no PEM certificate in CertificateRequest status")
	}
	leaf, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return fmt.Errorf("failed to parse CertificateRequest certificate: %w", err)
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(caBundlePEM) {
		return fmt.Errorf("failed to load current DPF CA pool")
	}
	if _, err := leaf.Verify(x509.VerifyOptions{Roots: roots}); err != nil {
		return fmt.Errorf("issued server certificate does not chain to current DPF CA: %w", err)
	}
	if err := rfclient.VerifyBMCIdentity(leaf, expectedHost); err != nil {
		return err
	}
	return nil
}

func (r *DPUDeviceReconciler) getBootstrapCACert(ctx context.Context, namespace string) ([]byte, error) {
	caTrustBundle := &corev1.ConfigMap{}
	if err := r.Client.Get(ctx, types.NamespacedName{Name: rfclient.CATrustBundleConfigMap, Namespace: namespace}, caTrustBundle); err != nil {
		return nil, fmt.Errorf("failed to get ConfigMap %q: %w", rfclient.CATrustBundleConfigMap, err)
	}
	bundle := []byte(caTrustBundle.Data[rfclient.CATrustBundleKey])
	if len(bundle) == 0 {
		return nil, fmt.Errorf("configmap %s is missing %q", rfclient.CATrustBundleConfigMap, rfclient.CATrustBundleKey)
	}
	if _, err := parseDesiredCATrustBundle(string(bundle)); err != nil {
		return nil, fmt.Errorf("configmap %s contains no valid certificate PEM: %w", rfclient.CATrustBundleConfigMap, err)
	}
	return bundle, nil
}

// generateCR builds a cert-manager CertificateRequest for the BMC server certificate. The CR uses
// a fixed, deterministic name (<dpudevice.Name>-server) and is owner-referenced to the DPUDevice so
// cert-manager objects are cleaned up on deletion.
func (r *DPUDeviceReconciler) generateCR(dpudevice *provisioningv1.DPUDevice, csr string) (*unstructured.Unstructured, error) {
	cr := &unstructured.Unstructured{}
	cr.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   certManagerGroup,
		Version: "v1",
		Kind:    "CertificateRequest",
	})
	cr.SetName(cutil.GenerateBMCServerCertRequestName(dpudevice.Name))
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
			Duration: serverCertDuration,
		}.ToUnstructured(),
		"issuerRef": map[string]interface{}{
			"name":  rfclient.Issuer,
			"kind":  "Issuer",
			"group": certManagerGroup,
		},
	}, "spec")
	if err != nil {
		return nil, fmt.Errorf("failed to generate spec to CertificateRequest: %w", err)
	}
	return cr, nil
}

// createServerCertCR creates the BMC server-certificate cert-manager CertificateRequest for the
// given BMC CSR. It is the single creation path shared by both mTLS bootstrap and rotation.
func (r *DPUDeviceReconciler) createServerCertCR(ctx context.Context, dpudevice *provisioningv1.DPUDevice, csr string) error {
	cr, err := r.generateCR(dpudevice, csr)
	if err != nil {
		return err
	}
	return r.Client.Create(ctx, cr)
}

// newServerCertRequest returns an unstructured CertificateRequest scoped to the device's fixed name.
func newServerCertRequest(dpudevice *provisioningv1.DPUDevice) *unstructured.Unstructured {
	cr := &unstructured.Unstructured{}
	cr.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   certManagerGroup,
		Version: "v1",
		Kind:    "CertificateRequest",
	})
	cr.SetName(cutil.GenerateBMCServerCertRequestName(dpudevice.Name))
	cr.SetNamespace(dpudevice.Namespace)
	return cr
}

// bmcServerCertRenewBefore returns the configured renew-before window, falling back to the default
// when DPFOperatorConfig does not set it, and clamps it so it stays below the cert duration.
func bmcServerCertRenewBefore(cfg *operatorv1.DPFOperatorConfig) time.Duration {
	renewBefore := defaultBMCServerCertRenewBefore
	if cfg != nil && cfg.Spec.ProvisioningController != nil && cfg.Spec.ProvisioningController.BMCServerCertRenewBefore != nil {
		renewBefore = cfg.Spec.ProvisioningController.BMCServerCertRenewBefore.Duration
	}
	if renewBefore <= 0 || renewBefore >= serverCertDuration {
		renewBefore = serverCertDuration / 2
	}
	return renewBefore
}

// certNotAfter decodes the PEM block of the given certificate and returns its NotAfter time.
func certNotAfter(certPEM string) (time.Time, error) {
	block, _ := pem.Decode([]byte(certPEM))
	if block == nil {
		return time.Time{}, fmt.Errorf("no PEM block found in certificate")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return time.Time{}, fmt.Errorf("failed to parse certificate: %w", err)
	}
	return cert.NotAfter, nil
}

// requeueUntil returns how long to wait before the next rotation check: the time until the renew
// boundary, floored at minServerCertRequeue and capped at maxServerCertRequeue.
func requeueUntil(notAfter time.Time, renewBefore time.Duration) time.Duration {
	untilRenew := time.Until(notAfter.Add(-renewBefore))
	if untilRenew < minServerCertRequeue {
		return minServerCertRequeue
	}
	if untilRenew > maxServerCertRequeue {
		return maxServerCertRequeue
	}
	return untilRenew
}

// reconcileServerCertRotation detects BMC mTLS server-certificate expiry and renews it before it
// expires or on a manual annotation trigger. It runs only after the device is Ready.
func (r *DPUDeviceReconciler) reconcileServerCertRotation(ctx context.Context, dpuDevice *provisioningv1.DPUDevice, renewBefore time.Duration) (ctrl.Result, error) {
	log := log.FromContext(ctx)

	if dpuDevice.Status.BMCServerCertificate == nil {
		dpuDevice.Status.BMCServerCertificate = &provisioningv1.CertificateStatus{}
	}
	serverCertStatus := dpuDevice.Status.BMCServerCertificate

	// Capture whether a rotation was already in progress before we mutate the condition below, so a
	// fresh rotation regenerates the CSR/CR while in-progress rotations only wait for issuance.
	rotationInProgress := false
	if cond := conditions.Get(dpuDevice, provisioningv1.ConditionDpuDeviceBMCServerCertificateReady); cond != nil &&
		cond.Status == metav1.ConditionFalse && cond.Reason == provisioningv1.ReasonBMCServerCertificateRotating {
		rotationInProgress = true
	}

	manualTrigger := dpuDevice.Annotations[provisioningv1.RotateBMCServerCertificateAnnotation]
	observedTrigger := ""
	if serverCertStatus.ObservedManualTrigger != nil {
		observedTrigger = *serverCertStatus.ObservedManualTrigger
	}
	manualRequested := manualTrigger != "" && manualTrigger != observedTrigger

	notAfter := serverCertStatus.NotAfter
	inWindow := notAfter != nil && time.Now().After(notAfter.Time.Add(-renewBefore))

	// Nothing to do: expiry known, outside the renew window, no manual request, and no pending rotation.
	if notAfter != nil && !inWindow && !manualRequested && !rotationInProgress {
		conditions.AddTrue(dpuDevice, provisioningv1.ConditionDpuDeviceBMCServerCertificateReady)
		return ctrl.Result{RequeueAfter: requeueUntil(notAfter.Time, renewBefore)}, nil
	}

	// cold-start backfill and/or rotation.
	bmcAddress := dpuDevice.BMCAddress()
	mtlsClient, err := rfclient.NewTLSClient(ctx, bmcAddress, dpuDevice.Namespace, r.Client)
	if err != nil {
		// A server-certificate verification failure cannot be healed over mTLS: rotation needs a
		// verified mTLS connection, which is exactly what is broken. Re-run setUpMTLS over basic
		// auth only — do not clear Initialized / re-enter initializeDPUDevice, which can factory-
		// reset the BMC on ReplaceServerCert failures.
		if rfclient.IsBMCServerCertUntrusted(err) {
			return r.recoverServerCert(ctx, dpuDevice, serverCertStatus, err)
		}
		if rfclient.IsRedfishClientCertStale(err) {
			return r.recoverRedfishClientCert(ctx, dpuDevice, err)
		}
		log.Error(err, "failed to open mTLS client for server cert rotation")
		conditions.AddFalse(dpuDevice, provisioningv1.ConditionDpuDeviceBMCServerCertificateReady,
			conditions.ConditionReason(provisioningv1.ReasonBMCServerCertificateRotationFailed),
			conditions.ConditionMessage(fmt.Sprintf("failed to open mTLS client: %v", err)))
		return ctrl.Result{RequeueAfter: serverCertRotationBackoff}, nil
	}

	// Cold start: backfill the expiry from the BMC instead of forcing a fleet-wide rotation.
	// Skip it when a rotation is already in progress: notAfter is recorded only once rotation
	// completes, so backfilling here would re-fetch the (old) served cert on every requeue while we
	// wait for issuance.
	if notAfter == nil && !manualRequested && !rotationInProgress {
		if handled, result := r.backfillServerCertExpiry(ctx, dpuDevice, mtlsClient, renewBefore); handled {
			return result, nil
		}
	}

	// Rotation needed.
	conditions.AddFalse(dpuDevice, provisioningv1.ConditionDpuDeviceBMCServerCertificateReady,
		conditions.ConditionReason(provisioningv1.ReasonBMCServerCertificateRotating),
		conditions.ConditionMessage("BMC mTLS server certificate rotation in progress"))

	issuedNotAfter, err := r.rotateServerCert(ctx, dpuDevice, mtlsClient, rotationInProgress)
	if err != nil {
		if errors.Is(err, errCertRequestPending) {
			log.Info("waiting for BMC server certificate to be issued")
			return ctrl.Result{RequeueAfter: serverCertIssuanceRequeue}, nil
		}
		conditions.AddFalse(dpuDevice, provisioningv1.ConditionDpuDeviceBMCServerCertificateReady,
			conditions.ConditionReason(provisioningv1.ReasonBMCServerCertificateRotationFailed),
			conditions.ConditionMessage(err.Error()))
		return ctrl.Result{}, err
	}

	serverCertStatus.NotAfter = issuedNotAfter
	serverCertStatus.LastRotationTime = ptr.To(metav1.Now())
	if manualRequested {
		serverCertStatus.ObservedManualTrigger = ptr.To(manualTrigger)
	}
	conditions.AddTrue(dpuDevice, provisioningv1.ConditionDpuDeviceBMCServerCertificateReady)
	log.Info("rotated BMC server certificate", "notAfter", issuedNotAfter.Time)
	return ctrl.Result{RequeueAfter: requeueUntil(issuedNotAfter.Time, renewBefore)}, nil
}

// recoverRedfishClientCert forces cert-manager to reissue the shared Redfish client certificates
// when the mounted leaf no longer chains to the current DPF CA (typical after CA replacement).
// Deleting the target Secrets is the standard cert-manager force-renew path; the Certificate
// resources recreate them. The controller re-reads the mounted files on the next NewTLSClient call.
func (r *DPUDeviceReconciler) recoverRedfishClientCert(ctx context.Context, dpuDevice *provisioningv1.DPUDevice, cause error) (ctrl.Result, error) {
	log := log.FromContext(ctx)
	log.Info("Redfish client certificate does not chain to current DPF CA; forcing reissue", "err", cause.Error())

	if err := r.forceReissueRedfishClientCerts(ctx, dpuDevice.Namespace); err != nil {
		conditions.AddFalse(dpuDevice, provisioningv1.ConditionDpuDeviceBMCServerCertificateReady,
			conditions.ConditionReason(provisioningv1.ReasonBMCServerCertificateRotationFailed),
			conditions.ConditionMessage(fmt.Sprintf("failed to reissue Redfish client certificate: %v", err)))
		return ctrl.Result{RequeueAfter: serverCertRotationBackoff}, nil
	}

	conditions.AddFalse(dpuDevice, provisioningv1.ConditionDpuDeviceBMCServerCertificateReady,
		conditions.ConditionReason(provisioningv1.ReasonRedfishClientCertStale),
		conditions.ConditionMessage("Redfish client certificate reissue in progress; waiting for mounted secret to update"))
	return ctrl.Result{RequeueAfter: serverCertIssuanceRequeue}, nil
}

// forceReissueRedfishClientCerts deletes the Redfish client-cert Secrets so cert-manager Certificate
// controllers recreate them under the current Issuer/CA. Both BF3 and BF4 secrets are renewed: after
// a CA replacement either (or both) can be stale, and renewal is rare and idempotent.
func (r *DPUDeviceReconciler) forceReissueRedfishClientCerts(ctx context.Context, namespace string) error {
	for _, name := range []string{rfclient.ClientCertSecret, rfclient.ClientCertSecretBF4} {
		secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace}}
		if err := r.Client.Delete(ctx, secret); err != nil && !apierrors.IsNotFound(err) {
			return fmt.Errorf("failed to delete Secret %s/%s: %w", namespace, name, err)
		}
	}
	return nil
}

// recoverServerCert re-bootstraps BMC mTLS for an already-initialized DPUDevice whose server
// certificate no longer verifies against the DPF CA. It runs setUpMTLS over basic auth only and
// never clears Initialized / re-enters initializeDPUDevice, so ReplaceServerCert failures cannot
// trigger a BMC factory reset.
func (r *DPUDeviceReconciler) recoverServerCert(ctx context.Context, dpuDevice *provisioningv1.DPUDevice, serverCertStatus *provisioningv1.CertificateStatus, verifyErr error) (ctrl.Result, error) {
	log := log.FromContext(ctx)
	log.Info("BMC server certificate failed verification; re-running setUpMTLS over basic auth", "verifyErr", verifyErr.Error())

	basicAuthClient, err := r.basicAuthClientForRecovery(ctx, dpuDevice)
	if err != nil {
		log.Error(err, "failed to authenticate to BMC for mTLS recovery")
		conditions.AddFalse(dpuDevice, provisioningv1.ConditionDpuDeviceBMCServerCertificateReady,
			conditions.ConditionReason(provisioningv1.ReasonBMCServerCertificateRotationFailed),
			conditions.ConditionMessage(fmt.Sprintf("failed to authenticate to BMC for mTLS recovery: %v", err)))
		return ctrl.Result{RequeueAfter: serverCertRotationBackoff}, nil
	}

	// Ignore needBmcReset: recovery must not factory-reset. ReplaceServerCert key mismatches are
	// healed by deleting the CertificateRequest (done inside setUpMTLS) and retrying.
	if _, err := r.setUpMTLS(ctx, dpuDevice, basicAuthClient); err != nil {
		log.Info("BMC mTLS recovery not complete yet", "err", err.Error())
		conditions.AddFalse(dpuDevice, provisioningv1.ConditionDpuDeviceBMCServerCertificateReady,
			conditions.ConditionReason(provisioningv1.ReasonBMCServerCertificateUntrusted),
			conditions.ConditionMessage(fmt.Sprintf("BMC mTLS recovery in progress: %v", err)))
		return ctrl.Result{RequeueAfter: serverCertIssuanceRequeue}, nil
	}

	// Fresh CA-valid server certificate installed. Clear expiry so the next reconcile backfills
	// notAfter over verified mTLS.
	serverCertStatus.NotAfter = nil
	conditions.AddFalse(dpuDevice, provisioningv1.ConditionDpuDeviceBMCServerCertificateReady,
		conditions.ConditionReason(provisioningv1.ReasonBMCServerCertificateRotating),
		conditions.ConditionMessage("BMC mTLS re-bootstrapped; verifying"))
	log.Info("BMC mTLS setUpMTLS recovery succeeded")
	return ctrl.Result{RequeueAfter: serverCertIssuanceRequeue}, nil
}

// basicAuthClientForRecovery resolves the current BMC credential and returns a basic-auth Redfish
// client. Unlike resolveAndAuthenticateBMC(..., alreadyInitialized=true), it always returns a usable
// client even when the shared credential is unchanged, because recovery must talk to the BMC over
// basic auth when mTLS is broken.
func (r *DPUDeviceReconciler) basicAuthClientForRecovery(ctx context.Context, dpuDevice *provisioningv1.DPUDevice) (*rfclient.Client, error) {
	cred, err := rfclient.ResolveBMCCredential(ctx, dpuDevice.Namespace, dpuDevice.Spec.BMCCredentialSecretName, r.Client)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve BMC credential: %w", err)
	}
	basicAuthClient, _, err := rfclient.VerifyBMCCredential(dpuDevice.BMCAddress(), cred.Password)
	if err != nil {
		return nil, fmt.Errorf("failed to authenticate to BMC over basic auth: %w", err)
	}
	return basicAuthClient, nil
}

// backfillServerCertExpiry handles the cold-start case where the BMC already serves a server
// certificate but its expiry has not been recorded yet. It reads the served certificate's
// expiry from the BMC and records it on the status instead of forcing a rotation, so an upgraded
// fleet does not rotate all at once.
// It returns handled=true with the result the reconcile should return when the backfilled
// certificate is still outside its renew window. It returns handled=false when the caller should
// fall through to a rotation: the fetch failed, the certificate could not be parsed, or the
// backfilled expiry is already within the renew window.
func (r *DPUDeviceReconciler) backfillServerCertExpiry(ctx context.Context, dpuDevice *provisioningv1.DPUDevice, mtlsClient *rfclient.Client, renewBefore time.Duration) (bool, ctrl.Result) {
	log := log.FromContext(ctx)

	_, info, err := mtlsClient.GetServerCert()
	if err != nil {
		log.Error(err, "failed to fetch served BMC server certificate; will rotate")
		return false, ctrl.Result{}
	}
	if info == nil || info.CertificateString == "" {
		return false, ctrl.Result{}
	}
	notAfter, err := certNotAfter(info.CertificateString)
	if err != nil {
		log.Error(err, "failed to parse served BMC server certificate; will rotate")
		return false, ctrl.Result{}
	}

	dpuDevice.Status.BMCServerCertificate.NotAfter = &metav1.Time{Time: notAfter}
	log.Info("backfilled BMC server certificate expiry", "notAfter", notAfter)

	// Already within the renew window: let the caller fall through to a rotation.
	if time.Now().After(notAfter.Add(-renewBefore)) {
		return false, ctrl.Result{}
	}

	conditions.AddTrue(dpuDevice, provisioningv1.ConditionDpuDeviceBMCServerCertificateReady)
	return true, ctrl.Result{RequeueAfter: requeueUntil(notAfter, renewBefore)}
}

// rotateServerCert re-runs the server-certificate issuance steps over the mTLS client, recreating a
// single fixed-name CertificateRequest. When a rotation is freshly decided it generates a new CSR on
// the BMC and recreates the CR; while a rotation is in progress it only waits for issuance and then
// installs the issued certificate. It returns the issued certificate's NotAfter on success, or
// errCertRequestPending while waiting for cert-manager to sign the request.
func (r *DPUDeviceReconciler) rotateServerCert(ctx context.Context, dpuDevice *provisioningv1.DPUDevice, mtlsClient *rfclient.Client, rotationInProgress bool) (*metav1.Time, error) {
	cr := newServerCertRequest(dpuDevice)
	getErr := r.Client.Get(ctx, types.NamespacedName{Name: cr.GetName(), Namespace: cr.GetNamespace()}, cr)
	if getErr != nil && !apierrors.IsNotFound(getErr) {
		return nil, fmt.Errorf("failed to get cert-manager CertificateRequest: %w", getErr)
	}

	if !rotationInProgress {
		return nil, r.startServerCertRotation(ctx, dpuDevice, mtlsClient, cr, getErr == nil)
	}
	return r.installIssuedServerCert(mtlsClient, cr, getErr)
}

// startServerCertRotation begins a fresh rotation: it deletes any stale CertificateRequest,
// generates a new CSR on the BMC, and recreates the CertificateRequest. crExists reports whether the
// CR was found by the caller. Issuance is asynchronous, so it returns errCertRequestPending on success.
func (r *DPUDeviceReconciler) startServerCertRotation(ctx context.Context, dpuDevice *provisioningv1.DPUDevice, mtlsClient *rfclient.Client, cr *unstructured.Unstructured, crExists bool) error {
	log := log.FromContext(ctx)

	if crExists {
		if err := r.Client.Delete(ctx, cr); err != nil && !apierrors.IsNotFound(err) {
			return fmt.Errorf("failed to delete existing CertificateRequest: %w", err)
		}
	}
	if dpuDevice.Status.BMCIP == nil {
		return fmt.Errorf("cannot generate CSR: DPUDevice %s has no BMCIP set", dpuDevice.Name)
	}
	resp, csrInfo, err := mtlsClient.GenerateCSR(*dpuDevice.Status.BMCIP)
	if err != nil {
		return fmt.Errorf("failed to generate CSR: %w", err)
	} else if resp.StatusCode() != http.StatusOK {
		return fmt.Errorf("failed to generate CSR, unexpected response status: %s", resp.Status())
	}
	if err := r.createServerCertCR(ctx, dpuDevice, csrInfo.CSRString); err != nil {
		return fmt.Errorf("failed to create cert-manager CertificateRequest: %w", err)
	}
	log.Info("created cert-manager CertificateRequest for server cert rotation", "name", cr.GetName())
	return errCertRequestPending
}

// installIssuedServerCert completes an in-progress rotation: it waits for cert-manager to sign the
// CertificateRequest, then installs the issued certificate on the BMC and returns its NotAfter.
// getErr is the result of the caller's CR Get. It returns errCertRequestPending while issuance is
// still pending.
func (r *DPUDeviceReconciler) installIssuedServerCert(mtlsClient *rfclient.Client, cr *unstructured.Unstructured, getErr error) (*metav1.Time, error) {
	if apierrors.IsNotFound(getErr) {
		// CR vanished mid-rotation; let the next reconcile start a fresh rotation.
		return nil, fmt.Errorf("CertificateRequest %s not found during rotation", cr.GetName())
	}
	certificate, found, err := unstructured.NestedString(cr.Object, "status", "certificate")
	if err != nil {
		return nil, fmt.Errorf("failed to read CertificateRequest status: %w", err)
	}
	if !found || certificate == "" {
		// Distinguish a still-pending request from one cert-manager has terminally rejected, so a
		// denied/failed request surfaces as a failure instead of looping as "in progress" forever.
		if failErr := certRequestFailure(cr); failErr != nil {
			return nil, failErr
		}
		return nil, errCertRequestPending
	}

	decodedCert, err := b64.StdEncoding.DecodeString(certificate)
	if err != nil {
		return nil, fmt.Errorf("failed to base64 decode certificate: %w", err)
	}
	resp, _, err := mtlsClient.ReplaceServerCert(string(decodedCert))
	if err != nil {
		return nil, fmt.Errorf("failed to replace server cert: %w", err)
	} else if resp.StatusCode() != http.StatusOK {
		return nil, fmt.Errorf("failed to replace server cert, unexpected response status: %s", resp.Status())
	}

	na, err := certNotAfter(string(decodedCert))
	if err != nil {
		return nil, fmt.Errorf("failed to read issued certificate expiry: %w", err)
	}
	return &metav1.Time{Time: na}, nil
}

// certRequestFailure inspects a cert-manager CertificateRequest's conditions and returns a non-nil
// terminal error when issuance has failed: Denied=True (rejected by an approver), InvalidRequest=True
// (malformed request), or Ready=False with a non-pending reason (issuer failure). It returns nil
// while the request is still legitimately pending (e.g. Ready=False/Pending), so callers keep waiting.
func certRequestFailure(cr *unstructured.Unstructured) error {
	conds, found, err := unstructured.NestedSlice(cr.Object, "status", "conditions")
	if err != nil || !found {
		return nil
	}
	for _, c := range conds {
		cond, ok := c.(map[string]interface{})
		if !ok {
			continue
		}
		condType, _, _ := unstructured.NestedString(cond, "type")
		condStatus, _, _ := unstructured.NestedString(cond, "status")
		reason, _, _ := unstructured.NestedString(cond, "reason")
		message, _, _ := unstructured.NestedString(cond, "message")
		switch condType {
		case "Denied":
			if condStatus == "True" {
				return fmt.Errorf("cert-manager denied the CertificateRequest: %s", message)
			}
		case "InvalidRequest":
			if condStatus == "True" {
				return fmt.Errorf("cert-manager rejected the CertificateRequest as invalid: %s", message)
			}
		case "Ready":
			// cert-manager uses reason "Pending" while issuing and "Failed" on a terminal error.
			if condStatus == "False" && reason == "Failed" {
				return fmt.Errorf("cert-manager failed to issue the CertificateRequest: %s", message)
			}
		}
	}
	return nil
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

// checkBMCCredentialSecretUniqueness rejects sharing a per-device BMC credential secret across
// DPUDevices. Only devices that have not yet adopted the secret in status are blocked, so a
// misconfigured peer does not take down an established device. The blocked device is requeued
// so it recovers after the conflicting reference is removed.
//
// When stop is true the caller should return result and err without further reconciliation.
func (r *DPUDeviceReconciler) checkBMCCredentialSecretUniqueness(ctx context.Context, dpuDevice *provisioningv1.DPUDevice) (ctrl.Result, bool, error) {
	conflictingDevice, err := r.findOtherDeviceUsingBMCCredentialSecret(ctx, dpuDevice)
	if err != nil {
		return ctrl.Result{}, true, err
	}
	if conflictingDevice == nil || hasAdoptedBMCCredentialSecret(dpuDevice) {
		return ctrl.Result{}, false, nil
	}

	secretName := *dpuDevice.Spec.BMCCredentialSecretName
	msg := fmt.Sprintf("BMC credential secret %q is already in use by DPUDevice %s/%s. Per-device credentials must not be shared.",
		secretName, conflictingDevice.Namespace, conflictingDevice.Name)
	conditions.AddFalse(dpuDevice, provisioningv1.ConditionBMCCredentialsReady,
		conditions.ConditionReason(provisioningv1.ReasonSharedCredentialSecretNotAllowed),
		conditions.ConditionMessage(msg))
	return ctrl.Result{RequeueAfter: 30 * time.Second}, true, nil
}

// findOtherDeviceUsingBMCCredentialSecret returns another DPUDevice in the same namespace that
// already references the same per-device BMC credential secret. The shared bmc-shared-password
// secret is intentionally allowed across devices.
func (r *DPUDeviceReconciler) findOtherDeviceUsingBMCCredentialSecret(ctx context.Context, dpuDevice *provisioningv1.DPUDevice) (*provisioningv1.DPUDevice, error) {
	if dpuDevice.Spec.BMCCredentialSecretName == nil {
		return nil, nil
	}
	secretName := *dpuDevice.Spec.BMCCredentialSecretName
	if secretName == "" || secretName == rfclient.BMCPasswordSecret {
		return nil, nil
	}

	dpuDeviceList := &provisioningv1.DPUDeviceList{}
	if err := r.Client.List(ctx, dpuDeviceList, client.InNamespace(dpuDevice.Namespace)); err != nil {
		return nil, fmt.Errorf("failed to list DPUDevices in namespace %q: %w", dpuDevice.Namespace, err)
	}

	for i := range dpuDeviceList.Items {
		candidate := &dpuDeviceList.Items[i]
		if candidate.Name == dpuDevice.Name || !candidate.DeletionTimestamp.IsZero() {
			continue
		}
		if candidate.Spec.BMCCredentialSecretName != nil && *candidate.Spec.BMCCredentialSecretName == secretName {
			return candidate, nil
		}
	}
	return nil, nil
}

// hasAdoptedBMCCredentialSecret reports whether the device has already adopted its current
// per-device BMC credential secret in status.
func hasAdoptedBMCCredentialSecret(dpuDevice *provisioningv1.DPUDevice) bool {
	if dpuDevice.Spec.BMCCredentialSecretName == nil || dpuDevice.Status.BMCCredentialSecretName == nil {
		return false
	}
	return *dpuDevice.Spec.BMCCredentialSecretName == *dpuDevice.Status.BMCCredentialSecretName &&
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
		// Keep the finalizer if another device still references the old secret (for example a
		// legacy share that predates the uniqueness check).
		inUse, err := r.isCredentialSecretInUseByOtherDevice(ctx, dpuDevice, oldSecretName)
		if err != nil {
			return err
		}
		if !inUse {
			if err := r.removeCredentialFinalizer(ctx, dpuDevice.Namespace, oldSecretName); err != nil {
				return fmt.Errorf("failed to remove finalizer from old secret %q: %w", oldSecretName, err)
			}
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

	// Keep the finalizer while another device still references the secret so it is not left
	// unprotected if sharing was introduced before the uniqueness check ran.
	inUse, err := r.isCredentialSecretInUseByOtherDevice(ctx, dpuDevice, secretName)
	if err != nil {
		return err
	}
	if inUse {
		return nil
	}

	return r.removeCredentialFinalizer(ctx, dpuDevice.Namespace, secretName)
}

// isCredentialSecretInUseByOtherDevice reports whether a DPUDevice other than the one being deleted
// still references the given credential secret.
func (r *DPUDeviceReconciler) isCredentialSecretInUseByOtherDevice(ctx context.Context, dpuDevice *provisioningv1.DPUDevice, secretName string) (bool, error) {
	dpuDeviceList := &provisioningv1.DPUDeviceList{}
	if err := r.Client.List(ctx, dpuDeviceList, client.InNamespace(dpuDevice.Namespace)); err != nil {
		return false, fmt.Errorf("failed to list DPUDevices in namespace %q: %w", dpuDevice.Namespace, err)
	}

	for _, other := range dpuDeviceList.Items {
		if other.Name == dpuDevice.Name || !other.DeletionTimestamp.IsZero() {
			continue
		}
		if other.Spec.BMCCredentialSecretName != nil && *other.Spec.BMCCredentialSecretName == secretName {
			return true, nil
		}
		if other.Status.BMCCredentialSecretName != nil && *other.Status.BMCCredentialSecretName == secretName {
			return true, nil
		}
	}

	return false, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *DPUDeviceReconciler) SetupWithManager(mgr ctrl.Manager) error {
	// Index DPUs by spec.dpuDeviceName so findOwningSpiffeDPU resolves the owning DPU with a
	// scoped lookup instead of listing all DPUs in the namespace on every DPUDevice reconcile.
	if err := mgr.GetFieldIndexer().IndexField(context.Background(), &provisioningv1.DPU{}, dpuByDPUDeviceNameField, indexDPUByDPUDeviceName); err != nil {
		return fmt.Errorf("indexing DPU by %s: %w", dpuByDPUDeviceNameField, err)
	}

	// Owned cert-manager CertificateRequest, so that when cert-manager fills in status.certificate
	// the device is re-enqueued immediately instead of waiting for the rotation requeue.
	certificateRequest := &unstructured.Unstructured{}
	certificateRequest.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   certManagerGroup,
		Version: "v1",
		Kind:    "CertificateRequest",
	})

	builder := ctrl.NewControllerManagedBy(mgr).
		For(&provisioningv1.DPUDevice{}).
		Owns(certificateRequest).
		Watches(&corev1.ConfigMap{}, handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, obj client.Object) []ctrl.Request {
			if obj.GetName() != CATrustBundleConfigMap {
				return nil
			}
			dpuDeviceList := &provisioningv1.DPUDeviceList{}
			if err := r.List(ctx, dpuDeviceList, client.InNamespace(obj.GetNamespace())); err != nil {
				return nil
			}
			requests := make([]ctrl.Request, 0, len(dpuDeviceList.Items))
			for _, dpuDevice := range dpuDeviceList.Items {
				requests = append(requests, ctrl.Request{
					NamespacedName: types.NamespacedName{
						Name:      dpuDevice.Name,
						Namespace: dpuDevice.Namespace,
					},
				})
			}
			return requests
		})).
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
		// A DPU becoming SPIFFE-stamped must trigger its DPUDevice so the ClusterStaticEntry is
		// created promptly (the DPUDevice controller does not otherwise watch DPUs).
		Watches(&provisioningv1.DPU{},
			handler.EnqueueRequestsFromMapFunc(mapDPUToDPUDevice),
			ctrlbuilder.WithPredicates(dpuSpiffeIdentityPredicate()))

	// ClusterStaticEntry is an optional upstream CRD, present only on SPIFFE-enabled clusters.
	// Registering a watch for a CRD that is not installed fails informer cache startup, which
	// would break DPUDevice reconciliation on every non-SPIFFE install. Gate the watch on CRD
	// presence; a cluster that installs the CRD later picks up the watch on the next controller
	// restart (the operator installs the CRD before SPIFFE is enabled).
	switch _, err := mgr.GetRESTMapper().RESTMapping(clusterStaticEntryGVK.GroupKind(), clusterStaticEntryGVK.Version); {
	case err == nil:
		clusterStaticEntry := &unstructured.Unstructured{}
		clusterStaticEntry.SetGroupVersionKind(clusterStaticEntryGVK)
		// Upstream spire-controller-manager owns ClusterStaticEntry status; watch it to re-mirror
		// into the SPIFFEEntryReady condition. Mapped back to the DPUDevice via stamped labels.
		builder = builder.Watches(clusterStaticEntry, handler.EnqueueRequestsFromMapFunc(mapClusterStaticEntryToDPUDevice))
	case meta.IsNoMatchError(err):
		// The CRD is genuinely absent (non-SPIFFE cluster): skip the watch. A cluster that
		// installs the CRD later picks it up on the next controller restart.
		log.Log.Info("ClusterStaticEntry CRD not installed; skipping SPIFFE ClusterStaticEntry watch",
			"gvk", clusterStaticEntryGVK.String())
	default:
		// Any other error (e.g. a transient discovery failure at startup) must fail setup. Treating
		// it like an absent CRD would permanently disable SPIFFE reconciliation for this
		// controller's lifetime; failing setup lets the manager restart and retry discovery.
		return fmt.Errorf("checking ClusterStaticEntry CRD availability: %w", err)
	}

	return builder.Complete(r)
}

// indexDPUByDPUDeviceName extracts the spec.dpuDeviceName index value for a DPU.
func indexDPUByDPUDeviceName(obj client.Object) []string {
	dpu := obj.(*provisioningv1.DPU)
	if dpu.Spec.DPUDeviceName == "" {
		return nil
	}
	return []string{dpu.Spec.DPUDeviceName}
}

func dpuSpiffeIdentityPredicate() predicate.Predicate {
	return predicate.Funcs{
		CreateFunc: func(e event.CreateEvent) bool {
			dpu, ok := dpuFromObject(e.Object)
			return ok && cutil.IsSpiffeDPU(dpu)
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			oldDPU, ok := dpuFromObject(e.ObjectOld)
			if !ok {
				return false
			}
			newDPU, ok := dpuFromObject(e.ObjectNew)
			return ok && cutil.IsSpiffeDPU(newDPU) && !cutil.IsSpiffeDPU(oldDPU)
		},
		DeleteFunc:  func(event.DeleteEvent) bool { return false },
		GenericFunc: func(event.GenericEvent) bool { return false },
	}
}

func dpuFromObject(obj client.Object) (*provisioningv1.DPU, bool) {
	dpu, ok := obj.(*provisioningv1.DPU)
	return dpu, ok && dpu != nil
}

// mapDPUToDPUDevice maps an accepted DPU watch event to the bound DPUDevice; the watch predicate
// owns SPIFFE relevance filtering.
func mapDPUToDPUDevice(_ context.Context, obj client.Object) []ctrl.Request {
	dpu, ok := dpuFromObject(obj)
	if !ok {
		return nil
	}
	return []ctrl.Request{{NamespacedName: types.NamespacedName{
		Name:      dpu.Spec.DPUDeviceName,
		Namespace: dpu.Namespace,
	}}}
}

// mapClusterStaticEntryToDPUDevice maps a ClusterStaticEntry watch event back to its owning
// DPUDevice using the labels stamped at creation time.
func mapClusterStaticEntryToDPUDevice(ctx context.Context, obj client.Object) []ctrl.Request {
	labels := obj.GetLabels()
	name := labels[LabelDPUDeviceName]
	namespace := labels[LabelDPUDeviceNamespace]
	if name == "" || namespace == "" {
		return nil
	}
	return []ctrl.Request{{NamespacedName: types.NamespacedName{Name: name, Namespace: namespace}}}
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
