# mock-dpuagent

![Version: 0.1.0](https://img.shields.io/badge/Version-0.1.0-informational?style=flat-square) ![Type: application](https://img.shields.io/badge/Type-application-informational?style=flat-square) ![AppVersion: 0.1.0](https://img.shields.io/badge/AppVersion-0.1.0-informational?style=flat-square)

A Helm chart for mock-dpuagent, N simulated BlueField DPUs (Redfish BMC + dpu-agent) for provisioning control plane scale tests

## Values

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| affinity | object | `{}` | affinity of the mock DPU pods. |
| image.repository | string | `"example.com/mock-dpuagent"` | image.repository is the mock-dpuagent container image, built with `make docker-build-mock-dpuagent`. |
| image.tag | string | `"v0.1.0"` | image.tag is the mock-dpuagent image tag. Defaults to the chart appVersion when empty. |
| imagePullSecrets | list | `[]` | imagePullSecrets are the pull secrets added to the mock and the reboot controller pods. |
| mockConfig.agent.dpuClusterJoin | string | `"simulated"` | mockConfig.agent.dpuClusterJoin selects how the mock joins the DPU cluster. simulated (containers) replays kubeadm join and kubelet node registration in-process with client-go; kubelet only starts a kubelet the VM image has already configured, see "Running in a VM" in the README. |
| mockConfig.agent.rebootMethod | string | `"NoAction"` | mockConfig.agent.rebootMethod is a comma separated list of NoAction, PowerCycle and SLR. Each agent run reports the next element; once the list is exhausted every run reports NoAction. PowerCycle and SLR wait for the host reboot, see "Host reboot" in the README. |
| mockConfig.bmc.firmware.bmc | string | `"BF-24.10-17"` | mockConfig.bmc.firmware.bmc is the BMC firmware version reported at power-on. bf3 only: below BF-24.10-17 every DPU goes through the BMC firmware upgrade and Manager.Reset before OS install, equal to it skips that path. |
| mockConfig.bmc.firmware.erot | string | `"mock-0.0"` | mockConfig.bmc.firmware.erot is the ERoT firmware version reported at power-on. On bf4 the controller compares erot, uefi, nic and bmc to the PLDM bundle and applies the bundle on any mismatch; the mock-0.0 placeholder never matches, so Update Firmware always runs. Setting all four to the bundle's versions makes the controller skip it. On bf3 it is only reported. |
| mockConfig.bmc.firmware.nic | string | `"mock-0.0"` | mockConfig.bmc.firmware.nic is the NIC firmware version reported at power-on, compared like erot. |
| mockConfig.bmc.firmware.uefi | string | `"mock-0.0"` | mockConfig.bmc.firmware.uefi is the UEFI firmware version reported at power-on, compared like erot. |
| mockConfig.bmc.psid | string | `"MT_0000001775"` | mockConfig.bmc.psid is the PSID the BMC reports. bf4 only: it must be a key of BlueFieldSoftware.spec.pldmFwBundle, and the bundle at that key must carry an image for this PSID. |
| mockConfig.bmc.responseDelay | object | `{"maxSeconds":1,"minSeconds":0}` | mockConfig.bmc.responseDelay holds every Redfish response back for a uniformly random number of whole seconds between `minSeconds` and `maxSeconds`, to imitate a slow BMC. Only the Redfish port is affected, not the host reboot endpoint. Unset or `maxSeconds: 0` means no extra latency. Operations listed in responseDelayOverrides use their own range instead. The default adds up to one second to every response. |
| mockConfig.bmc.responseDelayOverrides | list | `[{"maxSeconds":2,"minSeconds":1,"name":"System"}]` | mockConfig.bmc.responseDelayOverrides give single Redfish operations their own delay range instead of responseDelay. Each element has `name` (an operation name from the "Redfish response delays" table in the README), optional `methods` (a list of HTTP methods, omitted or `["*"]` means every method the operation serves), `minSeconds` and `maxSeconds`; a zero range means no delay for that operation. Unknown names, methods the operation does not serve and two elements covering the same operation and method stop the mock at startup. The default makes the System read, which a real BMC answers slowly, take one to two seconds. |
| mockConfig.dpuType | string | `"bf3"` | mockConfig.dpuType is required and selects the Redfish personality and the install flow the mock emulates: bf3 installs a BFB, bf4 installs an OS ISO and applies a PLDM bundle. |
| nodeSelector | object | `{}` | nodeSelector places the mock DPU pods, for example on a dedicated pool so they do not compete with the DPF controllers under test. |
| rebootController.enabled | bool | `true` | rebootController.enabled deploys the mock reboot controller that performs host reboots for DPUNodes with nodeRebootMethod external. Set to false when a custom script drives the host reboot instead. |
| rebootController.image.repository | string | `"example.com/mock-dpuagent"` | rebootController.image.repository is the reboot controller image. It ships in the same image as the mock. |
| rebootController.image.tag | string | `"v0.1.0"` | rebootController.image.tag is the reboot controller image tag. Defaults to the chart appVersion when empty. |
| rebootController.resources | object | `{"requests":{"cpu":"10m","memory":"64Mi"}}` | rebootController.resources are the container resources of the reboot controller pod. |
| rebootController.serviceAccount.annotations | object | `{}` | rebootController.serviceAccount.annotations are added to the reboot controller ServiceAccount. |
| replicas | int | `1` | replicas is the number of simulated DPUs. Every pod is one DPU with its own pod IP as BMC IP. |
| resources | object | `{"requests":{"cpu":"100m","memory":"64Mi"}}` | resources are the container resources of every mock DPU pod. The CPU request is sized for the short peaks between OS Installing and DPU Config rather than the idle average, see "Measured resource usage" in the README. |
| tolerations | list | `[]` | tolerations of the mock DPU pods. |

