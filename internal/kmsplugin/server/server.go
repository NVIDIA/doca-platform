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

// Package server implements the Kubernetes KMS v2 gRPC service on top of a
// pluggable encryption Backend.
package server

import (
	"context"

	"github.com/go-logr/logr"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	kmsapi "k8s.io/kms/apis/v2"
)

// apiVersion is the KMS v2 API version reported in Status responses.
const apiVersion = "v2"

// healthOK is the healthz value the kube-apiserver expects from a healthy plugin.
const healthOK = "ok"

// Backend performs the actual cryptographic operations. The Vault Transit
// service implements it; tests provide fakes.
//
// Error contract: a returned error should already be a gRPC status error with
// a code the kube-apiserver can act on, e.g. Unavailable for a connectivity
// problem or PermissionDenied for an auth failure; see the vault package's
// toGRPCError for the implementation Backend uses. ensureStatusError below is
// only a safety net that wraps a plain error as Internal, so an implementation
// that skips classification degrades to an unhelpful but still valid code
// instead of leaking codes.Unknown to the kube-apiserver.
type Backend interface {
	// Encrypt returns the ciphertext and the key ID used.
	Encrypt(ctx context.Context, plaintext []byte) (ciphertext []byte, keyID string, err error)
	// Decrypt returns the plaintext for the given ciphertext and key ID.
	Decrypt(ctx context.Context, ciphertext []byte, keyID string) (plaintext []byte, err error)
	// Status performs a live backend health check and returns the current key ID.
	Status(ctx context.Context) (keyID string, err error)
}

// Server adapts a Backend to the Kubernetes KMS v2 gRPC service.
type Server struct {
	kmsapi.UnimplementedKeyManagementServiceServer

	backend Backend
	log     logr.Logger
}

// New builds a KMS v2 Server backed by the given Backend.
func New(backend Backend, log logr.Logger) *Server {
	return &Server{backend: backend, log: log}
}

// Status reports plugin health and the current key ID. The Backend is expected
// to perform a live check, so any failure is surfaced to the kube-apiserver.
func (s *Server) Status(ctx context.Context, _ *kmsapi.StatusRequest) (*kmsapi.StatusResponse, error) {
	keyID, err := s.backend.Status(ctx)
	if err != nil {
		s.log.Error(err, "KMS status health check failed")
		return nil, ensureStatusError(err)
	}
	return &kmsapi.StatusResponse{
		Version: apiVersion,
		Healthz: healthOK,
		KeyId:   keyID,
	}, nil
}

// Encrypt encrypts the request plaintext.
func (s *Server) Encrypt(ctx context.Context, req *kmsapi.EncryptRequest) (*kmsapi.EncryptResponse, error) {
	if len(req.Uid) == 0 {
		return nil, status.Error(codes.InvalidArgument, "uid is required")
	}

	ciphertext, keyID, err := s.backend.Encrypt(ctx, req.Plaintext)
	if err != nil {
		s.log.Error(err, "encrypt failed", "uid", req.Uid)
		return nil, ensureStatusError(err)
	}

	return &kmsapi.EncryptResponse{
		Ciphertext: ciphertext,
		KeyId:      keyID,
	}, nil
}

// Decrypt decrypts the request ciphertext.
func (s *Server) Decrypt(ctx context.Context, req *kmsapi.DecryptRequest) (*kmsapi.DecryptResponse, error) {
	if len(req.Uid) == 0 {
		return nil, status.Error(codes.InvalidArgument, "uid is required")
	}

	plaintext, err := s.backend.Decrypt(ctx, req.Ciphertext, req.KeyId)
	if err != nil {
		s.log.Error(err, "decrypt failed", "uid", req.Uid)
		return nil, ensureStatusError(err)
	}

	return &kmsapi.DecryptResponse{
		Plaintext: plaintext,
	}, nil
}

// ensureStatusError guarantees a gRPC status error is returned, per the
// Backend error contract above. The Vault backend already returns classified
// status errors, so this only wraps a plain error as Internal, e.g. from a
// Backend implementation that has not done its own classification.
func ensureStatusError(err error) error {
	if _, ok := status.FromError(err); ok {
		return err
	}
	return status.Error(codes.Internal, err.Error())
}
