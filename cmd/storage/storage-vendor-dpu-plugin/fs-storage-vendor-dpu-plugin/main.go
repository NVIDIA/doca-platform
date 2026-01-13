/*
Copyright 2025 NVIDIA

  Licensed under the Apache License, Version 2.0 (the License);
  you may not use this file except in compliance with the License.
  You may obtain a copy of the License at

      http://www.apache.org/licenses/LICENSE-2.0

  Unless required by applicable law or agreed to in writing, software
  distributed under the License is distributed on an AS IS BASIS,
  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
  See the License for the specific language governing permissions and
  limitations under the License.
*/

package main

import (
	"log"

	storagevendor "github.com/nvidia/doca-platform/internal/storage/snap/storage-vendor-dpu-plugin/fs-storage-vendor-dpu-plugin"

	ctrl "sigs.k8s.io/controller-runtime"
)

func main() {
	server, listener, err := storagevendor.CreateGRPCServer("", "", "")
	if err != nil {
		log.Fatalf("Error starting gRPC server: %v", err)
	}

	// Start the server in a separate goroutine
	go func() {
		log.Println("AIO Filesystem gRPC server starting to serve.")
		if serveErr := server.Serve(listener); serveErr != nil {
			log.Printf("AIO Filesystem gRPC server encountered error: %v\n", serveErr)
		}
	}()

	log.Println("AIO Filesystem gRPC server is running. Press Ctrl+C to stop.")

	// Use controller-runtime's signal handler to get a context that will be canceled
	// when a SIGINT or SIGTERM is caught.
	ctx := ctrl.SetupSignalHandler()

	// Block until the context is canceled by a signal
	<-ctx.Done()

	log.Println("Received shutdown signal. Stopping AIO Filesystem gRPC server gracefully...")

	// Gracefully stop the server, allowing in-flight RPCs to complete.
	server.GracefulStop()

	// Close the listener
	if closeErr := listener.Close(); closeErr != nil {
		log.Printf("Error closing listener: %v", closeErr)
	}

	log.Println("AIO Filesystem gRPC server stopped.")
}
