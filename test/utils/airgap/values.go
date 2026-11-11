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

package airgap

import (
	"encoding/json"
	"strings"

	"k8s.io/apimachinery/pkg/runtime"
)

// RewriteHelmValues points the image repositories of DPUService helm values at the mirror: every
// "repository" value of a map under an "image" key, the common chart layout
// (image: {repository: ..., tag: ...}). A repository already in the mirror is left alone. Nil
// values stay nil.
func (m *Mirror) RewriteHelmValues(values *runtime.RawExtension) (*runtime.RawExtension, error) {
	if values == nil || len(values.Raw) == 0 {
		return values, nil
	}
	var v map[string]any
	if err := json.Unmarshal(values.Raw, &v); err != nil {
		return nil, err
	}
	if err := m.rewriteImages(v); err != nil {
		return nil, err
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return &runtime.RawExtension{Raw: raw}, nil
}

// rewriteImages walks the maps and lists of v and rewrites every map under an "image" key with
// rewriteRepository.
func (m *Mirror) rewriteImages(v any) error {
	switch v := v.(type) {
	case map[string]any:
		for k, child := range v {
			if img, ok := child.(map[string]any); ok && k == "image" {
				if err := m.rewriteRepository(img); err != nil {
					return err
				}
			}
			if err := m.rewriteImages(child); err != nil {
				return err
			}
		}
	case []any:
		for _, child := range v {
			if err := m.rewriteImages(child); err != nil {
				return err
			}
		}
	}
	return nil
}

// rewriteRepository points the "repository" value of image at the mirror, see RewriteHelmValues.
func (m *Mirror) rewriteRepository(image map[string]any) error {
	repo, ok := image["repository"].(string)
	if !ok || repo == "" || m.Contains(repo) {
		return nil
	}
	// A repository with a tag or digest is not a repository; leave it to the coverage check.
	if strings.Contains(repo[strings.LastIndex(repo, "/")+1:], ":") || strings.Contains(repo, "@") {
		return nil
	}
	mirrored, err := m.Repository(repo)
	if err != nil {
		return err
	}
	image["repository"] = mirrored
	return nil
}
