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

package tracing

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"testing"
	"time"

	"github.com/spf13/pflag"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

func newTestCAPEM(t *testing.T) string {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "test-ca"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	require.NoError(t, err)
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
}

func TestAddFlags(t *testing.T) {
	var opts FlagOptions
	fs := pflag.NewFlagSet("test", pflag.ContinueOnError)
	AddFlags(&opts, fs)

	require.NoError(t, fs.Parse([]string{
		"--otel-tracing-endpoint=https://collector:4320",
		"--otel-tracing-transport=grpc",
		"--otel-tracing-ca-pem=PEM-CONTENT",
		"--otel-tracing-record-idle-reconciles",
		"--otel-tracing-record-unchanged-reconciles",
		"--otel-tracing-sampling-percent=25",
		"--otel-tracing-rollout-timeout=30m",
	}))
	require.Equal(t, FlagOptions{
		Endpoint:                  "https://collector:4320",
		Transport:                 "grpc",
		CA:                        "PEM-CONTENT",
		RecordIdleReconciles:      true,
		RecordUnchangedReconciles: true,
		SamplingPercent:           25,
		RolloutTimeout:            30 * time.Minute,
	}, opts)
}

func TestAddFlags_DefaultsDisableTracing(t *testing.T) {
	var opts FlagOptions
	fs := pflag.NewFlagSet("test", pflag.ContinueOnError)
	AddFlags(&opts, fs)
	require.NoError(t, fs.Parse(nil))
	require.Equal(t, FlagOptions{
		Transport:       DefaultTransport,
		SamplingPercent: DefaultSamplingPercent,
		RolloutTimeout:  DefaultRolloutTimeout,
	}, opts, "idle and unchanged reconciles are skipped by default")
}

func TestFlagOptions_Validate(t *testing.T) {
	valid := FlagOptions{
		SamplingPercent: 100,
		RolloutTimeout:  time.Minute,
	}
	require.NoError(t, valid.validate())

	for name, mutate := range map[string]func(*FlagOptions){
		"negative sampling":  func(o *FlagOptions) { o.SamplingPercent = -1 },
		"sampling above 100": func(o *FlagOptions) { o.SamplingPercent = 101 },
		"negative timeout":   func(o *FlagOptions) { o.RolloutTimeout = -time.Second },
	} {
		t.Run(name, func(t *testing.T) {
			opts := valid
			mutate(&opts)
			require.Error(t, opts.validate())
		})
	}
}

func TestNewSampler(t *testing.T) {
	traceID := trace.TraceID{1}
	sample := func(sampler sdktrace.Sampler, parent trace.SpanContext) bool {
		ctx := trace.ContextWithSpanContext(context.Background(), parent)
		params := sdktrace.SamplingParameters{
			ParentContext: ctx,
			TraceID:       traceID,
			Name:          "span",
		}
		return sampler.ShouldSample(params).Decision == sdktrace.RecordAndSample
	}
	remote := func(flags trace.TraceFlags) trace.SpanContext {
		return trace.NewSpanContext(trace.SpanContextConfig{
			TraceID:    traceID,
			SpanID:     trace.SpanID{1},
			TraceFlags: flags,
			Remote:     true,
		})
	}

	require.True(t, sample(newSampler(100), trace.SpanContext{}), "everything is kept at 100 percent")
	require.True(t, sample(newSampler(100), remote(trace.FlagsSampled)))
	require.False(t, sample(newSampler(0), trace.SpanContext{}), "nothing is kept at 0 percent")
	require.True(t, sample(newSampler(0), remote(trace.FlagsSampled)), "the owner of an external trace decides about its sampling")
	require.False(t, sample(newSampler(100), remote(0)), "an unsampled remote parent, an idle reconcile, is never kept")
}

func TestDropProcessor(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(dropProcessor{recorder}))

	_, kept := tp.Tracer("test").Start(context.Background(), "kept")
	kept.End()
	_, dropped := tp.Tracer("test").Start(context.Background(), "dropped", trace.WithAttributes(attribute.Bool(dropAttribute, true)))
	dropped.End()

	ended := recorder.Ended()
	require.Len(t, ended, 1)
	require.Equal(t, "kept", ended[0].Name())
}

func TestAddFlags_InvalidTransport(t *testing.T) {
	var opts FlagOptions
	fs := pflag.NewFlagSet("test", pflag.ContinueOnError)
	AddFlags(&opts, fs)
	require.Error(t, fs.Parse([]string{"--otel-tracing-transport=carrier-pigeon"}))
}

func TestTLSConfigFromCAPEM(t *testing.T) {
	t.Run("empty PEM trusts only the system roots", func(t *testing.T) {
		cfg, err := tlsConfigFromCAPEM("")
		require.NoError(t, err)
		require.NotNil(t, cfg.RootCAs)
	})

	t.Run("valid PEM is added to the root pool", func(t *testing.T) {
		caPEM := newTestCAPEM(t)
		withoutCA, err := tlsConfigFromCAPEM("")
		require.NoError(t, err)
		withCA, err := tlsConfigFromCAPEM(caPEM)
		require.NoError(t, err)
		require.False(t, withCA.RootCAs.Equal(withoutCA.RootCAs))
	})

	t.Run("invalid PEM is rejected", func(t *testing.T) {
		_, err := tlsConfigFromCAPEM("not a certificate")
		require.Error(t, err)
	})
}

func TestNewTracerProvider_InvalidCAPEMWithTLSEndpoint(t *testing.T) {
	_, err := NewTracerProvider(context.Background(), "svc", "https://collector:4318", "http", "not a certificate", DefaultSamplingPercent)
	require.Error(t, err)
}

func TestNewTracerProvider_CAPEMIgnoredForPlaintextEndpoint(t *testing.T) {
	prev := otel.GetTracerProvider()
	t.Cleanup(func() { otel.SetTracerProvider(prev) })

	tp, err := NewTracerProvider(context.Background(), "svc", "collector:4318", "http", "not a certificate", DefaultSamplingPercent)
	require.NoError(t, err)
	require.NotNil(t, tp)
	require.NoError(t, tp.Shutdown(context.Background()))
}

func TestSetupTracerProviderFromFlags(t *testing.T) {
	t.Run("disabled returns a callable no-op shutdown and keeps the global provider", func(t *testing.T) {
		prev := otel.GetTracerProvider()
		shutdown, err := SetupTracerProviderFromFlags(context.Background(), "svc", FlagOptions{})
		require.NoError(t, err)
		require.NotNil(t, shutdown)
		shutdown()
		require.Same(t, prev, otel.GetTracerProvider())
	})

	for _, transport := range []Transport{TransportHTTP, TransportGRPC} {
		t.Run("enabled with transport "+string(transport)+" registers the global provider", func(t *testing.T) {
			prev := otel.GetTracerProvider()
			t.Cleanup(func() { otel.SetTracerProvider(prev) })

			shutdown, err := SetupTracerProviderFromFlags(context.Background(), "svc", FlagOptions{
				Endpoint:  "collector:4317",
				Transport: transport,
			})
			require.NoError(t, err)
			require.NotSame(t, prev, otel.GetTracerProvider())
			shutdown()
		})
	}

	t.Run("enabled with a TLS endpoint and a valid CA", func(t *testing.T) {
		prev := otel.GetTracerProvider()
		t.Cleanup(func() { otel.SetTracerProvider(prev) })

		shutdown, err := SetupTracerProviderFromFlags(context.Background(), "svc", FlagOptions{
			Endpoint:  "https://collector:4320",
			Transport: TransportHTTP,
			CA:        newTestCAPEM(t),
		})
		require.NoError(t, err)
		shutdown()
	})

	t.Run("unsupported transport is an error", func(t *testing.T) {
		_, err := SetupTracerProviderFromFlags(context.Background(), "svc", FlagOptions{
			Endpoint:  "collector:4317",
			Transport: Transport("carrier-pigeon"),
		})
		require.Error(t, err)
	})
}
