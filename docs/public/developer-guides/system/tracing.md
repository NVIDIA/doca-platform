# Tracing

DPF controllers can export OpenTelemetry trace spans for their reconcile
loops. The shared helpers live in `internal/utils/tracing` and are used by
every instrumented controller the same way.

One change of a top-level object, such as a DPUDeployment, is one trace. All
reconciles of the object and of everything it creates join it, so they show up
together in the backend.

The trace ID comes from the `traceparent` annotation when the object has one.
Otherwise DPF derives it from the object's UID and generation, so a spec change
starts a new trace. The ID is also set as the `workflow.id` span attribute, so
you can search for it.

The trace is flat, its spans have no parent and child between them. Controllers
do not call each other, they react to objects, so DPF cannot tell which
reconcile caused another. All of them sit side by side under the root of the
change: the rollout span, or the upstream span when the caller set the trace.
Read the order from the timestamps.

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
reconcile (including by a deferred patch call) still land on the span. An error
sets the error status and the `error.type` attribute, as the OpenTelemetry
convention for recording errors asks. Place
the call after the `Get` succeeds, not before: `ReconcileSpan` reads span
attributes (name, namespace, UID, generation, controller owner) and the workflow ID off
the object itself, so it needs a populated object to work with.

## Propagating the trace to child objects

`ReconcileSpan` only *reads* the trace from the object's trace context
annotations, nothing writes it automatically. The reconcile becomes a child of
the span in the annotation, so a trace set by an upstream caller like carbide
continues through DPF. When the reconciled object carries none (the caller is
not tracing-aware), the controller of a top-level object passes
`tracing.WorkflowRoot()` and `ReconcileSpan` derives the trace from the
object's UID and generation. Any other controller starts a trace of its own
with each reconcile in that case, because nothing would export the root of a
derived trace for it, and it does not pass the derived value on.

There are two pairs of annotations, so DPF never overwrites what a user or
consumer set. A user or consumer sets `tracing.k8s.io/traceparent` and
`tracing.k8s.io/tracestate`, and DPF never writes them. `InjectIntoObject`
writes `tracing.k8s.io/generated-traceparent` and
`tracing.k8s.io/generated-tracestate` on the objects DPF creates. `ReconcileSpan` reads the consumer pair first and the
generated pair second. DPF removes the consumer pair only where it replaces or
ends the trace of an object: `InjectFromObject` removes it from the object it
copies the trace of another object onto, and `RemoveFromObject` and
`EndObjectTrace` remove both pairs. For the objects the provisioning controllers read a trace from, see
[Setting the trace context](../../operational-readiness/observability/deployment/operator-managed-components.md#setting-the-trace-context).

`InjectIntoObject` copies the trace context unchanged, so repeated reconciles
write identical annotations and cause no extra updates. If your reconcile
creates or patches an object another controller will reconcile (and that
controller is also instrumented), call `tracing.InjectIntoObject` before the
`Create`/`Patch` call so that controller's span joins the same trace:

```go
newRevision.SetGroupVersionKind(dpuservicev1.DPUServiceChainGroupVersionKind)
tracing.InjectIntoObject(ctx, newRevision)
if err := c.Patch(ctx, newRevision, client.Apply, client.ForceOwnership, client.FieldOwner(dpuDeploymentControllerName)); err != nil {
    return fmt.Errorf("failed to patch DPUServiceChain %s: %w", client.ObjectKeyFromObject(newRevision), err)
}
```

This is easy to miss: without it, every controller-created object still
produces a span, but it starts a trace of its own instead of joining the
one of the change that created it. Audit every `Create`/`Patch` call your
controller makes against an object type that is (or will be) instrumented,
not just your own top-level `ReconcileSpan` call.

With tracing off, `ReconcileSpan` returns at once and `InjectIntoObject` writes
nothing. It removes the generated annotations from the object instead, so the
`Create`/`Patch` that follows clears them from objects written while tracing was
on. Keep the call before every write of a child for that reason. An object a
controller does not write anymore keeps its annotations.

An object that no DPF controller creates, such as a Pod, never gets the
annotations. To let its reconcile join the trace of a related object, pass
`tracing.JoinWorkflowOf` to `ReconcileSpan` with a function that returns the
candidates. The reconcile joins the first candidate that carries a trace, and
starts a trace of its own when none does. The function only runs when tracing
is on and the object carries none, so it can look the candidates up. The Pod
IPAM injector passes the ServiceChains of the Pod's service, then its
ServiceInterfaces. A Pod with neither, such as one that only uses virtual
networks in NodeServiceInterfaces mode, keeps a trace of its own.

## Idle reconciles

A reconcile that starts at the `resourceVersion` the last successful reconcile
of that object left behind is idle: nothing changed the object since, so it is
a requeue or a resync. `ReconcileSpan` does not record it, and neither the
spans started below it. A failed reconcile never makes the next one idle. An
idle reconcile that fails or changes the object after all is exported when it
ends, with `reconcile.idle_at_start` set and without its child spans. The
result is that a trace lists only reconciles that had something to do. A reconcile that only changes child objects and succeeds is
still dropped when it starts idle, as its children's own reconciles show the
change.

## Which spans are exported

The binaries take four flags, which the operator sets from the `tracing`
section of the DPFOperatorConfig, see the
[user guide](../../operational-readiness/observability/deployment/operator-managed-components.md#configuration):

* `--otel-tracing-record-idle-reconciles` exports idle reconciles too.
* `--otel-tracing-record-unchanged-reconciles` exports reconciles that changed
  nothing on their object, did not fail and started no child span with
  `StartSpan`. Such a reconcile only says that it ran, so it is dropped by
  default. A span of a controller that only patches its children without a child
  span is dropped too, so give those patches a span when they matter.
* `--otel-tracing-sampling-percent` keeps a share of the traces DPF starts. The
  decision comes from the trace ID, so all spans of a change are kept or dropped
  together. A trace with an upstream traceparent keeps the decision of its owner,
  which is in the sampled flag of the traceparent.
* `--otel-tracing-rollout-timeout` is when an unfinished rollout is reported as
  timed out. It is also how long a trace keeps collecting reconciles, see below.
  `0` turns it off.

Add a new setting as a flag in `tracing.AddFlags` and a field of the DPFOperatorConfig
the same way, and pass it from `setTracingFlags` and `tracingValueEdits` only
when it differs from the default.

## Reporting a change and its rollout

A rollout is one generation of an object reaching readiness. The controller of
a top-level object passes `tracing.WorkflowRoot()` to `ReconcileSpan` and calls
`tracing.ObserveRollout` from each reconcile, once the status is computed:

```go
ctx, endSpan := tracing.ReconcileSpan(ctx, dpuDeploymentControllerName, dpuDeployment, tracing.WorkflowRoot())
defer endSpan(&reterr)
// ...
tracing.ObserveRollout(ctx, dpuDeploymentControllerName, dpuDeployment,
    conditions.IsTrue(dpuDeployment, conditions.TypeReady) && dpuDeployment.Status.ObservedGeneration == dpuDeployment.Generation)
```

A change can also come from an input of the object that has its own generation,
such as the BFB, DPUFlavor, DPUServiceTemplate or DPUServiceConfiguration a
DPUDeployment references. Its own generation stays, so the controller passes
`tracing.Revision` with a value that changes with those inputs, see
`tracingRevision` in the DPUDeployment controller. The value includes each
input's UID and generation, so deleting an input and recreating it under the
same name starts a new trace even when the new object has the same generation. A new revision starts a new trace and a new rollout,
and its reconcile is not idle:

```go
ctx, endSpan := tracing.ReconcileSpan(ctx, dpuDeploymentControllerName, dpuDeployment, tracing.WorkflowRoot(),
    tracing.Revision(tracingRevision(ctx, r.Client, dpuDeployment)))
```

A spec change of an input counts, because it bumps the generation, and so does
replacing the input with a new object. Labels and annotations do not. The
rollout of a revision change has no observed generation to wait for, so it can
end earlier than the children settle.

The rollout starts when the process first sees a generation or revision. For an
object without an upstream trace context, the trace has two root spans. The
first is an instant `<controller>.change` span, exported right away so the
trace has a root while the change is in progress. The second is the
`<controller>.rollout` span, exported when the object is ready, when the
rollout timeout has passed, an hour by default (`rollout.result` is `timeout`
and the span has an error status), or
when a newer generation replaces it (`superseded`). It covers the real
duration, so the time a generation took to become ready is visible at a
glance, and it is the parent of the reconciles of the object and of everything
it creates. Until it is exported, those reconciles hang under a span the
backend does not have yet. The rollout is the end of the change, but nothing
closes the trace, so reconciles of the same generation that run after it ended
still join it, for as long as the rollout timeout, counted from when the
process first saw the change. A later reconcile starts a trace of its own with
a span link to the span of the change, or to the span of the upstream trace
context. It keeps the `workflow.id` attribute, so a query for it finds the
change and all of its traces. This keeps a trace short, as a trace should not
grow for hours. The timeout is checked when the object is reconciled. A
reconcile can come much later, so a rollout that was not ready by its deadline
ends at the deadline, not when the reconcile noticed.

An object that is ready the first time the process sees it has no rollout
span, as its start is unknown. Its change span is then the root and takes the
span ID the reconciles point at, so they do not hang under a missing span.
A restart loses a rollout that was in progress. When an upstream caller set the
trace, it owns the root and the end of that trace, and DPF exports only the
rollout span as a child of it.

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

A child span has no attributes of the reconciled object, its parent has them.
Find it with a structural query, which works as the reconcile span is the
parent of its child spans:

```text
{ span.k8s.resource.kind = "DPU" && span.k8s.object.name = "<name>" } > { span.dpu.phase = "OS Installing" }
```

A reconcile that changed nothing and started no child span is dropped, but a
child span keeps its reconcile. When an operation did nothing, such as a check
that found everything up to date, call `tracing.MarkUnchanged` with the context
of its span. That span is then dropped too, and a reconcile that only ran such
operations is dropped like one without child spans. Call it only for an
operation that did not fail.

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
