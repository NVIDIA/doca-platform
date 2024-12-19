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
	"context"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	kerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/clientcmd/api"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// CleanupAndWait deletes an object and waits for it to be removed before exiting.
func CleanupAndWait(ctx context.Context, c client.Client, objs ...client.Object) error {
	logger := log.FromContext(ctx)
	// Ensure each object is deleted by checking that each object returns an IsNotFound error in the api server.
	errs := []error{}
	for _, o := range objs {
		logger.Info("Deleting resource", "kind", o.GetObjectKind().GroupVersionKind().String(), "namespace", o.GetNamespace(), "name", o.GetName())
		if err := c.Delete(ctx, o); err != nil && !apierrors.IsNotFound(err) {
			return err
		}
		key := client.ObjectKeyFromObject(o)
		err := wait.ExponentialBackoff(
			wait.Backoff{
				Duration: 100 * time.Millisecond,
				Factor:   1.5,
				Steps:    15,
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
			errs = append(errs, fmt.Errorf("key %s, %s is not being deleted: %s", o.GetObjectKind().GroupVersionKind().String(), key, err))
		}
	}
	return kerrors.NewAggregate(errs)
}

// CleanupWithLabelAndWait creates a list ob objects with certain labels and deletes them. After deletion, it waits to be removed.
func CleanupWithLabelAndWait(ctx context.Context, c client.Client, labelSelector labels.Selector, resources ...client.ObjectList) error {
	var deleteObjs []client.Object

	for _, list := range resources {
		if err := c.List(context.Background(), list, &client.ListOptions{LabelSelector: labelSelector}); err != nil {
			return err
		}

		items, err := meta.ExtractList(list)
		if err != nil {
			return err
		}

		for _, item := range items {
			obj, ok := item.(client.Object)
			if !ok {
				return err
			}
			deleteObjs = append(deleteObjs, obj)
		}
	}

	return CleanupAndWait(ctx, c, deleteObjs...)
}

// CreateResourceIfNotExist creates a resource if it doesn't exist
func CreateResourceIfNotExist(ctx context.Context, c client.Client, obj client.Object) error {
	err := c.Create(ctx, obj)
	if err != nil && !apierrors.IsAlreadyExists(err) {
		return err
	}
	return nil
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
			Name:      fmt.Sprintf("%v-admin-kubeconfig", cluster.Name),
			Namespace: cluster.Namespace,
			Labels: map[string]string{
				"kamaji.clastix.io/component": "admin-kubeconfig",
				"kamaji.clastix.io/project":   "kamaji",
			},
		},
		Data: map[string][]byte{
			"admin.conf": confData,
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
	err = c.Patch(ctx, obj, client.RawPatch(types.MergePatchType, []byte(fmt.Sprintf("{\"metadata\":{\"annotations\":{%q: %q}}}", "annotatedAt", time.Now().Format(time.RFC3339)))))
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
