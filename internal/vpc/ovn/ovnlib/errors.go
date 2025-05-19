/*
COPYRIGHT 2025 NVIDIA

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
