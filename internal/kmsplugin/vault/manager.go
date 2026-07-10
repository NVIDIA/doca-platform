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

package vault

import (
	"context"
	"fmt"
	"time"

	"github.com/nvidia/doca-platform/internal/kmsplugin/config"

	"github.com/go-logr/logr"
	vaultapi "github.com/hashicorp/vault/api"
)

// Token manager tuning defaults.
const (
	// defaultCheckInterval is how often the loop verifies the token. This is
	// the single clock driving the whole token lifecycle
	defaultCheckInterval = config.DefaultTokenCheckInterval
	// defaultRenewFraction is the fraction of a token's creation TTL that
	// must have elapsed before the manager proactively renews it. At 2/3,
	// renewal is attempted with roughly a third of the token's original
	// lifetime still left, leaving headroom for a few check cycles to retry
	// before it would actually expire. Scaling with the token's own creation
	// TTL, rather than a fixed duration, means a short-lived token is
	// renewed near its own end instead of either never (a fixed threshold
	// longer than its TTL) or on every single check (a fixed threshold much
	// shorter than its TTL). The renew-eligible window (from the 2/3 point to
	// expiry) is creationTTL/3 wide, so renewal is only guaranteed to fire
	// before expiry when that window is at least one checkInterval wide, i.e.
	// creationTTL >= 3*defaultCheckInterval. A token whose creation TTL is
	// shorter than that may still expire between checks, since a tick can
	// skip over the whole window between its 2/3 point and its actual expiry.
	defaultRenewFraction = 2.0 / 3.0
	// defaultMinTTL is the fallback remaining-TTL threshold used only when
	// creation_ttl cannot be determined (an unexpected or malformed lookup
	// response; Vault normally always includes it), so the token still gets
	// renewed before it actually expires instead of the manager doing
	// nothing until then.
	defaultMinTTL = 5 * time.Minute
	// defaultCheckTimeout bounds a single check cycle: a lookup, plus a
	// possible renewal, post-renewal lookup and authentication attempt against
	// the configured auth method. It is a shared budget across up to four
	// sequential Vault calls, not a per-call timeout, so a hung connection
	// cannot wedge the loop and silently stop all future checks. Keep it below
	// defaultCheckInterval so timed-out checks do not immediately retry on a
	// queued tick, while still leaving room for slower delegated auth backends.
	defaultCheckTimeout = config.DefaultLoginTimeout
)

// TokenManager owns the Vault token lifecycle. A single background loop
// (Run) periodically confirms the current token is still valid, renews it
// once its TTL runs low, and falls back to authentication when lookup rejects
// the token, renewal returns an auth error, or a non-renewable token is getting
// close to expiry. It is the only code path that ever refreshes credentials:
// the request path (TransitService) never triggers authentication, so a token
// that is valid but lacks the required policy is never "fixed" by minting a
// new one, and there is nothing to collapse or pace against a burst of
// concurrent requests, since only this single goroutine ever calls the
// authenticator.
type TokenManager struct {
	vaultClient VaultTokenClient
	auth        Authenticator
	log         logr.Logger

	checkInterval time.Duration
	renewFraction float64
	minTTL        time.Duration
	checkTimeout  time.Duration

	// hasToken is only ever read or written from the goroutine running Run,
	// so it needs no lock.
	hasToken bool
}

// TokenManagerOption configures optional NewTokenManager behavior.
type TokenManagerOption func(*TokenManager)

// WithTokenCheckInterval overrides how often the manager checks the current token.
func WithTokenCheckInterval(interval time.Duration) TokenManagerOption {
	return func(m *TokenManager) {
		m.checkInterval = interval
	}
}

// WithLoginTimeout overrides the timeout for one token check cycle, including authentication.
func WithLoginTimeout(timeout time.Duration) TokenManagerOption {
	return func(m *TokenManager) {
		m.checkTimeout = timeout
	}
}

// NewTokenManager builds a TokenManager with default tuning.
func NewTokenManager(vaultClient VaultTokenClient, auth Authenticator, log logr.Logger, opts ...TokenManagerOption) *TokenManager {
	m := &TokenManager{
		vaultClient:   vaultClient,
		auth:          auth,
		log:           log,
		checkInterval: defaultCheckInterval,
		renewFraction: defaultRenewFraction,
		minTTL:        defaultMinTTL,
		checkTimeout:  defaultCheckTimeout,
	}
	for _, opt := range opts {
		opt(m)
	}
	return m
}

// Run is the single renew/reauth handler and the exported entry point for the
// manager loop: it performs an immediate check - which authenticates, since no
// token is set yet - and then repeats the check every checkInterval until ctx
// is canceled.
// Meant to run in a dedicated goroutine. A failed check is never fatal: the
// next tick tries again, so a failed initial authentication just delays the
// plugin becoming healthy rather than stopping it from starting.
func (m *TokenManager) Run(ctx context.Context) {
	m.check(ctx)

	ticker := time.NewTicker(m.checkInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.check(ctx)
		}
	}
}

// check performs one lookup-renew-or-reauthenticate cycle, bounded by
// checkTimeout so a hung Vault call cannot stall this and every future
// cycle. Any failure is logged and left for the next tick: a connectivity
// error is not fixed by re-authenticating, and a token that is valid but
// under-permissioned is left alone, since fresh authentication would carry the
// exact same policy and gain nothing.
func (m *TokenManager) check(ctx context.Context) {
	ctx, cancel := context.WithTimeout(ctx, m.checkTimeout)
	defer cancel()

	if !m.hasToken {
		if err := m.authenticate(ctx); err != nil {
			m.log.Error(err, "authentication failed, will retry on next check")
			return
		}
		// Inspect the fresh token so renewalDecision can warn when its
		// lifetime is too short for reliable renewal.
		if secret, ok := m.lookupToken(ctx); ok {
			_, _ = m.renewalDecision(secret)
		}
		return
	}

	secret, ok := m.lookupToken(ctx)
	if !ok {
		return
	}

	m.renewIfDue(ctx, secret)
}

// lookupToken looks up the current token and reports via ok whether a usable
// secret was returned. A lookup auth error is handled here rather than left
// to the caller: a rejected lookup already proves the current token needs
// replacing, so it re-authenticates immediately instead of waiting for the
// next cycle.
func (m *TokenManager) lookupToken(ctx context.Context) (secret *vaultapi.Secret, ok bool) {
	secret, err := m.vaultClient.LookupSelf(ctx)
	switch {
	case err != nil && isAuthError(err):
		m.log.Info("token lookup was rejected, re-authenticating", "err", err.Error())
		if aerr := m.reauthenticate(ctx); aerr != nil {
			// Lookup 401/403 proves the current token is unusable. If it
			// cannot be replaced now, drop it so the next cycle goes straight
			// to authentication instead of looking it up again.
			m.hasToken = false
			m.log.Error(aerr, "re-authentication failed, will retry on next check")
		}
		return nil, false
	case err != nil:
		m.log.Error(err, "token lookup failed, will retry on next check")
		return nil, false
	}
	if secret == nil {
		m.log.Error(fmt.Errorf("empty token lookup response"), "token lookup failed, will retry on next check")
		return nil, false
	}
	return secret, true
}

// renewIfDue parses the current token's TTL and, once decideRenewal says
// enough of its lifetime has elapsed, renews it or, if it cannot be renewed,
// replaces it via the configured auth method.
func (m *TokenManager) renewIfDue(ctx context.Context, secret *vaultapi.Secret) {
	decision, ok := m.renewalDecision(secret)
	if !ok {
		return
	}
	if !decision.renewNow {
		m.log.V(1).Info("token has enough TTL, skipping renewal", "ttl", decision.ttl)
		return
	}

	renewable, err := secret.TokenIsRenewable()
	if err != nil {
		m.log.Error(err, "parsing token renewable flag failed, will retry on next check")
		return
	}
	if !renewable {
		m.log.Info("token is not renewable, obtaining a replacement via the configured auth method")
		if aerr := m.reauthenticate(ctx); aerr != nil {
			m.log.Error(aerr, "re-authentication failed, will retry on next check")
		}
		return
	}

	if m.renewSelf(ctx) {
		m.reauthenticateIfRenewalStillDue(ctx)
	}
}

// renewSelf renews the current token, falling back to re-authentication when
// renewal itself fails with an auth error. It reports whether Vault accepted
// the renewal without error.
func (m *TokenManager) renewSelf(ctx context.Context) bool {
	if err := m.vaultClient.RenewSelf(ctx); err != nil {
		if isAuthError(err) {
			m.log.Info("token renewal failed with an auth error, re-authenticating",
				"err", err.Error())
			if aerr := m.reauthenticate(ctx); aerr != nil {
				// Unlike the lookup path, a renew 401/403 does not prove the
				// token is dead; it may only lack renewal permission. Keep it
				// and let the next lookup discard it if Vault rejects it there.
				m.log.Error(aerr, "re-authentication failed, will retry on next check")
			}
			return false
		}
		m.log.Error(err, "token renewal failed, will retry on next check")
		return false
	}
	m.log.Info("token renewed successfully")
	return true
}

// reauthenticateIfRenewalStillDue checks whether Vault capped a successful
// renewal so low that the token is still in the renew-eligible window. That
// happens near a token's max TTL; replacing the token immediately avoids
// waiting a full check interval with a token that may expire first.
func (m *TokenManager) reauthenticateIfRenewalStillDue(ctx context.Context) {
	secret, ok := m.lookupToken(ctx)
	if !ok {
		return
	}

	decision, ok := m.renewalDecision(secret)
	if !ok || !decision.renewNow {
		return
	}

	m.log.Info("token renewal did not restore enough TTL, obtaining a replacement via the configured auth method")
	if aerr := m.reauthenticate(ctx); aerr != nil {
		m.log.Error(aerr, "re-authentication failed, will retry on next check")
	}
}

func (m *TokenManager) renewalDecision(secret *vaultapi.Secret) (renewalDecision, bool) {
	ttl, err := secret.TokenTTL()
	if err != nil {
		m.log.Error(err, "parsing token TTL failed, will retry on next check")
		return renewalDecision{}, false
	}
	if ttl <= 0 {
		// Non-expiring: nothing to do this cycle.
		return renewalDecision{ttl: ttl}, true
	}

	creationTTL, ctErr := tokenCreationTTL(secret)
	if ctErr != nil {
		// creation_ttl is normally always present, so this only happens for
		// an unexpected or malformed response: fall back to a fixed floor
		// rather than skipping renewal entirely until the token expires.
		m.log.Error(ctErr, "parsing token creation TTL failed, falling back to a fixed renewal threshold")
	}

	decision := decideRenewal(ttl, creationTTL, ctErr, m.renewFraction, m.minTTL, m.checkInterval)
	if decision.shortCreationTTL {
		m.log.Info("token creation TTL is too short to guarantee a check tick lands inside its renew-eligible window and it may expire before it can be renewed, consider increasing the auth role's token TTL",
			"creationTTL", creationTTL, "checkInterval", m.checkInterval)
	}
	return decision, true
}

// authenticate runs the configured Authenticator and records when it succeeds.
// The caller decides whether a failed authentication means the current token
// should be discarded.
func (m *TokenManager) authenticate(ctx context.Context) error {
	if err := m.auth.Authenticate(ctx, m.vaultClient); err != nil {
		return err
	}
	m.hasToken = true
	return nil
}

func (m *TokenManager) reauthenticate(ctx context.Context) error {
	if err := m.authenticate(ctx); err != nil {
		return err
	}
	m.log.Info("re-authentication succeeded")
	return nil
}

// renewalDecision is the outcome of evaluating a token's TTL against the
// renewal policy in decideRenewal.
type renewalDecision struct {
	// ttl is the remaining token TTL.
	ttl time.Duration
	// renewNow reports whether the token should be renewed (or, if
	// non-renewable, replaced) this cycle.
	renewNow bool
	// shortCreationTTL reports whether creationTTL is known and too short to
	// guarantee a check tick lands inside the renew-eligible window before
	// the token expires (see defaultRenewFraction). It is always false when
	// creationTTL could not be determined.
	shortCreationTTL bool
}

// decideRenewal applies the fraction-of-lifetime-elapsed renewal policy: once
// 1-ttl/creationTTL reaches renewFraction, the token is due for renewal (see
// defaultRenewFraction for the rationale). When creationTTL could not be
// determined (creationTTLErr != nil), it falls back to the fixed minTTL floor
// instead, so the token still gets renewed before it actually expires.
func decideRenewal(ttl, creationTTL time.Duration, creationTTLErr error, renewFraction float64, minTTL, checkInterval time.Duration) renewalDecision {
	if creationTTLErr != nil {
		return renewalDecision{ttl: ttl, renewNow: ttl <= minTTL}
	}
	return renewalDecision{
		ttl:              ttl,
		renewNow:         1-float64(ttl)/float64(creationTTL) >= renewFraction,
		shortCreationTTL: creationTTL <= 3*checkInterval,
	}
}

// tokenCreationTTL extracts creation_ttl from a token lookup-self response:
// the TTL the token was created or last renewed with, used as the baseline
// for the fraction-of-lifetime-elapsed check above. Unlike the remaining
// TTL, the Vault Go SDK has no TokenTTL-style helper for this field, so it
// is parsed by requirePositiveInt64, the same helper parseKeyVersion
// (transit.go) uses for the Transit key_version.
func tokenCreationTTL(secret *vaultapi.Secret) (time.Duration, error) {
	if secret == nil || secret.Data == nil {
		return 0, fmt.Errorf("secret has no data")
	}
	seconds, err := requirePositiveInt64(secret.Data, "creation_ttl", "secret")
	if err != nil {
		return 0, err
	}
	return time.Duration(seconds) * time.Second, nil
}
