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

package common

import (
	"fmt"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func ValidateVolumeCapability(volCap *csi.VolumeCapability) error {
	if volCap == nil {
		return FieldIsRequiredError("VolumeCapability")
	}
	return CheckVolumeCapability("VolumeCapability", volCap)
}

func ValidateVolumeCapabilities(volCaps []*csi.VolumeCapability) error {
	if len(volCaps) == 0 {
		return FieldIsRequiredError("VolumeCapabilities")
	}
	for i, volCap := range volCaps {
		if volCap == nil {
			return FieldIsRequiredError("VolumeCapabilities")
		}
		err := CheckVolumeCapability(fmt.Sprintf("VolumeCapabilities[%d]", i), volCap)
		if err != nil {
			return err
		}
	}
	return nil
}

func CheckVolumeCapability(fieldName string, volCap *csi.VolumeCapability) error {
	if volCap.AccessMode == nil || volCap.AccessMode.Mode == 0 {
		return FieldIsRequiredError(fieldName + ".AccessMode")
	}
	switch aType := volCap.GetAccessType().(type) {
	case *csi.VolumeCapability_Block:
		if aType.Block == nil {
			return FieldIsRequiredError(fieldName + ".Block")
		}
	case *csi.VolumeCapability_Mount:
		return CallIsNotSupportedError("accessType Mount")
	default:
		return FieldIsRequiredError(fieldName + ".AccessType")
	}
	return nil
}

func CallIsNotSupportedError(methodName string) error {
	return status.Error(codes.Unimplemented, methodName+" is not supported")
}

func FieldIsRequiredError(filedName string) error {
	return status.Error(codes.InvalidArgument, FieldIsRequired(filedName))
}

func FieldIsRequired(filedName string) string {
	return filedName + ": field is required"
}
