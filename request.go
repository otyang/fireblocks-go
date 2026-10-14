package fireblocks

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// maxResponseBytes bounds how much of a response body is read into memory.
const maxResponseBytes = 64 << 20

// maxRetryAfter caps how long a server-provided Retry-After is honoured.
const maxRetryAfter = time.Minute

// request is an API call assembled by a generated service method.
type request struct {
	method    string
	path      string
	query     url.Values
	headers   http.Header
	body      any // JSON-encoded when non-nil
	multipart any // multipart/form-data encoded when non-nil
	err       error
}

func newRequest(method, path string) *request {
	return &request{method: method, path: path, query: url.Values{}, headers: http.Header{}}
}

// pathParam formats and escapes a path parameter, recording an error when it is empty.
func (r *request) pathParam(name string, v any) string {
	s := formatValue(v)
	if s == "" && r.err == nil {
		r.err = fmt.Errorf("%w %q", ErrMissingParameter, name)
	}
	return url.PathEscape(s)
}

// queryValue adds v to the query, repeating the key for each element of a slice.
// Nil pointers and empty slices are skipped.
func (r *request) queryValue(name string, v any) {
	for _, s := range formatValues(v) {
		r.query.Add(name, s)
	}
}

// queryCSV adds the elements of v to the query as one comma-separated value.
func (r *request) queryCSV(name string, v any) {
	if vals := formatValues(v); len(vals) > 0 {
		r.query.Set(name, strings.Join(vals, ","))
	}
}

func (r *request) header(name string, v any) {
	if vals := formatValues(v); len(vals) > 0 {
		r.headers.Set(name, strings.Join(vals, ","))
	}
}

// RequestOption customises a single API call.
type RequestOption func(*requestConfig)

type requestConfig struct {
	header         http.Header
	query          url.Values
	idempotencyKey string
	maxRetries     *int
	onResponse     []func(*http.Response)
}

// WithIdempotencyKey sets the Idempotency-Key header. Fireblocks uses it to
// deduplicate requests, which also makes retrying POST and PATCH requests safe.
// An empty key is ignored. Use a new key for every distinct operation: reusing
// one returns the response of the first request.
func WithIdempotencyKey(key string) RequestOption {
	return func(c *requestConfig) { c.idempotencyKey = key }
}

// WithHeader sets an additional HTTP header on the request.
func WithHeader(key, value string) RequestOption {
	return func(c *requestConfig) { c.header.Set(key, value) }
}

// WithQueryParam sets an additional query parameter, e.g. one the
// specification does not describe yet.
func WithQueryParam(key, value string) RequestOption {
	return func(c *requestConfig) { c.query.Set(key, value) }
}

// WithRetries overrides the client's maximum number of retries for this call.
// Negative values are ignored.
func WithRetries(n int) RequestOption {
	return func(c *requestConfig) {
		if n >= 0 {
			c.maxRetries = &n
		}
	}
}

// OnResponse registers fn to be called with the final HTTP response, before
// the body is decoded. Use it to read headers such as X-Request-ID or
// next-page. The response body can be read again inside fn.
func OnResponse(fn func(*http.Response)) RequestOption {
	return func(c *requestConfig) {
		if fn != nil {
			c.onResponse = append(c.onResponse, fn)
		}
	}
}

// isIdempotent reports whether an HTTP method can be retried without risk of
// performing an operation twice.
func isIdempotent(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions, http.MethodPut, http.MethodDelete:
		return true
	}
	return false
}

// do sends r, retrying according to the retry policy, and decodes a successful
// JSON response into out (when out is non-nil).
func (c *core) do(ctx context.Context, r *request, out any, opts []RequestOption) error {
	if r.err != nil {
		return r.err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	cfg := requestConfig{header: http.Header{}, query: url.Values{}}
	for _, o := range opts {
		if o != nil {
			o(&cfg)
		}
	}
	if cfg.idempotencyKey == "" {
		// An Idempotency-Key set with WithHeader counts as well.
		cfg.idempotencyKey = cfg.header.Get("Idempotency-Key")
	}
	maxRetries := c.maxRetries
	if cfg.maxRetries != nil {
		maxRetries = *cfg.maxRetries
	}

	var payload []byte
	contentType := ""
	switch {
	case r.multipart != nil:
		var err error
		if payload, contentType, err = encodeMultipart(r.multipart); err != nil {
			return fmt.Errorf("fireblocks: encode multipart body: %w", err)
		}
	case r.body != nil:
		var err error
		if payload, err = json.Marshal(r.body); err != nil {
			return fmt.Errorf("fireblocks: encode request body: %w", err)
		}
		contentType = "application/json"
	}

	// r.path is already escaped, so the URL is assembled as a string.
	q := r.query
	for k, vs := range cfg.query {
		q[k] = vs
	}
	fullURL := c.baseURL.String() + r.path
	if enc := q.Encode(); enc != "" {
		fullURL += "?" + enc
	}

	retrySafe := isIdempotent(r.method) || cfg.idempotencyKey != ""
	for attempt := 0; ; attempt++ {
		req, err := c.newHTTPRequest(ctx, r, fullURL, payload, contentType, &cfg)
		if err != nil {
			// Building or signing the request failed; repeating it would not help.
			return err
		}
		start := time.Now()
		resp, err := c.httpClient.Do(req)
		if err != nil {
			c.log(ctx, r, attempt, 0, time.Since(start), "", err)
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if retrySafe && attempt < maxRetries {
				if werr := sleep(ctx, c.backoff(attempt, nil)); werr != nil {
					return werr
				}
				continue
			}
			return fmt.Errorf("fireblocks: %s %s: %w", r.method, r.path, err)
		}

		body, readErr := readBody(resp.Body)
		resp.Body.Close()
		requestID := resp.Header.Get("X-Request-ID")
		c.log(ctx, r, attempt, resp.StatusCode, time.Since(start), requestID, readErr)

		if attempt < maxRetries && shouldRetryStatus(resp.StatusCode, retrySafe) {
			if werr := sleep(ctx, c.backoff(attempt, resp)); werr != nil {
				return werr
			}
			continue
		}
		if readErr != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return fmt.Errorf("fireblocks: %s %s: read response (status %d, request id %s): %w", r.method, r.path, resp.StatusCode, requestID, readErr)
		}

		for _, fn := range cfg.onResponse {
			resp.Body = io.NopCloser(bytes.NewReader(body))
			fn(resp)
		}
		if resp.StatusCode < 200 || resp.StatusCode > 299 {
			return newAPIError(resp, body)
		}
		if out != nil && len(bytes.TrimSpace(body)) > 0 {
			if err := json.Unmarshal(body, out); err != nil {
				return fmt.Errorf("fireblocks: %s %s: decode response (request id %s): %w", r.method, r.path, requestID, err)
			}
		}
		return nil
	}
}

// errResponseTooLarge is returned when a response body exceeds maxResponseBytes.
var errResponseTooLarge = fmt.Errorf("response body exceeds %d bytes", maxResponseBytes)

// readBody reads at most maxResponseBytes, failing instead of silently
// truncating a larger body.
func readBody(rc io.Reader) ([]byte, error) {
	body, err := io.ReadAll(io.LimitReader(rc, maxResponseBytes+1))
	if err == nil && len(body) > maxResponseBytes {
		return nil, errResponseTooLarge
	}
	return body, err
}

// newHTTPRequest builds and signs one attempt of r.
func (c *core) newHTTPRequest(ctx context.Context, r *request, fullURL string, payload []byte, contentType string, cfg *requestConfig) (*http.Request, error) {
	var body io.Reader
	if payload != nil {
		body = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, r.method, fullURL, body)
	if err != nil {
		return nil, fmt.Errorf("fireblocks: %s %s: %w", r.method, r.path, err)
	}
	// The JWT is bound to the exact URI (path + query) and body being sent,
	// so it is regenerated for every attempt.
	token, err := c.signer.token(req.URL.RequestURI(), payload)
	if err != nil {
		return nil, err
	}
	for k, vs := range r.headers {
		req.Header[k] = vs
	}
	for k, vs := range cfg.header {
		req.Header[k] = vs
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", c.userAgent)
	req.Header.Set("X-API-Key", c.apiKey)
	req.Header.Set("Authorization", "Bearer "+token)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	if cfg.idempotencyKey != "" {
		req.Header.Set("Idempotency-Key", cfg.idempotencyKey)
	}
	return req, nil
}

// shouldRetryStatus implements the retry policy: 429 is always retried because
// the request was rejected before processing; 5xx gateway-style errors only
// when repeating the request is safe.
func shouldRetryStatus(status int, retrySafe bool) bool {
	switch status {
	case http.StatusTooManyRequests:
		return true
	case http.StatusInternalServerError, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return retrySafe
	}
	return false
}

func (c *core) backoff(attempt int, resp *http.Response) time.Duration {
	if resp != nil {
		if ra := parseRetryAfter(resp.Header.Get("Retry-After")); ra > 0 {
			return min(ra, maxRetryAfter)
		}
	}
	// Double minBackoff per attempt without overflowing: a plain shift wraps
	// around for large attempt counts and can yield tiny or negative waits.
	d := c.minBackoff
	for i := 0; i < attempt && d < c.maxBackoff; i++ {
		d *= 2
	}
	if d <= 0 || d > c.maxBackoff {
		d = c.maxBackoff
	}
	// Equal jitter: wait between d/2 and d.
	half := d / 2
	return half + time.Duration(rand.Int64N(int64(half)+1))
}

func parseRetryAfter(v string) time.Duration {
	if v == "" {
		return 0
	}
	if secs, err := strconv.Atoi(v); err == nil && secs >= 0 {
		return time.Duration(secs) * time.Second
	}
	if t, err := http.ParseTime(v); err == nil {
		return time.Until(t)
	}
	return 0
}

func sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

func (c *core) log(ctx context.Context, r *request, attempt, status int, dur time.Duration, requestID string, err error) {
	if c.logger == nil {
		return
	}
	attrs := []slog.Attr{
		slog.String("method", r.method),
		slog.String("path", r.path),
		slog.Int("attempt", attempt+1),
		slog.Duration("duration", dur),
	}
	if status != 0 {
		attrs = append(attrs, slog.Int("status", status))
	}
	if requestID != "" {
		attrs = append(attrs, slog.String("request_id", requestID))
	}
	if err != nil {
		attrs = append(attrs, slog.String("error", err.Error()))
	}
	c.logger.LogAttrs(ctx, slog.LevelDebug, "fireblocks request", attrs...)
}
