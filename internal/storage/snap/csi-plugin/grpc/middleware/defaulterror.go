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

package middleware

import (
	"context"

	"github.com/go-logr/logr"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

var (
	// DefaultInternalErr is returned if any internal failure happened during request handling
	// we don't return details for internal errors to prevent possible leak of the sensitive
	// information. All internal error details should be printed to log instead of sending back to
	// the end user.
	DefaultInternalErr = status.Error(codes.Internal, "internal error")
)

// SetDefaultErr interceptor will set DefaultInternalErr grpc error,
// if non grpc error returned from the handler
func SetDefaultErr(ctx context.Context, req interface{},
	info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
	resp, err := handler(ctx, req)
	if err != nil {
		if _, isGRPCErr := err.(interface {
			GRPCStatus() *status.Status
		}); !isGRPCErr {
			reqLogger := logr.FromContextOrDiscard(ctx)
			reqLogger.Error(err, "internal error occurred")
			err = DefaultInternalErr
		}
	}
	return resp, err
}
