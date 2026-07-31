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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

var _ = Describe("Default error tests", func() {
	Context("SetDefaultErr", func() {
		It("no error", func() {
			handler := fakeHandler{}
			_, err := SetDefaultErr(context.Background(), nil, testUnaryServerInfo, handler.Handle)
			Expect(err).NotTo(HaveOccurred())
		})
		It("explicit GRPC error", func() {
			handler := fakeHandler{Error: status.Error(codes.InvalidArgument, "invalid arg")}
			_, err := SetDefaultErr(context.Background(), nil, testUnaryServerInfo, handler.Handle)
			Expect(err).To(MatchError(And(ContainSubstring("InvalidArgument"), ContainSubstring("invalid arg"))))
		})
		It("explicit GRPC Internal error", func() {
			handler := fakeHandler{Error: status.Error(codes.Internal, "something specific")}
			_, err := SetDefaultErr(context.Background(), nil, testUnaryServerInfo, handler.Handle)
			Expect(err).To(MatchError(And(ContainSubstring("Internal"), ContainSubstring("something specific"))))
		})
		It("non GRPC error", func() {
			handler := fakeHandler{Error: fmt.Errorf("test")}
			_, err := SetDefaultErr(context.Background(), nil, testUnaryServerInfo, handler.Handle)
			Expect(err).To(MatchError(DefaultInternalErr))
		})
	})
})
