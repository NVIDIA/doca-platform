/*
Copyright 2025 NVIDIA

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

package node

import (
	"path/filepath"

	"github.com/nvidia/doca-platform/internal/storage/snap/csi-plugin/config"
	utilsMount "github.com/nvidia/doca-platform/internal/storage/snap/csi-plugin/utils/mount"
	utilsNvme "github.com/nvidia/doca-platform/internal/storage/snap/csi-plugin/utils/nvme"
	utilsPci "github.com/nvidia/doca-platform/internal/storage/snap/csi-plugin/utils/pci"

	"github.com/container-storage-interface/spec/lib/go/csi"
)

type Handler interface {
	csi.NodeServer
}

// New returns new instance of default implementation of the node handler
func New(cfg config.Node, runtimeCfg *config.NodeRuntime, mount utilsMount.Utils, nvme utilsNvme.Utils, pci utilsPci.Utils) Handler {
	return &node{
		cfg:        cfg,
		runtimeCfg: runtimeCfg,
		mount:      mount,
		nvme:       nvme,
		pci:        pci,
	}
}

type node struct {
	csi.UnimplementedNodeServer
	cfg        config.Node
	runtimeCfg *config.NodeRuntime
	mount      utilsMount.Utils
	nvme       utilsNvme.Utils
	pci        utilsPci.Utils
}

func (h *node) getStagingPath(stagingPath, volumeID string) string {
	return filepath.Join(stagingPath, volumeID)
}
