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

package runner

import (
	"context"
	"sync"
	"sync/atomic"

	"k8s.io/klog/v2"
)

// Runnable is an interface expected by Runner from the service
type Runnable interface {
	// Run blocks until context is canceled or one of the services completes
	Run(ctx context.Context) error
	// Wait blocks until context is canceled or service is ready
	Wait(ctx context.Context) error
}

// Runner is an interface to register and run service as goroutine as a part of a single daemon
type Runner interface {
	Runnable
	// AddService add new service to the runner,
	// if service stops (with or without error) all other services managed by runner will be stopped
	AddService(serviceName string, r Runnable)
}

// New initialize and return new defaultRunner
func New() Runner {
	stopCtx, cFunc := context.WithCancel(context.Background())
	return &defaultRunner{
		stopCtx:    stopCtx,
		cancel:     cFunc,
		completeCH: make(chan completeResult),
		started:    make(chan struct{}),
	}
}

type completeResult struct {
	name string
	err  error
}

type waitFunc = func(ctx context.Context) error

// defaultRunner is a default implementation of the Runner interface
type defaultRunner struct {
	// used to wait for all dependent runnable to stop.
	wg sync.WaitGroup
	// channel to send results from the dependent runnable.
	// on error, the runner stops all dependencies and exit
	completeCH chan completeResult
	// used to notify all waiting dependencies that runner has started and they can run
	started chan struct{}
	// global stop context, indicates that the Runner should stop
	//nolint:containedctx
	stopCtx context.Context
	// the function cancels the global stop context of the runner
	cancel func()
	// mutex to protect waitFunc list
	waitFuncLck sync.Mutex
	// list of all registered wait functions.
	// runner's Wait() blocks until all listed functions complete.
	waitFuncs []waitFunc
}

// AddService is a Runner interface implementation for defaultRunner
func (r *defaultRunner) AddService(serviceName string, srv Runnable) {
	r.addService(serviceName, srv)
}

// Wait blocks until context is canceled or all dependencies(which were registered before Wait call)
// in runner are ready
func (r *defaultRunner) Wait(ctx context.Context) error {
	funcsToWait := r.getWaitFuncs()

	wg := sync.WaitGroup{}
	defer wg.Wait()

	innerCtx, innerCFunc := context.WithCancel(ctx)
	defer innerCFunc()

	wg.Add(1)
	// spawn goroutine which will cancel Wait in case if
	// runner.Run method exit
	go func() {
		defer wg.Done()
		select {
		// caller canceled context passed to Wait or
		// all wait functions are already complete, and now
		// we need to close this goroutine
		case <-innerCtx.Done():
		// runner shutdown
		case <-r.stopCtx.Done():
		}
		innerCFunc()
	}()

	for _, f := range funcsToWait {
		if err := f(innerCtx); err != nil {
			return err
		}
	}

	select {
	case <-innerCtx.Done():
		// context was canceled we should return context.Canceled error
		return context.Canceled
	default:
		return nil
	}
}

// Run blocks until context is canceled or one of the services completes
// Run will return only after all services in the runner are stopped
// If there are no services in the runner,
// Run will block until context is canceled.
// It is possible to call AddService before Run is called,
// in this case services will start only after Run called.
func (r *defaultRunner) Run(ctx context.Context) error {
	var err error
	close(r.started)
	klog.Info("Start services")
	func() {
		for {
			select {
			case <-ctx.Done():
				klog.Info("Shutdown signal received, waiting for all services to finish")
				return
			case result := <-r.completeCH:
				klog.V(2).InfoS("service finished, shutdown other services",
					"service", result.name)
				err = result.err
				return
			}
		}
	}()
	r.cancel()
	r.wg.Wait()
	klog.Info("All services finished")
	return err
}

func (r *defaultRunner) addWaitFunc(f waitFunc) {
	r.waitFuncLck.Lock()
	defer r.waitFuncLck.Unlock()
	r.waitFuncs = append(r.waitFuncs, f)
}

func (r *defaultRunner) getWaitFuncs() []waitFunc {
	r.waitFuncLck.Lock()
	defer r.waitFuncLck.Unlock()
	result := make([]waitFunc, len(r.waitFuncs))
	copy(result, r.waitFuncs)
	return result
}

func (r *defaultRunner) addService(serviceName string, srv Runnable) {
	r.wg.Add(1)
	r.addWaitFunc(srv.Wait)
	go func() {
		defer r.wg.Done()
		klog.V(2).InfoS("service created", "service", serviceName)
		var err error
		select {
		case <-r.started:
			klog.V(2).InfoS("start service", "service", serviceName)
			err = srv.Run(r.stopCtx)
			if err != nil {
				klog.ErrorS(err, "service return error", "service", serviceName)
			} else {
				klog.V(2).InfoS("service finished", "service", serviceName)
			}
		case <-r.stopCtx.Done():
			err = r.stopCtx.Err()
		}
		select {
		case r.completeCH <- completeResult{name: serviceName, err: err}:
		case <-r.stopCtx.Done(): // global shutdown, we don't need to submit completeResult
		}
	}()
}

var _ Runnable = &FakeRunnable{}

// FakeRunnableFunction represents a type definition for functions compatible with the FakeRunnable.
// The readyFunc parameter is a callback function that should be invoked to mark the runnable as ready.
type FakeRunnableFunction func(ctx context.Context, readyFunc func()) error

// NewFakeRunnable returns a new instance of FakeRunnable
func NewFakeRunnable() *FakeRunnable {
	return &FakeRunnable{readyCH: make(chan struct{})}
}

// FakeRunnable is a fake implementation of the Runnable interface
type FakeRunnable struct {
	runFunction FakeRunnableFunction
	readyCH     chan struct{}

	startedCondition atomic.Bool
	stoppedCondition atomic.Bool
	readyCondition   atomic.Bool
}

// SetFunction configure FakeRunnableFunction for FakeRunnable.
// The function can't be called after Runnable is started
func (s *FakeRunnable) SetFunction(f FakeRunnableFunction) {
	if s.startedCondition.Load() {
		panic("can't set function when Runnable is started")
	}
	s.runFunction = f
}

// Started returns true if started condition is set
func (s *FakeRunnable) Started() bool {
	return s.startedCondition.Load()
}

// Stopped returns true if stopped condition is set
func (s *FakeRunnable) Stopped() bool {
	return s.stoppedCondition.Load()
}

// Ready returns true if the ready condition is set.
// Note: This condition indicates that the runnable transitioned to a "ready" state at some point during execution.
// It will continue to return true even after the runnable completes, as long as it was marked as ready beforehand.
func (s *FakeRunnable) Ready() bool {
	return s.readyCondition.Load()
}

// Run implements Runnable interface
func (s *FakeRunnable) Run(ctx context.Context) error {
	s.startedCondition.Store(true)
	defer func() {
		s.stoppedCondition.Store(true)
	}()
	if s.runFunction != nil {
		return s.runFunction(ctx, func() { close(s.readyCH) })
	}
	// default behavior - marks runnable as ready and blocks until context is canceled
	close(s.readyCH)
	<-ctx.Done()
	return nil
}

// Wait implements Runnable interface
func (s *FakeRunnable) Wait(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return context.Canceled
	case <-s.readyCH:
		s.readyCondition.Store(true)
		return nil
	}
}
