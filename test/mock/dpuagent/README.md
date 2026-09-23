# mock-dpuagent

`mock-dpuagent` simulates one BlueField DPU for scale testing of the zero-trust provisioning
control plane. One process is one DPU: it serves the BMC Redfish API the provisioning controllers
call (DPUDevice discovery and initialization, Initialize Interface, Config FW Parameters, BF4
Update Firmware, OS Installing, Rebooting) and, once the controllers have installed an OS on it,
impersonates the `dpu-agent` of that DPU: it takes the identity from the install artifact (bf.cfg on
BF3, seed.iso on BF4), performs the real bootstrap token to client certificate exchange against
the management cluster, runs the 28 agent operations as no-ops that write the same
`DPU.status.agentStatus` fields, and joins the DPU cluster as a Ready Node.

No product code is changed; everything lives under `test/mock/dpuagent/`.

## End-to-end test

`test/e2e/mock_dpuagent_test.go` (Ginkgo label `MockDPU`, config `test/e2e/config-mock-dpuagent.yaml`)
verifies the mock itself: it drives simulated DPUs through the whole zero-trust provisioning flow in
kind and checks what the mock left behind in both clusters. Run it after changing anything under
`test/mock/dpuagent/`.

### Components

```
┌─ test host ──────────────┐
│ go test ./test/e2e       │  creates DPUFlavor, DPUDiscovery, BFB / BlueFieldSoftware and DPUSet;
│ Ginkgo label MockDPU     │  verifies DPU status, then Nodes and CSRs in the DPU cluster
└───────────┬──────────────┘
            │ management kubeconfig                               MOCK_DPU_CLUSTER_KUBECONFIG (verify, clean up)
            ▼                                                                               │
┌─ kind cluster dpf-test (management) ─────────────────────────────┐                        │
│                                                                  │                        ▼
│ DPF operator, provisioning controllers, bfb-registry             │  ┌─ kind cluster dpf-test-dpu (DPU cluster) ─┐
│    │ Redfish (443)             ▲ DPU.status patches              │  │ plain kubeadm API server                  │
│    ▼                           │                                 │  │ registered as a static DPUCluster         │
│ mock-dpuagent × N ─────────────┘                                 │  │ (kubeconfig Secret with its node IP)      │
│   one pod = one DPU: BMC Redfish server + dpu-agent replay ──────┼─▶│ Node + Lease per mock DPU                 │
│        ▲ POST /reboot (80)                                       │  └───────────────────────────────────────────┘
│ mock-reboot-controller   (nodeRebootMethod: external)            │
│ mock-bfb-server          (trimmed BFB / PLDM / ISO fixtures)     │
└──────────────────────────────────────────────────────────────────┘
```

Everything runs in two kind clusters on the same docker network. The management cluster is the
usual e2e kind cluster with the DPF operator; the suite configures it in zero-trust mode with only
the provisioning components enabled, pointing `kubernetesAPIServerVIP/Port` at the kind
control-plane node so the mock DPUs can reach the API server. The DPU cluster is a second kind
cluster with kubeadm defaults (cluster-info ConfigMap, bootstrap token auth, kubelet CSR
auto-approval), which is all the simulated kubelets need. It must be a separate cluster: with a
shared `kube-system` the node-join and dpu-agent bootstrap tokens of a DPU collide. The suite
registers it as a static `DPUCluster` through a kubeconfig that uses its node IP, and uses
`MOCK_DPU_CLUSTER_KUBECONFIG` to verify and clean up Nodes there.

The mock reboot controller stands in for the external system that reboots hosts. It watches for the
`dpunode-external-reboot-required` annotation, but only acts once every rebooting DPU of the node
reports `RebootStatus` `Pending` / `WaitingForManualPowerCycleOrReboot`: that is the point where the
DPUNode controller has finished recording the request. Rebooting earlier races that bookkeeping and
provokes a second reboot request, something a real host cannot do because it needs minutes to come
back. The controller then `POST`s `/reboot` to the mock's hostctl endpoint, removes the annotation,
and remembers the DPUNode resource version so stale cache reads do not trigger a second reboot.

### Running it

```sh
make test-env-e2e test-deploy-operator-helm   # management kind cluster with the DPF operator
make test-e2e-mock-dpuagent
```

The second command does everything mock-specific:

1. builds the mock image into the local docker and loads it into the management kind node
   (nothing is pushed);
2. deploys the mock and its reboot controller with the chart, `MOCK_DPUAGENT_REPLICAS` (default 2)
   DPUs;
3. creates the DPU kind cluster `dpf-test-dpu` if it does not exist yet (kubeconfig in
   `artifacts/dpf-test-dpu.kubeconfig`);
4. runs the suite.

Run it again after changing the mock: `kind load` replaces the image behind the tag and the suite
restarts the mock pods at the start of every case. The kind clusters are kept between runs;
`make clean-test-env` deletes both. Before anything else the target compares the host's
`fs.inotify.max_user_instances` and `max_user_watches` with the values kind recommends (512 and
524288) and prints the `sysctl` command to run when they are lower; two kind clusters plus one pod
per mock DPU otherwise fail with `too many open files`. The values are only checked, never changed.

### What a case covers

Each case takes every mock DPU from discovery to Ready, then deletes one DPU and lets the DPUSet
recreate and reprovision it. Along the way the DPUs pass through every phase of the zero-trust
Redfish flow except `Error` and the trusted-host-only `Host Network Configuration`:

| Phase | What happens against the mock |
|---|---|
| DPUDevice discovery and initialization | DPUDiscovery scans the pod IPs; factory reset, BMC password, server CSR / mTLS, firmware check. BF3 starts below the minimum BMC version, so the BMC firmware upgrade and `Manager.Reset` run. |
| Initializing, Pending, Node Effect | DPUSet creates one DPU per node. |
| Initialize Interface | Secure Boot is staged on the mock BMC (the DPUSet asks for it). |
| Perform ARM Force Restart | Two Arm restarts at least 90 s apart (`ComputerSystem.Reset` on BF3, `NvidiaChassis.Reset` on BF4), then Secure Boot is read back as active. Adds about 100 s per case. |
| Config FW Parameters | NIC firmware parameters through Redfish. |
| Update Firmware (BF4 only) | The PLDM bundle is uploaded and activated; the versions switch on the next Arm reset. |
| Prepare BFB, OS Installing | BF3: BFB with `bf.cfg` appended. BF4: OS ISO plus the cloud-init `seed.iso`. Both by `SimpleUpdate` from `bfb-registry`. |
| Rebooting | The agent reports `SLR` on its first run and `PowerCycle` on the second; the mock reboot controller performs both external reboots. |
| DPU Config, DPU Cluster Config | The agent runs its 28 operations, joins the DPU kind cluster as a Node; the controller labels the Node and revokes the join Secret and both bootstrap tokens. |
| Service Readiness, Node Effect Removal, Ready | Host OS init release (reported as skipped by the flavor), then Ready. |
| Deleting | One DPU is deleted; the DPUSet recreates it and it goes through the flow again with a fresh agent identity. Its mock BMC still has Secure Boot enabled, so this time Perform ARM Force Restart is skipped. |

The suite asserts the agent conditions, `kubeletVersion`, `hostOSInit` and `secureBoot` in the DPU
status, the approved CSRs in both clusters, a Ready Node with a fresh Lease in the DPU cluster,
and that the join Secret and bootstrap tokens were revoked.

`MOCK_DPUAGENT_SKIP_SECURE_BOOT=true` leaves Secure Boot unset on the DPUSet, which removes the
Perform ARM Force Restart row and its 100 s per case.

## Scale testing

A scale run points many mock DPUs at a management cluster and a DPU cluster the tester has
prepared, to measure the provisioning control plane rather than to verify the mock. The mock is
only the load generator: the tester owns the clusters, the DPF operator and its
`DPFOperatorConfig`, the `BFB` / `BlueFieldSoftware`, `DPUFlavor`, `DPUDiscovery` and `DPUSet`
objects, and varies them as the scenario requires. This section covers only what is specific to
the mock; everything a real DPU also needs is in the product documentation.

### What the mock needs from its environment

Every mock binds the Redfish server to 443 and the host reboot endpoint to 80, and the ports cannot
be moved: the controller builds some BMC URLs from the bare BMC IP without a port (for example
`https://<bmcIP>/redfish/v1/.../GenerateCSR`). Each mock therefore needs its own IP, which is the
BMC IP the controllers discover: a pod IP in containers, the VM IP in a VM. With `hostNetwork` a
node could run only one mock and the ports would collide with anything else on the node, so the
chart uses the pod network.

### Build

```sh
make docker-build-mock-dpuagent PUSH=true REGISTRY=<registry> TAG=<tag>   # container image with mock-dpuagent and mock-reboot-controller
make binary-mock-dpuagent ARCH=arm64                                        # bin/mock-dpuagent alone, for a VM image
```

### Running in containers

The chart in `test/mock/dpuagent/chart` deploys the fleet. Every value the chart accepts is in
`test/mock/dpuagent/chart/values-scale.yaml`, a complete and commented example for a scale run;
copy it, edit it and pass it to helm:

```sh
helm upgrade --install mock-dpuagent test/mock/dpuagent/chart \
  --namespace dpf-operator-system --create-namespace \
  -f my-values.yaml
```

[embedmd]:#(chart/values-scale.yaml)
```yaml
# Example values for a scale run of mock-dpuagent against a real management cluster. Every replica
# is one simulated DPU with its own pod IP as BMC IP. All replicas share this config, so one
# release is one DPU type; see the "Scale testing" section of test/mock/dpuagent/README.md.

# -- replicas is the number of simulated DPUs. Every pod is one DPU with its own pod IP as BMC IP.
replicas: 200

image:
  # -- image.repository is the mock-dpuagent container image, built with `make docker-build-mock-dpuagent`.
  repository: <registry>/mock-dpuagent
  # -- image.tag is the mock-dpuagent image tag. Defaults to the chart appVersion when empty.
  tag: <tag>

# -- imagePullSecrets are the pull secrets added to the mock and the reboot controller pods.
imagePullSecrets:
  - name: dpf-pull-secret

# Rendered verbatim into the ConfigMap mounted at /etc/mock-dpuagent/config.yaml and shared by
# every replica. See test/mock/dpuagent/pkg/config/config.go for the fields.
mockConfig:
  # -- mockConfig.dpuType is required and selects the Redfish personality and the install flow the mock emulates: bf3 installs a BFB, bf4 installs an OS ISO and applies a PLDM bundle.
  dpuType: bf3
  bmc:
    # -- mockConfig.bmc.psid is the PSID the BMC reports. bf4 only: it must be a key of BlueFieldSoftware.spec.pldmFwBundle, and the bundle at that key must carry an image for this PSID.
    psid: MT_0000001775
    firmware:
      # -- mockConfig.bmc.firmware.bmc is the BMC firmware version reported at power-on. bf3 only: below BF-24.10-17 every DPU goes through the BMC firmware upgrade and Manager.Reset before OS install, equal to it skips that path. This example takes the upgrade path.
      bmc: BF-24.07-10
      # -- mockConfig.bmc.firmware.erot is the ERoT firmware version reported at power-on. On bf4 the controller compares erot, uefi, nic and bmc to the PLDM bundle and applies the bundle on any mismatch; the mock-0.0 placeholder never matches, so Update Firmware always runs. Setting all four to the bundle's versions makes the controller skip it. On bf3 it is only reported.
      erot: mock-0.0
      # -- mockConfig.bmc.firmware.uefi is the UEFI firmware version reported at power-on, compared like erot.
      uefi: mock-0.0
      # -- mockConfig.bmc.firmware.nic is the NIC firmware version reported at power-on, compared like erot.
      nic: mock-0.0
    # -- (object) mockConfig.bmc.responseDelay holds every Redfish response back for a uniformly random number of whole seconds between `minSeconds` and `maxSeconds`, to imitate a slow BMC. Only the Redfish port is affected, not the host reboot endpoint. Unset or `maxSeconds: 0` means no extra latency. Operations listed in responseDelayOverrides use their own range instead. This example adds up to one second to every response.
    responseDelay:
      minSeconds: 0
      maxSeconds: 1
    # -- (list) mockConfig.bmc.responseDelayOverrides give single Redfish operations their own delay range instead of responseDelay. Each element has `name` (an operation name from the "Redfish response delays" table in the README), optional `methods` (a list of HTTP methods, omitted or `["*"]` means every method the operation serves), `minSeconds` and `maxSeconds`; a zero range means no delay for that operation. Unknown names, methods the operation does not serve and two elements covering the same operation and method stop the mock at startup. This example makes the System read, which a real BMC answers slowly, take one to two seconds.
    responseDelayOverrides:
      - name: System
        minSeconds: 1
        maxSeconds: 2
  agent:
    # -- mockConfig.agent.rebootMethod is a comma separated list of NoAction, PowerCycle and SLR. Each agent run reports the next element; once the list is exhausted every run reports NoAction. PowerCycle and SLR wait for the host reboot, see "Host reboot" in the README. This example makes every DPU go through both external reboots.
    rebootMethod: SLR,PowerCycle,NoAction
    # -- mockConfig.agent.dpuClusterJoin selects how the mock joins the DPU cluster. simulated (containers) replays kubeadm join and kubelet node registration in-process with client-go; kubelet only starts a kubelet the VM image has already configured, see "Running in a VM" in the README.
    dpuClusterJoin: simulated

# -- resources are the container resources of every mock DPU pod. The CPU request is sized for the short peaks between OS Installing and DPU Config rather than the idle average, so the scheduler does not pack more mocks on a node than it can serve when several DPUs install at once; a throttled mock would show up as control plane latency. Leave the CPU limit unset, see "Measured resource usage" in the README.
resources:
  requests:
    cpu: 100m
    memory: 64Mi
  limits:
    memory: 256Mi

# -- nodeSelector places the mock DPU pods. Keep the fleet off the nodes that run the DPF controllers so the mocks do not compete with the control plane under test.
nodeSelector:
  dpf.nvidia.com/mock-dpu-pool: "true"
# -- tolerations of the mock DPU pods.
tolerations: []
# -- affinity of the mock DPU pods.
affinity: {}

# The mock reboot controller plays the external system of nodeRebootMethod external.
rebootController:
  # -- rebootController.enabled deploys the mock reboot controller that performs host reboots for DPUNodes with nodeRebootMethod external. Set to false when a custom script drives the host reboot instead.
  enabled: true
  image:
    # -- rebootController.image.repository is the reboot controller image. It ships in the same image as the mock.
    repository: <registry>/mock-dpuagent
    # -- rebootController.image.tag is the reboot controller image tag. Defaults to the chart appVersion when empty.
    tag: <tag>
  serviceAccount:
    # -- rebootController.serviceAccount.annotations are added to the reboot controller ServiceAccount.
    annotations: {}
  # -- rebootController.resources are the container resources of the reboot controller pod.
  resources:
    requests:
      cpu: 10m
      memory: 64Mi
```

`mockConfig` is the mock's own configuration file; the chart only wraps it in a ConfigMap. All replicas
share it, so one release is one DPU type and a mixed BF3/BF4 fleet is not supported. Changing
`mockConfig` rolls every pod: the pods come back with new IPs and no state, so treat it as redeploying
the fleet. Firmware versions reported after an install or an upgrade are parsed from the artifacts
the controller sends and are not configurable.

### Redfish response delays

A real BMC answers some calls in milliseconds and others, such as `UpdateService.SimpleUpdate` or
the `Systems` reads, in many seconds. `mockConfig.bmc.responseDelay` sets one range for every
Redfish response; `mockConfig.bmc.responseDelayOverrides` gives single operations their own range.
Each override names an operation from the table below and, optionally, the HTTP methods it covers:

```yaml
mockConfig:
  bmc:
    responseDelay:                 # default for every call
      minSeconds: 1
      maxSeconds: 3
    responseDelayOverrides:
      - name: SecureBoot
        methods: [GET]             # omitted or ["*"]: every method the operation serves
        minSeconds: 1
        maxSeconds: 2
      - name: SecureBoot
        methods: [PATCH]
        minSeconds: 5
        maxSeconds: 8
      - name: Task                 # a zero range: this call is never delayed
        minSeconds: 0
        maxSeconds: 0
      - name: UpdateService.SimpleUpdate
        minSeconds: 20
        maxSeconds: 30
```

A response waits a uniformly random number of whole seconds in the range of its override, or of
`responseDelay` when no override covers it. The mock checks the overrides at startup and refuses to
start when a name is not in the table, a method is not one the operation serves (the error lists
the methods it does serve), or two overrides cover the same operation and method. `*` cannot be
combined with other methods in one list. Only the Redfish port is delayed, not the host reboot
endpoint.

The operation names come from the route table in `test/mock/dpuagent/pkg/redfish/routes.go`.
Redfish actions are named after the action, other resources after the resource, and every method
served on one path shares the name. `{system}`, `{chassis}`, `{manager}` and the other
placeholders accept any value.

| Name | Methods | URL |
|---|---|---|
| ServiceRoot | GET | `/redfish/v1` and `/redfish/v1/` |
| Systems | GET | `/redfish/v1/Systems` |
| Managers | GET | `/redfish/v1/Managers` |
| Manager | GET | `/redfish/v1/Managers/{manager}` |
| Chassis | GET | `/redfish/v1/Chassis/{chassis}` |
| System | GET | `/redfish/v1/Systems/{system}` |
| SystemOem | GET | `/redfish/v1/Systems/{system}/Oem/Nvidia` |
| Bios | GET | `/redfish/v1/Systems/{system}/Bios` |
| BiosSettings | PATCH | `/redfish/v1/Systems/{system}/Bios/Settings` |
| Mode.Set | POST | `/redfish/v1/Systems/{system}/Oem/Nvidia/Actions/Mode.Set` |
| SecureBoot | GET, PATCH | `/redfish/v1/Systems/{system}/SecureBoot` |
| SELEntries | GET | `/redfish/v1/Systems/{system}/LogServices/SEL/Entries` |
| SystemSettings | GET, PATCH | `/redfish/v1/Systems/{system}/Settings` |
| NetworkDeviceFunction | GET | `/redfish/v1/Chassis/{chassis}/NetworkAdapters/{adapter}/NetworkDeviceFunctions/{pf}` |
| ComputerSystem.Reset | POST | `/redfish/v1/Systems/{system}/Actions/ComputerSystem.Reset` |
| SOC.ForceReset | POST | `/redfish/v1/Systems/{system}/Oem/Nvidia/SOC.ForceReset` |
| NvidiaChassis.Reset | POST | `/redfish/v1/Chassis/{chassis}/Actions/Oem/NvidiaChassis.Reset` |
| HostRshim.Set | POST | `/redfish/v1/Systems/{system}/Oem/Nvidia/Actions/HostRshim.Set` |
| BMCRshim | GET, PATCH | `/redfish/v1/Managers/Bluefield_BMC/Oem/Nvidia` |
| HostPrivilegeConfig | PATCH | `/redfish/v1/Chassis/BlueField_0/NetworkAdapters/BlueField_NIC_0/Oem/Nvidia/HostPrivilegeConfig/Settings` |
| Account | PATCH | `/redfish/v1/AccountService/Accounts/{user}` |
| AccountService | PATCH | `/redfish/v1/AccountService` |
| Manager.ResetToDefaults | POST | `/redfish/v1/Managers/{manager}/Actions/Manager.ResetToDefaults` |
| Manager.Reset | POST | `/redfish/v1/Managers/{manager}/Actions/Manager.Reset` |
| CertificateService.GenerateCSR | POST | `/redfish/v1/CertificateService/Actions/CertificateService.GenerateCSR` |
| CertificateService.ReplaceCertificate | POST | `/redfish/v1/CertificateService/Actions/CertificateService.ReplaceCertificate` |
| ServerCertificate | GET | `/redfish/v1/Managers/{manager}/NetworkProtocol/HTTPS/Certificates/1` |
| TruststoreCertificates | GET, POST, DELETE | `/redfish/v1/Managers/{manager}/Truststore/Certificates` and `.../Certificates/{id}` |
| UpdateService | GET, POST | `/redfish/v1/UpdateService` |
| UpdateService.SimpleUpdate | POST | `/redfish/v1/UpdateService/Actions/UpdateService.SimpleUpdate` |
| FirmwareInventory | GET | `/redfish/v1/UpdateService/FirmwareInventory/{id}` |
| UpdateMultipart | POST | `/redfish/v1/UpdateService/update-multipart` |
| UpdateService.Activate | POST | `/redfish/v1/UpdateService/Actions/UpdateService.Activate` |
| Task | GET | `/redfish/v1/TaskService/Tasks/{id}` |
| VirtualMedia | GET, POST | `/redfish/v1/Managers/{manager}/VirtualMedia/{media}` and `.../VirtualMedia/{media}/Actions/{action}` |

### Running in a VM

In a VM (for example under KubeVirt) the mock runs as a plain process and a real kubelet joins the
DPU cluster. The mock does not build or configure that VM; the image is the VM owner's deliverable
and this is the contract it has to meet.

**What the VM image must contain**

- The `mock-dpuagent` binary (`make binary-mock-dpuagent ARCH=<arch>`).
- The mock configuration at `/etc/mock-dpuagent/config.yaml`: only the `mockConfig` block of the chart
  values from "Running in containers", written as a file, with `agent.dpuClusterJoin: kubelet`.
- A `kubelet.service` already configured to join the DPU cluster. The mock only runs
  `systemctl start kubelet`; it configures nothing.
- A guest host name whose last five characters are unique across the fleet, because the serial
  number is derived from them (see "Serial number" at the end). With KubeVirt, keep the guest
  hostname equal to the VMI name: `VirtualMachineInstanceReplicaSet` and `VirtualMachinePool` names
  both satisfy the rule, an image default such as `localhost` gives every VM the same serial.

**Network and reboot**

The VM IP is the BMC IP; ports 443 and 80 must be reachable from the management cluster and the
`DPUDiscovery` range must cover the VM IPs. `POST /reboot` on port 80 makes the mock run the agent
again exactly as in containers; it does not reboot the VM. The reboot controller and the custom
script option below work unchanged.

### Host reboot

A DPU that reports `SLR` or `PowerCycle` waits for its host to reboot. The mock has no host: the
reboot is simulated by `POST /reboot` on port 80 of the pod (hostctl), after which the mock
behaves as if the Arm had booted again and runs the agent once more. Two ways to drive it:

- **Mock reboot controller** (chart default, `rebootController.enabled: true`) with
  `nodeRebootMethod: external` on the DPUNodes. It reacts to the `dpunode-external-reboot-required`
  annotation with the timing described in the End-to-end test section.
- **Custom script** (`nodeRebootMethod: script`, set `rebootController.enabled: false`). The
  controller creates the Job only after it has recorded the reboot request, so the script can call
  the endpoint immediately and exit 0. The endpoint accepts an optional JSON body
  `{"method": "<reboot method>"}` and answers `200` with `{"status": "accepted"}`. The Job gets the
  DPUNode name in `DPUNODE_NAME`; its DPUs are `DPUNode.spec.dpus[].name`, which are `DPUDevice`
  names, and the pod IP is `DPUDevice.spec.bmcIp`:

  ```sh
  for dev in $(kubectl -n dpf-operator-system get dpunode "$DPUNODE_NAME" -o jsonpath='{.spec.dpus[*].name}'); do
    ip=$(kubectl -n dpf-operator-system get dpudevice "$dev" -o jsonpath='{.spec.bmcIp}')
    curl -sf -X POST "http://$ip/reboot" -H 'Content-Type: application/json' -d "{\"method\":\"$DPUNODE_REBOOT_METHOD\"}"
  done
  ```

### Recommended DPFOperatorConfig

```yaml
apiVersion: operator.dpu.nvidia.com/v1alpha1
kind: DPFOperatorConfig
metadata:
  name: dpfoperatorconfig
  namespace: dpf-operator-system
spec:
  deploymentMode: zero-trust
  imagePullSecrets:
    - dpf-pull-secret
  overrides:
    kubernetesAPIServerVIP: <management API server address reachable from the mock pods>
    kubernetesAPIServerPort: 6443
  provisioningController:
    replicas: 1
    bfbPVCName: bfb-pvc
    installInterface:
      installViaRedfish:
        skipDPUNodeDiscovery: false
        discoveredDPUDeviceBMCFactoryResetPolicy: OnInitialization
  dpuServiceController:
    replicas: 1
  staticClusterManager:
    disable: false
    replicas: 1
  kamajiClusterManager:
    disable: true
  nodeSRIOVDevicePluginController:
    disable: true
  dpuDetector:
    disable: true
  monitoring:
    disable: true
  serviceSetController:
    disable: true
  sfcController:
    disable: true
  multus:
    disable: true
  sriovDevicePlugin:
    disable: true
  flannel:
    disable: true
  ovsCNI:
    disable: true
  nvipam:
    disable: true
  cniInstaller:
    disable: true
  coreDNS:
    disable: true
  security:
    kata:
      disable: true
```

## Regenerating the fixtures

The fixtures are the test files under `test/mock/dpuagent/testdata/`, served to the controllers and
the mock BMCs by the e2e's `mock-bfb-server`:

| File | What it is | Who reads it |
|---|---|---|
| `bf3-trimmed.bfb` | A real BFB cut down to the headers and the software inventory. | `bfb-registry` parses the versions; the mock BMC streams it and splits off the appended `bf.cfg`. |
| `bf4-trimmed.fwpkg` | A real PLDM bundle cut down to its header and component table. | `bfb-registry` parses the versions; the mock BMC applies it during Update Firmware. |

Trimming keeps the files small enough for a ConfigMap and leaves only what the parsers read. The
mock reports the firmware and software versions parsed from these files and the e2e asserts those
exact values, so regenerate both together whenever the parsers or the expected versions change:

```sh
go run ./test/mock/dpuagent/testdata/gen --bfb <real.bfb> --pldm <real.fwpkg> --out test/mock/dpuagent/testdata
```

BF4 also involves two ISOs, neither of which is a fixture:

- **OS ISO** (`BlueFieldSoftware.spec.osIso`): the e2e generates 64 KiB of random bytes named
  `bf4-os-doca-bundle-3.3.0-420_mock.iso` at run time. The mock never reads the OS image; it
  streams it to `/dev/null` and reports the file name as the installed OS version.
- **seed.iso** (cloud-init `cidata` volume with the per-DPU `user-data`): built by the provisioning
  controller in the Prepare BFB phase and sent to the mock as a separate `SimpleUpdate` target. The
  mock does parse this one, since it carries the dpu-agent identity and the management cluster
  address.

## Measured resource usage

Reference numbers for sizing `resources` in the chart values, measured with five mock DPUs
provisioned from real BFB, PLDM and ISO files. Per-container CPU and working-set memory were
sampled from Prometheus every 15 seconds from pod start to Ready.

| | BF3 fleet | BF4 fleet |
|---|---|---|
| Time from DPUSet creation to all five DPUs Ready | 72 s | 91 s |
| Memory, average per pod | 12 MiB | 10 MiB |
| Memory, peak of any pod | 19 MiB | 20 MiB |
| CPU, average per pod | 0.006 cores | 0.008 cores |
| CPU, peak of any pod (1 min rate) | 0.09 cores | 0.19 cores |

Memory stays flat while the mock streams the BFB or ISO, because the payload is parsed on the fly
and discarded, so the `64Mi` request and `256Mi` limit in `values-scale.yaml` leave a wide margin.
The CPU peaks happen in the few seconds between OS Installing and DPU Config, and Prometheus
averages CPU over one minute, so second-level peaks are higher than the numbers above. The `100m`
request in `values-scale.yaml` is sized for that peak rather than the idle average: with a lower
request the scheduler packs more mocks on a node than it can serve when several DPUs install at
once, and a throttled mock shows up as control plane latency in the measurement. Leave the CPU
limit unset; without one the request only affects scheduling weight and the peaks can use idle
capacity.

## Serial number

The mock derives its BMC serial number from the host name at process start: `MT`, the two-digit
ISO year and ISO week, `X`, and the last five characters of the host name in upper case
(left-padded with `X` when the host name is shorter). Pod `mock-dpuagent-59fd6c885c-cg6kh` started
in week 38 of 2026 becomes `MT2638XCG6KH`. Deployment pod names end in five random characters that
are unique within the ReplicaSet, so containers need nothing extra and a serial can be matched to
its pod by eye; in a VM the guest host name has to provide the unique tail. The lower-cased serial
must be a valid DNS-1123 subdomain, because the controller uses it as the DPUDevice name and the
DPU Agent identity; the mock refuses to start otherwise. The PF0 MAC address is derived from the
serial.
