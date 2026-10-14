package fireblocks_test

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"

	fireblocks "github.com/otyang/fireblocks-go"
)

func ExampleNewClient() {
	secret, err := os.ReadFile("fireblocks_secret.key")
	if err != nil {
		log.Fatal(err)
	}
	client, err := fireblocks.NewClient(os.Getenv("FIREBLOCKS_API_KEY"), secret,
		fireblocks.WithBaseURL(fireblocks.SandboxBaseURL),
	)
	if err != nil {
		log.Fatal(err)
	}

	account, err := client.Vaults.GetVaultAccount(context.Background(), "0")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(*account.Name)
}

func ExampleTransactionsService_CreateTransaction() {
	client, err := fireblocks.NewClientFromEnv()
	if err != nil {
		log.Fatal(err)
	}

	tx, err := client.Transactions.CreateTransaction(context.Background(), &fireblocks.TransactionRequest{
		AssetID: fireblocks.Ptr("ETH_TEST5"),
		Amount:  fireblocks.NewTransactionRequestAmountFromString("0.01"),
		Source: &fireblocks.SourceTransferPeerPath{
			Type: fireblocks.TransferPeerPathTypeVaultAccount,
			ID:   fireblocks.Ptr("0"),
		},
		Destination: &fireblocks.DestinationTransferPeerPath{
			Type: fireblocks.TransferPeerPathTypeVaultAccount,
			ID:   fireblocks.Ptr("1"),
		},
		Note: fireblocks.Ptr("Created with fireblocks-go"),
	}, nil,
		// An idempotency key makes the call safe to retry.
		fireblocks.WithIdempotencyKey("order-1234"),
	)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(*tx.ID, *tx.Status)
}

func ExampleAPIError() {
	client, err := fireblocks.NewClientFromEnv()
	if err != nil {
		log.Fatal(err)
	}
	_, err = client.Vaults.GetVaultAccount(context.Background(), "does-not-exist")

	var apiErr *fireblocks.APIError
	if errors.As(err, &apiErr) {
		fmt.Println(apiErr.StatusCode, apiErr.Code, apiErr.Message, apiErr.RequestID)
	}
}
