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

package dpucluster

import (
	"context"
	"fmt"
	"net/http"
	"time"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
)

// This package is based on https://github.com/kubernetes-sigs/cluster-api/blob/v1.9.4/controllers/clustercache/
// It is a re-implementation of the clusterCache for the specific usage in DPF for dpu clusters.

// listWatchTimeout will only be used for List and Watch calls of informers
// because we don't want these requests to time out after the regular timeout
// of Options.Client.Timeout (default 10s).
// Lists of informers have no timeouts set.
// Watches of informers are timing out per default after [5m, 2*5m].
// https://github.com/kubernetes/client-go/blob/v0.32.0/tools/cache/reflector.go#L53-L55
// We are setting 11m to set a timeout for List calls without influencing Watch calls.
const listWatchTimeout = 11 * time.Minute

func (a *accessor) createConnection(ctx context.Context, opts *Options) (client.Client, *cacheWithCancel, *discovery.DiscoveryClient, error) {
	dpucluster := &provisioningv1.DPUCluster{}
	if err := opts.hostClient.Get(ctx, a.cluster, dpucluster); err != nil {
		return nil, nil, nil, fmt.Errorf("error getting DPUCluster: %w", err)
	}
	restConfig, err := NewConfig(opts.hostClient, dpucluster).restConfig(ctx,
		ClientOptionUserAgent{UserAgent: opts.userAgent},
		ClientOptionTimeout{Timeout: opts.timeout},
	)
	if err != nil {
		return nil, nil, nil, err
	}

	httpClient, mapper, err := createHTTPClientAndMapper(restConfig)
	if err != nil {
		return nil, nil, nil, err
	}

	cachedClient, cache, err := createCachedClient(ctx, restConfig, httpClient, mapper, opts)
	if err != nil {
		return nil, nil, nil, err
	}

	// create discovery client to check if the dpu cluster is healthy
	discoveryClient, err := discovery.NewDiscoveryClientForConfig(restConfig)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("error creating discovery client: %w", err)
	}

	return cachedClient, cache, discoveryClient, nil
}

// createHTTPClientAndMapper creates a http client and a dynamic REST mapper for the given cluster, based on the rest.Config.
func createHTTPClientAndMapper(config *rest.Config) (*http.Client, meta.RESTMapper, error) {
	httpClient, err := rest.HTTPClientFor(config)
	if err != nil {
		return nil, nil, fmt.Errorf("error creating HTTP client: %w", err)
	}

	// Create a dynamic REST mapper for the dpu cluster.
	mapper, err := apiutil.NewDynamicRESTMapper(config, httpClient)
	if err != nil {
		return nil, nil, fmt.Errorf("error creating dynamic REST mapper: %w", err)
	}

	// Verify if we can get a REST mapping from the dpu cluster apiserver.
	_, err = mapper.RESTMapping(corev1.SchemeGroupVersion.WithKind("Node").GroupKind(), corev1.SchemeGroupVersion.Version)
	if err != nil {
		return nil, nil, fmt.Errorf("error getting REST mapping: %w", err)
	}

	return httpClient, mapper, nil
}

// createCachedClient creates a cached client for the given dpu cluster, based on the rest.Config.
func createCachedClient(ctx context.Context, config *rest.Config, httpClient *http.Client, mapper meta.RESTMapper, opts *Options) (client.Client, *cacheWithCancel, error) {
	configWithTimeout := rest.CopyConfig(config)
	configWithTimeout.Timeout = listWatchTimeout
	httpClientWithTimeout, err := rest.HTTPClientFor(configWithTimeout)
	if err != nil {
		return nil, nil, fmt.Errorf("error creating HTTP client with %s timeout: %w", listWatchTimeout, err)
	}

	// Create the cache for the dpu cluster.
	cacheOptions := cache.Options{
		HTTPClient:           httpClientWithTimeout,
		Scheme:               opts.scheme,
		Mapper:               mapper,
		SyncPeriod:           opts.syncPeriod,
		DefaultNamespaces:    opts.defaultNamespaces,
		DefaultLabelSelector: opts.defaultLabelSelector,
		DefaultFieldSelector: opts.defaultFieldSelector,
		ByObject:             opts.byObject,
	}
	remoteCache, err := cache.New(configWithTimeout, cacheOptions)
	if err != nil {
		return nil, nil, fmt.Errorf("error creating cache: %w", err)
	}

	// Create the client for the cluster.
	cachedClient, err := client.New(config, client.Options{
		Scheme:     opts.scheme,
		Mapper:     mapper,
		HTTPClient: httpClient,
		Cache: &client.CacheOptions{
			Reader:       remoteCache,
			Unstructured: true,
			DisableFor:   opts.disableFor,
		},
	})
	if err != nil {
		return nil, nil, fmt.Errorf("error creating client: %w", err)
	}

	// set up a context for the cache than can when disconnecting
	// but also can cancel when the parent context is canceled
	cacheWithCancel := newCacheWithCancel(ctx, remoteCache)
	// Start the cache!
	cacheWithCancel.start()

	// Wait until the cache is initially synced.
	cacheSyncCtx, cacheSyncCtxCancel := context.WithTimeout(ctx, opts.initialSyncTimeout)
	defer cacheSyncCtxCancel()
	if !remoteCache.WaitForCacheSync(cacheSyncCtx) {
		cacheWithCancel.cancel()
		return nil, nil, fmt.Errorf("error when waiting for cache to sync: %w", cacheSyncCtx.Err())
	}

	// Wrap the cached client with a client that sets timeouts on all Get and List calls
	// If we don't set timeouts here Get and List calls can get stuck if they lazily create a new informer
	// and the informer than doesn't sync because the dpu cluster apiserver is not reachable.
	cachedClient = newClientWithTimeout(cachedClient, config.Timeout)

	return cachedClient, cacheWithCancel, nil
}

func newCacheWithCancel(ctx context.Context, c cache.Cache) *cacheWithCancel {
	cacheCtx, cacheCtxCancel := context.WithCancel(ctx)
	return &cacheWithCancel{
		Cache:      c,
		ctx:        cacheCtx,
		cancelFunc: cacheCtxCancel,
	}
}

type cacheWithCancel struct {
	cache.Cache
	ctx context.Context
	// cancelFunc cancels the context of the cache.
	cancelFunc context.CancelFunc
}

func (c cacheWithCancel) start() {
	go c.Cache.Start(c.ctx) // nolint:errcheck
}

func (c cacheWithCancel) cancel() {
	c.cancelFunc()
}

func newClientWithTimeout(client client.Client, timeout time.Duration) client.Client {
	return clientWithTimeout{
		Client:  client,
		timeout: timeout,
	}
}

type clientWithTimeout struct {
	client.Client
	timeout time.Duration
}

var _ client.Client = &clientWithTimeout{}

func (c clientWithTimeout) Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	return c.Client.Get(ctx, key, obj, opts...)
}

func (c clientWithTimeout) List(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	return c.Client.List(ctx, list, opts...)
}
