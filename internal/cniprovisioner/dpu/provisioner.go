/*
Copyright 2024 NVIDIA.

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

package dpucniprovisioner

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"time"

	"github.com/nvidia/doca-platform/internal/cniprovisioner/utils/networkhelper"
	dpu "github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/state"
	"github.com/nvidia/doca-platform/internal/utils/ovsclient"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"
	"k8s.io/utils/clock"
	kexec "k8s.io/utils/exec"
	"k8s.io/utils/ptr"
)

type Mode string

// String returns the string representation of the mode
func (m Mode) String() string {
	return string(m)
}

const (
	// InternalIPAM is the mode where IPAM is managed by DPUServiceIPAM objects and we have to provide the IPs to br-ovn
	// and the PF on the host
	InternalIPAM Mode = "internal-ipam"
	// ExternalIPAM is the mode where an external DHCP server provides the IPs to the br-ovn and PF on the host
	ExternalIPAM Mode = "external-ipam"
	// geneveHeaderSize is the size of the geneve header, which is 60 bytes.
	geneveHeaderSize = 60
	// maxMTUSize is the maximum MTU size that can be set on a network interface.
	maxMTUSize = 9216
)

const (
	// brOVN is the name of the bridge that is used by OVN as the external bridge (br-ex). This is the bridge that is
	// later connected with br-sfc. In the current OVN IC w/ DPU implementation, the internal port of this bridge acts
	// as the VTEP.
	brOVN = "br-ovn"

	// ovnInputGatewayOptsPath is the path to the file in which kubeovn-controller expects the additional gateway opts
	ovnInputGatewayOptsPath = "/etc/init-output/ovn_gateway_opts"
	// ovnInputRouterSubnetPath is the path to the file in which kubeovn-controller expects the Gateway Router Subnet
	ovnInputRouterSubnetPath = "/etc/init-output/ovn_gateway_router_subnet"
	// brOVNNetplanConfigPath is the path to the file which contains the netplan configuration for br-ovn
	brOVNNetplanConfigPath = "/etc/netplan/80-br-ovn.yaml"
	// netplanApplyDonePath is a file that indicates that a netplan apply has already ran and was successful
	netplanApplyDonePath = "/etc/netplan/.dpucniprovisioner.done"
)

type DPUCNIProvisioner struct {
	ctx                       context.Context
	ensureConfigurationTicker clock.Ticker
	ovsClient                 ovsclient.OVSClient
	networkHelper             networkhelper.NetworkHelper
	exec                      kexec.Interface
	kubernetesClient          kubernetes.Interface

	// FileSystemRoot controls the file system root. It's used for enabling easier testing of the package. Defaults to
	// empty.
	FileSystemRoot string

	// vtepIPNet is the IP that should be added to the VTEP interface.
	vtepIPNet *net.IPNet
	// gateway is the gateway IP that is configured on the routes related to OVN Kubernetes reaching its peer nodes
	// when traffic needs to go from one Pod running on Node A to another Pod running on Node B.
	gateway net.IP
	// vtepCIDR is the CIDR in which all the VTEP IPs of all the DPUs in the DPU cluster belong to. This CIDR is
	// configured on the routes related to traffic that needs to go from one Pod running on worker Node A to another Pod
	// running on worker Node B.
	vtepCIDR *net.IPNet
	// hostCIDR is the CIDR of the host machines that is configured on the routes related to OVN Kubernetes reaching
	// its peer nodes when traffic needs to go from one Pod running on worker Node A to another Pod running on control
	// plane A (and vice versa).
	hostCIDR *net.IPNet
	// pfIP is the IP that should be added to the PF on the host
	pfIP *net.IPNet
	// dpuHostName is the name of the DPU.
	dpuHostName string
	// gatewayDiscoveryNetwork is the network from which the DPUCNIProvisioner discovers the gateway that it should be
	// on relevant underlying systems.
	gatewayDiscoveryNetwork *net.IPNet

	// dhcpCmd is the struct that holds information about the DHCP Server process
	dhcpCmd kexec.Cmd
	// mode is the mode in which the CNI provisioner is running
	mode Mode
	// ovnMTU is the MTU that is configured for OVN
	ovnMTU int
}

// New creates a DPUCNIProvisioner that can configure the system
func New(ctx context.Context,
	mode Mode,
	clock clock.WithTicker,
	ovsClient ovsclient.OVSClient,
	networkHelper networkhelper.NetworkHelper,
	exec kexec.Interface,
	kubernetesClient kubernetes.Interface,
	vtepIPNet *net.IPNet,
	gateway net.IP,
	vtepCIDR *net.IPNet,
	hostCIDR *net.IPNet,
	pfIP *net.IPNet,
	dpuHostName string,
	gatewayDiscoveryNetwork *net.IPNet,
	ovnMTU int,
) *DPUCNIProvisioner {
	return &DPUCNIProvisioner{
		ctx:                       ctx,
		ensureConfigurationTicker: clock.NewTicker(30 * time.Second),
		ovsClient:                 ovsClient,
		networkHelper:             networkHelper,
		exec:                      exec,
		kubernetesClient:          kubernetesClient,
		FileSystemRoot:            "",
		vtepIPNet:                 vtepIPNet,
		gateway:                   gateway,
		vtepCIDR:                  vtepCIDR,
		hostCIDR:                  hostCIDR,
		pfIP:                      pfIP,
		dpuHostName:               dpuHostName,
		mode:                      mode,
		gatewayDiscoveryNetwork:   gatewayDiscoveryNetwork,
		ovnMTU:                    ovnMTU,
	}
}

// RunOnce runs the provisioning flow once and exits
func (p *DPUCNIProvisioner) RunOnce() error {
	if err := p.configure(); err != nil {
		return err
	}
	klog.Info("Configuration complete.")
	if p.mode == InternalIPAM {
		if err := p.startDHCPServer(); err != nil {
			return fmt.Errorf("error while starting DHCP server: %w", err)
		}
		klog.Info("DHCP Server started.")
	}

	return nil
}

// Stop stops the provisioner
func (p *DPUCNIProvisioner) Stop() {
	if p.mode == InternalIPAM {
		p.dhcpCmd.Stop()
	}

	klog.Info("Provisioner stopped")
}

// EnsureConfiguration ensures that particular configuration is in place. This is a blocking function.
func (p *DPUCNIProvisioner) EnsureConfiguration() {
	for {
		select {
		case <-p.ctx.Done():
			return
		case <-p.ensureConfigurationTicker.C():
		}
	}
}

// configure runs the provisioning flow once
func (p *DPUCNIProvisioner) configure() error {
	klog.Info("Configuring Kubernetes host name in OVS")
	if err := p.findAndSetKubernetesHostNameInOVS(); err != nil {
		return fmt.Errorf("error while setting the Kubernetes Host Name in OVS: %w", err)
	}
	if p.mode == ExternalIPAM {
		klog.Info("Configuring br-ovn")
		if err := p.configureBROVN(); err != nil {
			return fmt.Errorf("error while configuring br-ovn: %w", err)
		}
	}

	klog.Info("Configuring system to enable pod to pod on different node connectivity")
	if err := p.configurePodToPodOnDifferentNodeConnectivity(); err != nil {
		return err
	}

	klog.Info("Writing OVN Kubernetes expected input files")
	if err := p.writeFilesForOVN(); err != nil {
		return err
	}

	return nil
}

// findAndSetKubernetesHostNameInOVS discovers and sets the Kubernetes Host Name in OVS
func (p *DPUCNIProvisioner) findAndSetKubernetesHostNameInOVS() error {
	nodeClient := p.kubernetesClient.CoreV1().Nodes()
	n, err := nodeClient.Get(p.ctx, p.dpuHostName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("error while getting Kubernetes Node: %w", err)
	}
	hostName, ok := n.Labels[dpu.HostNameDPULabelKey]
	if !ok {
		return fmt.Errorf("required label %s is not set on node %s in the DPU cluster", dpu.HostNameDPULabelKey, p.dpuHostName)
	}

	if err := p.ovsClient.SetKubernetesHostNodeName(hostName); err != nil {
		return fmt.Errorf("error while setting the Kubernetes Host Name in OVS: %w", err)
	}
	return nil
}

// configurePodToPodOnDifferentNodeConnectivity configures the VTEP interface (br-ovn) and the ovn-encap-ip external ID
// so that traffic going through the geneve tunnels can function as expected.
func (p *DPUCNIProvisioner) configurePodToPodOnDifferentNodeConnectivity() error {
	if p.mode == InternalIPAM {
		if err := p.setLinkIPAddressIfNotSet(brOVN, p.vtepIPNet); err != nil {
			return fmt.Errorf("error while setting VTEP IP: %w", err)
		}
		if err := p.networkHelper.SetLinkUp(brOVN); err != nil {
			return fmt.Errorf("error while setting link %s up: %w", brOVN, err)
		}

		_, vtepNetwork, err := net.ParseCIDR(p.vtepIPNet.String())
		if err != nil {
			return fmt.Errorf("error while parsing network from VTEP IP %s: %w", p.vtepIPNet.String(), err)
		}

		if vtepNetwork.String() != p.vtepCIDR.String() {
			// Add route related to traffic that needs to go from one Pod running on worker Node A to another Pod running
			// on worker Node B.
			if err := p.addRouteIfNotExists(p.vtepCIDR, p.gateway, brOVN, nil); err != nil {
				return fmt.Errorf("error while adding route %s %s %s: %w", p.vtepCIDR, p.gateway.String(), brOVN, err)
			}
		}
	}

	// Add route related to traffic that needs to go from one Pod running on worker Node A to another Pod running on
	// control plane A (and vice versa).
	//
	// In our setup, we will already have a route pointing to the same CIDR via the SF designated for kubelet traffic
	// which gets a DHCP IP in that CIDR. Given that, we need to set the metric of this route to something very high
	// so that it's the last preferred route in the route table for that CIDR. The reason for that is this OVS bug that
	// selects the route with the highest prio - see issue 3871067.
	if err := p.addRouteIfNotExists(p.hostCIDR, p.gateway, brOVN, ptr.To[int](10000)); err != nil {
		return fmt.Errorf("error while adding route %s %s %s: %w", p.hostCIDR, p.gateway.String(), brOVN, err)
	}

	if err := p.ovsClient.SetOVNEncapIP(p.vtepIPNet.IP); err != nil {
		return fmt.Errorf("error while setting the OVN Encap IP: %w", err)
	}

	return nil
}

// setLinkIPAddressIfNotSet sets an IP address to a link if it's not already set
func (p *DPUCNIProvisioner) setLinkIPAddressIfNotSet(link string, ipNet *net.IPNet) error {
	hasIP, err := p.networkHelper.LinkIPAddressExists(link, ipNet)
	if err != nil {
		return fmt.Errorf("error checking whether IP exists: %w", err)
	}
	if hasIP {
		klog.Infof("Link %s has IP %s, skipping configuration", link, ipNet)
		return nil
	}
	if err := p.networkHelper.SetLinkIPAddress(link, ipNet); err != nil {
		return fmt.Errorf("error setting IP address: %w", err)
	}
	return nil
}

// addRouteIfNotExists adds a route if it doesn't already exist
func (p *DPUCNIProvisioner) addRouteIfNotExists(network *net.IPNet, gateway net.IP, device string, metric *int) error {
	hasRoute, err := p.networkHelper.RouteExists(network, gateway, device)
	if err != nil {
		return fmt.Errorf("error checking whether route exists: %w", err)
	}
	if hasRoute {
		klog.Infof("Route %s %s %s exists, skipping configuration", network, gateway, device)
		return nil
	}
	if err := p.networkHelper.AddRoute(network, gateway, device, metric); err != nil {
		return fmt.Errorf("error adding route: %w", err)
	}
	return nil
}

// writeFilesForOVN writes the input files that the ovnkube-controller expects
func (p *DPUCNIProvisioner) writeFilesForOVN() error {
	if err := p.writeOVNInputGatewayOptsFile(); err != nil {
		return fmt.Errorf("error while writing the file OVN expects to find additional gateway options: %w", err)
	}

	if err := p.writeOVNInputRouterSubnetPath(); err != nil {
		return fmt.Errorf("error while writing the file OVN expects to find the gateway router subnet: %w", err)
	}

	return nil
}

// configureBROVN requests an IP via DHCP for br-ovn and mutates the relevant fields of the DPUCNIProvisioner objects
func (p *DPUCNIProvisioner) configureBROVN() error {
	if err := p.requestIPForBROVN(); err != nil {
		return fmt.Errorf("error while br-ovn netplan: %w", err)
	}

	addrs, err := p.networkHelper.GetLinkIPAddresses(brOVN)
	if err != nil {
		return fmt.Errorf("error while getting IP addresses for link %s: %w", brOVN, err)
	}

	if len(addrs) != 1 {
		return fmt.Errorf("exactly 1 IP is expected in %s", brOVN)
	}

	p.vtepIPNet = addrs[0]

	gateway, err := p.networkHelper.GetGateway(p.gatewayDiscoveryNetwork)
	if err != nil {
		return fmt.Errorf("error while parsing gateway from gateway discovery network %s: %w", p.gatewayDiscoveryNetwork.String(), err)
	}

	p.gateway = gateway
	return nil
}

// writeOVNInputGatewayOptsFile writes the file in which kubeovn-controller expects the additional gateway opts
func (p *DPUCNIProvisioner) writeOVNInputGatewayOptsFile() error {
	configPath := filepath.Join(p.FileSystemRoot, ovnInputGatewayOptsPath)
	content := fmt.Sprintf("--gateway-nexthop=%s", p.gateway.String())
	err := os.WriteFile(configPath, []byte(content), 0644)
	if err != nil {
		return fmt.Errorf("error while writing file %s: %w", configPath, err)
	}
	return nil
}

// writeOVNInputRouterSubnetPath writes the file in which kubeovn-controller expects the Gateway Router Subnet
func (p *DPUCNIProvisioner) writeOVNInputRouterSubnetPath() error {
	configPath := filepath.Join(p.FileSystemRoot, ovnInputRouterSubnetPath)
	_, vtepNetwork, err := net.ParseCIDR(p.vtepIPNet.String())
	if err != nil {
		return fmt.Errorf("error while parsing network from VTEP IP %s: %w", p.vtepIPNet.String(), err)
	}
	content := vtepNetwork.String()
	err = os.WriteFile(configPath, []byte(content), 0644)
	if err != nil {
		return fmt.Errorf("error while writing file %s: %w", configPath, err)
	}
	return nil
}

// requestIPForBROVN writes a netplan file and runs netplan apply to request an IP for br-ovn
func (p *DPUCNIProvisioner) requestIPForBROVN() error {
	configPath := filepath.Join(p.FileSystemRoot, brOVNNetplanConfigPath)
	content := fmt.Sprintf(`
network:
  renderer: networkd
  version: 2
  bridges:
    %s:
      dhcp4: yes
      dhcp4-overrides:
        use-dns: no
      openvswitch: {}
`, brOVN)
	if err := os.WriteFile(configPath, []byte(content), 0600); err != nil {
		return fmt.Errorf("error while writing file %s: %w", configPath, err)
	}

	applyDonePath := filepath.Join(p.FileSystemRoot, netplanApplyDonePath)
	_, err := os.Stat(applyDonePath)
	if err == nil {
		klog.Info("netplan apply already ran, not rerunning")
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("error when calling stat on %s: %w", applyDonePath, err)
	}

	cmd := p.exec.Command("netplan", "apply")
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.SetStdout(&stdout)
	cmd.SetStderr(&stderr)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("error running netplan: stdout='%s' stderr='%s': %w", stdout.String(), stderr.String(), err)
	}

	_, err = os.Create(applyDonePath)
	if err != nil {
		return fmt.Errorf("error creating %s file: %w", applyDonePath, err)
	}
	return nil
}

// startDHCPServer starts a DHCP Server to enable the PF on the host to get an IP.
func (p *DPUCNIProvisioner) startDHCPServer() error {
	if p.dhcpCmd != nil {
		klog.Warning("DHCP Server already running")
		return nil
	}

	_, vtepNetwork, err := net.ParseCIDR(p.vtepIPNet.String())
	if err != nil {
		return fmt.Errorf("error while parsing network from VTEP IP %s: %w", p.vtepIPNet.String(), err)
	}

	mac, err := p.networkHelper.GetPFRepMACAddress("pf0hpf")
	if err != nil {
		return fmt.Errorf("error while parsing MAC address of the PF on the host: %w", err)
	}

	// Add the geneve header size to the MTU.
	pfMTU := p.ovnMTU + geneveHeaderSize

	if pfMTU == geneveHeaderSize || pfMTU > maxMTUSize {
		return errors.New("invalid PF MTU: it must be greater than 60 and less than or equal to 9216")
	}

	args := []string{
		"--keep-in-foreground",
		"--port=0",         // Disable DNS Server
		"--log-facility=-", // Log to stderr
		fmt.Sprintf("--interface=%s", brOVN),
		"--dhcp-option=option:router",
		fmt.Sprintf("--dhcp-option=option:mtu,%d", pfMTU),
		fmt.Sprintf("--dhcp-range=%s,static", vtepNetwork.IP.String()),
		fmt.Sprintf("--dhcp-host=%s,%s", mac, p.pfIP.IP.String()),
	}

	if vtepNetwork.String() != p.vtepCIDR.String() {
		args = append(args, fmt.Sprintf("--dhcp-option=option:classless-static-route,%s,%s", p.vtepCIDR.String(), p.gateway.String()))
	}

	cmd := p.exec.Command("dnsmasq", args...)

	cmd.SetStdout(os.Stdout)
	cmd.SetStderr(os.Stderr)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("error while starting the DHCP server: %w", err)
	}

	p.dhcpCmd = cmd
	return nil
}
