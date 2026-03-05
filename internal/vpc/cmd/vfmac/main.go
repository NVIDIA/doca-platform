/*
SPDX-FileCopyrightText: Copyright (c) 2025 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
SPDX-License-Identifier: LicenseRef-NvidiaProprietary

NVIDIA CORPORATION, its affiliates and licensors retain all intellectual
property and proprietary rights in and to this material, related
documentation and any modifications thereto. Any use, reproduction,
disclosure or distribution of this material and related documentation
 without an express license agreement from NVIDIA CORPORATION or
its affiliates is strictly prohibited.
*/

package main

import (
	"log"

	"gitlab-master.nvidia.com/doca-platform-foundation/ovn-vpc/vfmac"
)

func main() {
	// Create a new VFMAC instance with default configuration
	vfmacInstance := vfmac.NewVFMAC(nil, "", "")

	// Process VFs using the new instance
	if err := vfmacInstance.ProcessVFs(); err != nil {
		log.Fatalf("[ERROR] Error processing VFs: %v", err)
	}
	log.Printf("[INFO] Successfully processed VF MAC addresses")
}
