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

package informer

import (
	"sync"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/dynamic/dynamicinformer"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
)

const MaxEvents = 100

type TestInformer struct {
	informer cache.SharedIndexInformer

	Wg           sync.WaitGroup
	UpdateEvents chan Event
	DeleteEvents chan *unstructured.Unstructured
	StopCh       chan struct{}
}

type Event struct {
	OldObj *unstructured.Unstructured
	NewObj *unstructured.Unstructured
}

// NewInformer returns a new informer struct to test reconciliation transitions in our unit tests.
func NewInformer(cfg *rest.Config, gkv schema.GroupVersionKind, namespace, resourceName string) *TestInformer {
	GinkgoHelper()

	i := &TestInformer{}

	i.StopCh = make(chan struct{})
	i.UpdateEvents = make(chan Event, 100)
	i.DeleteEvents = make(chan *unstructured.Unstructured, 100)
	dc, err := dynamic.NewForConfig(cfg)
	Expect(err).ToNot(HaveOccurred())
	i.informer = dynamicinformer.
		NewFilteredDynamicSharedInformerFactory(dc, 0, namespace, nil).
		ForResource(schema.GroupVersionResource{
			Group:    gkv.Group,
			Version:  gkv.Version,
			Resource: resourceName,
		}).Informer()

	handlers := cache.ResourceEventHandlerFuncs{
		UpdateFunc: func(oldObj, obj interface{}) {
			ou := oldObj.(*unstructured.Unstructured)
			nu := obj.(*unstructured.Unstructured)
			i.UpdateEvents <- Event{
				OldObj: ou,
				NewObj: nu,
			}
		},
		DeleteFunc: func(obj interface{}) {
			o := obj.(*unstructured.Unstructured)
			i.DeleteEvents <- o
		},
	}
	_, err = i.informer.AddEventHandler(handlers)
	Expect(err).ToNot(HaveOccurred())
	i.Wg.Add(1)

	return i
}

func (i *TestInformer) Run() {
	i.informer.Run(i.StopCh)
	defer i.Wg.Done()
}

func (i *TestInformer) Cleanup() {
	close(i.StopCh)
	i.Wg.Wait()
	close(i.UpdateEvents)
	close(i.DeleteEvents)
}
