package fireblocks

import (
	"crypto"
	"crypto/rsa"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"runtime"
	"strings"
	"time"
)

// Version is the version of this SDK. It is sent in the User-Agent header.
const Version = "0.1.0"

// Base URLs of the Fireblocks API environments.
const (
	ProductionBaseURL = "https://api.fireblocks.io/v1"
	SandboxBaseURL    = "https://sandbox-api.fireblocks.io/v1"
	EUBaseURL         = "https://eu-api.fireblocks.io/v1"
	EU2BaseURL        = "https://eu2-api.fireblocks.io/v1"
)

const (
	defaultTimeout    = 60 * time.Second
	defaultMaxRetries = 2
	defaultMinBackoff = 500 * time.Millisecond
	defaultMaxBackoff = 8 * time.Second
)

// core holds the configuration and transport shared by every service.
type core struct {
	baseURL    *url.URL
	apiKey     string
	signer     *jwtSigner
	httpClient *http.Client
	userAgent  string
	maxRetries int
	minBackoff time.Duration
	maxBackoff time.Duration
	logger     *slog.Logger
}

type clientConfig struct {
	baseURL    string
	httpClient *http.Client
	userAgent  string
	maxRetries int
	minBackoff time.Duration
	maxBackoff time.Duration
	logger     *slog.Logger
	err        error
}

// ClientOption configures a Client.
type ClientOption func(*clientConfig)

// WithBaseURL sets the API base URL, e.g. SandboxBaseURL. When the URL has no
// path, "/v1" is appended. The default is ProductionBaseURL.
func WithBaseURL(baseURL string) ClientOption {
	return func(c *clientConfig) { c.baseURL = baseURL }
}

// WithHTTPClient sets the HTTP client used to send requests. The default is an
// http.Client with a 60 second timeout.
func WithHTTPClient(hc *http.Client) ClientOption {
	return func(c *clientConfig) {
		if hc == nil {
			c.err = errors.New("fireblocks: WithHTTPClient: nil client")
			return
		}
		c.httpClient = hc
	}
}

// WithUserAgent prepends ua to the SDK's User-Agent header.
func WithUserAgent(ua string) ClientOption {
	return func(c *clientConfig) { c.userAgent = ua }
}

// WithMaxRetries sets how many times a failed request is retried (default 2).
// Use 0 to disable retries. See the package documentation for the retry policy.
func WithMaxRetries(n int) ClientOption {
	return func(c *clientConfig) {
		if n < 0 {
			c.err = errors.New("fireblocks: WithMaxRetries: negative value")
			return
		}
		c.maxRetries = n
	}
}

// WithRetryBackoff sets the minimum and maximum wait between retries
// (default 500ms and 8s). The wait grows exponentially with jitter.
func WithRetryBackoff(minWait, maxWait time.Duration) ClientOption {
	return func(c *clientConfig) {
		if minWait <= 0 || maxWait < minWait {
			c.err = errors.New("fireblocks: WithRetryBackoff: invalid bounds")
			return
		}
		c.minBackoff, c.maxBackoff = minWait, maxWait
	}
}

// WithLogger enables debug logging of every request attempt (method, path,
// status, duration and request ID). Request and response bodies are never logged.
func WithLogger(l *slog.Logger) ClientOption {
	return func(c *clientConfig) { c.logger = l }
}

// NewClient returns a Client that authenticates with the given API key and the
// PEM-encoded RSA private key (the API user's secret key file).
func NewClient(apiKey string, privateKeyPEM []byte, opts ...ClientOption) (*Client, error) {
	key, err := ParseRSAPrivateKeyPEM(privateKeyPEM)
	if err != nil {
		return nil, err
	}
	return NewClientWithSigner(apiKey, key, opts...)
}

// NewClientWithSigner is like NewClient but takes a crypto.Signer, which lets the
// private key live in an HSM or KMS. The signer must hold an RSA key; it is
// asked for PKCS #1 v1.5 SHA-256 signatures (RS256).
func NewClientWithSigner(apiKey string, signer crypto.Signer, opts ...ClientOption) (*Client, error) {
	if strings.TrimSpace(apiKey) == "" {
		return nil, errors.New("fireblocks: API key is empty")
	}
	if signer == nil {
		return nil, errors.New("fireblocks: signer is nil")
	}
	if _, ok := signer.Public().(*rsa.PublicKey); !ok {
		return nil, fmt.Errorf("fireblocks: signer must hold an RSA key, got %T", signer.Public())
	}

	cfg := clientConfig{
		baseURL:    ProductionBaseURL,
		maxRetries: defaultMaxRetries,
		minBackoff: defaultMinBackoff,
		maxBackoff: defaultMaxBackoff,
	}
	for _, o := range opts {
		if o != nil {
			o(&cfg)
		}
	}
	if cfg.err != nil {
		return nil, cfg.err
	}
	base, err := parseBaseURL(cfg.baseURL)
	if err != nil {
		return nil, err
	}
	hc := cfg.httpClient
	if hc == nil {
		hc = &http.Client{Timeout: defaultTimeout}
	}
	ua := fmt.Sprintf("fireblocks-go/%s (%s; %s/%s)", Version, runtime.Version(), runtime.GOOS, runtime.GOARCH)
	if cfg.userAgent != "" {
		ua = cfg.userAgent + " " + ua
	}
	return newClient(&core{
		baseURL:    base,
		apiKey:     apiKey,
		signer:     &jwtSigner{apiKey: apiKey, key: signer, now: time.Now},
		httpClient: hc,
		userAgent:  ua,
		maxRetries: cfg.maxRetries,
		minBackoff: cfg.minBackoff,
		maxBackoff: cfg.maxBackoff,
		logger:     cfg.logger,
	}), nil
}

// Environment variables read by NewClientFromEnv.
const (
	envAPIKey        = "FIREBLOCKS_API_KEY"
	envSecretKey     = "FIREBLOCKS_SECRET_KEY"
	envSecretKeyPath = "FIREBLOCKS_SECRET_KEY_PATH"
	envBasePath      = "FIREBLOCKS_BASE_PATH"
)

// NewClientFromEnv builds a Client from environment variables:
//
//   - FIREBLOCKS_API_KEY (required)
//   - FIREBLOCKS_SECRET_KEY: the PEM private key itself, or
//     FIREBLOCKS_SECRET_KEY_PATH: a path to the PEM file
//   - FIREBLOCKS_BASE_PATH (optional): the API base URL
//
// Options passed explicitly take precedence over the environment.
func NewClientFromEnv(opts ...ClientOption) (*Client, error) {
	apiKey := os.Getenv(envAPIKey)
	if apiKey == "" {
		return nil, fmt.Errorf("fireblocks: %s is not set", envAPIKey)
	}
	var pemBytes []byte
	switch {
	case os.Getenv(envSecretKey) != "":
		pemBytes = []byte(os.Getenv(envSecretKey))
	case os.Getenv(envSecretKeyPath) != "":
		b, err := os.ReadFile(os.Getenv(envSecretKeyPath))
		if err != nil {
			return nil, fmt.Errorf("fireblocks: read %s: %w", envSecretKeyPath, err)
		}
		pemBytes = b
	default:
		return nil, fmt.Errorf("fireblocks: neither %s nor %s is set", envSecretKey, envSecretKeyPath)
	}
	if base := os.Getenv(envBasePath); base != "" {
		opts = append([]ClientOption{WithBaseURL(base)}, opts...)
	}
	return NewClient(apiKey, pemBytes, opts...)
}

func parseBaseURL(raw string) (*url.URL, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return nil, fmt.Errorf("fireblocks: invalid base URL: %w", err)
	}
	if u.Scheme != "https" && u.Scheme != "http" || u.Host == "" {
		return nil, fmt.Errorf("fireblocks: invalid base URL %q: want scheme://host[/path]", raw)
	}
	u.Path = strings.TrimRight(u.Path, "/")
	if u.Path == "" {
		u.Path = "/v1"
	}
	u.RawPath, u.RawQuery, u.Fragment = "", "", ""
	return u, nil
}
