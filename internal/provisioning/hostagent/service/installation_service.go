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
	"net/http"
	"strings"
	"time"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/internal/provisioning/hostagent/service/types"
	hostutil "github.com/nvidia/doca-platform/internal/provisioning/hostagent/util"

	restful "github.com/emicklei/go-restful/v3"
	"github.com/vishvananda/netlink"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	mgmtBridgeName     = "br-dpu-mgmt"
	mgmtBridgeIP       = "169.254.0.1/16"
	DefaultServiceIP   = "169.254.0.1"
	DefaultServicePort = 11029
)

type InstallationService struct {
	client.Client
	server *http.Server
}

func NewInstallationService(client client.Client, listenAddr string) *InstallationService {
	s := &InstallationService{
		Client: client,
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
	ws.Route(ws.GET("/healthz").To(s.HealthCheck))
	container := restful.NewContainer()
	container.Add(ws)
	s.server = &http.Server{
		Addr:    listenAddr,
		Handler: container,
	}
	return s
}

func (s *InstallationService) Start(setupBridge bool) error {
	if setupBridge {
		addr, err := netlink.ParseAddr(mgmtBridgeIP)
		if err != nil {
			return fmt.Errorf("failed to parse management bridge IP address: %w", err)
		}
		klog.Infof("Setting up management bridge")
		if err := setupMgmtBridge(addr); err != nil {
			return fmt.Errorf("failed to setup management bridge: %w", err)
		}
	}

	klog.Infof("Starting InstallationService server on %s", s.server.Addr)
	go func() {
		if err := s.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			klog.Fatalf("InstallationService server failed: %v", err)
		}
	}()
	return nil
}

func (s *InstallationService) Stop() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = s.server.Shutdown(ctx)
}

func (s *InstallationService) HealthCheck(req *restful.Request, resp *restful.Response) {
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
	if dpu.Status.DPUInternalStatus == nil {
		dpu.Status.DPUInternalStatus = &provisioningv1.DPUInternalStatus{
			Conditions: []metav1.Condition{},
		}
	}
	if request.DPUInfo.HostRebootRequired != nil {
		dpu.Status.DPUInternalStatus.HostRebootRequired = request.DPUInfo.HostRebootRequired
	}
	if request.DPUInfo.InitialBootID != nil {
		dpu.Status.DPUInternalStatus.InitialBootID = request.DPUInfo.InitialBootID
	}
	for _, condition := range request.DPUInfo.Conditions {
		meta.SetStatusCondition(&dpu.Status.DPUInternalStatus.Conditions, condition)
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

func setupMgmtBridge(bridgeAddr *netlink.Addr) error {
	bridge, err := hostutil.CreateBridgeIfNotExists(mgmtBridgeName)
	if err != nil {
		return fmt.Errorf("failed to create management bridge: %w", err)
	}

	// Add IP address to management bridge if not already present
	existingAddrs, err := netlink.AddrList(bridge, netlink.FAMILY_ALL)
	if err != nil {
		return fmt.Errorf("failed to get addresses for management bridge: %w", err)
	}
	alreadySet := false
	for _, existing := range existingAddrs {
		if existing.IPNet.String() == bridgeAddr.IPNet.String() {
			alreadySet = true
			break
		}
	}
	if !alreadySet {
		if err := netlink.AddrAdd(bridge, bridgeAddr); err != nil {
			return fmt.Errorf("failed to add IP address to management bridge: %w", err)
		}
		klog.Infof("Added address %s to management bridge %s", mgmtBridgeIP, mgmtBridgeName)
	} else {
		klog.V(3).Infof("Address %s already present on management bridge %s", mgmtBridgeIP, mgmtBridgeName)
	}

	if err := netlink.LinkSetUp(bridge); err != nil {
		return fmt.Errorf("failed to set management bridge up: %w", err)
	}

	// add rshim interfaces to management bridge if not already present
	links, err := netlink.LinkList()
	if err != nil {
		return fmt.Errorf("failed to list network interfaces: %w", err)
	}
	for _, link := range links {
		if !strings.HasPrefix(link.Attrs().Name, "tmfifo_net") {
			continue
		}
		if link.Attrs().MasterIndex == bridge.Attrs().Index {
			continue
		}
		if err := netlink.LinkSetMaster(link, bridge); err != nil {
			klog.Warningf("failed to add rshim NIC %s to bridge %s: %v", link.Attrs().Name, mgmtBridgeName, err)
			continue
		}
		klog.Infof("Added rshim NIC %s to bridge %s", link.Attrs().Name, mgmtBridgeName)
	}
	return nil
}
