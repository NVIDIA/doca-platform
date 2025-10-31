/*
Copyright 2025 NVIDIA

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

package reboot

import (
	"context"
	"os"
	"path/filepath"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
)

var _ = Describe("FileSystemStore", func() {
	Context("Delete BootID Files", func() {
		It("should delete boot ID files", func() {
			tmpDir := filepath.Join(os.TempDir(), "test-boot-id-deletion")
			_ = os.RemoveAll(tmpDir)
			_ = os.MkdirAll(tmpDir, 0755)
			defer os.RemoveAll(tmpDir) // nolint: errcheck
			rebootingDPU := &provisioningv1.DPU{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "rebooting-dpu",
					Namespace: "rebooting-namespace",
					UID:       "rebooting-uid",
				},
				Status: provisioningv1.DPUStatus{
					Phase: provisioningv1.DPURebooting,
				},
			}
			readyDPU := &provisioningv1.DPU{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "ready-dpu",
					Namespace: "ready-namespace",
					UID:       "ready-uid",
				},
				Status: provisioningv1.DPUStatus{
					Phase: provisioningv1.DPUReady,
				},
			}
			errorDPU := &provisioningv1.DPU{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "error-dpu",
					Namespace: "error-namespace",
					UID:       "error-uid",
				},
				Status: provisioningv1.DPUStatus{
					Phase: provisioningv1.DPUError,
				},
			}
			dpus := map[types.NamespacedName]*provisioningv1.DPU{
				{Name: rebootingDPU.Name, Namespace: rebootingDPU.Namespace}: rebootingDPU,
				{Name: readyDPU.Name, Namespace: readyDPU.Namespace}:         readyDPU,
				{Name: errorDPU.Name, Namespace: errorDPU.Namespace}:         errorDPU,
			}
			store := &fileSystemStore{
				bootIDDir: tmpDir,
				getDPUFunc: func(ctx context.Context, name types.NamespacedName) (*provisioningv1.DPU, error) {
					dpu, ok := dpus[name]
					if !ok {
						return nil, apierrors.NewNotFound(schema.GroupResource{Group: "provisioning.nvidia.com", Resource: "dpus"}, name.Name)
					}
					return dpu, nil
				},
			}
			type testCase struct {
				dpu             *provisioningv1.DPU
				shouldBeDeleted bool
			}
			testCases := []testCase{
				{
					dpu: &provisioningv1.DPU{
						ObjectMeta: metav1.ObjectMeta{
							Name:      rebootingDPU.Name,
							Namespace: rebootingDPU.Namespace,
							UID:       rebootingDPU.UID,
						},
					},
					shouldBeDeleted: false,
				},
				{
					dpu: &provisioningv1.DPU{
						ObjectMeta: metav1.ObjectMeta{
							Name:      rebootingDPU.Name,
							Namespace: rebootingDPU.Namespace,
							UID:       "different-uid",
						},
					},
					shouldBeDeleted: true,
				},
				{
					dpu: &provisioningv1.DPU{
						ObjectMeta: metav1.ObjectMeta{
							Name:      readyDPU.Name,
							Namespace: readyDPU.Namespace,
							UID:       readyDPU.UID,
						},
					},
					shouldBeDeleted: true,
				},
				{
					dpu: &provisioningv1.DPU{
						ObjectMeta: metav1.ObjectMeta{
							Name:      errorDPU.Name,
							Namespace: errorDPU.Namespace,
							UID:       errorDPU.UID,
						},
					},
					shouldBeDeleted: true,
				},
				{
					dpu: &provisioningv1.DPU{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "does-not-exist",
							Namespace: "does-not-exist",
							UID:       "does-not-exist",
						},
					},
					shouldBeDeleted: true,
				},
			}
			for _, tc := range testCases {
				err := store.PersistBootID(tc.dpu, false)
				Expect(err).To(Succeed())
				_, err = os.Stat(store.rebootRequestFileName(tc.dpu))
				Expect(err).To(Succeed())
				store.housekeeping(context.TODO())
				_, err = os.Stat(store.rebootRequestFileName(tc.dpu))
				if tc.shouldBeDeleted {
					Expect(os.IsNotExist(err)).To(BeTrue())
				} else {
					Expect(err).To(Succeed())
				}
			}
		})
	})
})
