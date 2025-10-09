//go:build linux

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

package main

import (
	"log"

	"github.com/nvidia/doca-platform/pkg/utils/networkhelper"
	"github.com/nvidia/doca-platform/pkg/vfmac"
)

func main() {
	// Create a new VFMAC instance with default configuration
	vfmacInstance, err := vfmac.NewVFMAC(nil, networkhelper.New(), "", "")
	if err != nil {
		log.Fatalf("[ERROR] Error creating VFMAC instance: %v", err)
	}

	// Process VFs using the new instance
	if err := vfmacInstance.ProcessVFs(); err != nil {
		log.Fatalf("[ERROR] Error processing VFs: %v", err)
	}
	log.Printf("[INFO] Successfully processed VF MAC addresses")
}
