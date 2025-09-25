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

package server

import (
	"context"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"log"
	"math/rand"
	"net"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/test/mock/dms/pkg/certs"
	"github.com/nvidia/doca-platform/test/mock/dms/pkg/config"

	"github.com/openconfig/gnmi/proto/gnmi"
	"github.com/openconfig/gnoi/os"
	"github.com/openconfig/gnoi/system"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
	"k8s.io/klog/v2"
)

const (
	setCallName      = "set"
	getCallName      = "get"
	activateCallName = "activate"
	installCallName  = "install"
	rebootCallName   = "reboot"
)

type DMSAPICommand string

const (
	DefaultCommand           DMSAPICommand = "DefaultCommand"
	DPUCapacityCommand       DMSAPICommand = "DPUCapacityCommand"
	PCIRescanRequiredCommand DMSAPICommand = "PCIRescanRequired"
	CurrentFWVersionCommand  DMSAPICommand = "CurrentFWVersion"
	RunningFWVersionCommand  DMSAPICommand = "RunningFWVersion"
	HostNetworkConfigCommand DMSAPICommand = "HostNetworkConfig"
	HostNetworDeleteCommand  DMSAPICommand = "HostNetworkDelete"
)

type APIHandler interface {
	os.OSServer
	system.SystemServer
	gnmi.GNMIServer
}

type ListenerManager interface {
	AllocateListener() (net.Listener, error)
}

type Server interface {
	ServeForDPUNode(*provisioningv1.DPUNode, net.Listener) error
}

type DPUNodeToPortListener struct {
	sync.RWMutex
	// MinPort is the highest port number the DMSServerMux can use when creating listeners.
	MinPort uint16 // Minimum port number for use when creating listener instances.
	// MaxPort is the highest port number the DMSServerMux can use when creating listeners.
	MaxPort uint16
	// mostRecentAllocatedPort allocated by the DMSServerMux.
	mostRecentAllocatedPort uint16
	// allocatedPorts is the set of ports that have already been allocated.
	allocatedPorts map[uint16]interface{}
}

type DMSServerMux struct {
	*grpc.Server
	ListenerManager
	// activeListenerForDPUNode maps a listener address to a DPU namespace name.
	dpuNodeForListener map[string]string
	// IP address to bind listener instances to.
	ip string
}

func NewDMSServerMux(ip string, cert *x509.Certificate, key *rsa.PrivateKey, listenerManager ListenerManager, handler APIHandler) *DMSServerMux {
	tlsCert, err := tls.X509KeyPair(certs.EncodeCertPEM(cert), certs.EncodePrivateKeyPEM(key))
	if err != nil {
		panic("Failed to create key pair")
	}
	caCertPool := x509.NewCertPool()
	caCertPool.AddCert(cert)

	tlsConfig := &tls.Config{
		ServerName:   ip,
		Certificates: []tls.Certificate{tlsCert},
		ClientCAs:    caCertPool,
		ClientAuth:   tls.NoClientCert,
		MinVersion:   tls.VersionTLS12,
	}

	d := &DMSServerMux{
		Server: grpc.NewServer(
			grpc.Creds(credentials.NewTLS(tlsConfig))),
		ListenerManager:    listenerManager,
		dpuNodeForListener: map[string]string{},
		ip:                 ip,
	}

	gnmi.RegisterGNMIServer(d.Server, handler)
	os.RegisterOSServer(d.Server, handler)
	system.RegisterSystemServer(d.Server, handler)

	return d
}

func (d *DPUNodeToPortListener) AllocateListener() (net.Listener, error) {
	d.Lock()
	defer d.Unlock()
	if d.allocatedPorts == nil {
		d.allocatedPorts = map[uint16]interface{}{}
	}
	if d.mostRecentAllocatedPort == 0 {
		d.mostRecentAllocatedPort = d.MinPort
	}
	// allocate a port for the DPU
	allocatedPort := uint16(0)
	for port := d.mostRecentAllocatedPort + 1; port <= d.MaxPort; port++ {
		// If the port is already allocated continue.
		if _, ok := d.allocatedPorts[port]; ok {
			continue
		}
		allocatedPort = port
		d.allocatedPorts[allocatedPort] = nil
		break
	}
	if allocatedPort == 0 {
		return nil, fmt.Errorf("no port allocated. most recent allocated port %d. max port %d", d.mostRecentAllocatedPort, d.MaxPort)
	}

	listener, err := net.Listen("tcp", fmt.Sprintf("0.0.0.0:%d", allocatedPort))
	if err != nil {
		return nil, err
	}

	// save the allocated port iff the listener is healthy.
	d.mostRecentAllocatedPort = allocatedPort
	return listener, nil
}

// ServeForDPUNode creates allocates a port for a given DPU node and updates the DPUNode spec to point to the port.
func (d *DMSServerMux) ServeForDPUNode(dpuNode *provisioningv1.DPUNode, listener net.Listener) error {
	if d.dpuNodeForListener == nil {
		d.dpuNodeForListener = map[string]string{}
	}
	// If we already have a listener for this DPU return early.
	// TODO: Should we check if this listener is working for re-entrancy?
	_, ok := d.dpuNodeForListener[listener.Addr().String()]
	if ok {
		return nil
	}

	// Get the port number from the listener's address
	_, portStr, err := net.SplitHostPort(listener.Addr().String())
	if err != nil {
		return fmt.Errorf("failed to split host and port from listener address: %v", err)
	}

	port, err := strconv.Atoi(portStr)
	if err != nil {
		return fmt.Errorf("failed to convert port string to integer: %v", err)
	}

	// Update the DPU node's address with the actual port
	dpuNode.Spec.NodeDMSAddress = &provisioningv1.DMSAddress{
		Port: uint16(port),
		IP:   d.ip,
	}
	go func() {
		if err := d.Serve(listener); err != nil {
			// TODO: Not sure this should be log.Fatal in the long term - but useful for now to uplevel error in serving.
			log.Fatalf("failed to serve for DPU node: %s,", err)
		}
	}()

	d.dpuNodeForListener[listener.Addr().String()] = fmt.Sprintf("%s/%s", dpuNode.Namespace, dpuNode.Name)
	return nil
}

type dpuResponseConfig struct {
	responseConfigs map[string]responseConfig
}
type responseConfig struct {
	delay     time.Duration
	errorRate float64
}

type ConfigurableAPIHandler struct {
	os.UnimplementedOSServer
	system.UnimplementedSystemServer
	gnmi.UnimplementedGNMIServer

	sync.RWMutex
	Config config.Config
	// configForPort maps port numbers to the DPUs they act as DMS servers for.
	configForPort map[string]dpuResponseConfig
}

func (d *ConfigurableAPIHandler) configForRequest(ctx context.Context, requestType string) (*responseConfig, error) {
	p, ok := peer.FromContext(ctx)
	if !ok {
		return nil, fmt.Errorf("could not get peer from context")
	}
	a, ok := p.LocalAddr.(*net.TCPAddr)
	if !ok {
		return nil, fmt.Errorf("could not get port from local address")
	}

	dpuConf := d.getDPUResponseConfiguration(a.Port)
	conf, ok := dpuConf.responseConfigs[requestType]
	if !ok {
		return nil, fmt.Errorf("could not get Config for requestType %s", requestType)
	}
	return &conf, nil
}

func (d *ConfigurableAPIHandler) getDPUResponseConfiguration(port int) dpuResponseConfig {
	d.Lock()
	defer d.Unlock()
	if d.configForPort == nil {
		d.configForPort = map[string]dpuResponseConfig{}
	}
	dpuConf, ok := d.configForPort[fmt.Sprintf("%d", port)]
	if ok {
		return dpuConf
	}
	dpuConf = dpuResponseConfig{
		responseConfigs: map[string]responseConfig{
			setCallName: {
				delay:     delayWithJitter(d.Config.Set.MeanDelaySeconds, d.Config.Set.DelayJitter),
				errorRate: d.Config.Set.ErrorRate,
			},
			getCallName: {
				delay:     delayWithJitter(d.Config.Set.MeanDelaySeconds, d.Config.Set.DelayJitter),
				errorRate: d.Config.Set.ErrorRate,
			},
			activateCallName: {
				delay:     delayWithJitter(d.Config.Activate.MeanDelaySeconds, d.Config.Activate.DelayJitter),
				errorRate: d.Config.Activate.ErrorRate,
			},
			installCallName: {
				delay:     delayWithJitter(d.Config.Install.MeanDelaySeconds, d.Config.Install.DelayJitter),
				errorRate: d.Config.Install.ErrorRate,
			},
			rebootCallName: {
				delay:     delayWithJitter(d.Config.Reboot.MeanDelaySeconds, d.Config.Reboot.DelayJitter),
				errorRate: d.Config.Reboot.ErrorRate,
			},
		},
	}
	d.configForPort[fmt.Sprintf("%d", port)] = dpuConf
	return dpuConf
}

func (d *ConfigurableAPIHandler) RebootStatus(ctx context.Context, req *system.RebootStatusRequest) (*system.RebootStatusResponse, error) {
	conf, err := d.configForRequest(ctx, rebootCallName)
	if err != nil {
		return nil, err
	}
	logger := klog.LoggerWithValues(klog.Background(), "api", rebootCallName)
	logger.Info("Calling")
	if rand.Float64() < conf.errorRate {
		logger.Info("returning error")
		return &system.RebootStatusResponse{
			Status: &system.RebootStatus{
				Status: system.RebootStatus_STATUS_FAILURE,
			},
		}, fmt.Errorf("error during activation")
	}
	logger.Info(fmt.Sprintf("waiting %f seconds ", conf.delay.Seconds()))
	time.Sleep(conf.delay)

	return &system.RebootStatusResponse{Active: false, Status: &system.RebootStatus{Status: system.RebootStatus_STATUS_SUCCESS}}, nil
}
func (d *ConfigurableAPIHandler) Install(req os.OS_InstallServer) error {
	conf, err := d.configForRequest(req.Context(), installCallName)
	if err != nil {
		return err
	}

	logger := klog.LoggerWithValues(klog.Background(), "api", rebootCallName)
	logger.Info("Calling")
	if rand.Float64() < conf.errorRate {
		logger.Info("returning error")
		return req.Send(&os.InstallResponse{Response: &os.InstallResponse_InstallError{
			InstallError: &os.InstallError{
				Type:   os.InstallError_INCOMPATIBLE,
				Detail: "install failed due to failure rate",
			},
		}})
	}
	logger.Info(fmt.Sprintf("waiting %f seconds ", conf.delay.Seconds()))
	time.Sleep(conf.delay)

	return req.Send(&os.InstallResponse{Response: &os.InstallResponse_Validated{
		Validated: &os.Validated{
			Version: "one",
		}}})
}

func (d *ConfigurableAPIHandler) Activate(ctx context.Context, req *os.ActivateRequest) (*os.ActivateResponse, error) {
	conf, err := d.configForRequest(ctx, activateCallName)
	if err != nil {
		return nil, err
	}
	logger := klog.LoggerWithValues(klog.Background(), "api", rebootCallName)
	logger.Info("Calling")
	if rand.Float64() < conf.errorRate {
		logger.Info("returning error")
		return &os.ActivateResponse{
			Response: &os.ActivateResponse_ActivateError{
				ActivateError: &os.ActivateError{
					Type:   os.ActivateError_UNSPECIFIED,
					Detail: "it's an error",
				},
			},
		}, fmt.Errorf("error during activation")
	}
	logger.Info(fmt.Sprintf("waiting %f seconds ", conf.delay.Seconds()))
	time.Sleep(conf.delay)
	return nil, nil
}

func (d *ConfigurableAPIHandler) Set(ctx context.Context, req *gnmi.SetRequest) (*gnmi.SetResponse, error) {
	conf, err := d.configForRequest(ctx, setCallName)
	if err != nil {
		return nil, err
	}
	logger := klog.LoggerWithValues(klog.Background(), "api", rebootCallName)
	logger.Info("Calling")
	if rand.Float64() < conf.errorRate {
		return &gnmi.SetResponse{
			Response: []*gnmi.UpdateResult{
				{
					Op: gnmi.UpdateResult_INVALID,
				},
			},
		}, fmt.Errorf("error during activation")
	}
	logger.Info(fmt.Sprintf("waiting %f seconds ", conf.delay.Seconds()))
	time.Sleep(conf.delay)

	return &gnmi.SetResponse{}, nil
}

func (d *ConfigurableAPIHandler) Get(ctx context.Context, req *gnmi.GetRequest) (*gnmi.GetResponse, error) {
	path := req.GetPath()
	if len(path) == 0 {
		return nil, status.Error(codes.InvalidArgument, "path is empty")
	}
	pathString, cmd, err := pathToString(path[0])
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "path to string failed: %v", err)
	}
	command := strings.Join(cmd, "")
	conf, err := d.configForRequest(ctx, getCallName)
	if err != nil {
		return nil, err
	}

	logger := klog.LoggerWithValues(klog.Background(), "api", getCallName, "path", pathString, "command", command)
	logger.Info("Calling")
	if rand.Float64() < conf.errorRate {
		return nil, status.Error(codes.Internal, "error during activation")
	}
	logger.Info(fmt.Sprintf("waiting %f seconds ", conf.delay.Seconds()))
	time.Sleep(conf.delay)
	resp := &gnmi.GetResponse{
		Notification: []*gnmi.Notification{
			{
				Update: []*gnmi.Update{},
			},
		},
	}
	commandType := getDMSCommandType(command)
	logger.Info(fmt.Sprintf("DMS command type %v", commandType))
	switch commandType {
	case DPUCapacityCommand:
		resp.Notification[0].Update = []*gnmi.Update{
			{
				Path: path[0],
				Val: &gnmi.TypedValue{
					Value: &gnmi.TypedValue_StringVal{
						// a description of the DPU, copied from a real DPU.
						StringVal: "NVIDIA BlueField-3 B3220 P-Series FHHL DPU; 200GbE (default mode) / NDR200 IB; Dual-port QSFP112; PCIe Gen5.0 x16 with x16 PCIe extension option; 16 Arm cores; 32GB on-board DDR; integrated BMC; Crypto Enabled",
					},
				},
			},
		}
	case PCIRescanRequiredCommand:
		resp.Notification[0].Update = []*gnmi.Update{
			{
				Path: path[0],
				Val: &gnmi.TypedValue{
					Value: &gnmi.TypedValue_StringVal{
						StringVal: "0x00000000",
					},
				},
			},
		}
	case CurrentFWVersionCommand:
		resp.Notification[0].Update = []*gnmi.Update{
			{
				Path: path[0],
				Val: &gnmi.TypedValue{
					Value: &gnmi.TypedValue_StringVal{
						StringVal: "32.43.2204",
					},
				},
			},
		}
	case RunningFWVersionCommand:
		resp.Notification[0].Update = []*gnmi.Update{
			{
				Path: path[0],
				Val: &gnmi.TypedValue{
					Value: &gnmi.TypedValue_StringVal{
						StringVal: "32.43.2204",
					},
				},
			},
		}
	case HostNetworkConfigCommand:
		resp.Notification[0].Update = []*gnmi.Update{
			{
				Path: path[0],
				Val: &gnmi.TypedValue{
					Value: &gnmi.TypedValue_IntVal{
						IntVal: 0,
					},
				},
			},
		}
	case HostNetworDeleteCommand:
		resp.Notification[0].Update = []*gnmi.Update{
			{
				Path: path[0],
				Val: &gnmi.TypedValue{
					Value: &gnmi.TypedValue_IntVal{
						IntVal: 0,
					},
				},
			},
		}
	case DefaultCommand:
		resp.Notification[0].Update = []*gnmi.Update{
			{
				Path: path[0],
				Val: &gnmi.TypedValue{
					Value: &gnmi.TypedValue_StringVal{
						StringVal: "0x00000001",
					},
				},
			},
		}
	}
	return resp, nil
}

// pathToString converts a gnmi.Path to a string. This is a copy of the function in DMS source code.
func pathToString(path *gnmi.Path) (string, []string, error) {
	if path == nil || len(path.Elem) == 0 {
		return "", nil, status.Errorf(codes.NotFound, "path to string failed: path is nil")
	}
	str := ""
	keys := make([]string, 1)
	for _, elem := range path.Elem {
		str += "/"
		str += elem.Name
		for key, val := range elem.Key {
			str += "[" + key + "=$" + strconv.Itoa(len(keys)) + "]"
			keys = append(keys, val)
		}
	}
	return str, keys, nil
}

func getDMSCommandType(input string) DMSAPICommand {
	patterns := map[DMSAPICommand]string{
		DPUCapacityCommand:       `flint -d [0-9a-f:.]+ query full`,
		PCIRescanRequiredCommand: `mlxreg -d [0-9a-f:.]+ --get --reg_name MFRL`,
		CurrentFWVersionCommand:  `flint -d [0-9a-f:.]+ q \|grep 'FW Version'`,
		RunningFWVersionCommand:  `flint -d [0-9a-f:.]+ q |grep 'FW Version\(Running\)'`,
		HostNetworkConfigCommand: `/opt/dpf/hostnetwork.sh --num_of_vfs [0-9]+ --serial_number [^--]+ --device_pci_address [0-9a-f:.]+ --control_plane_mtu [0-9]+`,
		HostNetworDeleteCommand:  `/opt/dpf/hostnetwork.sh --delete --device_pci_address [0-9a-f:.]+ --control_plane_mtu [0-9]+`,
	}
	for cmdType, pattern := range patterns {
		com := regexp.MustCompile(pattern)
		if com.MatchString(input) {
			return cmdType
		}

	}
	return DefaultCommand
}

func delayWithJitter(mean int64, maxJitter float64) time.Duration {
	meanDuration := time.Duration(mean * time.Second.Nanoseconds())
	jitterDuration := float64(meanDuration.Nanoseconds()) * maxJitter
	if jitterDuration <= 0 {
		return time.Duration(0)
	}
	jitter := time.Duration(rand.Int63n(int64(jitterDuration)*2)) - time.Duration(jitterDuration)
	return meanDuration + jitter
}
