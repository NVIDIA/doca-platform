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

package dummydpuservice

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"sync/atomic"
	"time"
)

const (
	envNodeName     = "NODE_NAME"
	envNodeIP       = "NODE_IP"
	envPodName      = "POD_NAME"
	envPodNamespace = "POD_NAMESPACE"
	envPodIP        = "POD_IP"
)

// PodInfo contains information about the Pod
type PodInfo struct {
	NodeName     string `json:"nodeName"`
	NodeIP       string `json:"nodeIP"`
	PodName      string `json:"podName"`
	PodNamespace string `json:"podNamespace"`
	PodIP        string `json:"podIP"`
}

// GetStaticPodInfo reads information about the Pod from the env variables
func GetStaticPodInfo() PodInfo {
	return PodInfo{
		NodeName:     os.Getenv(envNodeName),
		NodeIP:       os.Getenv(envNodeIP),
		PodName:      os.Getenv(envPodName),
		PodNamespace: os.Getenv(envPodNamespace),
		PodIP:        os.Getenv(envPodIP),
	}
}

var counter uint64

type respData struct {
	PodInfo
	ClientIP  string `json:"clientIP"`
	CallCount uint64 `json:"callCount"`
}

func marshalRespData(podInfo PodInfo, clientIP string) ([]byte, error) {
	return json.Marshal(respData{
		PodInfo:   podInfo,
		ClientIP:  clientIP,
		CallCount: counter})
}

// NewHTTPHandler returns default HTTP handler implementation for dummy service
func NewHTTPHandler(podInfo PodInfo) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		log.Printf("request from: %s, url %s", r.RemoteAddr, r.URL.String())
		atomic.AddUint64(&counter, 1)
		data, err := marshalRespData(podInfo, r.RemoteAddr)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(err.Error()))
		}
		if _, err := w.Write(append(data, []byte("\n")...)); err != nil {
			log.Printf("failed to write response: %v", err)
		}
	}
}

// NewEventsHandler returns default handler for server-side events
func NewEventsHandler(podInfo PodInfo) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		log.Printf("request from: %s, url %s", r.RemoteAddr, r.URL.String())
		atomic.AddUint64(&counter, 1)

		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Expose-Headers", "Content-Type")
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")

		for i := uint64(0); ; i++ {
			if _, err := fmt.Fprintf(w, "%s; event: %d\n", time.Now().Local().String(), i); err != nil {
				log.Printf("failed to write event: %v", err)
			}
			w.(http.Flusher).Flush()
			select {
			case <-r.Context().Done():
				log.Printf("connection for %s closed", r.RemoteAddr)
				return
			case <-time.After(time.Second):
			}
		}
	}
}
