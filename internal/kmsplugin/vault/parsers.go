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

package vault

import (
	"encoding/json"
	"fmt"
)

// requirePositiveInt64 parses a required field from Vault response data as a
// positive int64, returning an error if it is missing, not a number, or not
// positive. Vault's Go SDK decodes response numbers as json.Number rather
// than float64, so this is the shared way to parse any such field. subject
// names the enclosing data and is used only in the "missing" error message.
func requirePositiveInt64(data map[string]interface{}, field, subject string) (int64, error) {
	value, ok := data[field]
	if !ok || value == nil {
		return 0, fmt.Errorf("%s is missing %s", subject, field)
	}
	v, ok := value.(json.Number)
	if !ok {
		return 0, fmt.Errorf("%s has unexpected type %T", field, value)
	}
	n, err := v.Int64()
	if err != nil {
		return 0, fmt.Errorf("parsing %s %q: %w", field, v.String(), err)
	}
	if n <= 0 {
		return 0, fmt.Errorf("%s %d is not positive", field, n)
	}
	return n, nil
}
