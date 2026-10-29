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

	grpcCtx "github.com/nvidia/doca-platform/internal/storage/snap/csi-plugin/grpc/context"

	"github.com/google/uuid"
	"google.golang.org/grpc"
)

// SetReqIDMiddleware generates and set request id to context, it can be used for logging and other purpose
func SetReqIDMiddleware(ctx context.Context, req interface{},
	info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
	ctx = grpcCtx.SetRequestID(ctx, uuid.New().String())
	return handler(ctx, req)
}
