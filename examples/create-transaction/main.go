// Command create-transaction transfers an amount between two vault accounts
// and polls the transaction until it reaches a final status.
//
//	go run ./examples/create-transaction -asset ETH_TEST5 -amount 0.001 -from 0 -to 1
package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"flag"
	"fmt"
	"log"
	"time"

	fireblocks "github.com/otyang/fireblocks-go"
)

func main() {
	asset := flag.String("asset", "ETH_TEST5", "asset ID")
	amount := flag.String("amount", "0.001", "amount to transfer")
	from := flag.String("from", "0", "source vault account ID")
	to := flag.String("to", "1", "destination vault account ID")
	flag.Parse()

	client, err := fireblocks.NewClientFromEnv()
	if err != nil {
		log.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	// A unique idempotency key lets the SDK retry the POST safely on transient
	// errors without risking a duplicate transfer.
	key := make([]byte, 16)
	if _, err := rand.Read(key); err != nil {
		log.Fatal(err)
	}

	created, err := client.Transactions.CreateTransaction(ctx, &fireblocks.TransactionRequest{
		AssetID: asset,
		Amount:  fireblocks.NewTransactionRequestAmountFromString(*amount),
		Source: &fireblocks.SourceTransferPeerPath{
			Type: fireblocks.TransferPeerPathTypeVaultAccount,
			ID:   from,
		},
		Destination: &fireblocks.DestinationTransferPeerPath{
			Type: fireblocks.TransferPeerPathTypeVaultAccount,
			ID:   to,
		},
		Note: fireblocks.Ptr("fireblocks-go example"),
	}, nil, fireblocks.WithIdempotencyKey(hex.EncodeToString(key)))
	if err != nil {
		log.Fatal(err)
	}
	txID := fireblocks.Deref(created.ID)
	fmt.Println("created transaction", txID)

	// See https://developers.fireblocks.com/reference/primary-transaction-statuses.
	final := map[string]bool{"COMPLETED": true, "FAILED": true, "CANCELLED": true, "REJECTED": true, "BLOCKED": true, "TIMEOUT": true}
	for {
		tx, err := client.Transactions.GetTransaction(ctx, txID)
		if err != nil {
			log.Fatal(err)
		}
		status := fireblocks.Deref(tx.Status)
		fmt.Println("status:", status)
		if final[status] {
			return
		}
		select {
		case <-ctx.Done():
			log.Fatal(ctx.Err())
		case <-time.After(3 * time.Second):
		}
	}
}
