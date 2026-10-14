package fireblocks

import (
	"bytes"
	"context"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func TestGetVaultAccount(t *testing.T) {
	fs := newFakeServer(t, func(w http.ResponseWriter, r *http.Request, _ int) {
		w.Header().Set("X-Request-ID", "req-1")
		writeJSON(w, 200, map[string]any{"id": "0", "name": "Treasury", "hiddenOnUI": false})
	})
	c := fs.client(t)

	var seenRequestID string
	acct, err := c.Vaults.GetVaultAccount(context.Background(), "0", OnResponse(func(r *http.Response) {
		seenRequestID = r.Header.Get("X-Request-ID")
	}))
	if err != nil {
		t.Fatal(err)
	}
	if acct.ID == nil || *acct.ID != "0" || acct.Name == nil || *acct.Name != "Treasury" {
		t.Fatalf("unexpected account %+v", acct)
	}
	if seenRequestID != "req-1" {
		t.Errorf("OnResponse saw request id %q", seenRequestID)
	}
	call := fs.calls()[0]
	if call.Method != http.MethodGet || call.URI != "/v1/vault/accounts/0" {
		t.Errorf("got %s %s", call.Method, call.URI)
	}
	if ua := call.Header.Get("User-Agent"); !strings.HasPrefix(ua, "fireblocks-go/"+Version) {
		t.Errorf("User-Agent = %q", ua)
	}
	if len(call.Body) != 0 {
		t.Errorf("GET sent a body: %q", call.Body)
	}
}

func TestQueryParamsAndEscaping(t *testing.T) {
	fs := newFakeServer(t, func(w http.ResponseWriter, r *http.Request, _ int) {
		writeJSON(w, 200, map[string]any{"accounts": []any{}})
	})
	c := fs.client(t)
	_, err := c.Vaults.GetPagedVaultAccounts(context.Background(), &GetPagedVaultAccountsParams{
		NamePrefix: Ptr("a b&c"),
		Limit:      Ptr(10.0),
		OrderBy:    Ptr(GetPagedVaultAccountsOrderBy("DESC")),
		TagIDs:     []string{"t1", "t2"},
	}, WithQueryParam("extra", "1"))
	if err != nil {
		t.Fatal(err)
	}
	uri := fs.calls()[0].URI
	for _, want := range []string{"namePrefix=a+b%26c", "limit=10", "orderBy=DESC", "tagIds=t1", "tagIds=t2", "extra=1"} {
		if !strings.Contains(uri, want) {
			t.Errorf("URI %q does not contain %q", uri, want)
		}
	}
	if strings.Contains(uri, "before=") {
		t.Errorf("unset optional parameter was sent: %q", uri)
	}
}

func TestPathParamEscapingAndValidation(t *testing.T) {
	fs := newFakeServer(t, func(w http.ResponseWriter, r *http.Request, _ int) {
		writeJSON(w, 200, map[string]any{})
	})
	c := fs.client(t)
	if _, err := c.Transactions.GetTransaction(context.Background(), "a/b c"); err != nil {
		t.Fatal(err)
	}
	if got := fs.calls()[0].URI; got != "/v1/transactions/a%2Fb%20c" {
		t.Errorf("URI = %q", got)
	}

	_, err := c.Vaults.GetVaultAccount(context.Background(), "")
	if !errors.Is(err, ErrMissingParameter) {
		t.Fatalf("err = %v, want ErrMissingParameter", err)
	}
	if len(fs.calls()) != 1 {
		t.Error("request with a missing path parameter was sent")
	}
}

func TestCreateTransactionBody(t *testing.T) {
	fs := newFakeServer(t, func(w http.ResponseWriter, r *http.Request, _ int) {
		writeJSON(w, 200, map[string]any{"id": "tx-1", "status": "SUBMITTED"})
	})
	c := fs.client(t)
	resp, err := c.Transactions.CreateTransaction(context.Background(), &TransactionRequest{
		AssetID: Ptr("BTC_TEST"),
		Amount:  NewTransactionRequestAmountFromString("0.001"),
		Note:    Ptr("test"),
	}, &CreateTransactionParams{XEndUserWalletID: Ptr("wallet-1")}, WithIdempotencyKey("idem-1"))
	if err != nil {
		t.Fatal(err)
	}
	if resp.ID == nil || *resp.ID != "tx-1" {
		t.Errorf("resp = %+v", resp)
	}
	call := fs.calls()[0]
	if call.Header.Get("Content-Type") != "application/json" {
		t.Errorf("Content-Type = %q", call.Header.Get("Content-Type"))
	}
	if call.Header.Get("Idempotency-Key") != "idem-1" || call.Header.Get("X-End-User-Wallet-Id") != "wallet-1" {
		t.Errorf("headers = %v", call.Header)
	}
	var got map[string]any
	if err := json.Unmarshal(call.Body, &got); err != nil {
		t.Fatal(err)
	}
	if got["assetId"] != "BTC_TEST" || got["amount"] != "0.001" || got["note"] != "test" {
		t.Errorf("body = %s", call.Body)
	}
	if _, ok := got["source"]; ok {
		t.Error("unset optional field was serialized")
	}
}

func TestAPIError(t *testing.T) {
	fs := newFakeServer(t, func(w http.ResponseWriter, r *http.Request, _ int) {
		w.Header().Set("X-Request-ID", "req-404")
		writeJSON(w, 404, map[string]any{"message": "Vault account not found", "code": 11001})
	})
	_, err := fs.client(t).Vaults.GetVaultAccount(context.Background(), "99")
	apiErr, ok := AsAPIError(err)
	if !ok {
		t.Fatalf("err = %T %v", err, err)
	}
	if apiErr.StatusCode != 404 || apiErr.Code != 11001 || apiErr.Message != "Vault account not found" || apiErr.RequestID != "req-404" {
		t.Errorf("apiErr = %+v", apiErr)
	}
	if !IsNotFound(err) || IsUnauthorized(err) {
		t.Error("status helpers disagree with the status code")
	}
	if !strings.Contains(err.Error(), "code 11001") {
		t.Errorf("Error() = %q", err.Error())
	}
}

func TestAPIErrorNestedShape(t *testing.T) {
	resp := &http.Response{StatusCode: 400, Header: http.Header{}}
	e := newAPIError(resp, []byte(`{"error":{"type":"VALIDATION","message":"bad input"}}`))
	if e.Type != "VALIDATION" || e.Message != "bad input" {
		t.Errorf("e = %+v", e)
	}
	e = newAPIError(resp, []byte("upstream exploded"))
	if e.Message != "upstream exploded" {
		t.Errorf("non-JSON message = %q", e.Message)
	}
	e = newAPIError(resp, []byte(`"Unauthorized"`))
	if e.Message != "Unauthorized" {
		t.Errorf("JSON string message = %q", e.Message)
	}
	e = newAPIError(resp, []byte(`{"message":"m","error":"bad","errorCode":"42"}`))
	if e.Message != "m" || e.Code != 42 {
		t.Errorf("e = %+v", e)
	}
	long := strings.Repeat("é", 400) // 800 bytes
	e = newAPIError(resp, []byte(long))
	if len(e.Message) > maxErrorMessageLen || !utf8.ValidString(e.Message) {
		t.Errorf("long message: %d bytes, valid UTF-8 %v", len(e.Message), utf8.ValidString(e.Message))
	}
}

func TestRetryPolicy(t *testing.T) {
	tests := []struct {
		name      string
		status    int
		call      func(c *Client) error
		wantCalls int
	}{
		{"GET retried on 503", 503, func(c *Client) error {
			_, err := c.Vaults.GetVaultAccount(context.Background(), "0")
			return err
		}, 2},
		{"POST not retried on 503", 503, func(c *Client) error {
			_, err := c.Transactions.CreateTransaction(context.Background(), &TransactionRequest{}, nil)
			return err
		}, 1},
		{"POST with idempotency key retried on 503", 503, func(c *Client) error {
			_, err := c.Transactions.CreateTransaction(context.Background(), &TransactionRequest{}, nil, WithIdempotencyKey("k"))
			return err
		}, 2},
		{"POST retried on 429", 429, func(c *Client) error {
			_, err := c.Transactions.CreateTransaction(context.Background(), &TransactionRequest{}, nil)
			return err
		}, 2},
		{"GET not retried on 400", 400, func(c *Client) error {
			_, err := c.Vaults.GetVaultAccount(context.Background(), "0")
			return err
		}, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fs := newFakeServer(t, func(w http.ResponseWriter, r *http.Request, n int) {
				if n == 1 {
					writeJSON(w, tt.status, map[string]any{"message": "try again"})
					return
				}
				writeJSON(w, 200, map[string]any{"id": "x"})
			})
			_ = tt.call(fs.client(t))
			calls := fs.calls()
			if len(calls) != tt.wantCalls {
				t.Fatalf("server saw %d calls, want %d", len(calls), tt.wantCalls)
			}
			if len(calls) == 2 && calls[0].Claims.Nonce == calls[1].Claims.Nonce {
				t.Error("retry reused the JWT nonce")
			}
		})
	}
}

func TestRetriesExhausted(t *testing.T) {
	fs := newFakeServer(t, func(w http.ResponseWriter, r *http.Request, _ int) {
		writeJSON(w, 503, map[string]any{"message": "down"})
	})
	_, err := fs.client(t, WithMaxRetries(3)).Vaults.GetVaultAccount(context.Background(), "0")
	if e, ok := AsAPIError(err); !ok || e.StatusCode != 503 {
		t.Fatalf("err = %v", err)
	}
	if n := len(fs.calls()); n != 4 {
		t.Errorf("calls = %d, want 4", n)
	}

	fs2 := newFakeServer(t, func(w http.ResponseWriter, r *http.Request, _ int) {
		writeJSON(w, 503, map[string]any{})
	})
	_, _ = fs2.client(t).Vaults.GetVaultAccount(context.Background(), "0", WithRetries(0))
	if n := len(fs2.calls()); n != 1 {
		t.Errorf("WithRetries(0): calls = %d, want 1", n)
	}
}

func TestContextCancellation(t *testing.T) {
	fs := newFakeServer(t, func(w http.ResponseWriter, r *http.Request, _ int) {
		writeJSON(w, 503, map[string]any{})
	})
	c := fs.client(t, WithRetryBackoff(time.Hour, time.Hour))
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err := c.Vaults.GetVaultAccount(ctx, "0")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want deadline exceeded", err)
	}
}

func TestMultipartUpload(t *testing.T) {
	fs := newFakeServer(t, func(w http.ResponseWriter, r *http.Request, _ int) {
		w.WriteHeader(202)
	})
	err := fs.client(t).Trading.UploadOrderRequirementFile(context.Background(), "order-1", &OrderRequirementFileUpload{
		FileKey: "passport",
		File:    &File{Name: "id.pdf", ContentType: "application/pdf", Content: strings.NewReader("%PDF-1.7")},
	})
	if err != nil {
		t.Fatal(err)
	}
	call := fs.calls()[0]
	mediaType, params, err := mime.ParseMediaType(call.Header.Get("Content-Type"))
	if err != nil || mediaType != "multipart/form-data" {
		t.Fatalf("Content-Type = %q", call.Header.Get("Content-Type"))
	}
	mr := multipart.NewReader(bytes.NewReader(call.Body), params["boundary"])
	parts := map[string]string{}
	for {
		p, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(p)
		parts[p.FormName()] = string(b)
		if p.FormName() == "file" && p.FileName() != "id.pdf" {
			t.Errorf("file name = %q", p.FileName())
		}
	}
	if parts["fileKey"] != "passport" || parts["file"] != "%PDF-1.7" {
		t.Errorf("parts = %v", parts)
	}
}

func TestUnionRoundTrip(t *testing.T) {
	var a AccessType
	if err := json.Unmarshal([]byte(`{"type":"PROVIDER","providerId":"p1"}`), &a); err != nil {
		t.Fatal(err)
	}
	d, err := a.Discriminator()
	if err != nil || d != "PROVIDER" {
		t.Fatalf("Discriminator() = %q, %v", d, err)
	}
	v, err := a.ValueByDiscriminator()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := v.(DirectAccess); !ok {
		t.Errorf("ValueByDiscriminator() = %T, want DirectAccess", v)
	}
	out, err := json.Marshal(a)
	if err != nil || !strings.Contains(string(out), `"providerId":"p1"`) {
		t.Errorf("re-encoded union = %s, %v", out, err)
	}

	var empty TransactionRequestAmount
	if _, err := empty.AsString(); err == nil {
		t.Error("AsString on an empty union should fail")
	}
	if b, _ := json.Marshal(empty); string(b) != "null" {
		t.Errorf("empty union marshals to %s", b)
	}
}

func TestNewClientValidation(t *testing.T) {
	if _, err := NewClient("", pkcs8PEM(t)); err == nil {
		t.Error("empty API key accepted")
	}
	if _, err := NewClient("k", []byte("not a key")); err == nil {
		t.Error("invalid PEM accepted")
	}
	if _, err := NewClient("k", pkcs8PEM(t), WithBaseURL("ftp://x")); err == nil {
		t.Error("invalid base URL accepted")
	}
	if _, err := NewClient("k", pkcs8PEM(t), WithMaxRetries(-1)); err == nil {
		t.Error("negative retries accepted")
	}
}

func TestParseBaseURL(t *testing.T) {
	for in, want := range map[string]string{
		"https://api.fireblocks.io":             "https://api.fireblocks.io/v1",
		"https://api.fireblocks.io/":            "https://api.fireblocks.io/v1",
		"https://sandbox-api.fireblocks.io/v1/": "https://sandbox-api.fireblocks.io/v1",
		"http://localhost:8080/proxy/v1":        "http://localhost:8080/proxy/v1",
	} {
		u, err := parseBaseURL(in)
		if err != nil || u.String() != want {
			t.Errorf("parseBaseURL(%q) = %v, %v; want %s", in, u, err, want)
		}
	}
}

func TestPrivateKeyFormats(t *testing.T) {
	k := rsaKey(t)
	pkcs1 := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(k)})
	escaped := []byte(strings.ReplaceAll(string(pkcs8PEM(t)), "\n", `\n`))
	for name, data := range map[string][]byte{"pkcs1": pkcs1, "pkcs8": pkcs8PEM(t), "escaped newlines": escaped} {
		got, err := ParseRSAPrivateKeyPEM(data)
		if err != nil || !got.Equal(k) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestNewClientFromEnv(t *testing.T) {
	t.Setenv(envAPIKey, testAPIKey)
	t.Setenv(envSecretKey, string(pkcs8PEM(t)))
	t.Setenv(envBasePath, SandboxBaseURL)
	c, err := NewClientFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if c.core.baseURL.String() != SandboxBaseURL {
		t.Errorf("base URL = %s", c.core.baseURL)
	}
	t.Setenv(envAPIKey, "")
	if _, err := NewClientFromEnv(); err == nil {
		t.Error("missing API key accepted")
	}
}

func TestFormatValues(t *testing.T) {
	type enum string
	for _, tt := range []struct {
		in   any
		want []string
	}{
		{(*string)(nil), nil},
		{Ptr("x"), []string{"x"}},
		{Ptr(true), []string{"true"}},
		{Ptr(int64(42)), []string{"42"}},
		{Ptr(1.5), []string{"1.5"}},
		{[]string{}, []string{}},
		{[]enum{"A", "B"}, []string{"A", "B"}},
		{Ptr(enum("C")), []string{"C"}},
		{NewTransactionRequestAmountFromString("7"), []string{"7"}},
		{Ptr(float32(0.1)), []string{"0.1"}},
		{[]float64{1, 2.5}, []string{"1", "2.5"}},
		{struct {
			A int `json:"a"`
		}{1}, []string{`{"a":1}`}},
		{map[string]string{"k": "v"}, []string{`{"k":"v"}`}},
	} {
		got := formatValues(tt.in)
		if strings.Join(got, "|") != strings.Join(tt.want, "|") || len(got) != len(tt.want) {
			t.Errorf("formatValues(%#v) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
