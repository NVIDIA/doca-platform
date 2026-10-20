---
title: "Air-Gapped Deployment"
---

[[_TOC_]]

## Scope

In an air-gapped deployment, container images and helm charts are pulled from a mirror registry that
you host inside your network. Nothing redirects pulls in the background: you configure every image
the DPU pulls with the reference of its copy in the mirror, and what a pod spec names is what the
DPU pulls.

This guide covers the DPU, which then reaches `Ready` without internet access:

| Target                    | Covered here                                                              |
|---------------------------|---------------------------------------------------------------------------|
| DPU cluster               | Yes: the container images the DPU pulls, and the containerd sandbox image |
| Host (management) cluster | No: it keeps its internet access (planned for a later release)            |

The host cluster keeps pulling its own images and the helm charts (the dpf-operator chart, the
helmfile prerequisites, and the DPUService charts that Argo CD pulls) from their upstream
locations. The BFB reaches the DPU through the provisioning flow of the host cluster, so it is not
affected either.

## Overview

1. [Prepare a mirror registry](#1-prepare-a-mirror-registry) that the DPU can reach.
2. [Copy the images into the mirror](#2-copy-the-images-into-the-mirror) with `dpfctl airgap images`
   and `skopeo` or `crane`.
3. [Point the DPU images at the mirror](#3-point-the-dpu-images-at-the-mirror) in the
   `DPFOperatorConfig`, the `DPUFlavor` and your DPUServices.
4. [Install packages from your apt repository](#4-install-packages-from-your-apt-repository), if
   your `DPUFlavor` installs packages.
5. [Check the result](#5-check-the-result).

You need [`dpfctl`](../../operational-readiness/troubleshooting/dpfctl/airgap.md) and
[helm](https://helm.sh) v3 on a machine with internet access and access to the mirror.

## 1. Prepare a mirror registry

You need an OCI registry that the DPU can reach while the internet is cut off. Any registry that
implements the OCI distribution specification works, provided it supports:

* **OCI image indexes (multi-arch images).** The copy keeps all architectures, and the DPU pulls
  the `arm64` one.
* **Anonymous pulls**, at least of the images listed in [Mirror authentication](#mirror-authentication).

Harbor, JFrog Artifactory, Sonatype Nexus, Red Hat Quay and Zot generally meet these
requirements. Your registry may need some setup before copying into it succeeds, such as creating
the project, organization or repository to copy into, or granting push permission to the account
you copy with. For example, Harbor does not create projects on push: create the project named by
the first path component of the mirror (`dpf` in `mirror.example.com/dpf`) before you copy.

The CNCF Distribution registry (`registry:2`) also works; use it only for development and test.

### Mirror TLS

The DPU OS ships with the standard public CA bundle, so a mirror served over HTTPS with a publicly
trusted certificate needs no configuration on the DPU.

For a certificate of a private CA, configure how containerd on the DPU connects to the mirror
host with a `hosts.toml` file, written by the `configFiles` of the `DPUFlavor`. It configures only
how containerd connects to the mirror host; it does not redirect any other registry.

```yaml
# DPUFlavor (spec excerpt): containerd trusts a private CA for the mirror
spec:
  configFiles:
    - operation: override
      path: /etc/containerd/certs.d/mirror.example.com/ca.crt
      permissions: "0644"
      raw: |
        -----BEGIN CERTIFICATE-----
        ...your CA certificate...
        -----END CERTIFICATE-----
    - operation: override
      path: /etc/containerd/certs.d/mirror.example.com/hosts.toml
      permissions: "0644"
      raw: |
        server = "https://mirror.example.com"

        [host."https://mirror.example.com"]
          capabilities = ["pull", "resolve"]
          ca = "/etc/containerd/certs.d/mirror.example.com/ca.crt"
```

The directory name is the mirror host with its port, if any, for example
`/etc/containerd/certs.d/mirror.example.com:5000/`.

A test mirror that serves plain HTTP, such as `registry:2` without TLS, needs the `http://` scheme
in its `hosts.toml`. Without that scheme, containerd connects with HTTPS, and every pull fails:

```yaml
# DPUFlavor (spec excerpt): containerd pulls from a plain-HTTP test mirror
spec:
  configFiles:
    - operation: override
      path: /etc/containerd/certs.d/mirror.example.com:5000/hosts.toml
      permissions: "0644"
      raw: |
        server = "http://mirror.example.com:5000"

        [host."http://mirror.example.com:5000"]
          capabilities = ["pull", "resolve"]
```

Copy into such a mirror with `skopeo copy --dest-tls-verify=false`, or with `crane copy --insecure`.

> [!NOTE]
> `configFiles` of the default type `cloud-init` cannot change once the `DPUFlavor` exists. To
> change the mirror CA, `hosts.toml` or the apt source of an existing installation, create a new
> `DPUFlavor` and point the `DPUDeployment` at it. The new `DPUFlavor` reprovisions the DPUs. A
> file of `type: agent-applied` stays editable. The DPU agent writes it from a ConfigMap in the
> namespace of the DPUs (`contentFrom.configMapKeyRef`), before it installs packages and starts
> kubelet.

containerd reads these files because the containerd configuration of the BFB sets
`config_path = "/etc/containerd/certs.d"`. Leave `spec.containerdConfig.registryEndpoint` of the
`DPUFlavor` unset: it configures a mirror for `nvcr.io` only, and on containerd 1.x the DPU agent
then replaces `config_path` with an inline mirror, so containerd stops reading
`/etc/containerd/certs.d`.

apt on the DPU ([step 4](#4-install-packages-from-your-apt-repository)) does not read the files of
containerd. If your apt repository uses HTTPS with a certificate of a private CA, point apt at the
CA file for the host of the repository. Write the host without its port:

```yaml
# DPUFlavor (spec excerpt): apt trusts the private CA for the host of your apt repository
spec:
  configFiles:
    - operation: override
      path: /etc/apt/apt.conf.d/99airgap-mirror
      permissions: "0644"
      raw: |
        Acquire::https::mirror.example.com::CaInfo "/etc/containerd/certs.d/mirror.example.com/ca.crt";
```

To trust the CA on the whole DPU OS instead, add the CA to the `ca.crt` key of the
`dpf-ca-trust-bundle` ConfigMap. Do this on the host cluster before DPF provisions the DPU. DPF
checks its own provisioning connections against the same ConfigMap, so they then also trust
certificates of your CA.

### Mirror authentication

containerd and the DPU cluster pull some images without credentials:

* the containerd sandbox image (`pause`);
* the `kube-proxy` and CoreDNS images of a Kamaji DPU cluster.

The mirror must allow anonymous pulls of these images. The DPF components on the DPU get the image
pull secrets of `spec.imagePullSecrets` of the `DPFOperatorConfig`, and DPUServices get those
described in [Using Private Registries](../../advanced-configuration/using-private-registries.md).
Anonymous pulls of all DPU images is the simpler setup.

## 2. Copy the images into the mirror

Every DPF release ships an air-gap bill of materials (BOM) inside its `dpf-operator` helm chart, at
`airgap/airgapped.yaml`. It lists the container images and helm charts of the release:

```yaml
schemaVersion: v1alpha1
images:                         # needed by every installation
  - name: dpf-system            # a DPF image: name only
    scope: [dpu, host]
  - ref: ghcr.io/flannel-io/flannel:v0.27.2@sha256:782c111d...
    scope: [dpu]
  - ref: registry.k8s.io/pause:3.9
    scope: [dpu]
    sandbox: true
services:                       # optional DPF charts: mirror only those you deploy
  - name: dpf-storage           # a DPF chart: name only
    images:
      - name: storage-system
        scope: [dpu, host]
      - ref: nvcr.io/nvidia/doca/doca_vfs:1.8.0-doca3.5.0
        scope: [dpu]
charts:                         # the helm charts, which the host cluster pulls from upstream
  - name: dpf-operator
```

* **`images`** are pulled by every installation.
  * DPF images are listed by `name` only. Their registry and tag are those you install DPF from.
    For example, DPF releases are published to `nvcr.io/nvidia/doca`, so the entry
    `name: dpf-system` is the image `nvcr.io/nvidia/doca/dpf-system`, tagged with the version of
    the release.
  * Third-party images are listed by their full reference, mostly pinned by digest.
* **`services`** are the optional DPF charts deployed as DPUServices, the DPF storage chart and the
  VPC charts, with the images they pull. Mirror only those you deploy. Other DPUServices, such as
  those of the use-case guides, are not in the BOM, because the BOM lists only what DPF itself
  deploys. List their images as in [Images of your DPUServices](#images-of-your-dpuservices).
* **`scope`** is the cluster that pulls an image: `dpu`, `host` or both. A DPU air gap needs the
  images with `dpu` in their scope.
* **`sandbox: true`** marks the containerd sandbox image (see [The sandbox image](#the-sandbox-image)).
* **`kamajiAddon: true`** marks the `kube-proxy` and CoreDNS images of a Kamaji DPU cluster (see
  [Kamaji DPU cluster addons](#kamaji-dpu-cluster-addons)).

You do not need to read the BOM yourself: `dpfctl airgap images` reads it from the chart and lists
what to copy, one `source destination` line per image. Every image is copied to
`<mirror>/<name>:<tag>`: `ghcr.io/flannel-io/flannel:v0.27.2` is copied to
`mirror.example.com/dpf/flannel:v0.27.2`. This is the reference you configure in
[step 3](#3-point-the-dpu-images-at-the-mirror). To keep the upstream registry and path in the
mirror instead, see [Where a copy goes](../../operational-readiness/troubleshooting/dpfctl/airgap.md#where-a-copy-goes).

```shell
dpfctl airgap images \
  --chart https://helm.ngc.nvidia.com/nvidia/doca/dpf-operator --version v26.10.0 \
  --mirror mirror.example.com/dpf \
  --scope dpu \
  -f my-dpuservices/
```

* `--chart` and `--version` name the `dpf-operator` chart you install. A local chart archive or
  directory works too. The registry and version of the DPF images are those of the operator image
  of the chart.
* `--scope dpu` lists the images the DPU pulls.
* `-f` adds the DPUServices you deploy: pass the files or directories of your `DPUService`,
  `DPUServiceTemplate` and `DPUServiceConfiguration` manifests, with their environment variables
  substituted. See [Images of your DPUServices](#images-of-your-dpuservices).
* `--service dpf-storage` (repeatable) selects services of the BOM by chart name, and
  `--all-services` selects all of them.

See [`dpfctl airgap`](../../operational-readiness/troubleshooting/dpfctl/airgap.md) for all flags
and the JSON output, which also shows where each image comes from.

Copy the images with `skopeo` while you still have internet access:

```shell
(
  set -o pipefail
  dpfctl airgap images \
    --chart https://helm.ngc.nvidia.com/nvidia/doca/dpf-operator --version v26.10.0 \
    --mirror mirror.example.com/dpf --scope dpu -f my-dpuservices/ |
    while read -r src dst; do
      skopeo copy --all --preserve-digests "docker://$src" "docker://$dst" ||
        { echo "failed to copy $src" >&2; exit 1; }
    done
)
```

Or with `crane`, replacing the `skopeo` command with `crane copy "$src" "$dst"`.

* `--all` copies every architecture of a multi-arch image; `crane copy` does so by default.
* An image pinned by digest is copied from its digest to its tag, because `skopeo` does not take a
  reference with both. The digests are preserved, so the mirror serves the image under its tag and
  its original digest.
* Log in to the mirror first (`skopeo login` or `crane auth login`) if pushing requires credentials.

### Images of your DPUServices

The `services` of the BOM are only the optional DPF charts, such as the DPF storage chart, because
the BOM lists what DPF itself deploys. `dpfctl airgap images -f` lists the images of every other
DPUService you deploy, for example the services of a use-case guide. It renders the chart of each
service with the values of your manifests.

The steps below use the manifests of the
[HBN and OVN-Kubernetes use case](../host-trusted/use-cases/hbn-ovnk/README.md). They apply the
same way to the manifests of any other use case or of your own services.

**1.** Set the environment variables that the manifests use. The use-case guides keep them in
`manifests/00-env-vars/envvars.env`. Edit the file to fit your environment, then source it:

```shell
cd docs/public/user-guides/host-trusted/use-cases/hbn-ovnk
source manifests/00-env-vars/envvars.env
```

**2.** Write a copy of the manifests with the variables substituted. The use-case guide pipes the
manifests through `envsubst` into `kubectl apply`. `dpfctl` substitutes no variables, and reads
only the `.yaml`, `.yml` and `.json` files of a directory, so write `.yaml` files:

```shell
mkdir -p my-dpuservices
for f in manifests/05-dpudeployment-installation/*.yaml; do
  envsubst < "$f" > "my-dpuservices/$(basename "$f")"
done
```

Keep all the manifests of a service together. `dpfctl` merges the values of a
`DPUServiceConfiguration` over those of the `DPUServiceTemplate` with the same
`deploymentServiceName`. It reads only the `DPUService`, `DPUServiceTemplate` and
`DPUServiceConfiguration` objects, so other objects, such as the `DPUDeployment` and the
`DPUFlavor`, can stay in the directory.

**3.** Pass the directory with `-f`:

```shell
dpfctl airgap images \
  --chart https://helm.ngc.nvidia.com/nvidia/doca/dpf-operator --version v26.10.0 \
  --mirror mirror.example.com/dpf --scope dpu -f my-dpuservices/
```

`dpfctl` reads everything it needs from the manifests and the charts. To check a service by hand,
or to find the image keys to change in [step 3](#3-point-the-dpu-images-at-the-mirror):

| What                     | Where it comes from                                                                                       |
|--------------------------|-----------------------------------------------------------------------------------------------------------|
| Chart                    | `repoURL`, `chart` and `version` in `spec.helmChart.source` of the `DPUServiceTemplate` or `DPUService`   |
| Images the manifests set | The image values of the template and of the configuration, for example `image.repository` and `image.tag` |
| Images the chart sets    | The chart defaults: `helm show values <chart> --repo <repoURL> --version <version>`                       |
| Values of the variables  | The environment file that you sourced, for example `HELM_REGISTRY_REPO_URL` and `HBN_NGC_IMAGE_URL`       |

For an `oci://` repository, list the chart defaults with
`helm show values <repoURL>/<chart> --version <version>`.

**4.** Copy the images with the `skopeo` or `crane` loop above. Then point the services at the
mirror in the same files, see [DPUService images](#dpuservice-images). Once every image of your
services is below the mirror, this command prints nothing:

```shell
dpfctl airgap images \
  --chart https://helm.ngc.nvidia.com/nvidia/doca/dpf-operator --version v26.10.0 \
  --mirror mirror.example.com/dpf --scope dpu -f my-dpuservices/ -o json |
  jq -r '.[] | select(any(.origins[]; startswith("service "))) | .source' |
  grep -v '^mirror.example.com/dpf/'
```

Apply these files instead of the original manifests. The directory also holds the `DPUFlavor` of
the use case. Add the settings of [Mirror TLS](#mirror-tls), [The sandbox image](#the-sandbox-image)
and [Install packages from your apt repository](#4-install-packages-from-your-apt-repository) to
it first:

```shell
kubectl apply -f my-dpuservices/
```

## 3. Point the DPU images at the mirror

Nothing on the DPU rewrites image references. Configure every image the DPU pulls with the
reference of its copy in the mirror, the second column of `dpfctl airgap images`. An image you miss
fails with `ImagePullBackOff`, or `FailedCreatePodSandBox` for the sandbox image, once egress is
cut; [step 5](#5-check-the-result) finds it.

### Where each DPU image is set

| Image (upstream repository)                                     | Setting                                                                                                                                        |
|-----------------------------------------------------------------|------------------------------------------------------------------------------------------------------------------------------------------------|
| `dpf-system` (DPF image)                                        | `DPFOperatorConfig` `spec.sfcController.controller.image` and `spec.sriovDevicePlugin.configInit.image`                                        |
| `dpf-cni-installer` (DPF image)                                 | `DPFOperatorConfig` `spec.cniInstaller.installer.image`                                                                                        |
| `ghcr.io/flannel-io/flannel`                                    | `DPFOperatorConfig` `spec.flannel.daemon.image`                                                                                                |
| `ghcr.io/flannel-io/flannel-cni-plugin`                         | `DPFOperatorConfig` `spec.flannel.cni.image`                                                                                                   |
| `ghcr.io/k8snetworkplumbingwg/multus-cni`                       | `DPFOperatorConfig` `spec.multus.cni.image`                                                                                                    |
| `ghcr.io/k8snetworkplumbingwg/sriov-network-device-plugin`      | `DPFOperatorConfig` `spec.sriovDevicePlugin.deviceplugin.image`                                                                                |
| `ghcr.io/mellanox/nvidia-k8s-ipam`                              | `DPFOperatorConfig` `spec.nvipam.node.image`                                                                                                   |
| `registry.k8s.io/node-problem-detector/node-problem-detector`   | `DPFOperatorConfig` `spec.monitoring.nodeProblemDetector.daemon.image`                                                                         |
| `docker.io/otel/opentelemetry-collector-contrib`                | `DPFOperatorConfig` `spec.monitoring.openTelemetryCollector.daemon.image`                                                                      |
| `quay.io/kata-containers/kata-deploy`                           | `DPFOperatorConfig` `spec.security.kata.daemon.image` (only with Kata containers)                                                              |
| `ghcr.io/spiffe/spiffe-csi-driver`                              | `DPFOperatorConfig` `spec.security.spiffe.csiDriver.driver.image` (only with SPIFFE)                                                           |
| `registry.k8s.io/sig-storage/csi-node-driver-registrar`         | `DPFOperatorConfig` `spec.security.spiffe.csiDriver.nodeDriverRegistrar.image` (only with SPIFFE)                                              |
| `registry.k8s.io/kube-proxy`, `registry.k8s.io/coredns/coredns` | `DPFOperatorConfig` `spec.kamajiClusterManager.dpuClusterImages.addonsRepository`: see [Kamaji DPU cluster addons](#kamaji-dpu-cluster-addons) |
| `registry.k8s.io/pause` (`sandbox: true`)                       | `DPUFlavor` `spec.containerdConfig.sandboxImage`: see [The sandbox image](#the-sandbox-image)                                                  |
| Images of your DPUServices                                      | The helm values of the DPUService: see [DPUService images](#dpuservice-images)                                                                 |

### DPFOperatorConfig

Add the image fields to the `DPFOperatorConfig` you install. Set each field to the repository of
the mirror copy, the second column of `dpfctl airgap images` without the tag and digest. DPF adds
the tag of the installed release to the DPF images, and the tag and digest that the release pins
to the other images. The fields then stay valid when you upgrade DPF:

```yaml
apiVersion: operator.dpu.nvidia.com/v1alpha1
kind: DPFOperatorConfig
metadata:
  name: dpfoperatorconfig
  namespace: dpf-operator-system
spec:
  # ... your other settings
  sfcController:
    controller:
      image: mirror.example.com/dpf/dpf-system
  cniInstaller:
    installer:
      image: mirror.example.com/dpf/dpf-cni-installer
  flannel:
    daemon:
      image: mirror.example.com/dpf/flannel
    cni:
      image: mirror.example.com/dpf/flannel-cni-plugin
  multus:
    cni:
      image: mirror.example.com/dpf/multus-cni
  sriovDevicePlugin:
    deviceplugin:
      image: mirror.example.com/dpf/sriov-network-device-plugin
    configInit:
      image: mirror.example.com/dpf/dpf-system
  nvipam:
    node:
      image: mirror.example.com/dpf/nvidia-k8s-ipam
  monitoring:
    nodeProblemDetector:
      daemon:
        image: mirror.example.com/dpf/node-problem-detector
    openTelemetryCollector:
      # ... your collector settings
      daemon:
        image: mirror.example.com/dpf/opentelemetry-collector-contrib
  kamajiClusterManager:
    dpuClusterImages:
      addonsRepository: mirror.example.com/dpf
```

Set only the fields of the components you run, for example `security.kata` only if you use Kata
containers. Do not add a tag. A field with a tag keeps that image after an upgrade. For an image
that DPF does not build, the tag also replaces the tag and digest that the release pins.

### Kamaji DPU cluster addons

A Kamaji DPU cluster pulls its `kube-proxy` and CoreDNS addon images from the repository
`spec.kamajiClusterManager.dpuClusterImages.addonsRepository` of the `DPFOperatorConfig`, as
`<addonsRepository>/kube-proxy:<Kubernetes version>` and `<addonsRepository>/coredns:<CoreDNS
version>`. Set it to the mirror, as in the example above: that is where both were copied to. With
the nested layout, set it to the copy of `registry.k8s.io` instead, see
[Where a copy goes](../../operational-readiness/troubleshooting/dpfctl/airgap.md#where-a-copy-goes).
A DPU cluster with a keepalived VIP (`spec.clusterEndpoint.keepalived.vip` of the `DPUCluster`) has
no CoreDNS addon.

`dpuClusterImages` applies when DPF creates a Kamaji DPU cluster. Set it before you create the
`DPUCluster`: an existing DPU cluster keeps its images.

### The sandbox image

containerd pulls its sandbox (`pause`) image before any pod of the DPU runs, from its own
configuration; no pod spec names it. If containerd cannot pull it, every pod fails with
`FailedCreatePodSandBox`. Set it to the mirror copy of the image the BOM marks `sandbox: true`,
in the `DPUFlavor`:

```yaml
# DPUFlavor (spec excerpt)
spec:
  containerdConfig:
    sandboxImage: mirror.example.com/dpf/pause:3.9
```

The DPU agent writes it into the containerd configuration of the DPU. The BOM lists the sandbox
image of the BFB the release ships with. If you run another BFB, read its sandbox image on a
provisioned DPU (`grep -h -e sandbox_image -e sandbox /etc/containerd/*.toml`), and copy that image
into the mirror as well.

### DPUService images

Point each DPUService at the mirror in its chart values. Most charts take the image as
`image.repository` and `image.tag`, as `dummydpuservice`, the example DPUService of DPF, does.
Usually the change looks like this:

```yaml
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUService
metadata:
  name: dummydpuservice
  namespace: dummydpuservice
spec:
  helmChart:
    source:
      # ... repoURL, chart and version stay as they are
    values:
      image:
        repository: mirror.example.com/dpf/dummydpuservice
        tag: v0.1.0
      # ... your other values
```

Change the repository and keep the tag that the manifest sets, because DPF adds no tag to
DPUService values. Set the values in `spec.helmChart.values` of the DPUService or
DPUServiceTemplate, or in `spec.serviceConfiguration.helmChart.values` of a
DPUServiceConfiguration. Each chart has its own keys, which `helm show values` lists:

* `doca-hbn` in the HBN and OVN-Kubernetes use case takes the same keys. Its manifest sets the tag
  `3.5.0-doca3.5.0`.
* A chart whose manifests set no image pulls the images of its defaults, for example
  `doca-telemetry` and `ovn-kubernetes-chart` in that use case. Add the image keys that
  `helm show values` lists to the values.

Check the result as in [Images of your DPUServices](#images-of-your-dpuservices). The charts
themselves are pulled by Argo CD on the host cluster, so the chart `repoURL` stays as it is.

## 4. Install packages from your apt repository

This applies only if your `DPUFlavor` installs packages (`spec.packages`). The BOM does not list
packages: every package and every dependency it pulls in must be installable from an apt
repository that the DPU can reach, for the OS release and CPU architecture of your BFB. DPF does
not ship an apt repository or recommend a tool to build one.

The `DPUFlavor` writes the apt source of your repository with `spec.configFiles`, for example to
`/etc/apt/airgap-mirror.list`, and pins each package to it with `repoFileRef`, so the package
resolves only from your repository:

```yaml
# DPUFlavor (spec excerpt)
spec:
  configFiles:
    - operation: override
      path: /etc/apt/keyrings/airgap-mirror.asc
      permissions: "0644"
      raw: |
        -----BEGIN PGP PUBLIC KEY BLOCK-----
        ...the public key that signs your repository, from gpg --armor --export <key ID>...
        -----END PGP PUBLIC KEY BLOCK-----
    - operation: override
      path: /etc/apt/airgap-mirror.list
      permissions: "0644"
      raw: |
        deb [signed-by=/etc/apt/keyrings/airgap-mirror.asc] https://mirror.example.com/apt noble main
  packages:
    - name: <package>
      repoFileRef: /etc/apt/airgap-mirror.list
```

See the [DPUFlavor API reference](../../developer-guides/api/api.md#dpuflavor) for the fields.

## 5. Check the result

After the DPU is provisioned, list the images of the pods of the DPU cluster that do not come from
the mirror. The output must be empty:

```shell
# with the kubeconfig of the DPU cluster
kubectl get pods -A -o jsonpath='{range .items[*]}{range .spec.initContainers[*]}{.image}{"\n"}{end}{range .spec.containers[*]}{.image}{"\n"}{end}{end}' |
  sort -u | grep -v '^mirror.example.com/dpf/'
```

On the DPU, check the sandbox image:

```shell
grep -h -e sandbox_image -e sandbox /etc/containerd/*.toml
```

An image that does not pull shows up in `kubectl describe pod` as `ImagePullBackOff`, or as
`FailedCreatePodSandBox` for the sandbox image. Check that its reference names the mirror (see
[Where each DPU image is set](#where-each-dpu-image-is-set)) and that the mirror has the image
at that reference.

## Upgrade DPF

Copy the images of the new release into the mirror before you upgrade DPF. Run
`dpfctl airgap images` with the `--version` of the new `dpf-operator` chart, and copy the listed
images as in [step 2](#2-copy-the-images-into-the-mirror). Then upgrade DPF.

The image fields of [step 3](#3-point-the-dpu-images-at-the-mirror) set repositories without tags,
so the `DPFOperatorConfig` needs no change. DPF adds the tags of the new release, and the DPU pulls
the new images from the mirror. An existing Kamaji DPU cluster moves to the Kubernetes version of
the new release and pulls the matching `kube-proxy` image from the mirror.

Copy the images of a new DPUService version the same way before you change the DPUService.

## Known limitations

* **The host cluster is not air-gapped.** It pulls its images, the helmfile prerequisites and the
  helm charts from their upstream locations.
* **Services not in the BOM.** The `services` of the BOM are the optional DPF charts only. Pass the
  manifests of every other DPUService you deploy with `-f`, so `dpfctl` renders their charts with
  your values (see [Images of your DPUServices](#images-of-your-dpuservices)).
* **Private chart repositories.** `dpfctl` downloads charts without credentials, so a chart in a
  repository that requires them fails to download. Pass a private `dpf-operator` chart as a local
  archive with `--chart`. Leave a DPUService with such a chart out of `-f`. List the images of that
  chart with `helm template` and your credentials, and copy each to `<mirror>/<name>:<tag>`. See
  [`dpfctl airgap`](../../operational-readiness/troubleshooting/dpfctl/airgap.md#usage).
* **Images enabled by values `dpfctl` does not see.** An image a chart pulls only when a value is
  set elsewhere, for example in a `DPUServiceConfiguration` you did not pass with `-f`, is not
  listed. Pass all the manifests of a service together.
* **Packages.** The BOM does not list `.deb` packages; see
  [step 4](#4-install-packages-from-your-apt-repository).
* **The DPU OS contacts internet hosts on its own.** Units of the BFB OS, such as snapd, Ubuntu
  Pro, `motd-news` and the apt timers, contact internet hosts. In an air gap their requests fail,
  and your firewall can log them as blocked egress. DPF needs none of these units, so you can
  ignore these failures. `systemd-timesyncd` fails the same way, and then nothing keeps the DPU
  clock in sync. Point it at an NTP server inside your network:

  ```yaml
  # DPUFlavor (spec excerpt)
  spec:
    configFiles:
      - operation: override
        path: /etc/systemd/timesyncd.conf.d/airgap.conf
        permissions: "0644"
        raw: |
          [Time]
          NTP=ntp.example.com
  ```
