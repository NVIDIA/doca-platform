---
title: "Secondary Network Support for HBN-OVNK Use Case"
---

[TOC]

This section covers an **advanced configuration** of the secondary network feature of the
[Host Based Networking and OVN Kubernetes](../../user-guides/host-trusted/use-cases/hbn-ovnk/README.md)
use case. Enabling and configuring this feature allows for the creation of pods with multiple network interfaces,
where the secondary networks are also accelerated by OVN Kubernetes.


Before proceeding with this advanced configuration, please ensure you have reviewed the
[Host Based Networking and OVN Kubernetes](../../user-guides/host-trusted/use-cases/hbn-ovnk/README.md)
configuration guide first and completed that first. This advanced configuration builds upon that setup
and provides additional steps to enable secondary network support.


## 1. Upgrade OVN Kubernetes from the Helm Chart

Upgrade the OVN Kubernetes CNI components from the helm chart.
Ensure [environment variables](../../user-guides/host-trusted/use-cases/hbn-ovnk/README.md#0-required-variables)
are set before running this command.


```shell
envsubst < manifests/01-cni-installation/ovn-kubernetes_secondary_network.yml | helm upgrade --install -n ovn-kubernetes ovn-kubernetes ${OVN_KUBERNETES_REPO_URL}/ovn-kubernetes-chart --version ${OVN_KUBERNETES_CHART_TAG} --values -
```

<details markdown="1"><summary>**OVN-Kubernetes Helm values**</summary>

[embedmd]:#(manifests/01-cni-installation/ovn-kubernetes_secondary_network.yml)
```yml
commonManifests:
  enabled: true
nodeWithoutDPUManifests:
  enabled: true
controlPlaneManifests:
  enabled: true
nodeWithDPUManifests:
  enabled: true
  nodeMgmtPortDpResourceName: nvidia.com/ovnk-mgmt-vf
  dpuServiceAccountNamespace: dpf-operator-system
global:
  enableMultiNetwork: "true" # enables secondary network support
gatewayOpts: --gateway-interface=derive-from-mgmt-port
## Note this CIDR is followed by a trailing /24 which informs OVN Kubernetes on how to split the CIDR per node.
podNetwork: $POD_CIDR/24
serviceNetwork: $SERVICE_CIDR
k8sAPIServer: https://$TARGETCLUSTER_API_SERVER_HOST:$TARGETCLUSTER_API_SERVER_PORT
```

</details>

### Verification

These verification commands may need to be run multiple times to ensure the condition is met.

Verify the CNI installation with:
```shell
## Ensure all nodes in the cluster are ready.
kubectl wait --for=condition=ready nodes --all
## Ensure all pods in the ovn-kubernetes namespace are ready.
kubectl wait --for=condition=ready --namespace ovn-kubernetes pods --all --timeout=300s
```

## 2. Update the DPUServiceTemplate

Ensure [environment variables](../../user-guides/host-trusted/use-cases/hbn-ovnk/README.md#0-required-variables) are set before running this command.
```shell
cat manifests/02-dpudeployment-modifications/*.yaml | envsubst | kubectl apply -f -
```

<details markdown="1"><summary>**OVN DPUServiceTemplate to deploy OVN workloads to the DPUs**</summary>

[embedmd]:#(manifests/02-dpudeployment-modifications/dpuservicetemplate_ovn_secondary_network.yaml)
```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceTemplate
metadata:
  name: ovn
  namespace: dpf-operator-system
spec:
  deploymentServiceName: "ovn"
  helmChart:
    source:
      repoURL: $OVN_KUBERNETES_REPO_URL
      chart: ovn-kubernetes-chart
      version: $OVN_KUBERNETES_CHART_TAG
    values:
      commonManifests:
        enabled: true
      dpuManifests:
        enabled: true
      global:
        enableMultiNetwork: "true" # enables secondary network support
      leaseNamespace: "ovn-kubernetes"
      gatewayOpts: "--gateway-interface=br-dpu"
```

</details>

### Verification

These verification commands may need to be run multiple times to ensure the condition is met.

Note that the DPUService name will have a random suffix. For example, `ovn-hbn-doca-hbn-l2xsl`. Use the correct name for the verification.

Verify the DPU and Service installation with:
```shell
## Ensure the DPUServices are created and have been reconciled.
kubectl wait --for=condition=ApplicationsReconciled --namespace dpf-operator-system dpuservices -l svc.dpu.nvidia.com/owned-by-dpudeployment=dpf-operator-system_ovn-hbn
## Ensure the DPUServiceIPAMs have been reconciled
kubectl wait --for=condition=DPUIPAMObjectReconciled --namespace dpf-operator-system dpuserviceipam --all
## Ensure the DPUServiceInterfaces have been reconciled
kubectl wait --for=condition=ServiceInterfaceSetReconciled --namespace dpf-operator-system dpuserviceinterface --all
## Ensure the DPUServiceChains have been reconciled
kubectl wait --for=condition=ServiceChainSetReconciled --namespace dpf-operator-system dpuservicechain --all
```

## 3. Test Traffic

If you want to create pods with secondary networks, first create a secondary network NetworkAttachmentDefinition.

```shell
kubectl apply -f manifests/03-test-traffic/nad_bf3_p0_vfs.yaml
```

Now you can create pods with secondary network interfaces using the following command:

```shell
kubectl apply -f manifests/03-test-traffic/pods-secondary-network.yaml
```

Once the pods are running, you can check the network interfaces inside the pods to verify that the secondary network interfaces have been created. They should be created with the interface name `net1` and should have an IP address from the `192.168.100.0/24` as defined in the NetworkAttachmentDefinition.

You can ping or run iperf traffic between the pods using the secondary network interfaces to test connectivity and performance.

