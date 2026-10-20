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

// Package bom implements the air-gap bill of materials (BOM) of DPF: the container images and
// helm charts a DPF installation pulls, so they can be mirrored into a registry reachable from an
// air-gapped environment.
//
// A BOM (schema v1alpha1) has three sections:
//
//   - images: pulled by some or all installations; mirror them all. First-party (DPF-built)
//     images are listed by name only, their registry and tag are those the user installs DPF
//     from. Third-party images are full references, as the repository and the rendered charts
//     have them (tag or digest).
//   - services: optional DPUService charts, each with the images it renders. Users need only the
//     services they deploy.
//   - charts: every helm chart to copy into the mirror, the charts of the services included.
//     First-party charts (the DPF release and VPC charts) are listed by name only, third-party
//     ones with repoURL, name and version. Remote dependencies of first-party charts are not
//     listed, they are packaged into the chart. Charts carry no scope: only the host cluster
//     pulls charts (helm and Argo CD); the DPU cluster pulls images only.
//
// Every image carries a scope, the cluster(s) that pull it: dpu and/or host. schemaVersion is the
// version of this format. The image with sandbox: true is the pause image that containerd on the
// DPU starts in every pod, as its sandbox container.
package bom
