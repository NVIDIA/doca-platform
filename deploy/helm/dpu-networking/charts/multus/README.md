# multus

![Version: 0.1.0](https://img.shields.io/badge/Version-0.1.0-informational?style=flat-square) ![Type: application](https://img.shields.io/badge/Type-application-informational?style=flat-square) ![AppVersion: 0.1.0](https://img.shields.io/badge/AppVersion-0.1.0-informational?style=flat-square)

A Helm chart for Kubernetes

## Values

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| imagePullSecrets | list | `[]` |  |
| kubeMultusDs.installMultusBinary.containerSecurityContext.privileged | bool | `true` |  |
| kubeMultusDs.installMultusBinary.image.repository | string | `"ghcr.io/k8snetworkplumbingwg/multus-cni"` |  |
| kubeMultusDs.installMultusBinary.image.tag | string | `"v3.9.3"` |  |
| kubeMultusDs.installMultusBinary.resources.requests.cpu | string | `"10m"` |  |
| kubeMultusDs.installMultusBinary.resources.requests.memory | string | `"15Mi"` |  |
| kubeMultusDs.kubeMultus.containerSecurityContext.privileged | bool | `true` |  |
| kubeMultusDs.kubeMultus.image.repository | string | `"ghcr.io/k8snetworkplumbingwg/multus-cni"` |  |
| kubeMultusDs.kubeMultus.image.tag | string | `"v3.9.3"` |  |
| kubeMultusDs.kubeMultus.resources.limits.cpu | string | `"100m"` |  |
| kubeMultusDs.kubeMultus.resources.limits.memory | string | `"50Mi"` |  |
| kubeMultusDs.kubeMultus.resources.requests.cpu | string | `"100m"` |  |
| kubeMultusDs.kubeMultus.resources.requests.memory | string | `"50Mi"` |  |
| kubernetesClusterDomain | string | `"cluster.local"` |  |
| multus.serviceAccount.annotations | object | `{}` |  |
| multusDaemonConfig.daemonConfigJson | string | `"{\n  \"name\": \"multus-cni-network\",\n  \"type\": \"multus\",\n  \"capabilities\": {\n    \"portMappings\": true\n  },\n  \"delegates\": [\n    {\n      \"cniVersion\": \"0.3.1\",\n      \"name\": \"default-cni-network\",\n      \"plugins\": [\n        {\n          \"type\": \"flannel\",\n          \"name\": \"flannel.1\",\n            \"delegate\": {\n              \"isDefaultGateway\": true,\n              \"hairpinMode\": true\n            }\n          },\n          {\n            \"type\": \"portmap\",\n            \"capabilities\": {\n              \"portMappings\": true\n            }\n          }\n      ]\n    }\n  ],\n  \"kubeconfig\": \"/etc/cni/net.d/multus.d/multus.kubeconfig\"\n}"` |  |
| networkAttachmentBrHBNJson | string | `"{\n  \"cniVersion\": \"0.4.0\",\n  \"type\": \"ovs\",\n  \"mtu\": 1500,\n  \"bridge\": \"br-hbn\",\n  \"interface_type\": \"dpdk\"\n}"` |  |
| networkAttachmentBrSFCJson | string | `"{\n  \"cniVersion\": \"0.4.0\",\n  \"type\": \"ovs\",\n  \"bridge\": \"br-sfc\",\n  \"mtu\": 1500,\n  \"interface_type\": \"dpdk\",\n  \"ipam\": {\n    \"type\": \"nv-ipam\"\n  }\n}"` |  |
| networkAttachmentIpRequestJson | string | `"{\n  \"cniVersion\": \"0.4.0\",\n  \"type\": \"dummy\",\n  \"ipam\": {\n    \"type\": \"nv-ipam\"\n  }\n}"` |  |

