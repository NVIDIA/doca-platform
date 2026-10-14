/*
Copyright The Helm Authors.

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
	"fmt"
	"log"
)

// Copied from https://github.com/helm/helm/blob/v3.16.1/pkg/chartutil/coalesce.go#L259

// MergeMaps deeply merges 2 maps. If there is conflict, the value from dst will be used.
func MergeMaps(dst, src map[string]interface{}) map[string]interface{} {
	return coalesceTablesFullKey(log.Printf, dst, src, "", true)
}

type printFn func(format string, v ...interface{})

// coalesceTablesFullKey merges a source map into a destination map.
//
// dest is considered authoritative.
func coalesceTablesFullKey(printf printFn, dst, src map[string]interface{}, prefix string, merge bool) map[string]interface{} {
	// When --reuse-values is set but there are no modifications yet, return new values
	if src == nil {
		return dst
	}
	if dst == nil {
		return src
	}
	// Because dest has higher precedence than src, dest values override src
	// values.
	for key, val := range src {
		fullkey := concatPrefix(prefix, key)
		if dv, ok := dst[key]; ok && !merge && dv == nil {
			delete(dst, key)
		} else if !ok {
			dst[key] = val
		} else if istable(val) {
			if istable(dv) {
				coalesceTablesFullKey(printf, dv.(map[string]interface{}), val.(map[string]interface{}), fullkey, merge)
			} else {
				printf("warning: cannot overwrite table with non table for %s (%v)", fullkey, val)
			}
		} else if istable(dv) && val != nil {
			printf("warning: destination for %s is a table. Ignoring non-table value (%v)", fullkey, val)
		}
	}
	return dst
}

// istable is a special-purpose function to see if the present thing matches the definition of a YAML table.
func istable(v interface{}) bool {
	_, ok := v.(map[string]interface{})
	return ok
}

func concatPrefix(a, b string) string {
	if a == "" {
		return b
	}
	return fmt.Sprintf("%s.%s", a, b)
}
