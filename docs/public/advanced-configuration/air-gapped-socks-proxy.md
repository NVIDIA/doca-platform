---
title: "Air-Gapped DPUs with a SOCKS Proxy"
---

[[_TOC_]]

In some environments the DPUs have no direct route to the internet, but the DPU services, system components and
their images still have to be pulled from public registries. This guide describes how to let the DPU's `containerd`
pull images through a SOCKS5 proxy that is reachable from the DPU, for example a proxy running on the management cluster.

The solution has two parts:

* A SOCKS5 proxy, deployed on a cluster that has internet access and is reachable from the DPUs.
* A `DPUFlavor` that configures `containerd` on the DPU to use that proxy.

> [!NOTE]
> This is a reference guide. DPF does not ship or manage a SOCKS proxy, and it does not depend on any specific
> implementation. The [3proxy](https://github.com/3proxy/3proxy) deployment shown below is only an example. You can use
> any SOCKS5 proxy solution that fits your environment and security requirements. Only the `DPUFlavor` configuration is
> specific to DPF, and it only needs the address of a reachable SOCKS5 endpoint.

> [!NOTE]
> This guide only covers image pulls performed by `containerd` on the DPU. It does not configure a proxy for
> the DPF components running on the management cluster, nor for the DPU's operating system (for example `apt`).

## Prerequisites

* The DPUs can reach the address (IP and port) on which the SOCKS proxy is exposed.
* The proxy can reach the registries the DPU pulls from.
* (Optional) Kubernetes LoadBalancer solution for the management cluster (e.g. MetalLB)

## Deploy the SOCKS proxy (example)

If you already have a SOCKS5 proxy, skip to [Configure the DPUFlavor](#configure-the-dpuflavor).

As an example, the proxy below is a [3proxy](https://github.com/3proxy/3proxy) instance running as a `Deployment`,
exposed through a `Service`. Create it in a namespace of your choice (this guide uses `socks-proxy`).

<details markdown="1"><summary><b>SOCKS proxy Namespace, ConfigMap and Deployment</b></summary>

```yaml
---
apiVersion: v1
kind: Namespace
metadata:
  name: socks-proxy
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: socks-proxy-config
  namespace: socks-proxy
  labels:
    app.kubernetes.io/name: socks-proxy
data:
  3proxy.cfg: |
    log
    auth none
    allow *
    socks -4 -i0.0.0.0 -p1888
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: socks-proxy
  namespace: socks-proxy
  labels:
    app.kubernetes.io/name: socks-proxy
spec:
  replicas: 1
  selector:
    matchLabels:
      app.kubernetes.io/name: socks-proxy
  template:
    metadata:
      labels:
        app.kubernetes.io/name: socks-proxy
    spec:
      securityContext:
        runAsNonRoot: true
        runAsUser: 65535
        runAsGroup: 65535
        fsGroup: 65535
        fsGroupChangePolicy: OnRootMismatch
      containers:
      - name: socks-proxy
        image: docker.io/3proxy/3proxy:0.9.5
        imagePullPolicy: IfNotPresent
        ports:
        - name: socks-tcp
          containerPort: 1888
          protocol: TCP
        resources:
          requests:
            cpu: 50m
            memory: 64Mi
          limits:
            cpu: 500m
            memory: 256Mi
        livenessProbe:
          tcpSocket:
            port: 1888
          initialDelaySeconds: 5
          periodSeconds: 5
        readinessProbe:
          tcpSocket:
            port: 1888
          initialDelaySeconds: 5
          periodSeconds: 5
        volumeMounts:
        - name: cfg
          mountPath: /etc/3proxy/3proxy.cfg
          subPath: 3proxy.cfg
          readOnly: true
      volumes:
      - name: cfg
        configMap:
          name: socks-proxy-config
```

</details>

Expose the proxy with a `Service` that the DPUs can reach. With a `LoadBalancer` implementation available, use a
`LoadBalancer` Service and restrict the sources that may connect:

<details markdown="1"><summary><b>LoadBalancer Service</b></summary>

```yaml
apiVersion: v1
kind: Service
metadata:
  name: socks-proxy-external
  namespace: socks-proxy
  labels:
    app.kubernetes.io/name: socks-proxy
  annotations:
    # Only needed with MetalLB. Pick an unused IP from the pool.
    metallb.universe.tf/loadBalancerIPs: "<LOAD_BALANCER_IP>"
spec:
  type: LoadBalancer
  externalTrafficPolicy: Local
  selector:
    app.kubernetes.io/name: socks-proxy
  ports:
  - name: socks-tcp
    port: 1888
    targetPort: 1888
    protocol: TCP
  loadBalancerSourceRanges:
  - <DPU_NETWORK_CIDR>
```

</details>

Without a `LoadBalancer` implementation, a `NodePort` Service works as well. The proxy is then reachable on the IP of
any node that the DPUs can reach, at the allocated node port:

<details markdown="1"><summary><b>NodePort Service</b></summary>

```yaml
apiVersion: v1
kind: Service
metadata:
  name: socks-proxy-external
  namespace: socks-proxy
  labels:
    app.kubernetes.io/name: socks-proxy
spec:
  type: NodePort
  selector:
    app.kubernetes.io/name: socks-proxy
  ports:
  - name: socks-tcp
    port: 1888
    targetPort: 1888
    nodePort: 31199
    protocol: TCP
```

</details>

> [!WARNING]
> The proxy configuration and Services above are an example and are not meant for production use. They set
> `auth none` and `allow *`, so anyone who can reach the endpoint can use the proxy to reach anything the proxy can
> reach. In production, build access to the SOCKS proxy in a secure way, for example with ACLs, firewall rules and
> Network Policies that only allow the DPU network to connect. Proxy authentication is not covered by this guide, see
> [Limitations](#limitations).

## Configure the DPUFlavor

`containerd` on the DPU reads its proxy settings from the environment of its systemd unit. Add a systemd drop-in with
the `configFiles` field of the `DPUFlavor`.

> [!NOTE]
> The `configFiles` of a `DPUFlavor` are immutable, so this entry cannot be added to an existing `DPUFlavor`
> by editing it. Include it in a new `DPUFlavor` and use that flavor to provision or reprovision the DPUs.

```yaml
apiVersion: provisioning.dpu.nvidia.com/v1alpha1
kind: DPUFlavor
metadata:
  name: <your-dpuflavor>
  namespace: dpf-operator-system
spec:
  # ... the rest of the DPUFlavor ...
  configFiles:
  - operation: override
    path: /etc/systemd/system/containerd.service.d/socks-proxy.conf
    permissions: "0644"
    raw: |
      [Service]
      Environment="HTTPS_PROXY=socks5://<PROXY_IP>:<PROXY_PORT>"
      Environment="https_proxy=socks5://<PROXY_IP>:<PROXY_PORT>"
      Environment="NO_PROXY=.cluster.local,10.0.0.0/8,172.16.0.0/12,192.168.0.0/16,localhost"
      Environment="no_proxy=.cluster.local,10.0.0.0/8,172.16.0.0/12,192.168.0.0/16,localhost"
```

Replace:

* `<PROXY_IP>` and `<PROXY_PORT>` with the `LoadBalancer` IP and port `1888`, or with a node IP and the node port when
  using `NodePort` (for example `socks5://10.0.110.2:31199`).
* The `NO_PROXY` / `no_proxy` list with the CIDRs of your internal networks. Traffic to these destinations bypasses the
  proxy. It must at least cover the Kubernetes API server of the DPU cluster and the management network; otherwise
  components such as Multus fail to reach the API server.

If you render the flavor per DPU, the same `configFiles` entry can be used in a `DPUFlavorTemplate`, see
[DPUFlavorTemplate: Per-DPU Flavor Rendering](dpuflavortemplate-per-dpu-config.md).

## Verify

On the DPU, confirm that `containerd` has the proxy in its environment and that a pull succeeds:

```bash
systemctl show containerd --property=Environment
crictl pull <image>
```

On the proxy, the `3proxy` log shows the connections made on behalf of the DPU:

```bash
kubectl -n socks-proxy logs deploy/socks-proxy
```

> [!NOTE]
> To validate that nothing bypasses the proxy, test in an environment where the DPU actually has no internet
> connectivity, rather than one where the proxy merely happens to be used.

## Limitations

* The proxy URL is plain configuration in the `DPUFlavor`; credentials in it would be stored in clear text and are not
  supported by this setup. Use network-level restrictions to control access to the proxy.
* Only `containerd` is configured. Other traffic originating on the DPU is not proxied.
* The `NO_PROXY` list is not derived automatically and must match your network layout.
