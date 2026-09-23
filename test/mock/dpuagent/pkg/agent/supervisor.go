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

package agent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/internal/provisioning/dpuagent/operations"
	"github.com/nvidia/doca-platform/test/mock/dpuagent/pkg/artifact"
	"github.com/nvidia/doca-platform/test/mock/dpuagent/pkg/config"
	"github.com/nvidia/doca-platform/test/mock/dpuagent/pkg/node"
	"github.com/nvidia/doca-platform/test/mock/dpuagent/pkg/redfish"

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	clientset "k8s.io/client-go/kubernetes"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/klog/v2"
	crclient "sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	// DefaultWorkDir is the fixed directory where identities and credentials live. It is a code
	// constant on purpose: nothing in the configuration file differs between instances.
	DefaultWorkDir = "/var/lib/mock-dpuagent"

	seedISOFile = "seed.iso"

	// osBootDelay is how long the Arm reports OsStarting before the agent comes up.
	osBootDelay = 3 * time.Second
	// bootstrapRetryInterval paces retries of identity bootstrap while the API is unreachable, the
	// way systemd would restart a crashed dpu-agent.
	bootstrapRetryInterval = 10 * time.Second
)

// Supervisor turns the two physical DPU events, power-on into a new OS and reboot, into agent
// runs. It implements redfish.Supervisor and hostctl.HostRebooter. All state lives in memory: a
// restarted mock process is a DPU back in its factory state, waiting for the controller to install
// an OS; a VM that lost power is not simulated.
type Supervisor struct {
	cfg     *config.Config
	methods []provisioningv1.RebootMethodType
	workDir string
	bmc     *redfish.State
	scheme  *runtime.Scheme

	mu          sync.Mutex
	cancel      context.CancelFunc
	done        chan struct{}
	identityDir string
	identity    *Identity
	cursor      int
	joiner      *node.Joiner
}

// NewSupervisor validates the configuration and prepares the work directory.
func NewSupervisor(cfg *config.Config, workDir string, bmc *redfish.State) (*Supervisor, error) {
	methods, err := cfg.RebootMethods()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(workDir, 0o700); err != nil {
		return nil, fmt.Errorf("create work dir %s: %w", workDir, err)
	}
	if cfg.Agent.DPUClusterJoin == config.JoinKubelet {
		for _, bin := range []string{"kubelet", "systemctl"} {
			if _, err := lookPath(bin); err != nil {
				return nil, fmt.Errorf("agent.dpuClusterJoin=kubelet requires %s on this host: %w", bin, err)
			}
		}
	}
	scheme := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(provisioningv1.AddToScheme(scheme))
	return &Supervisor{cfg: cfg, methods: methods, workDir: workDir, bmc: bmc, scheme: scheme}, nil
}

// Boot is the DPU powering into a freshly installed OS. artifact is the bf.cfg (BF3) or the
// cidata seed.iso (BF4) the install flow delivered; the dpu-agent identity is taken from it.
func (s *Supervisor) Boot(ctx context.Context, art []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stopRunLocked()
	s.bmc.SetOSStarting()

	id, dir, err := s.installIdentity(art)
	if err != nil {
		// Like a real DPU whose install failed: the previously installed OS, if any, boots with
		// its old agent identity and the controller sees a DPU that never picks up the new one.
		if s.identity == nil {
			klog.ErrorS(err, "boot failed: the install artifact does not carry a usable dpu-agent identity and no OS was installed before; the Arm stays up without an agent")
			s.bmc.SetPowerOn()
			return
		}
		klog.ErrorS(err, "boot failed: the install artifact does not carry a usable dpu-agent identity; booting the previously installed OS with its old agent", "dpu", s.identity.DPUName)
		s.powerCycleLocked(ctx)
		s.startRunLocked(ctx)
		return
	}
	s.identity, s.identityDir, s.cursor = id, dir, 0
	s.joiner = node.NewJoiner(dir, id.DPUName)
	klog.InfoS("DPU booting into the installed OS", "dpu", id.DPUName, "namespace", id.DPUNamespace, "uid", id.DPUUID, "dir", dir)
	s.powerCycleLocked(ctx)
	s.startRunLocked(ctx)
}

// Reboot restarts an already installed DPU: the running agent is interrupted, the Arm goes through
// OsStarting and the agent runs again with the same identity.
func (s *Supervisor) Reboot(ctx context.Context) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stopRunLocked()
	s.powerCycleLocked(ctx)
	if s.identity == nil {
		klog.InfoS("DPU rebooted before any OS was installed; no agent to start")
		return
	}
	klog.InfoS("DPU rebooted", "dpu", s.identity.DPUName)
	s.startRunLocked(ctx)
}

// HostReboot is the external host power cycle or reboot: firmware the BMC activated takes effect,
// then the DPU reboots.
func (s *Supervisor) HostReboot(ctx context.Context, method string) {
	if s.bmc.ApplyActivatedBundle() {
		klog.InfoS("host reboot applied the activated PLDM bundle")
	}
	klog.InfoS("host reboot", "method", method)
	go s.Reboot(ctx)
}

// Stop interrupts the current run and waits for it.
func (s *Supervisor) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stopRunLocked()
}

func (s *Supervisor) installIdentity(art []byte) (*Identity, string, error) {
	var userData []byte
	var err error
	switch s.cfg.DPUType {
	case config.DPUTypeBF4:
		userData, err = artifact.UserDataFromSeedISO(art, filepath.Join(s.workDir, seedISOFile))
	default:
		userData, err = artifact.UserDataFromBFCFG(art)
	}
	if err != nil {
		return nil, "", err
	}
	files, err := artifact.AgentFilesFromUserData(userData)
	if err != nil {
		return nil, "", err
	}
	id, err := ParseAgentConf(files.AgentConf)
	if err != nil {
		return nil, "", err
	}
	dir := filepath.Join(s.workDir, id.DPUUID)
	if _, err := Materialize(files, dir); err != nil {
		return nil, "", err
	}
	return id, dir, nil
}

func (s *Supervisor) powerCycleLocked(ctx context.Context) {
	s.bmc.SetOSStarting()
	select {
	case <-time.After(osBootDelay):
	case <-ctx.Done():
	}
	s.bmc.SetPowerOn()
}

func (s *Supervisor) stopRunLocked() {
	if s.cancel == nil {
		return
	}
	s.cancel()
	<-s.done
	s.cancel, s.done = nil, nil
}

func (s *Supervisor) startRunLocked(ctx context.Context) {
	runCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	s.cancel, s.done = cancel, done
	in := &runInputs{
		rebootMethod: s.nextRebootMethodLocked(),
		powerOff:     s.bmc.SetPowerOff,
		joinMode:     s.cfg.Agent.DPUClusterJoin,
		joiner:       s.joiner,
	}
	id, dir := s.identity, s.identityDir
	go func() {
		defer close(done)
		s.run(runCtx, id, dir, in)
	}()
}

// nextRebootMethodLocked consumes the next element of agent.rebootMethod; past the end every run
// reports NoAction.
func (s *Supervisor) nextRebootMethodLocked() provisioningv1.RebootMethodType {
	method := provisioningv1.RebootMethodNoAction
	if s.cursor < len(s.methods) {
		method = s.methods[s.cursor]
	}
	s.cursor++
	return method
}

// run is one agent lifetime: bootstrap the identity, run the pipeline, then stay idle until the
// context ends (reboot, reprovision or process shutdown).
func (s *Supervisor) run(ctx context.Context, id *Identity, dir string, in *runInputs) {
	klog.InfoS("dpu-agent starting", "dpu", id.DPUName, "rebootMethod", in.rebootMethod)
	var optCtx *operations.Context
	err := wait.PollUntilContextCancel(ctx, bootstrapRetryInterval, true, func(ctx context.Context) (bool, error) {
		var err error
		optCtx, err = s.buildContext(ctx, id, dir)
		if err != nil {
			klog.ErrorS(err, "dpu-agent bootstrap failed, retrying", "dpu", id.DPUName)
			return false, nil
		}
		return true, nil
	})
	if err != nil {
		return
	}
	runner := NewRunner(optCtx, buildOperations(in))
	if err := runner.Run(ctx); err != nil {
		if ctx.Err() != nil {
			klog.InfoS("dpu-agent run interrupted", "dpu", id.DPUName)
			return
		}
		klog.ErrorS(err, "dpu-agent run failed", "dpu", id.DPUName)
		return
	}
	klog.InfoS("dpu-agent successfully completed all operations", "dpu", id.DPUName)
	<-ctx.Done()
}

func (s *Supervisor) buildContext(ctx context.Context, id *Identity, dir string) (*operations.Context, error) {
	cfg, err := Bootstrap(ctx, dir, id)
	if err != nil {
		return nil, err
	}
	dpuClient, err := crclient.NewWithWatch(cfg, crclient.Options{Scheme: s.scheme})
	if err != nil {
		return nil, fmt.Errorf("create controller-runtime client: %w", err)
	}
	k8sClient, err := clientset.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("create kubernetes clientset: %w", err)
	}
	return &operations.Context{
		Client:      dpuClient,
		WatchClient: dpuClient,
		K8sClient:   k8sClient,
		Options:     id.Options(),
	}, nil
}
