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

package v1alpha1

// ExcludeRange contains range of IP addresses to exclude from allocation
// startIP and endIP are part of the ExcludeRange
type ExcludeRange struct {
	StartIP string `json:"startIP"`
	EndIP   string `json:"endIP"`
}

// Route contains static route parameters
type Route struct {
	// The destination of the route, in CIDR notation
	Dst string `json:"dst"`
}
