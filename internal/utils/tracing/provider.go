/*
Copyright 2026 NVIDIA

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// This file bootstraps the process-wide OTLP TracerProvider each controller binary's main.go
// registers before starting its manager: CLI flags (FlagOptions/AddFlags), the setup entry point
// (SetupTracerProviderFromFlags/NewTracerProvider), and the OTLP exporter/TLS internals they use.
// See tracing.go for the controller-facing span API (ReconcileSpan, StartSpan, EndSpan, ...).
package tracing

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/spf13/pflag"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	sdkresource "go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.41.0"
	"google.golang.org/grpc/credentials"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
)

// Transport is the OTLP transport used to export spans.
type Transport string

const (
	// TransportHTTP exports spans over OTLP/HTTP.
	TransportHTTP Transport = "http"
	// TransportGRPC exports spans over OTLP/gRPC.
	TransportGRPC Transport = "grpc"
	// DefaultTransport is the transport the --otel-tracing-transport flag defaults to.
	DefaultTransport = TransportHTTP
	// DefaultSamplingPercent is the value the --otel-tracing-sampling-percent flag defaults to.
	DefaultSamplingPercent = 100
	// DefaultRolloutTimeout is the value the --otel-tracing-rollout-timeout flag defaults to.
	DefaultRolloutTimeout = time.Hour

	// shutdownTimeout bounds how long flushing the pending spans may block process exit.
	shutdownTimeout = 5 * time.Second

	// dropAttribute marks a span that must not be exported. ReconcileSpan sets it on a reconcile span
	// that is not wanted, which is only known when the reconcile ends.
	dropAttribute = "dpf.tracing.drop"
)

// FlagOptions holds the CLI flag values controlling the TracerProvider bootstrap. Tracing is
// disabled while Endpoint is empty.
type FlagOptions struct {
	// Endpoint is the OTLP endpoint to export trace spans to. Empty disables tracing.
	Endpoint string
	// Transport is TransportHTTP or TransportGRPC. AddFlags defaults it to DefaultTransport.
	Transport Transport
	// CA is the PEM-encoded CA bundle content, not a file path. Inline content means a CA
	// rotation changes the Deployment pod template and rolls the pod, which a mounted file would not.
	CA string
	// RecordIdleReconciles exports the spans of idle reconciles, see ReconcileSpan.
	RecordIdleReconciles bool
	// RecordUnchangedReconciles exports the spans of reconciles that changed nothing on their object,
	// did not fail and have no child spans.
	RecordUnchangedReconciles bool
	// SamplingPercent is the share of the traces that is exported, from 0 to 100. AddFlags defaults it
	// to DefaultSamplingPercent.
	SamplingPercent int
	// RolloutTimeout is how long a change may take to become ready before its rollout is reported as
	// timed out, and how long its trace collects reconciles. AddFlags defaults it to
	// DefaultRolloutTimeout. Zero turns the timeout off.
	RolloutTimeout time.Duration
}

// AddFlags registers the --otel-tracing-* flags on fs, writing into opts. Mirrors the
// logsv1.AddFlags(logOptions, fs) pattern binaries already use for klog setup: call this during
// flag registration, then pass the same opts to SetupTracerProviderFromFlags after parsing.
func AddFlags(opts *FlagOptions, fs *pflag.FlagSet) {
	fs.StringVar(&opts.Endpoint, "otel-tracing-endpoint", "", "OTLP endpoint to export trace spans to. If empty, tracing is disabled.")
	opts.Transport = DefaultTransport
	fs.Func("otel-tracing-transport", fmt.Sprintf("OTLP transport used to export trace spans (%s or %s). Defaults to %s.", TransportGRPC, TransportHTTP, DefaultTransport), func(value string) error {
		switch transport := Transport(value); transport {
		case TransportGRPC, TransportHTTP:
			opts.Transport = transport
			return nil
		default:
			return fmt.Errorf("invalid OTLP transport %q: use %s or %s", value, TransportGRPC, TransportHTTP)
		}
	})
	fs.StringVar(&opts.CA, "otel-tracing-ca-pem", "", "PEM-encoded CA certificate bundle content used to verify the otel-tracing-endpoint TLS certificate.")
	fs.BoolVar(&opts.RecordIdleReconciles, "otel-tracing-record-idle-reconciles", false, "Export the spans of idle reconciles, which start at the resourceVersion the last successful reconcile left behind.")
	fs.BoolVar(&opts.RecordUnchangedReconciles, "otel-tracing-record-unchanged-reconciles", false, "Export the spans of reconciles that changed nothing on their object, did not fail and have no child spans.")
	fs.IntVar(&opts.SamplingPercent, "otel-tracing-sampling-percent", DefaultSamplingPercent, "Share of the traces that is exported, from 0 to 100. The decision is made per trace.")
	fs.DurationVar(&opts.RolloutTimeout, "otel-tracing-rollout-timeout", DefaultRolloutTimeout, "How long a change may take to become ready before its rollout span is exported as timed out. 0 turns the timeout off.")
}

// validate returns an error when opts holds a setting that cannot be applied.
func (o FlagOptions) validate() error {
	if o.SamplingPercent < 0 || o.SamplingPercent > 100 {
		return fmt.Errorf("invalid sampling percent %d: use a value from 0 to 100", o.SamplingPercent)
	}
	if o.RolloutTimeout < 0 {
		return fmt.Errorf("invalid rollout timeout %s: use a positive duration", o.RolloutTimeout)
	}
	return nil
}

// dropProcessor does not pass the spans marked with dropAttribute on to the processor it wraps.
type dropProcessor struct {
	sdktrace.SpanProcessor
}

// OnEnd drops a span marked with dropAttribute.
func (p dropProcessor) OnEnd(s sdktrace.ReadOnlySpan) {
	for _, kv := range s.Attributes() {
		if string(kv.Key) == dropAttribute {
			return
		}
	}
	p.SpanProcessor.OnEnd(s)
}

// SetupTracerProviderFromFlags builds the TracerProvider from opts (see NewTracerProvider),
// registers it as the global TracerProvider together with the W3C trace context propagator, and
// returns a shutdown func that is always safe to defer. It flushes pending spans and logs its own
// error. When tracing is disabled (opts.Endpoint == "") nothing is registered, the global
// otel.Tracer stays the default no-op tracer and the shutdown func does nothing:
//
//	var tracingOpts tracing.FlagOptions
//	tracing.AddFlags(&tracingOpts, fs)
//	// ... pflag.Parse() ...
//	shutdownTracing, err := tracing.SetupTracerProviderFromFlags(context.Background(), serviceName, tracingOpts)
//	if err != nil { ... }
//	defer shutdownTracing()
func SetupTracerProviderFromFlags(ctx context.Context, serviceName string, opts FlagOptions) (shutdown func(), err error) {
	if opts.Endpoint == "" {
		return func() {}, nil
	}
	if err := opts.validate(); err != nil {
		return nil, err
	}
	tp, err := NewTracerProvider(ctx, serviceName, opts.Endpoint, opts.Transport, opts.CA, opts.SamplingPercent)
	if err != nil {
		return nil, err
	}
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagator)
	reconciles.Store(newTracker(opts))
	log := ctrllog.FromContext(ctx)
	log.Info("Tracing enabled", "service", serviceName, "endpoint", opts.Endpoint, "transport", opts.Transport)
	return func() {
		ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		if err := tp.Shutdown(ctx); err != nil {
			log.Error(err, "unable to shut down tracer provider")
		}
	}, nil
}

// NewTracerProvider builds a TracerProvider exporting spans over OTLP to endpoint. It does not
// register it globally, see SetupTracerProviderFromFlags. An empty endpoint is an error.
//
// transport is TransportHTTP or TransportGRPC. caPEM, when non-empty, is PEM-encoded CA
// certificate bundle content (not a file path) used to verify the endpoint's TLS certificate in
// addition to the system trust store. It is only consulted for endpoints that enable TLS (an
// "https://" prefix, see parseEndpoint).
//
// samplingPercent is the share of the traces that is exported, from 0 to 100. The decision is made
// from the trace ID, so all spans of a trace are kept or dropped together, including those that have
// a remote parent.
func NewTracerProvider(ctx context.Context, serviceName, endpoint string, transport Transport, caPEM string, samplingPercent int) (*sdktrace.TracerProvider, error) {
	exporter, err := newExporter(ctx, endpoint, transport, caPEM)
	if err != nil {
		return nil, fmt.Errorf("failed to create OTLP trace exporter: %w", err)
	}

	res, err := sdkresource.New(ctx,
		sdkresource.WithAttributes(semconv.ServiceName(serviceName)),
		sdkresource.WithFromEnv(),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to build tracing resource: %w", err)
	}

	return sdktrace.NewTracerProvider(
		sdktrace.WithSpanProcessor(dropProcessor{sdktrace.NewBatchSpanProcessor(exporter)}),
		sdktrace.WithResource(res),
		sdktrace.WithIDGenerator(idGenerator{}),
		sdktrace.WithSampler(newSampler(samplingPercent)),
	), nil
}

// newSampler returns the sampler that keeps percent of the traces DPF starts. The decision comes from
// the trace ID, so all spans of a trace are kept or dropped together. A trace with a remote parent
// follows the decision of the parent, so the owner of an external trace decides about its sampling.
// The traces DPF derives carry the decision of the ratio in their sampled flag, see keeps.
func newSampler(percent int) sdktrace.Sampler {
	return sdktrace.ParentBased(newRatioSampler(percent))
}

// newRatioSampler returns the sampler that keeps percent of the trace IDs.
func newRatioSampler(percent int) sdktrace.Sampler {
	return sdktrace.TraceIDRatioBased(float64(percent) / 100)
}

// newExporter builds the OTLP span exporter for endpoint over transport.
func newExporter(ctx context.Context, endpoint string, transport Transport, caPEM string) (sdktrace.SpanExporter, error) {
	target, secure, err := parseEndpoint(endpoint)
	if err != nil {
		return nil, err
	}
	// A nil tlsCfg means a plaintext connection.
	var tlsCfg *tls.Config
	if secure {
		if tlsCfg, err = tlsConfigFromCAPEM(caPEM); err != nil {
			return nil, err
		}
	}

	switch transport {
	case TransportGRPC:
		return newGRPCExporter(ctx, target, tlsCfg)
	case TransportHTTP:
		return newHTTPExporter(ctx, target, tlsCfg)
	default:
		return nil, fmt.Errorf("unsupported tracing transport %q, must be %q or %q", transport, TransportGRPC, TransportHTTP)
	}
}

// newHTTPExporter builds an OTLP/HTTP span exporter, plaintext when tlsCfg is nil.
func newHTTPExporter(ctx context.Context, target string, tlsCfg *tls.Config) (sdktrace.SpanExporter, error) {
	opts := []otlptracehttp.Option{otlptracehttp.WithEndpoint(target)}
	if tlsCfg != nil {
		opts = append(opts, otlptracehttp.WithTLSClientConfig(tlsCfg))
	} else {
		opts = append(opts, otlptracehttp.WithInsecure())
	}
	return otlptracehttp.New(ctx, opts...)
}

// newGRPCExporter builds an OTLP/gRPC span exporter, plaintext when tlsCfg is nil.
func newGRPCExporter(ctx context.Context, target string, tlsCfg *tls.Config) (sdktrace.SpanExporter, error) {
	opts := []otlptracegrpc.Option{otlptracegrpc.WithEndpoint(target)}
	if tlsCfg != nil {
		opts = append(opts, otlptracegrpc.WithTLSCredentials(credentials.NewTLS(tlsCfg)))
	} else {
		opts = append(opts, otlptracegrpc.WithInsecure())
	}
	return otlptracegrpc.New(ctx, opts...)
}

// parseEndpoint returns the host:port to dial and whether TLS should be used, matching the format
// documented on DPFOperatorConfig spec.monitoring.openTelemetryCollector.tracing.endpoint. An
// endpoint without a scheme is a plaintext host:port.
func parseEndpoint(endpoint string) (target string, secure bool, err error) {
	if endpoint == "" {
		return "", false, fmt.Errorf("endpoint must not be empty")
	}
	rawURL := endpoint
	if !strings.Contains(endpoint, "://") {
		rawURL = "http://" + endpoint
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", false, fmt.Errorf("invalid tracing endpoint %q: %w", endpoint, err)
	}
	if u.Host == "" {
		return "", false, fmt.Errorf("tracing endpoint %q has no host", endpoint)
	}
	if u.Path != "" && u.Path != "/" {
		return "", false, fmt.Errorf("tracing endpoint %q must not contain a path", endpoint)
	}
	switch u.Scheme {
	case "https":
		return u.Host, true, nil
	case "http":
		return u.Host, false, nil
	default:
		return "", false, fmt.Errorf("unsupported tracing endpoint scheme %q", u.Scheme)
	}
}

// tlsConfigFromCAPEM returns a TLS config trusting the system roots plus any PEM certificates in
// caPEM. caPEM may be empty, in which case only the system roots are trusted.
func tlsConfigFromCAPEM(caPEM string) (*tls.Config, error) {
	pool, err := x509.SystemCertPool()
	if err != nil || pool == nil {
		pool = x509.NewCertPool()
	}
	if caPEM != "" {
		if !pool.AppendCertsFromPEM([]byte(caPEM)) {
			return nil, fmt.Errorf("no valid certificates found in tracing CA PEM content")
		}
	}
	return &tls.Config{RootCAs: pool}, nil
}
