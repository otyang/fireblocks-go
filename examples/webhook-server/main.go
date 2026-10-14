// Command webhook-server receives Fireblocks webhook notifications and
// verifies their signature before processing them.
//
//	FIREBLOCKS_WEBHOOK_PUBLIC_KEY_PATH=./fireblocks_webhook.pub go run ./examples/webhook-server
package main

import (
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"os"

	fireblocks "github.com/otyang/fireblocks-go"
)

func main() {
	pemBytes, err := os.ReadFile(os.Getenv("FIREBLOCKS_WEBHOOK_PUBLIC_KEY_PATH"))
	if err != nil {
		log.Fatal(err)
	}
	pub, err := fireblocks.ParseRSAPublicKeyPEM(pemBytes)
	if err != nil {
		log.Fatal(err)
	}

	http.HandleFunc("/webhook", func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
		if err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		if err := fireblocks.VerifyWebhookSignature(body, r.Header.Get("Fireblocks-Signature"), pub); err != nil {
			if errors.Is(err, fireblocks.ErrInvalidWebhookSignature) {
				http.Error(w, "invalid signature", http.StatusUnauthorized)
				return
			}
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		var event struct {
			Type string          `json:"type"`
			Data json.RawMessage `json:"data"`
		}
		if err := json.Unmarshal(body, &event); err != nil {
			http.Error(w, "bad payload", http.StatusBadRequest)
			return
		}
		log.Printf("received %s event", event.Type)
		w.WriteHeader(http.StatusOK)
	})
	log.Println("listening on :8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}
