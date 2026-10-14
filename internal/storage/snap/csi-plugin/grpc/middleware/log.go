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
	"fmt"
	"reflect"
	"strings"

	grpcCtx "github.com/nvidia/doca-platform/internal/storage/snap/csi-plugin/grpc/context"

	"github.com/go-logr/logr"
	"google.golang.org/grpc"
	ctrl "sigs.k8s.io/controller-runtime"
)

// SetLoggerMiddleware creates logger instance with additional information and saves it to req context
func SetLoggerMiddleware(ctx context.Context, req interface{},
	info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
	reqID, _ := grpcCtx.GetRequestID(ctx)
	reqLogger := ctrl.Log.WithValues("method", info.FullMethod, "reqID", reqID)
	fieldName, fieldValue := getIdentifierFieldForReq(req)
	if fieldName != "" {
		reqLogger = reqLogger.WithValues(fieldName, fieldValue)
	}
	return handler(logr.NewContext(ctx, reqLogger), req)
}

// LogRequestMiddleware log request with configured logger
func LogRequestMiddleware(ctx context.Context, req interface{},
	info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
	reqLogger := logr.FromContextOrDiscard(ctx)
	reqLogger = addFieldsToLogger(reqLogger, req)
	reqLogger.Info("REQUEST")
	return handler(ctx, req)
}

// LogResponseMiddleware log response with configured logger
func LogResponseMiddleware(ctx context.Context, req interface{},
	info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
	reqLogger := logr.FromContextOrDiscard(ctx)
	resp, err := handler(ctx, req)
	if err != nil {
		reqLogger.Error(err, "ERROR RESPONSE")
	} else {
		reqLogger = addFieldsToLogger(reqLogger, resp)
		reqLogger.Info("RESPONSE")
	}
	return resp, err
}

// log exported fields from req or resp object
func addFieldsToLogger(logger logr.Logger, obj interface{}) logr.Logger {
	val := reflect.ValueOf(obj).Elem()
	if !val.IsValid() {
		return logger
	}
	valType := val.Type()
	fieldCount := valType.NumField()
	var logFields []interface{}
	for i := 0; i < fieldCount; i++ {
		name := valType.Field(i).Name
		if valType.Field(i).PkgPath != "" {
			// for exported filed this values should be empty
			continue
		}
		// exclude secrets from log message
		if strings.Contains(name, "Secrets") {
			continue
		}
		logFields = append(logFields, name, fmt.Sprintf("%v", val.Field(i).Interface()))
	}
	return logger.WithValues(logFields...)
}
