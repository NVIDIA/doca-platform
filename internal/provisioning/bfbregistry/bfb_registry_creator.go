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
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	PodName       = "bfb-registry"
	ConfigMapName = "bfb-registry-config"
	// NodePortConfigMapName is the ConfigMap in which earlier releases stored the NodePort of
	// the bfb-registry Service. Their Service was owned by the leader Pod and therefore
	// garbage collected when the leader changed, so the leader kept the port there to create
	// it again with the port that is baked into DPU OS cloud-init. The Service is owned by
	// the DPF Operator now, which only reads the ConfigMap, to carry the port over when it
	// replaces the Service of such a release.
	NodePortConfigMapName = "bfb-registry-nodeport"
	// NodePortConfigMapKey is the key of NodePortConfigMapName holding the https NodePort.
	NodePortConfigMapKey = "https"
	LabelPartOf          = "app.kubernetes.io/part-of"
	LabelDPUComponent    = "dpu.nvidia.com/component"
	LabelValue           = "bfb-registry"
	ContainerPort        = 8082
	// HTTPSContainerPort is the only port the bfb-registry container exposes after the
	// HTTPS migration; the plain-HTTP listener is removed (LLD task 4 §5).
	HTTPSContainerPort = 8443
	BFBHostPath        = "/var/lib/nvidia/dpf/bfb"

	// ServerCertSecretName is the Secret (and Certificate CR) holding the bfb-registry
	// HTTPS server certificate, issued by the platform CA Issuer.
	ServerCertSecretName = "bfb-registry-server-cert"
	// ServerCertMountPath is where the server certificate Secret is mounted into the Pod.
	ServerCertMountPath = "/etc/bfb-registry/tls"

	// volumeNameNginxTmp is the emptyDir volume shared between the nginx and
	// cert-reloader containers (holds the rendered nginx.conf and nginx.pid).
	volumeNameNginxTmp = "nginx-tmp"
	// volumeNameServerCert is the volume backed by the HTTPS server certificate Secret.
	volumeNameServerCert = "server-cert"

	// bfbRegistryPodDeleteMaxWait bounds how long we block waiting for the API server to
	// finish deleting the bfb-registry Pod after we issue Delete. On deadline, we log and
	// return without failing the manager so provisioning can start; EnsureBFBRegistry from
	// the DPU reconciler can retry.
	bfbRegistryPodDeletePollInterval = 200 * time.Millisecond
	bfbRegistryPodDeleteMaxWait      = 15 * time.Second

	// failedPodRecreateBackoff is how long a terminated bfb-registry Pod is kept before it
	// is replaced: while the node is under pressure the kubelet rejects every new Pod within
	// seconds, and replacing it on each rejection event would loop.
	failedPodRecreateBackoff = 30 * time.Second
)

// now is overridden in tests.
var now = time.Now

// BFBRegistryRunnable creates the bfb-registry Pod when the Operator does not run the registry as a
// Deployment, which is the case unless the BFB PVC is ReadWriteMany. The
// Service and the server Certificate in front of it belong to the DPF Operator, and the Pod waits
// for the Secret of the Certificate before it starts.
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
	// The server-cert volume is optional: false, so the Pod stays in ContainerCreating until
	// the DPF Operator's Certificate has produced the Secret.
	if _, err := r.ensurePod(ctx, namespace, nodeName, registryImage, pod); err != nil {
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

// ensurePod ensures a bfb-registry Pod owned by the leader Pod exists, replacing one owned by
// another leader, or a terminated one after failedPodRecreateBackoff. It returns the remaining
// backoff, if any, after which it must be called again.
func (r *BFBRegistryRunnable) ensurePod(ctx context.Context, namespace, nodeName, image string, leaderPod *corev1.Pod) (time.Duration, error) {
	ownerRef := leaderControllerRef(leaderPod)
	existing := &corev1.Pod{}
	err := r.Client.Get(ctx, client.ObjectKey{Namespace: namespace, Name: PodName}, existing)
	if err != nil {
		if !apierrors.IsNotFound(err) {
			return 0, err
		}
		desired := r.desiredPod(namespace, nodeName, image, ownerRef)
		if err := r.Client.Create(ctx, desired); err != nil {
			if apierrors.IsAlreadyExists(err) {
				return 0, nil
			}
			return 0, err
		}
		return 0, nil
	}
	if podOwnedByLeaderPod(existing, leaderPod) {
		terminated := existing.Status.Phase == corev1.PodFailed || existing.Status.Phase == corev1.PodSucceeded
		if !terminated || !existing.DeletionTimestamp.IsZero() {
			return 0, nil
		}
		if remaining := failedPodRecreateBackoff - now().Sub(existing.CreationTimestamp.Time); remaining > 0 {
			return remaining, nil
		}
		log.FromContext(ctx).Info("replacing terminated bfb-registry pod", "phase", existing.Status.Phase,
			"reason", existing.Status.Reason, "message", existing.Status.Message)
	}
	if err := r.Client.Delete(ctx, existing, client.GracePeriodSeconds(0)); err != nil && !apierrors.IsNotFound(err) {
		return 0, err
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
			return 0, nil
		}
		return 0, fmt.Errorf("waiting for bfb-registry pod deletion: %w", err)
	}
	desired := r.desiredPod(namespace, nodeName, image, ownerRef)
	if err := r.Client.Create(ctx, desired); err != nil {
		if apierrors.IsAlreadyExists(err) {
			return 0, nil
		}
		return 0, err
	}
	return 0, nil
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
			Labels:          RegistryPodLabels(),
			OwnerReferences: []metav1.OwnerReference{*ownerRef},
		},
		Spec: corev1.PodSpec{
			NodeName:                      nodeName,
			ImagePullSecrets:              r.ImagePullSecrets,
			TerminationGracePeriodSeconds: ptr.To(int64(0)),
			// Share the PID namespace so the cert-reloader sidecar can send SIGHUP
			// (nginx -s reload) to the nginx master in the other container.
			ShareProcessNamespace: ptr.To(true),
			SecurityContext: &corev1.PodSecurityContext{
				FSGroup: ptr.To(int64(65532)),
			},
			Containers: []corev1.Container{
				{
					Name:    "bfb-registry",
					Image:   image,
					Command: []string{"/bin/sh", "-c", "envsubst '${NGINX_HTTPS_PORT}' < /nginx/nginx.conf.template > /nginx/nginx.conf && /usr/local/nginx/sbin/nginx -c /nginx/nginx.conf -g \"daemon off;\""},
					Ports:   []corev1.ContainerPort{{Name: "https", ContainerPort: HTTPSContainerPort}},
					Env:     []corev1.EnvVar{{Name: "NGINX_HTTPS_PORT", Value: fmt.Sprintf("%d", HTTPSContainerPort)}},
					SecurityContext: &corev1.SecurityContext{
						RunAsUser:  ptr.To(int64(65532)),
						RunAsGroup: ptr.To(int64(65532)),
						Privileged: ptr.To(true),
					},
					VolumeMounts: []corev1.VolumeMount{
						{Name: "bfb", MountPath: "/bfb"},
						{Name: "config", MountPath: "/nginx/nginx.conf.template", SubPath: "nginx.conf", ReadOnly: true},
						{Name: volumeNameNginxTmp, MountPath: "/nginx"},
						// non-subPath so kubelet refreshes the mount in place on cert rotation.
						{Name: volumeNameServerCert, MountPath: ServerCertMountPath, ReadOnly: true},
					},
				},
				{
					Name:  "cert-reloader",
					Image: image,
					Command: []string{
						"/usr/local/bin/certreloader",
						"-cert-dir", ServerCertMountPath,
						"-nginx-bin", "/usr/local/nginx/sbin/nginx",
						"-nginx-conf", "/nginx/nginx.conf",
					},
					SecurityContext: &corev1.SecurityContext{
						RunAsUser:  ptr.To(int64(65532)),
						RunAsGroup: ptr.To(int64(65532)),
						// The nginx container is privileged and therefore runs AppArmor
						// "unconfined". AppArmor mediates signals by profile: a container
						// under the default containerd profile may only signal peers in the
						// same profile, so "nginx -s reload" (SIGHUP to the nginx master in
						// the other container) is denied with EPERM. Run the reloader
						// unconfined too so it shares the nginx AppArmor domain and the
						// graceful reload succeeds.
						//
						// This "unconfined" workaround only exists because the nginx
						// container is privileged (added in commit 0d51c1da6 "fix: add
						// privilege for init-container and registry pod", to let the non-root
						// uid 65532 nginx read the root-owned hostPath BFB directory). We
						// should drop Privileged from the nginx container (e.g. serve BFBs
						// from a PVC, or fix the hostPath ownership/fsGroup) so both
						// containers run under the same default AppArmor profile; then this
						// explicit Unconfined override can be removed and the reload will be
						// allowed as a same-profile signal.
						AppArmorProfile: &corev1.AppArmorProfile{
							Type: corev1.AppArmorProfileTypeUnconfined,
						},
					},
					VolumeMounts: []corev1.VolumeMount{
						// Shares /nginx (nginx.pid) and the cert dir with the nginx container.
						{Name: volumeNameNginxTmp, MountPath: "/nginx"},
						{Name: volumeNameServerCert, MountPath: ServerCertMountPath, ReadOnly: true},
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
				{Name: volumeNameNginxTmp, VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}},
				{
					// optional: false keeps the Pod in ContainerCreating until the cert
					// Secret exists; non-subPath so kubelet syncs rotations into the mount.
					Name: volumeNameServerCert,
					VolumeSource: corev1.VolumeSource{
						Secret: &corev1.SecretVolumeSource{
							SecretName:  ServerCertSecretName,
							DefaultMode: ptr.To(int32(0o440)),
							Optional:    ptr.To(false),
						},
					},
				},
				bfbVol,
			},
		},
	}
}

// RegistryPodLabels are the labels of the registry Pods, whoever creates them. The bfb-registry
// Service selects the Pods by them.
func RegistryPodLabels() map[string]string {
	return map[string]string{
		LabelPartOf:       LabelValue,
		LabelDPUComponent: LabelValue,
	}
}

// APIServerVIPFromDMSPodEnvs extracts the KUBERNETES_SERVICE_HOST value from the DMS Pod
// environment strings ("KEY=VALUE", built from the provisioning controller's --dms-pod-envs
// flag, which the operator populates from DPFOperatorConfig KubernetesAPIServerVIP). It returns
// "" when the variable is not present. This is the VIP the hostagent/DMS Pod uses to reach the
// bfb-registry NodePort, and the host of the registry address for the Redfish install. The DPF
// Operator puts the same VIP in the SANs of the server certificate.
func APIServerVIPFromDMSPodEnvs(envs []string) string {
	const prefix = "KUBERNETES_SERVICE_HOST="
	for _, e := range envs {
		if strings.HasPrefix(e, prefix) {
			return strings.TrimPrefix(e, prefix)
		}
	}
	return ""
}

// EnsureBFBRegistryDeps carries client and controller options used when ensuring the bfb-registry Pod.
type EnsureBFBRegistryDeps struct {
	Client           client.Client
	BFBPVC           string
	ImagePullSecrets []corev1.LocalObjectReference
}

// EnsureBFBRegistry ensures the bfb-registry Pod exists in the given namespace. A non-zero
// duration is when to call it again.
func EnsureBFBRegistry(ctx context.Context, deps EnsureBFBRegistryDeps, namespace, leaderPodName, nodeName, registryImage string) (time.Duration, error) {
	leaderPod := &corev1.Pod{}
	if err := deps.Client.Get(ctx, client.ObjectKey{Namespace: namespace, Name: leaderPodName}, leaderPod); err != nil {
		return 0, fmt.Errorf("get leader pod %s/%s: %w", namespace, leaderPodName, err)
	}

	run := &BFBRegistryRunnable{
		Client:           deps.Client,
		BFBPVC:           deps.BFBPVC,
		ImagePullSecrets: deps.ImagePullSecrets,
	}
	return run.ensurePod(ctx, namespace, nodeName, registryImage, leaderPod)
}
