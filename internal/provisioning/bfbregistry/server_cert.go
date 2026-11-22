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

package bfbregistry

import (
	"fmt"
	"net"
	"net/url"
	"strings"

	"k8s.io/apimachinery/pkg/util/validation"
)

// DefaultBFBRegistryReplicas is the replica count of the Operator-owned registry Deployment
// (ReadWriteMany BFB PVC) when registry.replicas is unset.
const DefaultBFBRegistryReplicas int32 = 1

// ServerCertSANs returns the DNS names and IP addresses the bfb-registry server certificate
// covers: the Service DNS names, the Kubernetes API server VIP (an IP or a hostname) and the host of
// registry.loadBalancerAddress. Those are the addresses the clients connect to: the Redfish BFB
// download uses the VIP, the host agent the load balancer address or the Service name.
//
// No node IP is included. The registry Pod is not tied to one node, and no client connects to a
// node address.
func ServerCertSANs(namespace, apiServerVIP, loadBalancerAddress string) (dnsNames, ipAddresses []string) {
	dnsNames = []string{
		PodName,
		fmt.Sprintf("%s.%s", PodName, namespace),
		fmt.Sprintf("%s.%s.svc", PodName, namespace),
		fmt.Sprintf("%s.%s.svc.cluster.local", PodName, namespace),
	}
	// The VIP and the load balancer host may be hostnames (e.g. the OpenShift API or route
	// hostname), which go into the DNS SANs. Duplicates and empty/invalid values are skipped.
	seen := map[string]struct{}{}
	for _, name := range dnsNames {
		seen[name] = struct{}{}
	}
	for _, addr := range []string{apiServerVIP, loadBalancerSANHost(loadBalancerAddress)} {
		if _, ok := seen[addr]; ok {
			continue
		}
		switch {
		case net.ParseIP(addr) != nil:
			ipAddresses = append(ipAddresses, addr)
		case addr != "" && len(validation.IsDNS1123Subdomain(addr)) == 0:
			dnsNames = append(dnsNames, addr)
		default:
			continue
		}
		seen[addr] = struct{}{}
	}
	return dnsNames, ipAddresses
}

// loadBalancerSANHost returns the host from registry.loadBalancerAddress for use as a
// certificate SAN. The scheme and port are stripped. An empty or unparseable address
// returns "".
func loadBalancerSANHost(address string) string {
	address = strings.TrimSpace(address)
	if address == "" {
		return ""
	}
	if !strings.Contains(address, "://") {
		address = "https://" + address
	}
	u, err := url.Parse(address)
	if err != nil {
		return ""
	}
	return u.Hostname()
}
