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

package client

import (
	"context"
	"fmt"
	"strings"

	"github.com/go-resty/resty/v2"
)

// FirmwareSlot is Oem.Nvidia ActiveFirmwareSlot or InactiveFirmwareSlot.
// CX9, MCU, CPLD and drive members do not have these slots.
type FirmwareSlot struct {
	SlotID                   int    `json:"SlotId"`
	Version                  string `json:"Version"`
	FirmwareComparisonNumber int64  `json:"FirmwareComparisonNumber"`
	BuildType                string `json:"BuildType"`
	FirmwareState            string `json:"FirmwareState"`
}

// FirmwareInventoryMember is one UpdateService/FirmwareInventory member.
type FirmwareInventoryMember struct {
	ID          string     `json:"Id"`
	Version     string     `json:"Version"`
	Updateable  bool       `json:"Updateable"`
	RelatedItem []odataRef `json:"RelatedItem"`
	Oem         struct {
		Nvidia struct {
			ActiveFirmwareSlot   FirmwareSlot `json:"ActiveFirmwareSlot"`
			InactiveFirmwareSlot FirmwareSlot `json:"InactiveFirmwareSlot"`
		} `json:"Nvidia"`
	} `json:"Oem"`
}

// InactiveVersion returns the staged inactive-slot version. It is empty when the member has no slot.
func (m FirmwareInventoryMember) InactiveVersion() string {
	return m.Oem.Nvidia.InactiveFirmwareSlot.Version
}

// collectionBody is a Redfish collection reduced to its member references.
type collectionBody struct {
	Members []odataRef `json:"Members"`
}

// GetFirmwareInventory reads one firmware inventory member.
func (c *Client) GetFirmwareInventory(ctx context.Context, id string) (*resty.Response, *VersionInfo, error) {
	return read[VersionInfo](ctx, c, RedfishAPIs.Path("APICheckBMCFW"), map[string]string{"BMC_FW_ID": id})
}

// GetFirmwareInventoryMember reads one member, including its inactive slot and RelatedItem.
func (c *Client) GetFirmwareInventoryMember(ctx context.Context, id string) (*resty.Response, *FirmwareInventoryMember, error) {
	return read[FirmwareInventoryMember](ctx, c, RedfishAPIs.Path("APICheckBMCFW"), map[string]string{"BMC_FW_ID": id})
}

// ListFirmwareInventory reads every firmware inventory member. The collection is expanded one
// member at a time because a CMX BMC is not required to honor $expand.
func (c *Client) ListFirmwareInventory(ctx context.Context) ([]FirmwareInventoryMember, error) {
	_, collection, err := read[collectionBody](ctx, c, RedfishAPIs.Path("APIGetFirmwareInventory"))
	if err != nil {
		return nil, err
	}
	members := make([]FirmwareInventoryMember, 0, len(collection.Members))
	for _, ref := range collection.Members {
		id := pathID(ref.ODataID)
		if id == "" {
			continue
		}
		_, member, err := c.GetFirmwareInventoryMember(ctx, id)
		if err != nil {
			return nil, fmt.Errorf("firmware inventory %s: %w", id, err)
		}
		if member.ID == "" {
			member.ID = id
		}
		members = append(members, *member)
	}
	return members, nil
}

// pathID returns the last segment of a Redfish @odata.id.
func pathID(odataID string) string {
	if odataID == "" {
		return ""
	}
	return odataID[strings.LastIndex(odataID, "/")+1:]
}
