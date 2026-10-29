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

	utilsSync "github.com/nvidia/doca-platform/internal/storage/snap/csi-plugin/utils/sync"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// NewSerializeVolumeRequestsMiddleware initialize middleware which prevents concurrent operations
// on a single volume
func NewSerializeVolumeRequestsMiddleware(locker utilsSync.IDLocker) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{},
		info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (resp interface{}, err error) {
		fieldName, fieldValue := getIdentifierFieldForReq(req)
		if fieldName == "" {
			return handler(ctx, req)
		}
		gotLock, unlockFunc := locker.TryLock(utilsSync.LockKey(fieldName, fieldValue))
		if gotLock {
			defer unlockFunc()
			return handler(ctx, req)
		}
		return nil, status.Error(codes.Aborted, "pending")
	}
}
