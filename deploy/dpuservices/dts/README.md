# DOCA Telemetry Service
Requirements:
* NGC_API key to access `doca/nvstaging`.
* DPU as part of DPF setup


# Hacks for DPF
## Prepare secrets file
Make sure that `doca/nvstaging` secrets are applied to the setup.
Prepare the secrets file:

```
cat << EOF | kubectl apply -f -
apiVersion: v1
kind: Secret
metadata:
  name: dpf-helm-secret
  namespace: dpf-operator-system
  labels:
    argocd.argoproj.io/secret-type: repository
stringData:
  name: doca-helm
  url: https://helm.ngc.nvidia.com/nvstaging/doca
  type: helm
  username: \$oauthtoken
  password: $NGC_API_KEY

EOF
```


## Access private NGC docker registry

In DPUService.yaml under:
```
spec:
  helmChart:
```

add:
```
    values:
      - name: dpf-pull-secret
```

# Run Service
Start service with:
```
kubectl apply -f DPUService.yaml
```

Get config for a cluster containing DPU:

```
kubectl get secret  -n dpu-cplane-tenant1  dpu-cplane-tenant1-admin-kubeconfig  -o jsonpath='{.data.admin\.conf}' | base64 -d  > config.yaml
```

Check that pods are running:

```
kubectl get pods -A --kubeconfig=config.yaml  | grep doca
```



# Verification

Get `$DTS_POD_NAME`.

Check logs to see that all the processes are spawned
```
kubectl logs -n dpf-operator-system $DTS_POD_NAME --kubeconfig=config.yaml

Defaulted container "doca-telemetry-service" out of: doca-telemetry-service, init-telemetry-service (init)
2024-09-22 12:26:37,644 INFO Set uid to user 0 succeeded
2024-09-22 12:26:37,650 INFO supervisord started with pid 7
2024-09-22 12:26:38,655 INFO spawned: 'blueman_service' with pid 9
2024-09-22 12:26:38,661 INFO spawned: 'clx' with pid 10
2024-09-22 12:26:38,667 INFO spawned: 'doca_config_watcher' with pid 11
2024-09-22 12:26:38,672 INFO spawned: 'on_startup' with pid 12
2024-09-22 12:26:38,694 INFO success: on_startup entered RUNNING state, process has stayed up for > than 0 seconds (startsecs)
2024-09-22 12:26:38,698 INFO exited: on_startup (exit status 0; expected)
2024-09-22 12:26:40,276 INFO success: blueman_service entered RUNNING state, process has stayed up for > than 1 seconds (startsecs)
2024-09-22 12:26:40,276 INFO success: clx entered RUNNING state, process has stayed up for > than 1 seconds (startsecs)
2024-09-22 12:26:40,277 INFO success: doca_config_watcher entered RUNNING state, process has stayed up for > than 1 seconds (startsecs)
```

See the metrics at prometheus endpoint:
```
kubectl exec -it -n dpf-operator-system $DTS_POD_NAME --kubeconfig=config.yaml curl 0.0.0.0:9100/metrics
```