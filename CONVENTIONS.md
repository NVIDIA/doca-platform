# Contribution guide
This project is in an early development phase and has a lightweight contribution process.

## Merging changes
One approval from another reviewer and a positive CI signal is required to merge changes. Changes should be merged by the author.

## Reviews
Reviews should be lightweight to not slow down contributions.

Reviews should focus on:
- Structure - Does the code meet the structure defined for the repo?
- Suitable testing  - Is the happy path covered in unit and integration tests?

## Repository Structure

The following general principles should be taken into account when reviewing a PR for structure:
- Components share dependencies - be a single go module - unless there are specific reasons to introduce another module.
- Components share build components and general tooling - e.g. Dockerfile, Makefile unless there are specific reasons not to.
- Components define a directory structure with their component name under the top level directories where appropriate. e.g. `api/component-name`, `cmd/component-name`, and `config/component-name`.
- Components add code generation, build process and other development flow targets to the `Makefile`.
- Code is under the `internal` package unless an explicit decision is taken to share it.
- Controllers are in a `controllers` folder under their component name.
- A shared package is on the same level in the directory structure as its highest-level consumer.

  
The below is a representation of how multiple components are structured in the repo:
```
├── Dockerfile
├── Makefile
├── go.mod
├── go.sum
├── doc
├── hack
├── test
├── README.md
├── api
│      ├── controlplane
│      │         └── v1alpha1
│      │             ├── dpfcluster_types.go
│      └── dpuservice
│             └── v1alpha1
│                 ├── dpuservice_types.go
├── cmd
│      ├── controlplane
│      │         └── main.go
│      └── dpuservice
│          └── main.go
├── config
│         ├── controlplane
│         │         ├── crd
│         │         │         ├── bases
│         │         │         │         └── controlplane.dpu.nvidia.com_dpfclusters.yaml
│         └── dpuservice
│             ├── crd
│             │         ├── bases
│             │         │         └── svc.dpu.nvidia.com_dpuservices.yaml
│             │         ├── kustomization.yaml
│             │         └── kustomizeconfig.yaml
│             ├── default
│             │         ├── kustomization.yaml
│             │         └── manager_config_patch.yaml
│             ├── manager
│             │         ├── kustomization.yaml
│             │         └── manager.yaml
│             ├── prometheus
│             │         ├── kustomization.yaml
│             │         └── monitor.yaml
│             ├── rbac
│             │         ├── auth_proxy_client_clusterrole.yaml
│             │         ├── auth_proxy_role.yaml
│             │         ├── auth_proxy_role_binding.yaml
│             │         ├── auth_proxy_service.yaml
│             │         ├── controlplane.dpf_dpfcluster_editor_role.yaml
│             │         ├── controlplane.dpf_dpfcluster_viewer_role.yaml
│             │         ├── kustomization.yaml
│             │         ├── leader_election_role.yaml
│             │         ├── leader_election_role_binding.yaml
│             │         ├── role.yaml
│             │         ├── role_binding.yaml
│             │         ├── service_account.yaml
│             │         ├── svc.dpf_dpuservice_editor_role.yaml
│             │         └── svc.dpf_dpuservice_viewer_role.yaml
│             └── samples
│                 ├── controlplane.dpf_v1alpha1_dpfcluster.yaml
│                 ├── kustomization.yaml
│                 └── svc.dpf_v1alpha1_dpuservice.yaml
├── internal
│         ├── controlplane
│         │         └── controllers
│         │             ├── dpfcluster_controller.go
│         └── dpuservice
│             └── controllers
│                 ├── dpuservice_controller.go
```

## Developer Certificate of Origin

All contributions comply with the [Developer Certificate of Origin](https://developercertificate.org/):
  ```
    Developer Certificate of Origin
    Version 1.1
    
    Copyright (C) 2004, 2006 The Linux Foundation and its contributors.
    1 Letterman Drive
    Suite D4700
    San Francisco, CA, 94129
    
    Everyone is permitted to copy and distribute verbatim copies of this license document, but changing it is not allowed.

    Developer's Certificate of Origin 1.1
    
    By making a contribution to this project, I certify that:
    
    (a) The contribution was created in whole or in part by me and I have the right to submit it under the open source license indicated in the file; or
    
    (b) The contribution is based upon previous work that, to the best of my knowledge, is covered under an appropriate open source license and I have the right under that license to submit that work with modifications, whether created in whole or in part by me, under the same open source license (unless I am permitted to submit under a different license), as indicated in the file; or
    
    (c) The contribution was provided directly to me by some other person who certified (a), (b) or (c) and I have not modified it.
    
    (d) I understand and agree that this project and the contribution are public and that a record of the contribution (including all personal information I submit with it, including my sign-off) is maintained indefinitely and may be redistributed consistent with this project or the open source license(s) involved.
  ```

## Log Convention

- `klog.V(0).InfoS` = `klog.InfoS` - Generally useful for this to always be visible to a cluster operator
- `klog.V(1).InfoS` - A reasonable default log level if you don't want verbosity.
- `klog.V(2).InfoS` - Useful steady state information about the service and important log messages that may correlate to significant changes in the system. This is the recommended default log level for most systems.
- `klog.V(3).InfoS` - Extended information about changes
- `klog.V(4).InfoS` - Debug level verbosity

xref: https://github.com/kubernetes/community/blob/master/contributors/devel/sig-instrumentation/logging.md#what-method-to-use
