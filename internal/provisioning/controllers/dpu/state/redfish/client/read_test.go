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

package client

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httptrace"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nvidia/doca-platform/internal/utils/tracing"

	"github.com/go-resty/resty/v2"
	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func testReadClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	server := httptest.NewTLSServer(handler)
	t.Cleanup(server.Close)
	c, err := NewRawClient(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.CloseIdleConnections)
	return c
}

type readTransportFunc func(*http.Request) (*http.Response, error)

func (f readTransportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestReadAppliesDefaultDeadlineAndPreservesEarlierDeadline(t *testing.T) {
	for _, short := range []bool{false, true} {
		t.Run(fmt.Sprint(short), func(t *testing.T) {
			ctx := context.Background()
			if short {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, 10*time.Second)
				defer cancel()
			}
			c := &Client{Client: resty.New().SetBaseURL("https://bmc.invalid")}
			defer c.CloseIdleConnections()
			called := false
			started := time.Now()
			c.readClient().GetClient().Transport = readTransportFunc(func(r *http.Request) (*http.Response, error) {
				called = true
				deadline, ok := r.Context().Deadline()
				if !ok {
					t.Fatal("request has no deadline")
				}
				if short {
					parentDeadline, _ := ctx.Deadline()
					if !deadline.Equal(parentDeadline) {
						t.Fatalf("parent deadline changed: got %v want %v", deadline, parentDeadline)
					}
				} else if deadline.Before(started.Add(ReadTimeout)) || deadline.After(time.Now().Add(ReadTimeout)) {
					t.Fatalf("unexpected default deadline: %v", deadline)
				}
				return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{}`)), Request: r}, nil
			})
			if _, _, err := c.GetManagers(ctx); err != nil || !called {
				t.Fatalf("read failed: called=%v err=%v", called, err)
			}
		})
	}
}

func TestReadStatusAndPayload(t *testing.T) {
	for _, status := range []int{201, 202, 204, 301, 302, 307, 400, 401, 403, 404, 408, 409, 429, 500, 502, 503, 504, 599, 799} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			c := testReadClient(t, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Retry-After", "12")
				w.WriteHeader(status)
				_, _ = io.WriteString(w, "not JSON; sensitive response must not be logged")
			})
			resp, data, err := c.GetRootService(context.Background())
			if !HasHTTPStatus(err, status) || data != nil || resp == nil || resp.Header().Get("Retry-After") != "12" {
				t.Fatalf("status lost: response=%v data=%v error=%v", resp, data, err)
			}
			if strings.Contains(err.Error(), "sensitive") {
				t.Fatal("response leaked into error")
			}
		})
	}
	for _, body := range []string{"", "null", "not JSON", "[]"} {
		t.Run("invalid-"+body, func(t *testing.T) {
			c := testReadClient(t, func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, body) })
			resp, data, err := c.GetRootService(context.Background())
			var requestErr *RequestError
			if !errors.As(err, &requestErr) || requestErr.StatusCode != 200 || requestErr.Cause == nil || data != nil || resp == nil {
				t.Fatalf("required payload failure not preserved: %v", err)
			}
		})
	}
}

func TestReadErrorIdentifiers(t *testing.T) {
	c := testReadClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = io.WriteString(w, `{"error":{"code":"Base.1.GeneralError","@Message.ExtendedInfo":[{"MessageId":"Base.1.PasswordChangeRequired"}]}}`)
	})
	resp, _, err := c.GetManagers(context.Background())
	var requestErr *RequestError
	if !errors.As(err, &requestErr) || requestErr.Code != "Base.1.GeneralError" || len(requestErr.MessageIDs) != 1 || !PasswordChangeRequired(resp) {
		t.Fatalf("identifiers lost: %v", err)
	}
}

func TestReadDoesNotFollowRedirect(t *testing.T) {
	var followed atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { followed.Add(1) }))
	defer target.Close()
	c := testReadClient(t, func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, http.StatusFound) })
	_, _, err := c.GetManagers(context.Background())
	if !HasHTTPStatus(err, http.StatusFound) || followed.Load() != 0 {
		t.Fatalf("redirect followed: %v", err)
	}
	_, _, err = read[Managers](context.Background(), c, target.URL)
	if err == nil || followed.Load() != 0 {
		t.Fatal("absolute collection link followed")
	}
}

func TestLogicalReadSharesPrerequisiteDeadline(t *testing.T) {
	c := &Client{Client: resty.New().SetBaseURL("https://bmc.invalid")}
	defer c.CloseIdleConnections()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	want, _ := ctx.Deadline()
	var paths []string
	c.readClient().GetClient().Transport = readTransportFunc(func(r *http.Request) (*http.Response, error) {
		paths = append(paths, r.URL.Path)
		if got, ok := r.Context().Deadline(); !ok || !got.Equal(want) {
			t.Fatalf("parent deadline not preserved: got %v want %v", got, want)
		}
		if r.URL.Path == "/redfish/v1/Systems" {
			return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"Members":[{"@odata.id":"/redfish/v1/Systems/Bluefield"}]}`)), Request: r}, nil
		}
		cancel()
		return nil, r.Context().Err()
	})
	_, _, err := c.GetSystem(ctx)
	if !errors.Is(err, context.Canceled) || strings.Join(paths, ",") != "/redfish/v1/Systems,/redfish/v1/Systems/Bluefield" {
		t.Fatalf("logical read lost cancellation or skipped a request: paths=%v err=%v", paths, err)
	}
}

func TestLogicalReadSharesDefaultDeadline(t *testing.T) {
	c := &Client{Client: resty.New().SetBaseURL("https://bmc.invalid")}
	defer c.CloseIdleConnections()
	var firstDeadline time.Time
	var paths []string
	started := time.Now()
	c.readClient().GetClient().Transport = readTransportFunc(func(r *http.Request) (*http.Response, error) {
		paths = append(paths, r.URL.Path)
		deadline, ok := r.Context().Deadline()
		if !ok {
			t.Fatal("request has no deadline")
		}
		if firstDeadline.IsZero() {
			firstDeadline = deadline
			if deadline.Before(started.Add(ReadTimeout)) || deadline.After(time.Now().Add(ReadTimeout)) {
				t.Fatalf("unexpected default deadline: %v", deadline)
			}
		} else if !deadline.Equal(firstDeadline) {
			t.Fatalf("prerequisite and resource received different budgets: %v != %v", firstDeadline, deadline)
		}
		body := `{}`
		if r.URL.Path == "/redfish/v1/Systems" {
			body = `{"Members":[{"@odata.id":"/redfish/v1/Systems/Bluefield"}]}`
		}
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	})
	if _, _, err := c.GetSystem(context.Background()); err != nil {
		t.Fatal(err)
	}
	if strings.Join(paths, ",") != "/redfish/v1/Systems,/redfish/v1/Systems/Bluefield" {
		t.Fatalf("unexpected request sequence: %v", paths)
	}
}

func TestReadCanceledBeforeDispatch(t *testing.T) {
	var requests atomic.Int32
	c := testReadClient(t, func(w http.ResponseWriter, r *http.Request) { requests.Add(1) })
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _, err := c.GetSystem(ctx)
	if !errors.Is(err, context.Canceled) || requests.Load() != 0 {
		t.Fatalf("dispatched canceled read: %v", err)
	}
	if !strings.Contains(err.Error(), "canceled") {
		t.Fatalf("cancellation missing from error: %v", err)
	}
}

func TestReadPartialBodyPreservesStatusAndCause(t *testing.T) {
	for _, cause := range []error{context.DeadlineExceeded, context.Canceled, io.ErrUnexpectedEOF} {
		t.Run(cause.Error(), func(t *testing.T) {
			c := &Client{Client: resty.New().SetBaseURL("https://bmc.invalid")}
			defer c.CloseIdleConnections()
			c.readClient().GetClient().Transport = readTransportFunc(func(r *http.Request) (*http.Response, error) {
				body := io.MultiReader(strings.NewReader(`{"Members":`), readFunc(func([]byte) (int, error) { return 0, cause }))
				return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(body), Request: r}, nil
			})
			resp, data, err := c.GetManagers(context.Background())
			var requestErr *RequestError
			if !errors.As(err, &requestErr) {
				t.Fatalf("missing RequestError: %T %v", err, err)
			}
			if !errors.Is(err, cause) || requestErr.StatusCode != 200 || resp == nil || data != nil {
				t.Fatalf("status/cause lost: status=%d cause=%T %v want=%v", requestErr.StatusCode, requestErr.Cause, requestErr.Cause, cause)
			}
			if HasHTTPStatus(err, 200) {
				t.Fatal("partial response classified as completed status")
			}
		})
	}
}

// readFunc adapts a controlled body read for error and cancellation tests.
type readFunc func([]byte) (int, error)

func (f readFunc) Read(p []byte) (int, error) { return f(p) }

func TestReadPartialBodyCancellation(t *testing.T) {
	release := make(chan struct{})
	c := testReadClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"Members":`)
		w.(http.Flusher).Flush()
		// Keep the response unfinished until the client has returned.
		<-release
	})
	defer close(release)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	transport := c.readClient().GetClient().Transport
	defer transport.(*http.Transport).CloseIdleConnections()
	c.readClient().GetClient().Transport = readTransportFunc(func(r *http.Request) (*http.Response, error) {
		resp, err := transport.RoundTrip(r)
		if err == nil {
			body := resp.Body
			resp.Body = struct {
				io.Reader
				io.Closer
			}{readFunc(func(p []byte) (int, error) {
				n, err := body.Read(p)
				if n > 0 {
					cancel()
				}
				return n, err
			}), body}
		}
		return resp, err
	})
	resp, data, err := c.GetManagers(ctx)
	var requestErr *RequestError
	if !errors.As(err, &requestErr) {
		t.Fatalf("missing RequestError: %T %v ctx=%v", err, err, ctx.Err())
	}
	if !errors.Is(err, context.Canceled) || requestErr.StatusCode != 200 || resp == nil || data != nil {
		t.Fatalf("status/cause lost: status=%d cause=%T %v ctx=%v", requestErr.StatusCode, requestErr.Cause, requestErr.Cause, ctx.Err())
	}
	if HasHTTPStatus(err, 200) {
		t.Fatal("partial response classified as completed status")
	}
}

func TestReadPoolCancellationDoesNotCancelOtherRequest(t *testing.T) {
	arrived := make(chan struct{}, 2)
	release := make(chan struct{})
	c := testReadClient(t, func(w http.ResponseWriter, r *http.Request) {
		arrived <- struct{}{}
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		_, _ = io.WriteString(w, `{}`)
	})
	defer close(release)
	parent, stop := context.WithTimeout(context.Background(), 10*time.Second)
	defer stop()
	done := make(chan error, 1)
	go func() { _, _, err := c.GetManagers(parent); done <- err }()
	select {
	case <-arrived:
	case <-parent.Done():
		t.Fatal("first request did not reach the server")
	}
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	// Cancel inside the transport's connection-acquisition path.
	ctx = httptrace.WithClientTrace(ctx, &httptrace.ClientTrace{GetConn: func(string) { cancel() }})
	_, _, err := c.GetSystems(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("pool wait unbounded: %v", err)
	}
	select {
	case release <- struct{}{}:
	case <-parent.Done():
		t.Fatal("first request was not waiting for release")
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("other request canceled: %v", err)
		}
	case <-parent.Done():
		t.Fatal("first request did not finish")
	}
	select {
	case <-arrived:
		t.Fatal("canceled request reached the server")
	default:
	}
}

func TestReadDialAndTransportIsolation(t *testing.T) {
	c := &Client{Client: resty.New().SetBaseURL("https://127.0.0.1")}
	defer c.CloseIdleConnections()
	original := c.Client.GetClient().Transport
	reader := c.readClient()
	transport, err := reader.Transport()
	if err != nil {
		t.Fatal(err)
	}
	if transport == original || transport.TLSHandshakeTimeout != readConnectTimeout || transport.MaxConnsPerHost != readMaxConnsPerHost || c.Client.GetClient().Timeout != 0 {
		t.Fatal("read transport did not preserve mutation isolation / caps")
	}
	release := make(chan struct{})
	defer close(release)
	transport.DialContext = func(ctx context.Context, _, _ string) (net.Conn, error) {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-release:
			return nil, context.Canceled
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	_, _, err = c.GetManagers(ctx)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("dial not canceled: %v", err)
	}
}

func TestReadHandshakeCancellation(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer func() { _ = clientConn.Close() }()
	defer func() { _ = serverConn.Close() }()
	c, err := NewRawClient("https://bmc.invalid")
	if err != nil {
		t.Fatal(err)
	}
	defer c.CloseIdleConnections()
	transport, err := c.readClient().Transport()
	if err != nil {
		t.Fatal(err)
	}
	transport.DialContext = func(context.Context, string, string) (net.Conn, error) { return clientConn, nil }
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	started := make(chan error, 1)
	go func() {
		var b [1]byte
		_, err := serverConn.Read(b[:])
		started <- err
		if err == nil {
			cancel() // TLS has started sending its ClientHello.
		}
	}()
	_, _, err = c.GetManagers(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("TLS not canceled: %T %v ctx=%v", err, err, ctx.Err())
	}
	select {
	case err := <-started:
		if err != nil {
			t.Fatal(err)
		}
	default:
		t.Fatal("TLS handshake did not start")
	}
}

func TestReadFailureDoesNotRetryOrMutate(t *testing.T) {
	for _, status := range []int{http.StatusServiceUnavailable, http.StatusForbidden} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			var mutations, managers atomic.Int32
			c := testReadClient(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet {
					mutations.Add(1)
				}
				if strings.HasSuffix(r.URL.Path, "/Managers") {
					managers.Add(1)
					w.WriteHeader(status)
				}
				_, _ = io.WriteString(w, `{}`)
			})
			_, err := RotatePassword(context.Background(), c.BaseURL, "new", "old")
			if err == nil || mutations.Load() != 0 || managers.Load() != 1 {
				t.Fatalf("failure triggered retry/write: %v, requests=%d writes=%d", err, managers.Load(), mutations.Load())
			}
		})
	}
}

func TestCredentialVerificationRootForbidden(t *testing.T) {
	for _, required := range []bool{false, true} {
		t.Run(fmt.Sprint(required), func(t *testing.T) {
			var writes atomic.Int32
			c := testReadClient(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet {
					writes.Add(1)
				}
				w.WriteHeader(http.StatusForbidden)
				if required {
					_, _ = io.WriteString(w, `{"error":{"@Message.ExtendedInfo":[{"MessageId":"Base.1.0.PasswordChangeRequired"}]}}`)
				} else {
					_, _ = io.WriteString(w, `{"error":{"code":"Base.1.0.InsufficientPrivilege"}}`)
				}
			})
			verified, _, err := VerifyBMCCredential(context.Background(), c.BaseURL, BMCDefaultPassword)
			if verified != nil {
				defer verified.CloseIdleConnections()
			}
			if required {
				if err != nil || verified == nil {
					t.Fatalf("structured password-change requirement blocked verification: %v", err)
				}
			} else if verified != nil || !HasHTTPStatus(err, http.StatusForbidden) {
				t.Fatalf("plain forbidden response accepted: client=%v err=%v", verified, err)
			}
			if writes.Load() != 0 {
				t.Fatal("verification dispatched a mutation")
			}
		})
	}
}

func TestReadCancellationDoesNotChangeMutationLifetime(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	c := testReadClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			cancel()
			<-r.Context().Done()
			return
		}
		_, _ = io.WriteString(w, `{}`)
	})
	_, _, err := c.GetManagers(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("read did not reach cancellation: %T %v ctx=%v", err, err, ctx.Err())
	}
	if _, _, err := c.EnableMTLS(context.Background()); err != nil {
		t.Fatalf("read cancellation leaked into mutation: %v", err)
	}
}

func TestLegacyBootObservationDoesNotUseReadTransport(t *testing.T) {
	var reads atomic.Int32
	c := testReadClient(t, func(w http.ResponseWriter, r *http.Request) {
		reads.Add(1)
		if strings.HasSuffix(r.URL.Path, "/Systems") {
			_, _ = io.WriteString(w, `{"Members":[{"@odata.id":"/redfish/v1/Systems/Bluefield"}]}`)
			return
		}
		_, _ = io.WriteString(w, `{"Boot":{"BootSourceOverrideTarget":"Usb"}}`)
	})
	// A failed bounded-read transport must not affect the explicitly deferred
	// observation inside a BF4 mutation sequence.
	c.readClient().GetClient().Transport = failingReadTransport{}
	_, settings, err := c.GetSettingsForBootMutation(context.Background())
	if err != nil || settings.Boot.BootSourceOverrideTarget != "Usb" || reads.Load() != 2 {
		t.Fatalf("legacy boot observation changed: settings=%v err=%v reads=%d", settings, err, reads.Load())
	}
}

type failingReadTransport struct{}

func (failingReadTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, context.DeadlineExceeded
}

func TestLegacyVirtualMediaSequenceDoesNotUseReadTransport(t *testing.T) {
	var writes atomic.Int32
	c := testReadClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			writes.Add(1)
			_, _ = io.WriteString(w, `{}`)
			return
		}
		if strings.HasSuffix(r.URL.Path, "/Managers") {
			_, _ = io.WriteString(w, `{"Members":[{"@odata.id":"/redfish/v1/Managers/BMC"}]}`)
			return
		}
		_, _ = fmt.Fprintf(w, `{"Inserted":%t}`, writes.Load() == 2)
	})
	c.readClient().GetClient().Transport = failingReadTransport{}
	if _, err := c.InsertVirtualMediaImage(context.Background()); err != nil || writes.Load() != 2 {
		t.Fatalf("legacy eject/observe/insert/observe changed: err=%v writes=%d", err, writes.Load())
	}
}

func TestAcquireAndReadSharesDeadline(t *testing.T) {
	parent := context.Background()
	ctx, cancel := context.WithTimeout(parent, 10*time.Second)
	defer cancel()
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{}`)
	}))
	defer server.Close()
	c, err := NewBasicAuthClient(ctx, server.URL, "root", "test-password")
	if err != nil {
		t.Fatal(err)
	}
	defer c.CloseIdleConnections()
	want, _ := ctx.Deadline()
	transport, err := c.readClient().Transport()
	if err != nil {
		t.Fatal(err)
	}
	defer transport.CloseIdleConnections()
	c.readClient().GetClient().Transport = readTransportFunc(func(r *http.Request) (*http.Response, error) {
		if got, ok := r.Context().Deadline(); !ok || !got.Equal(want) {
			t.Fatalf("acquisition/read deadline changed: got=%v want=%v", got, want)
		}
		cancel()
		return nil, r.Context().Err()
	})
	_, _, err = c.GetManagers(ctx)
	if !errors.Is(err, context.Canceled) || parent.Err() != nil {
		t.Fatalf("acquisition/read cancellation or parent changed: %v", err)
	}
}

func TestReadReusesAndClosesConnections(t *testing.T) {
	var opened atomic.Int32
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{}`)
	}))
	server.Config.ConnState = func(_ net.Conn, state http.ConnState) {
		if state == http.StateNew {
			opened.Add(1)
		}
	}
	server.StartTLS()
	defer server.Close()
	c, err := NewRawClient(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer c.CloseIdleConnections()
	for range 2 {
		if _, _, err := c.GetManagers(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if opened.Load() != 1 {
		t.Fatalf("read pool not reused: %d connections", opened.Load())
	}
	c.CloseIdleConnections()
	if _, _, err := c.GetManagers(context.Background()); err != nil {
		t.Fatal(err)
	}
	if opened.Load() != 2 {
		t.Fatalf("idle connection was not disposed: %d connections", opened.Load())
	}
}

// A cancel that races the response must surface as a cancel, whether the race
// shows up as a truncated body (decode error) or as a non-context transport error.
func TestReadCancelRacingResponseReportsCancellation(t *testing.T) {
	for name, body := range map[string]func(cancel context.CancelFunc) io.ReadCloser{
		"empty body":   func(cancel context.CancelFunc) io.ReadCloser { cancel(); return io.NopCloser(strings.NewReader("")) },
		"read failure": func(cancel context.CancelFunc) io.ReadCloser { cancel(); return io.NopCloser(iotestErrReader{}) },
	} {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			t.Cleanup(cancel)
			c := testReadClient(t, func(http.ResponseWriter, *http.Request) {})
			c.readClient().GetClient().Transport = readTransportFunc(func(r *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       body(cancel),
					Header:     http.Header{},
					Request:    r,
				}, nil
			})
			if _, _, err := c.GetManagers(ctx); !errors.Is(err, context.Canceled) {
				t.Fatalf("want context.Canceled, got %T %v", err, err)
			}
		})
	}
}

// iotestErrReader is a body that fails every read with a non-context error.
type iotestErrReader struct{}

// Read always returns a connection error.
func (iotestErrReader) Read([]byte) (int, error) { return 0, errors.New("connection reset") }

// TestBMCHostPortStripsCredentials checks that a BMC URL keeps only host and port.
func TestBMCHostPortStripsCredentials(t *testing.T) {
	host, port := bmcHostPort("https://root:0penBmc@10.0.0.8:443/redfish?token=secret")
	if host != "10.0.0.8" || port != "443" {
		t.Fatalf("bmcHostPort() = %s %s", host, port)
	}
	host, port = bmcHostPort("bmc.example:443")
	if host != "bmc.example" || port != "443" {
		t.Fatalf("bmcHostPort(host:port) = %s %s", host, port)
	}
	if host, port := bmcHostPort(""); host != "" || port != "" {
		t.Fatalf("bmcHostPort(empty) = %s %s", host, port)
	}
	if user := redfishUser(nil, "https://root:0penBmc@10.0.0.8:443", ""); user != "root" {
		t.Fatalf("redfishUser() = %q", user)
	}
}

// TestRedfishAPINameUsesMap checks that a request path returns the API name stored for that path.
func TestRedfishAPINameUsesMap(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{RedfishAPIs.Path("APIInstallBFB") + "?token=secret", "APIInstallBFB"},
		{RedfishAPIs.Path("APICheckOSImage"), "APICheckOSImage"},
		{RedfishAPIs.Path("APIRootService"), "APIRootService"},
		{RedfishAPIs.Path("APICheckBMCFW"), "APICheckBMCFW"},
		{RedfishAPIs.Path("APICheckDPUUEFI"), "APICheckDPUUEFI"},
		{RedfishAPIs.Path("APICheckDPUNIC"), "APICheckDPUNIC"},
		{"redfish/v1/TaskService/Tasks/1", "APICheckProgress"},
		{"redfish/v1/Managers/Bluefield_BMC/VirtualMedia/IMAGE/Actions/VirtualMedia.EjectMedia", "APIEjectVirtualMedia"},
		{"redfish/v1/Systems/Bluefield/Settings", "APIBluefieldSettings"},
		{"redfish/v1/Chassis/BlueField_0/Actions/Oem/NvidiaChassis.Reset", "APIChassisReset"},
		{"redfish/v1/Managers/Bluefield_BMC/Truststore/Certificates/1", "APITruststoreCert"},
	}
	for _, tt := range tests {
		if got := RedfishAPIs.Name(tt.path); got != tt.want {
			t.Errorf("Name(%q) = %q, want %q", tt.path, got, tt.want)
		}
	}
}

// TestRedfishHookNestsUnderPhaseSpan checks that a POST with no request context is a child of the
// span captured when the client was created, and that the span name is the API constant.
// Templated GETs are named from the unsubstituted path, while the BMC receives the filled-in path.
func TestRedfishHookNestsUnderPhaseSpan(t *testing.T) {
	rec := tracetest.NewSpanRecorder()
	prev := otel.GetTracerProvider()
	otel.SetTracerProvider(sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec)))
	t.Cleanup(func() { otel.SetTracerProvider(prev) })

	var gotPaths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPaths = append(gotPaths, r.URL.Path)
		w.WriteHeader(http.StatusAccepted)
	}))
	t.Cleanup(server.Close)

	phaseCtx, phaseSpan := tracing.StartSpan(context.Background(), "dpu.phase.OSInstalling")
	base, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	base.User = url.UserPassword("root", "0penBmc")
	c := &Client{
		Client:      resty.New().SetBaseURL(base.String()),
		traceParent: traceParentFrom(phaseCtx),
	}
	c.installRedfishTracing(c.Client)

	if _, err := c.R().Post(RedfishAPIs.Path("APIInstallBFB") + "?token=secret"); err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		name  string
		param string
		value string
	}{
		{"APICheckBMCFW", "BMC_FW_ID", "BlueField_FW_BMC_0"},
		{"APICheckDPUUEFI", "DPU_UEFI_ID", "BlueField_FW_CPU_0"},
		{"APICheckDPUNIC", "DPU_NIC_ID", "BlueField_FW_NIC_0"},
	} {
		if _, err := c.R().SetPathParam(tt.param, tt.value).Get(RedfishAPIs.Path(tt.name)); err != nil {
			t.Fatal(err)
		}
	}
	phaseSpan.End()

	var child sdktrace.ReadOnlySpan
	for _, span := range rec.Ended() {
		if span.Name() == "dpu.redfish.APIInstallBFB" {
			child = span
		}
	}
	if child == nil {
		names := make([]string, 0, len(rec.Ended()))
		for _, span := range rec.Ended() {
			names = append(names, span.Name())
		}
		t.Fatalf("missing redfish span in %v", names)
	}
	if child.Parent().SpanID() != phaseSpan.SpanContext().SpanID() {
		t.Fatalf("parent = %s, want phase span %s", child.Parent().SpanID(), phaseSpan.SpanContext().SpanID())
	}
	names := map[string]struct{}{}
	for _, span := range rec.Ended() {
		names[span.Name()] = struct{}{}
	}
	for _, name := range []string{"dpu.redfish.APICheckBMCFW", "dpu.redfish.APICheckDPUUEFI", "dpu.redfish.APICheckDPUNIC"} {
		if _, ok := names[name]; !ok {
			t.Errorf("missing span %s in %v", name, names)
		}
	}
	wantPath := "/redfish/v1/UpdateService/FirmwareInventory/BlueField_FW_BMC_0"
	foundPath := false
	for _, path := range gotPaths {
		if path == wantPath {
			foundPath = true
		}
		if strings.Contains(path, "{") {
			t.Errorf("BMC received unsubstituted path %s", path)
		}
	}
	if !foundPath {
		t.Errorf("paths = %v, want %s", gotPaths, wantPath)
	}
	if path := spanStringAttr(rec, "dpu.redfish.APICheckBMCFW", "path"); path != wantPath {
		t.Errorf("templated GET path = %s, want %s", path, wantPath)
	}
	got := map[string]string{}
	for _, attr := range child.Attributes() {
		switch string(attr.Key) {
		case "method", "host", "port", "user", "path":
			got[string(attr.Key)] = attr.Value.AsString()
		}
		if string(attr.Key) == "http.response.status_code" && attr.Value.AsInt64() != http.StatusAccepted {
			t.Fatalf("status = %d", attr.Value.AsInt64())
		}
	}
	bmc, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	if got["method"] != http.MethodPost || got["host"] != bmc.Hostname() || got["port"] != bmc.Port() || got["user"] != "root" ||
		got["path"] != "/"+RedfishAPIs.Path("APIInstallBFB") {
		t.Fatalf("attributes = %v", got)
	}
}

// TestMTLSClientSpanUser checks that spans from the verified mTLS client report the BMC account of
// the client's BlueField family as user, since that client has no basic-auth username.
func TestMTLSClientSpanUser(t *testing.T) {
	rec := tracetest.NewSpanRecorder()
	prev := otel.GetTracerProvider()
	otel.SetTracerProvider(sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec)))
	t.Cleanup(func() { otel.SetTracerProvider(prev) })

	authority := newCA(t, "test-ca")
	server := newBMC(t, authority, nil)
	certPEM, keyPEM := authority.clientPair(t)
	pair, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	pool.AddCert(authority.cert)
	for _, tt := range []struct {
		isBF4 bool
		want  string
	}{
		{false, BF3BMCUser},
		{true, BF4BMCUser},
	} {
		rec.Reset()
		c, err := newVerifiedTLSClient(server.URL, pool, pair, tt.isBF4)
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err := c.GetRootService(context.Background()); err != nil {
			t.Fatal(err)
		}
		c.CloseIdleConnections()
		if got := spanStringAttr(rec, "dpu.redfish.APIRootService", "user"); got != tt.want {
			t.Errorf("isBF4=%t: user = %q, want %q", tt.isBF4, got, tt.want)
		}
	}
}

// spanStringAttr returns the string attribute key of the first ended span named name, or "" when
// there is no such span or attribute.
func spanStringAttr(rec *tracetest.SpanRecorder, name, key string) string {
	for _, span := range rec.Ended() {
		if span.Name() != name {
			continue
		}
		for _, attr := range span.Attributes() {
			if string(attr.Key) == key {
				return attr.Value.AsString()
			}
		}
		return ""
	}
	return ""
}

// TestSharedClientMutationJoinsCallerSpan checks that a mutation on a client with no traceParent,
// as the shared mTLS clients are, is a child of the span on the caller's context.
func TestSharedClientMutationJoinsCallerSpan(t *testing.T) {
	rec := tracetest.NewSpanRecorder()
	prev := otel.GetTracerProvider()
	otel.SetTracerProvider(sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec)))
	t.Cleanup(func() { otel.SetTracerProvider(prev) })

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
		_, _ = io.WriteString(w, `{"Id":"1"}`)
	}))
	t.Cleanup(server.Close)
	c := &Client{
		Client: resty.New().SetBaseURL(server.URL),
	}
	c.installRedfishTracing(c.Client)

	phaseCtx, phaseSpan := tracing.StartSpan(context.Background(), "dpu.phase.OSInstalling")
	if _, _, err := c.InstallBFB(phaseCtx, "bfb.example/image.bfb"); err != nil {
		t.Fatal(err)
	}
	phaseSpan.End()

	for _, span := range rec.Ended() {
		if span.Name() != "dpu.redfish.APIInstallBFB" {
			continue
		}
		if span.Parent().SpanID() != phaseSpan.SpanContext().SpanID() {
			t.Fatalf("parent = %s, want phase span %s", span.Parent().SpanID(), phaseSpan.SpanContext().SpanID())
		}
		return
	}
	t.Fatalf("missing dpu.redfish.APIInstallBFB span")
}
