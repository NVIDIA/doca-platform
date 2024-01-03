## DPF Operator

The DPF Operator is responsible for installing the system components of DPF and configuring OVN Kubernetes. 
Today's demo covers only the system installation. OVN Kubernetes configuration was covered previously [here.](https://nvidia-my.sharepoint.com/:v:/r/personal/vremmas_nvidia_com/Documents/Recordings/%5BDEMO%5D%20DPF_%20Custom%20OVN%20Kubernetes%20Deployment-20240502_113137-Meeting%20Recording.mp4?csf=1&web=1&e=V4AwBr)

### User flow

#### 0) Operator Prerequisites

- Clean install of OCP.
- Kamaji installed
- Kamaji TenantControlPlane deployed and working
- PVC created for DPF Provisioning controller
- operator-sdk CLI installed


Starting from an environment with the prerequisites installed and the default `kubeconfig` pointing to the OCP cluster.
```bash 
kubectl get -n kamaji-system pods
kubectl get tenantcontrolplanes -n dpu-cplane-tenant1
kubectl get pvc -n dpf-operator-system
kubectl get -n dpf-operator-system pods
```

#### 1) Install the operator bundle:

Deploy the operator using operator-sdk

```bash
operator-sdk run bundle --namespace dpf-operator-system --index-image quay.io/operator-framework/opm:v1.39.0 harbor.mellanox.com/cloud-orchestration-dev/dpf/dpf-operator-bundle:0.0.0-nightly
kubectl -n dpf-operator-system get pods
```

#### 2) Create the DPFOperatorConfig

```yaml
apiVersion: operator.dpu.nvidia.com/v1alpha1
kind: DPFOperatorConfig
metadata:
  name: dpfoperatorconfig
  namespace: dpf-operator-system
spec:
  ## Config for the provisioning controller.
  provisioningConfiguration: 
    bfbPVCName: "bfb-pvc" # Matches existing PVC
    imagePullSecret: "dpf-pull-secret" # Matches existing secret. Not required for this demo.
  ## Used for options the user is advised against - e.g. disabling system components
  overrides:
     disableOVNKubernetesReconcile: true
   ## Config for the ovn-kubernetes configuration.
  ## Not covered in this demo.
  hostNetworkConfiguration:
    hostPF0: ens2f0np0
    hostPF0VF0: enp23s0f0v0
    cidr: 10.0.96.0/20
    hosts:
    - hostClusterNodeName: ocp-worker-1
      dpuClusterNodeName: dpu-worker-1
      hostIP: 10.0.96.10/24
      dpuIP: 10.0.96.20/24
      gateway: 10.0.96.254
    - hostClusterNodeName: ocp-worker-2
      dpuClusterNodeName: dpu-worker-2
      hostIP: 10.0.97.10/24
      dpuIP: 10.0.97.20/24
      gateway: 10.0.97.254
```


### Operator flow

1) Mirror ImagePullSecrets to DPUCluster
Secrets are mirrored to the DPUCluster to ensure they're available to DPUServices.

2) Deploy system components on OCP cluster
    1. **ArgoCD**
    2. **DPUService controller-manager** including DPUService, DPUServiceChain, DPUServiceIPAM and DPUServiceInterface controllers
    3. **DPFProvisioning controller-manager** including BFB, DPUSet and DPU controllers
   
3) Deploy system components as DPUServices
   1. **ServiceFunctionChainSet controller**
   2. **SR-IOV device plugin**
   3. **Multus**
   4. **Flannel**
   5. **NVIPAM**
   6. **SFC CNI****
   7. **ServiceFunctionChain controller****
   
      ** Not yet implemented.


DPF Operator creates DPUServices for components destined for the DPUCluster. The DPUService controller creates ArgoCD Applications on the OCP cluster. ArgoCD reconciles these applications from helm charts to Kubernetes objects on the DPUClusters.


In action:

```bash
cat <<EOF | kubectl apply -f -
apiVersion: operator.dpu.nvidia.com/v1alpha1
kind: DPFOperatorConfig
metadata:
  name: dpfoperatorconfig
  namespace: dpf-operator-system
spec:
  ## Config for the provisioning controller.
  provisioningConfiguration: 
    bfbPVCName: "bfb-pvc"
    imagePullSecret: "dpf-pull-secret"
  ## Config for the ovn-kubernetes configuration.
  hostNetworkConfiguration:
    hostPF0: ens2f0np0
    hostPF0VF0: enp23s0f0v0
    cidr: 10.0.96.0/20
    hosts:
    - hostClusterNodeName: ocp-worker-1
      dpuClusterNodeName: dpu-worker-1
      hostIP: 10.0.96.10/24
      dpuIP: 10.0.96.20/24
      gateway: 10.0.96.254
    - hostClusterNodeName: ocp-worker-2
      dpuClusterNodeName: dpu-worker-2
      hostIP: 10.0.97.10/24
      dpuIP: 10.0.97.20/24
      gateway: 10.0.97.254
  ## Used for options the user is advised against - e.g. disabling system components
  overrides:
    disableOVNKubernetesReconcile: true 
EOF

kubectl get -n dpf-operator-system  dpfoperatorconfig
watch kubectl get -n dpf-operator-system pods,dpuservices,applications
```

```bash
kubectl get secrets -n dpu-cplane-tenant1   dpu-cplane-tenant1-admin-kubeconfig -o json | jq -r '.data["admin.conf"]'   | base64 --decode   > killian/conf
kubectl get dpuservices -A
kubectl --kubeconfig killian/conf get -n dpf-operator-system deployments
kubectl --kubeconfig killian/conf get -n dpf-operator-system daemonsets
```


## Clean up 
Delete the DPF Operator config

```bash
kubectl delete -n dpf-operator-system dpfoperatorconfig --all --wait=false
watch kubectl get -n dpf-operator-system pods,dpuservices,applications,dpfoperatorconfig
```

Delete the DPF Operator

```bash 
operator-sdk cleanup -n dpf-operator-system dpf-operator
watch kubectl get pods -n dpf-operator-system
```
