# spire-agent-rbac

![Version: 0.1.0](https://img.shields.io/badge/Version-0.1.0-informational?style=flat-square) ![Type: application](https://img.shields.io/badge/Type-application-informational?style=flat-square) ![AppVersion: 0.1.0](https://img.shields.io/badge/AppVersion-0.1.0-informational?style=flat-square)

DPU cluster RBAC for the SPIRE agent Kubernetes workload attestor

## Values

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| name | string | `"dpf-spire-kubelet-pods"` | name is the name given to both the ClusterRole and the ClusterRoleBinding. It is fixed rather than derived from the release name because both objects are cluster scoped and are referenced by name when debugging workload attestation. |
| subjectGroup | string | `"system:nodes"` | subjectGroup is the group the ClusterRole is bound to. The SPIRE agent runs on the DPU host OS and authenticates to the DPU cluster with the kubelet client certificate, so it reaches the apiserver as a member of system:nodes. |

