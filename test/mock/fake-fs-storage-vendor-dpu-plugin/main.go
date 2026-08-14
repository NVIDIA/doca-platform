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

// Binary fake-fs-storage-vendor-dpu-plugin is a test-only stand-in for fs-storage-vendor-dpu-plugin, the
// vendor plugin that runs on the DPU and turns a DPUVolume into a filesystem device SNAP can emulate.
//
// The real plugin backs each device with an NFS export from a storage vendor appliance, which does not fit
// an e2e lane. This one backs it with a local directory on the DPU instead: CreateDevice creates
// <volume directory>/<device name> and DeleteDevice removes it. Everything around that is the real plugin —
// the same package supplies the SNAP JSON-RPC client, the device naming and the socket paths, and the
// fsdev_aio_create/delete calls are the real ones, so the host still gets a genuine emulated VirtioFS device
// and the suite exercises the true SNAP path end to end.
//
// It listens on the same socket and ships under the same binary name as the real plugin, so the vendor Helm
// chart deploys it unmodified; the SNAP e2e suite only overrides the image with FAKE_FS_STORAGE_IMAGE.
package main

import (
	"log"

	ctrl "sigs.k8s.io/controller-runtime"
)

func main() {
	server, listener, err := createGRPCServer("", "", "")
	if err != nil {
		log.Fatalf("Error starting fake gRPC server: %v", err)
	}

	serveErrCh := make(chan error, 1)
	go func() {
		log.Println("Fake AIO Filesystem gRPC server starting to serve.")
		serveErrCh <- server.Serve(listener)
	}()

	log.Println("Fake AIO Filesystem gRPC server is running. Press Ctrl+C to stop.")

	ctx := ctrl.SetupSignalHandler()
	select {
	case <-ctx.Done():
	case serveErr := <-serveErrCh:
		log.Fatalf("Fake AIO Filesystem gRPC server stopped unexpectedly: %v", serveErr)
	}

	log.Println("Shutting down fake AIO Filesystem gRPC server...")
	server.GracefulStop()

	if closeErr := listener.Close(); closeErr != nil {
		log.Printf("Error closing listener: %v", closeErr)
	}

	log.Println("Fake AIO Filesystem gRPC server stopped.")
}
