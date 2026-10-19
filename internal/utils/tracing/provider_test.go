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
	}))
	require.Equal(t, FlagOptions{
		Endpoint:  "https://collector:4320",
		Transport: "grpc",
		CA:        "PEM-CONTENT",
	}, opts)
}

func TestAddFlags_DefaultsDisableTracing(t *testing.T) {
	var opts FlagOptions
	fs := pflag.NewFlagSet("test", pflag.ContinueOnError)
	AddFlags(&opts, fs)
	require.NoError(t, fs.Parse(nil))
	require.Equal(t, FlagOptions{Transport: DefaultTransport}, opts)
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
	_, err := NewTracerProvider(context.Background(), "svc", "https://collector:4318", "http", "not a certificate")
	require.Error(t, err)
}

func TestNewTracerProvider_CAPEMIgnoredForPlaintextEndpoint(t *testing.T) {
	prev := otel.GetTracerProvider()
	t.Cleanup(func() { otel.SetTracerProvider(prev) })

	tp, err := NewTracerProvider(context.Background(), "svc", "collector:4318", "http", "not a certificate")
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
