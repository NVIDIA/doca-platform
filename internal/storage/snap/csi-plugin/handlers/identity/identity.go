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

package identity

import (
	"context"

	"github.com/nvidia/doca-platform/internal/storage/snap/csi-plugin/common"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

type Handler interface {
	csi.IdentityServer
}

type identity struct {
	csi.UnimplementedIdentityServer
}

// New returns new instance of default implementation of the identity handler
func New() (Handler, error) {
	return &identity{}, nil
}

// GetPluginInfo is a handler for GetPluginInfo request
func (h *identity) GetPluginInfo(
	ctx context.Context,
	req *csi.GetPluginInfoRequest) (
	*csi.GetPluginInfoResponse, error) {
	return &csi.GetPluginInfoResponse{
		Name:          common.PluginName,
		VendorVersion: common.VendorVersion,
	}, nil
}

// GetPluginCapabilities is a handler for GetPluginCapabilities request
// return ControllerService capability
func (h *identity) GetPluginCapabilities(
	ctx context.Context,
	req *csi.GetPluginCapabilitiesRequest) (
	*csi.GetPluginCapabilitiesResponse, error) {
	return &csi.GetPluginCapabilitiesResponse{
		Capabilities: []*csi.PluginCapability{{
			Type: &csi.PluginCapability_Service_{
				Service: &csi.PluginCapability_Service{Type: csi.PluginCapability_Service_CONTROLLER_SERVICE}}}},
	}, nil
}

// Probe is a handler for Probe request
// currently always return true
func (h *identity) Probe(
	ctx context.Context,
	req *csi.ProbeRequest) (
	*csi.ProbeResponse, error) {
	return &csi.ProbeResponse{Ready: wrapperspb.Bool(true)}, nil
}
