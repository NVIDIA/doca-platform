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

package spire

import "time"

const (
	// EntryX509SVIDTTL is the X509-SVID lifetime stamped on every ClusterStaticEntry DPF
	// creates. Shared by the DPU Agent and DPUService entries so a DPU's identities rotate on
	// one cadence; an implementation detail of those entries, not a user-facing API field.
	EntryX509SVIDTTL = time.Hour
	// EntryJWTSVIDTTL is the JWT-SVID lifetime, kept short because the DPU Agent presents it
	// to the kube-apiserver on every call and a leaked token stays usable until it expires.
	EntryJWTSVIDTTL = 120 * time.Second
)
