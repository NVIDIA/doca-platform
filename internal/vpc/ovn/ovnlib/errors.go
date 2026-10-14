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

package ovnlib

import "fmt"

type ErrCode int

const (
	ErrNotFound ErrCode = iota
	ErrInvalidArgument
	ErrInternal
	ErrUnknown
	// we can add more errors here
)

type OVNError struct {
	Code    ErrCode
	Message string
}

// Error method implements error interface
func (e *OVNError) Error() string {
	switch e.Code {
	case ErrNotFound:
		return "Not Found: " + e.Message
	case ErrInvalidArgument:
		return "Invalid Argument: " + e.Message
	case ErrInternal:
		return "Internal Error: " + e.Message
	default:
		return fmt.Sprintf("Error %d: %s", e.Code, e.Message)
	}
}

func GetOvnErrorCodeFromError(err error) ErrCode {
	if ovnError, ok := err.(*OVNError); ok {
		return ovnError.Code
	}
	return ErrUnknown
}

func NewOvnError(code ErrCode, format string, a ...any) *OVNError {
	return &OVNError{Code: code, Message: fmt.Sprintf(format, a...)}
}
