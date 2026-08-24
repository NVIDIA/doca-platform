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
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/nvidia/doca-platform/internal/provisioning/utils/filesystem"

	"github.com/hashicorp/hcl"
	"github.com/spiffe/go-spiffe/v2/bundle/jwtbundle"
	"github.com/spiffe/go-spiffe/v2/svid/jwtsvid"
	"github.com/spiffe/go-spiffe/v2/workloadapi"
	"golang.org/x/oauth2"
)

const (
	defaultConfigPath = "/etc/spiffe-helper/helper.conf"
	retryMin          = time.Second
	retryMax          = time.Minute
	grantType         = "urn:ietf:params:oauth:grant-type:token-exchange"
	tokenType         = "urn:ietf:params:oauth:token-type:jwt"
)

type helperConfig struct {
	AgentAddress          string      `hcl:"agent_address"`
	CertDir               string      `hcl:"cert_dir"`
	JWTSVIDs              []jwtConfig `hcl:"jwt_svids"`
	JWTSVIDFileMode       int         `hcl:"jwt_svid_file_mode"`
	TokenExchangeEndpoint string      `hcl:"token_exchange_endpoint"`
}

type jwtConfig struct {
	JWTAudience       string   `hcl:"jwt_audience"`
	JWTExtraAudiences []string `hcl:"jwt_extra_audiences"`
	JWTSVIDFilename   string   `hcl:"jwt_svid_file_name"`
}

type fetcher struct {
	config     helperConfig
	source     jwtSVIDSource
	httpClient *http.Client
}

type jwtSVIDSource interface {
	FetchJWTSVIDs(context.Context, jwtsvid.Params) ([]*jwtsvid.SVID, error)
	jwtbundle.Source
}

func loadConfig(path string) (helperConfig, error) {
	var config helperConfig
	data, err := os.ReadFile(path)
	if err != nil {
		return helperConfig{}, fmt.Errorf("read config: %w", err)
	}
	if err := hcl.Decode(&config, string(data)); err != nil {
		return helperConfig{}, fmt.Errorf("parse config: %w", err)
	}
	if config.AgentAddress == "" {
		return helperConfig{}, errors.New("agent_address is required")
	}
	if config.CertDir == "" {
		return helperConfig{}, errors.New("cert_dir is required")
	}
	if len(config.JWTSVIDs) == 0 {
		return helperConfig{}, errors.New("jwt_svids is required")
	}
	filenames := make(map[string]struct{}, len(config.JWTSVIDs))
	for _, jwt := range config.JWTSVIDs {
		if jwt.JWTAudience == "" {
			return helperConfig{}, errors.New("jwt_audience is required in jwt_svids")
		}
		if jwt.JWTSVIDFilename == "" {
			return helperConfig{}, errors.New("jwt_svid_file_name is required in jwt_svids")
		}
		// Each entry gets its own refresh loop, so a shared file name is two goroutines racing.
		if _, duplicate := filenames[jwt.JWTSVIDFilename]; duplicate {
			return helperConfig{}, fmt.Errorf("duplicate jwt_svid_file_name %q in jwt_svids", jwt.JWTSVIDFilename)
		}
		filenames[jwt.JWTSVIDFilename] = struct{}{}
	}
	if config.JWTSVIDFileMode < 0 || config.JWTSVIDFileMode > 0o777 {
		return helperConfig{}, errors.New("jwt_svid_file_mode must be between 0 and 0777")
	}
	if config.JWTSVIDFileMode == 0 {
		config.JWTSVIDFileMode = 0600
	}
	return config, nil
}

func (f *fetcher) refreshToken(ctx context.Context, jwt jwtConfig) (time.Time, error) {
	svids, err := f.source.FetchJWTSVIDs(ctx, jwtsvid.Params{Audience: jwt.JWTAudience, ExtraAudiences: jwt.JWTExtraAudiences})
	if err != nil {
		return time.Time{}, fmt.Errorf("fetch JWT-SVID: %w", err)
	}
	if len(svids) == 0 {
		return time.Time{}, errors.New("fetch JWT-SVID: no SVIDs returned")
	}
	for _, svid := range svids {
		if _, err := jwtsvid.ParseAndValidate(svid.Marshal(), f.source, []string{jwt.JWTAudience}); err != nil {
			return time.Time{}, fmt.Errorf("validate JWT-SVID: %w", err)
		}
	}

	svid := svids[0]
	token := svid.Marshal()
	expiry := svid.Expiry
	if f.config.TokenExchangeEndpoint != "" {
		exchanged, err := exchange(ctx, f.httpClient, f.config.TokenExchangeEndpoint, token)
		if err != nil {
			return time.Time{}, fmt.Errorf("exchange JWT-SVID: %w", err)
		}
		token = exchanged.AccessToken
		// RFC 8693 makes expires_in RECOMMENDED rather than required. Keep the subject SVID
		// expiry when the endpoint omits it, otherwise the refresh loop never writes a token.
		if !exchanged.Expiry.IsZero() {
			expiry = exchanged.Expiry
		}
	}
	if !expiry.After(time.Now()) {
		return time.Time{}, errors.New("token has no future expiry")
	}

	// The DPU Agent reads this path as client-go's BearerTokenFile, so a truncate-in-place write
	// would let it authenticate with a partial token.
	if err := filesystem.AtomicWrite(filepath.Join(f.config.CertDir, jwt.JWTSVIDFilename), []byte(token), os.FileMode(f.config.JWTSVIDFileMode)); err != nil {
		return time.Time{}, fmt.Errorf("write token: %w", err)
	}
	return expiry, nil
}

func exchange(ctx context.Context, client *http.Client, endpoint, subjectToken string) (*oauth2.Token, error) {
	config := &oauth2.Config{Endpoint: oauth2.Endpoint{
		TokenURL:  endpoint,
		AuthStyle: oauth2.AuthStyleInParams,
	}}
	ctx = context.WithValue(ctx, oauth2.HTTPClient, client)
	// Use Config.Exchange for RFC 8693 while retaining its HTTP and response handling. It also
	// emits an empty code= parameter that token exchange endpoints are expected to ignore.
	return config.Exchange(ctx, "",
		oauth2.SetAuthURLParam("grant_type", grantType),
		oauth2.SetAuthURLParam("subject_token", subjectToken),
		oauth2.SetAuthURLParam("subject_token_type", tokenType),
	)
}

// refreshDelay waits out half of the token's remaining life. The floor matters more than the
// halving: without it a token issued with a second to live would drive an unbounded fetch loop
// against the SPIRE Agent and the exchange endpoint. Such a token is refreshed late instead.
func refreshDelay(now, expiry time.Time) time.Duration {
	remaining := expiry.Sub(now)
	delay := remaining/2 + time.Second
	// The one-second nudge must not push the refresh past the expiry of a short-lived token.
	if delay >= remaining {
		delay = remaining / 2
	}
	return max(delay, retryMin)
}

// agentAddress accepts the bare socket path that DPF cloud-init renders as well as an address that
// already carries a scheme, matching what the upstream helper tolerates.
func agentAddress(address string) string {
	if strings.Contains(address, "://") {
		return address
	}
	return "unix://" + address
}

// nextRetry doubles the backoff up to retryMax and reports whether it had already saturated there,
// which is what distinguishes a transient failure from one an operator needs to hear about.
func nextRetry(current time.Duration) (next time.Duration, saturated bool) {
	return min(current*2, retryMax), current == retryMax
}

func (f *fetcher) updateToken(ctx context.Context, jwt jwtConfig) {
	path := filepath.Join(f.config.CertDir, jwt.JWTSVIDFilename)
	retry := retryMin
	var lastSuccess time.Time
	for {
		expiry, err := f.refreshToken(ctx, jwt)
		var delay time.Duration
		if err == nil {
			retry = retryMin
			lastSuccess = time.Now()
			delay = refreshDelay(lastSuccess, expiry)
			slog.Info("Token updated", "path", path, "expiresAt", expiry)
		} else {
			if ctx.Err() != nil {
				return
			}
			delay = retry
			var saturated bool
			retry, saturated = nextRetry(retry)
			// A stuck refresh never exits, so systemd keeps reporting the unit as running while
			// the token goes stale and the DPU Agent quietly loses API access. Once the backoff
			// has saturated, say so at error level: it is the only signal an operator gets.
			if saturated {
				lastSuccessText := "never"
				if !lastSuccess.IsZero() {
					lastSuccessText = lastSuccess.Format(time.RFC3339)
				}
				slog.Error("Token refresh still failing", "path", path, "error", err,
					"retryIn", delay, "lastSuccess", lastSuccessText)
			} else {
				slog.Warn("Token refresh failed", "path", path, "error", err, "retryIn", delay)
			}
		}

		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

func run(ctx context.Context, config helperConfig, source jwtSVIDSource, httpClient *http.Client) {
	fetcher := fetcher{config: config, source: source, httpClient: httpClient}
	var wg sync.WaitGroup
	for _, jwt := range config.JWTSVIDs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			fetcher.updateToken(ctx, jwt)
		}()
	}
	wg.Wait()
}

func main() {
	configPath := flag.String("config", defaultConfigPath, "Path to the helper configuration")
	flag.Parse()
	config, err := loadConfig(*configPath)
	if err != nil {
		slog.Error("Invalid configuration", "error", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	source, err := workloadapi.NewJWTSource(ctx, workloadapi.WithClientOptions(workloadapi.WithAddr(agentAddress(config.AgentAddress))))
	if err != nil {
		slog.Error("Create Workload API client", "error", err)
		os.Exit(1)
	}
	defer func() {
		if err := source.Close(); err != nil {
			slog.Warn("Close Workload API client", "error", err)
		}
	}()
	run(ctx, config, source, &http.Client{Timeout: 30 * time.Second})
}
