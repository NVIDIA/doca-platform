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

package utils

import (
	"bytes"
	"context"
	cryptorand "crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"math/rand"
	"net"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/test/e2e/cleanup"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	kerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/clientcmd/api"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// GenerateDPUObj sets the name, namespace, and labels on a given client.Object.
// Default is It scope cleanup labels; if customLabels provided, they replace the default.
func GenerateDPUObj[T client.Object](name, ns string, obj T, customLabels ...map[string]string) T {
	obj.SetName(name)
	obj.SetNamespace(ns)
	// Default to It scope labels; replace with customLabels if provided
	labels := cleanup.CleanupLabels.It
	if len(customLabels) > 0 {
		labels = customLabels[0]
	}
	obj.SetLabels(labels)
	return obj
}

// CleanupAndWait deletes an object and waits for it to be removed before exiting.
func CleanupAndWait(ctx context.Context, c client.Client, objs ...client.Object) error {
	return cleanupAndWait(ctx, c, false, objs...)
}

// CleanupWithLabelAndWait collects all resources matching the label selector and delegates to CleanupAndWait for parallel deletion
func CleanupWithLabelAndWait(ctx context.Context, c client.Client, labelSelector labels.Selector, resources ...client.ObjectList) error {
	var deleteObjs []client.Object

	logger := log.FromContext(ctx)

	// Collect all matching objects across the provided resource types
	for _, list := range resources {
		if err := c.List(ctx, list, &client.ListOptions{LabelSelector: labelSelector}); err != nil {
			if meta.IsNoMatchError(err) {
				logger.Info("Resource not registered in API server, skipping", "resource", list.GetObjectKind().GroupVersionKind().String())
				continue
			}
			return err
		}

		// Extract and convert runtime.Object to client.Object for CleanupAndWait
		if err := meta.EachListItem(list,
			func(item runtime.Object) error {
				deleteObjs = append(deleteObjs, item.(client.Object))
				return nil
			},
		); err != nil {
			return err
		}
	}

	return CleanupAndWait(ctx, c, deleteObjs...)
}

// CleanupWithFinalizerRemovalAndWait removes finalizers from resources then deletes them. After deletion, it waits for them to be removed
// Note: this should be used when such a "force" deletion is ok and leads to no side-effects. e.g. no controllers need to take action as a result
// of a resource removal.
func CleanupWithFinalizerRemovalAndWait(ctx context.Context, c client.Client, resources ...client.Object) error {
	return cleanupAndWait(ctx, c, true, resources...)
}

// CreateResourceIfNotExist creates a resource if it doesn't exist
func CreateResourceIfNotExist(ctx context.Context, c client.Client, obj client.Object) error {
	err := c.Create(ctx, obj)
	if err != nil && !apierrors.IsAlreadyExists(err) {
		return err
	}
	return nil
}

// cleanupAndWait is a helper function to delete and wait for resources to be deleted.
// It fires all delete requests first, then waits for all resources to be gone.
// This allows finalizers to resolve naturally regardless of deletion order.
func cleanupAndWait(ctx context.Context, c client.Client, removeFinalizers bool, objs ...client.Object) error {
	logger := log.FromContext(ctx)

	// Step 1: Fire all delete requests without waiting. This ensures all resources are marked for
	// deletion before we start polling, so finalizers that depend on other resources being deleted
	// can resolve naturally in the background.
	for _, o := range objs {
		logger.Info("Deleting resource", "kind", o.GetObjectKind().GroupVersionKind().String(), "namespace", o.GetNamespace(), "name", o.GetName())
		if err := c.Delete(ctx, o); err != nil && !apierrors.IsNotFound(err) {
			return err
		}
		if removeFinalizers {
			// Use RawPatch to reliably clear finalizers during cleanup.
			// MergeFrom() doesnt work when obj does not have its GVK set, which is the case for non-cache clients
			patch := []byte(`{"metadata":{"finalizers":[]}}`)
			if err := c.Patch(ctx, o, client.RawPatch(types.MergePatchType, patch)); err != nil && !apierrors.IsNotFound(err) {
				return err
			}
		}
	}

	// Step 2: Poll until all resources are gone. By this point every resource has a deletionTimestamp,
	// so finalizer controllers can reconcile them in any order.
	errs := []error{}
	for _, o := range objs {
		logger.Info("Waiting for resource to be deleted", "kind", o.GetObjectKind().GroupVersionKind().String(), "namespace", o.GetNamespace(), "name", o.GetName())
		key := client.ObjectKeyFromObject(o)
		err := wait.ExponentialBackoff(
			wait.Backoff{
				Duration: 100 * time.Millisecond,
				Factor:   1.5,
				Steps:    17,
				Jitter:   0.4,
			},
			func() (done bool, err error) {
				if err := c.Get(ctx, key, o); err != nil {
					if apierrors.IsNotFound(err) {
						return true, nil
					}
					logger.Error(err, "Failed delete resource", "namespace", key.Namespace, "name", key.Name, "kind", o.GetObjectKind().GroupVersionKind().String())
					return false, nil
				}
				return false, nil
			})
		if err != nil {
			_ = c.Get(ctx, key, o)
			errs = append(errs, fmt.Errorf("key %s, %s is not being deleted: %s", o.GetObjectKind().GroupVersionKind().String(), key, err))
		}
	}
	return kerrors.NewAggregate(errs)
}

// GetFakeKamajiClusterSecretFromEnvtest creates a kamaji secret using the envtest information to simulate that we have
// a kamaji cluster. In reality, this is the same envtest Kubernetes API.
func GetFakeKamajiClusterSecretFromEnvtest(cluster provisioningv1.DPUCluster, cfg *rest.Config) (*corev1.Secret, error) {
	config := &api.Config{
		Clusters: map[string]*api.Cluster{
			cluster.Name: {
				Server:                   cfg.Host,
				CertificateAuthorityData: cfg.CAData,
			},
		},
		AuthInfos: map[string]*api.AuthInfo{
			"user": {
				ClientKeyData:         cfg.KeyData,
				ClientCertificateData: cfg.CertData,
			},
		},
		Contexts: map[string]*api.Context{
			"default": {
				Cluster:  cluster.Name,
				AuthInfo: "user",
			},
		},
		CurrentContext: "default",
	}

	confData, err := clientcmd.Write(*config)
	if err != nil {
		return nil, err
	}
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-admin-kubeconfig", cluster.Name),
			Namespace: cluster.Namespace,
			Labels: map[string]string{
				"kamaji.clastix.io/component": "admin-kubeconfig",
				"kamaji.clastix.io/project":   "kamaji",
			},
		},
		Data: map[string][]byte{
			"super-admin.conf": confData,
		},
	}, nil
}

func GetTestLabels() map[string]string {
	return map[string]string{"some": "label", "color": "blue", "lab": "santa-clara"}
}

// ForceObjectReconcileWithAnnotation adds patches the passed object with an annotation to force it to be reconciled.
func ForceObjectReconcileWithAnnotation(ctx context.Context, c client.Client, obj client.Object) error {
	err := c.Get(ctx, client.ObjectKeyFromObject(obj), obj)
	if err != nil {
		return err
	}

	// Use nanosecond precision and a random number to ensure uniqueness
	uniqueValue := fmt.Sprintf("%d-%d", time.Now().UnixNano(), rand.Int63())
	err = c.Patch(ctx, obj, client.RawPatch(types.MergePatchType, []byte(fmt.Sprintf("{\"metadata\":{\"annotations\":{%q: %q}}}", "annotatedAt", uniqueValue))))
	if err != nil {
		return err
	}
	return nil
}

func GetTestDPUCluster(ns, name string) provisioningv1.DPUCluster {
	return provisioningv1.DPUCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ns,
		},
		Spec: provisioningv1.DPUClusterSpec{
			Type:       "kamaji",
			Kubeconfig: fmt.Sprintf("%v-admin-kubeconfig", name),
		},
	}
}

// ResolveBFBImageURL resolves a BFB image URL to a real file path.
// On our test environment, we can access NFS files via HTTP.
// To be able to test the latest BFB image, we need to resolve the URL to a real file path.
func ResolveBFBImageURL(bfbURL string) (string, error) {
	// Parse the URL to get the path.
	u, err := url.Parse(bfbURL)
	if err != nil {
		panic(err)
	}

	// Return early if the URL does not contain a wildcard or does not start with a certain path.
	if !strings.Contains(u.Path, "*") || !strings.HasPrefix(u.Path, "/auto/sw_mc_soc_release/doca_dpu/") {
		return bfbURL, nil
	}

	// Get the real file path from the path in the URI.
	file, err := filepath.Glob(u.Path)
	if err != nil {
		return "", err
	}
	if len(file) == 0 {
		return "", fmt.Errorf("no file found for %s", u.Path)
	}
	if len(file) > 1 {
		return "", fmt.Errorf("multiple files found for %s", u.Path)
	}

	return fmt.Sprintf("%s://%s%s", u.Scheme, u.Host, file[0]), nil
}

// ResolveHBNImageURL processes the HBN image URL and returns it if valid.
func ResolveHBNImageURL(hbnURL string) (string, error) {
	// If URL doesn't contain a colon, return as-is
	if !strings.Contains(hbnURL, ":") {
		return hbnURL, nil
	}

	// Basic validation - should have repository:tag format
	parts := strings.SplitN(hbnURL, ":", 2)
	if len(parts) != 2 {
		return hbnURL, fmt.Errorf("invalid format, expected repository:tag")
	}

	return hbnURL, nil
}

// CreateMTLSCerts creates mTLS certificates for testing purposes.
// Returns CA certificate, client certificate, client key, server certificate, and server key as PEM-encoded bytes.
func CreateMTLSCerts(dmsIP string) (caCrtBytes, clientCrtBytes, clientKeyBytes, srvCrtBytes, srvKeyBytes []byte) {
	// CA Private Key
	caPrivKey, err := rsa.GenerateKey(cryptorand.Reader, 2048)
	if err != nil {
		panic(fmt.Sprintf("failed to generate CA private key: %v", err))
	}

	// CA Certificate Template
	caTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(2024),
		Subject: pkix.Name{
			Organization: []string{"Test CA Org"},
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().AddDate(1, 0, 0), // Valid for 1 year
		IsCA:                  true,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth, x509.ExtKeyUsageServerAuth},
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		BasicConstraintsValid: true,
	}

	// Create CA Certificate
	caCertBytes, err := x509.CreateCertificate(cryptorand.Reader, caTemplate, caTemplate, &caPrivKey.PublicKey, caPrivKey)
	if err != nil {
		panic(fmt.Sprintf("failed to create CA certificate: %v", err))
	}
	caCert, err := x509.ParseCertificate(caCertBytes)
	if err != nil {
		panic(fmt.Sprintf("failed to parse CA certificate: %v", err))
	}

	// Server Private Key
	serverPrivKey, err := rsa.GenerateKey(cryptorand.Reader, 2048)
	if err != nil {
		panic(fmt.Sprintf("failed to generate server private key: %v", err))
	}

	// Server Certificate Template
	serverTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(2025),
		Subject: pkix.Name{
			CommonName: dmsIP,
		},
		IPAddresses: []net.IP{net.ParseIP(dmsIP)},
		NotBefore:   time.Now(),
		NotAfter:    time.Now().AddDate(1, 0, 0),
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		KeyUsage:    x509.KeyUsageDigitalSignature,
	}

	// Create Server Certificate
	serverCertBytes, err := x509.CreateCertificate(cryptorand.Reader, serverTemplate, caCert, &serverPrivKey.PublicKey, caPrivKey)
	if err != nil {
		panic(fmt.Sprintf("failed to create server certificate: %v", err))
	}

	// Client Private Key
	clientPrivKey, err := rsa.GenerateKey(cryptorand.Reader, 2048)
	if err != nil {
		panic(fmt.Sprintf("failed to generate client private key: %v", err))
	}

	// Client Certificate Template
	clientTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(2026),
		Subject: pkix.Name{
			CommonName: "client",
		},
		NotBefore:   time.Now(),
		NotAfter:    time.Now().AddDate(1, 0, 0),
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
		KeyUsage:    x509.KeyUsageDigitalSignature,
	}

	// Create Client Certificate
	clientCertBytes, err := x509.CreateCertificate(cryptorand.Reader, clientTemplate, caCert, &clientPrivKey.PublicKey, caPrivKey)
	if err != nil {
		panic(fmt.Sprintf("failed to create client certificate: %v", err))
	}

	// PEM Encode
	caCertPEM := new(bytes.Buffer)
	if err := pem.Encode(caCertPEM, &pem.Block{Type: "CERTIFICATE", Bytes: caCertBytes}); err != nil {
		panic(fmt.Sprintf("failed to encode CA certificate: %v", err))
	}

	serverCertPEM := new(bytes.Buffer)
	if err := pem.Encode(serverCertPEM, &pem.Block{Type: "CERTIFICATE", Bytes: serverCertBytes}); err != nil {
		panic(fmt.Sprintf("failed to encode server certificate: %v", err))
	}
	serverPrivKeyPEM := new(bytes.Buffer)
	if err := pem.Encode(serverPrivKeyPEM, &pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(serverPrivKey)}); err != nil {
		panic(fmt.Sprintf("failed to encode server private key: %v", err))
	}

	clientCertPEM := new(bytes.Buffer)
	if err := pem.Encode(clientCertPEM, &pem.Block{Type: "CERTIFICATE", Bytes: clientCertBytes}); err != nil {
		panic(fmt.Sprintf("failed to encode client certificate: %v", err))
	}
	clientPrivKeyPEM := new(bytes.Buffer)
	if err := pem.Encode(clientPrivKeyPEM, &pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(clientPrivKey)}); err != nil {
		panic(fmt.Sprintf("failed to encode client private key: %v", err))
	}

	return caCertPEM.Bytes(), clientCertPEM.Bytes(), clientPrivKeyPEM.Bytes(), serverCertPEM.Bytes(), serverPrivKeyPEM.Bytes()
}

// PatchStatus re-fetches obj, calls mutate() to modify status fields, then patches the status
// subresource using a merge patch. Retries on conflict to avoid "object has been modified" errors
// from concurrent controller reconciliations.
func PatchStatus(ctx context.Context, c client.Client, obj client.Object, mutate func()) {
	GinkgoHelper()
	Eventually(func(g Gomega) {
		g.Expect(c.Get(ctx, client.ObjectKeyFromObject(obj), obj)).To(Succeed())
		mutate()
		g.Expect(c.Status().Patch(ctx, obj, client.Merge)).To(Succeed())
	}).WithTimeout(10 * time.Second).WithPolling(100 * time.Millisecond).Should(Succeed())
}
