package fireblocks

import (
	"crypto"
	"crypto/rsa"
	"crypto/sha512"
	"encoding/base64"
	"errors"
	"strings"
)

// ErrInvalidWebhookSignature is returned by VerifyWebhookSignature when the
// signature does not match the payload.
var ErrInvalidWebhookSignature = errors.New("fireblocks: invalid webhook signature")

// VerifyWebhookSignature checks the Fireblocks-Signature header of a (v1)
// webhook notification. body must be the raw request body, signature the
// base64 header value and publicKey the Fireblocks webhook public key for your
// environment (see https://developers.fireblocks.com/docs/webhooks-notifications).
// Parse the published PEM key once with ParseRSAPublicKeyPEM.
func VerifyWebhookSignature(body []byte, signature string, publicKey *rsa.PublicKey) error {
	if publicKey == nil {
		return errors.New("fireblocks: webhook public key is nil")
	}
	sig, err := base64.StdEncoding.DecodeString(strings.TrimSpace(signature))
	if err != nil {
		return ErrInvalidWebhookSignature
	}
	digest := sha512.Sum512(body)
	if rsa.VerifyPKCS1v15(publicKey, crypto.SHA512, digest[:], sig) != nil {
		return ErrInvalidWebhookSignature
	}
	return nil
}
