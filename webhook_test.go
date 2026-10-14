package fireblocks

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha512"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"testing"
)

func TestVerifyWebhookSignature(t *testing.T) {
	k := rsaKey(t)
	body := []byte(`{"type":"TRANSACTION_CREATED","data":{"id":"tx-1"}}`)
	digest := sha512.Sum512(body)
	sig, err := rsa.SignPKCS1v15(rand.Reader, k, crypto.SHA512, digest[:])
	if err != nil {
		t.Fatal(err)
	}
	der, _ := x509.MarshalPKIXPublicKey(&k.PublicKey)
	pub, err := ParseRSAPublicKeyPEM(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}))
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifyWebhookSignature(body, base64.StdEncoding.EncodeToString(sig), pub); err != nil {
		t.Errorf("valid signature rejected: %v", err)
	}
	tampered := append([]byte{}, body...)
	tampered[10] ^= 1
	if err := VerifyWebhookSignature(tampered, base64.StdEncoding.EncodeToString(sig), pub); !errors.Is(err, ErrInvalidWebhookSignature) {
		t.Errorf("tampered body: err = %v", err)
	}
	if err := VerifyWebhookSignature(body, "!!not base64!!", pub); !errors.Is(err, ErrInvalidWebhookSignature) {
		t.Errorf("garbage signature: err = %v", err)
	}
}
