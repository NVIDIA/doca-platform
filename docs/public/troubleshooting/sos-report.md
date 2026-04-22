---
title: "Reporting Issues with sosreport"
---

[TOC]

## Environment

* DOCA Platform Framework Kubernetes cluster

## Issue

* What is the recommended way for generating a sosreport in DOCA Platform Framework

* It may not be possible to connect to DOCA Platform Framework nodes via SSH from
outside the cluster by default but sosreport may need to be run for troubleshooting
purposes.


## Generating a sosreport with a Debug Pod 

### Target Host Cluster

#### Create a Secret Containing the kubeconfig

In order to run `sosreport`, a `kubeconfig` is needed to access the API Server. 

**1.** Create a secret containing the `kubeconfig`

```shell
kubectl create secret generic admin-config --from-file=kubeconfig=<path_to_kubeconfig>
```

#### Deploy sosreport

**1.** Display the list of nodes in the cluster and export the selected node.
The following command will display the list of nodes:

```shell
kubectl get nodes
```

**2.** Then create a debug pod by deploying the following manifest:

```shell
cat <<EOF | kubectl create -f -
apiVersion: v1
kind: Pod
metadata:
  name: dpf-sosreport
spec:
  nodeName: ${NODE_NAME}
  containers:
  - name: sosreport
    image: ghcr.io/nvidia/sosreport:latest
    env:
    - name: CASE_ID
      value: "${CASE_ID}"
    imagePullPolicy: IfNotPresent
    securityContext:
      privileged: true
      runAsUser: 0
    volumeMounts:
      - mountPath: /host
        name: host
      - mountPath: /run
        name: run
      - mountPath: /var/log
        name: varlog
        # sosreport check if this file exist before executing the kubernetes plugin,
        # without it no kubernetes output will be available.
      - mountPath: /etc/kubernetes/admin.conf
        name: adminconf
        subPath: kubeconfig
      - mountPath: /etc/localtime
        name: localtime
      - mountPath: /etc/machine-id
        name: machineid
      - mountPath: /boot
        name: boot
      - mountPath: /usr/lib/modules/
        name: modules
  volumes:
    - hostPath:
        path: /
      name: host
    - hostPath:
        path: /run
      name: run
    - hostPath:
        path: /boot
      name: boot
    - hostPath:
        path: /usr/lib/modules/
      name: modules
    - hostPath:
        path: /var/log
      name: varlog
    - secret:
        secretName: admin-config
      name: adminconf
    - hostPath:
        path: /etc/localtime
      name: localtime
    - hostPath:
        path: /etc/machine-id
      name: machineid
  restartPolicy: Never
  hostIPC: true
  hostNetwork: true
  hostPID: true
EOF
```


### Target Tenant Cluster

#### Find the Tenant Cluster kubeconfig

In order to run `sosreport`, a `kubeconfig` is needed to access the API Server.
When the report has to be generated for a tenant cluster, we have to retrieve the 
`kubeconfig` from the host cluster.

**1.** Get the `kubeconfig` name from the dpucluster `spec`.

```shell
export KUBECONFIG_NAME=$(kubectl get dpucluster -n ${NAMESPACE} ${CLUSTER_NAME} -o jsonpath='{.spec.kubeconfig}')
```  

**2.** Create the `kubeconfig` from the secret data

```shell
kubectl get secrets -n ${NAMESPACE} ${KUBECONFIG_NAME} -o json \
  | jq -r '.data["super-admin.conf"]' \
  | base64 --decode \
  > /tmp/${NAMESPACE}-${CLUSTER_NAME}.kubeconfig
```

**3.** Create a secret containing the `kubeconfig` in the tenant cluster

```shell
kubectl create secret generic admin-config --from-file=kubeconfig=/tmp/${NAMESPACE}-${CLUSTER_NAME}.kubeconfig \
  --kubeconfig=/tmp/${NAMESPACE}-${CLUSTER_NAME}.kubeconfig
```

#### Deploy sosreport

**1.** Display the list of nodes in the cluster and export the selected node.
The following command will display the list of nodes:

```shell
kubectl get nodes
```

**2.** Then create a debug pod by deploying the following manifest:


```shell
cat <<EOF | kubectl --kubeconfig=/tmp/${NAMESPACE}-${CLUSTER_NAME}.kubeconfig create -f -
apiVersion: v1
kind: Pod
metadata:
  name: dpf-sosreport
spec:
  nodeName: ${NODE_NAME}
  containers:
  - name: sosreport
    image: ghcr.io/nvidia/sosreport:latest
    env:
    - name: CASE_ID
      value: "${CASE_ID}"
    imagePullPolicy: IfNotPresent
    securityContext:
      privileged: true
      runAsUser: 0
    volumeMounts:
      - mountPath: /host
        name: host
      - mountPath: /run
        name: run
      - mountPath: /var/log
        name: varlog
        # sosreport check if this file exist before executing the kubernetes plugin,
        # without it no kubernetes output will be available.
      - mountPath: /etc/kubernetes/admin.conf
        name: adminconf
        subPath: kubeconfig
      - mountPath: /etc/localtime
        name: localtime
      - mountPath: /etc/machine-id
        name: machineid
      - mountPath: /boot
        name: boot
      - mountPath: /usr/lib/modules/
        name: modules
  volumes:
    - hostPath:
        path: /
      name: host
    - hostPath:
        path: /run
      name: run
    - hostPath:
        path: /boot
      name: boot
    - hostPath:
        path: /usr/lib/modules/
      name: modules
    - hostPath:
        path: /var/log
      name: varlog
    - secret:
        secretName: admin-config
      name: adminconf
    - hostPath:
        path: /etc/localtime
      name: localtime
    - hostPath:
        path: /etc/machine-id
      name: machineid
  restartPolicy: Never
  hostIPC: true
  hostNetwork: true
  hostPID: true
EOF
```

### Retrieve the Generated Report

The final repost archive is available under `/tmp` in the node filesystem.

In order to untar it, run :

```shell
tar -x --xz -f sosreport-<node_name>-<case_id>-<date>-xxx.tar.xz
``` 