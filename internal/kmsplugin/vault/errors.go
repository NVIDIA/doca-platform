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

package vault

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"net/http"

	vaultapi "github.com/hashicorp/vault/api"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// isAuthError reports whether err is a Vault authentication or authorization
// failure (HTTP 401/403). These are potentially recoverable by re-reading the
// credential files and logging in again.
func isAuthError(err error) bool {
	var respErr *vaultapi.ResponseError
	if errors.As(err, &respErr) {
		return respErr.StatusCode == http.StatusForbidden || respErr.StatusCode == http.StatusUnauthorized
	}
	return false
}

// isConnectivityError reports whether err looks like a network, timeout, TLS
// or Vault server-side (5xx) failure. These are not fixed by re-authenticating,
// so the caller should back off and surface the plugin as unhealthy.
func isConnectivityError(err error) bool {
	if err == nil {
		return false
	}

	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}

	var certErr *tls.CertificateVerificationError
	if errors.As(err, &certErr) {
		return true
	}

	var netErr net.Error
	if errors.As(err, &netErr) {
		return true
	}

	// A 5xx from Vault indicates a server-side problem rather than a client
	// authentication issue.
	var respErr *vaultapi.ResponseError
	if errors.As(err, &respErr) {
		return respErr.StatusCode >= http.StatusInternalServerError
	}

	return false
}

// toGRPCError maps a backend error to a gRPC status error with an appropriate
// code so the kube-apiserver can distinguish transient from permanent
// failures. This is what lets TransitService satisfy the error contract
// documented on server.Backend: every error TransitService returns has
// already gone through here before the server package sees it.
func toGRPCError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, context.Canceled):
		return status.Error(codes.Canceled, err.Error())
	case isConnectivityError(err):
		return status.Error(codes.Unavailable, err.Error())
	case isAuthError(err):
		return status.Error(codes.PermissionDenied, err.Error())
	default:
		return status.Error(codes.Internal, err.Error())
	}
}
