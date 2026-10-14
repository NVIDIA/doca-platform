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
	"bytes"
	"context"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/go-logr/logr"
	"github.com/go-logr/logr/funcr"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	ctrl "sigs.k8s.io/controller-runtime"
)

var _ = Describe("Log tests", func() {
	var (
		ctx     context.Context
		testLog logr.Logger
		buf     *bytes.Buffer
	)
	BeforeEach(func() {
		ctx = context.Background()
		buf = &bytes.Buffer{}
		testLog = funcr.New(func(prefix, args string) {
			buf.WriteString(prefix)
			buf.WriteString(args)
			buf.WriteString("\n")
		}, funcr.Options{})
	})

	Context("SetLoggerMiddleware", func() {
		BeforeEach(func() {
			origLog := ctrl.Log
			DeferCleanup(func() { ctrl.SetLogger(origLog) })
			ctrl.SetLogger(testLog)
		})
		It("set", func() {
			handler := fakeHandler{}
			_, err := SetLoggerMiddleware(ctx, &csi.CreateVolumeRequest{Name: "test"},
				testUnaryServerInfo, handler.Handle)
			Expect(err).NotTo(HaveOccurred())
			logFromCtx, err := logr.FromContext(handler.CalledWithCtx)
			Expect(err).NotTo(HaveOccurred())
			logFromCtx.Info("test entry")
			Expect(buf.String()).To(And(
				ContainSubstring(`"msg"="test entry"`),
				ContainSubstring(`"method"="foobar"`),
				ContainSubstring(`"volumeName"="test"`),
				ContainSubstring(`"reqID"`),
			))
		})
		It("error", func() {
			handler := fakeHandler{Error: errTest}
			_, err := SetLoggerMiddleware(ctx, &csi.CreateVolumeRequest{Name: "test"},
				testUnaryServerInfo, handler.Handle)
			Expect(err).To(MatchError(errTest))
		})
	})
	Context("LogRequestMiddleware", func() {
		It("log", func() {
			handler := fakeHandler{}
			_, err := LogRequestMiddleware(logr.NewContext(ctx, testLog),
				&csi.CreateVolumeRequest{Name: "test", Parameters: map[string]string{"param1": "value1"}},
				testUnaryServerInfo, handler.Handle)
			Expect(err).NotTo(HaveOccurred())
			Expect(buf.String()).To(And(
				ContainSubstring(`"msg"="REQUEST"`),
				ContainSubstring(`"Parameters"="map[param1:value1]"`),
			))
		})
		It("error", func() {
			handler := fakeHandler{Error: errTest}
			_, err := LogRequestMiddleware(logr.NewContext(ctx, testLog),
				&csi.CreateVolumeRequest{Name: "test"},
				testUnaryServerInfo, handler.Handle)
			Expect(err).To(MatchError(errTest))
		})
	})
	Context("LogResponseMiddleware", func() {
		It("log", func() {
			handler := fakeHandler{
				Resp: &csi.CreateVolumeResponse{Volume: &csi.Volume{VolumeId: "12345"}},
			}
			_, err := LogResponseMiddleware(logr.NewContext(ctx, testLog),
				&csi.CreateVolumeRequest{Name: "test"},
				testUnaryServerInfo, handler.Handle)
			Expect(err).NotTo(HaveOccurred())
			Expect(buf.String()).To(And(
				ContainSubstring(`"msg"="RESPONSE"`),
				ContainSubstring(`"Volume"="volume_id:\"12345\""`),
			))
		})
		It("error", func() {
			handler := fakeHandler{Error: errTest}
			_, err := LogResponseMiddleware(logr.NewContext(ctx, testLog),
				&csi.CreateVolumeRequest{Name: "test"},
				testUnaryServerInfo, handler.Handle)
			Expect(err).To(MatchError(errTest))
			Expect(buf.String()).To(And(
				ContainSubstring(`"msg"="ERROR RESPONSE"`),
				ContainSubstring(`"error"="test error"`),
			))
		})
	})
})
