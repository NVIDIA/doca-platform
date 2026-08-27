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

package util

import (
	"fmt"
	"net/url"
	"path"
	"strings"
	"sync"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"

	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
)

var DownloadingTaskMap sync.Map

type ComponentDownloadTask struct {
	TaskName      string
	URL           string
	FileName      string
	ComponentName string
	UID           types.UID
}

// ComponentType represents the type of component to download
type ComponentType string

const (
	ComponentTypeFwBundle         ComponentType = "fwbundle"
	ComponentTypePlatformFwBundle ComponentType = "platformpldmfwbundle"
	ComponentTypeOSISO            ComponentType = "osiso"
	ComponentTypeNicFw            ComponentType = "nicfw"
)

// SpecURLForComponent returns the spec value (typically a URL) for single-valued
// componentTypes. The DPU PLDM bundle is keyed per-PSID (see PldmFwBundles), so it is
// not single-valued and always returns "" here.
func SpecURLForComponent(bfs *provisioningv1.BlueFieldSoftware, componentType ComponentType) string {
	switch componentType {
	case ComponentTypePlatformFwBundle:
		return ptr.Deref(bfs.Spec.PlatformPldmFwBundle, "")
	case ComponentTypeOSISO:
		return bfs.Spec.OsIso
	case ComponentTypeNicFw:
		return ptr.Deref(bfs.Spec.NicFw, "")
	}
	return ""
}

// PldmFwBundles returns the spec DPU PLDM firmware bundles keyed by PSID (nil-safe).
// Empty URLs are dropped so callers never attempt an empty download.
func PldmFwBundles(bfs *provisioningv1.BlueFieldSoftware) map[string]string {
	if bfs == nil || len(bfs.Spec.PldmFwBundle) == 0 {
		return nil
	}
	out := make(map[string]string, len(bfs.Spec.PldmFwBundle))
	for psid, url := range bfs.Spec.PldmFwBundle {
		if url != "" {
			out[psid] = url
		}
	}
	return out
}

// FilenameFromHTTPURL returns the last path segment of rawURL when it is a valid
// http(s) URL with a usable filename. Otherwise it returns "".
func FilenameFromHTTPURL(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return ""
	}
	p := u.Path
	if p == "" {
		return ""
	}
	base := path.Base(strings.TrimSuffix(p, "/"))
	if base == "." || base == "/" || base == "" {
		return ""
	}
	if strings.ContainsAny(base, "/\\") {
		return ""
	}
	if base == ".." || strings.HasPrefix(base, "..") {
		return ""
	}
	return base
}

// ComponentDownloadFilename returns the local filename for a component given the
// spec field value. For http(s) URLs it uses {namespace}-{name}-{URL basename} so
// different BlueFieldSoftware objects never collide under the shared components dir.
func ComponentDownloadFilename(bfs *provisioningv1.BlueFieldSoftware, componentType ComponentType, specValue string) string {
	if name := FilenameFromHTTPURL(specValue); name != "" {
		return fmt.Sprintf("%s-%s-%s", bfs.Namespace, bfs.Name, name)
	}
	return GenerateComponentTaskName(*bfs, componentType)
}

func GenerateComponentTaskName(bfs provisioningv1.BlueFieldSoftware, componentType ComponentType) string {
	return fmt.Sprintf("%s-%s-%s", bfs.Namespace, bfs.Name, componentType)
}

// PldmTaskName returns the download task name for the DPU PLDM bundle of a specific
// PSID, keeping per-PSID downloads of the same BlueFieldSoftware distinct. An empty
// PSID collapses to the plain bundle name (no trailing separator).
func PldmTaskName(bfs *provisioningv1.BlueFieldSoftware, psid string) string {
	name := GenerateComponentTaskName(*bfs, ComponentTypeFwBundle)
	if psid == "" {
		return name
	}
	return fmt.Sprintf("%s-%s", name, psid)
}

// PldmComponentFilename returns the on-disk filename for a per-PSID DPU PLDM bundle. The
// PSID is part of the name so bundles for different PSIDs (which may share a URL basename)
// never collide on shared bfb storage; an empty PSID collapses to {namespace}-{name}-{base}.
func PldmComponentFilename(bfs *provisioningv1.BlueFieldSoftware, psid, url string) string {
	if name := FilenameFromHTTPURL(url); name != "" {
		if psid == "" {
			return fmt.Sprintf("%s-%s-%s", bfs.Namespace, bfs.Name, name)
		}
		return fmt.Sprintf("%s-%s-%s-%s", bfs.Namespace, bfs.Name, psid, name)
	}
	return PldmTaskName(bfs, psid)
}
