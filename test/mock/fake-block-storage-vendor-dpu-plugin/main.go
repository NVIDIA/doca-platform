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

// Binary fake-block-storage-vendor-dpu-plugin provides local AIO block devices for SNAP e2e tests.
// It replaces the external SPDK target while preserving the real SNAP NVMe namespace, controller, and
// PCI hot-plug path.
package main

import (
	"log"

	ctrl "sigs.k8s.io/controller-runtime"
)

func main() {
	server, listener, err := createGRPCServer()
	if err != nil {
		log.Fatalf("Error starting fake block gRPC server: %v", err)
	}

	serveErrCh := make(chan error, 1)
	go func() {
		log.Println("Fake AIO block gRPC server starting to serve.")
		serveErrCh <- server.Serve(listener)
	}()

	ctx := ctrl.SetupSignalHandler()
	select {
	case <-ctx.Done():
	case serveErr := <-serveErrCh:
		log.Fatalf("Fake AIO block gRPC server stopped unexpectedly: %v", serveErr)
	}

	log.Println("Shutting down fake AIO block gRPC server...")
	server.GracefulStop()
	if closeErr := listener.Close(); closeErr != nil {
		log.Printf("Error closing listener: %v", closeErr)
	}
}
