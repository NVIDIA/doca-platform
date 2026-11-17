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

package e2e

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// dpuObjectSizeBudget is the largest size a Ready DPU object may have, measured as compact JSON without
// metadata.managedFields. The DPU is a single etcd object and every write and watch event carries all of it,
// so raising the budget must be an explicit, reviewed change. It is a regression budget, far below the
// 1.5 MiB etcd request limit.
//
// managedFields counts towards the stored size but is left out of the measurement: the API server writes it,
// and its size depends on how many field managers have written the object and in what order, so it differs
// between runs of the same test and would make the check flaky. It records field paths and never values, so
// growth in DPF content such as long condition messages does not show up in it. The gap between the budget
// and the etcd limit leaves room for it.
const dpuObjectSizeBudget = 64 * 1024

type dpuObjectPart struct {
	name string
	size int
}

// measureDPUObject returns the compact JSON size of the DPU without metadata.managedFields, and the size of
// metadata, spec and each status field, largest first.
func measureDPUObject(dpu *provisioningv1.DPU) (int, []dpuObjectPart, error) {
	obj := dpu.DeepCopy()
	obj.ManagedFields = nil
	raw, err := json.Marshal(obj)
	if err != nil {
		return 0, nil, err
	}

	var top map[string]json.RawMessage
	if err := json.Unmarshal(raw, &top); err != nil {
		return 0, nil, err
	}
	var status map[string]json.RawMessage
	if s, ok := top["status"]; ok {
		if err := json.Unmarshal(s, &status); err != nil {
			return 0, nil, err
		}
	}

	parts := make([]dpuObjectPart, 0, len(status)+2)
	for _, name := range []string{"metadata", "spec"} {
		if v, ok := top[name]; ok {
			parts = append(parts, dpuObjectPart{name: name, size: len(v)})
		}
	}
	for name, v := range status {
		parts = append(parts, dpuObjectPart{name: "status." + name, size: len(v)})
	}
	slices.SortFunc(parts, func(a, b dpuObjectPart) int {
		return cmp.Or(cmp.Compare(b.size, a.size), cmp.Compare(a.name, b.name))
	})
	return len(raw), parts, nil
}

// dpuOverBudgetError returns nil when size is within the budget, and otherwise an error naming the size, the
// budget and the size of each part.
func dpuOverBudgetError(dpu *provisioningv1.DPU, size int, parts []dpuObjectPart, budget int) error {
	if size <= budget {
		return nil
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "DPU %s is %d bytes without managedFields, over the %d byte budget:",
		klog.KObj(dpu), size, budget)
	for _, p := range parts {
		fmt.Fprintf(&sb, "\n  %s: %d bytes", p.name, p.size)
	}
	return errors.New(sb.String())
}

// VerifyDPUObjectSizes fails if the list does not hold expectedDPUs DPUs, or if any of them is larger than
// dpuObjectSizeBudget. Call it once the DPUs are Ready.
func VerifyDPUObjectSizes(ctx context.Context, c client.Client, expectedDPUs int, opts ...client.ListOption) {
	By(fmt.Sprintf("Verifying every DPU object is within the %d byte size budget", dpuObjectSizeBudget))
	dpus := &provisioningv1.DPUList{}
	Expect(c.List(ctx, dpus, opts...)).To(Succeed())
	Expect(dpus.Items).To(HaveLen(expectedDPUs), "expected %d DPUs to measure", expectedDPUs)
	errs := make([]error, 0, len(dpus.Items))
	for i := range dpus.Items {
		dpu := &dpus.Items[i]
		size, parts, err := measureDPUObject(dpu)
		if err != nil {
			GinkgoWriter.Printf("Failed to calculate object size for DPU %s: %v\n", klog.KObj(dpu), err)
			errs = append(errs, fmt.Errorf("measuring DPU %s: %w", klog.KObj(dpu), err))
			continue
		}
		GinkgoWriter.Printf("DPU %s object size: %d bytes (budget %d)\n", klog.KObj(dpu), size, dpuObjectSizeBudget)
		errs = append(errs, dpuOverBudgetError(dpu, size, parts, dpuObjectSizeBudget))
	}
	Expect(errors.Join(errs...)).NotTo(HaveOccurred())
}
