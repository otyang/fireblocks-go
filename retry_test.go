package fireblocks

import (
	"context"
	"crypto"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestBackoffBounds(t *testing.T) {
	c := &core{minBackoff: 500 * time.Millisecond, maxBackoff: 8 * time.Second}
	for attempt := 0; attempt < 200; attempt++ {
		d := c.backoff(attempt, nil)
		if d <= 0 || d > c.maxBackoff {
			t.Fatalf("backoff(%d) = %v, want in (0, %v]", attempt, d, c.maxBackoff)
		}
		if attempt >= 5 && d < c.maxBackoff/2 {
			t.Fatalf("backoff(%d) = %v, want at least %v", attempt, d, c.maxBackoff/2)
		}
	}
	if d := c.backoff(0, nil); d < 250*time.Millisecond || d > 500*time.Millisecond {
		t.Errorf("backoff(0) = %v, want within [250ms, 500ms]", d)
	}
}

func TestBackoffRetryAfter(t *testing.T) {
	c := &core{minBackoff: time.Millisecond, maxBackoff: 2 * time.Millisecond}
	resp := func(v string) *http.Response {
		return &http.Response{Header: http.Header{"Retry-After": {v}}}
	}
	if d := c.backoff(0, resp("3")); d != 3*time.Second {
		t.Errorf("Retry-After 3: waited %v", d)
	}
	if d := c.backoff(0, resp("3600")); d != maxRetryAfter {
		t.Errorf("Retry-After 3600: waited %v, want the %v cap", d, maxRetryAfter)
	}
	if d := c.backoff(0, resp("soon")); d > c.maxBackoff {
		t.Errorf("invalid Retry-After: waited %v", d)
	}
	date := time.Now().Add(2 * time.Second).UTC().Format(http.TimeFormat)
	if d := c.backoff(0, resp(date)); d <= 0 || d > 2*time.Second {
		t.Errorf("Retry-After date: waited %v", d)
	}
}

// dropFirst closes the connection without a response on the first request.
func dropFirst(w http.ResponseWriter, _ *http.Request, n int) {
	if n == 1 {
		conn, _, err := w.(http.Hijacker).Hijack()
		if err == nil {
			conn.Close()
		}
		return
	}
	writeJSON(w, 200, map[string]any{"id": "x"})
}

func TestNetworkErrorRetryPolicy(t *testing.T) {
	t.Run("GET retried", func(t *testing.T) {
		fs := newFakeServer(t, dropFirst)
		if _, err := fs.client(t).Vaults.GetVaultAccount(context.Background(), "0"); err != nil {
			t.Fatal(err)
		}
		if n := len(fs.calls()); n != 2 {
			t.Errorf("calls = %d, want 2", n)
		}
	})
	t.Run("POST not retried", func(t *testing.T) {
		fs := newFakeServer(t, dropFirst)
		_, err := fs.client(t).Transactions.CreateTransaction(context.Background(), &TransactionRequest{}, nil)
		if err == nil {
			t.Fatal("expected a network error")
		}
		if _, ok := AsAPIError(err); ok {
			t.Fatalf("err = %v, want a transport error", err)
		}
		if n := len(fs.calls()); n != 1 {
			t.Errorf("calls = %d, want 1", n)
		}
	})
	t.Run("POST with idempotency key retried", func(t *testing.T) {
		fs := newFakeServer(t, dropFirst)
		_, err := fs.client(t).Transactions.CreateTransaction(context.Background(), &TransactionRequest{}, nil, WithIdempotencyKey("k"))
		if err != nil {
			t.Fatal(err)
		}
		calls := fs.calls()
		if len(calls) != 2 {
			t.Fatalf("calls = %d, want 2", len(calls))
		}
		if calls[0].Header.Get("Idempotency-Key") != "k" || calls[1].Header.Get("Idempotency-Key") != "k" {
			t.Error("idempotency key not sent on every attempt")
		}
	})
}

func TestIdempotencyKeyHeaderEnablesRetry(t *testing.T) {
	fs := newFakeServer(t, func(w http.ResponseWriter, r *http.Request, n int) {
		if n == 1 {
			writeJSON(w, 503, map[string]any{})
			return
		}
		writeJSON(w, 200, map[string]any{"id": "x"})
	})
	_, err := fs.client(t).Transactions.CreateTransaction(context.Background(), &TransactionRequest{}, nil, WithHeader("Idempotency-Key", "k"))
	if err != nil {
		t.Fatal(err)
	}
	if n := len(fs.calls()); n != 2 {
		t.Errorf("calls = %d, want 2", n)
	}
}

type failingSigner struct {
	crypto.Signer
	calls atomic.Int32
}

func (s *failingSigner) Sign(io.Reader, []byte, crypto.SignerOpts) ([]byte, error) {
	s.calls.Add(1)
	return nil, errors.New("kms unavailable")
}

func TestSignerErrorIsNotRetried(t *testing.T) {
	fs := newFakeServer(t, func(w http.ResponseWriter, r *http.Request, _ int) {
		writeJSON(w, 200, map[string]any{})
	})
	signer := &failingSigner{Signer: rsaKey(t)}
	c, err := NewClientWithSigner(testAPIKey, signer, WithBaseURL(fs.srv.URL), WithRetryBackoff(time.Millisecond, time.Millisecond))
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.Vaults.GetVaultAccount(context.Background(), "0")
	if err == nil || !strings.Contains(err.Error(), "kms unavailable") {
		t.Fatalf("err = %v", err)
	}
	if n := signer.calls.Load(); n != 1 {
		t.Errorf("Sign called %d times, want 1", n)
	}
	if n := len(fs.calls()); n != 0 {
		t.Errorf("server saw %d calls, want 0", n)
	}
}

type zeroReader struct{}

func (zeroReader) Read(p []byte) (int, error) {
	clear(p)
	return len(p), nil
}

func TestReadBodyLimit(t *testing.T) {
	if b, err := readBody(strings.NewReader("ok")); err != nil || string(b) != "ok" {
		t.Errorf("readBody = %q, %v", b, err)
	}
	if _, err := readBody(zeroReader{}); !errors.Is(err, errResponseTooLarge) {
		t.Errorf("oversized body: err = %v", err)
	}
}

func TestJWTURIMatchesWire(t *testing.T) {
	// verifyJWT (run by the fake server) checks that claims.uri equals the
	// request URI the server received; exercise characters that need escaping
	// in both the path and the query.
	fs := newFakeServer(t, func(w http.ResponseWriter, r *http.Request, _ int) {
		writeJSON(w, 200, map[string]any{})
	})
	_, err := fs.client(t).Transactions.ValidateAddress(context.Background(), "ETH TEST/5", "0x+ü?#%",
		WithQueryParam("q", "a b+c&d=é"))
	if err != nil {
		t.Fatal(err)
	}
	got := fs.calls()[0].Claims.URI
	want := "/v1/transactions/validate_address/ETH%20TEST%2F5/0x+%C3%BC%3F%23%25?q=a+b%2Bc%26d%3D%C3%A9"
	if got != want {
		t.Errorf("claims.uri = %q, want %q", got, want)
	}
}
