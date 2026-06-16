//go:build linux

// Modifications copyright (C) 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
//
// Copyright 2018-2019 Red Hat, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"github.com/nvidia/doca-platform/internal/cni/dpf-ovs-cni/plugin"
	"github.com/nvidia/doca-platform/internal/cni/dpf-ovs-cni/sriov"

	"github.com/containernetworking/cni/pkg/skel"
	"github.com/containernetworking/cni/pkg/version"
	"github.com/containernetworking/plugins/pkg/utils/buildversion"
)

func main() {
	// buildversion.BuildVersion is stamped via -ldflags by `binary-sfc-cni`
	// so the CNI About output reports the DPF release version.
	d := plugin.NewDpfCNI(sriov.DefaultOps{})
	skel.PluginMainFuncs(skel.CNIFuncs{
		Add:   d.CmdAdd,
		Check: d.CmdCheck,
		Del:   d.CmdDel,
	}, version.All, buildversion.BuildString("OVS bridge"))
}
