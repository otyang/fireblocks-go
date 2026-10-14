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
	"errors"
	"fmt"
	"strings"
	"time"
)

// tokenLifetime keeps exp strictly below the iat+30s limit Fireblocks enforces.
const tokenLifetime = 29 * time.Second

// jwtSigner creates the per-request JWT that Fireblocks requires in the
// Authorization header. See https://developers.fireblocks.com/reference/signing-a-request-jwt-structure.
type jwtSigner struct {
	apiKey string
	key    crypto.Signer
	now    func() time.Time
}

type jwtClaims struct {
	URI      string `json:"uri"`
	Nonce    string `json:"nonce"`
	Iat      int64  `json:"iat"`
	Exp      int64  `json:"exp"`
	Sub      string `json:"sub"`
	BodyHash string `json:"bodyHash"`
}

var jwtHeader = base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","typ":"JWT"}`))

// token signs a JWT bound to the request URI (path and query) and body.
func (s *jwtSigner) token(uri string, body []byte) (string, error) {
	nonce, err := randomHex(16)
	if err != nil {
		return "", err
	}
	bodyHash := sha256.Sum256(body)
	now := s.now()
	claims, err := json.Marshal(jwtClaims{
		URI:      uri,
		Nonce:    nonce,
		Iat:      now.Unix(),
		Exp:      now.Add(tokenLifetime).Unix(),
		Sub:      s.apiKey,
		BodyHash: hex.EncodeToString(bodyHash[:]),
	})
	if err != nil {
		return "", err
	}
	signingInput := jwtHeader + "." + base64.RawURLEncoding.EncodeToString(claims)
	digest := sha256.Sum256([]byte(signingInput))
	sig, err := s.key.Sign(rand.Reader, digest[:], crypto.SHA256)
	if err != nil {
		return "", fmt.Errorf("fireblocks: sign request: %w", err)
	}
	return signingInput + "." + base64.RawURLEncoding.EncodeToString(sig), nil
}

func randomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("fireblocks: generate nonce: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// decodePEM finds the first PEM block, tolerating keys whose newlines were
// escaped as "\n" (common when keys are stored in environment variables).
func decodePEM(data []byte) (*pem.Block, error) {
	block, _ := pem.Decode(data)
	if block == nil && strings.Contains(string(data), `\n`) {
		block, _ = pem.Decode([]byte(strings.ReplaceAll(string(data), `\n`, "\n")))
	}
	if block == nil {
		return nil, errors.New("fireblocks: no PEM data found")
	}
	return block, nil
}

// ParseRSAPrivateKeyPEM parses a PEM-encoded RSA private key in PKCS #1
// ("RSA PRIVATE KEY") or PKCS #8 ("PRIVATE KEY") form.
func ParseRSAPrivateKeyPEM(data []byte) (*rsa.PrivateKey, error) {
	block, err := decodePEM(data)
	if err != nil {
		return nil, err
	}
	if k, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return k, nil
	}
	k, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("fireblocks: parse private key: %w", err)
	}
	rk, ok := k.(*rsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("fireblocks: private key is %T, want RSA", k)
	}
	return rk, nil
}

// ParseRSAPublicKeyPEM parses a PEM-encoded RSA public key in PKIX
// ("PUBLIC KEY") or PKCS #1 ("RSA PUBLIC KEY") form.
func ParseRSAPublicKeyPEM(data []byte) (*rsa.PublicKey, error) {
	block, err := decodePEM(data)
	if err != nil {
		return nil, err
	}
	if k, err := x509.ParsePKCS1PublicKey(block.Bytes); err == nil {
		return k, nil
	}
	k, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("fireblocks: parse public key: %w", err)
	}
	rk, ok := k.(*rsa.PublicKey)
	if !ok {
		return nil, fmt.Errorf("fireblocks: public key is %T, want RSA", k)
	}
	return rk, nil
}
