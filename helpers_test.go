package fireblocks

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

const testAPIKey = "test-api-key"

var (
	keyOnce sync.Once
	testKey *rsa.PrivateKey
)

func rsaKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	keyOnce.Do(func() {
		k, err := rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			panic(err)
		}
		testKey = k
	})
	return testKey
}

func pkcs8PEM(t *testing.T) []byte {
	t.Helper()
	der, err := x509.MarshalPKCS8PrivateKey(rsaKey(t))
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})
}

// capturedRequest is what the fake Fireblocks server saw.
type capturedRequest struct {
	Method string
	URI    string
	Header http.Header
	Body   []byte
	Claims jwtClaims
}

// fakeServer verifies the JWT of every request and replies using handler.
type fakeServer struct {
	t        *testing.T
	srv      *httptest.Server
	mu       sync.Mutex
	requests []capturedRequest
}

func newFakeServer(t *testing.T, handler func(w http.ResponseWriter, r *http.Request, n int)) *fakeServer {
	t.Helper()
	fs := &fakeServer{t: t}
	fs.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		claims := verifyJWT(t, r, body)
		fs.mu.Lock()
		fs.requests = append(fs.requests, capturedRequest{r.Method, r.URL.RequestURI(), r.Header.Clone(), body, claims})
		n := len(fs.requests)
		fs.mu.Unlock()
		handler(w, r, n)
	}))
	t.Cleanup(fs.srv.Close)
	return fs
}

func (fs *fakeServer) client(t *testing.T, opts ...ClientOption) *Client {
	t.Helper()
	opts = append([]ClientOption{
		WithBaseURL(fs.srv.URL + "/v1"),
		WithRetryBackoff(time.Millisecond, 2*time.Millisecond),
	}, opts...)
	c, err := NewClient(testAPIKey, pkcs8PEM(t), opts...)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func (fs *fakeServer) calls() []capturedRequest {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	return append([]capturedRequest(nil), fs.requests...)
}

// verifyJWT checks the Fireblocks request-signing contract and returns the claims.
func verifyJWT(t *testing.T, r *http.Request, body []byte) jwtClaims {
	t.Helper()
	if got := r.Header.Get("X-API-Key"); got != testAPIKey {
		t.Errorf("X-API-Key = %q", got)
	}
	auth := r.Header.Get("Authorization")
	token, ok := strings.CutPrefix(auth, "Bearer ")
	if !ok {
		t.Errorf("Authorization header %q is not a bearer token", auth)
		return jwtClaims{}
	}
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Errorf("JWT has %d parts", len(parts))
		return jwtClaims{}
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		t.Errorf("decode signature: %v", err)
	}
	digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if err := rsa.VerifyPKCS1v15(&testKey.PublicKey, crypto.SHA256, digest[:], sig); err != nil {
		t.Errorf("JWT signature invalid: %v", err)
	}
	var header map[string]string
	hb, _ := base64.RawURLEncoding.DecodeString(parts[0])
	_ = json.Unmarshal(hb, &header)
	if header["alg"] != "RS256" || header["typ"] != "JWT" {
		t.Errorf("JWT header = %v", header)
	}
	var claims jwtClaims
	cb, _ := base64.RawURLEncoding.DecodeString(parts[1])
	if err := json.Unmarshal(cb, &claims); err != nil {
		t.Errorf("decode claims: %v", err)
	}
	if claims.URI != r.URL.RequestURI() {
		t.Errorf("claims.uri = %q, request URI = %q", claims.URI, r.URL.RequestURI())
	}
	if claims.Sub != testAPIKey {
		t.Errorf("claims.sub = %q", claims.Sub)
	}
	sum := sha256.Sum256(body)
	if claims.BodyHash != hex.EncodeToString(sum[:]) {
		t.Errorf("claims.bodyHash does not match the body")
	}
	if claims.Exp <= claims.Iat || claims.Exp-claims.Iat >= 30 {
		t.Errorf("exp-iat = %d, want 0 < x < 30", claims.Exp-claims.Iat)
	}
	if claims.Nonce == "" {
		t.Error("claims.nonce is empty")
	}
	return claims
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
