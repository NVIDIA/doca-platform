/*
COPYRIGHT 2024 NVIDIA

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

// Code generated by "libovsdb.modelgen"
// DO NOT EDIT.

package ovsmodel

const OpenvSwitchTable = "Open_vSwitch"

// OpenvSwitch defines an object in Open_vSwitch table
type OpenvSwitch struct {
	UUID            string            `ovsdb:"_uuid"`
	Bridges         []string          `ovsdb:"bridges"`
	CurCfg          int               `ovsdb:"cur_cfg"`
	DatapathTypes   []string          `ovsdb:"datapath_types"`
	Datapaths       map[string]string `ovsdb:"datapaths"`
	DbVersion       *string           `ovsdb:"db_version"`
	DocaInitialized bool              `ovsdb:"doca_initialized"`
	DocaVersion     *string           `ovsdb:"doca_version"`
	DpdkInitialized bool              `ovsdb:"dpdk_initialized"`
	DpdkVersion     *string           `ovsdb:"dpdk_version"`
	ExternalIDs     map[string]string `ovsdb:"external_ids"`
	IfaceTypes      []string          `ovsdb:"iface_types"`
	ManagerOptions  []string          `ovsdb:"manager_options"`
	NextCfg         int               `ovsdb:"next_cfg"`
	OtherConfig     map[string]string `ovsdb:"other_config"`
	OVSVersion      *string           `ovsdb:"ovs_version"`
	SSL             *string           `ovsdb:"ssl"`
	Statistics      map[string]string `ovsdb:"statistics"`
	SystemType      *string           `ovsdb:"system_type"`
	SystemVersion   *string           `ovsdb:"system_version"`
}
