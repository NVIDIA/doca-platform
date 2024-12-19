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

package webhooks

import (
	"context"
	"fmt"
	"slices"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
)

// NetworkInjector is a component that can inject Multus annotations and resources on Pods
type NetworkInjector struct {
	// Client is the client to the Kubernetes API server
	Client client.Reader
	// Settings are the settings for this component
	Settings NetworkInjectorSettings
}

// NetworkInjectorSettings are the settings for the Network Injector
type NetworkInjectorSettings struct {
	// NADName is the name of the network attachment definition that the injector should use to configure VFs for the
	// default network
	NADName string
	// NADNamespace is the namespace of the network attachment definition that the injector should use to configure VFs
	// for the default network
	NADNamespace string
}

const (
	// netAttachDefResourceNameAnnotation is the key of the network attachment definition annotation that indicates the
	// resource name.
	netAttachDefResourceNameAnnotation = "k8s.v1.cni.cncf.io/resourceName"
	// annotationKeyToBeInjected is the multus annotation we inject to the pods so that multus can inject the VFs
	annotationKeyToBeInjected = "v1.multus-cni.io/default-network"
)

var (
	controlPlaneNodeLabels = map[string]bool{
		"node-role.kubernetes.io/master":        true,
		"node-role.kubernetes.io/control-plane": true,
	}
)

var _ webhook.CustomDefaulter = &NetworkInjector{}

// +kubebuilder:webhook:path=/mutate--v1-pod,mutating=true,failurePolicy=fail,sideEffects=None,groups="",resources=pods,verbs=create,versions=v1,name=network-injector.dpu.nvidia.com,admissionReviewVersions=v1
// +kubebuilder:rbac:groups=k8s.cni.cncf.io,resources=network-attachment-definitions,verbs=get;list;watch;

func (webhook *NetworkInjector) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(&corev1.Pod{}).
		WithDefaulter(webhook).
		Complete()
}

// Default implements webhook.Defaulter so a webhook will be registered for the type.
func (webhook *NetworkInjector) Default(ctx context.Context, obj runtime.Object) error {
	log := ctrl.LoggerFrom(ctx)

	pod, ok := obj.(*corev1.Pod)
	if !ok {
		return apierrors.NewBadRequest(fmt.Sprintf("expected a Pod but got a %T", obj))
	}

	ctrl.LoggerInto(ctx, log.WithValues("Pod", types.NamespacedName{Namespace: pod.Namespace, Name: pod.Name}))
	// If the pod is on the host network no-op.
	if pod.Spec.HostNetwork {
		return nil
	}

	// If the pod explicitly selects a control plane node no-op.
	controlPlanePod, err := webhook.isScheduledToControlPlane(ctx, pod)
	if err != nil {
		return err
	}
	if controlPlanePod {
		return nil
	}

	vfResourceName, err := getVFResourceName(ctx, webhook.Client, webhook.Settings.NADName, webhook.Settings.NADNamespace)
	if err != nil {
		return fmt.Errorf("error while getting VF resource name: %w", err)
	}

	return injectNetworkResources(ctx, pod, webhook.Settings.NADName, webhook.Settings.NADNamespace, vfResourceName)
}

// getVFResourceName gets the resource name that relates to the VFs that should be injected.
func getVFResourceName(ctx context.Context, c client.Reader, netAttachDefName string, netAttachDefNamespace string) (corev1.ResourceName, error) {
	netAttachDef := &unstructured.Unstructured{}
	netAttachDef.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "k8s.cni.cncf.io",
		Version: "v1",
		Kind:    "NetworkAttachmentDefinition",
	})
	key := client.ObjectKey{Namespace: netAttachDefNamespace, Name: netAttachDefName}
	if err := c.Get(ctx, key, netAttachDef); err != nil {
		return "", fmt.Errorf("error while getting %s %s: %w", netAttachDef.GetObjectKind().GroupVersionKind().String(), key.String(), err)
	}

	if v, ok := netAttachDef.GetAnnotations()[netAttachDefResourceNameAnnotation]; ok {
		return corev1.ResourceName(v), nil
	}

	return "", fmt.Errorf("resource can't be found in network attachment definition because annotation %s doesn't exist", netAttachDefResourceNameAnnotation)
}

// If the pod has nodeAffinity set to a specific name check if the node it's scheduled to is a control plane node.
// This is the case for pods created by DaemonSets which set node affinity matching to a node name on creation.
func (webhook *NetworkInjector) isScheduledToControlPlane(ctx context.Context, pod *corev1.Pod) (bool, error) {
	// If the pod has a control plane node selector no-op.
	for label := range controlPlaneNodeLabels {
		if _, ok := pod.Spec.NodeSelector[label]; ok {
			return true, nil
		}
	}
	if terms := getNodeSelectorTerms(pod); terms != nil {
		for _, term := range terms {
			// If the pod selects for a control plane node label return true.
			if term.MatchExpressions != nil {
				for _, expression := range term.MatchExpressions {
					if _, ok := controlPlaneNodeLabels[expression.Key]; ok {
						if expression.Operator == corev1.NodeSelectorOpExists {
							return true, nil
						}

						if expression.Operator == corev1.NodeSelectorOpIn &&
							slices.Contains(expression.Values, "") {
							return true, nil
						}
					}
				}
			}

			// If the pod selects a specific control plane nodename. This is the case for DaemonSet pods.
			isControlPlanePod, err := webhook.hasControlPlaneNodeName(ctx, term)
			if err != nil {
				return false, err
			}
			if isControlPlanePod {
				return true, nil
			}
		}
	}
	return false, nil
}

func (webhook *NetworkInjector) hasControlPlaneNodeName(ctx context.Context, term corev1.NodeSelectorTerm) (bool, error) {
	var nodeName string
	if term.MatchFields != nil {
		for _, field := range term.MatchFields {
			if field.Key == "metadata.name" {
				nodeName = field.Values[0]
			}
		}
	}
	if nodeName == "" {
		return false, nil
	}
	node := &corev1.Node{}
	if err := webhook.Client.Get(ctx, client.ObjectKey{Namespace: "", Name: nodeName}, node); err != nil {
		return false, fmt.Errorf("failed to get node pod is scheduled to %q: %w", nodeName, err)
	}
	if node.Labels == nil {
		return false, nil
	}

	for label := range controlPlaneNodeLabels {
		if _, ok := node.Labels[label]; ok {
			return true, nil
		}
	}
	return false, nil
}

func getNodeSelectorTerms(pod *corev1.Pod) []corev1.NodeSelectorTerm {
	if pod.Spec.Affinity != nil &&
		pod.Spec.Affinity.NodeAffinity != nil &&
		pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution != nil &&
		pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms != nil {
		return pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms
	}
	return nil
}

func injectNetworkResources(ctx context.Context, pod *corev1.Pod, netAttachDefName string, netAttachDefNamespace string, vfResourceName corev1.ResourceName) error {
	log := ctrl.LoggerFrom(ctx)
	// Inject device requests. One additional VF.
	if pod.Spec.Containers[0].Resources.Requests == nil {
		pod.Spec.Containers[0].Resources.Requests = corev1.ResourceList{}
	}
	if pod.Spec.Containers[0].Resources.Limits == nil {
		pod.Spec.Containers[0].Resources.Limits = corev1.ResourceList{}

	}
	if _, ok := pod.Spec.Containers[0].Resources.Requests[vfResourceName]; ok {
		res := pod.Spec.Containers[0].Resources.Requests[vfResourceName]
		res.Add(resource.MustParse("1"))
		pod.Spec.Containers[0].Resources.Requests[vfResourceName] = res
	} else {
		pod.Spec.Containers[0].Resources.Requests[vfResourceName] = resource.MustParse("1")
	}

	if _, ok := pod.Spec.Containers[0].Resources.Limits[vfResourceName]; ok {
		res := pod.Spec.Containers[0].Resources.Limits[vfResourceName]
		res.Add(resource.MustParse("1"))
		pod.Spec.Containers[0].Resources.Limits[vfResourceName] = res
	} else {
		pod.Spec.Containers[0].Resources.Limits[vfResourceName] = resource.MustParse("1")
	}
	if pod.Annotations == nil {
		pod.Annotations = map[string]string{}
	}
	pod.Annotations[annotationKeyToBeInjected] = fmt.Sprintf("%s/%s", netAttachDefNamespace, netAttachDefName)
	log.Info(fmt.Sprintf("injected resource %v into pod", vfResourceName))
	return nil
}
