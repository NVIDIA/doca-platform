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

package main

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeKeyPair writes a self-signed ECDSA cert/key into dir as tls.crt/tls.key,
// matching what cert-manager produces and what reloadNginx validates.
func writeKeyPair(t *testing.T, dir string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "bfb-registry"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	require.NoError(t, err)

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	require.NoError(t, os.WriteFile(filepath.Join(dir, certFileName), certPEM, 0o600))

	keyDER, err := x509.MarshalECPrivateKey(key)
	require.NoError(t, err)
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	require.NoError(t, os.WriteFile(filepath.Join(dir, keyFileName), keyPEM, 0o600))
}

// writeFakeNginx writes an executable shell script that records its args to
// recordPath and exits with the given code, standing in for the nginx binary.
func writeFakeNginx(t *testing.T, dir, recordPath string, exitCode int) string {
	t.Helper()
	binPath := filepath.Join(dir, "fake-nginx")
	script := "#!/bin/sh\n" +
		"echo \"$@\" > " + recordPath + "\n" +
		"echo fake-nginx-output\n" +
		"exit " + strconv.Itoa(exitCode) + "\n"
	require.NoError(t, os.WriteFile(binPath, []byte(script), 0o755))
	return binPath
}

func TestReloadNginx_InvalidKeypair(t *testing.T) {
	dir := t.TempDir()
	cfg := &config{
		certDir:   dir, // no tls.crt / tls.key present
		nginxBin:  "/bin/true",
		nginxConf: "/nginx/nginx.conf",
	}
	err := reloadNginx(cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "validate keypair")
}

func TestReloadNginx_Success(t *testing.T) {
	certDir := t.TempDir()
	binDir := t.TempDir()
	writeKeyPair(t, certDir)

	record := filepath.Join(binDir, "args.txt")
	nginxBin := writeFakeNginx(t, binDir, record, 0)

	cfg := &config{
		certDir:   certDir,
		nginxBin:  nginxBin,
		nginxConf: "/nginx/nginx.conf",
	}
	require.NoError(t, reloadNginx(cfg))

	// The reload must invoke nginx with the graceful-reload arguments.
	got, err := os.ReadFile(record)
	require.NoError(t, err)
	assert.Contains(t, string(got), "-c /nginx/nginx.conf -s reload")
}

func TestReloadNginx_NginxFails(t *testing.T) {
	certDir := t.TempDir()
	binDir := t.TempDir()
	writeKeyPair(t, certDir)

	record := filepath.Join(binDir, "args.txt")
	nginxBin := writeFakeNginx(t, binDir, record, 1)

	cfg := &config{
		certDir:   certDir,
		nginxBin:  nginxBin,
		nginxConf: "/nginx/nginx.conf",
	}
	err := reloadNginx(cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "nginx -s reload")
	// CombinedOutput is surfaced in the error to aid debugging.
	assert.Contains(t, err.Error(), "fake-nginx-output")
}

// TestRun_DebouncesEventBurst verifies that a burst of filesystem events
// (as produced by kubelet's atomic ..data symlink swap) is coalesced into a
// single onChange call, and that a later, separate change triggers another.
func TestRun_DebouncesEventBurst(t *testing.T) {
	dir := t.TempDir()

	var count atomic.Int64
	var wg sync.WaitGroup
	cfg := &config{
		certDir:  dir,
		debounce: 200 * time.Millisecond,
	}

	ctx, cancel := context.WithCancel(context.Background())
	wg.Add(1)
	go func() {
		defer wg.Done()
		assert.NoError(t, run(ctx, cfg, func() { count.Add(1) }))
	}()

	// Give run() time to register the directory watch before generating events.
	time.Sleep(150 * time.Millisecond)

	// Burst of events, all within the debounce window.
	for i := 0; i < 5; i++ {
		require.NoError(t, os.WriteFile(filepath.Join(dir, "f"+strconv.Itoa(i)), []byte("x"), 0o600))
		time.Sleep(10 * time.Millisecond)
	}

	// Wait past the debounce window: the burst should collapse to one call.
	require.Eventually(t, func() bool { return count.Load() == 1 }, 2*time.Second, 20*time.Millisecond)

	// A second, separate change must trigger exactly one more call.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "tls.crt"), []byte("y"), 0o600))
	require.Eventually(t, func() bool { return count.Load() == 2 }, 2*time.Second, 20*time.Millisecond)

	cancel()
	wg.Wait()
	assert.Equal(t, int64(2), count.Load())
}
