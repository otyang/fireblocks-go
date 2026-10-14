// Command list-vaults pages through every vault account in the workspace and
// prints its non-zero balances.
//
//	export FIREBLOCKS_API_KEY=...
//	export FIREBLOCKS_SECRET_KEY_PATH=./fireblocks_secret.key
//	export FIREBLOCKS_BASE_PATH=https://sandbox-api.fireblocks.io/v1
//	go run ./examples/list-vaults
package main

import (
	"context"
	"fmt"
	"log"
	"time"

	fireblocks "github.com/otyang/fireblocks-go"
)

func main() {
	client, err := fireblocks.NewClientFromEnv()
	if err != nil {
		log.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	params := &fireblocks.GetPagedVaultAccountsParams{Limit: fireblocks.Ptr(100.0)}
	for {
		page, err := client.Vaults.GetPagedVaultAccounts(ctx, params)
		if err != nil {
			log.Fatal(err)
		}
		for _, acct := range page.Accounts {
			fmt.Printf("vault %s %q\n", fireblocks.Deref(acct.ID), fireblocks.Deref(acct.Name))
			for _, asset := range acct.Assets {
				if total := fireblocks.Deref(asset.Total); total != "" && total != "0" {
					fmt.Printf("  %-12s %s\n", fireblocks.Deref(asset.ID), total)
				}
			}
		}
		if page.Paging == nil || fireblocks.Deref(page.Paging.After) == "" {
			return
		}
		params.After = page.Paging.After
	}
}
