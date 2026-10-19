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

package bom

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// Engine renders charts and turns the images found into BOM entries. Renderings are memoized for
// the lifetime of the engine (one generation run): a chart is rendered once per set of values.
type Engine struct {
	Renderer Renderer
	// FirstPartyRegistries are registry prefixes (host or host/path) of DPF-built images. An image
	// below one of them becomes a first-party BOM entry, listed by name (its last path element).
	FirstPartyRegistries []string
	// Logf receives progress and warnings. Optional.
	Logf func(format string, args ...any)

	rendered map[string][]FoundImage
}

// RenderImages renders a chart and returns the images found in it, see ImagesFromManifests.
func (e *Engine) RenderImages(ctx context.Context, req RenderRequest) ([]FoundImage, error) {
	key, err := renderKey(req)
	if err != nil {
		return nil, err
	}
	if found, ok := e.rendered[key]; ok {
		return found, nil
	}
	if e.Renderer == nil {
		return nil, fmt.Errorf("chart %s: no renderer is configured", req.Chart)
	}
	e.logf("rendering chart %s", req.Chart)
	data, err := e.Renderer.Render(ctx, req)
	if err != nil {
		return nil, err
	}
	found, err := ImagesFromManifests(data)
	if err != nil {
		return nil, fmt.Errorf("chart %s: %w", req.Chart, err)
	}
	if e.rendered == nil {
		e.rendered = map[string][]FoundImage{}
	}
	e.rendered[key] = found
	return found, nil
}

func (e *Engine) logf(format string, args ...any) {
	if e.Logf != nil {
		e.Logf(format, args...)
	}
}

func renderKey(req RenderRequest) (string, error) {
	// encoding/json sorts map keys, so equal values give equal keys.
	data, err := json.Marshal(req)
	if err != nil {
		return "", fmt.Errorf("chart %s: failed to encode the values: %w", req.Chart, err)
	}
	return string(data), nil
}

// FirstPartyName returns the BOM name of a first-party image, see FirstPartyRegistries.
func (e *Engine) FirstPartyName(ref ImageRef) (string, bool) {
	repo := ref.Repository()
	for _, prefix := range e.FirstPartyRegistries {
		prefix = strings.TrimSuffix(strings.TrimPrefix(prefix, "oci://"), "/")
		if prefix != "" && strings.HasPrefix(repo, prefix+"/") {
			return repo[strings.LastIndex(repo, "/")+1:], true
		}
	}
	return "", false
}

// Image returns the BOM entry of an image pulled by scopes: a name for a first-party image, the
// normalised reference otherwise.
func (e *Engine) Image(ref ImageRef, scopes ...Scope) Image {
	if name, ok := e.FirstPartyName(ref); ok {
		return Image{Name: name, Scope: scopes}
	}
	return Image{Ref: ref.String(), Scope: scopes}
}

// Images converts found images into BOM entries pulled by scope, see Image. A strict value that
// is not a valid reference fails. A non-strict value is dropped, with a log line, when it does not
// parse or is a bare word without registry, path, tag or digest (e.g. "true").
func (e *Engine) Images(found []FoundImage, scopes ...Scope) ([]Image, error) {
	var out []Image
	var errs []error
	for _, f := range found {
		ref, err := ParseImageRef(f.Raw)
		if err != nil && f.Strict {
			errs = append(errs, fmt.Errorf("%s: %w", f, err))
			continue
		}
		if err != nil || (!f.Strict && !strings.ContainsAny(f.Raw, "/:@")) {
			e.logf("ignoring %s: not an image reference", f)
			continue
		}
		out = append(out, e.Image(ref, scopes...))
	}
	return normalizeImages(out), errors.Join(errs...)
}
