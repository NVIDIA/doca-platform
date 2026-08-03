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
	"net"
	"os"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
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
	// HTTPSContainerPort is the only port the bfb-registry container exposes after the
	// HTTPS migration; the plain-HTTP listener is removed (LLD task 4 §5).
	HTTPSContainerPort = 8443
	BFBHostPath        = "/var/lib/nvidia/dpf/bfb"

	// ServerCertSecretName is the Secret (and Certificate CR) holding the bfb-registry
	// HTTPS server certificate, issued by the provisioning CA Issuer.
	ServerCertSecretName = "bfb-registry-server-cert"
	// ServerCertIssuerName is the cert-manager CA Issuer that signs the server certificate.
	ServerCertIssuerName = "dpf-provisioning-issuer"
	// ServerCertMountPath is where the server certificate Secret is mounted into the Pod.
	ServerCertMountPath = "/etc/bfb-registry/tls"

	// volumeNameNginxTmp is the emptyDir volume shared between the nginx and
	// cert-reloader containers (holds the rendered nginx.conf and nginx.pid).
	volumeNameNginxTmp = "nginx-tmp"
	// volumeNameServerCert is the volume backed by the HTTPS server certificate Secret.
	volumeNameServerCert = "server-cert"

	// serverCertDuration / serverCertRenewBefore follow the HLD's 30-90 day leaf certs:
	// 90-day lifetime, renewed 30 days early to trigger rotation well ahead of expiry.
	serverCertDuration    = 2160 * time.Hour
	serverCertRenewBefore = 720 * time.Hour

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
	// KubernetesAPIServerVIP is the Kubernetes API server VIP configured for the DMS/hostagent
	// Pod (via DPFOperatorConfig KubernetesAPIServerVIP). When set it is added to the
	// bfb-registry server certificate SANs so the hostagent's VIP-based NodePort BFB download
	// passes TLS verification. Empty when no VIP override is configured.
	KubernetesAPIServerVIP string
}

func (r *BFBRegistryRunnable) Start(ctx context.Context) error {
	logger := log.FromContext(ctx)
	namespace := os.Getenv("POD_NAMESPACE")
	podName := os.Getenv("POD_NAME")
	nodeName := os.Getenv("NODE_NAME")
	nodeIP := os.Getenv("NODE_IP")
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
	// Ensure the server certificate before the Pod: the server-cert volume is
	// optional: false, so the Pod stays in ContainerCreating until the Secret is ready.
	if err := r.ensureServerCertificate(ctx, namespace, nodeIP, pod); err != nil {
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
				Name:       "https",
				Port:       int32(HTTPSContainerPort),
				TargetPort: intstr.FromInt(HTTPSContainerPort),
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

var certificateGVK = schema.GroupVersionKind{Group: "cert-manager.io", Version: "v1", Kind: "Certificate"}

// APIServerVIPFromDMSPodEnvs extracts the KUBERNETES_SERVICE_HOST value from the DMS Pod
// environment strings ("KEY=VALUE", built from the provisioning controller's --dms-pod-envs
// flag, which the operator populates from DPFOperatorConfig KubernetesAPIServerVIP). It returns
// "" when the variable is not present. This is the VIP the hostagent/DMS Pod uses to reach the
// bfb-registry NodePort, and therefore must be covered by the server certificate SANs.
func APIServerVIPFromDMSPodEnvs(envs []string) string {
	const prefix = "KUBERNETES_SERVICE_HOST="
	for _, e := range envs {
		if strings.HasPrefix(e, prefix) {
			return strings.TrimPrefix(e, prefix)
		}
	}
	return ""
}

// serverCertSANs returns the DNS names and IP addresses the bfb-registry server
// certificate must cover.
func serverCertSANs(namespace, nodeIP, apiServerVIP string) (dnsNames, ipAddresses []string) {
	dnsNames = []string{
		PodName,
		fmt.Sprintf("%s.%s", PodName, namespace),
		fmt.Sprintf("%s.%s.svc", PodName, namespace),
		fmt.Sprintf("%s.%s.svc.cluster.local", PodName, namespace),
	}
	// The hostagent BFB download falls back to reaching the bfb-registry NodePort via the
	// Kubernetes API server VIP (KUBERNETES_SERVICE_HOST in the hostagent/DMS Pod). Include
	// every address the hostagent may connect to so the fallback passes TLS verification:
	//   - nodeIP: the node the registry Pod is scheduled on.
	//   - apiServerVIP: the VIP the operator configured for the DMS Pod (threaded from
	//     DPFOperatorConfig KubernetesAPIServerVIP via --dms-pod-envs); this is what the
	//     hostagent uses when the override is set.
	//   - KUBERNETES_SERVICE_HOST: the provisioning controller's own env (in-cluster ClusterIP,
	//     or the shared value on setups without a separate VIP).
	// Add all valid, distinct IPs; duplicates and empty/invalid values are skipped.
	seen := map[string]struct{}{}
	for _, ip := range []string{nodeIP, apiServerVIP, os.Getenv("KUBERNETES_SERVICE_HOST")} {
		if net.ParseIP(ip) == nil {
			continue
		}
		if _, ok := seen[ip]; ok {
			continue
		}
		seen[ip] = struct{}{}
		ipAddresses = append(ipAddresses, ip)
	}
	return dnsNames, ipAddresses
}

// ensureServerCertificate creates or updates the cert-manager Certificate that
// produces the bfb-registry HTTPS server cert.
func (r *BFBRegistryRunnable) ensureServerCertificate(ctx context.Context, namespace, nodeIP string, leaderPod *corev1.Pod) error {
	if nodeIP == "" {
		return fmt.Errorf("NODE_IP is empty, cannot build bfb-registry server certificate SANs")
	}
	dnsNames, ipAddresses := serverCertSANs(namespace, nodeIP, r.KubernetesAPIServerVIP)

	cert := &unstructured.Unstructured{}
	cert.SetGroupVersionKind(certificateGVK)
	cert.SetNamespace(namespace)
	cert.SetName(ServerCertSecretName)

	mutateFn := func() error {
		cert.SetOwnerReferences([]metav1.OwnerReference{*leaderControllerRef(leaderPod)})
		spec := map[string]interface{}{
			"secretName":  ServerCertSecretName,
			"commonName":  PodName,
			"duration":    metav1.Duration{Duration: serverCertDuration}.ToUnstructured(),
			"renewBefore": metav1.Duration{Duration: serverCertRenewBefore}.ToUnstructured(),
			"privateKey": map[string]interface{}{
				"algorithm": "ECDSA",
				"size":      int64(256),
			},
			"usages":      []interface{}{"server auth", "digital signature", "key encipherment"},
			"dnsNames":    toInterfaceSlice(dnsNames),
			"ipAddresses": toInterfaceSlice(ipAddresses),
			"issuerRef": map[string]interface{}{
				"name":  ServerCertIssuerName,
				"kind":  "Issuer",
				"group": "cert-manager.io",
			},
		}
		return unstructured.SetNestedMap(cert.Object, spec, "spec")
	}

	if _, err := controllerutil.CreateOrUpdate(ctx, r.Client, cert, mutateFn); err != nil {
		if apierrors.IsAlreadyExists(err) {
			return nil
		}
		return fmt.Errorf("ensure bfb-registry server certificate: %w", err)
	}
	return nil
}

func toInterfaceSlice(in []string) []interface{} {
	out := make([]interface{}, len(in))
	for i, v := range in {
		out[i] = v
	}
	return out
}

// EnsureBFBRegistryDeps carries client and controller options used when ensuring bfb-registry objects.
type EnsureBFBRegistryDeps struct {
	Client           client.Client
	BFBPVC           string
	ImagePullSecrets []corev1.LocalObjectReference
	// KubernetesAPIServerVIP is the Kubernetes API server VIP configured for the DMS/hostagent
	// Pod; when set it is added to the bfb-registry server certificate SANs. See
	// BFBRegistryRunnable.KubernetesAPIServerVIP.
	KubernetesAPIServerVIP string
}

// EnsureBFBRegistry ensures the bfb-registry server Certificate, Pod and Service
// exist in the given namespace.
func EnsureBFBRegistry(ctx context.Context, deps EnsureBFBRegistryDeps, namespace, leaderPodName, nodeName, nodeIP, registryImage string) error {
	leaderPod := &corev1.Pod{}
	if err := deps.Client.Get(ctx, client.ObjectKey{Namespace: namespace, Name: leaderPodName}, leaderPod); err != nil {
		return fmt.Errorf("get leader pod %s/%s: %w", namespace, leaderPodName, err)
	}

	run := &BFBRegistryRunnable{
		Client:                 deps.Client,
		BFBPVC:                 deps.BFBPVC,
		ImagePullSecrets:       deps.ImagePullSecrets,
		KubernetesAPIServerVIP: deps.KubernetesAPIServerVIP,
	}
	if err := run.ensureServerCertificate(ctx, namespace, nodeIP, leaderPod); err != nil {
		return err
	}
	if err := run.ensurePod(ctx, namespace, nodeName, registryImage, leaderPod); err != nil {
		return err
	}
	return run.ensureService(ctx, namespace, leaderPod)
}
