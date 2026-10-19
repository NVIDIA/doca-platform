# Tracing

DPF controllers can export OpenTelemetry trace spans for their reconcile
loops. The shared helpers live in `internal/utils/tracing` and are used by
every instrumented controller the same way, so a request can be followed
across controllers (and, once an upstream caller like carbide injects a W3C
`traceparent`, across services) as a single trace instead of disconnected
per-controller logs.

For how to enable tracing on a running deployment and view the result, see
[Tracing](../../operational-readiness/observability/deployment/operator-managed-components.md#tracing)
in the operator-managed-components guide. This page covers instrumenting
code.

## Instrumenting a reconcile loop

Call `tracing.ReconcileSpan` once the reconciled object has been fetched, and
defer the function it returns so the span's duration covers the whole reconcile
and any error is recorded on it:

```go
dpuService := &dpuservicev1.DPUService{}
if err := r.Client.Get(ctx, req.NamespacedName, dpuService); err != nil {
    return ctrl.Result{}, client.IgnoreNotFound(err)
}

ctx, endSpan := tracing.ReconcileSpan(ctx, DPUServiceControllerName, dpuService)
defer endSpan(&reterr)
```

`reterr` must be the function's named return so errors set later in the
reconcile (including by a deferred patch call) still land on the span. Place
the call after the `Get` succeeds, not before: `ReconcileSpan` reads span
attributes (name, namespace, UID, generation, controller owner) and parent trace context off
the object itself, so it needs a populated object to work with.

## Propagating trace context to child objects

`ReconcileSpan` only *reads* a parent trace from the object's annotations —
nothing writes it automatically. `InjectIntoObject` copies the trace context
the reconciled object arrived with, unchanged, so repeated reconciles write
identical annotations and cause no extra updates. When the reconciled object
carries no trace context (the caller is not tracing-aware), `ReconcileSpan`
starts a trace derived from the object's UID and generation, so the object and
the children it creates still share one trace. A spec change bumps the
generation and starts a new trace. If your reconcile creates or patches an
object another controller will reconcile (and that controller is also
instrumented), call `tracing.InjectIntoObject` before the `Create`/`Patch`
call so that controller's span continues the same trace instead of starting
a new, disconnected one:

```go
newRevision.SetGroupVersionKind(dpuservicev1.DPUServiceChainGroupVersionKind)
tracing.InjectIntoObject(ctx, newRevision)
if err := c.Patch(ctx, newRevision, client.Apply, client.ForceOwnership, client.FieldOwner(dpuDeploymentControllerName)); err != nil {
    return fmt.Errorf("failed to patch DPUServiceChain %s: %w", client.ObjectKeyFromObject(newRevision), err)
}
```

This is easy to miss: without it, every controller-created object still
produces a span, but each one roots its own trace instead of joining the
trace of whatever caused it to be created, which defeats the point of
tracing a request across controllers. Audit every `Create`/`Patch` call your
controller makes against an object type that is (or will be) instrumented,
not just your own top-level `ReconcileSpan` call.

## Spans for a slow or meaningful sub-operation

Use `tracing.StartSpan`/`tracing.EndSpan` for an operation worth seeing on
its own in the trace — a network call (e.g. Redfish to a BMC), a Kubernetes
API call, or a slow phase of a longer reconcile. Don't instrument every
internal function call; a span for everything is as unhelpful as a span for
nothing.

```go
ctx, span := tracing.StartSpan(ctx, "dpu.redfish.powerOn")
err := r.redfishClient.PowerOn(ctx, dpu)
tracing.EndSpan(span, err)
```

Add data to the current span with `tracing.AddAttributes`. Never add
secrets, and sanitize URLs (strip credentials/query params) before adding
them:

```go
tracing.AddAttributes(ctx, attribute.String("bmc.address", sanitizedAddress))
```

## Wiring a new binary

Every instrumented binary's `main.go` follows the same shape (see
`cmd/dpuservice/main.go` for a complete example):

```go
var tracingOpts tracing.FlagOptions
tracing.AddFlags(&tracingOpts, fs)

// ... flag parsing ...

shutdownTracing, err := tracing.SetupTracerProviderFromFlags(context.Background(), "dpf-my-controller", tracingOpts)
if err != nil {
    setupLog.Error(err, "unable to set up tracing")
    os.Exit(1)
}
defer shutdownTracing()
```

The second argument is the service name Tempo and Grafana group the binary's
spans under. Name it `dpf-<component>-controller`, for example
`dpf-dpuservice-controller`, and keep it in a `TracingServiceName` constant
next to the controllers of the binary.

`shutdownTracing` flushes pending spans and logs its own error. It is always
safe to defer, including when tracing is disabled (no `--otel-tracing-endpoint`
flag set). It does nothing in that case, so callers never need to branch on
whether tracing is enabled.

To pass the `DPFOperatorConfig` `tracing` config to the binary, use the helper
that matches how the operator deploys it:

* As a Deployment: `setTracingFlags` in `internal/operator/inventory`.
* As a DPUService with a Helm chart: add `tracing.endpoint`, `transport` and
  `caPEM` values to the chart and set them with `tracingValueEdits`.

The `dpf-operator` itself is not traced.
