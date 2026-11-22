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
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("ServerCertSANs", func() {
	It("covers Service DNS and the VIP IP without a node IP", func() {
		dns, ips := ServerCertSANs("dpf-operator-system", "192.0.2.10", "")
		Expect(dns).To(Equal([]string{
			"bfb-registry",
			"bfb-registry.dpf-operator-system",
			"bfb-registry.dpf-operator-system.svc",
			"bfb-registry.dpf-operator-system.svc.cluster.local",
		}))
		Expect(ips).To(Equal([]string{"192.0.2.10"}))
	})

	It("covers only the Service DNS names when nothing else is configured", func() {
		dns, ips := ServerCertSANs("ns", "", "")
		Expect(dns).To(HaveLen(4))
		Expect(ips).To(BeEmpty())
	})

	It("adds a DNS SAN when kubernetesAPIServerVIP is a hostname", func() {
		dns, ips := ServerCertSANs("ns", "vip.example.com", "")
		Expect(dns).To(ContainElement("vip.example.com"))
		Expect(ips).To(BeEmpty())
	})

	It("ignores an invalid kubernetesAPIServerVIP", func() {
		dns, ips := ServerCertSANs("ns", "not a host!", "")
		Expect(dns).To(HaveLen(4))
		Expect(ips).To(BeEmpty())
	})

	It("does not duplicate an address that is configured twice", func() {
		_, ips := ServerCertSANs("ns", "10.0.0.10", "https://10.0.0.10:31272")
		Expect(ips).To(ConsistOf("10.0.0.10"))
	})

	It("adds a load balancer hostname as a DNS SAN", func() {
		dns, _ := ServerCertSANs("dpf-provisioning", "", "https://bfb-registry-dpf-operator-system.apps.example.com")
		Expect(dns).To(ContainElement("bfb-registry-dpf-operator-system.apps.example.com"))
	})

	It("strips the port from a load balancer hostname", func() {
		dns, _ := ServerCertSANs("dpf-provisioning", "", "https://bfb.example.com:30443")
		Expect(dns).To(ContainElement("bfb.example.com"))
	})

	It("adds an IP load balancer address as an IP SAN", func() {
		dns, ips := ServerCertSANs("dpf-provisioning", "", "https://10.0.110.2:31272")
		Expect(dns).To(ConsistOf(
			"bfb-registry",
			"bfb-registry.dpf-provisioning",
			"bfb-registry.dpf-provisioning.svc",
			"bfb-registry.dpf-provisioning.svc.cluster.local",
		))
		Expect(ips).To(ConsistOf("10.0.110.2"))
	})

	It("does not duplicate a load balancer host that is already a service DNS name", func() {
		dns, _ := ServerCertSANs("dpf-provisioning", "", "https://bfb-registry:8443")
		Expect(dns).To(ConsistOf(
			"bfb-registry",
			"bfb-registry.dpf-provisioning",
			"bfb-registry.dpf-provisioning.svc",
			"bfb-registry.dpf-provisioning.svc.cluster.local",
		))
	})
})
