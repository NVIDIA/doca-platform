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

package nvidia

import (
	"bytes"
	"context"
	_ "embed"
	"fmt"
	"reflect"
	"strings"
	"sync/atomic"
	"text/template"
	"time"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/internal/clustermanager/controller"
	"github.com/nvidia/doca-platform/internal/operator/inventory"
	operatorutils "github.com/nvidia/doca-platform/internal/operator/utils"
	cutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/util"
	"github.com/nvidia/doca-platform/internal/utils"
	"github.com/nvidia/doca-platform/pkg/dpucluster"
	kamajiv1 "github.com/nvidia/doca-platform/third_party/forked/github.com/clastix/kamaji/api/v1alpha1"

	"github.com/Masterminds/sprig/v3"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	kerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/retry"
	"k8s.io/klog/v2"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

// +kubebuilder:rbac:groups=kamaji.clastix.io,resources=tenantcontrolplanes,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=monitoring.coreos.com,resources=servicemonitors,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=apps,resources=daemonsets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=services,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;create;update;patch
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch

const (
	kubernetesNodeRoleMaster       = "node-role.kubernetes.io/master"
	kubernetesNodeRoleControlPlane = "node-role.kubernetes.io/control-plane"

	// Kube-apiserver security configuration (exported for tests)
	// TLSCipherSuites defines the allowed TLS cipher suites for kube-apiserver
	TLSCipherSuites = "TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305"
	// AdmissionPlugins defines the enabled admission plugins for kube-apiserver
	AdmissionPlugins = "NamespaceLifecycle,LimitRanger,ServiceAccount,AlwaysPullImages,MutatingAdmissionWebhook,ValidatingAdmissionWebhook,ResourceQuota,PodSecurity,PodNodeSelector,NodeRestriction,EventRateLimit"
)

var (
	//go:embed manifests/keepalived.yaml
	keepalivedData []byte
	tmpl           = template.Must(template.New("").Funcs(sprig.FuncMap()).Parse(string(keepalivedData)))
	tolerations    = []corev1.Toleration{
		{
			Key:      kubernetesNodeRoleMaster,
			Operator: corev1.TolerationOpExists,
			Effect:   corev1.TaintEffectNoSchedule,
		},
		{
			Key:      kubernetesNodeRoleControlPlane,
			Operator: corev1.TolerationOpExists,
			Effect:   corev1.TaintEffectNoSchedule,
		},
	}

	masterNodeAffinity = &corev1.NodeAffinity{
		RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{NodeSelectorTerms: []corev1.NodeSelectorTerm{
			{
				MatchExpressions: []corev1.NodeSelectorRequirement{
					{
						Key:      kubernetesNodeRoleMaster,
						Operator: corev1.NodeSelectorOpExists,
					},
				},
			},
			{
				MatchExpressions: []corev1.NodeSelectorRequirement{
					{
						Key:      kubernetesNodeRoleControlPlane,
						Operator: corev1.NodeSelectorOpExists,
					},
				},
			},
		}},
	}

	serviceMonitorGVK = schema.GroupVersionKind{
		Group:   "monitoring.coreos.com",
		Version: "v1",
		Kind:    "ServiceMonitor",
	}
	kamaji134CompatibilityPatches = []kamajiv1.JSONPatch{
		{
			Op:   "remove",
			Path: "/crashLoopBackOff",
		},
		{
			Op:   "remove",
			Path: "/imagePullCredentialsVerificationPolicy",
		},
		{
			Op:   "add",
			Path: "/featureGates",
			Value: &apiextensionsv1.JSON{
				Raw: []byte(`{"KubeletCrashLoopBackOffMax":false,"KubeletEnsureSecretPulledImages":false}`),
			},
		},
	}
	staticKeySecretWatchClient atomic.Value
)

func init() {
	controller.RegisterWatch(&kamajiv1.TenantControlPlane{}, handler.EnqueueRequestsFromMapFunc(tcpToDPUCluster))
	controller.RegisterWatch(&appsv1.DaemonSet{}, handler.EnqueueRequestsFromMapFunc(daemonsetToDPUCluster))
	controller.RegisterWatch(&corev1.Secret{}, handler.EnqueueRequestsFromMapFunc(secretToDPUClusters))
	controller.RegisterRawWatch(source.Channel(dpuClusterRequeueEvents, &handler.EnqueueRequestForObject{}))
}

type clusterHandler struct {
	client.Client
	Scheme           *runtime.Scheme
	keepalivedImage  string
	requeueScheduler requeueScheduler
	requeueAfter     time.Duration
	reloadVerifier   reloadVerifier
	tenantClient     tenantClientProvider
	recorder         record.EventRecorder
}

// HandlerOption configures a cluster handler.
type HandlerOption func(*clusterHandler)

// WithRequeueScheduler configures the static key rotation requeue scheduler.
func WithRequeueScheduler(scheduler requeueScheduler) HandlerOption {
	return func(handler *clusterHandler) {
		handler.requeueScheduler = scheduler
	}
}

// WithRequeueAfter configures the static key rotation requeue interval.
func WithRequeueAfter(requeueAfter time.Duration) HandlerOption {
	return func(handler *clusterHandler) {
		handler.requeueAfter = requeueAfter
	}
}

// WithReloadVerifier configures the static key reload verifier.
func WithReloadVerifier(verifier reloadVerifier) HandlerOption {
	return func(handler *clusterHandler) {
		handler.reloadVerifier = verifier
	}
}

// WithEventRecorder configures the event recorder.
func WithEventRecorder(recorder record.EventRecorder) HandlerOption {
	return func(handler *clusterHandler) {
		handler.recorder = recorder
	}
}

// NewHandler creates a Kamaji cluster handler.
func NewHandler(client client.Client, scheme *runtime.Scheme, keepalivedImage string, opts ...HandlerOption) controller.ClusterHandler {
	staticKeySecretWatchClient.Store(client)
	h := &clusterHandler{
		Client:           client,
		Scheme:           scheme,
		keepalivedImage:  keepalivedImage,
		requeueScheduler: defaultDPUClusterRequeueScheduler,
		reloadVerifier:   &metricsReloadVerifier{client: client},
		requeueAfter:     defaultStaticKeyRotationRequeueAfter,
		tenantClient:     &dpuClusterTenantClientProvider{hostClient: client, scheme: scheme},
	}
	for _, opt := range opts {
		opt(h)
	}
	return h
}

type tenantClientProvider interface {
	Client(context.Context, *provisioningv1.DPUCluster) (client.Client, error)
}

type dpuClusterTenantClientProvider struct {
	hostClient client.Reader
	scheme     *runtime.Scheme
}

// Client returns a tenant cluster client for the DPUCluster.
func (p *dpuClusterTenantClientProvider) Client(ctx context.Context, dc *provisioningv1.DPUCluster) (client.Client, error) {
	return dpucluster.NewConfig(p.hostClient, dc).Client(ctx, dpucluster.ClientOptionScheme{Scheme: p.scheme})
}

func (cm *clusterHandler) ReconcileCluster(ctx context.Context, dc *provisioningv1.DPUCluster) (string, []metav1.Condition, error) {
	var conds []metav1.Condition
	kubeconfig, nodePort, cond, err := cm.reconcileKamaji(ctx, dc)
	if err != nil {
		return "", nil, err
	}
	if cond != nil {
		conds = append(conds, *cond)
	}

	rotationResult, err := cm.reconcileStaticKeyRotation(ctx, dc)
	if err != nil {
		return "", nil, err
	}
	if rotationResult != nil {
		conds = append(conds, rotationResult.conditions...)
	}

	cond, err = cm.reconcileKeepalived(ctx, dc, nodePort)
	if err != nil {
		return "", nil, err
	}
	if cond != nil {
		conds = append(conds, *cond)
	}

	// TODO: add conditions for monitoring
	err = cm.reconcileMonitoring(ctx, dc, nodePort)
	if err != nil {
		return "", nil, err
	}

	return kubeconfig, conds, err
}

func (cm *clusterHandler) CleanUpCluster(ctx context.Context, dc *provisioningv1.DPUCluster) (bool, error) {
	// cleanup is done via owner reference
	return true, nil
}

func (cm *clusterHandler) Type() string {
	return string(provisioningv1.KamajiCluster)
}

// reconcileConfigMap is a generic helper to reconcile ConfigMaps, eliminating code duplication
func (cm *clusterHandler) reconcileConfigMap(ctx context.Context, dc *provisioningv1.DPUCluster,
	cmName string, expectedCMFunc func(*provisioningv1.DPUCluster, *runtime.Scheme) (*corev1.ConfigMap, error),
	resourceType string) error {
	logger := log.FromContext(ctx)
	nn := kamajiTCPName(dc)

	configMap := &corev1.ConfigMap{}
	if err := cm.Client.Get(ctx, types.NamespacedName{Name: cmName, Namespace: nn.Namespace}, configMap); err != nil {
		if !apierrors.IsNotFound(err) {
			return fmt.Errorf("failed to get %s configmap, err: %v", resourceType, err)
		}

		// ConfigMap doesn't exist, create it
		expectedCM, err := expectedCMFunc(dc, cm.Scheme)
		if err != nil {
			return fmt.Errorf("failed to generate %s configmap, err: %v", resourceType, err)
		}
		if err := cm.Client.Create(ctx, expectedCM); err != nil {
			return fmt.Errorf("failed to create %s configmap, err: %v", resourceType, err)
		}
		logger.V(3).Info("created configmap", "type", resourceType, "name", cmName)
	}
	return nil
}

func (cm *clusterHandler) reconcileAuditPolicyConfigMap(ctx context.Context, dc *provisioningv1.DPUCluster) error {
	nn := kamajiTCPName(dc)
	return cm.reconcileConfigMap(ctx, dc, fmt.Sprintf("%s-audit-policy", nn.Name),
		expectedAuditPolicyConfigMap, "audit policy")
}

func (cm *clusterHandler) reconcileEventRateLimitConfigMap(ctx context.Context, dc *provisioningv1.DPUCluster) error {
	nn := kamajiTCPName(dc)
	return cm.reconcileConfigMap(ctx, dc, fmt.Sprintf("%s-eventratelimit", nn.Name),
		expectedEventRateLimitConfigMap, "eventratelimit")
}

// getClusterCondition determines the cluster status condition based on TCP status
func (cm *clusterHandler) getClusterCondition(tcp *kamajiv1.TenantControlPlane, dc *provisioningv1.DPUCluster) *metav1.Condition {
	tcpStatus := tcp.Status.Kubernetes.Version.Status
	if tcpStatus == nil || (*tcpStatus != kamajiv1.VersionReady && *tcpStatus != kamajiv1.VersionUpgrading) {
		return cutil.NewCondition(string(provisioningv1.ConditionCreated), fmt.Errorf("creating cluster"), "ClusterNotReady", "")
	}

	if *tcpStatus == kamajiv1.VersionUpgrading {
		return cutil.NewCondition(string(provisioningv1.ConditionUpgraded), fmt.Errorf("upgrading control plane"), "", "")
	}

	// Check if upgrade just completed
	for _, cond := range dc.Status.Conditions {
		if cond.Type == string(provisioningv1.ConditionUpgraded) &&
			*tcpStatus == kamajiv1.VersionReady &&
			cond.Status == metav1.ConditionFalse {
			return cutil.NewCondition(string(provisioningv1.ConditionUpgraded), nil, "Upgraded", "")
		}
	}

	return cutil.NewCondition(string(provisioningv1.ConditionCreated), nil, "Created", "")
}

func (cm *clusterHandler) reconcileKeepalived(ctx context.Context, dc *provisioningv1.DPUCluster, nodePort int32) (*metav1.Condition, error) {
	if dc.Spec.ClusterEndpoint == nil || dc.Spec.ClusterEndpoint.Keepalived == nil || nodePort == 0 {
		return nil, nil
	}

	// Copy imagePullSecrets from operator namespace to tenant namespace
	imagePullSecrets, err := cm.copyImagePullSecrets(ctx, dc.Namespace)
	if err != nil {
		return nil, fmt.Errorf("failed to copy imagePullSecrets: %w", err)
	}

	values := struct {
		Name             string
		Interface        string
		VirtualRouterID  int
		VirtualIP        string
		NodePort         int32
		NodeSelector     map[string]string
		ImagePullSecrets []string
		KeepalivedImage  string
	}{
		Name:             fmt.Sprintf("%s-keepalived", dc.Name),
		Interface:        dc.Spec.ClusterEndpoint.Keepalived.Interface,
		VirtualRouterID:  dc.Spec.ClusterEndpoint.Keepalived.VirtualRouterID,
		VirtualIP:        dc.Spec.ClusterEndpoint.Keepalived.VIP,
		NodePort:         nodePort,
		NodeSelector:     dc.Spec.ClusterEndpoint.Keepalived.NodeSelector,
		ImagePullSecrets: imagePullSecrets,
		KeepalivedImage:  cm.keepalivedImage,
	}
	buf := bytes.NewBuffer(nil)
	if err := tmpl.Execute(buf, values); err != nil {
		return nil, fmt.Errorf("failed to execute template, err: %v", err)
	}
	objs, err := operatorutils.BytesToUnstructured(buf.Bytes())
	if err != nil {
		return nil, fmt.Errorf("error while converting keepalived manifests to objects: %w", err)
	} else if len(objs) == 0 {
		return nil, fmt.Errorf("no objects found in keepalived manifests")
	}

	if err := inventory.NewEdits().
		AddForAll(inventory.NamespaceEdit(dc.Namespace)).
		AddForAll(inventory.OwnerReferenceEdit(dc, cm.Scheme)).
		AddForKindS(inventory.DaemonsetKind, inventory.TolerationsEdit(tolerations)).
		AddForKindS(inventory.DaemonsetKind, inventory.NodeAffinityEdit(masterNodeAffinity)).
		Apply(objs); err != nil {
		return nil, fmt.Errorf("failed to set namespace for keepalived manifests, err: %v", err)
	}
	for _, obj := range objs {
		err = func() error {
			tc, cancel := context.WithTimeout(ctx, 5*time.Second)
			defer cancel()
			if err = cm.Client.Patch(tc, obj, client.Apply, client.FieldOwner("kamaji-cluster-manager")); err != nil {
				return fmt.Errorf("error patching %v %v: %w", obj.GetObjectKind().GroupVersionKind().Kind, klog.KObj(obj), err)
			}
			return nil
		}()
		if err != nil {
			return nil, err
		}
	}
	return cutil.NewCondition("KeepalivedReady", nil, "Deployed", ""), nil
}

func (cm *clusterHandler) reconcileKamaji(ctx context.Context, dc *provisioningv1.DPUCluster) (string, int32, *metav1.Condition, error) {
	logger := log.FromContext(ctx)
	nn := kamajiTCPName(dc)
	svc := &corev1.Service{}
	svcCreated, tcpCreated := true, true
	if err := cm.Client.Get(ctx, nn, svc); err != nil {
		if !apierrors.IsNotFound(err) {
			return "", 0, nil, fmt.Errorf("failed to get service, err: %v", err)
		}
		svcCreated = false
	}
	tcp := &kamajiv1.TenantControlPlane{}
	if err := cm.Client.Get(ctx, nn, tcp); err != nil {
		if !apierrors.IsNotFound(err) {
			return "", 0, nil, fmt.Errorf("failed to get tenant control plane, err: %v", err)
		}
		tcpCreated = false
	}

	// Reconcile audit policy ConfigMap
	if err := cm.reconcileAuditPolicyConfigMap(ctx, dc); err != nil {
		return "", 0, nil, err
	}

	// Reconcile EventRateLimit ConfigMap
	if err := cm.reconcileEventRateLimitConfigMap(ctx, dc); err != nil {
		return "", 0, nil, err
	}

	if !svcCreated && !tcpCreated {
		if err := cm.Client.Create(ctx, expectedService(dc)); err != nil {
			return "", 0, nil, fmt.Errorf("failed to create service, err: %v", err)
		}
		if err := cm.Client.Get(ctx, nn, svc); err != nil {
			return "", 0, nil, fmt.Errorf("failed to get service after creation, err: %v", err)
		}
	}
	var nodePort int32
	if !tcpCreated {
		for _, p := range svc.Spec.Ports {
			if p.Name != "kube-apiserver" || p.NodePort == 0 {
				continue
			}
			nodePort = p.NodePort
			break
		}
		if nodePort == 0 {
			return "", 0, nil, fmt.Errorf("missing NodePort for kube-apiserver")
		}
		// Encryption at rest and its DPUCluster metadata are applied only at TenantControlPlane
		// creation time. The plan is derived from an existing per-cluster Secret when present,
		// otherwise from the current DPFOperatorConfig.
		encPlan, err := cm.reconcileEncryptionConfig(ctx, dc)
		if err != nil {
			return "", 0, nil, fmt.Errorf("failed to reconcile etcd encryption config, err: %v", err)
		}
		tcp, err = expectedTenantControlPlane(dc, cm.Scheme, nodePort)
		if err != nil {
			return "", 0, nil, fmt.Errorf("failed to generate expected TCP, err: %v", err)
		}
		cm.patchDPUClusterEncryptionMetadata(dc, encPlan)
		applyTCPEncryptionIfEnabled(tcp, encPlan)
		if err := cm.Client.Create(ctx, tcp); err != nil {
			return "", 0, nil, fmt.Errorf("failed to create cluster request, err: %v", err)
		}
	} else {
		nodePort = tcp.Spec.NetworkProfile.Port
		if tcp.Spec.Kubernetes.Version != cutil.KubernetesVersion {
			logger.V(1).Info("need to upgrade TCP", "tcp", tcp.Name)
			// Use retry logic to handle conflicts when the object has been modified
			err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
				// Get the latest version to avoid conflicts
				latest := &kamajiv1.TenantControlPlane{}
				if err := cm.Client.Get(ctx, nn, latest); err != nil {
					return err
				}

				// Update the version
				latest.Spec.Kubernetes.Version = cutil.KubernetesVersion

				for _, newPatch := range kamaji134CompatibilityPatches {
					found := false
					for _, gotPatch := range latest.Spec.Kubernetes.Kubelet.ConfigurationJSONPatches {
						if gotPatch.Op == newPatch.Op && gotPatch.Path == newPatch.Path && reflect.DeepEqual(gotPatch.Value, newPatch.Value) {
							found = true
							break
						}
					}
					if !found {
						latest.Spec.Kubernetes.Kubelet.ConfigurationJSONPatches = append(latest.Spec.Kubernetes.Kubelet.ConfigurationJSONPatches, newPatch)
					}
				}

				// Clear managedFields to avoid conflicts with server-side apply
				latest.SetManagedFields(nil)
				latest.SetGroupVersionKind(kamajiv1.GroupVersion.WithKind("TenantControlPlane"))

				// Apply the update
				return cm.Client.Patch(ctx, latest, client.Apply, client.FieldOwner("kamaji-cluster-manager"), client.ForceOwnership)
			})
			if err != nil {
				return "", 0, nil, fmt.Errorf("failed to upgrade TCP, err: %v", err)
			}
		}
	}

	condition := cm.getClusterCondition(tcp, dc)
	return adminKubeconfigName(dc), nodePort, condition, nil
}

func (cm *clusterHandler) reconcileMonitoring(ctx context.Context, dc *provisioningv1.DPUCluster, nodePort int32) error {
	dpfOperatorConfig, err := utils.GetDPFOperatorConfig(ctx, cm.Client)
	if err != nil {
		return fmt.Errorf("get DPFOperatorConfig to verify if monitoring is enabled: %v", err)
	}

	// Delete monitoring resources if monitoring is disabled or DPUCluster is in deletion.
	if !dpfOperatorConfig.MonitoringEnabled() || !dc.DeletionTimestamp.IsZero() {
		return cm.reconcileDeleteMonitoring(ctx, dc)
	}

	var errs []error
	if err := cm.reconcileMonitoringService(ctx, dc, nodePort); err != nil {
		errs = append(errs, fmt.Errorf("reconcile metrics Service: %v", err))
	}

	if err := cm.reconcileServiceMonitor(ctx, dc); err != nil {
		errs = append(errs, fmt.Errorf("reconcile ServiceMonitor: %v", err))
	}

	return kerrors.NewAggregate(errs)
}

func (cm *clusterHandler) reconcileDeleteMonitoring(ctx context.Context, dc *provisioningv1.DPUCluster) error {
	var errs []error
	if err := cm.reconcileDeleteMonitoringService(ctx, dc); err != nil {
		errs = append(errs, fmt.Errorf("reconcile delete metrics Service: %v", err))
	}
	if err := cm.reconcileDeleteServiceMonitor(ctx, dc); err != nil {
		errs = append(errs, fmt.Errorf("reconcile delete ServiceMonitor: %v", err))
	}
	return kerrors.NewAggregate(errs)
}

func (cm *clusterHandler) reconcileMonitoringService(ctx context.Context, dc *provisioningv1.DPUCluster, nodePort int32) error {
	logger := log.FromContext(ctx)
	svc := getMetricsService(dc, nodePort)
	logger.V(1).Info("reconciling metrics Service", "service", klog.KObj(svc))
	owner := metav1.NewControllerRef(dc, provisioningv1.DPUClusterGroupVersionKind)
	svc.SetOwnerReferences([]metav1.OwnerReference{*owner})
	err := cm.Client.Patch(ctx, svc, client.Apply, client.FieldOwner("kamaji-cluster-manager"), client.ForceOwnership)
	if err != nil {
		return fmt.Errorf("failed to update metrics Service, err: %v", err)
	}

	return nil
}

func (cm *clusterHandler) reconcileDeleteMonitoringService(ctx context.Context, dc *provisioningv1.DPUCluster) error {
	logger := log.FromContext(ctx)
	svc := &corev1.Service{}
	svc.SetName(fmt.Sprintf("%s-metrics", dc.GetName()))
	svc.SetNamespace(dc.GetNamespace())
	if err := cm.Client.Get(ctx, types.NamespacedName{Namespace: svc.GetNamespace(), Name: svc.GetName()}, svc); err == nil {
		logger.V(1).Info("deleting metrics Service because monitoring is disabled", "service", klog.KObj(svc))
		if ownedBy(svc, dc) {
			if err := cm.Client.Delete(ctx, svc); err != nil && !apierrors.IsNotFound(err) {
				return fmt.Errorf("failed to delete metrics Service: %v", err)
			}
		}
	} else if !apierrors.IsNotFound(err) {
		return fmt.Errorf("failed to get metrics Service: %v", err)
	}

	return nil
}

func (cm *clusterHandler) reconcileServiceMonitor(ctx context.Context, dc *provisioningv1.DPUCluster) error {
	logger := log.FromContext(ctx)

	// Return early if no ServiceMonitor CRD is found
	if !cm.hasGVK(serviceMonitorGVK) {
		logger.V(1).Info("ServiceMonitor CRD not found, skipping ServiceMonitor reconciliation")
		return nil
	}

	// Deploy ServiceMonitor resource for Prometheus scraping.
	// This is copied from https://kamaji.clastix.io/guides/monitoring/#enable-metrics-scraping
	sm := getServiceMonitorResource(dc, serviceMonitorGVK)
	logger.V(1).Info("reconciling ServiceMonitor", "servicemonitor", klog.KObj(sm))
	owner := metav1.NewControllerRef(dc, provisioningv1.DPUClusterGroupVersionKind)
	sm.SetOwnerReferences([]metav1.OwnerReference{*owner})
	err := cm.Client.Patch(ctx, sm, client.Apply, client.FieldOwner("kamaji-cluster-manager"), client.ForceOwnership)
	if err != nil {
		return fmt.Errorf("failed to update ServiceMonitor, err: %v", err)
	}

	return nil
}

func (cm *clusterHandler) reconcileDeleteServiceMonitor(ctx context.Context, dc *provisioningv1.DPUCluster) error {
	logger := log.FromContext(ctx)

	// Return early if no ServiceMonitor CRD is found
	if !cm.hasGVK(serviceMonitorGVK) {
		return nil
	}

	sm := &unstructured.Unstructured{}
	sm.SetGroupVersionKind(serviceMonitorGVK)
	sm.SetName(dc.GetName())
	sm.SetNamespace(dc.GetNamespace())
	if err := cm.Client.Get(ctx, types.NamespacedName{Namespace: sm.GetNamespace(), Name: sm.GetName()}, sm); err == nil {
		if ownedBy(sm, dc) {
			logger.V(1).Info("deleting ServiceMonitor because monitoring is disabled", "servicemonitor", klog.KObj(sm))
			if err := cm.Client.Delete(ctx, sm); err != nil && !apierrors.IsNotFound(err) {
				return fmt.Errorf("failed to delete ServiceMonitor: %v", err)
			}
		}
	} else if !apierrors.IsNotFound(err) {
		return fmt.Errorf("failed to get ServiceMonitor: %v", err)
	}

	return nil
}

func (cm *clusterHandler) hasGVK(gvk schema.GroupVersionKind) bool {
	_, err := cm.Client.RESTMapper().RESTMapping(gvk.GroupKind(), gvk.Version)
	return err == nil
}

func ownedBy(obj metav1.Object, owner metav1.Object) bool {
	for _, ref := range obj.GetOwnerReferences() {
		if ref.UID == owner.GetUID() && ref.Controller != nil && *ref.Controller {
			return true
		}
	}
	return false
}

// metricsServiceName returns the per-cluster Kamaji metrics Service name.
func metricsServiceName(dc metav1.Object) string {
	return fmt.Sprintf("%s-metrics", dc.GetName())
}

func getMetricsService(dc *provisioningv1.DPUCluster, nodePort int32) *corev1.Service {
	svc := &corev1.Service{}
	svc.SetGroupVersionKind(corev1.SchemeGroupVersion.WithKind("Service"))
	svc.SetName(metricsServiceName(dc))
	svc.SetNamespace(dc.GetNamespace())
	svc.SetLabels(map[string]string{
		"kamaji.clastix.io/name": dc.GetName() + "-metrics",
	})
	svc.Spec = corev1.ServiceSpec{
		Selector: map[string]string{
			"kamaji.clastix.io/name": dc.GetName(),
		},
		Ports: []corev1.ServicePort{
			{
				Name:       "kube-apiserver-metrics",
				Port:       6443,
				Protocol:   corev1.ProtocolTCP,
				TargetPort: intstr.IntOrString{IntVal: nodePort},
			},
			{
				Name:       "kube-controller-manager-metrics",
				Port:       10257,
				Protocol:   corev1.ProtocolTCP,
				TargetPort: intstr.IntOrString{IntVal: 10257},
			},
			{
				Name:       "kube-scheduler-metrics",
				Port:       10259,
				Protocol:   corev1.ProtocolTCP,
				TargetPort: intstr.IntOrString{IntVal: 10259},
			},
		},
	}
	return svc
}

// processMetricsRegex matches the process metrics kept for every DPU cluster
// control plane component.
const processMetricsRegex = "process_cpu_seconds_total|process_resident_memory_bytes|process_start_time_seconds"

// keepMetricsRelabeling returns a metric relabeling that keeps only the metrics
// matching the given regex. The allowlists passed to it mirror the ones applied
// to the management cluster control plane in
// deploy/helmfiles/values/kube-prometheus-stack.yaml; keep the two in sync.
func keepMetricsRelabeling(regex string) map[string]interface{} {
	return map[string]interface{}{
		"action": "keep",
		"regex":  regex,
		"sourceLabels": []interface{}{
			"__name__",
		},
	}
}

// renameToDPFPrefixRelabeling returns a metric relabeling that renames all
// metrics by prepending "dpf_", so that DPU cluster control-plane metrics are
// clearly namespaced as DPF metrics in the management cluster's Prometheus.
func renameToDPFPrefixRelabeling() map[string]interface{} {
	return map[string]interface{}{
		"action": "replace",
		"regex":  "(.+)",
		"sourceLabels": []interface{}{
			"__name__",
		},
		"targetLabel": "__name__",
		"replacement": "dpf_${1}",
	}
}

// apiserverMetricsClientSecretName returns the conventional Kamaji Secret name used in ServiceMonitor configuration.
func apiserverMetricsClientSecretName(dc metav1.Object) string {
	return dc.GetName() + "-api-server-kubelet-client-certificate"
}

func getServiceMonitorResource(dc *provisioningv1.DPUCluster, gvk schema.GroupVersionKind) *unstructured.Unstructured {
	clusterName := dc.GetName()
	secretName := apiserverMetricsClientSecretName(dc)

	sm := &unstructured.Unstructured{}
	sm.SetName(clusterName)
	sm.SetNamespace(dc.GetNamespace())
	sm.SetGroupVersionKind(gvk)
	sm.SetLabels(map[string]string{
		"kamaji.clastix.io/name":                   clusterName + "-metrics",
		provisioningv1.DPUClusterNameLabelKey:      clusterName,
		provisioningv1.DPUClusterNamespaceLabelKey: dc.GetNamespace(),
	})
	sm.Object["spec"] = map[string]interface{}{
		"selector": map[string]interface{}{
			"matchLabels": map[string]interface{}{
				"kamaji.clastix.io/name": clusterName + "-metrics",
			},
		},
		"endpoints": []interface{}{
			map[string]interface{}{
				"port":          "kube-apiserver-metrics",
				"scheme":        "https",
				"path":          "/metrics",
				"interval":      "15s",
				"scrapeTimeout": "10s",
				"tlsConfig": map[string]interface{}{
					"insecureSkipVerify": true,
					"cert": map[string]interface{}{
						"secret": map[string]interface{}{
							"name": secretName,
							"key":  "apiserver-kubelet-client.crt",
						},
					},
					"keySecret": map[string]interface{}{
						"name": secretName,
						"key":  "apiserver-kubelet-client.key",
					},
				},
				"metricRelabelings": []interface{}{
					// Keep only the metrics consumed by the DPF dashboards and
					// alert/recording rules for the DPU cluster control plane.
					keepMetricsRelabeling("apiserver_request_total|apiserver_request_duration_seconds_(bucket|sum|count)|apiserver_current_inflight_requests|apiserver_longrunning_requests|apiserver_storage_size_bytes|apiserver_storage_objects|etcd_requests_total|etcd_request_errors_total|etcd_request_duration_seconds_(bucket|sum|count)|" + processMetricsRegex),
					// Drop the same histogram buckets the kube-prometheus-stack
					// chart drops by default, restricted to the histogram
					// families the keep rule lets through.
					map[string]interface{}{
						"action": "drop",
						"regex":  `(etcd_request|apiserver_request)_duration_seconds_bucket;(0\.15|0\.2|0\.3|0\.35|0\.4|0\.45|0\.6|0\.7|0\.8|0\.9|1\.25|1\.5|1\.75|2|3|3\.5|4|4\.5|6|7|8|9|15|25|30|50)(\.0)?`,
						"sourceLabels": []interface{}{
							"__name__",
							"le",
						},
					},
					// Prefix all metrics with dpf_ so DPU cluster control-plane
					// metrics are namespaced in the management cluster's Prometheus.
					renameToDPFPrefixRelabeling(),
				},
				"relabelings": []interface{}{
					map[string]interface{}{
						"action":      "replace",
						"targetLabel": "cluster",
						"replacement": clusterName,
					},
					map[string]interface{}{
						"action":      "replace",
						"targetLabel": "job",
						"replacement": "apiserver",
					},
				},
			},
			map[string]interface{}{
				"port":          "kube-controller-manager-metrics",
				"scheme":        "https",
				"path":          "/metrics",
				"interval":      "15s",
				"scrapeTimeout": "10s",
				"tlsConfig": map[string]interface{}{
					"insecureSkipVerify": true,
					"cert": map[string]interface{}{
						"secret": map[string]interface{}{
							"name": secretName,
							"key":  "apiserver-kubelet-client.crt",
						},
					},
					"keySecret": map[string]interface{}{
						"name": secretName,
						"key":  "apiserver-kubelet-client.key",
					},
				},
				"metricRelabelings": []interface{}{
					// Keep only the metrics consumed by the DPF dashboards and
					// alert/recording rules for the DPU cluster control plane.
					keepMetricsRelabeling("workqueue_(depth|adds_total|retries_total|queue_duration_seconds_(bucket|sum|count)|work_duration_seconds_(bucket|sum|count))|rest_client_requests_total|leader_election_master_status|" + processMetricsRegex),
					// Prefix all metrics with dpf_ so DPU cluster control-plane
					// metrics are namespaced in the management cluster's Prometheus.
					renameToDPFPrefixRelabeling(),
				},
				"relabelings": []interface{}{
					map[string]interface{}{
						"action":      "replace",
						"targetLabel": "cluster",
						"replacement": clusterName,
					},
					map[string]interface{}{
						"action":      "replace",
						"targetLabel": "job",
						"replacement": "kube-controller-manager",
					},
				},
			},
			map[string]interface{}{
				"port":          "kube-scheduler-metrics",
				"scheme":        "https",
				"path":          "/metrics",
				"interval":      "15s",
				"scrapeTimeout": "10s",
				"tlsConfig": map[string]interface{}{
					"insecureSkipVerify": true,
					"cert": map[string]interface{}{
						"secret": map[string]interface{}{
							"name": secretName,
							"key":  "apiserver-kubelet-client.crt",
						},
					},
					"keySecret": map[string]interface{}{
						"name": secretName,
						"key":  "apiserver-kubelet-client.key",
					},
				},
				"metricRelabelings": []interface{}{
					// Keep only the metrics consumed by the DPF dashboards and
					// alert/recording rules for the DPU cluster control plane.
					keepMetricsRelabeling("scheduler_pending_pods|scheduler_schedule_attempts_total|scheduler_scheduling_attempt_duration_seconds_(bucket|sum|count)|" + processMetricsRegex),
					// Prefix all metrics with dpf_ so DPU cluster control-plane
					// metrics are namespaced in the management cluster's Prometheus.
					renameToDPFPrefixRelabeling(),
				},
				"relabelings": []interface{}{
					map[string]interface{}{
						"action":      "replace",
						"targetLabel": "cluster",
						"replacement": clusterName,
					},
					map[string]interface{}{
						"action":      "replace",
						"targetLabel": "job",
						"replacement": "kube-scheduler",
					},
				},
			},
		},
	}

	return sm
}

func kamajiTCPName(dc *provisioningv1.DPUCluster) types.NamespacedName {
	return cutil.GetNamespacedName(dc)
}

func adminKubeconfigName(dc *provisioningv1.DPUCluster) string {
	return fmt.Sprintf("%s-admin-kubeconfig", kamajiTCPName(dc).Name)
}

func expectedService(dc *provisioningv1.DPUCluster) *corev1.Service {
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      dc.Name,
			Namespace: dc.Namespace,
		},
		Spec: corev1.ServiceSpec{
			Type: corev1.ServiceTypeNodePort,
			Ports: []corev1.ServicePort{
				{
					Name:     "kube-apiserver",
					Protocol: corev1.ProtocolTCP,
					// Port could be any value, because it will be updated by Kamaji once the TCP is created
					Port: 6443,
				},
			},
			Selector: map[string]string{
				"kamaji.clastix.io/name": dc.Name,
			},
		},
	}
	return svc
}

func expectedAuditPolicyConfigMap(dc *provisioningv1.DPUCluster, scheme *runtime.Scheme) (*corev1.ConfigMap, error) {
	nn := kamajiTCPName(dc)
	auditPolicy := `# Log all requests at the Metadata level.
apiVersion: audit.k8s.io/v1
kind: Policy
rules:
- level: Metadata
`
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-audit-policy", nn.Name),
			Namespace: nn.Namespace,
			Labels: map[string]string{
				"tenant.clastix.io":                   nn.Name,
				provisioningv1.DPUClusterNameLabelKey: dc.Name,
			},
		},
		Data: map[string]string{
			"audit-policy.yaml": auditPolicy,
		},
	}
	if err := controllerutil.SetOwnerReference(dc, cm, scheme); err != nil {
		return nil, fmt.Errorf("failed to set owner reference, err: %v", err)
	}
	return cm, nil
}

func expectedEventRateLimitConfigMap(dc *provisioningv1.DPUCluster, scheme *runtime.Scheme) (*corev1.ConfigMap, error) {
	nn := kamajiTCPName(dc)
	eventRateLimitConfig := `apiVersion: apiserver.config.k8s.io/v1
kind: AdmissionConfiguration
plugins:
- name: EventRateLimit
  configuration:
    apiVersion: eventratelimit.admission.k8s.io/v1alpha1
    kind: Configuration
    limits:
    - type: Namespace
      qps: 500
      burst: 1000
      cacheSize: 2000
    - type: User
      qps: 500
      burst: 1000
`
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-eventratelimit", nn.Name),
			Namespace: nn.Namespace,
			Labels: map[string]string{
				"tenant.clastix.io":                   nn.Name,
				provisioningv1.DPUClusterNameLabelKey: dc.Name,
			},
		},
		Data: map[string]string{
			"eventratelimit.yaml": eventRateLimitConfig,
		},
	}
	if err := controllerutil.SetOwnerReference(dc, cm, scheme); err != nil {
		return nil, fmt.Errorf("failed to set owner reference, err: %v", err)
	}
	return cm, nil
}

func expectedTenantControlPlane(dc *provisioningv1.DPUCluster, scheme *runtime.Scheme, nodePort int32) (*kamajiv1.TenantControlPlane, error) {
	nn := kamajiTCPName(dc)
	tcp := &kamajiv1.TenantControlPlane{
		ObjectMeta: metav1.ObjectMeta{
			Name:      nn.Name,
			Namespace: nn.Namespace,
			Labels: map[string]string{
				"tenant.clastix.io":                   nn.Name,
				provisioningv1.DPUClusterNameLabelKey: dc.Name,
			},
		},
		Spec: kamajiv1.TenantControlPlaneSpec{
			DataStore: "default",
			ControlPlane: kamajiv1.ControlPlane{
				Deployment: kamajiv1.DeploymentSpec{
					Affinity: &corev1.Affinity{
						NodeAffinity: &corev1.NodeAffinity{
							RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
								NodeSelectorTerms: []corev1.NodeSelectorTerm{
									{
										MatchExpressions: []corev1.NodeSelectorRequirement{
											{
												Key:      kubernetesNodeRoleMaster,
												Operator: corev1.NodeSelectorOpExists,
											},
										},
									},
									{
										MatchExpressions: []corev1.NodeSelectorRequirement{
											{
												Key:      kubernetesNodeRoleControlPlane,
												Operator: corev1.NodeSelectorOpExists,
											},
										},
									},
								},
							},
						},
					},
					Tolerations: tolerations,
					Replicas:    ptr.To[int32](3),
					AdditionalMetadata: kamajiv1.AdditionalMetadata{
						Labels: map[string]string{
							"tenant.clastix.io": nn.Name,
						},
					},
					Resources: &kamajiv1.ControlPlaneComponentsResources{
						APIServer: &corev1.ResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse("250m"),
								corev1.ResourceMemory: resource.MustParse("512Mi"),
							},
						},
						ControllerManager: &corev1.ResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse("125m"),
								corev1.ResourceMemory: resource.MustParse("256Mi"),
							},
						},
						Scheduler: &corev1.ResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse("125m"),
								corev1.ResourceMemory: resource.MustParse("256Mi"),
							},
						},
					},
					ExtraArgs: &kamajiv1.ControlPlaneExtraArgs{
						APIServer: []string{
							"--audit-log-path=/var/log/kubernetes/audit.log",
							"--audit-policy-file=/etc/kubernetes/audit-policy.yaml",
							"--audit-log-maxage=30",
							"--audit-log-maxbackup=10",
							"--audit-log-maxsize=100",
							// It can be set to true if the RBAC is enabled. refer to https://kubespray.io/#/docs/operations/hardening
							"--anonymous-auth=true",
							"--profiling=false",
							fmt.Sprintf("--tls-cipher-suites=%s", TLSCipherSuites),
							"--request-timeout=120s",
							"--admission-control-config-file=/etc/kubernetes/eventratelimit/eventratelimit.yaml",
						},
						ControllerManager: []string{
							"--profiling=false",
						},
						Scheduler: []string{
							"--profiling=false",
						},
					},
					// Add volumes for audit policy and eventratelimit
					AdditionalVolumes: []corev1.Volume{
						{
							Name: "audit-policy",
							VolumeSource: corev1.VolumeSource{
								ConfigMap: &corev1.ConfigMapVolumeSource{
									LocalObjectReference: corev1.LocalObjectReference{
										Name: fmt.Sprintf("%s-audit-policy", nn.Name),
									},
								},
							},
						},
						{
							Name: "audit-log",
							VolumeSource: corev1.VolumeSource{
								HostPath: &corev1.HostPathVolumeSource{
									Path: fmt.Sprintf("/var/log/kubernetes/kamaji/%s", nn.Name),
									Type: ptr.To(corev1.HostPathDirectoryOrCreate),
								},
							},
						},
						{
							Name: "eventratelimit-config",
							VolumeSource: corev1.VolumeSource{
								ConfigMap: &corev1.ConfigMapVolumeSource{
									LocalObjectReference: corev1.LocalObjectReference{
										Name: fmt.Sprintf("%s-eventratelimit", nn.Name),
									},
								},
							},
						},
					},
					// Add volume mounts for API server
					AdditionalVolumeMounts: &kamajiv1.AdditionalVolumeMounts{
						APIServer: []corev1.VolumeMount{
							{
								Name:      "audit-policy",
								MountPath: "/etc/kubernetes/audit-policy.yaml",
								SubPath:   "audit-policy.yaml",
								ReadOnly:  true,
							},
							{
								Name:      "audit-log",
								MountPath: "/var/log/kubernetes",
							},
							{
								Name:      "eventratelimit-config",
								MountPath: "/etc/kubernetes/eventratelimit",
								ReadOnly:  true,
							},
						},
					},
				},
				Service: kamajiv1.ServiceSpec{
					AdditionalMetadata: kamajiv1.AdditionalMetadata{
						Labels: map[string]string{
							"tenant.clastix.io": nn.Name,
						},
					},
					ServiceType: kamajiv1.ServiceTypeNodePort,
				},
			},
			Kubernetes: kamajiv1.KubernetesSpec{
				Version: cutil.KubernetesVersion,
				Kubelet: kamajiv1.KubeletSpec{
					CGroupFS:              "systemd",
					PreferredAddressTypes: []kamajiv1.KubeletPreferredAddressType{kamajiv1.NodeInternalIP, kamajiv1.NodeHostName, kamajiv1.NodeExternalIP},
					// Required for compatibility with v1.34 kubelet versions.
					ConfigurationJSONPatches: kamaji134CompatibilityPatches,
				},
				AdmissionControllers: kamajiv1.AdmissionControllers{
					"NamespaceLifecycle",
					"LimitRanger",
					"ServiceAccount",
					"AlwaysPullImages",
					"MutatingAdmissionWebhook",
					"ValidatingAdmissionWebhook",
					"ResourceQuota",
					"PodSecurity",
					"PodNodeSelector",
					"NodeRestriction",
					"EventRateLimit",
				},
			},
			Addons: kamajiv1.AddonsSpec{
				CoreDNS:   &kamajiv1.AddonSpec{},
				KubeProxy: &kamajiv1.AddonSpec{},
			},
		},
	}
	if dc.Spec.ClusterEndpoint != nil && dc.Spec.ClusterEndpoint.Keepalived != nil {
		tcp.Spec.NetworkProfile = kamajiv1.NetworkProfileSpec{
			Address: dc.Spec.ClusterEndpoint.Keepalived.VIP,
			Port:    nodePort,
			PodCIDR: "10.244.0.0/14",
		}
	}
	if err := controllerutil.SetOwnerReference(dc, tcp, scheme); err != nil {
		return nil, fmt.Errorf("failed to set owner reference, err: %v", err)
	}
	return tcp, nil
}

func tcpToDPUCluster(ctx context.Context, o client.Object) []reconcile.Request {
	return []reconcile.Request{
		{
			NamespacedName: cutil.GetNamespacedName(o),
		},
	}
}

func daemonsetToDPUCluster(ctx context.Context, o client.Object) []reconcile.Request {
	refs := o.GetOwnerReferences()
	for _, ref := range refs {
		if ref.APIVersion != "cluster-manager.dpu.nvidia.com" || ref.Kind != "DPUCluster" {
			continue
		}
		nn := types.NamespacedName{
			Name:      ref.Name,
			Namespace: o.GetNamespace(),
		}
		return []reconcile.Request{
			{
				NamespacedName: nn,
			},
		}
	}
	return nil
}

func (cm *clusterHandler) copyImagePullSecrets(ctx context.Context, targetNamespace string) ([]string, error) {
	logger := log.FromContext(ctx)

	// Get DPFOperatorConfig to find imagePullSecrets
	dpfOperatorConfig, err := utils.GetDPFOperatorConfig(ctx, cm.Client)
	if err != nil {
		return nil, fmt.Errorf("failed to get DPFOperatorConfig: %w", err)
	}

	// If target namespace is same as operator namespace, secrets are already available
	if targetNamespace == dpfOperatorConfig.Namespace {
		return dpfOperatorConfig.Spec.ImagePullSecrets, nil
	}

	// If no imagePullSecrets configured, return empty list
	if len(dpfOperatorConfig.Spec.ImagePullSecrets) == 0 {
		logger.V(1).Info("no imagePullSecrets configured in DPFOperatorConfig")
		return nil, nil
	}

	copiedSecrets := make([]string, 0, len(dpfOperatorConfig.Spec.ImagePullSecrets))
	for _, secretName := range dpfOperatorConfig.Spec.ImagePullSecrets {
		// Get secret from operator namespace
		srcSecret := &corev1.Secret{}
		if err := cm.Client.Get(ctx, types.NamespacedName{
			Name:      secretName,
			Namespace: dpfOperatorConfig.Namespace,
		}, srcSecret); err != nil {
			if apierrors.IsNotFound(err) {
				logger.V(1).Info("imagePullSecret not found in operator namespace, skipping",
					"secret", secretName, "namespace", dpfOperatorConfig.Namespace)
				continue
			}
			return nil, fmt.Errorf("failed to get imagePullSecret %s: %w", secretName, err)
		}

		// Use server-side apply to create or update secret atomically
		dstSecret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      secretName,
				Namespace: targetNamespace,
			},
			Type: srcSecret.Type,
			Data: srcSecret.Data,
		}
		dstSecret.SetGroupVersionKind(corev1.SchemeGroupVersion.WithKind("Secret"))

		// Patch will create if not exists, or update if exists
		if err := cm.Client.Patch(ctx, dstSecret, client.Apply,
			client.FieldOwner("kamaji-cluster-manager"), client.ForceOwnership); err != nil {
			return nil, fmt.Errorf("failed to apply imagePullSecret %s in namespace %s: %w",
				secretName, targetNamespace, err)
		}

		logger.V(1).Info("applied imagePullSecret in tenant namespace",
			"secret", secretName, "namespace", targetNamespace)

		copiedSecrets = append(copiedSecrets, secretName)
	}

	return copiedSecrets, nil
}

func (cm *clusterHandler) DPFOperatorConfigToDPUClusters(ctx context.Context, o client.Object) []reconcile.Request {
	dcList := &provisioningv1.DPUClusterList{}
	if err := cm.Client.List(ctx, dcList); err != nil {
		klog.Errorf("failed to list DPUClusters for DPFOperatorConfig %s: %v", klog.KObj(o), err)
		return nil
	}
	requests := make([]reconcile.Request, 0, len(dcList.Items))
	for _, dc := range dcList.Items {
		requests = append(requests, reconcile.Request{
			NamespacedName: types.NamespacedName{
				Name:      dc.Name,
				Namespace: dc.Namespace,
			},
		})
	}
	return requests
}

// secretToDPUClusters maps Secret changes relevant to staticKey encryption-at-rest to DPUCluster reconcile requests.
func secretToDPUClusters(ctx context.Context, o client.Object) []reconcile.Request {
	c, ok := staticKeySecretWatchClient.Load().(client.Client)
	if !ok || c == nil {
		return nil
	}
	if clusterName := o.GetLabels()[provisioningv1.DPUClusterNameLabelKey]; clusterName != "" &&
		strings.HasPrefix(o.GetName(), encryptionConfigSecretNamePrefix+"-") {
		return []reconcile.Request{{
			NamespacedName: types.NamespacedName{Name: clusterName, Namespace: o.GetNamespace()},
		}}
	}

	if !isConfiguredStaticKeySourceSecret(ctx, c, o) {
		return nil
	}

	dcList := &provisioningv1.DPUClusterList{}
	if err := c.List(ctx, dcList); err != nil {
		klog.Errorf("failed to list DPUClusters for staticKey Secret %s: %v", klog.KObj(o), err)
		return nil
	}
	requests := make([]reconcile.Request, 0, len(dcList.Items))
	for _, dc := range dcList.Items {
		if dc.Spec.Type != string(provisioningv1.KamajiCluster) {
			continue
		}
		requests = append(requests, reconcile.Request{NamespacedName: types.NamespacedName{Name: dc.Name, Namespace: dc.Namespace}})
	}
	return requests
}

// isConfiguredStaticKeySourceSecret reports whether the Secret is the staticKey source configured in DPFOperatorConfig.
func isConfiguredStaticKeySourceSecret(ctx context.Context, c client.Client, secret client.Object) bool {
	cfg, err := utils.GetDPFOperatorConfig(ctx, c)
	if err != nil {
		return false
	}
	staticKey, ok := staticKeyConfiguration(cfg)
	if !ok {
		return false
	}
	ref := staticKey.KeySecretRef
	return secret.GetNamespace() == cfg.Namespace && secret.GetName() == ref.Name
}
