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
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"google.golang.org/grpc"
)

var errTest = fmt.Errorf("test error")

func TestSource(t *testing.T) {
	RegisterFailHandler(Fail)
	suiteName := "GRPC Middleware Suite"
	RunSpecs(t, suiteName)
}

type fakeHandler struct {
	IsCalled      bool
	CalledWithCtx context.Context
	CalledWithReq interface{}
	Resp          interface{}
	Error         error
}

func (fh *fakeHandler) Handle(ctx context.Context, req interface{}) (interface{}, error) {
	fh.IsCalled = true
	fh.CalledWithCtx = ctx
	fh.CalledWithReq = req
	return fh.Resp, fh.Error
}

const testFullMethod = "foobar"

var testUnaryServerInfo = &grpc.UnaryServerInfo{
	FullMethod: testFullMethod,
}
