/*
COPYRIGHT 2024 NVIDIA

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

package controller

import (
	"context"

	"github.com/nvidia/doca-platform/internal/ovsutils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	gomock "go.uber.org/mock/gomock"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

//nolint:goconst
var _ = Describe("service interface controller", func() {
	var (
		mockCtrl       *gomock.Controller
		cleanupObjects []client.Object
		sir            *ServiceInterfaceReconciler
		ovsMock        *ovsutils.MockAPI
		ctx            = context.Background()
		testNode       = "test-node"
	)

	BeforeEach(func() {
		mockCtrl = gomock.NewController(GinkgoT())
		ovsMock = ovsutils.NewMockAPI(mockCtrl)

		sir = &ServiceInterfaceReconciler{
			Client:   testClient,
			NodeName: testNode,
			OVS:      ovsMock,
		}

	})

	AfterEach(func() {
		for _, obj := range cleanupObjects {
			Expect(testClient.Delete(ctx, obj)).To(Succeed())
		}

		mockCtrl.Finish()
	})

	It("reconcile non existing object - consider as deleted", func() {
		nn := types.NamespacedName{
			Namespace: "non-existing",
			Name:      "non-existing",
		}

		result, err := sir.Reconcile(ctx, ctrl.Request{NamespacedName: nn})
		Expect(err).To(Succeed())
		Expect(result.Requeue).To(BeFalse())
		Expect(result.RequeueAfter).To(BeZero())
	})
})
