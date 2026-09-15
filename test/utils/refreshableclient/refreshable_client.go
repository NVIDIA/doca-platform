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

package refreshableclient

import (
	"context"
	"fmt"
	"sync"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
)

// Client is a wrapper around a controller-runtime client that allows for refreshing the internal client.
// This is useful for e2e tests where the client needs to be refreshed after a change in the cluster
// because e.g. the tunnel needs to be recreated.
type Client struct {
	mu     sync.RWMutex
	client ctrlclient.Client
}

func New() *Client {
	return &Client{}
}

func (c *Client) Set(client ctrlclient.Client) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.client = client
}

func (c *Client) current() (ctrlclient.Client, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.client == nil {
		return nil, fmt.Errorf("refreshable client is not initialized")
	}
	return c.client, nil
}

func (c *Client) mustCurrent(method string) ctrlclient.Client {
	current, err := c.current()
	if err != nil {
		panic(fmt.Sprintf("refreshable client: %s called before Set(): %v", method, err))
	}
	return current
}

func (c *Client) Get(ctx context.Context, key ctrlclient.ObjectKey, obj ctrlclient.Object, opts ...ctrlclient.GetOption) error {
	current, err := c.current()
	if err != nil {
		return err
	}
	return current.Get(ctx, key, obj, opts...)
}

func (c *Client) List(ctx context.Context, list ctrlclient.ObjectList, opts ...ctrlclient.ListOption) error {
	current, err := c.current()
	if err != nil {
		return err
	}
	return current.List(ctx, list, opts...)
}

func (c *Client) Apply(ctx context.Context, obj runtime.ApplyConfiguration, opts ...ctrlclient.ApplyOption) error {
	current, err := c.current()
	if err != nil {
		return err
	}
	return current.Apply(ctx, obj, opts...)
}

func (c *Client) Create(ctx context.Context, obj ctrlclient.Object, opts ...ctrlclient.CreateOption) error {
	current, err := c.current()
	if err != nil {
		return err
	}
	return current.Create(ctx, obj, opts...)
}

func (c *Client) Delete(ctx context.Context, obj ctrlclient.Object, opts ...ctrlclient.DeleteOption) error {
	current, err := c.current()
	if err != nil {
		return err
	}
	return current.Delete(ctx, obj, opts...)
}

func (c *Client) Update(ctx context.Context, obj ctrlclient.Object, opts ...ctrlclient.UpdateOption) error {
	current, err := c.current()
	if err != nil {
		return err
	}
	return current.Update(ctx, obj, opts...)
}

func (c *Client) Patch(ctx context.Context, obj ctrlclient.Object, patch ctrlclient.Patch, opts ...ctrlclient.PatchOption) error {
	current, err := c.current()
	if err != nil {
		return err
	}
	return current.Patch(ctx, obj, patch, opts...)
}

func (c *Client) DeleteAllOf(ctx context.Context, obj ctrlclient.Object, opts ...ctrlclient.DeleteAllOfOption) error {
	current, err := c.current()
	if err != nil {
		return err
	}
	return current.DeleteAllOf(ctx, obj, opts...)
}

func (c *Client) Status() ctrlclient.SubResourceWriter {
	return c.mustCurrent("Status()").Status()
}

func (c *Client) SubResource(subResource string) ctrlclient.SubResourceClient {
	return c.mustCurrent("SubResource()").SubResource(subResource)
}

func (c *Client) Scheme() *runtime.Scheme {
	return c.mustCurrent("Scheme()").Scheme()
}

func (c *Client) RESTMapper() meta.RESTMapper {
	return c.mustCurrent("RESTMapper()").RESTMapper()
}

func (c *Client) GroupVersionKindFor(obj runtime.Object) (schema.GroupVersionKind, error) {
	current, err := c.current()
	if err != nil {
		return schema.GroupVersionKind{}, err
	}
	return current.GroupVersionKindFor(obj)
}

func (c *Client) IsObjectNamespaced(obj runtime.Object) (bool, error) {
	current, err := c.current()
	if err != nil {
		return false, err
	}
	return current.IsObjectNamespaced(obj)
}

var _ ctrlclient.Client = &Client{}
