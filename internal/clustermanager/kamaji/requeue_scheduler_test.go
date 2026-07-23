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

package nvidia

import (
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/event"
)

var _ = Describe("channelRequeueScheduler", func() {
	It("deduplicates pending requests and allows scheduling again after delivery", func() {
		events := make(chan event.GenericEvent, 2)
		scheduler := newChannelRequeueScheduler(events).(*channelRequeueScheduler)
		cluster := types.NamespacedName{Name: "test-cluster", Namespace: "test-ns"}
		stopPendingTimer := func() {
			scheduler.lock.Lock()
			defer scheduler.lock.Unlock()
			if timer, ok := scheduler.timers[cluster]; ok {
				timer.Stop()
				delete(scheduler.timers, cluster)
			}
		}
		DeferCleanup(stopPendingTimer)

		scheduler.Schedule(cluster, time.Hour)
		scheduler.Schedule(cluster, time.Hour)

		scheduler.lock.Lock()
		pendingTimers := len(scheduler.timers)
		_, clusterPending := scheduler.timers[cluster]
		scheduler.lock.Unlock()
		Expect(pendingTimers).To(Equal(1))
		Expect(clusterPending).To(BeTrue())

		stopPendingTimer()
		scheduler.Schedule(cluster, 0)
		var received event.GenericEvent
		Eventually(events).Should(Receive(&received))
		Expect(received.Object.GetName()).To(Equal(cluster.Name))
		Expect(received.Object.GetNamespace()).To(Equal(cluster.Namespace))

		scheduler.Schedule(cluster, 0)
		Eventually(events).Should(Receive(&received))
		Expect(received.Object.GetName()).To(Equal(cluster.Name))
		Expect(received.Object.GetNamespace()).To(Equal(cluster.Namespace))
	})
})
