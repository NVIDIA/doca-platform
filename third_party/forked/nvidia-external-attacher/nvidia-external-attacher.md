# NVIDIA External Attacher

## Overview
The `nvidia-external-attacher` is a customized version of the upstream [external-attacher](https://github.com/kubernetes-csi/external-attacher) project, specifically adapted for DPF use cases. It serves as a sidecar container that attaches volumes to nodes by calling the `ControllerPublish` and `ControllerUnpublish` functions of vendor CSI drivers.

The interface of the `nvidia-external-attacher` remains consistent with the upstream `external-attacher`, with the only difference being that the `nvidia-external-attacher` handles the `SVVolumeAttachment` Custom Resource Definition (CRD) instead of `VolumeAttachment`.

## Code Maintenance
The `nvidia-external-attacher` is maintained as a git submodule within the DPF project. This approach allows us to track changes and apply patches to the upstream external-attacher codebase.

The `nvidia-external-attacher` folder contains the `SVVolumeAttachment` and `SVVolumeAttachmentList` types from the `storage.dpu.nvidia.com` DPF API group, along with the required annotations to generate the clientset. The generated clientset and informer are also stored in nvidia-external-attacher and copied to the `external-attacher` code tree when needed.

### Cloning the Repository
By default, cloning the DPF repository does not download the `external-attacher` code. To clone the repository along with all submodules, use the following command:

```bash
git clone --recurse-submodules $DPF_REPO_URL
```

If you have already cloned the repository without submodules, you can initialize and update the submodules using:

```bash
git submodule update --init --recursive
```

## Future Support and Version Upgrades
To ensure compatibility with the latest features and bug fixes from the community's `external-attacher` project, the `nvidia-external-attacher` can be upgraded to a newer version of the `external-attacher` codebase. Below are the steps to perform such an upgrade:
1. Update `branch` variable in `.git.moudules`  to the desired branch from the `external-attacher` repository.
2. Update the submodule configuration by `git submodule sync` command.
3. Create a new folder named branch in the `patches` directory and generate new patch file for this new branch.
4. Update `EXTERNAL_ATTACHER_BRANCH` in Makefile to the new branch name.

## Generate New Patch File
If there is a bugfix, we need to update and maintain it in the form of a patch
1. Update `SVVolumeAttachment` and `SVVolumeAttachmentList` types in the API folder of `nvidia-external-attacher` if needed.
2. Regenerate `client`, `lister` and `informer`.
    ```
    make generate-client-for-nvidia-external-attacher
    ```
3. Apply previous patches and copy API and client code to the `external-attacher` with the following command.
    ```
    hack/client.sh $DPF_DIR $EXTERNAL_ATTACHER_BRANCH
    ```
4. Update the code according to the issue you want to fix.
5. Commit code to local repo to generate a commit ID.
6. Generate new patch files.
    ```
    cd third_party/forked/nvidia-external-attacher/external-attacher
    git format-patch HEAD~1..HEAD -o ../hack/patches/$EXTERNAL_ATTACHER_BRANCH
    ```
