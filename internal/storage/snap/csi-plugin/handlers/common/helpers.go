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
	"strconv"
	"strings"

	storagev1 "github.com/nvidia/doca-platform/api/storage/v1alpha1"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// ValidateVolumeCapability validates a single VolumeCapability entry.
func ValidateVolumeCapability(volCap *csi.VolumeCapability) error {
	if volCap == nil {
		return FieldIsRequiredError("VolumeCapability")
	}
	return CheckVolumeCapability("VolumeCapability", volCap)
}

// ValidateVolumeCapabilities validates a list of VolumeCapability entries.
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

// CheckVolumeCapability validates a single VolumeCapability entry.
// accepts fieldName as a parameter to provide more detailed error message
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

// CallIsNotSupportedError returns an error with Unimplemented code and message
func CallIsNotSupportedError(methodName string) error {
	return status.Error(codes.Unimplemented, methodName+" is not supported")
}

// FieldIsRequiredError returns an error with InvalidArgument code and message
func FieldIsRequiredError(fieldName string) error {
	return status.Error(codes.InvalidArgument, FieldIsRequired(fieldName))
}

// FieldIsInvalidError returns an error with InvalidArgument code and message
func FieldIsInvalidError(fieldName string, msg string) error {
	return status.Error(codes.InvalidArgument, fieldName+": "+msg)
}

// FieldIsRequired returns a string with the message "field is required"
func FieldIsRequired(fieldName string) string {
	return fieldName + ": field is required"
}

// FunctionTypeConfigFromStrings constructs a FunctionTypeConfig from functionType and hotplugFunction strings.
// The function validates that functionType is either "vf" or "pf" (case-insensitive) and that hotplugFunction
// is a valid boolean value. It also ensures that hotplugFunction cannot be true when functionType is "vf".
// If functionType is empty, it defaults to "vf". If hotplugFunction is empty, it defaults to false.
func FunctionTypeConfigFromStrings(functionType string, hotplugFunction string) (storagev1.FunctionTypeConfig, error) {
	result := storagev1.FunctionTypeConfig{
		FunctionType:    DefaultFunctionType,
		HotplugFunction: DefaultHotplugFunction,
	}
	var err error
	if functionType != "" {
		switch strings.ToLower(functionType) {
		case string(storagev1.FunctionTypeVF):
			result.FunctionType = storagev1.FunctionTypeVF
		case string(storagev1.FunctionTypePF):
			result.FunctionType = storagev1.FunctionTypePF
		default:
			return storagev1.FunctionTypeConfig{}, fmt.Errorf("functionType: unsupported value %s, supported values are: %s, %s",
				functionType, storagev1.FunctionTypeVF, storagev1.FunctionTypePF)
		}
	}
	if hotplugFunction != "" {
		result.HotplugFunction, err = strconv.ParseBool(hotplugFunction)
		if err != nil {
			return storagev1.FunctionTypeConfig{}, fmt.Errorf("hotplugFunction: is not a boolean value")
		}
	}
	if result.FunctionType == storagev1.FunctionTypeVF && result.HotplugFunction {
		return storagev1.FunctionTypeConfig{}, fmt.Errorf("hotplugFunction: can only be true when functionType is %s", storagev1.FunctionTypePF)
	}
	return result, nil
}

// FunctionTypeConfigAsStrings converts a FunctionTypeConfig to a string representation of the functionType and hotplugFunction.
// The functionType is converted to a lowercase string. The hotplugFunction is converted to a string representation of a boolean value.
func FunctionTypeConfigAsStrings(config storagev1.FunctionTypeConfig) (string, string) {
	return strings.ToLower(string(config.FunctionType)), strconv.FormatBool(config.HotplugFunction)
}
