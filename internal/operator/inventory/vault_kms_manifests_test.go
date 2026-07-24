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

package inventory

import (
	"context"
	"testing"

	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"
	"github.com/nvidia/doca-platform/internal/release"

	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestVaultKMSObjects_Parse(t *testing.T) {
	g := NewWithT(t)
	g.Expect((&vaultKMSObjects{data: vaultKMSData}).Parse()).To(Succeed())

	t.Run("fails without data", func(t *testing.T) {
		NewWithT(t).Expect((&vaultKMSObjects{}).Parse()).To(HaveOccurred())
	})
}

func TestVaultKMSObjects_GenerateManifests(t *testing.T) {
	g := NewWithT(t)
	p := &vaultKMSObjects{data: vaultKMSData}
	g.Expect(p.Parse()).To(Succeed())
	defaults := &release.Defaults{}
	g.Expect(defaults.Parse()).To(Succeed())

	t.Run("no objects when disabled", func(t *testing.T) {
		g := NewWithT(t)
		vars := newDefaultVariables(defaults)
		objs, err := p.GenerateManifests(context.Background(), vars)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(objs).To(BeEmpty())
	})

	t.Run("no objects when enabled but config missing", func(t *testing.T) {
		g := NewWithT(t)
		vars := newDefaultVariables(defaults)
		vars.DisableSystemComponents[operatorv1.VaultKMSName] = false
		objs, err := p.GenerateManifests(context.Background(), vars)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(objs).To(BeEmpty())
	})

	t.Run("variables enable component when config is present", func(t *testing.T) {
		g := NewWithT(t)
		cfg := tokenConfig()
		vars := VariablesFromDPFOperatorConfig(defaults, &operatorv1.DPFOperatorConfig{
			ObjectMeta: metav1.ObjectMeta{Namespace: "dpf-operator-system"},
			Spec: operatorv1.DPFOperatorConfigSpec{
				DeploymentMode:         operatorv1.DeploymentModeHostTrusted,
				ProvisioningController: &operatorv1.ProvisioningControllerConfiguration{},
				Security:               &operatorv1.SecurityConfiguration{VaultKMS: cfg},
			},
		}, nil)

		g.Expect(vars.DisableSystemComponents[operatorv1.VaultKMSName]).To(BeFalse())
		g.Expect(vars.VaultKMS).To(Equal(cfg))
	})

	t.Run("variables disable component when config disables it", func(t *testing.T) {
		g := NewWithT(t)
		cfg := tokenConfig()
		cfg.Disable = ptr.To(true)
		vars := VariablesFromDPFOperatorConfig(defaults, &operatorv1.DPFOperatorConfig{
			ObjectMeta: metav1.ObjectMeta{Namespace: "dpf-operator-system"},
			Spec: operatorv1.DPFOperatorConfigSpec{
				DeploymentMode:         operatorv1.DeploymentModeHostTrusted,
				ProvisioningController: &operatorv1.ProvisioningControllerConfiguration{},
				Security:               &operatorv1.SecurityConfiguration{VaultKMS: cfg},
			},
		}, nil)

		g.Expect(vars.DisableSystemComponents[operatorv1.VaultKMSName]).To(BeTrue())
		g.Expect(vars.VaultKMS).To(Equal(cfg))
	})

	t.Run("token auth", func(t *testing.T) {
		g := NewWithT(t)
		objs, err := p.GenerateManifests(context.Background(), vaultKMSVars(defaults, tokenConfig()))
		g.Expect(err).NotTo(HaveOccurred())

		sa := getObjectOfKind(objs, string(ServiceAccountKind), "dpf-kms-plugin")
		g.Expect(sa).NotTo(BeNil())
		g.Expect(sa.GetLabels()[operatorv1.DPFComponentLabelKey]).To(Equal(operatorv1.VaultKMSName.String()))

		ds := getDaemonSetFromGeneratedObjs(g, objs)
		g.Expect(ds).NotTo(BeNil())
		g.Expect(ds.Labels[operatorv1.DPFComponentLabelKey]).To(Equal(operatorv1.VaultKMSName.String()))
		g.Expect(ds.Spec.Selector.MatchLabels[operatorv1.DPFComponentLabelKey]).To(Equal(operatorv1.VaultKMSName.String()))
		g.Expect(ds.Spec.Template.Labels[operatorv1.DPFComponentLabelKey]).To(Equal(operatorv1.VaultKMSName.String()))
		g.Expect(ds.Spec.Template.Spec.AutomountServiceAccountToken).NotTo(BeNil())
		g.Expect(*ds.Spec.Template.Spec.AutomountServiceAccountToken).To(BeFalse())
		g.Expect(ds.Spec.Template.Spec.ServiceAccountName).To(Equal("dpf-kms-plugin"))

		c := ds.Spec.Template.Spec.Containers[0]
		g.Expect(c.Args).To(ContainElements(
			"--vault-address=https://vault.example:8200",
			"--socket-path=/var/lib/dpf/kmsplugin/vault-kms/kms.sock",
			"--vault-key-name=k8s-etcd",
			"--vault-transit-mount=transit",
			"--vault-auth-method=token",
			"--vault-token-file=/etc/dpf/kms/token/token",
		))
		g.Expect(c.Args).NotTo(ContainElement(ContainSubstring("--vault-token-check-interval=")))
		g.Expect(c.Args).NotTo(ContainElement(ContainSubstring("--vault-login-timeout=")))
		g.Expect(c.SecurityContext).NotTo(BeNil())
		g.Expect(c.SecurityContext.AllowPrivilegeEscalation).NotTo(BeNil())
		g.Expect(*c.SecurityContext.AllowPrivilegeEscalation).To(BeFalse())
		g.Expect(c.SecurityContext.ReadOnlyRootFilesystem).NotTo(BeNil())
		g.Expect(*c.SecurityContext.ReadOnlyRootFilesystem).To(BeTrue())
		g.Expect(c.SecurityContext.Capabilities).NotTo(BeNil())
		g.Expect(c.SecurityContext.Capabilities.Drop).To(ConsistOf(corev1.Capability("ALL")))
		g.Expect(c.SecurityContext.SeccompProfile).NotTo(BeNil())
		g.Expect(c.SecurityContext.SeccompProfile.Type).To(Equal(corev1.SeccompProfileTypeRuntimeDefault))

		tokenVol := volumeByName(ds.Spec.Template.Spec.Volumes, "vault-kms-token")
		g.Expect(tokenVol).NotTo(BeNil())
		g.Expect(tokenVol.Secret).NotTo(BeNil())
		g.Expect(tokenVol.Secret.SecretName).To(Equal("dpf-token"))
		g.Expect(tokenVol.Secret.Items).To(ConsistOf(corev1.KeyToPath{Key: "token", Path: "token"}))

		g.Expect(volumeByName(ds.Spec.Template.Spec.Volumes, "kms-socket")).NotTo(BeNil())

		var mountPaths []string
		for _, m := range c.VolumeMounts {
			mountPaths = append(mountPaths, m.MountPath)
		}
		g.Expect(mountPaths).To(ContainElements("/var/lib/dpf/kmsplugin/vault-kms", "/etc/dpf/kms/token"))
	})

	t.Run("readiness probe", func(t *testing.T) {
		g := NewWithT(t)
		objs, err := p.GenerateManifests(context.Background(), vaultKMSVars(defaults, tokenConfig()))
		g.Expect(err).NotTo(HaveOccurred())

		ds := getDaemonSetFromGeneratedObjs(g, objs)
		g.Expect(ds).NotTo(BeNil())

		c := ds.Spec.Template.Spec.Containers[0]
		g.Expect(c.ReadinessProbe).NotTo(BeNil())
		g.Expect(c.ReadinessProbe.Exec).NotTo(BeNil())
		g.Expect(c.ReadinessProbe.Exec.Command).To(Equal([]string{
			"/dpf-kms-plugin",
			"readyz",
			"--socket-path=/var/lib/dpf/kmsplugin/vault-kms/kms.sock",
		}))
		// The probe runs infrequently to avoid unnecessary load on the backend.
		g.Expect(c.ReadinessProbe.InitialDelaySeconds).To(Equal(int32(10)))
		g.Expect(c.ReadinessProbe.PeriodSeconds).To(Equal(int32(60)))
		g.Expect(c.ReadinessProbe.TimeoutSeconds).To(Equal(int32(30)))
		g.Expect(c.ReadinessProbe.FailureThreshold).To(Equal(int32(2)))
		g.Expect(c.ReadinessProbe.SuccessThreshold).To(Equal(int32(1)))

		// A backend outage must mark the pod NotReady, never restart it, so there
		// is no liveness or startup probe.
		g.Expect(c.LivenessProbe).To(BeNil())
		g.Expect(c.StartupProbe).To(BeNil())
	})

	t.Run("control-plane scheduling and image override", func(t *testing.T) {
		g := NewWithT(t)
		vars := vaultKMSVars(defaults, tokenConfig())
		vars.Images[operatorv1.VaultKMSName.WithContainer(operatorv1.VaultKMSContainer)] = "registry.example/dpf-system:test"
		vars.ImagePullSecrets = []string{"pull-secret"}

		objs, err := p.GenerateManifests(context.Background(), vars)
		g.Expect(err).NotTo(HaveOccurred())

		ds := getDaemonSetFromGeneratedObjs(g, objs)
		g.Expect(ds).NotTo(BeNil())
		g.Expect(ds.Spec.Template.Spec.Containers[0].Image).To(Equal("registry.example/dpf-system:test"))
		g.Expect(ds.Spec.Template.Spec.ImagePullSecrets).To(ConsistOf(corev1.LocalObjectReference{Name: "pull-secret"}))
		g.Expect(ds.Spec.Template.Spec.Affinity).NotTo(BeNil())
		g.Expect(ds.Spec.Template.Spec.Affinity.NodeAffinity).NotTo(BeNil())
		g.Expect(ds.Spec.Template.Spec.Tolerations).To(ContainElements(controlPlaneTolerations))
	})

	t.Run("resources override", func(t *testing.T) {
		g := NewWithT(t)
		vars := vaultKMSVars(defaults, tokenConfig())
		vars.Resources[operatorv1.VaultKMSName.WithContainer(operatorv1.VaultKMSContainer)] = corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("100m"),
				corev1.ResourceMemory: resource.MustParse("128Mi"),
			},
			Limits: corev1.ResourceList{
				corev1.ResourceMemory: resource.MustParse("256Mi"),
			},
		}

		objs, err := p.GenerateManifests(context.Background(), vars)
		g.Expect(err).NotTo(HaveOccurred())

		ds := getDaemonSetFromGeneratedObjs(g, objs)
		g.Expect(ds).NotTo(BeNil())
		g.Expect(ds.Spec.Template.Spec.Containers[0].Resources).To(Equal(vars.Resources[operatorv1.VaultKMSName.WithContainer(operatorv1.VaultKMSContainer)]))
	})

	t.Run("optional timing", func(t *testing.T) {
		g := NewWithT(t)
		cfg := tokenConfig()
		cfg.TokenCheckIntervalSeconds = ptr.To[int32](30)
		cfg.LoginTimeoutSeconds = ptr.To[int32](10)

		objs, err := p.GenerateManifests(context.Background(), vaultKMSVars(defaults, cfg))
		g.Expect(err).NotTo(HaveOccurred())

		ds := getDaemonSetFromGeneratedObjs(g, objs)
		g.Expect(ds).NotTo(BeNil())
		g.Expect(ds.Spec.Template.Spec.Containers[0].Args).To(ContainElements(
			"--vault-token-check-interval=30s",
			"--vault-login-timeout=10s",
		))
	})

	t.Run("approle auth", func(t *testing.T) {
		g := NewWithT(t)
		cfg := &operatorv1.VaultKMSConfiguration{
			Address: "https://vault.example:8200",
			Transit: operatorv1.VaultKMSTransit{KeyName: "k8s-etcd", Mount: ptr.To("transit")},
			Auth: operatorv1.VaultKMSAuth{
				Method: operatorv1.VaultKMSAuthMethodAppRole,
				AppRole: &operatorv1.VaultKMSAppRoleAuth{
					SecretName:          "dpf-approle",
					AuthEngineMountPath: ptr.To("approle"),
					RoleIDKey:           "role_id",
					SecretIDKey:         "secret_id",
				},
			},
		}
		objs, err := p.GenerateManifests(context.Background(), vaultKMSVars(defaults, cfg))
		g.Expect(err).NotTo(HaveOccurred())

		ds := getDaemonSetFromGeneratedObjs(g, objs)
		g.Expect(ds).NotTo(BeNil())
		g.Expect(ds.Spec.Template.Spec.Containers[0].Args).To(ContainElements(
			"--vault-auth-method=approle",
			"--vault-approle-role-id-file=/etc/dpf/kms/approle/role_id",
			"--vault-approle-secret-id-file=/etc/dpf/kms/approle/secret_id",
			"--vault-auth-mount=approle",
		))

		vol := volumeByName(ds.Spec.Template.Spec.Volumes, "vault-kms-approle")
		g.Expect(vol).NotTo(BeNil())
		g.Expect(vol.Secret).NotTo(BeNil())
		g.Expect(vol.Secret.SecretName).To(Equal("dpf-approle"))
		g.Expect(vol.Secret.Items).To(ConsistOf(
			corev1.KeyToPath{Key: "role_id", Path: "role_id"},
			corev1.KeyToPath{Key: "secret_id", Path: "secret_id"},
		))
	})

	t.Run("userpass auth", func(t *testing.T) {
		g := NewWithT(t)
		cfg := &operatorv1.VaultKMSConfiguration{
			Address: "https://vault.example:8200",
			Transit: operatorv1.VaultKMSTransit{KeyName: "k8s-etcd", Mount: ptr.To("transit")},
			Auth: operatorv1.VaultKMSAuth{
				Method: operatorv1.VaultKMSAuthMethodUserpass,
				Userpass: &operatorv1.VaultKMSUserpassAuth{
					SecretName:          "dpf-userpass",
					AuthEngineMountPath: ptr.To("userpass"),
					UsernameKey:         "username",
					PasswordKey:         "password",
				},
			},
		}
		objs, err := p.GenerateManifests(context.Background(), vaultKMSVars(defaults, cfg))
		g.Expect(err).NotTo(HaveOccurred())

		ds := getDaemonSetFromGeneratedObjs(g, objs)
		g.Expect(ds).NotTo(BeNil())
		g.Expect(ds.Spec.Template.Spec.Containers[0].Args).To(ContainElements(
			"--vault-auth-method=userpass",
			"--vault-userpass-username-file=/etc/dpf/kms/userpass/username",
			"--vault-userpass-password-file=/etc/dpf/kms/userpass/password",
			"--vault-auth-mount=userpass",
		))

		vol := volumeByName(ds.Spec.Template.Spec.Volumes, "vault-kms-userpass")
		g.Expect(vol).NotTo(BeNil())
		g.Expect(vol.Secret).NotTo(BeNil())
		g.Expect(vol.Secret.SecretName).To(Equal("dpf-userpass"))
		g.Expect(vol.Secret.Items).To(ConsistOf(
			corev1.KeyToPath{Key: "username", Path: "username"},
			corev1.KeyToPath{Key: "password", Path: "password"},
		))
	})

	t.Run("kubernetes auth", func(t *testing.T) {
		g := NewWithT(t)
		cfg := &operatorv1.VaultKMSConfiguration{
			Address: "https://vault.example:8200",
			Transit: operatorv1.VaultKMSTransit{KeyName: "k8s-etcd", Mount: ptr.To("transit")},
			Auth: operatorv1.VaultKMSAuth{
				Method:     operatorv1.VaultKMSAuthMethodKubernetes,
				Kubernetes: &operatorv1.VaultKMSKubernetesAuth{Role: "dpf-kms", Audience: ptr.To("vault"), AuthEngineMountPath: ptr.To("kubernetes")},
			},
		}
		objs, err := p.GenerateManifests(context.Background(), vaultKMSVars(defaults, cfg))
		g.Expect(err).NotTo(HaveOccurred())

		ds := getDaemonSetFromGeneratedObjs(g, objs)
		g.Expect(ds).NotTo(BeNil())
		g.Expect(ds.Spec.Template.Spec.Containers[0].Args).To(ContainElements(
			"--vault-auth-method=kubernetes",
			"--vault-kubernetes-role=dpf-kms",
			"--vault-kubernetes-jwt-file=/var/run/secrets/vault/token",
			"--vault-auth-mount=kubernetes",
		))

		vol := volumeByName(ds.Spec.Template.Spec.Volumes, "vault-kms-sa-token")
		g.Expect(vol).NotTo(BeNil())
		g.Expect(vol.Projected).NotTo(BeNil())
		g.Expect(vol.Projected.Sources).To(HaveLen(1))
		g.Expect(vol.Projected.Sources[0].ServiceAccountToken).NotTo(BeNil())
		g.Expect(vol.Projected.Sources[0].ServiceAccountToken.Path).To(Equal("token"))
		g.Expect(vol.Projected.Sources[0].ServiceAccountToken.Audience).To(Equal("vault"))
	})

	t.Run("jwt auth", func(t *testing.T) {
		g := NewWithT(t)
		cfg := &operatorv1.VaultKMSConfiguration{
			Address: "https://vault.example:8200",
			Transit: operatorv1.VaultKMSTransit{KeyName: "k8s-etcd", Mount: ptr.To("transit")},
			Auth: operatorv1.VaultKMSAuth{
				Method: operatorv1.VaultKMSAuthMethodJWT,
				JWT: &operatorv1.VaultKMSJWTAuth{
					Role:                "dpf-kms",
					JWTSecretRef:        operatorv1.SecretKeyRef{Name: "dpf-jwt", Key: "jwt"},
					AuthEngineMountPath: ptr.To("jwt"),
				},
			},
		}
		objs, err := p.GenerateManifests(context.Background(), vaultKMSVars(defaults, cfg))
		g.Expect(err).NotTo(HaveOccurred())

		ds := getDaemonSetFromGeneratedObjs(g, objs)
		g.Expect(ds).NotTo(BeNil())
		g.Expect(ds.Spec.Template.Spec.Containers[0].Args).To(ContainElements(
			"--vault-auth-method=jwt",
			"--vault-jwt-role=dpf-kms",
			"--vault-jwt-file=/etc/dpf/kms/jwt/jwt",
			"--vault-auth-mount=jwt",
		))

		vol := volumeByName(ds.Spec.Template.Spec.Volumes, "vault-kms-jwt")
		g.Expect(vol).NotTo(BeNil())
		g.Expect(vol.Secret).NotTo(BeNil())
		g.Expect(vol.Secret.SecretName).To(Equal("dpf-jwt"))
		g.Expect(vol.Secret.Items).To(ConsistOf(corev1.KeyToPath{Key: "jwt", Path: "jwt"}))
	})

	t.Run("TLS and namespace", func(t *testing.T) {
		g := NewWithT(t)
		cfg := tokenConfig()
		cfg.TLS = &operatorv1.VaultKMSTLS{CACertConfigMapRef: &operatorv1.ConfigMapKeyRef{Name: "vault-ca", Key: "ca.crt"}}
		cfg.Namespace = ptr.To("admin")

		objs, err := p.GenerateManifests(context.Background(), vaultKMSVars(defaults, cfg))
		g.Expect(err).NotTo(HaveOccurred())

		ds := getDaemonSetFromGeneratedObjs(g, objs)
		g.Expect(ds).NotTo(BeNil())

		c := ds.Spec.Template.Spec.Containers[0]
		g.Expect(c.Args).To(ContainElement("--vault-ca-cert=/etc/dpf/kms/tls/ca.crt"))
		g.Expect(c.Args).To(ContainElement("--vault-namespace=admin"))

		caVol := volumeByName(ds.Spec.Template.Spec.Volumes, "vault-kms-ca")
		g.Expect(caVol).NotTo(BeNil())
		g.Expect(caVol.ConfigMap).NotTo(BeNil())
		g.Expect(caVol.ConfigMap.Name).To(Equal("vault-ca"))
		g.Expect(caVol.ConfigMap.Items).To(ConsistOf(corev1.KeyToPath{Key: "ca.crt", Path: "ca.crt"}))

		var caMount *corev1.VolumeMount
		for i := range c.VolumeMounts {
			if c.VolumeMounts[i].Name == "vault-kms-ca" {
				caMount = &c.VolumeMounts[i]
				break
			}
		}
		g.Expect(caMount).NotTo(BeNil())
		g.Expect(caMount.MountPath).To(Equal("/etc/dpf/kms/tls"))
		g.Expect(caMount.ReadOnly).To(BeTrue())
		g.Expect(caMount.SubPath).To(BeEmpty())
		g.Expect(caMount.SubPathExpr).To(BeEmpty())

		g.Expect(c.Env).NotTo(ContainElement(corev1.EnvVar{Name: "VAULT_NAMESPACE", Value: "admin"}))
	})
}

func TestVaultKMSObjects_IsReadyForUpgrade(t *testing.T) {
	g := NewWithT(t)
	p := &vaultKMSObjects{data: vaultKMSData}
	g.Expect(p.Parse()).To(Succeed())
	testClient := fake.NewClientBuilder().WithScheme(scheme.Scheme).Build()
	config := &operatorv1.DPFOperatorConfig{
		ObjectMeta: metav1.ObjectMeta{Namespace: "dpf-operator-system"},
		Status: operatorv1.DPFOperatorConfigStatus{
			Version: ptr.To("v26.4.0"),
		},
	}

	g.Expect(p.IsReadyForUpgrade(context.Background(), testClient, config)).To(Succeed())

	config.Status.Version = ptr.To("v26.10.0")
	g.Expect(p.IsReadyForUpgrade(context.Background(), testClient, config)).To(HaveOccurred())
}

func TestVaultKMSObjects_IsReady(t *testing.T) {
	g := NewWithT(t)
	p := &vaultKMSObjects{data: vaultKMSData}
	g.Expect(p.Parse()).To(Succeed())

	t.Run("fails when daemonset targets zero nodes", func(t *testing.T) {
		g := NewWithT(t)
		ds := vaultKMSDaemonSetWithStatus(appsv1.DaemonSetStatus{})
		testClient := fake.NewClientBuilder().WithScheme(scheme.Scheme).WithObjects(ds).Build()

		err := p.IsReady(context.Background(), testClient, "dpf-operator-system")

		g.Expect(err).To(HaveOccurred())
		g.Expect(err.Error()).To(ContainSubstring("has no scheduled pods"))
	})

	t.Run("succeeds when daemonset has available scheduled pods", func(t *testing.T) {
		g := NewWithT(t)
		ds := vaultKMSDaemonSetWithStatus(appsv1.DaemonSetStatus{
			DesiredNumberScheduled: 1,
			UpdatedNumberScheduled: 1,
			NumberAvailable:        1,
		})
		testClient := fake.NewClientBuilder().WithScheme(scheme.Scheme).WithObjects(ds).Build()

		g.Expect(p.IsReady(context.Background(), testClient, "dpf-operator-system")).To(Succeed())
	})
}

// vaultKMSVars returns default variables with the vault-kms component enabled and configured.
func vaultKMSVars(defaults *release.Defaults, cfg *operatorv1.VaultKMSConfiguration) Variables {
	vars := newDefaultVariables(defaults)
	vars.Namespace = "dpf-operator-system"
	vars.DisableSystemComponents[operatorv1.VaultKMSName] = false
	vars.VaultKMS = cfg
	return vars
}

func getDaemonSetFromGeneratedObjs(g Gomega, generatedObjs []client.Object) *appsv1.DaemonSet {
	var daemonSet *appsv1.DaemonSet
	for _, obj := range generatedObjs {
		if obj.GetObjectKind().GroupVersionKind().Kind == string(DaemonSetKind) {
			ds := &appsv1.DaemonSet{}
			unstructuredObj, ok := obj.(*unstructured.Unstructured)
			g.Expect(ok).To(BeTrue())
			err := runtime.DefaultUnstructuredConverter.FromUnstructured(unstructuredObj.UnstructuredContent(), ds)
			g.Expect(err).NotTo(HaveOccurred())
			daemonSet = ds
			break
		}
	}
	return daemonSet
}

func getObjectOfKind(objs []client.Object, kind, name string) client.Object {
	for _, obj := range objs {
		if obj.GetObjectKind().GroupVersionKind().Kind == kind && obj.GetName() == name {
			return obj
		}
	}
	return nil
}

func volumeByName(volumes []corev1.Volume, name string) *corev1.Volume {
	for i := range volumes {
		if volumes[i].Name == name {
			return &volumes[i]
		}
	}
	return nil
}

func vaultKMSDaemonSetWithStatus(status appsv1.DaemonSetStatus) *appsv1.DaemonSet {
	return &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      vaultKMSDaemonSetName,
			Namespace: "dpf-operator-system",
		},
		Status: status,
	}
}

func tokenConfig() *operatorv1.VaultKMSConfiguration {
	return &operatorv1.VaultKMSConfiguration{
		Address: "https://vault.example:8200",
		Transit: operatorv1.VaultKMSTransit{KeyName: "k8s-etcd", Mount: ptr.To("transit")},
		Auth: operatorv1.VaultKMSAuth{
			Method: operatorv1.VaultKMSAuthMethodToken,
			Token:  &operatorv1.VaultKMSTokenAuth{TokenSecretRef: operatorv1.SecretKeyRef{Name: "dpf-token", Key: "token"}},
		},
	}
}
