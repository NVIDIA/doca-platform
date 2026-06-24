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

package identity

import (
	"fmt"
	"strings"

	k8svalidation "k8s.io/apimachinery/pkg/util/validation"
)

// ValidateTrustDomain checks that td is a non-empty DNS-1123 subdomain suitable
// for use as a SPIFFE ID authority segment.
func ValidateTrustDomain(td string) (string, error) {
	trimmed := strings.TrimSpace(td)
	if trimmed == "" {
		return "", fmt.Errorf("trust domain is empty after trimming")
	}
	if errs := k8svalidation.IsDNS1123Subdomain(trimmed); len(errs) > 0 {
		return "", fmt.Errorf("trust domain %q is not a valid DNS-1123 subdomain: %s", trimmed, strings.Join(errs, "; "))
	}
	return trimmed, nil
}
