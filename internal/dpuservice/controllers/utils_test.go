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

package controllers

import (
	"time"

	dpuservicev1 "github.com/nvidia/doca-platform/api/dpuservice/v1alpha1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("Sort function", func() {
	Context("When sorting DPUService", func() {
		DescribeTable("Validates the DPUServices are sorted fron oldest to newest", func(dpuServices []dpuservicev1.DPUService) {
			objects := make([]client.Object, len(dpuServices))
			for i := range dpuServices {
				objects[i] = &dpuServices[i]
			}
			sortObjectsByCreationTimestamp(objects)
			for i := 0; i < len(objects)-1; i++ {
				Expect(objects[i].GetCreationTimestamp().Time.Before(objects[i+1].GetCreationTimestamp().Time)).To(BeTrue())
			}
		},
			Entry("from oldest to newest", []dpuservicev1.DPUService{
				{
					ObjectMeta: metav1.ObjectMeta{
						CreationTimestamp: metav1.NewTime(time.Now().Add(-time.Hour)),
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						CreationTimestamp: metav1.NewTime(time.Now()),
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						CreationTimestamp: metav1.NewTime(time.Now().Add(time.Hour)),
					},
				},
			}),
			Entry("from newest to oldest", []dpuservicev1.DPUService{
				{
					ObjectMeta: metav1.ObjectMeta{
						CreationTimestamp: metav1.NewTime(time.Now().Add(time.Hour)),
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						CreationTimestamp: metav1.NewTime(time.Now()),
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						CreationTimestamp: metav1.NewTime(time.Now().Add(-time.Hour)),
					},
				},
			}),
			Entry("random order", []dpuservicev1.DPUService{
				{
					ObjectMeta: metav1.ObjectMeta{
						CreationTimestamp: metav1.NewTime(time.Now().Add(time.Hour)),
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						CreationTimestamp: metav1.NewTime(time.Now().Add(-time.Hour)),
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						CreationTimestamp: metav1.NewTime(time.Now()),
					},
				},
			}),
		)
	})
})

var _ = Describe("getRevisionHistoryLimit", func() {
	now := time.Now()
	Context("When getting the revision history limit", func() {
		DescribeTable("Validates the revision history limit is correct", func(dpuServices []dpuservicev1.DPUService, revisionHistoryLimit int32, expected []dpuservicev1.DPUService) {
			objects := make([]client.Object, len(dpuServices))
			for i := range dpuServices {
				objects[i] = &dpuServices[i]
			}
			expectsObjects := make([]client.Object, len(expected))
			for i := range expected {
				expectsObjects[i] = &expected[i]
			}
			res := getRevisionHistoryLimitList(objects, revisionHistoryLimit)
			Expect(res).To(HaveLen(len(expectsObjects)))
			Expect(res).To(ConsistOf(expectsObjects))
		},
			Entry("less than the limit", []dpuservicev1.DPUService{
				{
					ObjectMeta: metav1.ObjectMeta{
						CreationTimestamp: metav1.NewTime(now.Add(-time.Hour)),
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						CreationTimestamp: metav1.NewTime(now),
					},
				},
			}, int32(5), []dpuservicev1.DPUService{
				{
					ObjectMeta: metav1.ObjectMeta{
						CreationTimestamp: metav1.NewTime(now),
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						CreationTimestamp: metav1.NewTime(now.Add(-time.Hour)),
					},
				},
			}),
			Entry("equal to the limit", []dpuservicev1.DPUService{
				{
					ObjectMeta: metav1.ObjectMeta{
						CreationTimestamp: metav1.NewTime(now.Add(2 * time.Hour)),
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						CreationTimestamp: metav1.NewTime(now.Add(-time.Hour)),
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						CreationTimestamp: metav1.NewTime(now),
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						CreationTimestamp: metav1.NewTime(now.Add(time.Hour)),
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						CreationTimestamp: metav1.NewTime(now.Add(3 * time.Hour)),
					},
				},
			}, int32(5), []dpuservicev1.DPUService{
				{
					ObjectMeta: metav1.ObjectMeta{
						CreationTimestamp: metav1.NewTime(now.Add(3 * time.Hour)),
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						CreationTimestamp: metav1.NewTime(now.Add(2 * time.Hour)),
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						CreationTimestamp: metav1.NewTime(now.Add(time.Hour)),
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						CreationTimestamp: metav1.NewTime(now),
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						CreationTimestamp: metav1.NewTime(now.Add(-time.Hour)),
					},
				},
			}),
			Entry("more than the limit", []dpuservicev1.DPUService{
				{
					ObjectMeta: metav1.ObjectMeta{
						CreationTimestamp: metav1.NewTime(now.Add(-time.Hour)),
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						CreationTimestamp: metav1.NewTime(now),
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						CreationTimestamp: metav1.NewTime(now.Add(time.Hour)),
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						CreationTimestamp: metav1.NewTime(now.Add(2 * time.Hour)),
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						CreationTimestamp: metav1.NewTime(now.Add(3 * time.Hour)),
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						CreationTimestamp: metav1.NewTime(now.Add(4 * time.Hour)),
					},
				},
			}, int32(5), []dpuservicev1.DPUService{
				{
					ObjectMeta: metav1.ObjectMeta{
						CreationTimestamp: metav1.NewTime(now.Add(4 * time.Hour)),
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						CreationTimestamp: metav1.NewTime(now.Add(3 * time.Hour)),
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						CreationTimestamp: metav1.NewTime(now.Add(2 * time.Hour)),
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						CreationTimestamp: metav1.NewTime(now.Add(time.Hour)),
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						CreationTimestamp: metav1.NewTime(now),
					},
				},
			}),
		)
	})
})
