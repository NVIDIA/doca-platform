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
	"errors"
	"fmt"
	"os"
	"os/signal"

	"github.com/nvidia/doca-platform/internal/cniprovisioner/utils/readyz"
	"github.com/nvidia/doca-platform/internal/ipallocator"

	"github.com/containernetworking/cni/libcni"
	kerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/klog/v2"
)

// requiredEnvVariables are environment variables that the program needs in order to run. Boolean indicates if this env
// variable should be configured by Kubernetes downward API (easier for user to understand the mistake).
var requiredEnvVariables = map[string]bool{
	"IP_ALLOCATOR_REQUESTS": true,
	"K8S_POD_NAME":          true,
	"K8S_POD_NAMESPACE":     true,
	"K8S_POD_UID":           true,
}

type Mode string

// String returns the string representation of the mode
func (m Mode) String() string {
	return string(m)
}

const (
	Allocator   Mode = "allocator"
	Deallocator Mode = "deallocator"
)

func main() {
	if len(os.Args) != 2 {
		klog.Fatal("expecting mode to be specified via args")
	}

	modeRaw := os.Args[1]
	mode, err := parseMode(modeRaw)
	if err != nil {
		klog.Fatalf("error while parsing mode: %s", err.Error())
	}

	klog.Info("Starting IP Allocator")
	env, err := parseEnv()
	if err != nil {
		klog.Fatalf("error while parsing environment: %s", err.Error())
	}

	allocator := ipallocator.New(
		libcni.NewCNIConfig([]string{ipallocator.CNIBinDir}, nil),
		env["K8S_POD_NAME"],
		env["K8S_POD_NAMESPACE"],
		env["K8S_POD_UID"],
	)

	reqs, err := allocator.ParseRequests(env["IP_ALLOCATOR_REQUESTS"])
	if err != nil {
		klog.Fatalf("error while parsing IP requests: %s", err.Error())
	}

	switch mode {
	case Allocator:
		if err := runInAllocatorMode(allocator, reqs); err != nil {
			klog.Fatal(err)
		}
	case Deallocator:
		if err := runInDeallocatorMode(allocator, reqs); err != nil {
			klog.Fatal(err)
		}
	}
}

// parseMode parses the mode in which the binary should be started
func parseMode(mode string) (Mode, error) {
	m := map[Mode]struct{}{
		Allocator:   {},
		Deallocator: {},
	}
	modeTyped := Mode(mode)
	if _, ok := m[modeTyped]; !ok {
		return "", errors.New("unknown mode")
	}

	return modeTyped, nil
}

// runInAllocatorMode runs the allocator in Allocator mode
func runInAllocatorMode(a *ipallocator.NVIPAMIPAllocator, reqs []ipallocator.NVIPAMIPAllocatorRequest) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	for _, req := range reqs {
		if err := a.Allocate(ctx, req); err != nil {
			return err
		}
	}

	if err := readyz.ReportReady(); err != nil {
		return err
	}

	klog.Info("IP allocation is done")

	ch := make(chan os.Signal, 1)
	signal.Notify(ch, os.Interrupt)
	<-ch
	klog.Info("Received termination signal, terminating.")

	return nil
}

// runInDeallocatorMode runs the allocator in Deallocator mode
func runInDeallocatorMode(a *ipallocator.NVIPAMIPAllocator, reqs []ipallocator.NVIPAMIPAllocatorRequest) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	for _, req := range reqs {
		if err := a.Deallocate(ctx, req); err != nil {
			return err
		}
	}

	klog.Info("IP deallocation is done")
	return nil
}

// parseEnv parses the required environment variables
func parseEnv() (map[string]string, error) {
	var errs []error
	m := make(map[string]string)
	for key, expectedViaDownwardAPI := range requiredEnvVariables {
		value := os.Getenv(key)
		if value == "" {
			err := fmt.Errorf("required %s environment variable is not found", key)
			if expectedViaDownwardAPI {
				err = errors.Join(err, errors.New("this is supposed to be configured via Kubernetes Downward API in production"))
			}
			errs = append(errs, err)
		}
		m[key] = value
	}

	return m, kerrors.NewAggregate(errs)
}
