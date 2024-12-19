# How to request Cluster Credential For DPUService

In order to request credential to access a cluster API Server, users should deploy
a `DPUServiceCredentialRequest` custom resource.

## Example

The following is an example of a `DPUServiceCredentialRequest` which requests credentials
to access a cluster with a duration of 1h.

```yaml
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceCredentialRequest
metadata:
  name: dpu-01-credential-request
  namespace: default
spec:
  serviceAccount:
    name: dpu-01-sa
    namespace: default
  duration: 1h
  targetCluster: 
    name: dpu-cplane-tenant1
    namespace: default
  type: kubeconfig
  secret:
    name: dpu-01-credential
    namespace: default
  metadata:
    labels:
      dpuservice: dpu-01
```

In the above example:
- A `DPUServiceCredentialRequest` named `dpu-01-credential-request` is created, that
  request a `ServiceAccount` to be created in a specific cluster. As part of the request,
  a token has to be requested for the `ServiceAccount` with a `ttl` of 1h. The output
  that is expected is a secret of type `kubeconfig`, containing all the needed information
  in order to access the cluster i.e. the token, API Server endpoint and the cluster CA.
- `targetCluster` is a reference to an existing `dpuCluster` that will provide the
  needed authentication informatio to access the API SERVER.
- The controller will create the `ServiceAccount` and request a token with the requested
  duration. It will generate an output of the requested `.spec.type` and create a
  secret with the output pasted as `spec.data`.
- If the `DPUServiceCredentialRequest` does not exist, or is not up-to-date, the controller
  will try to bring it as close to the desired state as possible.
- The controller will requeue the `DPUServiceCredentialRequest` in order to reconcile
  it when 80% of the token last time will expended. It will then request a new token.

You can run this example by saving it into `dpuservice_credential_request.yaml`

```shell
$ kubectl apply -f dpuservice_credential_request.yaml
```

## How to use the DPUServiceCredentialRequest output to get a client

The `DPUServiceCredentialRequest` creates a secret with the following types:
- `kubeconfig`
- `tokenFile`

### Using type kubeconfig

The `kubeconfig` type creates a valid `kubeConfig` file and save it as a kubernetes
secret. This file will automatically be updated with valid authentication information.
The secret can be mounted in a `pod` and use to create a `client` in order to access to
the target cluster API Server. 

If using `client.go`, by mouting the secret and setting `KUBECONFIG` to the mounted path,
we can use [clientcmd](https://pkg.go.dev/k8s.io/client-go/tools/clientcmd) to create
a `client`. [clientcmd](https://pkg.go.dev/k8s.io/client-go/tools/clientcmd) also provides 
the necessary helpers to directly create a `client` using a `kubeConfig` file.

### Using type tokenFile

The `tokenFile` type create a secret containing the following keys:
- `TOKEN_FILE`, contains the token attached to the `ServiceAccount`. It can be used
  as a bearer token.
- `KUBERNETES_SERVICE_HOST`, contains the target cluster API Server host.
- `KUBERNETES_SERVICE_PORT`, contains the target cluster API Server port.
- `KUBERNETES_CA_DATA`, contains the target cluster API Server certificate authority.

These are the needed information in order to access the target cluster API Server.
Generally we want to use the `KUBERNETES*` as environment variables and mount the
`TOKEN_FILE`. Using `client.go` it is possible to have a self rotating `client`. For
this, the default environment variables and files must be overwritten with the content
of the secret. The following example shows how to do so:

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: mypod
  namespace: default
spec:
  containers:
  - name: test-container
    image: ubuntu
    command: ["/bin/sh", "-c"]
    args: ["sleep 1000"]
    env:
    - name: KUBERNETES_SERVICE_HOST
      valueFrom:
        secretKeyRef:
          name: my-secret
          key: KUBERNETES_SERVICE_HOST
    - name: KUBERNETES_SERVICE_PORT
      valueFrom:
        secretKeyRef:
          name: my-secret
          key: KUBERNETES_SERVICE_PORT
    volumeMounts:
    - name: tokenfile
      mountPath: "/var/run/secrets/kubernetes.io/serviceaccount"
      readOnly: true
  volumes:
  - name: tokenfile
    projected:
      sources:
      - secret:
          name: dpu-01-credential
          items:
          - key: TOKEN_FILE
            path: token
      - secret:
          name: dpu-01-credential
          items:
          - key: KUBERNETES_CA_DATA
            path: ca.crt
---
apiVersion: v1
kind: Secret
metadata:
  name: dpu-01-credential
  namespace: default
type: Opaque
data:
  TOKEN_FILE: c29tZSByZWFsIHNlcmlvdXMgdG9rZW4gbm93Cg==
  KUBERNETES_SERVICE_HOST: aHR0cHM6Ly9kZXZlbG9wZXIuY29t
  KUBERNETES_SERVICE_PORT: MTk0NDM=
  KUBERNETES_CA_DATA: LS0tLS1CRUdJTiBDRVJUSUZJQ0FURS0tLS0tCk1JSURERENDQWUyQ0NRRHlQa0JnR3Foa2pPUFFRREFnWUdDQ3FHU000OUJBTUNBMGN4RURBT0JnTlZCQU1NCk1JSUJJakFOQmdrcWhraUc5dzBCQVFFRkFBT0JqUUF3Z1lrQ2dZRUF3ZUFJa2hQY2d3RFFZSktvWklodmNOCk1TRURCREl4TWpBeE1EY3hNakF4TURCYUZ3MHlNREF3TVRBeE16QXhNRm9YRFRJNU1EQXdNVEV4TXpBeE1GCkRUSTVNREF3TVRFeE16QXhNRm93RFRFTE1Ba0dBMVVFQmhNQ1ZWTXhFVEFQQmdOVkJBb01DbXRsZVcxaGJtClpYSXhFekFSQmdOVkJBc01DbTlwYjNOaGJXd3RjM0F3SGhjTk1UUXdOREEzTVRFeU1qQTFXaGNOTWpRd05ECk1ERXlNakExV2hjTk1qUXdOREF6TURNeU1qQTFXakJDTVNnd0pnWURWUVFEREI5b2RIUndjem92TDI1aGMzCllXMXNMbWx2SUhObGNuUnBibVZ5TVEwd0N3WURWUVFERXhCeWIyUmxjeTVwYm1kbGJuUnBibVZ5TVEwd0N3ClZRUURFeEJ5YjJSbGN5NXBibWRsYm5ScGJtVnlNUTB3Q3dZRFZRUURFeEJ5YjJSbGN5NXBibWRsYm5ScGJtCk1RMHdDd1lEVlFRREV4QnliMlJsY3k1cGJtZGxiblJwYm1WeU1RMHdDd1lEVlFRREV4QnliMlJsY3k1cGJtCmJuUnBibVZ5TVEwd0N3WURWUVFERXhCeWIyUmxjeTVwYm1kbGJuUnBibVZ5TVEwd0N3WURWUVFERXhCeWIyCmN5NXBibWRsYm5ScGJtVnlNUTB3Q3dZRFZRUURFeEJ5YjJSbGN5NXBibWRsYm5ScGJtVnlNUTB3Q3dZRFZRCkV4QnliMlJsY3k1cGJtZGxiblJwYm1WeU1RMHdDd1lEVlFRREV4QnliMlJsY3k1cGJtZGxiblJwYm1WeU1RCkN3WURWUVFERXhCeWIyUmxjeTVwYm1kbGJuUnBibVZ5TVEwd0N3WURWUVFERXhCeWIyUmxjeTVwYm1kbGJuCmJtVnlNUTB3Q3dZRFZRUURFeEJ5YjJSbGN5NXBibWRsYm5ScGJtVnlNUTB3Q3dZRFZRUURFeEJ5YjJSbGN5CmJtZGxiblJwYm1WeU1RMHdDd1lEVlFRREV4QnliMlJsY3k1cGJtZGxiblJwYm1WeU1RMHdDd1lEVlFRREV4CmIyUmxjeTVwYm1kbGJuUnBibVZ5TVEwd0N3WURWUVFERXhCeWIyUmxjeTVwYm1kbGJuUnBibVZ5TVEwd0N3ClZRUURFeEJ5YjJSbGN5NXBibWRsYm5ScGJtVnlNUTB3Q3dZRFZRUURFeEJ5YjJSbGN5NXBibWRsYm5ScGJtCk1RMHdDd1lEVlFRREV4QnliMlJsY3k1cGJtZGxiblJwYm1WeU1RMHdDd1lEVlFRREV4QnliMlJsY3k1cGJtCmJuUnBibVZ5TVEwd0N3WURWUVFERXhCeWIyUmxjeTVwYm1kbGJuUnBibVZ5TVEwd0N3WURWUVFERXhCeWIyCmN5NXBibWRsYm5ScGJtVnlNUTB3Q3dZRFZRUURFeEJ5YjJSbGN5NXBibWRsYm5ScGJtVnlNUTB3Q3dZRFZRCkV4QnliMlJsY3k1cGJtZGxiblJwYm1WeU1RMHdDd1lEVlFRREV4QnliMlJsY3k1cGJtZGxiblJwYm1WeU1RCi0tLS0tRU5EIENFUlRJRklDQVRFLS0tLS0K
```

We can then call [InClusterConfig()](https://pkg.go.dev/k8s.io/client-go/rest#InClusterConfig)
and get a self rotating client (This is automatically done with controller-runtime `GetConfigOrDie`).
This will free the user from having to watch the mounted secret files.

**Note** This will overwrite the default values, which are necessary to access the
local API Server. If this is not desirable, it is still possible to adapt the `InClusterConfig()`
logic.


## API Server consideration

The `DPUServiceCredentialRequest` will request tokens for the provided `ServiceAccount`
by setting the `Audience` to the API server audience based on the server config.
The specific configuration flag is `--api-audiences`. The service account token
authenticator will validate that tokens used against the API server are bound to
at least one of these audiences.
