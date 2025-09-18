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

package hostcontroller

import (
	"context"
	"fmt"
	"sync"

	storagev1 "github.com/nvidia/doca-platform/api/storage/v1alpha1"
	"github.com/nvidia/doca-platform/internal/storage/snap/host-controller/dpuclusterhelper"
	"github.com/nvidia/doca-platform/internal/storage/snap/host-controller/utils"
	"github.com/nvidia/doca-platform/pkg/dpucluster"

	corev1 "k8s.io/api/core/v1"
	eventv1 "k8s.io/api/events/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"
	ctrl "sigs.k8s.io/controller-runtime"
	ctrlbuilder "sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrlcontroller "sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch
// +kubebuilder:rbac:groups=storage.dpu.nvidia.com,resources=dpuvolumes,verbs=get;list;watch

const (
	eventControllerName = "eventcontroller"
)

// EventReconciler reconciles Event objects in dpu clusters and redistributes them to DPUVolume objects
type EventReconciler struct {
	client.Client
	Scheme      *runtime.Scheme
	RemoteCache *dpucluster.RemoteCache
	Recorder    record.EventRecorder
	Options     Options

	controller       ctrlcontroller.TypedController[RequestWithCluster]
	dpuClusterHelper dpuclusterhelper.DPUClusterHelper
	ownedByHelper    utils.OwnedByHelper
	processedEvents  *processedEvents
}

// RequestWithCluster is a request with a cluster key to identify the cluster that originated the request
type RequestWithCluster struct {
	ctrl.Request
	cluster client.ObjectKey
}

// Reconcile processes Event objects and redistributes them to DPUVolume objects when appropriate
func (r *EventReconciler) Reconcile(ctx context.Context, req RequestWithCluster) (ctrl.Result, error) {
	reqLog := ctrllog.FromContext(ctx)
	// check if dpu cluster is still exist to avoid reconciliation of events that have no chance to succeed
	dpuCluster, err := r.dpuClusterHelper.GetDPUCluster(ctx, req.cluster)
	if err != nil {
		if apierrors.IsNotFound(err) {
			reqLog.Info("DPU cluster not found, skipping", "cluster", req.cluster.String())
			r.processedEvents.Remove(req.cluster, req.NamespacedName)
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	dpuclient, err := r.dpuClusterHelper.GetClient(ctx, dpuCluster)
	if err != nil {
		return ctrl.Result{}, err
	}

	// get event object
	event := &eventv1.Event{}
	if err := dpuclient.Client.Get(ctx, req.NamespacedName, event); err != nil {
		if apierrors.IsNotFound(err) {
			// Return early if the event is not found.
			r.processedEvents.Remove(req.cluster, req.NamespacedName)
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	if r.processedEvents.Contains(req.cluster, req.NamespacedName) {
		// the event has been already processed successfully, no need to reconcile again
		return ctrl.Result{}, nil
	}

	result, err := r.reconcileEvent(ctx, event, dpuclient)
	if err != nil {
		return ctrl.Result{}, err
	}

	// add the event to the processed events map to avoid processing it again during resync
	r.processedEvents.Add(req.cluster, req.NamespacedName)

	return result, nil
}

// reconcileEvent processes the event and redistributes it based on the involved object type
func (r *EventReconciler) reconcileEvent(ctx context.Context, event *eventv1.Event, dpuclient dpuclusterhelper.ClientForDPUCluster) (ctrl.Result, error) {
	reqLog := ctrllog.FromContext(ctx)

	// object specific logic
	if isPVCEvent(event) {
		return r.handlePVCEvent(ctx, event, dpuclient)
	}

	reqLog.Info("Event does not match handled resource types, skipping",
		"eventKind", event.Regarding.Kind,
		"eventName", event.Name)

	return ctrl.Result{}, nil
}

// handlePVCEvent processes events related to PVCs by finding the associated DPUVolume and redistributing the event
func (r *EventReconciler) handlePVCEvent(ctx context.Context, event *eventv1.Event, dpuclient dpuclusterhelper.ClientForDPUCluster) (ctrl.Result, error) {
	reqLog := ctrllog.FromContext(ctx)

	// Get the PVC object from the involved object reference
	pvc := &corev1.PersistentVolumeClaim{}
	pvcKey := types.NamespacedName{
		Namespace: event.Regarding.Namespace,
		Name:      event.Regarding.Name,
	}
	dpuClusterKey := client.ObjectKeyFromObject(dpuclient.DPUCluster)

	if err := dpuclient.Client.Get(ctx, pvcKey, pvc); err != nil {
		if apierrors.IsNotFound(err) {
			reqLog.Info("PVC referenced in event not found in DPU cluster, skipping", "pvc", pvcKey, "cluster", dpuClusterKey)
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, fmt.Errorf("failed to get PVC %s: %w", pvcKey.String(), err)
	}
	// Resolve reference to DPUVolume using ownerref lib
	dpuVolumeRef, err := r.ownedByHelper.GetOwnedBy(pvc)
	if err != nil {
		reqLog.Info("PVC does not have DPUVolume owner reference, skipping event redistribution",
			"pvc", pvcKey, "cluster", dpuClusterKey, "error", err.Error())
		return ctrl.Result{}, nil
	}

	// Get the DPUVolume object from the host cluster
	dpuVolume := &storagev1.DPUVolume{}
	if err := r.Client.Get(ctx, dpuVolumeRef, dpuVolume); err != nil {
		if apierrors.IsNotFound(err) {
			reqLog.Info("DPUVolume referenced by PVC not found, skipping", "dpuVolume", dpuVolumeRef.String())
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, fmt.Errorf("failed to get DPUVolume %s: %w", dpuVolumeRef.String(), err)
	}

	// Redistribute original event to DPUVolume object with modified message
	originalMessage := event.Note
	modifiedMessage := fmt.Sprintf("Event from PVC %s in cluster %s: %s",
		client.ObjectKeyFromObject(pvc).String(), dpuClusterKey.String(), originalMessage)

	reqLog.Info("Redistributing event to DPUVolume",
		"originalEvent", client.ObjectKeyFromObject(event),
		"targetDPUVolume", dpuVolumeRef,
		"cluster", dpuClusterKey)

	// Create event for DPUVolume
	r.Recorder.Event(dpuVolume, event.Type, event.Reason, modifiedMessage)

	return ctrl.Result{}, nil
}

// WatchDPUClusterEvent is a callback function to register event watches when a DPU cluster is created/updated
func (r *EventReconciler) WatchDPUClusterEvent(_ context.Context, _ client.Client, cluster client.ObjectKey) (dpucluster.Watcher, error) {
	return dpucluster.NewWatcher(
		dpucluster.TypedWatcherOptions[client.Object, RequestWithCluster]{
			Name: "event-watch",
			Kind: &eventv1.Event{},
			EventHandler: handler.TypedEnqueueRequestsFromMapFunc(func(ctx context.Context, o client.Object) []RequestWithCluster {
				return []RequestWithCluster{
					{
						Request: ctrl.Request{
							NamespacedName: types.NamespacedName{
								Namespace: o.GetNamespace(),
								Name:      o.GetName(),
							},
						},
						cluster: cluster,
					},
				}
			}),
			Predicates: []predicate.Predicate{
				// Note: do not forget to update RemoteCache config in main.go to watch all needed events
				predicate.NewPredicateFuncs(func(obj client.Object) bool {
					return isPVCEvent(obj)
				}),
			},
			Watcher: r.controller,
		}), nil
}

// isPVCEvent checks if the given event is related to a PersistentVolumeClaim
func isPVCEvent(obj client.Object) bool {
	event, ok := obj.(*eventv1.Event)
	if !ok {
		return false
	}
	return event.Regarding.APIVersion == "v1" && event.Regarding.Kind == "PersistentVolumeClaim"
}

// noOpSource is a source that does nothing
type noOpSource struct{}

// Start is a no-op
func (n *noOpSource) Start(ctx context.Context, queue workqueue.TypedRateLimitingInterface[RequestWithCluster]) error {
	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *EventReconciler) SetupWithManager(mgr ctrl.Manager) error {
	r.processedEvents = &processedEvents{
		events: make(map[string]struct{}),
	}
	r.ownedByHelper = utils.New(dpuVolumeOwnedByAnnotation)
	r.dpuClusterHelper = dpuclusterhelper.New(mgr.GetClient(), r.RemoteCache)
	c, err := ctrlbuilder.TypedControllerManagedBy[RequestWithCluster](mgr).
		Named(eventControllerName).
		WatchesRawSource(&noOpSource{}).
		Build(r)
	r.controller = c
	return err
}

// processedEvents is a map of processed events
// it is used to avoid processing the same event multiple times
type processedEvents struct {
	sync.RWMutex
	events map[string]struct{}
}

// key returns a unique key for a given cluster and event
func (p *processedEvents) key(cluster, event client.ObjectKey) string {
	return fmt.Sprintf("%s:%s", cluster.String(), event.String())
}

// Add adds a new event to the map
func (p *processedEvents) Add(cluster, event client.ObjectKey) {
	p.Lock()
	defer p.Unlock()
	p.events[p.key(cluster, event)] = struct{}{}
}

// Contains checks if a given event has been processed
func (p *processedEvents) Contains(cluster, event client.ObjectKey) bool {
	p.RLock()
	defer p.RUnlock()
	_, ok := p.events[p.key(cluster, event)]
	return ok
}

// Remove removes a given event from the map
func (p *processedEvents) Remove(cluster, event client.ObjectKey) {
	p.Lock()
	defer p.Unlock()
	delete(p.events, p.key(cluster, event))
}
