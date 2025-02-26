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

package main

import (
	"context"
	"os"
	"os/signal"
	"sync"

	"github.com/nvidia/doca-platform/internal/ovshelper"
	"github.com/nvidia/doca-platform/internal/utils/ovsclient"

	"k8s.io/klog/v2"
	"k8s.io/utils/clock"
	kexec "k8s.io/utils/exec"
)

func main() {
	klog.Info("Starting OVS Helper")

	ctx, cancel := context.WithCancel(context.Background())
	c := clock.RealClock{}
	ovsClient, err := ovsclient.New(kexec.New())
	if err != nil {
		klog.Fatal(err)
	}

	handler := ovshelper.New(c, ovsClient)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		handler.Start(ctx)
	}()

	klog.Info("OVS Helper has started.")

	ch := make(chan os.Signal, 1)
	signal.Notify(ch, os.Interrupt)
	<-ch
	klog.Info("Received termination signal, terminating.")
	cancel()
	wg.Wait()
}
