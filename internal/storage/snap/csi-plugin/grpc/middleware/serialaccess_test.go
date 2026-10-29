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

	"github.com/container-storage-interface/spec/lib/go/csi"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Serial access tests", func() {
	Context("NewSerializeVolumeRequestsMiddleware", func() {
		var (
			lck utilsSync.IDLocker
			ctx context.Context
		)
		BeforeEach(func() {
			lck = utilsSync.NewIDLocker()
			ctx = context.Background()
		})
		It("locked", func() {
			handler := fakeHandler{}
			locked, unlock := lck.TryLock(utilsSync.LockKey(VolumeNameFieldName, "volume1"))
			defer unlock()
			Expect(locked).To(BeTrue())
			m := NewSerializeVolumeRequestsMiddleware(lck)
			_, err := m(ctx, &csi.CreateVolumeRequest{Name: "volume1"}, testUnaryServerInfo, handler.Handle)
			Expect(err).To(MatchError(And(
				ContainSubstring("Aborted"),
				ContainSubstring("pending"),
			)))
		})
		It("not locked", func() {
			handler := fakeHandler{}
			m := NewSerializeVolumeRequestsMiddleware(lck)
			_, err := m(ctx, &csi.CreateVolumeRequest{Name: "volume1"}, testUnaryServerInfo, handler.Handle)
			Expect(err).NotTo(HaveOccurred())
		})
		It("error", func() {
			handler := fakeHandler{Error: errTest}
			m := NewSerializeVolumeRequestsMiddleware(lck)
			_, err := m(ctx, &csi.CreateVolumeRequest{Name: "volume1"}, testUnaryServerInfo, handler.Handle)
			Expect(err).To(MatchError(errTest))
		})
	})
	Expect(true).To(BeTrue())
})
