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
	"log"
	"net/http"
	"os"

	"github.com/nvidia/doca-platform/internal/dummydpuservice"
)

const (
	defaultHTTPPort = "8080"
	envHTTPPort     = "HTTP_PORT"
)

func main() {
	podInfo := dummydpuservice.GetStaticPodInfo()

	log.Printf("%+v\n", podInfo)

	http.HandleFunc("/", dummydpuservice.NewHTTPHandler(podInfo))
	http.HandleFunc("/ping", dummydpuservice.NewEventsHandler(podInfo))

	httpPort := os.Getenv(envHTTPPort)
	if httpPort == "" {
		httpPort = defaultHTTPPort
	}
	log.Printf("listen on port %s", httpPort)
	log.Fatal(http.ListenAndServe(":"+httpPort, nil))
}
