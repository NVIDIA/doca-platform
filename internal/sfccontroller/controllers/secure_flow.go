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

package controller

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"time"

	"antrea.io/antrea/pkg/ovs/openflow"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

const (
	checkInterval  = 5 * time.Second // Interval between connection checks
	retryInterval  = 1 * time.Second // Initial retry interval
	connectTimeOut = 1 * time.Second // server connection timeout
)

// SecureConnection encapsulates configuration for monitoring server connection
type SecureConnection struct {
	mgr                       manager.Manager
	OFBridge                  openflow.Bridge
	secureFlowDeletionTimeout time.Duration

	OnReconnected func() error
}

// NewSecureConnection creates a new SecureConnection
func NewSecureConnection(mgr manager.Manager, bridge openflow.Bridge, timeout time.Duration) (*SecureConnection, error) {
	sc := &SecureConnection{
		mgr:                       mgr,
		OFBridge:                  bridge,
		secureFlowDeletionTimeout: timeout,
	}
	return sc, nil

}

// Monitor starts a goroutine to periodically check the server connection status.
// If check fails, attempt to reconnect using exponential backoff.
// If it cannot re-establish a connection within the specified time, it flags and process disconnection event handler.
func (sc *SecureConnection) Monitor(ctx context.Context) {
	go func() {
		disconnected := false
		for {
			select {
			case <-ctx.Done():
				log.Println("[Monitor] Context canceled, stopping monitoring goroutine.")
				return
			case <-time.After(checkInterval):
				log.Println("[Monitor] Checking server connection...")
				disconnected = sc.handleConnectionCheck(sc.checkConnectionWithBackoff(ctx), disconnected)
			}
		}
	}()
}

func (sc *SecureConnection) handleConnectionCheck(connectionErr error, disconnected bool) bool {
	if connectionErr != nil {
		log.Printf("[Monitor] API server disconnection noticed: %v\n", connectionErr)
		if err := sc.processDisconnectEvent(); err != nil {
			log.Printf("[Monitor] Failed processing post disconnect: %v\n", err)
		}
		return true
	}
	if disconnected && sc.OnReconnected != nil {
		if err := sc.OnReconnected(); err != nil {
			log.Printf("[Monitor] Failed processing reconnect: %v\n", err)
			return true
		}
	}
	return false
}

// checkConnectionWithBackoff attempts to connect to the server and uses exponential backoff if the initial attempt fails.
func (sc *SecureConnection) checkConnectionWithBackoff(ctx context.Context) error {
	ctxWithTimeout, cancel := context.WithTimeout(ctx, sc.secureFlowDeletionTimeout)
	defer cancel()

	backoff := wait.Backoff{
		Duration: retryInterval,
		Factor:   1.5,
		Jitter:   0.1,
		Steps:    100,
		Cap:      sc.secureFlowDeletionTimeout,
	}

	onRetry := func(err error) bool {
		log.Println("[Monitor][Retry] Checking server connection...")
		return true
	}

	err := retry.OnError(backoff, onRetry, func() error {
		select {
		case <-ctxWithTimeout.Done():
			return fmt.Errorf("context canceled or deadline exceeded")
		default:
			return sc.checkKubernetesAPIServer()
		}
	})
	return err
}

func (sc *SecureConnection) checkKubernetesAPIServer() error {
	// Get the HTTP client and REST config from manager
	httpClient := sc.mgr.GetHTTPClient()
	restConfig := sc.mgr.GetConfig()

	// Construct the API server URL
	apiServerURL := restConfig.Host + "/livez"

	// Create a context with connect timeout
	ctx, cancel := context.WithTimeout(context.Background(), connectTimeOut)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", apiServerURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %v", err)
	}

	// Execute the request - will timeout after connect timeout seconds (1)
	resp, err := httpClient.Do(req)
	if err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			log.Printf("API server liveness check timed out")
			return fmt.Errorf("API server liveness check timed out")
		}
		return fmt.Errorf("failed to check API server liveness: %v", err)
	}
	defer func(Body io.ReadCloser) {
		_ = Body.Close()
	}(resp.Body)

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response body: %v", err)
	}

	log.Printf("API server liveness response: %s status: %d", string(body), resp.StatusCode)
	if resp.StatusCode != http.StatusOK {
		log.Printf("API server liveness returned non-200 status code: %d body: %s", resp.StatusCode, string(body))
		return fmt.Errorf("API server returned non-200 status: %d, body: %s", resp.StatusCode, string(body))
	}

	if string(body) != "ok" {
		return fmt.Errorf("API server not healthy, response: %s", string(body))
	}

	return nil
}

// processDisconnectEvent is called we notice a disconnect event from the server.
func (sc *SecureConnection) processDisconnectEvent() error {
	log.Println("[secure flow mode] removing existing flows due to disconnect event")

	err := sc.DeleteAllFlows()
	if err != nil {
		return fmt.Errorf("error deleting all flows: %w", err)
	}
	return nil
}

func (sc *SecureConnection) DeleteAllFlows() error {

	flowsStatusMap, err := sc.OFBridge.DumpFlows(0, 0)
	if err != nil {
		return fmt.Errorf("failed to get flow cookies: %w", err)
	}
	currentCookiesSet := sets.New[uint64]()
	for cookie := range flowsStatusMap {
		currentCookiesSet.Insert(cookie)
	}

	for flowCookie := range currentCookiesSet {
		log.Printf("remove cookie=%d/-1", flowCookie)
		flowErrors := sc.OFBridge.DeleteFlowsByCookie(flowCookie, math.MaxUint64)
		if flowErrors != nil {
			return fmt.Errorf("failed to delete flow cookie %d : %w", flowCookie, flowErrors)
		}
	}
	return nil
}
