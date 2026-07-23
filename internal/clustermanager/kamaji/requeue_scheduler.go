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
	"sync"
	"time"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/event"
)

var (
	dpuClusterRequeueEvents           = make(chan event.GenericEvent, 1024)
	defaultDPUClusterRequeueScheduler = newChannelRequeueScheduler(dpuClusterRequeueEvents)
)

// requeueScheduler schedules delayed DPUCluster reconciliations.
type requeueScheduler interface {
	Schedule(types.NamespacedName, time.Duration)
}

// channelRequeueScheduler sends delayed DPUCluster generic events into a controller-runtime channel source.
type channelRequeueScheduler struct {
	events chan event.GenericEvent
	lock   sync.Mutex
	timers map[types.NamespacedName]*time.Timer
}

// newChannelRequeueScheduler creates a scheduler backed by the given generic event channel.
func newChannelRequeueScheduler(events chan event.GenericEvent) requeueScheduler {
	return &channelRequeueScheduler{
		events: events,
		timers: map[types.NamespacedName]*time.Timer{},
	}
}

// Schedule enqueues the DPUCluster after the given delay, deduplicating pending requests by name.
func (s *channelRequeueScheduler) Schedule(nn types.NamespacedName, after time.Duration) {
	s.lock.Lock()
	defer s.lock.Unlock()
	if _, ok := s.timers[nn]; ok {
		return
	}
	s.timers[nn] = time.AfterFunc(after, func() {
		s.lock.Lock()
		delete(s.timers, nn)
		s.lock.Unlock()
		select {
		case s.events <- event.GenericEvent{
			Object: &provisioningv1.DPUCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      nn.Name,
					Namespace: nn.Namespace,
				},
			},
		}:
		default:
		}
	})
}
