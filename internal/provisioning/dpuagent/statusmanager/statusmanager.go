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

// Package statusmanager owns the DPU agent's in-memory AgentStatus and pushes it to the DPU CR.
// All methods are safe for concurrent use. A single worker goroutine performs every push, so
// pushes never interleave and an older snapshot can never overwrite a newer one.
package statusmanager

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const defaultRetryInterval = 2 * time.Second

// ErrStaleDPU is returned when the DPU UID no longer matches the UID given to New.
var ErrStaleDPU = errors.New("stale DPU object")

// Manager holds the agent's AgentStatus in memory and pushes it to the DPU CR status.
type Manager struct {
	client client.Client
	key    client.ObjectKey
	// uid is the expected DPU UID; empty means pushes do not check the DPU UID.
	uid string

	// mu guards status and pending.
	mu     sync.Mutex
	status provisioningv1.AgentStatus
	// pending holds the requests waiting for a push. The worker takes them together with the
	// status snapshot, so every request it takes is covered by that snapshot.
	pending []*request

	// wake tells the worker that pending has requests. It holds at most one signal.
	wake          chan struct{}
	retryInterval time.Duration

	// ctx is the worker's context, set by Start. UpdateRemote waits on it instead of a caller
	// context, so a caller cannot leave a request behind that the worker keeps retrying.
	ctx       context.Context
	startOnce sync.Once
}

type request struct {
	// untilSuccess retries the push until it succeeds; otherwise the push is tried once.
	untilSuccess bool
	// done is closed by the worker once the request is finished; err is written before that.
	done chan struct{}
	err  error
}

func (r *request) finish(err error) {
	r.err = err
	close(r.done)
}

// New returns a Manager that pushes to the DPU namespace/name. Call Start before UpdateRemote.
// When uid is not empty, pushes refuse a DPU with another UID, and UpdateRemote with untilSuccess
// stops retrying when the DPU is missing or has another UID.
func New(c client.Client, namespace, name, uid string) *Manager {
	return &Manager{
		client:        c,
		key:           client.ObjectKey{Namespace: namespace, Name: name},
		uid:           uid,
		wake:          make(chan struct{}, 1),
		retryInterval: defaultRetryInterval,
	}
}

// Start runs the push worker until ctx is canceled. The worker is meant to live as long as the
// agent. Only the first call starts a worker; later calls do nothing.
func (m *Manager) Start(ctx context.Context) {
	m.startOnce.Do(func() {
		m.ctx = ctx
		go m.run(ctx)
	})
}

// GetLocal returns a copy of the in-memory status, including changes not pushed yet.
// It does not read the DPU CR.
func (m *Manager) GetLocal() provisioningv1.AgentStatus {
	m.mu.Lock()
	defer m.mu.Unlock()
	return *m.status.DeepCopy()
}

// UpdateLocal runs fn on the in-memory status under the lock. It does not push.
func (m *Manager) UpdateLocal(fn func(s *provisioningv1.AgentStatus)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	fn(&m.status)
}

// UpdateRemote pushes the in-memory status to the DPU CR and waits for the push. With
// untilSuccess it retries until the push succeeds or the DPU is gone (see New); otherwise it
// tries once. It returns the error of the last push, or the worker's context error when the
// worker stops first. Call Start before UpdateRemote.
func (m *Manager) UpdateRemote(untilSuccess bool) error {
	if m.ctx == nil {
		panic("statusmanager: UpdateRemote called before Start")
	}
	req := &request{untilSuccess: untilSuccess, done: make(chan struct{})}
	m.enqueue(req)
	select {
	case m.wake <- struct{}{}:
	default: // The worker is already signaled and takes every pending request.
	}
	select {
	case <-req.done:
		return req.err
	case <-m.ctx.Done():
		return m.ctx.Err()
	}
}

func (m *Manager) enqueue(req *request) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pending = append(m.pending, req)
}

func (m *Manager) run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-m.wake:
			m.process(ctx)
		}
	}
}

// process pushes for every pending request. Requests added during a push stay pending and
// get a push of their own, unless a retry of this batch takes them first.
func (m *Manager) process(ctx context.Context) {
	var batch []*request
	for {
		var snapshot provisioningv1.AgentStatus
		batch, snapshot = m.takeBatch(batch)
		if len(batch) == 0 {
			return
		}
		err := m.push(ctx, &snapshot)
		if err == nil {
			for _, req := range batch {
				req.finish(nil)
			}
			return
		}

		var retry []*request
		for _, req := range batch {
			if req.untilSuccess && !m.isDPUGone(err) {
				retry = append(retry, req)
			} else {
				req.finish(err)
			}
		}
		if len(retry) == 0 {
			return
		}
		klog.Warningf("Failed to update DPU status: %v", err)
		select {
		case <-ctx.Done():
			return
		case <-time.After(m.retryInterval):
		}
		batch = retry
	}
}

// takeBatch moves the pending requests into batch and copies the status under one lock, so
// the snapshot includes every UpdateLocal made before any request in the batch.
func (m *Manager) takeBatch(batch []*request) ([]*request, provisioningv1.AgentStatus) {
	m.mu.Lock()
	defer m.mu.Unlock()
	batch = append(batch, m.pending...)
	m.pending = nil
	return batch, *m.status.DeepCopy()
}

// isDPUGone reports whether err means the DPU was deleted or recreated, so retrying cannot
// succeed. Only checked when uid is set.
func (m *Manager) isDPUGone(err error) bool {
	return m.uid != "" && (apierrors.IsNotFound(err) || errors.Is(err, ErrStaleDPU))
}

// push reads the latest DPU, merges s into its AgentStatus, and patches the status.
func (m *Manager) push(ctx context.Context, s *provisioningv1.AgentStatus) error {
	latestDPU := &provisioningv1.DPU{}
	if err := m.client.Get(ctx, m.key, latestDPU); err != nil {
		return err
	}
	if m.uid != "" && string(latestDPU.UID) != m.uid {
		return fmt.Errorf("%w: expected UID %s but got %s", ErrStaleDPU, m.uid, latestDPU.UID)
	}
	patch := client.MergeFrom(latestDPU.DeepCopy())
	if latestDPU.Status.AgentStatus == nil {
		latestDPU.Status.AgentStatus = &provisioningv1.AgentStatus{
			Conditions: []metav1.Condition{},
		}
	}
	mergeAgentStatus(latestDPU.Status.AgentStatus, s)
	return m.client.Status().Patch(ctx, latestDPU, patch)
}

// mergeAgentStatus copies the fields set in s onto dst. A nil field leaves dst unchanged, so an
// agent that restarted with an empty status does not wipe what it reported before.
func mergeAgentStatus(dst, s *provisioningv1.AgentStatus) {
	if s.LastStartupTime != nil {
		dst.LastStartupTime = s.LastStartupTime
	}
	if s.InitialBootID != nil {
		dst.InitialBootID = s.InitialBootID
	}
	if s.RebootMethod != nil {
		dst.RebootMethod = s.RebootMethod
	}
	if s.RebootSequenceCount != nil {
		dst.RebootSequenceCount = s.RebootSequenceCount
	}
	if s.KubeletVersion != nil {
		dst.KubeletVersion = s.KubeletVersion
	}
	if s.TrustBundleHash != nil {
		dst.TrustBundleHash = s.TrustBundleHash
	}
	if s.TrustBundleLastUpdateTime != nil {
		dst.TrustBundleLastUpdateTime = s.TrustBundleLastUpdateTime
	}
	if s.LastObservedPendingNVConfig != nil {
		dst.LastObservedPendingNVConfig = s.LastObservedPendingNVConfig.DeepCopy()
	}
	// hostOSInit is always copied, nil included: it must read unset until ReleaseHostOSInit
	// records this boot's outcome, so a result left from an earlier run is cleared.
	dst.HostOSInit = s.HostOSInit.DeepCopy()
	if s.EWNICRuntimeConfig != nil {
		dst.EWNICRuntimeConfig = s.EWNICRuntimeConfig.DeepCopy()
	}
	for _, condition := range s.Conditions {
		meta.SetStatusCondition(&dst.Conditions, condition)
	}
	if s.PreInstall != nil {
		if dst.PreInstall == nil {
			dst.PreInstall = &provisioningv1.AgentPreInstallStatus{
				Conditions: []metav1.Condition{},
			}
		}
		if reported := s.PreInstall.AgentReported; reported != nil && !reported.IsZero() {
			dst.PreInstall.AgentReported = reported.DeepCopy()
		}
		for _, condition := range s.PreInstall.Conditions {
			meta.SetStatusCondition(&dst.PreInstall.Conditions, condition)
		}
	}
}
