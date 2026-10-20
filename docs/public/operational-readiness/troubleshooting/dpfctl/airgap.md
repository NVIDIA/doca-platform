---
title: "dpfctl airgap"
---

[[_TOC_]]

The `dpfctl airgap` command lists what to copy into a mirror registry for an
[air-gapped DPF installation](../../../user-guides/airgapped/README.md): the container images and
helm charts of the air-gap bill of materials (BOM) that a DPF release ships in its `dpf-operator`
chart (`airgap/airgapped.yaml`), and those of the DPUServices you deploy.

`dpfctl` only lists the copies. Copy them with a tool like [skopeo](https://github.com/containers/skopeo),
[crane](https://github.com/google/go-containerregistry/tree/main/cmd/crane) or helm.

## Usage

```sh
dpfctl airgap [command] [flags]
```

Available Commands:

| Command | Description                                         |
|---------|-----------------------------------------------------|
| images  | List the images to copy into a mirror registry      |
| charts  | List the helm charts to copy into a mirror registry |

Both commands take the same flags. They need the [helm](https://helm.sh) v3 binary to download
the `dpf-operator` chart and to render the charts of the DPUServices passed with `-f`. helm runs
with a private configuration, so the downloads are anonymous. `dpfctl` does not support
credentials, so a chart in a repository that requires them fails to download. Pull such a
`dpf-operator` chart with your own helm (`helm pull`), and pass the archive with `--chart`.
`dpfctl airgap charts` then also needs `--dpf-chart-repo`. Leave the manifests of a DPUService
with such a chart out of `-f`, and list the images of that chart with `helm template` and your
credentials. The commands do not connect to a cluster.

## Where a copy goes

`--layout` sets where below the mirror registry a copy goes.

**`flat` (the default)** copies every image to `<mirror>/<name>:<tag>`, where the name is the last
element of its repository, and pushes every chart to `oci://<mirror>`:

- The image `nvcr.io/nvidia/doca/doca_hbn:3.5.0` is copied to `<mirror>/doca_hbn:3.5.0`.
- The chart `doca-hbn` of `https://helm.ngc.nvidia.com/nvidia/doca` is pushed to
  `oci://<mirror>/doca-hbn`.

Images of different repositories that share a name and a tag cannot both be copied to a flat
mirror; `dpfctl` then fails and names them. Use the nested layout for such a set.

A chart is pushed to `<mirror>/<chart name>:<version>`, so a chart and an image with the same name
and version clash too. The copy that runs last overwrites the other, and a pod that needs the image
then pulls the chart and fails. `dpfctl` does not detect such a pair, because the DPF charts and
images share no name. For your own charts, use the nested layout, or pass another mirror path to
`airgap charts`, such as `--mirror mirror.example.com/dpf-charts`.

**`nested`** keeps the upstream location below the mirror:

- The image `nvcr.io/nvidia/doca/doca_hbn:3.5.0` is copied to
  `<mirror>/nvcr.io/nvidia/doca/doca_hbn:3.5.0`.
- The charts of the helm repository `https://helm.ngc.nvidia.com/nvidia/doca` are pushed to the
  OCI repository `oci://<mirror>/helm.ngc.nvidia.com/nvidia/doca`.

The upstream registry host is lower case in the path. A repository path cannot contain a colon,
so the colon of a port becomes an underscore: `harbor.example.com:8443/team/app:1.0` is copied to
`<mirror>/harbor.example.com_8443/team/app:1.0`.

The registry must support nested repository paths.

In the `DPFOperatorConfig`, set `spec.kamajiClusterManager.dpuClusterImages.addonsRepository` to
the copy of `registry.k8s.io`, for example `<mirror>/registry.k8s.io`. Kamaji pulls the kube-proxy
and CoreDNS addons of a Kamaji DPU cluster as `<addonsRepository>/<name>:<tag>`. So `dpfctl` also
lists a second copy of the CoreDNS image, `<mirror>/registry.k8s.io/coredns:<tag>`.

An image the BOM pins by tag and digest is copied from its digest to its tag. Copied with its
digests preserved, the original reference `<mirror>/<image>:<tag>@<digest>` resolves on the
mirror. An image without tag and digest is copied with the tag `latest`, the tag a container
runtime pulls for it.

Two references that pin different digests of one tag are both copied to that tag, and `dpfctl`
warns about them. The tag then points at the image copied last, so a pod that pulls by tag alone
gets that image. A pull by digest finds both, unless the registry deletes images without a tag.
Pin the same digest in both, for example in the values of your DPUService.

## What is listed

- **The images and charts every installation needs.** These are the `images` and the `charts` of
  the BOM that no service lists.
  - The BOM lists DPF images and charts by name only. Their registry and version are those of the
    operator image of the `dpf-operator` chart, or of `--dpf-registry`, `--dpf-version` and
    `--dpf-chart-repo`.
- **The optional services of the BOM you select.** Select them with `--service` or
  `--all-services`.
- **The DPUServices you deploy, passed with `-f`.**
  - Pass the files or directories of your `DPUService`, `DPUServiceTemplate` and
    `DPUServiceConfiguration` manifests, or `-` to read them from standard input. A file is read
    with any extension, a directory for its `*.yaml`, `*.yml` and `*.json` files.
  - Their charts are rendered with their values, so the images listed are those the services pull.
  - Substitute any environment variables in the manifests first, for example with `envsubst`:
    `envsubst < hbn.yaml | dpfctl airgap images ... -f -`.

`--scope dpu` or `--scope host` narrows the images to those the DPU cluster or the host cluster
pulls.

## Flags

| Flag               | Default                                | Description                                                                                                                                          |
|--------------------|----------------------------------------|------------------------------------------------------------------------------------------------------------------------------------------------------|
| `--chart`          |                                        | The `dpf-operator` chart of the release: `<repository URL>/dpf-operator` (an http(s) or `oci://` repository), or a local chart archive or directory. |
| `--version`        |                                        | Version of the chart in a repository, for example `v26.10.0`.                                                                                        |
| `--bom`            |                                        | A BOM file, instead of `--chart`. Needs `--dpf-registry` and `--dpf-version`.                                                                        |
| `--dpf-registry`   | registry of the chart's operator image | Registry of the DPF images, for example `nvcr.io/nvidia/doca`.                                                                                       |
| `--dpf-version`    | version of the chart's operator image  | DPF release version: the tag of the DPF images and the version of the DPF charts.                                                                    |
| `--dpf-chart-repo` | repository of `--chart`                | Helm repository of the DPF charts.                                                                                                                   |
| `--mirror`         |                                        | Mirror registry to copy to, `host[:port][/path]` (required).                                                                                         |
| `--layout`         | `flat`                                 | Layout of the mirror, `flat` or `nested`: see [Where a copy goes](#where-a-copy-goes).                                                               |
| `--scope`          | `dpu` and `host`                       | Cluster whose images to list, `dpu` or `host` (repeatable).                                                                                          |
| `--service`        |                                        | Optional service of the BOM you deploy, for example `dpf-storage` (repeatable).                                                                      |
| `--all-services`   | `false`                                | Include all the optional services of the BOM.                                                                                                        |
| `-f`, `--filename` |                                        | File or directory of DPUService manifests you deploy, or `-` for standard input (repeatable).                                                        |
| `--helm`           | `$HELM`, then `helm` in `$PATH`        | The helm v3 binary.                                                                                                                                  |
| `-o`, `--output`   | `pairs`                                | Output format, `pairs` or `json`.                                                                                                                    |

## images

`dpfctl airgap images` prints one `source destination` line per image. With `-o json` it prints
each image with its reference, the clusters that pull it, and the parts of the BOM or the services
that list it.

Copy the images of a release, with those of your DPUServices, with skopeo:

```shell
dpfctl airgap images --chart https://helm.ngc.nvidia.com/nvidia/doca/dpf-operator --version v26.10.0 \
  --mirror mirror.example.com:5000 -f my-dpuservices/ |
  while read -r src dst; do
    skopeo copy --all --preserve-digests "docker://$src" "docker://$dst"
  done
```

Or with crane:

```shell
dpfctl airgap images --chart https://helm.ngc.nvidia.com/nvidia/doca/dpf-operator --version v26.10.0 \
  --mirror mirror.example.com:5000 -f my-dpuservices/ |
  while read -r src dst; do crane copy "$src" "$dst"; done
```

## charts

`dpfctl airgap charts` prints one `repository name version destination` line per chart. Copy them
with helm:

```shell
dpfctl airgap charts --chart https://helm.ngc.nvidia.com/nvidia/doca/dpf-operator --version v26.10.0 \
  --mirror mirror.example.com:5000 -f my-dpuservices/ |
  while read -r repo name version dst; do
    case "$repo" in
      oci://*) helm pull "$repo/$name" --version "$version" ;;
      *) helm pull "$name" --repo "$repo" --version "$version" ;;
    esac
    helm push "$name-$version.tgz" "$dst"
  done
```
