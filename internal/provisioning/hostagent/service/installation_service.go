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

package service

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"time"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/internal/provisioning/hostagent/service/types"

	restful "github.com/emicklei/go-restful/v3"
	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	// Host management interface configuration
	// Using IPv6 link-local address for tmfifo interfaces
	// fe80::1 is used on the host side, fe80::2 on the DPU side
	hostMgmtIPv6 = "fe80::1"
	hostMgmtPort = "11029"
	// localhostAddr is the address for localhost listener
	localhostAddr = "localhost:" + hostMgmtPort

	// Prefix for tmfifo network interfaces
	tmfifoNetPrefix    = "tmfifo_net"
	localhostIfaceName = "localhost"

	// interfaceScanInterval is how often to scan for new tmfifo_net interfaces
	interfaceScanInterval = 10 * time.Second

	// debRepoDir contains dpu-agent .deb packages and APT metadata, baked into the image at build time.
	debRepoDir = "/deb"
	// rpmRepoDir contains dpu-agent .rpm packages and YUM metadata, baked into the image at build time.
	rpmRepoDir = "/rpm"
)

// NetworkConfigurator is an interface for triggering host network configuration.
// It is satisfied by networkmanager.NetworkManager.
type NetworkConfigurator interface {
	AddNetworkRequest(dpu *provisioningv1.DPU) error
}

type InstallationService struct {
	client.Client
	handler http.Handler
	// mu protects listeners
	mu sync.Mutex
	// listeners maps interface names to their listeners
	listeners      map[string]net.Listener
	networkManager NetworkConfigurator
}

func NewInstallationService(client client.Client, nm NetworkConfigurator) *InstallationService {
	s := &InstallationService{
		Client:         client,
		listeners:      make(map[string]net.Listener),
		networkManager: nm,
	}
	ws := new(restful.WebService).Path("/")
	ws.Route(
		ws.POST("/update-status").
			Consumes(restful.MIME_JSON).
			Produces(restful.MIME_JSON).
			To(s.UpdateStatus))
	ws.Route(
		ws.GET("/get-object").
			Param(ws.QueryParameter("group", "the API group of the object (empty for core API group)")).
			Param(ws.QueryParameter("version", "the API version of the object").Required(true)).
			Param(ws.QueryParameter("kind", "the kind of the object").Required(true)).
			Param(ws.QueryParameter("namespace", "the namespace of the object (empty for cluster-scoped objects)")).
			Param(ws.QueryParameter("name", "the name of the object").Required(true)).
			Produces(restful.MIME_JSON).
			To(s.GetObject))
	ws.Route(
		ws.POST("/configure-host-vfs").
			Consumes(restful.MIME_JSON).
			Produces(restful.MIME_JSON).
			To(s.ConfigureHostVFs))
	ws.Route(ws.GET("/healthz").To(s.HealthCheck))
	// Package repositories: serve .deb and .rpm packages for DPU provisioning.
	ws.Route(ws.GET("/deb/{subpath:*}").To(serveRepoFile(debRepoDir)))
	ws.Route(ws.GET("/rpm/{subpath:*}").To(serveRepoFile(rpmRepoDir)))
	container := restful.NewContainer()
	container.Add(ws)
	s.handler = container
	return s
}

// Start starts the InstallationService HTTP server.
//
// The server always listens on localhost:11029 for local access (e.g., debugging, health checks).
// When setupInterfaces is true, it also sets up IPv6 link-local addresses for each tmfifo_net
// interface and creates additional listeners bound to each interface.
//
// Network topology when setupInterfaces=true:
//
//	┌────────────────────────────────────────────────────────────────────────────────────────┐
//	│                                      HOST                                              │
//	│                                                                                        │
//	│                      ┌─────────────────────────────────────┐                           │
//	│                      │        InstallationService          │                           │
//	│                      │            (HTTP Server)            │                           │
//	│                      └──────────────────┬──────────────────┘                           │
//	│                                         │                                              │
//	│      ┌──────────────────────────────────┼──────────────────────────────────┐           │
//	│      │                                  │                                  │           │
//	│      ▼                                  ▼                                  ▼           │
//	│ ┌──────────┐                ┌─────────────────────┐            ┌─────────────────────┐ │
//	│ │localhost │                │     tmfifo_net0     │            │     tmfifo_net1     │ │
//	│ │ :11029   │                │  ┌───────────────┐  │            │  ┌───────────────┐  │ │
//	│ └──────────┘                │  │ listener      │  │            │  │ listener      │  │ │
//	│      │                      │  │ [fe80::1%     │  │            │  │ [fe80::1%     │  │ │
//	│      │                      │  │ tmfifo_net0]  │  │            │  │ tmfifo_net1]  │  │ │
//	│      │                      │  │ :11029        │  │            │  │ :11029        │  │ │
//	│      │                      │  └───────┬───────┘  │            │  └───────┬───────┘  │ │
//	│      │                      │          │          │            │          │          │ │
//	│      │                      │  ┌───────┴───────┐  │            │  ┌───────┴───────┐  │ │
//	│      ▼                      │  │  tmfifo_net0  │  │            │  │  tmfifo_net1  │  │ │
//	│  Local access               │  │  fe80::1/64   │  │            │  │  fe80::1/64   │  │ │
//	│  (curl, tests)              │  └───────┬───────┘  │            │  └───────┬───────┘  │ │
//	│                             └──────────┼──────────┘            └──────────┼──────────┘ │
//	└────────────────────────────────────────┼──────────────────────────────────┼────────────┘
//	                                         │        rshim                     │
//	┌────────────────────────────────────────┼──────────────────────────────────┼────────────┐
//	│                                        │                                  │            │
//	│  ┌─────────────────────────────────────┴─────┐  ┌─────────────────────────┴─────────┐  │
//	│  │               DPU 0                       │  │               DPU 1               │  │
//	│  │  ┌──────────────────────────────────────┐ │  │  ┌──────────────────────────────┐ │  │
//	│  │  │            tmfifo_net0               │ │  │  │         tmfifo_net0          │ │  │
//	│  │  │            fe80::2/64                │ │  │  │         fe80::2/64           │ │  │
//	│  │  └──────────────────────────────────────┘ │  │  └──────────────────────────────┘ │  │
//	│  │                                           │  │                                   │  │
//	│  │  DPU Agent connects to Host via           │  │  DPU Agent connects to Host via   │  │
//	│  │  [fe80::1%tmfifo_net0]:11029              │  │  [fe80::1%tmfifo_net0]:11029      │  │
//	│  └───────────────────────────────────────────┘  └───────────────────────────────────┘  │
//	│                                                                                        │
//	└────────────────────────────────────────────────────────────────────────────────────────┘
//
// Each tmfifo_net interface on the Host uses the same IPv6 link-local address (fe80::1/64).
// Since fe80 addresses are link-scoped (bound to the interface), they don't conflict.
// The server binds a listener to each interface using the scope syntax [fe80::1%iface]:port.
// Each DPU has its own tmfifo_net0 with fe80::2/64, connecting to the Host's fe80::1.
// This approach eliminates the need for VRF while maintaining proper isolation.
func (s *InstallationService) Start(setupInterfaces bool) error {
	// Start background goroutine to periodically scan and create listeners.
	// wait.Until runs immediately on start, then periodically.
	// This handles both localhost and tmfifo_net interfaces.
	go wait.Until(
		func() { s.scanAndCreateListeners(setupInterfaces) },
		interfaceScanInterval,
		wait.NeverStop,
	)

	if setupInterfaces {
		// Subscribe to netlink events before starting the goroutine
		updates := make(chan netlink.LinkUpdate)
		if err := netlink.LinkSubscribe(updates, wait.NeverStop); err != nil {
			return fmt.Errorf("failed to subscribe to netlink events: %w", err)
		}
		// Watch for interface deletions to close stale listeners
		go s.watchInterfaceDeletes(updates)
	}

	return nil
}

// watchInterfaceDeletes monitors netlink events for interface deletions.
// When an interface is deleted, it closes the corresponding listener so that
// scanAndCreateListeners can recreate it when the interface reappears.
func (s *InstallationService) watchInterfaceDeletes(updates <-chan netlink.LinkUpdate) {
	klog.Info("Started watching for interface deletion events")
	for update := range updates {
		// RTM_DELLINK indicates interface deletion
		if update.Header.Type != unix.RTM_DELLINK {
			continue
		}

		ifaceName := update.Attrs().Name
		if !strings.HasPrefix(ifaceName, tmfifoNetPrefix) {
			continue
		}

		s.mu.Lock()
		if listener, exists := s.listeners[ifaceName]; exists {
			klog.Infof("Interface %s deleted, closing listener", ifaceName)
			_ = listener.Close()
			delete(s.listeners, ifaceName)
		}
		s.mu.Unlock()
	}
}

// scanAndCreateListeners scans for interfaces and creates listeners for them.
// It handles both localhost (IPv4) and tmfifo_net (IPv6) interfaces.
func (s *InstallationService) scanAndCreateListeners(setupInterfaces bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Always check localhost listener (IPv4)
	if _, exists := s.listeners[localhostIfaceName]; !exists {
		listener, err := net.Listen("tcp", localhostAddr)
		if err != nil {
			klog.Warningf("Failed to create localhost listener: %v", err)
		} else {
			klog.Infof("Created listener on %s", localhostAddr)
			s.listeners[localhostIfaceName] = listener
			go s.serveInterface(listener, localhostIfaceName)
		}
	}

	if !setupInterfaces {
		return
	}

	// Scan for tmfifo_net interfaces (IPv6)
	ifaceNames, err := setupMgmtInterfaces()
	if err != nil {
		klog.Warningf("Failed to scan management interfaces: %v", err)
		return
	}

	for _, ifaceName := range ifaceNames {
		if _, exists := s.listeners[ifaceName]; exists {
			continue
		}

		listenAddr := fmt.Sprintf("[%s%%%s]:%s", hostMgmtIPv6, ifaceName, hostMgmtPort)
		klog.Infof("Discovered new interface %s, creating listener on %s", ifaceName, listenAddr)

		listener, err := listenTCP6WithRetry(listenAddr)
		if err != nil {
			klog.Warningf("Failed to create listener for interface %s: %v", ifaceName, err)
			continue
		}

		klog.Infof("Created listener on %s", listenAddr)
		s.listeners[ifaceName] = listener
		go s.serveInterface(listener, ifaceName)
	}
}

// Stop closes all listeners.
func (s *InstallationService) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, listener := range s.listeners {
		_ = listener.Close()
	}
}

// serveInterface runs http.Serve for an interface (localhost or tmfifo).
// On exit, it removes the interface from listeners so scanAndCreateListeners can recreate it.
func (s *InstallationService) serveInterface(listener net.Listener, ifaceName string) {
	klog.Infof("Serving interface %s, address %s", ifaceName, listener.Addr())
	err := http.Serve(listener, s.handler)
	klog.Warningf("Server on interface %s exited: %v", ifaceName, err)

	// Close the listener
	_ = listener.Close()

	// Remove from listeners so scanAndCreateListeners can recreate it
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.listeners, ifaceName)
}

func (s *InstallationService) HealthCheck(req *restful.Request, resp *restful.Response) {
	resp.WriteHeader(http.StatusOK)
}

func (s *InstallationService) ConfigureHostVFs(req *restful.Request, resp *restful.Response) {
	var request types.ConfigureHostVFsRequest
	if err := req.ReadEntity(&request); err != nil {
		klog.Errorf("failed to read configure host VF request: %v", err)
		_ = resp.WriteError(http.StatusBadRequest, err)
		return
	}
	klog.Infof("Received configure host VF request: %#v", request)

	if s.networkManager == nil {
		klog.Errorf("network manager is not configured")
		_ = resp.WriteError(http.StatusServiceUnavailable, fmt.Errorf("network manager is not configured"))
		return
	}

	dpu := &provisioningv1.DPU{}
	if err := s.Get(req.Request.Context(), client.ObjectKey{Namespace: request.DPUNamespace, Name: request.DPUName}, dpu); err != nil {
		klog.Errorf("failed to get DPU %s/%s: %v", request.DPUNamespace, request.DPUName, err)
		_ = resp.WriteError(http.StatusNotFound, err)
		return
	}

	if err := s.networkManager.AddNetworkRequest(dpu); err != nil {
		klog.Errorf("failed to add network request for DPU %s/%s: %v", request.DPUNamespace, request.DPUName, err)
		_ = resp.WriteError(http.StatusInternalServerError, err)
		return
	}

	klog.Infof("Successfully added network request for DPU %s/%s", request.DPUNamespace, request.DPUName)
	resp.WriteHeader(http.StatusOK)
}

func (s *InstallationService) UpdateStatus(req *restful.Request, resp *restful.Response) {
	var request types.UpdateStatusRequest
	if err := req.ReadEntity(&request); err != nil {
		klog.Errorf("failed to read update status request: %v", err)
		_ = resp.WriteError(http.StatusBadRequest, err)
		return
	}
	klog.Infof("Received update status request: %#v", request)

	dpu := &provisioningv1.DPU{}
	if err := s.Get(req.Request.Context(), client.ObjectKey{Namespace: request.DPUNamespace, Name: request.DPUName}, dpu); err != nil {
		klog.Errorf("failed to get DPU %s: %v", request.DPUName, err)
		_ = resp.WriteError(http.StatusNotFound, err)
		return
	}

	patch := client.MergeFrom(dpu.DeepCopy())
	if dpu.Status.AgentStatus == nil {
		dpu.Status.AgentStatus = &provisioningv1.AgentStatus{
			Conditions: []metav1.Condition{},
		}
	}
	if request.AgentStatus.LastStartupTime != nil {
		dpu.Status.AgentStatus.LastStartupTime = request.AgentStatus.LastStartupTime
	}
	if request.AgentStatus.HostRebootRequired != nil {
		dpu.Status.AgentStatus.HostRebootRequired = request.AgentStatus.HostRebootRequired
	}
	if request.AgentStatus.InitialBootID != nil {
		dpu.Status.AgentStatus.InitialBootID = request.AgentStatus.InitialBootID
	}
	if request.AgentStatus.RebootMethod != nil {
		dpu.Status.AgentStatus.RebootMethod = request.AgentStatus.RebootMethod
	}
	for _, condition := range request.AgentStatus.Conditions {
		meta.SetStatusCondition(&dpu.Status.AgentStatus.Conditions, condition)
	}

	if err := s.Status().Patch(req.Request.Context(), dpu, patch); err != nil {
		klog.Errorf("failed to patch DPU %s: %v", request.DPUName, err)
		_ = resp.WriteError(http.StatusInternalServerError, err)
		return
	}
	resp.WriteHeader(http.StatusOK)
}

type GetObjectResponse struct {
}

func (s *InstallationService) GetObject(req *restful.Request, resp *restful.Response) {
	group := req.QueryParameter("group") // empty for core API group
	version := req.QueryParameter("version")
	kind := req.QueryParameter("kind")
	namespace := req.QueryParameter("namespace") // empty for cluster-scoped objects
	name := req.QueryParameter("name")

	gvk := schema.GroupVersionKind{
		Group:   group,
		Version: version,
		Kind:    kind,
	}
	klog.Infof("Received request to get object %s %s/%s", gvk, namespace, name)

	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(gvk)
	if err := s.Get(req.Request.Context(), client.ObjectKey{Namespace: namespace, Name: name}, obj); err != nil {
		klog.Errorf("failed to get object %s %s/%s: %v", gvk, namespace, name, err)
		_ = resp.WriteError(http.StatusNotFound, err)
		return
	}
	_ = resp.WriteEntity(obj)
}

// serveRepoFile returns a handler that serves static files from the given repository directory.
// It sanitizes the requested subpath to prevent directory traversal attacks.
func serveRepoFile(repoDir string) restful.RouteFunction {
	return func(req *restful.Request, resp *restful.Response) {
		subpath := req.PathParameter("subpath")

		cleanPath := filepath.Clean("/" + subpath)
		if strings.Contains(cleanPath, "..") {
			resp.WriteHeader(http.StatusForbidden)
			return
		}
		fullPath := filepath.Join(repoDir, cleanPath)
		if !strings.HasPrefix(fullPath, repoDir) {
			resp.WriteHeader(http.StatusForbidden)
			return
		}

		klog.V(4).Infof("Serving repo file: %s", fullPath)
		http.ServeFile(resp.ResponseWriter, req.Request, fullPath)
	}
}

// setupMgmtInterfaces sets up IPv6 link-local addresses for all tmfifo_net interfaces.
// Each interface gets the same fe80::1/64 address, which is valid because fe80 addresses
// are link-scoped and don't conflict across different interfaces.
// Returns the list of interface names that were set up.
func setupMgmtInterfaces() ([]string, error) {
	// Parse the IPv6 link-local address
	ipv6Addr, err := netlink.ParseAddr(hostMgmtIPv6 + "/64")
	if err != nil {
		return nil, fmt.Errorf("failed to parse IPv6 address: %w", err)
	}

	links, err := netlink.LinkList()
	if err != nil {
		return nil, fmt.Errorf("failed to list network interfaces: %w", err)
	}

	ifaceNames := []string{}
	for _, link := range links {
		if !strings.HasPrefix(link.Attrs().Name, tmfifoNetPrefix) {
			continue
		}

		ifaceName := link.Attrs().Name

		// Add IPv6 link-local address to interface if not already present
		existingAddrs, err := netlink.AddrList(link, netlink.FAMILY_V6)
		if err != nil {
			klog.Warningf("Failed to get addresses for %s: %v", ifaceName, err)
			continue
		}
		alreadySet := false
		for _, existing := range existingAddrs {
			if existing.IP.Equal(ipv6Addr.IP) {
				alreadySet = true
				break
			}
		}
		if !alreadySet {
			if err := netlink.AddrAdd(link, ipv6Addr); err != nil {
				klog.Warningf("Failed to add IPv6 address to %s: %v", ifaceName, err)
				continue
			}
			klog.Infof("Added address %s/64 to interface %s", hostMgmtIPv6, ifaceName)
		}

		// Ensure interface is up
		if link.Attrs().Flags&net.FlagUp == 0 {
			if err := netlink.LinkSetUp(link); err != nil {
				klog.Warningf("Failed to set %s up: %v", ifaceName, err)
				continue
			}
			klog.Infof("Set interface %s up", ifaceName)
		}

		ifaceNames = append(ifaceNames, ifaceName)
	}

	return ifaceNames, nil
}

// listenTCP6WithRetry attempts to create a TCP6 listener with retries.
// This is needed because IPv6 addresses may be in DAD (Duplicate Address Detection)
// tentative state immediately after being added, during which binding will fail.
func listenTCP6WithRetry(address string) (net.Listener, error) {
	var listener net.Listener
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	err := wait.PollUntilContextCancel(ctx, time.Second, true, func(ctx context.Context) (bool, error) {
		var listenErr error
		listener, listenErr = net.Listen("tcp6", address)
		if listenErr != nil {
			klog.V(2).Infof("Listen on %s failed: %v, retrying...", address, listenErr)
			return false, nil
		}
		return true, nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to listen on %s after retries: %w", address, err)
	}
	return listener, nil
}
