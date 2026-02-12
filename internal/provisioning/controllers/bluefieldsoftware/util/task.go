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

package util

import (
	"fmt"
	"sync"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"

	"k8s.io/apimachinery/pkg/types"
)

var DownloadingTaskMap sync.Map

type ComponentDownloadTask struct {
	TaskName      string
	URL           string
	FileName      string
	ComponentName string
	UID           types.UID
}

// ComponentType represents the type of component to download
type ComponentType string

const (
	ComponentTypeFwBundle  ComponentType = "fwbundle"
	ComponentTypeOSISO     ComponentType = "osiso"
	ComponentTypeBMCEROT   ComponentType = "bmcerot"
	ComponentTypeBMC       ComponentType = "bmc"
	ComponentTypeNIC       ComponentType = "nic"
	ComponentTypeGRACEEROT ComponentType = "graceerot"
	ComponentTypeGRACEFW   ComponentType = "gracefw"
)

func DefaultComponentFilename(bfs *provisioningv1.BlueFieldSoftware, componentType ComponentType) string {
	return fmt.Sprintf("%s-%s-%s", bfs.Namespace, bfs.Name, componentType)
}

func GenerateComponentTaskName(bfs provisioningv1.BlueFieldSoftware, componentType ComponentType) string {
	return fmt.Sprintf("%s-%s-%s", bfs.Namespace, bfs.Name, componentType)
}
