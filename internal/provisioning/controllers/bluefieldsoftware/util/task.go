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
	ComponentTypeAstraNicFw       ComponentType = "astranicfw"
)

// SpecURLForComponent returns the spec value (typically a URL) for componentType.
// Fields that live under TmpFwComponents return "" when TmpFwComponents is nil.
func SpecURLForComponent(bfs *provisioningv1.BlueFieldSoftware, componentType ComponentType) string {
	switch componentType {
	case ComponentTypeFwBundle:
		return ptr.Deref(bfs.Spec.PldmFwBundle, "")
	case ComponentTypePlatformFwBundle:
		return ptr.Deref(bfs.Spec.PlatformPldmFwBundle, "")
	case ComponentTypeOSISO:
		return bfs.Spec.OsIso
	}
	return ""
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
