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
	"errors"
	"fmt"
	"os"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
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

	// bfbRegistryPodDeleteMaxWait bounds how long we block waiting for the API server to
	// finish deleting the bfb-registry Pod after we issue Delete. On deadline, we log and
	// return without failing the manager so provisioning can start; EnsureBFBRegistry from
	// the DPU reconciler can retry.
	bfbRegistryPodDeletePollInterval = 200 * time.Millisecond
	bfbRegistryPodDeleteMaxWait      = 15 * time.Second
)

// BFBRegistryRunnable creates the bfb-registry Pod and Service when the provisioning controller.
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

	if err := r.removeLegacyDaemonSet(ctx, namespace); err != nil {
		return err
	}
	if err := r.ensurePod(ctx, namespace, nodeName, registryImage, pod); err != nil {
		return err
	}
	if err := r.ensureService(ctx, namespace, pod); err != nil {
		return err
	}
	<-ctx.Done()
	return ctx.Err()
}

// removeLegacyDaemonSet deletes the legacy bfb-registry DaemonSet if present
func (r *BFBRegistryRunnable) removeLegacyDaemonSet(ctx context.Context, namespace string) error {
	ds := &appsv1.DaemonSet{}
	err := r.Client.Get(ctx, client.ObjectKey{Namespace: namespace, Name: PodName}, ds)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return err
	}
	if err := r.Client.Delete(ctx, ds); err != nil && !apierrors.IsNotFound(err) {
		return err
	}
	return nil
}

func serviceOwnedByLeaderPod(svc *corev1.Service, leaderPod *corev1.Pod) bool {
	for i := range svc.OwnerReferences {
		ref := &svc.OwnerReferences[i]
		if ref.Kind == "Pod" && ref.Name == leaderPod.Name && ref.UID == leaderPod.UID &&
			ref.Controller != nil && *ref.Controller {
			return true
		}
	}
	return false
}

func podOwnedByLeaderPod(pod *corev1.Pod, leaderPod *corev1.Pod) bool {
	for i := range pod.OwnerReferences {
		ref := &pod.OwnerReferences[i]
		if ref.Kind == "Pod" && ref.Name == leaderPod.Name && ref.UID == leaderPod.UID &&
			ref.Controller != nil && *ref.Controller {
			return true
		}
	}
	return false
}

func leaderControllerRef(leaderPod *corev1.Pod) *metav1.OwnerReference {
	ref := metav1.NewControllerRef(leaderPod, corev1.SchemeGroupVersion.WithKind("Pod"))
	ref.Controller = ptr.To(true)
	ref.BlockOwnerDeletion = ptr.To(true)
	return ref
}

func (r *BFBRegistryRunnable) ensurePod(ctx context.Context, namespace, nodeName, image string, leaderPod *corev1.Pod) error {
	ownerRef := leaderControllerRef(leaderPod)
	existing := &corev1.Pod{}
	err := r.Client.Get(ctx, client.ObjectKey{Namespace: namespace, Name: PodName}, existing)
	if err != nil {
		if !apierrors.IsNotFound(err) {
			return err
		}
		desired := r.desiredPod(namespace, nodeName, image, ownerRef)
		if err := r.Client.Create(ctx, desired); err != nil {
			if apierrors.IsAlreadyExists(err) {
				return nil
			}
			return err
		}
		return nil
	}
	if podOwnedByLeaderPod(existing, leaderPod) {
		return nil
	}
	if err := r.Client.Delete(ctx, existing, client.GracePeriodSeconds(0)); err != nil && !apierrors.IsNotFound(err) {
		return err
	}
	logger := log.FromContext(ctx)
	if err := wait.PollUntilContextTimeout(ctx, bfbRegistryPodDeletePollInterval, bfbRegistryPodDeleteMaxWait, true, func(ctx context.Context) (bool, error) {
		err := r.Client.Get(ctx, client.ObjectKey{Namespace: namespace, Name: PodName}, &corev1.Pod{})
		if apierrors.IsNotFound(err) {
			return true, nil
		}
		if err != nil {
			return false, err
		}
		return false, nil
	}); err != nil {
		// DeadlineExceeded means the pod is still terminating (e.g. finalizers). Do not fail
		// manager.Start; the DPU controller watch will call EnsureBFBRegistry again.
		if errors.Is(err, context.DeadlineExceeded) {
			logger.Error(err, "timed out waiting for bfb-registry pod deletion; continuing without recreating pod, will retry from reconcile",
				"maxWait", bfbRegistryPodDeleteMaxWait)
			return nil
		}
		return fmt.Errorf("waiting for bfb-registry pod deletion: %w", err)
	}
	desired := r.desiredPod(namespace, nodeName, image, ownerRef)
	if err := r.Client.Create(ctx, desired); err != nil {
		if apierrors.IsAlreadyExists(err) {
			return nil
		}
		return err
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
						Privileged: ptr.To(true),
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

func (r *BFBRegistryRunnable) ensureService(ctx context.Context, namespace string, leaderPod *corev1.Pod) error {
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      PodName,
		},
	}
	mutateFn := func() error {
		ownerRef := leaderControllerRef(leaderPod)
		svc.OwnerReferences = []metav1.OwnerReference{*ownerRef}
		svc.Labels = map[string]string{}
		svc.Annotations = map[string]string{}

		svc.Spec.Type = corev1.ServiceTypeNodePort
		svc.Spec.Selector = bfbRegistryPodLabels()
		svc.Spec.Ports = []corev1.ServicePort{
			{
				Name:       "http",
				Port:       int32(ContainerPort),
				TargetPort: intstr.FromInt(ContainerPort),
			},
		}
		return nil
	}
	if _, err := controllerutil.CreateOrUpdate(ctx, r.Client, svc, mutateFn); err != nil {
		// In HA/restart windows, a stale cache read can race with another writer:
		// Get returns NotFound, then Create returns AlreadyExists. Treat this as
		// idempotent success to avoid crashing the manager; later reconciles keep
		// converging Service spec/ownership via CreateOrUpdate.
		if apierrors.IsAlreadyExists(err) {
			log.FromContext(ctx).Info("bfb-registry service already exists during ensure; continuing", "service", PodName)
			return nil
		}
		return err
	}
	return nil
}

// EnsureBFBRegistryDeps carries client and controller options used when ensuring bfb-registry objects.
type EnsureBFBRegistryDeps struct {
	Client           client.Client
	BFBPVC           string
	ImagePullSecrets []corev1.LocalObjectReference
}

// EnsureBFBRegistry ensures the bfb-registry Pod and Service exist in the given namespace.
func EnsureBFBRegistry(ctx context.Context, deps EnsureBFBRegistryDeps, namespace, leaderPodName, nodeName, registryImage string) error {
	leaderPod := &corev1.Pod{}
	if err := deps.Client.Get(ctx, client.ObjectKey{Namespace: namespace, Name: leaderPodName}, leaderPod); err != nil {
		return fmt.Errorf("get leader pod %s/%s: %w", namespace, leaderPodName, err)
	}

	run := &BFBRegistryRunnable{
		Client:           deps.Client,
		BFBPVC:           deps.BFBPVC,
		ImagePullSecrets: deps.ImagePullSecrets,
	}
	if err := run.ensurePod(ctx, namespace, nodeName, registryImage, leaderPod); err != nil {
		return err
	}
	return run.ensureService(ctx, namespace, leaderPod)
}
