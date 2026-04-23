/*
Copyright 2025 NVIDIA

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
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	dpuservicev1 "github.com/nvidia/doca-platform/api/dpuservice/v1alpha1"
	dpfutils "github.com/nvidia/doca-platform/internal/utils"

	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	corev1 "k8s.io/api/core/v1"
	kerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/apimachinery/pkg/util/yaml"
	"oras.land/oras-go/v2"
	"oras.land/oras-go/v2/registry/remote"
	"oras.land/oras-go/v2/registry/remote/auth"
	"oras.land/oras-go/v2/registry/remote/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
)

type ChartHelper interface {
	// GetAnnotationsFromChart returns the annotations for a given chart
	GetAnnotationsFromChart(ctx context.Context, c client.Client, source dpuservicev1.ApplicationSource) (map[string]string, error)
}

type chartHelper struct{}

var _ ChartHelper = &chartHelper{}

// NewChartHelper returns a ChartHelper that is able to do operations with charts. It is able to authenticate to registries
// using secrets that conform with the ArgoCD way of providing credentials for registries.
func NewChartHelper() ChartHelper {
	return &chartHelper{}
}

// GetAnnotationsFromChart returns the annotations found in the specified chart by pulling the chart locally
func (u *chartHelper) GetAnnotationsFromChart(ctx context.Context, c client.Client, source dpuservicev1.ApplicationSource) (map[string]string, error) {
	// Add the source of the helm chart to the logging context.
	ctrllog.IntoContext(ctx, ctrllog.FromContext(ctx).WithValues("source", source))

	username, password, err := getChartPullCredentials(ctx, c, source)
	if err != nil {
		return nil, err
	}
	return getAnnotationsForChart(ctx, source, username, password)
}

func getAnnotationsForChart(ctx context.Context, source dpuservicev1.ApplicationSource, username, password string) (map[string]string, error) {
	var annotations map[string]string
	var err error
	switch {
	case strings.HasPrefix(source.RepoURL, "oci://"):
		annotations, err = getAnnotationsFromOCIManifest(source, username, password)
		if err != nil {
			return nil, err
		}
	case strings.HasPrefix(source.RepoURL, "https://"):
		annotations, err = getAnnotationsFromHelmRegistry(ctx, source, username, password)
		if err != nil {
			return nil, err
		}
	default:
		// This should not happen as this should be validated in the DPUService API type.
		return nil, fmt.Errorf("unsupported chart source %s: must be oci:// or https://", source.RepoURL)
	}
	return annotations, nil
}

func getAnnotationsFromHelmRegistry(ctx context.Context, source dpuservicev1.ApplicationSource, username, password string) (_ map[string]string, reterr error) {
	url := strings.Join([]string{source.RepoURL, "index.yaml"}, "/")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed creating request: %v", err)
	}

	// If username and password are both set add a header for basic authentication.
	// If one but not the other is set authentication may fail.
	if username != "" && password != "" {
		// Add Basic Authentication header
		encodedAuth := base64.StdEncoding.EncodeToString([]byte(username + ":" + password))
		req.Header.Add("Authorization", "Basic "+encodedAuth)
	}

	// Perform the request
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %v", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			reterr = kerrors.NewAggregate([]error{reterr, err})
		}
	}()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("retrieving annotations from helm chart failed: http Response: %s", resp.Status)
	}

	// Read the response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response body failed: %v", err)
	}
	index := &HelmRegistryIndex{}
	err = yaml.Unmarshal(body, index)
	if err != nil {
		return nil, err
	}
	return annotationsFromIndex(source, index)
}

func annotationsFromIndex(source dpuservicev1.ApplicationSource, index *HelmRegistryIndex) (map[string]string, error) {
	chart, ok := index.Entries[source.Chart]
	if !ok {
		return nil, fmt.Errorf("chart %s not found in index", source.Chart)
	}
	for _, c := range chart {
		if c.Version == source.Version {
			return c.Annotations, nil
		}
	}
	return nil, fmt.Errorf("version %s for chart %s not found", source.Version, source.Chart)
}

type HelmRegistryIndex struct {
	Entries map[string][]HelmRegistryChart `json:"entries"`
}

type HelmRegistryChart struct {
	Name        string            `json:"name"`
	Version     string            `json:"version"`
	Annotations map[string]string `json:"annotations"`
}

// getChartPullCredentials returns the username and the password to pull the given chart based on Secrets that exist in the
// cluster. Empty values are returned if no secret is found for the relevant chart.
// TODO: Consider abstracting the secret we use today to be DPF specific instead of ArgoCD specific.
func getChartPullCredentials(ctx context.Context, c client.Client, source dpuservicev1.ApplicationSource) (string, string, error) {
	dpfOperatorConfig, err := dpfutils.GetDPFOperatorConfig(ctx, c)
	if err != nil {
		return "", "", fmt.Errorf("getting DPFOperatorConfig: %w", err)
	}

	secrets := &corev1.SecretList{}
	if err := c.List(ctx,
		secrets,
		client.MatchingLabels{
			"argocd.argoproj.io/secret-type": "repository",
		},
		client.InNamespace(dpfOperatorConfig.GetArgoCDNamespace())); err != nil {
		return "", "", fmt.Errorf("listing secrets: %w", err)
	}

	dpuServiceTemplateRepoURL := source.RepoURL
	dpuServiceTemplateRepoURL, _ = strings.CutPrefix(dpuServiceTemplateRepoURL, "oci://")

	var username string
	var password string
	for _, secret := range secrets.Items {
		repositoryType, ok := secret.Data["type"]
		if !ok {
			continue
		}
		if string(repositoryType) != "helm" {
			continue
		}
		repoURL, ok := secret.Data["url"]
		if !ok {
			continue
		}
		// If the repo for this secret doesn't match the one in the dpuServiceTemplate continue to check the next one.
		if string(repoURL) != dpuServiceTemplateRepoURL {
			continue
		}

		usernameParsed, ok := secret.Data["username"]
		if !ok {
			continue
		}
		username = string(usernameParsed)

		passwordParsed, ok := secret.Data["password"]
		if !ok {
			continue
		}
		password = string(passwordParsed)

		break
	}

	// Note: username and password can be empty
	return username, password, nil
}

// getAnnotationsFromOCIManifest gets the annotations from the OCI manifest
func getAnnotationsFromOCIManifest(source dpuservicev1.ApplicationSource, username string, password string) (map[string]string, error) {
	reg := source.RepoURL
	reg, _ = strings.CutPrefix(reg, "oci://")
	repo, err := remote.NewRepository(strings.Join([]string{reg, source.Chart}, "/"))
	if err != nil {
		return nil, fmt.Errorf("failed to create repository: %w", err)
	}
	repo.Client = &auth.Client{
		Client: retry.DefaultClient,
		Cache:  auth.NewCache(),
		Credential: auth.StaticCredential(repo.Reference.Registry, auth.Credential{
			Username: username,
			Password: password,
		}),
	}

	tag := source.Version
	_, out, err := oras.Fetch(context.Background(), repo, tag, oras.DefaultFetchOptions)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch manifest: %w", err)
	}
	d, err := io.ReadAll(out)
	if err != nil {
		return nil, fmt.Errorf("failed to read descriptor: %w", err)

	}
	desc := ocispec.Descriptor{}
	if err := json.Unmarshal(d, &desc); err != nil {
		return nil, fmt.Errorf("failed to unmarshal descriptor from json: %w", err)
	}

	return desc.Annotations, nil
}
