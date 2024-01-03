# dpf-standalone Local Dev

This guide helps the developer in setting and running a local dpf-standalone env that can be used to deploy the various DPF components and run tests. Allow user to run full provisioning and service chaining flows.

## Prerequisites
1. [GO](https://go.dev/doc/install) >= 1.22
2. [Docker Engine](https://docs.docker.com/engine/install/)
3. [dpf-standalone prerequisites](https://gitlab-master.nvidia.com/doca-platform-foundation/dpf-standalone#prerequisites)

## Dev Flow
1. Go to your local dev project git repo
```
cd <path_to_local_repo/doca-platform-foundation
```
2. Export relevant env vars for image building and pushing
```
export REGISTRY=gitlab-master.nvidia.com:5005/doca-platform-foundation/doca-platform-foundation/e2e
export IMAGE_PULL_KEY=<GITLAB_API_TOKEN>
export TAG=v0.1.0-$(git rev-parse --short HEAD)-$USER-test
```
3. Generate all 
```
make generate
```
4. Build images required for the DPF e2e test.
```
make test-release-e2e-quick

# If you plan to run the e2e testing suite with the full provisioning flow (see documentation below), you will need to
# create a full release instead:
make release
```
5. SSH to local machine
6. Export Variables
```
export NGC_API_KEY=<NGC_API_KEY>
export REGISTRY=gitlab-master.nvidia.com:5005/doca-platform-foundation/doca-platform-foundation/e2e
export IMAGE_PULL_KEY=<GITLAB_API_TOKEN>
export TAG=v0.1.0-$(git rev-parse --short HEAD)-$USER-test
```
_Note:_
NGC API Key can be generated from org.ngc.nvidia.com/setup

7. docker login to NGC Catalog # can use podman as well
```
echo "$NGC_API_KEY" | docker login --username \$oauthtoken --password-stdin nvcr.io
```
8. Pull latest dpf-standalone image
```
docker pull nvcr.io/nvstaging/doca/dpf-standalone
```
9. Run dpf-standalone # you will be required to provide the host root password to begin installation
```
docker run -ti --rm --net=host nvcr.io/nvstaging/doca/dpf-standalone \
  -k -K -u root \
  -e ngc_key=$NGC_API_KEY \
  -e '{"deploy_dpf_operator_chart": false}' \
  -e '{"deploy_dpf_bfb_pvc": false}' \
  -e '{"deploy_dpf_create_node_feature_rule": false}' \
  -e '{"deploy_dpf_create_operator_config": false}' \
  install.yml
```
10. Create a directory for BFB images
```
mkdir -p /bfb-images
```
11. Copy a BFB file from DOCA Release dir to local /bfb-images dir. For example:
```
cp /auto/sw_mc_soc_release/doca_dpu/doca_2.7.0/GA/bfbs/qp/bf-bundle-2.7.0-33_24.04_ubuntu-22.04_prod.bfb /bfb-images
```
12. On the target machine - Go to your local dev project git repo
```
cd <path_to_local_repo/doca-platform-foundation
```
13. Generate image pull secrets
```
hack/scripts/create-artefact-secrets.sh
```
14. Deploy DPF Operator
```
make test-deploy-operator-helm
```

### Run e2e Tests
To run DPF e2e tests:
15. Export dpf-standalone-e2e testing vars
```
# This indicates whether the full provisioning flow should be validated. If you set it, you need to make a full release
# before executing the tests.
export DPF_E2E_NUM_DPU_NODES=1
```
16. Run e2e tests
```
make test-e2e
```
_Note:_
If you want to keep the created resources (e.g., `DPFOperatorConfig`, `DPU` and such) you can use the environment variable `E2E_SKIP_CLEANUP=true`
```
E2E_SKIP_CLEANUP=true make test-e2e
```

### Provisioning Flow
To test full provisioning flow, follow steps 1-8.

_Notes:_

1. a host reboot will happen during the provisioning, so it is not possible to provision the local host
(the host from which the dpf-standalone command is executed). You should use dpf-standalone from another host with
-e target_host argument.
2. BFB file name and URL can change based on your DPU instance - Production or Development.

#### Use Latest Image From Remote Registry
9. Run dpf-standalone # you will be required to provide the host root password to begin installation
```
docker run -ti --rm --net=host nvcr.io/nvstaging/doca/dpf-standalone \
  -k -K -u root \
  -e ngc_key=$NGC_API_KEY \
  -e '{"test_service_bfb_file_name": "bf-bundle-2.7.0-33_24.04_ubuntu-22.04_prod.bfb", "test_service_bfb_download_url": "http://nbu-nfs.mellanox.com/auto/sw_mc_soc_release/doca_dpu/doca_2.7.0/GA/bfbs/pk", "deploy_test_service": true, "target_host": "<target-host-ip-here>"}' \
  install.yml
```

This command will run the entire provisioning flow. No further steps are needed.

#### Use Local Image
9. Run dpf-standalone # you will be required to provide the host root password to begin installation
```
docker run -ti --rm --net=host nvcr.io/nvstaging/doca/dpf-standalone \
  -k -K -u root \
  -e ngc_key=$NGC_API_KEY \
  -e '{"deploy_dpf_operator_chart": false, "deploy_dpf_bfb_pvc": false, "target_host": "<target-host-ip-here>"}' \
  install.yml
```
Continue with steps 10-14 and then:

15. Create PVC for BFB:
```
cat <<'EOF' | kubectl create -f -
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: bfb
  namespace: dpf-operator-system
spec:
  accessModes:
    - ReadWriteOnce
  resources:
    requests:
      storage: 10Gi
  volumeMode: Filesystem
EOF
 ```
16. Create DPFOperatorConfig CR:
```
cat <<'EOF' | kubectl create -f -
apiVersion: operator.dpu.nvidia.com/v1alpha1
kind: DPFOperatorConfig
metadata:
  name: dpfoperatorconfig
  namespace: dpf-operator-system
spec:
  provisioningController:
    bfbPVCName: "bfb"
    dmsTimeout: 900
EOF
```
17. Create [DPUCluster CR](../cluster-manager/dev-guide.md):
```
cat <<'EOF' | kubectl create -f -
apiVersion: provisioning.dpu.nvidia.com/v1alpha1
kind: DPUCluster
metadata:
  name: dpu-cluster-0
  namespace: dpu-cplane-tenant1
spec:
  type: nvidia
  maxNodes: 1
  version: v1.29.0
  clusterEndpoint:
    keepalived:
      #interface: ens9f0
      interface: br-dpu
      virtualRouterID: 126
      vip: "10.33.33.34"
      nodeSelector:
        node-role.kubernetes.io/control-plane: ""
EOF
```
18. Create BFB CR:
```
cat <<'EOF' | kubectl create -f -
apiVersion: provisioning.dpu.nvidia.com/v1alpha1
kind: Bfb
metadata:
  name: bf-bundle-2.7.0
  namespace: dpf-operator-system
spec:
  # fileName must use ".bfb" extension
  fileName: "bf-bundle-2.7.0-33.bfb"
  # the URL to download bfb file - using internal HTTP server
  url: "http://bfb-server.dpf-operator-system/bf-bundle-2.7.0-33_24.04_ubuntu-22.04_prod.bfb"
EOF
```
18. Create DPUSet CR:
```
cat <<'EOF' | kubectl create -f -
apiVersion: provisioning.dpu.nvidia.com/v1alpha1
kind: DpuSet
metadata:
  name: dpuset-1
  namespace: dpf-operator-system
spec:
  nodeSelector:
    matchLabels:
      feature.node.kubernetes.io/dpu-enabled: "true"
  strategy:
    rollingUpdate:
      maxUnavailable: "10%"
    type: RollingUpdate
  dpuTemplate:
    spec:
      dpuFlavor: dpf-provisioning-hbn-ovn
      automaticNodeReboot: true
      BFB:
        bfb: "bf-bundle-2.7.0"
      nodeEffect:
        taint:
          key: "dpu"
          value: "provisioning"
          effect: NoSchedule
      cluster:
        name: "dpu-cplane-tenant1"
        namespace: "dpu-cplane-tenant1"
EOF
```

After applying DPUSet CR, the provisioning process should start.
More Information can be found in [DPU Provisioning](https://gitlab-master.nvidia.com/doca-platform-foundation/dpf-standalone#dpu-provisioning)

### Test DPU Services
To test full provisioning flow, follow steps 1-14 and then follow [DPU Services](https://gitlab-master.nvidia.com/doca-platform-foundation/dpf-standalone#deploy-test-service-on-dpu)

### Clean dpf-standalone env
To clean the env run:
```
docker run --rm --net=host nvcr.io/nvstaging/doca/dpf-standalone:latest -u root reset.yml
```
## Troubleshooting
