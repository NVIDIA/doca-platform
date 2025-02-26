# DOCA Blueman service

Requirements:
* Meet the requirements of [DTS guide](../dts/README.md)
* Apply "Hacks for DPF" from [DTS guide](../dts/README.md)
* DTS service should run (see the [DTS guide](../dts/README.md))

# Run Service

Assuming DTS service is running, manually log into Bluefield(s) and run:

```
systemctl start dpe
```

After that, from DPF host node start service with:
```
kubectl apply -f DPUService.yaml
```

Check that pods are running:

```
kubectl get secret  -n dpu-cplane-tenant1  dpu-cplane-tenant1-admin-kubeconfig  -o jsonpath='{.data.admin\.conf}' | base64 -d  > config.yaml

kubectl get pods -A --kubeconfig=config.yaml  | grep doca
```

# Verify service is running:

Set IP tables from the DPF host with BF physically installed:

```
export DPF_BF_IP= #set BF IP

iptables -t nat -A PREROUTING -p tcp --dport 10000 -j DNAT --to-destination $DPF_BF_IP:10000
iptables -t nat -A PREROUTING -p tcp --dport 443 -j DNAT --to-destination $DPF_BF_IP:443

```

Open in browser to access UI:
```
export DPF_HOST_IP=  #set host IP
https://$DPF_HOST_IP/login
```

