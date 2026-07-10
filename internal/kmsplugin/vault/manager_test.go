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
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/go-logr/logr"
	"github.com/go-logr/logr/funcr"
	vaultapi "github.com/hashicorp/vault/api"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Manager", func() {
	var ctx context.Context

	BeforeEach(func() {
		ctx = context.Background()
	})

	Describe("check", func() {
		It("logs in when no token has been obtained yet", func() {
			auth := &fakeAuthenticator{tokens: []string{"t1"}}
			api := &fakeAPI{lookupFunc: func(_ context.Context) (*vaultapi.Secret, error) {
				return lookupSelfSecret(time.Hour), nil
			}}
			m := NewTokenManager(api, auth, logr.Discard())

			m.check(ctx)

			Expect(auth.calls).To(Equal(1))
			Expect(api.setTokenCalls).To(ConsistOf("t1"))
			Expect(api.lookupCalls).To(Equal(1), "a freshly authenticated token is looked up so short token TTL warnings are emitted immediately")
			Expect(api.renewCalls).To(Equal(0))
			Expect(m.hasToken).To(BeTrue())
		})

		It("warns when the initial token creation TTL is too short for the check interval", func() {
			auth := &fakeAuthenticator{tokens: []string{"t1"}}
			api := &fakeAPI{lookupFunc: func(_ context.Context) (*vaultapi.Secret, error) {
				return lookupSelfSecret(30 * time.Second), nil
			}}
			log, buf := testLogger()
			m := NewTokenManager(api, auth, log)

			m.check(ctx)

			Expect(auth.calls).To(Equal(1))
			Expect(api.lookupCalls).To(Equal(1))
			Expect(api.renewCalls).To(Equal(0))
			Expect(buf.String()).To(ContainSubstring(`"msg"="token creation TTL is too short to guarantee a check tick lands inside its renew-eligible window and it may expire before it can be renewed, consider increasing the auth role's token TTL"`))
		})

		It("leaves hasToken false when the initial authentication fails", func() {
			auth := &fakeAuthenticator{errs: []error{errors.New("boom")}}
			m := NewTokenManager(&fakeAPI{}, auth, logr.Discard())

			m.check(ctx)

			Expect(auth.calls).To(Equal(1))
			Expect(m.hasToken).To(BeFalse())
		})

		It("does not log at default verbosity when the token is healthy", func() {
			auth := &fakeAuthenticator{}
			api := &fakeAPI{lookupFunc: func(_ context.Context) (*vaultapi.Secret, error) {
				return lookupSelfSecret(time.Hour), nil
			}}
			log, buf := testLogger()
			m := NewTokenManager(api, auth, log)
			m.hasToken = true

			m.check(ctx)

			Expect(api.lookupCalls).To(Equal(1))
			Expect(api.renewCalls).To(Equal(0))
			Expect(auth.calls).To(Equal(0))
			Expect(buf.String()).NotTo(ContainSubstring(`"msg"="token has enough TTL, skipping renewal"`))
		})

		It("logs at verbosity 1 when the token is healthy", func() {
			auth := &fakeAuthenticator{}
			api := &fakeAPI{lookupFunc: func(_ context.Context) (*vaultapi.Secret, error) {
				return lookupSelfSecret(time.Hour), nil
			}}
			log, buf := testLogger(1)
			m := NewTokenManager(api, auth, log)
			m.hasToken = true

			m.check(ctx)

			Expect(api.lookupCalls).To(Equal(1))
			Expect(api.renewCalls).To(Equal(0))
			Expect(auth.calls).To(Equal(0))
			Expect(buf.String()).To(ContainSubstring(`"msg"="token has enough TTL, skipping renewal"`))
		})

		It("does nothing for a non-expiring token", func() {
			auth := &fakeAuthenticator{}
			api := &fakeAPI{lookupFunc: func(_ context.Context) (*vaultapi.Secret, error) {
				return lookupSelfSecret(0), nil
			}}
			m := NewTokenManager(api, auth, logr.Discard())
			m.hasToken = true

			m.check(ctx)

			Expect(api.renewCalls).To(Equal(0))
			Expect(auth.calls).To(Equal(0))
		})

		It("renews the token once the elapsed fraction of its creation TTL reaches renewFraction", func() {
			auth := &fakeAuthenticator{}
			lookupResponses := []*vaultapi.Secret{
				// 50 of 60 minutes elapsed (5/6): past the 2/3 threshold.
				lookupSelfSecretAt(10*time.Minute, time.Hour),
				// After renewal, the token is back below the renewal threshold.
				lookupSelfSecret(time.Hour),
			}
			lookupIdx := 0
			api := &fakeAPI{
				lookupFunc: func(_ context.Context) (*vaultapi.Secret, error) {
					secret := lookupResponses[lookupIdx]
					lookupIdx++
					return secret, nil
				},
			}
			log, buf := testLogger()
			m := NewTokenManager(api, auth, log)
			m.hasToken = true

			m.check(ctx)

			Expect(api.renewCalls).To(Equal(1))
			Expect(api.lookupCalls).To(Equal(2))
			Expect(auth.calls).To(Equal(0))
			Expect(m.hasToken).To(BeTrue())
			Expect(buf.String()).To(ContainSubstring(`"msg"="token renewed successfully"`))
		})

		It("re-authenticates when a successful renewal leaves the token past the renewal threshold", func() {
			auth := &fakeAuthenticator{tokens: []string{"t2"}}
			lookupResponses := []*vaultapi.Secret{
				// The token is due for renewal.
				lookupSelfSecretAt(10*time.Minute, time.Hour),
				// Vault accepted renewal but capped the TTL near the token's max TTL.
				lookupSelfSecretAt(10*time.Minute, time.Hour),
			}
			lookupIdx := 0
			api := &fakeAPI{
				lookupFunc: func(_ context.Context) (*vaultapi.Secret, error) {
					secret := lookupResponses[lookupIdx]
					lookupIdx++
					return secret, nil
				},
			}
			log, buf := testLogger()
			m := NewTokenManager(api, auth, log)
			m.hasToken = true

			m.check(ctx)

			Expect(api.renewCalls).To(Equal(1))
			Expect(api.lookupCalls).To(Equal(2))
			Expect(auth.calls).To(Equal(1))
			Expect(api.setTokenCalls).To(ConsistOf("t2"))
			Expect(buf.String()).To(ContainSubstring(`"msg"="re-authentication succeeded"`))
		})

		It("re-authenticates when renewal fails with an auth error", func() {
			auth := &fakeAuthenticator{tokens: []string{"t2"}}
			api := &fakeAPI{
				lookupFunc: func(_ context.Context) (*vaultapi.Secret, error) {
					return lookupSelfSecretAt(10*time.Minute, time.Hour), nil
				},
				renewFunc: func(_ context.Context) error {
					return responseError(403)
				},
			}
			m := NewTokenManager(api, auth, logr.Discard())
			m.hasToken = true

			m.check(ctx)

			Expect(api.renewCalls).To(Equal(1))
			Expect(auth.calls).To(Equal(1))
			Expect(api.setTokenCalls).To(ConsistOf("t2"))
		})

		It("keeps the current token when re-authentication after renewal failure fails", func() {
			auth := &fakeAuthenticator{errs: []error{errors.New("login broken")}}
			api := &fakeAPI{
				lookupFunc: func(_ context.Context) (*vaultapi.Secret, error) {
					return lookupSelfSecretAt(10*time.Minute, time.Hour), nil
				},
				renewFunc: func(_ context.Context) error {
					return responseError(403)
				},
			}
			m := NewTokenManager(api, auth, logr.Discard())
			m.hasToken = true

			m.check(ctx)

			Expect(api.renewCalls).To(Equal(1))
			Expect(auth.calls).To(Equal(1))
			// A renew 403 may be a renewal-permission issue rather than a dead
			// token. Keep using the current token; the next lookup will discard it
			// if Vault rejects it there too.
			Expect(m.hasToken).To(BeTrue())
		})

		It("does not re-authenticate when renewal fails with a connectivity error", func() {
			auth := &fakeAuthenticator{}
			api := &fakeAPI{
				lookupFunc: func(_ context.Context) (*vaultapi.Secret, error) {
					return lookupSelfSecretAt(10*time.Minute, time.Hour), nil
				},
				renewFunc: func(_ context.Context) error {
					return connectivityErr()
				},
			}
			m := NewTokenManager(api, auth, logr.Discard())
			m.hasToken = true

			m.check(ctx)

			Expect(api.renewCalls).To(Equal(1))
			Expect(auth.calls).To(Equal(0))
			Expect(m.hasToken).To(BeTrue())
		})

		It("re-authenticates instead of renewing when the token is not renewable", func() {
			auth := &fakeAuthenticator{tokens: []string{"t2"}}
			api := &fakeAPI{
				lookupFunc: func(_ context.Context) (*vaultapi.Secret, error) {
					secret := lookupSelfSecretAt(10*time.Minute, time.Hour)
					secret.Data["renewable"] = false
					return secret, nil
				},
			}
			m := NewTokenManager(api, auth, logr.Discard())
			m.hasToken = true

			m.check(ctx)

			Expect(api.renewCalls).To(Equal(0))
			Expect(auth.calls).To(Equal(1))
			Expect(api.setTokenCalls).To(ConsistOf("t2"))
		})

		It("keeps the current token when replacing a non-renewable token fails", func() {
			auth := &fakeAuthenticator{errs: []error{errors.New("login broken")}}
			api := &fakeAPI{
				lookupFunc: func(_ context.Context) (*vaultapi.Secret, error) {
					secret := lookupSelfSecretAt(10*time.Minute, time.Hour)
					secret.Data["renewable"] = false
					return secret, nil
				},
			}
			m := NewTokenManager(api, auth, logr.Discard())
			m.hasToken = true

			m.check(ctx)

			Expect(api.renewCalls).To(Equal(0))
			Expect(auth.calls).To(Equal(1))
			Expect(m.hasToken).To(BeTrue())
		})

		It("re-authenticates when the token itself is no longer valid", func() {
			auth := &fakeAuthenticator{tokens: []string{"t2"}}
			api := &fakeAPI{lookupFunc: func(_ context.Context) (*vaultapi.Secret, error) {
				return nil, responseError(403)
			}}
			m := NewTokenManager(api, auth, logr.Discard())
			m.hasToken = true

			m.check(ctx)

			Expect(auth.calls).To(Equal(1))
			Expect(api.renewCalls).To(Equal(0))
			Expect(api.setTokenCalls).To(ConsistOf("t2"))
		})

		It("discards the current token when lookup fails with an auth error and re-authentication fails", func() {
			auth := &fakeAuthenticator{errs: []error{errors.New("login broken")}}
			api := &fakeAPI{lookupFunc: func(_ context.Context) (*vaultapi.Secret, error) {
				return nil, responseError(403)
			}}
			m := NewTokenManager(api, auth, logr.Discard())
			m.hasToken = true

			m.check(ctx)

			Expect(auth.calls).To(Equal(1))
			Expect(api.renewCalls).To(Equal(0))
			Expect(m.hasToken).To(BeFalse())
		})

		It("leaves the token alone on a connectivity error instead of re-authenticating", func() {
			auth := &fakeAuthenticator{}
			api := &fakeAPI{lookupFunc: func(_ context.Context) (*vaultapi.Secret, error) {
				return nil, connectivityErr()
			}}
			m := NewTokenManager(api, auth, logr.Discard())
			m.hasToken = true

			m.check(ctx)

			Expect(auth.calls).To(Equal(0))
			Expect(api.renewCalls).To(Equal(0))
			// A connectivity failure says nothing about the token itself, so
			// it must not be discarded.
			Expect(m.hasToken).To(BeTrue())
		})

		It("leaves the token alone when lookup returns an empty response", func() {
			auth := &fakeAuthenticator{}
			api := &fakeAPI{lookupFunc: func(_ context.Context) (*vaultapi.Secret, error) {
				return nil, nil
			}}
			m := NewTokenManager(api, auth, logr.Discard())
			m.hasToken = true

			m.check(ctx)

			Expect(auth.calls).To(Equal(0))
			Expect(api.renewCalls).To(Equal(0))
			Expect(m.hasToken).To(BeTrue())
		})

		It("does not renew or re-authenticate when the TTL cannot be parsed", func() {
			auth := &fakeAuthenticator{}
			api := &fakeAPI{lookupFunc: func(_ context.Context) (*vaultapi.Secret, error) {
				return &vaultapi.Secret{Data: map[string]interface{}{"ttl": "garbage"}}, nil
			}}
			m := NewTokenManager(api, auth, logr.Discard())
			m.hasToken = true

			m.check(ctx)

			Expect(api.renewCalls).To(Equal(0))
			Expect(auth.calls).To(Equal(0))
		})

		It("falls back to minTTL and renews when the creation TTL cannot be parsed and the remaining TTL has dropped to the floor", func() {
			auth := &fakeAuthenticator{}
			lookupResponses := []*vaultapi.Secret{
				{
					Data: map[string]interface{}{
						"ttl":       json.Number("60"),
						"renewable": true,
					},
				},
				lookupSelfSecret(time.Hour),
			}
			lookupIdx := 0
			api := &fakeAPI{
				lookupFunc: func(_ context.Context) (*vaultapi.Secret, error) {
					// ttl is below the default minTTL and creation_ttl is
					// missing: the fraction can't be computed, so the fixed
					// floor is the only signal left to renew on.
					secret := lookupResponses[lookupIdx]
					lookupIdx++
					return secret, nil
				},
			}
			m := NewTokenManager(api, auth, logr.Discard())
			m.hasToken = true

			m.check(ctx)

			Expect(api.renewCalls).To(Equal(1))
			Expect(api.lookupCalls).To(Equal(2))
			Expect(auth.calls).To(Equal(0))
		})

		It("bounds a hung authentication attempt with checkTimeout instead of hanging forever", func() {
			auth := &blockingAuthenticator{release: make(chan struct{}), started: make(chan struct{})}
			m := NewTokenManager(&fakeAPI{}, auth, logr.Discard())
			m.checkTimeout = 100 * time.Millisecond

			done := make(chan struct{})
			go func() {
				defer close(done)
				m.check(ctx)
			}()

			Eventually(auth.started).WithTimeout(5 * time.Second).Should(BeClosed())
			Eventually(done).WithTimeout(5 * time.Second).Should(BeClosed())
			Expect(m.hasToken).To(BeFalse())
		})

		It("applies token manager timing options", func() {
			m := NewTokenManager(&fakeAPI{}, &fakeAuthenticator{}, logr.Discard(),
				WithTokenCheckInterval(30*time.Second),
				WithLoginTimeout(10*time.Second))

			Expect(m.checkInterval).To(Equal(30 * time.Second))
			Expect(m.checkTimeout).To(Equal(10 * time.Second))
		})
	})

	// The renewal-fraction and short-creation-TTL-warning boundary cases live
	// here as pure decideRenewal cases rather than as check() integration
	// tests: they exercise the policy math directly, without needing a fake
	// secret and TokenManager for each threshold. The check Describe block
	// above keeps only enough cases (e.g. "renews the token once the elapsed
	// fraction ... reaches renewFraction") to prove check wires into
	// decideRenewal and acts on its decision.
	Describe("decideRenewal", func() {
		DescribeTable("evaluating whether a token is due for renewal",
			func(ttl, creationTTL time.Duration, creationTTLErr error, wantRenew, wantShortCreationTTL bool) {
				decision := decideRenewal(ttl, creationTTL, creationTTLErr, defaultRenewFraction, defaultMinTTL, defaultCheckInterval)
				Expect(decision.renewNow).To(Equal(wantRenew))
				Expect(decision.shortCreationTTL).To(Equal(wantShortCreationTTL))
			},
			Entry("renews once the elapsed fraction reaches renewFraction",
				10*time.Minute, time.Hour, error(nil), true, false),
			Entry("renews at the exact renewFraction boundary",
				20*time.Minute, time.Hour, error(nil), true, false),
			Entry("does not renew before the elapsed fraction reaches renewFraction",
				40*time.Minute, time.Hour, error(nil), false, false),
			Entry("flags a short creation TTL and still renews once the fraction is reached",
				5*time.Second, 20*time.Second, error(nil), true, true),
			Entry("flags a short creation TTL but does not renew before the fraction is reached",
				15*time.Second, 20*time.Second, error(nil), false, true),
			Entry("flags a creation TTL between one and three check intervals and renews once the fraction is reached",
				20*time.Second, 120*time.Second, error(nil), true, true),
			Entry("falls back to the minTTL floor and does not renew when the remaining ttl is comfortable",
				10*time.Minute, time.Duration(0), errors.New("missing creation_ttl"), false, false),
			Entry("falls back to the minTTL floor and renews once the remaining ttl drops to it",
				1*time.Minute, time.Duration(0), errors.New("missing creation_ttl"), true, false),
		)
	})

	Describe("Run", func() {
		It("performs the first check immediately instead of waiting for the first tick", func() {
			auth := &fakeAuthenticator{tokens: []string{"t1"}}
			m := NewTokenManager(&fakeAPI{}, auth, logr.Discard())
			// A long interval would never fire within this test's timeout, so
			// seeing an authenticate call proves it came from Run's initial
			// check rather than from the ticker.
			m.checkInterval = time.Hour

			runCtx, cancel := context.WithCancel(ctx)
			DeferCleanup(cancel)
			go m.Run(runCtx)

			Eventually(auth.callCount).Should(Equal(1))
		})

		It("repeats the check on every interval until the context is canceled", func() {
			lookupCalls := make(chan struct{}, 4)
			auth := &fakeAuthenticator{}
			api := &fakeAPI{lookupFunc: func(_ context.Context) (*vaultapi.Secret, error) {
				select {
				case lookupCalls <- struct{}{}:
				default:
				}
				return lookupSelfSecret(time.Hour), nil
			}}
			m := NewTokenManager(api, auth, logr.Discard())
			m.checkInterval = 100 * time.Millisecond
			m.hasToken = true

			runCtx, cancel := context.WithCancel(ctx)
			DeferCleanup(cancel)
			go m.Run(runCtx)

			Eventually(lookupCalls).WithTimeout(5 * time.Second).Should(Receive())
			Eventually(lookupCalls).WithTimeout(5 * time.Second).Should(Receive())
		})
	})
})

func testLogger(verbosity ...int) (logr.Logger, *bytes.Buffer) {
	buf := &bytes.Buffer{}
	options := funcr.Options{}
	if len(verbosity) > 0 {
		options.Verbosity = verbosity[0]
	}
	return funcr.New(func(prefix, args string) {
		buf.WriteString(prefix)
		buf.WriteString(args)
		buf.WriteString("\n")
	}, options), buf
}
