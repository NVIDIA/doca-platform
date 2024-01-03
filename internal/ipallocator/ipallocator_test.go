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

package ipallocator_test

import (
	"context"
	"os"
	"path/filepath"

	"github.com/nvidia/doca-platform/internal/ipallocator"

	"github.com/containernetworking/cni/libcni"
	"github.com/containernetworking/cni/pkg/invoke/fakes"
	"github.com/containernetworking/cni/pkg/version"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/utils/ptr"
)

var _ = Describe("IPAllocator", func() {
	var tmpDir string
	BeforeEach(func() {
		var err error
		tmpDir, err = os.MkdirTemp("", "ipallocator")
		defer func() {
			err := os.RemoveAll(tmpDir)
			Expect(err).ToNot(HaveOccurred())
		}()
		Expect(err).NotTo(HaveOccurred())
	})
	DescribeTable("ParseRequests works as expected", func(input string, expectError bool, expectedReqs []ipallocator.NVIPAMIPAllocatorRequest) {
		allocator := ipallocator.New(nil, "", "", "")

		reqs, err := allocator.ParseRequests(input)
		if expectError {
			Expect(err).To(HaveOccurred())
		} else {
			Expect(err).ToNot(HaveOccurred())
			Expect(reqs).To(BeComparableTo(expectedReqs))
		}
	},
		Entry("one IP request", `[{"name":"req1","poolName":"pool1","poolType":"ippool"}]`, false, []ipallocator.NVIPAMIPAllocatorRequest{
			{
				Name:     "req1",
				PoolName: "pool1",
				PoolType: ipallocator.PoolTypeIPPool,
			},
		}),
		Entry("two IP requests", `[{"name":"req1","poolName":"pool1","poolType":"ippool"},{"name":"req2","poolName":"pool2","poolType":"cidrpool"}]`, false, []ipallocator.NVIPAMIPAllocatorRequest{
			{
				Name:     "req1",
				PoolName: "pool1",
				PoolType: ipallocator.PoolTypeIPPool,
			},
			{
				Name:     "req2",
				PoolName: "pool2",
				PoolType: ipallocator.PoolTypeCIDRPool,
			},
		}),
		Entry("no poolType", `[{"name":"req1","poolName":"pool1"}]`, false, []ipallocator.NVIPAMIPAllocatorRequest{
			{
				Name:     "req1",
				PoolName: "pool1",
			},
		}),
		Entry("empty string", ``, true, nil),
		Entry("no name", `[{"poolName":"some"}]`, true, nil),
		Entry("duplicate name", `[{"name":"req1","poolName":"pool1","poolType":"ippool"},{"name":"req1","poolName":"pool2","poolType":"cidrpool"}]:`, true, nil),
		Entry("uknown field", `[{"name":"req1","poolName":"pool1","badField":"val"}]`, true, nil),
		Entry("bad poolType", `[{"name":"req1","poolName":"pool1","poolType":"whateverpool"}]`, true, nil),
		Entry("bad allocateWithIndex", `[{"name":"req1","poolName":"pool1","poolType":"cidrpool","allocateIPWithIndex":-1}]`, true, nil),
	)
	It("should call CNI ADD with correct parameters", func() {
		rawExec := &fakes.RawExec{}
		rawExec.ExecPluginCall.Returns.ResultBytes = []byte(`{ "cniVersion": "0.4.0", "ips": [ { "version": "4", "address": "192.168.0.5/16", "gateway": "192.168.0.1" } ], "dns": {} }`)
		versionDecoder := &fakes.VersionDecoder{}
		versionDecoder.DecodeCall.Returns.PluginInfo = version.PluginSupports("0.4.0")
		pluginExec := &struct {
			*fakes.RawExec
			*fakes.VersionDecoder
		}{
			RawExec:        rawExec,
			VersionDecoder: versionDecoder,
		}

		allocator := ipallocator.New(
			libcni.NewCNIConfigWithCacheDir([]string{filepath.Join(tmpDir, "/opt/cni/bin")}, filepath.Join(tmpDir, "/cache"), pluginExec),
			"some-pod",
			"some-namespace",
			"6e29ade3-5ec4-436d-a5bd-c53ec3ca816e",
		)
		allocator.FileSystemRoot = tmpDir

		req := ipallocator.NVIPAMIPAllocatorRequest{
			Name:                "req1",
			PoolName:            "some-pool",
			PoolType:            ipallocator.PoolTypeIPPool,
			AllocateIPWIthIndex: ptr.To[int32](2),
		}
		Expect(allocator.Allocate(context.Background(), req)).To(Succeed())
		Expect(rawExec.ExecPluginCall.Received.Environ).To(ContainElement("CNI_COMMAND=ADD"))
		Expect(rawExec.ExecPluginCall.Received.Environ).To(ContainElement("CNI_ARGS=K8S_POD_NAME=some-pod;K8S_POD_NAMESPACE=some-namespace;K8S_POD_UID=6e29ade3-5ec4-436d-a5bd-c53ec3ca816e"))
		Expect(rawExec.ExecPluginCall.Received.StdinData).To(Equal([]byte(`{"args":{"cni":{"allocateIPWithIndex":2}},"cniVersion":"0.4.0","ipam":{"poolName":"some-pool","poolType":"ippool","type":"nv-ipam"},"name":"req1","type":"nv-ipam"}`)))
		Expect(rawExec.FindInPathCall.Received.Paths).To(ContainElement(filepath.Join(tmpDir, ipallocator.CNIBinDir)))
		Expect(rawExec.FindInPathCall.Received.Plugin).To(Equal("nv-ipam"))
	})
	It("should not run CNI ADD if entry already exists in the cache", func() {
		rawExec := &fakes.RawExec{}
		rawExec.ExecPluginCall.Returns.ResultBytes = []byte(`{ "cniVersion": "0.4.0", "ips": [ { "version": "4", "address": "192.168.0.5/16", "gateway": "192.168.0.1" } ], "dns": {} }`)
		versionDecoder := &fakes.VersionDecoder{}
		versionDecoder.DecodeCall.Returns.PluginInfo = version.PluginSupports("0.4.0")
		pluginExec := &struct {
			*fakes.RawExec
			*fakes.VersionDecoder
		}{
			RawExec:        rawExec,
			VersionDecoder: versionDecoder,
		}

		allocator := ipallocator.New(
			libcni.NewCNIConfigWithCacheDir([]string{filepath.Join(tmpDir, "/opt/cni/bin")}, filepath.Join(tmpDir, "/cache"), pluginExec),
			"some-pod",
			"some-namespace",
			"6e29ade3-5ec4-436d-a5bd-c53ec3ca816e",
		)
		allocator.FileSystemRoot = tmpDir

		By("Calling the allocator for the first time, we expect the CNI ADD to happen")
		req := ipallocator.NVIPAMIPAllocatorRequest{
			Name:     "req1",
			PoolName: "some-pool",
			PoolType: ipallocator.PoolTypeIPPool,
		}
		Expect(allocator.Allocate(context.Background(), req)).To(Succeed())
		Expect(rawExec.ExecPluginCall.Received.Environ).To(ContainElement("CNI_COMMAND=ADD"))
		Expect(rawExec.ExecPluginCall.Received.Environ).To(ContainElement("CNI_ARGS=K8S_POD_NAME=some-pod;K8S_POD_NAMESPACE=some-namespace;K8S_POD_UID=6e29ade3-5ec4-436d-a5bd-c53ec3ca816e"))
		Expect(rawExec.ExecPluginCall.Received.StdinData).To(Equal([]byte(`{"cniVersion":"0.4.0","ipam":{"poolName":"some-pool","poolType":"ippool","type":"nv-ipam"},"name":"req1","type":"nv-ipam"}`)))
		Expect(rawExec.FindInPathCall.Received.Paths).To(ContainElement(filepath.Join(tmpDir, ipallocator.CNIBinDir)))
		Expect(rawExec.FindInPathCall.Received.Plugin).To(Equal("nv-ipam"))

		By("Calling the allocator for the second time, we don't expect any CNI call")
		rawExec.ExecPluginCall.Received.Environ = []string{}
		rawExec.ExecPluginCall.Received.StdinData = []byte{}
		rawExec.ExecPluginCall.Received.PluginPath = ""

		Expect(allocator.Allocate(context.Background(), req)).To(Succeed())
		Expect(rawExec.ExecPluginCall.Received.Environ).To(BeEmpty())
		Expect(rawExec.ExecPluginCall.Received.Environ).To(BeEmpty())
		Expect(rawExec.ExecPluginCall.Received.StdinData).To(BeEmpty())
	})
	It("should populate a result file with IP", func() {
		rawExec := &fakes.RawExec{}
		rawExec.ExecPluginCall.Returns.ResultBytes = []byte(`{ "cniVersion": "0.4.0", "ips": [ { "version": "4", "address": "192.168.0.5/16", "gateway": "192.168.0.1" } ], "dns": {} }`)
		versionDecoder := &fakes.VersionDecoder{}
		versionDecoder.DecodeCall.Returns.PluginInfo = version.PluginSupports("0.4.0")
		pluginExec := &struct {
			*fakes.RawExec
			*fakes.VersionDecoder
		}{
			RawExec:        rawExec,
			VersionDecoder: versionDecoder,
		}

		allocator := ipallocator.New(
			libcni.NewCNIConfigWithCacheDir([]string{filepath.Join(tmpDir, "/opt/cni/bin")}, filepath.Join(tmpDir, "/cache"), pluginExec),
			"some-pod",
			"some-namespace",
			"6e29ade3-5ec4-436d-a5bd-c53ec3ca816e",
		)
		allocator.FileSystemRoot = tmpDir
		req := ipallocator.NVIPAMIPAllocatorRequest{
			Name:     "req1",
			PoolName: "some-pool",
			PoolType: ipallocator.PoolTypeIPPool,
		}
		Expect(allocator.Allocate(context.Background(), req)).To(Succeed())

		content, err := os.ReadFile(filepath.Join(tmpDir, "/tmp/ips/req1"))
		Expect(err).ToNot(HaveOccurred())
		Expect(content).To(Equal([]byte(`[{"ip":"192.168.0.5/16","gateway":"192.168.0.1"}]`)))
	})
	It("should overwrite an existing result file with IP", func() {
		rawExec := &fakes.RawExec{}
		rawExec.ExecPluginCall.Returns.ResultBytes = []byte(`{ "cniVersion": "0.4.0", "ips": [ { "version": "4", "address": "192.168.0.5/16", "gateway": "192.168.0.1" } ], "dns": {} }`)
		versionDecoder := &fakes.VersionDecoder{}
		versionDecoder.DecodeCall.Returns.PluginInfo = version.PluginSupports("0.4.0")
		pluginExec := &struct {
			*fakes.RawExec
			*fakes.VersionDecoder
		}{
			RawExec:        rawExec,
			VersionDecoder: versionDecoder,
		}

		allocator := ipallocator.New(
			libcni.NewCNIConfigWithCacheDir([]string{filepath.Join(tmpDir, "/opt/cni/bin")}, filepath.Join(tmpDir, "/cache"), pluginExec),
			"some-pod",
			"some-namespace",
			"6e29ade3-5ec4-436d-a5bd-c53ec3ca816e",
		)
		allocator.FileSystemRoot = tmpDir

		resultFile := filepath.Join(tmpDir, "/tmp/ips/req1")
		Expect(os.MkdirAll(filepath.Dir(resultFile), 0755)).ToNot(HaveOccurred())
		Expect(os.WriteFile(resultFile, []byte("some-content"), 0644)).ToNot(HaveOccurred())

		req := ipallocator.NVIPAMIPAllocatorRequest{
			Name:     "req1",
			PoolName: "some-pool",
			PoolType: ipallocator.PoolTypeIPPool,
		}
		Expect(allocator.Allocate(context.Background(), req)).To(Succeed())

		content, err := os.ReadFile(resultFile)
		Expect(err).ToNot(HaveOccurred())
		Expect(content).To(Equal([]byte(`[{"ip":"192.168.0.5/16","gateway":"192.168.0.1"}]`)))
	})
	It("should call CNI DEL with correct parameters", func() {
		rawExec := &fakes.RawExec{}
		rawExec.ExecPluginCall.Returns.ResultBytes = []byte(`{ "cniVersion": "0.4.0", "ips": [ { "version": "4", "address": "192.168.0.5/16", "gateway": "192.168.0.1" } ], "dns": {} }`)
		versionDecoder := &fakes.VersionDecoder{}
		versionDecoder.DecodeCall.Returns.PluginInfo = version.PluginSupports("0.4.0")
		pluginExec := &struct {
			*fakes.RawExec
			*fakes.VersionDecoder
		}{
			RawExec:        rawExec,
			VersionDecoder: versionDecoder,
		}

		allocator := ipallocator.New(
			libcni.NewCNIConfigWithCacheDir([]string{filepath.Join(tmpDir, "/opt/cni/bin")}, filepath.Join(tmpDir, "/cache"), pluginExec),
			"some-pod",
			"some-namespace",
			"6e29ade3-5ec4-436d-a5bd-c53ec3ca816e",
		)
		allocator.FileSystemRoot = tmpDir

		By("Doing a CNI ADD to populate the cache")
		req := ipallocator.NVIPAMIPAllocatorRequest{
			Name:     "req1",
			PoolName: "some-pool",
			PoolType: ipallocator.PoolTypeIPPool,
		}
		Expect(allocator.Allocate(context.Background(), req)).To(Succeed())
		Expect(rawExec.ExecPluginCall.Received.Environ).To(ContainElement("CNI_COMMAND=ADD"))
		Expect(rawExec.ExecPluginCall.Received.Environ).To(ContainElement("CNI_ARGS=K8S_POD_NAME=some-pod;K8S_POD_NAMESPACE=some-namespace;K8S_POD_UID=6e29ade3-5ec4-436d-a5bd-c53ec3ca816e"))
		Expect(rawExec.ExecPluginCall.Received.StdinData).To(Equal([]byte(`{"cniVersion":"0.4.0","ipam":{"poolName":"some-pool","poolType":"ippool","type":"nv-ipam"},"name":"req1","type":"nv-ipam"}`)))
		Expect(rawExec.FindInPathCall.Received.Paths).To(ContainElement(filepath.Join(tmpDir, ipallocator.CNIBinDir)))
		Expect(rawExec.FindInPathCall.Received.Plugin).To(Equal("nv-ipam"))

		By("Doing a CNI DEL")
		rawExec.ExecPluginCall.Received.Environ = []string{}
		rawExec.ExecPluginCall.Received.StdinData = []byte{}
		rawExec.ExecPluginCall.Received.PluginPath = ""

		Expect(allocator.Deallocate(context.Background(), req)).To(Succeed())
		Expect(rawExec.ExecPluginCall.Received.Environ).To(ContainElement("CNI_COMMAND=DEL"))
		Expect(rawExec.ExecPluginCall.Received.Environ).To(ContainElement("CNI_ARGS=K8S_POD_NAME=some-pod;K8S_POD_NAMESPACE=some-namespace;K8S_POD_UID=6e29ade3-5ec4-436d-a5bd-c53ec3ca816e"))
		Expect(rawExec.ExecPluginCall.Received.StdinData).To(Equal([]byte(`{"cniVersion":"0.4.0","ipam":{"poolName":"some-pool","poolType":"ippool","type":"nv-ipam"},"name":"req1","prevResult":{"cniVersion":"0.4.0","ips":[{"version":"4","address":"192.168.0.5/16","gateway":"192.168.0.1"}],"dns":{}},"type":"nv-ipam"}`)))
		Expect(rawExec.FindInPathCall.Received.Paths).To(ContainElement(filepath.Join(tmpDir, ipallocator.CNIBinDir)))
		Expect(rawExec.FindInPathCall.Received.Plugin).To(Equal("nv-ipam"))
	})
	It("should not run CNI DEL if cache doesn't have entry", func() {
		rawExec := &fakes.RawExec{}
		rawExec.ExecPluginCall.Returns.ResultBytes = []byte(`{ "cniVersion": "0.4.0", "ips": [ { "version": "4", "address": "192.168.0.5/16", "gateway": "192.168.0.1" } ], "dns": {} }`)
		versionDecoder := &fakes.VersionDecoder{}
		versionDecoder.DecodeCall.Returns.PluginInfo = version.PluginSupports("0.4.0")
		pluginExec := &struct {
			*fakes.RawExec
			*fakes.VersionDecoder
		}{
			RawExec:        rawExec,
			VersionDecoder: versionDecoder,
		}

		allocator := ipallocator.New(
			libcni.NewCNIConfigWithCacheDir([]string{filepath.Join(tmpDir, "/opt/cni/bin")}, filepath.Join(tmpDir, "/cache"), pluginExec),
			"some-pod",
			"some-namespace",
			"6e29ade3-5ec4-436d-a5bd-c53ec3ca816e",
		)
		allocator.FileSystemRoot = tmpDir

		req := ipallocator.NVIPAMIPAllocatorRequest{
			Name:     "req1",
			PoolName: "some-pool",
			PoolType: ipallocator.PoolTypeIPPool,
		}
		Expect(allocator.Deallocate(context.Background(), req)).To(HaveOccurred())
		Expect(rawExec.ExecPluginCall.Received.Environ).To(BeEmpty())
		Expect(rawExec.ExecPluginCall.Received.Environ).To(BeEmpty())
		Expect(rawExec.ExecPluginCall.Received.StdinData).To(BeEmpty())
	})
	It("should be able to support more than 1 request", func() {
		rawExec := &fakes.RawExec{}
		rawExec.ExecPluginCall.Returns.ResultBytes = []byte(`{ "cniVersion": "0.4.0", "ips": [ { "version": "4", "address": "192.168.0.5/16", "gateway": "192.168.0.1" } ], "dns": {} }`)
		versionDecoder := &fakes.VersionDecoder{}
		versionDecoder.DecodeCall.Returns.PluginInfo = version.PluginSupports("0.4.0")
		pluginExec := &struct {
			*fakes.RawExec
			*fakes.VersionDecoder
		}{
			RawExec:        rawExec,
			VersionDecoder: versionDecoder,
		}

		allocator := ipallocator.New(
			libcni.NewCNIConfigWithCacheDir([]string{filepath.Join(tmpDir, "/opt/cni/bin")}, filepath.Join(tmpDir, "/cache"), pluginExec),
			"some-pod",
			"some-namespace",
			"6e29ade3-5ec4-436d-a5bd-c53ec3ca816e",
		)
		allocator.FileSystemRoot = tmpDir

		By("Calling Allocate() for the first request")
		req1 := ipallocator.NVIPAMIPAllocatorRequest{
			Name:                "req1",
			PoolName:            "some-pool",
			PoolType:            ipallocator.PoolTypeIPPool,
			AllocateIPWIthIndex: ptr.To[int32](2),
		}
		Expect(allocator.Allocate(context.Background(), req1)).To(Succeed())
		By("Calling Allocate() for the second request")
		req2 := ipallocator.NVIPAMIPAllocatorRequest{
			Name:     "req2",
			PoolName: "some-pool",
			PoolType: ipallocator.PoolTypeIPPool,
		}
		Expect(allocator.Allocate(context.Background(), req2)).To(Succeed())

		By("Calling Deallocate() for the first request")
		Expect(allocator.Deallocate(context.Background(), req1)).To(Succeed())
		By("Calling Deallocate() for the second request")
		Expect(allocator.Deallocate(context.Background(), req2)).To(Succeed())
	})
})
