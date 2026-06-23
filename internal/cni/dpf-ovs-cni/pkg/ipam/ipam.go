//go:build linux

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

package ipam

import (
	cnitypes "github.com/containernetworking/cni/pkg/types"
	current "github.com/containernetworking/cni/pkg/types/100"
	cniipam "github.com/containernetworking/plugins/pkg/ipam"
)

// API wraps IPAM plugin operations used by CNI commands.
//
//go:generate mockgen -build_constraint linux -copyright_file ../../../../../hack/boilerplate.go.txt -destination mock/ipam.go -source ipam.go
type API interface {
	ExecAdd(plugin string, netconf []byte) (cnitypes.Result, error)
	ExecDel(plugin string, netconf []byte) error
	ExecCheck(plugin string, netconf []byte) error
	ConfigureIface(ifName string, res *current.Result) error
}

// DefaultAPI uses the live CNI IPAM package.
type DefaultAPI struct{}

var _ API = DefaultAPI{}

func (DefaultAPI) ExecAdd(plugin string, netconf []byte) (cnitypes.Result, error) {
	return cniipam.ExecAdd(plugin, netconf)
}

func (DefaultAPI) ExecDel(plugin string, netconf []byte) error {
	return cniipam.ExecDel(plugin, netconf)
}

func (DefaultAPI) ExecCheck(plugin string, netconf []byte) error {
	return cniipam.ExecCheck(plugin, netconf)
}

func (DefaultAPI) ConfigureIface(ifName string, res *current.Result) error {
	return cniipam.ConfigureIface(ifName, res)
}
