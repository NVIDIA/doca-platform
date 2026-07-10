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

package server

import (
	"context"
	"fmt"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	kmsapi "k8s.io/kms/apis/v2"
)

// CheckReady connects to the KMS v2 plugin over the given Unix domain socket and
// verifies it reports healthy. It calls the same Status RPC the kube-apiserver
// uses, which performs a live backend probe, so a successful result means the
// running plugin can actually reach the backend and use the configured key.
//
// It is intended for the DaemonSet readiness probe and performs no backend
// authentication of its own: it only talks to the local plugin socket.
func CheckReady(ctx context.Context, socketPath string) error {
	conn, err := grpc.NewClient("unix://"+socketPath, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return fmt.Errorf("connecting to KMS socket %q: %w", socketPath, err)
	}
	defer func() { _ = conn.Close() }()

	resp, err := kmsapi.NewKeyManagementServiceClient(conn).Status(ctx, &kmsapi.StatusRequest{})
	if err != nil {
		return fmt.Errorf("KMS status check failed: %w", err)
	}
	if resp.Healthz != healthOK {
		return fmt.Errorf("KMS plugin not healthy: healthz=%q", resp.Healthz)
	}
	if resp.Version != apiVersion {
		return fmt.Errorf("unexpected KMS API version %q, want %q", resp.Version, apiVersion)
	}
	if resp.KeyId == "" {
		return fmt.Errorf("KMS plugin reported an empty key ID")
	}
	return nil
}
