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

package controller

import (
	"context"
	"fmt"

	"github.com/nvidia/doca-platform/internal/storage/snap/csi-plugin/config"
	"github.com/nvidia/doca-platform/internal/storage/snap/csi-plugin/controller/clusterhelper"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type Handler interface {
	csi.ControllerServer
}

// New returns new instance of default implementation of the controller handler
func New(cfg config.Controller, clusterHelper clusterhelper.Helper) Handler {
	return &controller{
		cfg:           cfg,
		clusterhelper: clusterHelper,
	}
}

type controller struct {
	csi.UnimplementedControllerServer
	cfg           config.Controller
	clusterhelper clusterhelper.Helper
}

// getClients return clients for host and dpu clsuters
func (h *controller) getClients(ctx context.Context) (client.Client, client.Client, error) {
	hostClient, err := h.clusterhelper.GetHostClusterClient(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get client for host cluster: %v", err)
	}
	dpuClient, err := h.clusterhelper.GetDPUClusterClient(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get client for dpu cluster: %v", err)
	}
	return hostClient, dpuClient, nil
}
