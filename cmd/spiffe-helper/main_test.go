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
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
	"github.com/spiffe/go-spiffe/v2/bundle/jwtbundle"
	"github.com/spiffe/go-spiffe/v2/spiffeid"
	"github.com/spiffe/go-spiffe/v2/svid/jwtsvid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testAudience = "dpf-dpu-agent"

type fakeJWTSource struct {
	svid   *jwtsvid.SVID
	bundle *jwtbundle.Bundle
	err    error
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func (s *fakeJWTSource) FetchJWTSVIDs(context.Context, jwtsvid.Params) ([]*jwtsvid.SVID, error) {
	if s.svid == nil {
		return nil, s.err
	}
	return []*jwtsvid.SVID{s.svid}, s.err
}

func (s *fakeJWTSource) GetJWTBundleForTrustDomain(spiffeid.TrustDomain) (*jwtbundle.Bundle, error) {
	return s.bundle, nil
}

func TestLoadConfig(t *testing.T) {
	const required = `agent_address = "/run/spire/agent.sock"
cert_dir = "/var/lib/dpf/dpuagent/spiffe"
jwt_svids = [{jwt_audience="dpf-dpu-agent",jwt_svid_file_name="token"}]
`
	tests := []struct {
		name         string
		optional     string
		wantMode     int
		wantEndpoint string
	}{
		{
			// 0640 rather than the 0600 default, so a dropped decode cannot pass as a parse.
			name: "optional settings parsed",
			optional: `jwt_svid_file_mode = 0640
token_exchange_endpoint = "https://identity-keys.example/v1/exchange"
`,
			wantMode:     0640,
			wantEndpoint: "https://identity-keys.example/v1/exchange",
		},
		{name: "optional settings defaulted", wantMode: 0600},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "helper.conf")
			require.NoError(t, os.WriteFile(path, []byte(required+test.optional), 0600))
			config, err := loadConfig(path)
			require.NoError(t, err)
			assert.Equal(t, "/var/lib/dpf/dpuagent/spiffe", config.CertDir)
			assert.Equal(t, "token", config.JWTSVIDs[0].JWTSVIDFilename)
			assert.Equal(t, test.wantMode, config.JWTSVIDFileMode)
			assert.Equal(t, test.wantEndpoint, config.TokenExchangeEndpoint)
		})
	}
}

func TestLoadConfigRejectsInvalidConfigs(t *testing.T) {
	tests := []struct {
		name     string
		contents string
		wantErr  string
	}{
		{
			name: "missing agent_address",
			contents: `cert_dir = "/spiffe"
jwt_svids = [{jwt_audience="a",jwt_svid_file_name="token"}]
`,
			wantErr: "agent_address is required",
		},
		{
			name: "missing cert_dir",
			contents: `agent_address = "/run/spire/agent.sock"
jwt_svids = [{jwt_audience="a",jwt_svid_file_name="token"}]
`,
			wantErr: "cert_dir is required",
		},
		{
			name: "missing jwt_svids",
			contents: `agent_address = "/run/spire/agent.sock"
cert_dir = "/spiffe"
`,
			wantErr: "jwt_svids is required",
		},
		{
			name: "missing jwt_audience",
			contents: `agent_address = "/run/spire/agent.sock"
cert_dir = "/spiffe"
jwt_svids = [{jwt_svid_file_name="token"}]
`,
			wantErr: "jwt_audience is required",
		},
		{
			name: "missing jwt_svid_file_name",
			contents: `agent_address = "/run/spire/agent.sock"
cert_dir = "/spiffe"
jwt_svids = [{jwt_audience="a"}]
`,
			wantErr: "jwt_svid_file_name is required",
		},
		{
			name: "duplicate jwt_svid_file_name",
			contents: `agent_address = "/run/spire/agent.sock"
cert_dir = "/spiffe"
jwt_svids = [{jwt_audience="a",jwt_svid_file_name="token"},{jwt_audience="b",jwt_svid_file_name="token"}]
`,
			wantErr: `duplicate jwt_svid_file_name "token"`,
		},
		{
			name: "negative file mode",
			contents: `agent_address = "/run/spire/agent.sock"
cert_dir = "/spiffe"
jwt_svids = [{jwt_audience="a",jwt_svid_file_name="token"}]
jwt_svid_file_mode = -1
`,
			wantErr: "jwt_svid_file_mode must be between 0 and 0777",
		},
		{
			name: "file mode above 0777",
			contents: `agent_address = "/run/spire/agent.sock"
cert_dir = "/spiffe"
jwt_svids = [{jwt_audience="a",jwt_svid_file_name="token"}]
jwt_svid_file_mode = 07777
`,
			wantErr: "jwt_svid_file_mode must be between 0 and 0777",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "helper.conf")
			require.NoError(t, os.WriteFile(path, []byte(test.contents), 0600))
			_, err := loadConfig(path)
			require.ErrorContains(t, err, test.wantErr)
		})
	}
}

func TestLoadConfigRejectsMissingFile(t *testing.T) {
	_, err := loadConfig(filepath.Join(t.TempDir(), "absent.conf"))
	require.ErrorContains(t, err, "read config")
}

func TestAgentAddress(t *testing.T) {
	tests := []struct {
		address string
		want    string
	}{
		{address: "/run/spire/agent.sock", want: "unix:///run/spire/agent.sock"},
		{address: "unix:///run/spire/agent.sock", want: "unix:///run/spire/agent.sock"},
		{address: "tcp://127.0.0.1:8081", want: "tcp://127.0.0.1:8081"},
	}
	for _, test := range tests {
		assert.Equal(t, test.want, agentAddress(test.address))
	}
}

func TestRefreshDelay(t *testing.T) {
	now := time.Now()
	tests := []struct {
		name      string
		remaining time.Duration
		want      time.Duration
	}{
		{name: "long lived", remaining: time.Hour, want: 30*time.Minute + time.Second},
		{name: "four seconds", remaining: 4 * time.Second, want: 3 * time.Second},
		{name: "two seconds refreshes before expiry", remaining: 2 * time.Second, want: time.Second},
		{name: "sub-second token is floored rather than spun", remaining: 500 * time.Millisecond, want: retryMin},
		{name: "already expired", remaining: -time.Hour, want: retryMin},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			delay := refreshDelay(now, now.Add(test.remaining))
			assert.Equal(t, test.want, delay)
			assert.GreaterOrEqual(t, delay, retryMin, "the refresh loop must never spin")
			// Below the floor the token expires before the refresh, by design.
			if test.remaining > retryMin {
				assert.Less(t, delay, test.remaining, "refresh must happen before the token expires")
			}
		})
	}
}

func TestNextRetry(t *testing.T) {
	tests := []struct {
		name          string
		current       time.Duration
		want          time.Duration
		wantSaturated bool
	}{
		{name: "doubles from the floor", current: retryMin, want: 2 * retryMin},
		{name: "clamps at the ceiling", current: retryMax/2 + time.Second, want: retryMax},
		{name: "reports saturation only at the ceiling", current: retryMax, want: retryMax, wantSaturated: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			next, saturated := nextRetry(test.current)
			assert.Equal(t, test.want, next)
			assert.Equal(t, test.wantSaturated, saturated)
			assert.LessOrEqual(t, next, retryMax, "backoff must never exceed the ceiling")
		})
	}
}

func TestRefreshRejectsEmptySVIDList(t *testing.T) {
	f := fetcher{
		config: helperConfig{
			CertDir:         t.TempDir(),
			JWTSVIDs:        []jwtConfig{{JWTAudience: testAudience, JWTSVIDFilename: "token"}},
			JWTSVIDFileMode: 0600,
		},
		source:     &fakeJWTSource{},
		httpClient: http.DefaultClient,
	}
	_, err := f.refreshToken(context.Background(), f.config.JWTSVIDs[0])
	require.ErrorContains(t, err, "no SVIDs returned")
}

func TestRefreshWritesDirectAndExchangedTokens(t *testing.T) {
	source, rawToken := newJWTSource(t)

	// The SVID expires in an hour and the exchanged token in a minute, so an expiry taken from the
	// wrong one of the two is far outside the tolerance rather than indistinguishable.
	tests := []struct {
		name       string
		response   string
		want       string
		wantExpiry func() time.Time
	}{
		{
			name:       "direct",
			want:       rawToken,
			wantExpiry: func() time.Time { return source.svid.Expiry },
		},
		{
			name:       "exchanged",
			response:   `{"access_token":"exchanged-token","token_type":"Bearer","expires_in":60}`,
			want:       "exchanged-token",
			wantExpiry: func() time.Time { return time.Now().Add(time.Minute) },
		},
		{
			// RFC 8693 lists expires_in as RECOMMENDED, not required. Without a fallback the zero
			// time would schedule an immediate refresh and spin.
			name:       "exchanged without expires_in",
			response:   `{"access_token":"exchanged-token","token_type":"Bearer"}`,
			want:       "exchanged-token",
			wantExpiry: func() time.Time { return source.svid.Expiry },
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			endpoint, client := "", http.DefaultClient
			if test.response != "" {
				server := newExchangeServer(t, test.response)
				endpoint, client = server.URL, server.Client()
			}
			dir := t.TempDir()
			f := fetcher{
				config: helperConfig{
					CertDir:  dir,
					JWTSVIDs: []jwtConfig{{JWTAudience: testAudience, JWTSVIDFilename: "token"}},
					// 0640 rather than 0600, which is what the atomic write's temporary file is
					// created with anyway and so would pass without the mode being applied.
					JWTSVIDFileMode:       0640,
					TokenExchangeEndpoint: endpoint,
				},
				source:     source,
				httpClient: client,
			}
			expiry, err := f.refreshToken(context.Background(), f.config.JWTSVIDs[0])
			require.NoError(t, err)
			assert.WithinDuration(t, test.wantExpiry(), expiry, 5*time.Second)
			path := filepath.Join(dir, "token")
			got, err := os.ReadFile(path)
			require.NoError(t, err)
			assert.Equal(t, test.want, string(got))
			info, err := os.Stat(path)
			require.NoError(t, err)
			assert.Equal(t, os.FileMode(0640), info.Mode().Perm())
		})
	}
}

// A truncate-in-place write would leave a stale mode and expose a partial token to readers.
func TestRefreshReplacesExistingTokenAndTightensMode(t *testing.T) {
	source, rawToken := newJWTSource(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "token")
	require.NoError(t, os.WriteFile(path, []byte("stale-and-world-readable"), 0644))

	f := fetcher{
		config: helperConfig{
			CertDir:         dir,
			JWTSVIDs:        []jwtConfig{{JWTAudience: testAudience, JWTSVIDFilename: "token"}},
			JWTSVIDFileMode: 0600,
		},
		source:     source,
		httpClient: http.DefaultClient,
	}

	_, err := f.refreshToken(context.Background(), f.config.JWTSVIDs[0])
	require.NoError(t, err)
	got, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, rawToken, string(got))
	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0600), info.Mode().Perm())

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	assert.Len(t, entries, 1, "temporary files must not be left behind")
}

func TestRefreshKeepsPreviousTokenWhenExchangeFails(t *testing.T) {
	source, _ := newJWTSource(t)
	server := newExchangeServer(t, "")

	dir := t.TempDir()
	path := filepath.Join(dir, "token")
	require.NoError(t, os.WriteFile(path, []byte("previous-token"), 0600))
	f := fetcher{
		config: helperConfig{
			CertDir:               dir,
			JWTSVIDs:              []jwtConfig{{JWTAudience: testAudience, JWTSVIDFilename: "token"}},
			JWTSVIDFileMode:       0600,
			TokenExchangeEndpoint: server.URL,
		},
		source:     source,
		httpClient: server.Client(),
	}

	_, err := f.refreshToken(context.Background(), f.config.JWTSVIDs[0])
	require.ErrorContains(t, err, "exchange JWT-SVID")
	got, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "previous-token", string(got), "a failed exchange must not clobber a usable token")
}

func TestRunWritesEveryConfiguredToken(t *testing.T) {
	source, rawToken := newJWTSource(t)
	dir := t.TempDir()
	config := helperConfig{
		CertDir: dir,
		JWTSVIDs: []jwtConfig{
			{JWTAudience: testAudience, JWTSVIDFilename: "token"},
			{JWTAudience: testAudience, JWTSVIDFilename: "second-token"},
		},
		JWTSVIDFileMode: 0600,
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		run(ctx, config, source, http.DefaultClient)
	}()

	for _, name := range []string{"token", "second-token"} {
		require.Eventually(t, func() bool {
			data, err := os.ReadFile(filepath.Join(dir, name))
			return err == nil && string(data) == rawToken
		}, 5*time.Second, 10*time.Millisecond, "%q was never written", name)
	}

	cancel()
	<-done
}

func TestRunKeepsExistingTokenWhenStopped(t *testing.T) {
	source, _ := newJWTSource(t)
	dir := t.TempDir()
	tokenPath := filepath.Join(dir, "token")
	require.NoError(t, os.WriteFile(tokenPath, []byte("existing-token"), 0600))
	config := helperConfig{
		CertDir:         dir,
		JWTSVIDs:        []jwtConfig{{JWTAudience: testAudience, JWTSVIDFilename: "token"}},
		JWTSVIDFileMode: 0600,
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	source.err = ctx.Err()

	run(ctx, config, source, http.DefaultClient)
	got, err := os.ReadFile(tokenPath)
	require.NoError(t, err)
	assert.Equal(t, "existing-token", string(got))
}

func TestExchangeUsesTokenExchangeGrant(t *testing.T) {
	t.Parallel()

	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Header.Get("Authorization") != "" {
			t.Errorf("unexpected client authentication: %q", r.Header.Get("Authorization"))
		}
		if err := r.ParseForm(); err != nil {
			t.Error(err)
		}
		if r.Form.Get("grant_type") != grantType ||
			r.Form.Get("subject_token") != "local-svid" ||
			r.Form.Get("subject_token_type") != tokenType {
			t.Errorf("unexpected token exchange form: %v", r.Form)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": {"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"access_token":"exchanged","token_type":"Bearer","expires_in":60}`)),
		}, nil
	})}

	token, err := exchange(t.Context(), client, "https://exchange.example/v1/exchange", "local-svid")
	if err != nil {
		t.Fatal(err)
	}
	if token.AccessToken != "exchanged" || !token.Expiry.After(time.Now()) {
		t.Fatalf("token = %#v", token)
	}
}

func newJWTSource(t *testing.T) (*fakeJWTSource, string) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	const keyID = "test-key"
	signer, err := jose.NewSigner(
		jose.SigningKey{Algorithm: jose.RS256, Key: key},
		(&jose.SignerOptions{}).WithHeader("kid", keyID),
	)
	require.NoError(t, err)
	expiry := time.Now().Add(time.Hour)
	raw, err := jwt.Signed(signer).Claims(jwt.Claims{
		Subject:  "spiffe://example.org/dpu-agent",
		Audience: jwt.Audience{testAudience},
		Expiry:   jwt.NewNumericDate(expiry),
	}).Serialize()
	require.NoError(t, err)
	svid, err := jwtsvid.ParseInsecure(raw, []string{testAudience})
	require.NoError(t, err)
	trustDomain := spiffeid.RequireTrustDomainFromString("example.org")
	return &fakeJWTSource{
		svid:   svid,
		bundle: jwtbundle.FromJWTAuthorities(trustDomain, map[string]crypto.PublicKey{keyID: &key.PublicKey}),
	}, raw
}

// newExchangeServer replies with the given token exchange response body, or fails the exchange when
// the body is empty.
func newExchangeServer(t *testing.T, response string) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if response == "" {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if _, err := fmt.Fprint(w, response); err != nil {
			t.Error(err)
		}
	}))
	t.Cleanup(server.Close)
	return server
}
