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
	"errors"
	"fmt"
	"net/url"

	vaultapi "github.com/hashicorp/vault/api"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func responseError(code int) error {
	return &vaultapi.ResponseError{StatusCode: code, Errors: []string{"boom"}}
}

var _ = Describe("error classification", func() {
	Describe("isAuthError", func() {
		It("matches 401 and 403 response errors", func() {
			Expect(isAuthError(responseError(403))).To(BeTrue())
			Expect(isAuthError(responseError(401))).To(BeTrue())
		})
		It("does not match other errors", func() {
			Expect(isAuthError(responseError(500))).To(BeFalse())
			Expect(isAuthError(errors.New("plain"))).To(BeFalse())
			Expect(isAuthError(nil)).To(BeFalse())
		})
	})

	Describe("isConnectivityError", func() {
		It("matches url errors, context deadlines and 5xx responses", func() {
			Expect(isConnectivityError(&url.Error{Op: "Get", URL: "https://vault", Err: errors.New("refused")})).To(BeTrue())
			Expect(isConnectivityError(context.DeadlineExceeded)).To(BeTrue())
			Expect(isConnectivityError(responseError(503))).To(BeTrue())
		})
		It("does not match auth errors, caller cancellation or plain errors", func() {
			Expect(isConnectivityError(responseError(403))).To(BeFalse())
			Expect(isConnectivityError(context.Canceled)).To(BeFalse())
			Expect(isConnectivityError(errors.New("plain"))).To(BeFalse())
			Expect(isConnectivityError(nil)).To(BeFalse())
		})
	})

	Describe("toGRPCError", func() {
		It("maps connectivity failures to Unavailable", func() {
			err := toGRPCError(&url.Error{Op: "Get", URL: "https://vault", Err: errors.New("refused")})
			Expect(status.Code(err)).To(Equal(codes.Unavailable))
		})
		It("maps caller cancellation to Canceled", func() {
			Expect(status.Code(toGRPCError(context.Canceled))).To(Equal(codes.Canceled))
			Expect(status.Code(toGRPCError(fmt.Errorf("wrapped: %w", context.Canceled)))).To(Equal(codes.Canceled))
		})
		It("maps auth failures to PermissionDenied", func() {
			Expect(status.Code(toGRPCError(responseError(403)))).To(Equal(codes.PermissionDenied))
		})
		It("maps everything else to Internal", func() {
			Expect(status.Code(toGRPCError(errors.New("plain")))).To(Equal(codes.Internal))
		})
		It("returns nil for nil", func() {
			Expect(toGRPCError(nil)).To(Succeed())
		})
	})
})
