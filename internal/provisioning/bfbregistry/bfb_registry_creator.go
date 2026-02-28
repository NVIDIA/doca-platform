/*
Copyright 2026 NVIDIA

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

package bfbregistry

import (
	"context"
	"fmt"
	"os"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	PodName           = "bfb-registry"
	ConfigMapName     = "bfb-registry-config"
	LabelPartOf       = "app.kubernetes.io/part-of"
	LabelDPUComponent = "dpu.nvidia.com/component"
	LabelValue        = "bfb-registry"
	ContainerPort     = 8082
	BFBHostPath       = "/var/lib/nvidia/dpf/bfb"
	NodePort          = 30082
)

// BFBRegistryRunnable creates the bfb-registry Pod and Service when the provisioning controller becomes leader.
type BFBRegistryRunnable struct {
	Client           client.Client
	BFBPVC           string
	ImagePullSecrets []corev1.LocalObjectReference
}

func (r *BFBRegistryRunnable) Start(ctx context.Context) error {
	logger := log.FromContext(ctx)
	namespace := os.Getenv("POD_NAMESPACE")
	podName := os.Getenv("POD_NAME")
	nodeName := os.Getenv("NODE_NAME")
	registryImage := os.Getenv("BFB_REGISTRY_IMAGE")
	if namespace == "" || podName == "" || nodeName == "" || registryImage == "" {
		logger.Info("bfb-registry leader runnable skipping: required env not set (POD_NAMESPACE, POD_NAME, NODE_NAME, BFB_REGISTRY_IMAGE)",
			"POD_NAMESPACE", namespace, "POD_NAME", podName, "NODE_NAME", nodeName, "BFB_REGISTRY_IMAGE_set", registryImage != "")
		<-ctx.Done()
		return ctx.Err()
	}

	pod := &corev1.Pod{}
	if err := r.Client.Get(ctx, client.ObjectKey{Namespace: namespace, Name: podName}, pod); err != nil {
		return fmt.Errorf("get leader pod %s/%s: %w", namespace, podName, err)
	}
	podOwnerRef := metav1.NewControllerRef(pod, corev1.SchemeGroupVersion.WithKind("Pod"))
	podOwnerRef.Controller = ptr.To(true)
	podOwnerRef.BlockOwnerDeletion = ptr.To(true)

	if err := r.ensurePod(ctx, namespace, nodeName, registryImage, podOwnerRef); err != nil {
		return err
	}
	if err := r.ensureService(ctx, namespace, podOwnerRef); err != nil {
		return err
	}
	<-ctx.Done()
	return ctx.Err()
}

func (r *BFBRegistryRunnable) ensurePod(ctx context.Context, namespace, nodeName, image string, ownerRef *metav1.OwnerReference) error {
	desired := r.desiredPod(namespace, nodeName, image, ownerRef)
	existing := &corev1.Pod{}
	err := r.Client.Get(ctx, client.ObjectKey{Namespace: namespace, Name: PodName}, existing)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return r.Client.Create(ctx, desired)
		}
		return err
	}
	// Pod spec is largely immutable; recreate if spec or owner ref changed.
	if !equality.Semantic.DeepEqual(existing.Spec, desired.Spec) || !ownerRefEqual(existing.OwnerReferences, desired.OwnerReferences) {
		if err := r.Client.Delete(ctx, existing); err != nil && !apierrors.IsNotFound(err) {
			return err
		}
		return r.Client.Create(ctx, desired)
	}
	return nil
}

func (r *BFBRegistryRunnable) desiredPod(namespace, nodeName, image string, ownerRef *metav1.OwnerReference) *corev1.Pod {
	bfbVol := corev1.Volume{Name: "bfb"}
	if r.BFBPVC != "" {
		bfbVol.VolumeSource = corev1.VolumeSource{
			PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: r.BFBPVC},
		}
	} else {
		t := corev1.HostPathDirectoryOrCreate
		bfbVol.VolumeSource = corev1.VolumeSource{
			HostPath: &corev1.HostPathVolumeSource{Path: BFBHostPath, Type: &t},
		}
	}
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:       namespace,
			Name:            PodName,
			Labels:          bfbRegistryPodLabels(),
			OwnerReferences: []metav1.OwnerReference{*ownerRef},
		},
		Spec: corev1.PodSpec{
			NodeName:                      nodeName,
			ImagePullSecrets:              r.ImagePullSecrets,
			TerminationGracePeriodSeconds: ptr.To(int64(0)),
			SecurityContext: &corev1.PodSecurityContext{
				FSGroup: ptr.To(int64(65532)),
			},
			Containers: []corev1.Container{
				{
					Name:    "bfb-registry",
					Image:   image,
					Command: []string{"/bin/sh", "-c", "envsubst '${NGINX_PORT}' < /nginx/nginx.conf.template > /nginx/nginx.conf && /usr/local/nginx/sbin/nginx -c /nginx/nginx.conf -g \"daemon off;\""},
					Ports:   []corev1.ContainerPort{{Name: "http", ContainerPort: ContainerPort}},
					Env:     []corev1.EnvVar{{Name: "NGINX_PORT", Value: fmt.Sprintf("%d", ContainerPort)}},
					SecurityContext: &corev1.SecurityContext{
						RunAsUser:  ptr.To(int64(65532)),
						RunAsGroup: ptr.To(int64(65532)),
					},
					VolumeMounts: []corev1.VolumeMount{
						{Name: "bfb", MountPath: "/bfb"},
						{Name: "config", MountPath: "/nginx/nginx.conf.template", SubPath: "nginx.conf", ReadOnly: true},
						{Name: "nginx-tmp", MountPath: "/nginx"},
					},
				},
			},
			Volumes: []corev1.Volume{
				{
					Name: "config",
					VolumeSource: corev1.VolumeSource{
						ConfigMap: &corev1.ConfigMapVolumeSource{
							LocalObjectReference: corev1.LocalObjectReference{Name: ConfigMapName},
						},
					},
				},
				{Name: "nginx-tmp", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}},
				bfbVol,
			},
		},
	}
}

func bfbRegistryPodLabels() map[string]string {
	return map[string]string{
		LabelPartOf:       LabelValue,
		LabelDPUComponent: LabelValue,
	}
}

func ownerRefEqual(a, b []metav1.OwnerReference) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].UID != b[i].UID || a[i].Name != b[i].Name {
			return false
		}
	}
	return true
}

func (r *BFBRegistryRunnable) ensureService(ctx context.Context, namespace string, ownerRef *metav1.OwnerReference) error {
	desired := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:       namespace,
			Name:            PodName,
			OwnerReferences: []metav1.OwnerReference{*ownerRef},
		},
		Spec: corev1.ServiceSpec{
			Type:     corev1.ServiceTypeNodePort,
			Selector: bfbRegistryPodLabels(),
			Ports: []corev1.ServicePort{
				{
					Name:       "http",
					Port:       int32(ContainerPort),
					TargetPort: intstr.FromInt(ContainerPort),
					NodePort:   NodePort,
				},
			},
		},
	}
	existing := &corev1.Service{}
	err := r.Client.Get(ctx, client.ObjectKey{Namespace: namespace, Name: PodName}, existing)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return r.Client.Create(ctx, desired)
		}
		return err
	}
	if existing.Spec.Type != desired.Spec.Type ||
		!equality.Semantic.DeepEqual(existing.Spec.Ports, desired.Spec.Ports) ||
		!equality.Semantic.DeepEqual(existing.Spec.Selector, desired.Spec.Selector) ||
		!ownerRefEqual(existing.OwnerReferences, desired.OwnerReferences) {
		existing.Spec = desired.Spec
		existing.OwnerReferences = desired.OwnerReferences
		return r.Client.Update(ctx, existing)
	}
	return nil
}
