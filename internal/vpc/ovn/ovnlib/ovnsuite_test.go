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

package ovnlib_test

import (
	"context"
	"os/exec"
	"testing"
	"time"

	"github.com/nvidia/doca-platform/internal/vpc/ovn/ovnlib"

	"github.com/kelseyhightower/envconfig"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/ovn-org/libovsdb/client"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

type Config struct {
	ImageName string `envconfig:"OVN_CENTRAL_IMAGE_NAME" default:"ovn-central:v24.09.0"`
}

var (
	ovnClient ovnlib.OVNWrapper
	ctx       context.Context
)

var _ = BeforeSuite(func() {
	// Set up a logger
	logger := zap.New(zap.UseDevMode(true))
	ctrllog.SetLogger(logger)

	ctx = ctrllog.IntoContext(context.Background(), logger)
	var err error

	// Parse env variables
	var config Config
	err = envconfig.Process("", &config)
	Expect(err).NotTo(HaveOccurred(), "failed to parse environment variables: %v", err)

	// Run Docker container
	cmd := exec.Command("docker", "run", "--rm", "--name", "ovn-central", "-d", "-p", "6641:6641", config.ImageName, "bash", "-c",
		"/usr/share/ovn/scripts/ovn-ctl --db-nb-create-insecure-remote=yes --db-sb-create-insecure-remote=yes start_northd && tail -f /dev/null")
	output, err := cmd.CombinedOutput()
	Expect(err).NotTo(HaveOccurred(), "failed to run Docker container: %s", output)

	// Wait for container to be ready
	time.Sleep(5 * time.Second)

	ovnNBConfig := &ovnlib.Config{
		EndPoint:           "tcp:127.0.0.1:6641",
		OVNNBReconnectTime: 5,
	}
	ovnClient, err = ovnlib.GetOvnNBClient(ctx, ovnNBConfig, []client.Option{})
	Expect(err).NotTo(HaveOccurred(), "failed to create OVN client")
})

var _ = AfterSuite(func() {
	ovnClient.Close()
	// Stop and remove Docker container
	cmd := exec.Command("docker", "stop", "ovn-central")
	output, err := cmd.CombinedOutput()
	Expect(err).NotTo(HaveOccurred(), "Failed to stop Docker container: %s", output)
})

var _ = AfterEach(func() {
	_ = ovnClient.ClearAll(ctx)
})

func TestOvnLibrary(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "OVN Library Suite")
}
