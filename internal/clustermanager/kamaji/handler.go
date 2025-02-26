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
	"text/template"
	"time"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/internal/clustermanager/controller"
	kamajiv1 "github.com/nvidia/doca-platform/internal/kamaji/api/v1alpha1"
	"github.com/nvidia/doca-platform/internal/operator/inventory"
	"github.com/nvidia/doca-platform/internal/operator/utils"
	cutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/util"

	"github.com/Masterminds/sprig/v3"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// +kubebuilder:rbac:groups=kamaji.clastix.io,resources=tenantcontrolplanes,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=apps,resources=daemonsets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=services,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch;create;update;patch;delete

const (
	kubernetesNodeRoleMaster       = "node-role.kubernetes.io/master"
	kubernetesNodeRoleControlPlane = "node-role.kubernetes.io/control-plane"
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
)

func init() {
	controller.RegisterWatch(&kamajiv1.TenantControlPlane{}, handler.EnqueueRequestsFromMapFunc(tcpToDPUCluster))
	controller.RegisterWatch(&appsv1.DaemonSet{}, handler.EnqueueRequestsFromMapFunc(daemonsetToDPUCluster))
}

type clusterHandler struct {
	client.Client
	Scheme *runtime.Scheme
}

func NewHandler(client client.Client, scheme *runtime.Scheme) controller.ClusterHandler {
	return &clusterHandler{Client: client, Scheme: scheme}
}

func (cm *clusterHandler) ReconcileCluster(ctx context.Context, dc *provisioningv1.DPUCluster) (string, []metav1.Condition, error) {
	var conds []metav1.Condition
	kubeconfig, nodePort, cond, err := cm.reconcileKamaji(ctx, dc)
	if err != nil {
		return "", nil, err
	} else if cond != nil {
		conds = append(conds, *cond)
	}

	cond, err = cm.reconcileKeepalived(ctx, dc, nodePort)
	if err != nil {
		return "", nil, err
	} else if cond != nil {
		conds = append(conds, *cond)
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

func (cm *clusterHandler) reconcileKeepalived(ctx context.Context, dc *provisioningv1.DPUCluster, nodePort int32) (*metav1.Condition, error) {
	if dc.Spec.ClusterEndpoint == nil || dc.Spec.ClusterEndpoint.Keepalived == nil || nodePort == 0 {
		return nil, nil
	}
	values := struct {
		Name            string
		Interface       string
		VirtualRouterID int
		VirtualIP       string
		NodePort        int32
		NodeSelector    map[string]string
	}{
		Name:            fmt.Sprintf("%s-keepalived", dc.Name),
		Interface:       dc.Spec.ClusterEndpoint.Keepalived.Interface,
		VirtualRouterID: dc.Spec.ClusterEndpoint.Keepalived.VirtualRouterID,
		VirtualIP:       dc.Spec.ClusterEndpoint.Keepalived.VIP,
		NodePort:        nodePort,
		NodeSelector:    dc.Spec.ClusterEndpoint.Keepalived.NodeSelector,
	}
	tc, tcCancel := context.WithTimeout(ctx, 5*time.Second)
	defer tcCancel()
	ds := &appsv1.DaemonSet{}
	if err := cm.Client.Get(tc, types.NamespacedName{Namespace: dc.Namespace, Name: values.Name}, ds); err == nil {
		return cutil.NewCondition("KeepalivedReady", nil, "Deployed", ""), nil
	} else if !apierrors.IsNotFound(err) {
		return nil, fmt.Errorf("failed to get keepalived Daemonset, err: %v", err)
	}

	buf := bytes.NewBuffer(nil)
	if err := tmpl.Execute(buf, values); err != nil {
		return nil, fmt.Errorf("failed to execute template, err: %v", err)
	}
	objs, err := utils.BytesToUnstructured(buf.Bytes())
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
			return "", 0, nil, fmt.Errorf("failed to get TCP, err: %v", err)
		}
		tcpCreated = false
	}
	if !svcCreated && !tcpCreated {
		if err := cm.Client.Create(ctx, expectedService(dc)); err != nil {
			return "", 0, nil, fmt.Errorf("failed to create service, err: %v", err)
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
		var err error
		tcp, err = expectedTenantControlPlane(dc, cm.Scheme, nodePort)
		if err != nil {
			return "", 0, nil, fmt.Errorf("failed to generate expected TCP, err: %v", err)
		}
		if err := cm.Client.Create(ctx, tcp); err != nil {
			return "", 0, nil, fmt.Errorf("failed to create cluster request, err: %v", err)
		}
	} else {
		nodePort = tcp.Spec.NetworkProfile.Port
	}

	tcpStatus := tcp.Status.Kubernetes.Version.Status
	if tcpStatus == nil || *tcpStatus != kamajiv1.VersionReady {
		return "", nodePort, cutil.NewCondition(string(provisioningv1.ConditionCreated), fmt.Errorf("creating cluster"), "ClusterNotReady", ""), nil
	}
	return adminKubeconfigName(dc), nodePort, cutil.NewCondition(string(provisioningv1.ConditionCreated), nil, "Created", ""), nil
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

func expectedTenantControlPlane(dc *provisioningv1.DPUCluster, scheme *runtime.Scheme, nodePort int32) (*kamajiv1.TenantControlPlane, error) {
	nn := kamajiTCPName(dc)
	tcp := &kamajiv1.TenantControlPlane{
		ObjectMeta: metav1.ObjectMeta{
			Name:      nn.Name,
			Namespace: nn.Namespace,
			Labels: map[string]string{
				"tenant.clastix.io":               nn.Name,
				provisioningv1.DPUClusterLabelKey: dc.Name,
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
				Version: dc.Spec.Version,
				Kubelet: kamajiv1.KubeletSpec{
					CGroupFS: "systemd",
				},
				AdmissionControllers: kamajiv1.AdmissionControllers{
					"ResourceQuota",
					"LimitRanger",
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
