//go:build linux

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
	"encoding/json"
	"os"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
)

var _ = Describe("FileSystemStore", func() {
	var tmpDir string

	BeforeEach(func() {
		var err error
		tmpDir, err = os.MkdirTemp("", "test-boot-id-")
		Expect(err).NotTo(HaveOccurred())
	})

	AfterEach(func() {
		if tmpDir != "" {
			_ = os.RemoveAll(tmpDir)
		}
	})

	Context("Persist BootID Files", func() {
		It("should persist reboot cycle metadata in the request file", func() {
			dpu := &provisioningv1.DPU{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "persist-dpu",
					Namespace: "persist-namespace",
					UID:       "persist-uid",
				},
				Status: provisioningv1.DPUStatus{
					PreviousPhase: provisioningv1.DPUConfig,
					AgentStatus: &provisioningv1.AgentStatus{
						RebootSequenceCount: ptr.To(int32(2)),
					},
				},
			}
			store := &fileSystemStore{bootIDDir: tmpDir}

			err := store.PersistBootID(dpu)
			Expect(err).To(Succeed())

			request := readPersistedRequest(store.rebootRequestFileName(dpu))
			Expect(request.PreviousPhase).To(Equal(provisioningv1.DPUConfig))
			Expect(request.RebootSequenceCount).NotTo(BeNil())
			Expect(*request.RebootSequenceCount).To(Equal(int32(2)))
		})
	})

	Context("Detecting completed reboot", func() {
		It("should ignore reboot request files when previous phase is missing", func() {
			dpu := &provisioningv1.DPU{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "missing-phase-dpu",
					Namespace: "missing-phase-namespace",
					UID:       "missing-phase-uid",
				},
				Status: provisioningv1.DPUStatus{
					PreviousPhase: provisioningv1.DPUConfig,
					AgentStatus: &provisioningv1.AgentStatus{
						RebootSequenceCount: ptr.To(int32(3)),
					},
				},
			}
			store := &fileSystemStore{bootIDDir: tmpDir}
			writeRequestFile(store.rebootRequestFileName(dpu), &RebootRequest{
				DPUName:             dpu.Name,
				DPUNamespace:        dpu.Namespace,
				UID:                 string(dpu.UID),
				RebootID:            "previous-boot-id",
				RebootSequenceCount: ptr.To(int32(3)),
			})

			finished, err := store.IsRebootFinished(dpu)
			Expect(err).NotTo(HaveOccurred())
			Expect(finished).To(BeFalse())
		})

		It("should ignore reboot request files from a different previous phase", func() {
			dpu := &provisioningv1.DPU{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "phase-dpu",
					Namespace: "phase-namespace",
					UID:       "phase-uid",
				},
				Status: provisioningv1.DPUStatus{
					PreviousPhase: provisioningv1.DPUConfig,
					AgentStatus: &provisioningv1.AgentStatus{
						RebootSequenceCount: ptr.To(int32(3)),
					},
				},
			}
			store := &fileSystemStore{bootIDDir: tmpDir}
			writeRequestFile(store.rebootRequestFileName(dpu), &RebootRequest{
				DPUName:             dpu.Name,
				DPUNamespace:        dpu.Namespace,
				UID:                 string(dpu.UID),
				RebootID:            "previous-boot-id",
				PreviousPhase:       provisioningv1.DPUInitializeInterface,
				RebootSequenceCount: ptr.To(int32(3)),
			})

			finished, err := store.IsRebootFinished(dpu)
			Expect(err).NotTo(HaveOccurred())
			Expect(finished).To(BeFalse())
		})

		It("should ignore reboot request files from a different reboot sequence count", func() {
			dpu := &provisioningv1.DPU{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "sequence-dpu",
					Namespace: "sequence-namespace",
					UID:       "sequence-uid",
				},
				Status: provisioningv1.DPUStatus{
					PreviousPhase: provisioningv1.DPUConfig,
					AgentStatus: &provisioningv1.AgentStatus{
						RebootSequenceCount: ptr.To(int32(4)),
					},
				},
			}
			store := &fileSystemStore{bootIDDir: tmpDir}
			writeRequestFile(store.rebootRequestFileName(dpu), &RebootRequest{
				DPUName:             dpu.Name,
				DPUNamespace:        dpu.Namespace,
				UID:                 string(dpu.UID),
				RebootID:            "previous-boot-id",
				PreviousPhase:       provisioningv1.DPUConfig,
				RebootSequenceCount: ptr.To(int32(3)),
			})

			finished, err := store.IsRebootFinished(dpu)
			Expect(err).NotTo(HaveOccurred())
			Expect(finished).To(BeFalse())
		})

		It("should report reboot finished only when request metadata matches the current cycle", func() {
			dpu := &provisioningv1.DPU{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "current-cycle-dpu",
					Namespace: "current-cycle-namespace",
					UID:       "current-cycle-uid",
				},
				Status: provisioningv1.DPUStatus{
					PreviousPhase: provisioningv1.DPUConfig,
					AgentStatus: &provisioningv1.AgentStatus{
						RebootSequenceCount: ptr.To(int32(5)),
					},
				},
			}
			store := &fileSystemStore{bootIDDir: tmpDir}
			writeRequestFile(store.rebootRequestFileName(dpu), &RebootRequest{
				DPUName:             dpu.Name,
				DPUNamespace:        dpu.Namespace,
				UID:                 string(dpu.UID),
				RebootID:            "previous-boot-id",
				PreviousPhase:       provisioningv1.DPUConfig,
				RebootSequenceCount: ptr.To(int32(5)),
			})

			finished, err := store.IsRebootFinished(dpu)
			Expect(err).NotTo(HaveOccurred())
			Expect(finished).To(BeTrue())
		})
	})

	Context("Delete BootID Files", func() {
		It("should delete boot ID files", func() {
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
				err := store.PersistBootID(tc.dpu)
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

func readPersistedRequest(path string) *RebootRequest {
	data, err := os.ReadFile(path)
	Expect(err).NotTo(HaveOccurred())
	request := &RebootRequest{}
	err = json.Unmarshal(data, request)
	Expect(err).NotTo(HaveOccurred())
	return request
}

func writeRequestFile(path string, request *RebootRequest) {
	data, err := json.Marshal(request)
	Expect(err).NotTo(HaveOccurred())
	err = os.WriteFile(path, data, 0644)
	Expect(err).NotTo(HaveOccurred())
}
