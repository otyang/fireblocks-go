# fireblocks-go

[![Go Reference](https://pkg.go.dev/badge/github.com/otyang/fireblocks-go.svg)](https://pkg.go.dev/github.com/otyang/fireblocks-go)
[![CI](https://github.com/otyang/fireblocks-go/actions/workflows/ci.yml/badge.svg)](https://github.com/otyang/fireblocks-go/actions/workflows/ci.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/otyang/fireblocks-go)](https://goreportcard.com/report/github.com/otyang/fireblocks-go)

An idiomatic, fully typed Go client for the [Fireblocks API](https://developers.fireblocks.com/reference).

> **Unofficial.** This project is not affiliated with or endorsed by Fireblocks.

- **Complete:** 488 operations across 57 services, generated from the official OpenAPI specification (v1.6.2).
- **Typed:** request and response structs, string enums with constants, and union types for `oneOf`/`anyOf`.
- **Safe by default:** Fireblocks JWT request signing, plus retries that never repeat a non-idempotent call.
- **Zero dependencies:** only the Go standard library.
- **HSM/KMS ready:** sign with any `crypto.Signer`.

## Installation

```sh
go get github.com/otyang/fireblocks-go
```

Requires Go 1.22 or newer.

## Quick start

```go
package main

import (
	"context"
	"fmt"
	"log"
	"os"

	fireblocks "github.com/otyang/fireblocks-go"
)

func main() {
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
	fmt.Println(fireblocks.Deref(account.Name))
}
```

## Usage

### Services and methods

Endpoints are grouped by API area, matching the Fireblocks API reference:

```go
client.Vaults.GetPagedVaultAccounts(ctx, params)
client.Transactions.CreateTransaction(ctx, body, params)
client.ExternalWallets.GetExternalWallets(ctx)
client.Staking.GetChains(ctx)
client.WebhooksV2.CreateWebhook(ctx, body)
```

Every method follows the same shape:

```go
func (s *XService) Method(
	ctx context.Context,
	pathParam1 string, ...,        // path parameters, then required query/header parameters
	body *RequestType,             // when the endpoint has a request body
	params *MethodParams,          // when the endpoint has optional parameters (nil is fine)
	opts ...fireblocks.RequestOption,
) (*ResponseType, error)
```

See the **[API reference](docs/api/README.md)** for every service and method, or browse the types on [pkg.go.dev](https://pkg.go.dev/github.com/otyang/fireblocks-go).

### Creating a transaction

```go
tx, err := client.Transactions.CreateTransaction(ctx, &fireblocks.TransactionRequest{
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
}, nil, fireblocks.WithIdempotencyKey(orderID))
```

### Optional fields

Optional scalar fields are pointers, so "not set" can be told apart from a zero value. `fireblocks.Ptr` builds one and `fireblocks.Deref` reads one safely:

```go
req := &fireblocks.CreateVaultAccountRequest{Name: fireblocks.Ptr("Treasury")}
fmt.Println(fireblocks.Deref(account.Name)) // "" when the field is absent
```

### Pagination

List endpoints return their cursor in the response body. Pass it back in the params:

```go
params := &fireblocks.GetPagedVaultAccountsParams{Limit: fireblocks.Ptr(100.0)}
for {
	page, err := client.Vaults.GetPagedVaultAccounts(ctx, params)
	if err != nil {
		return err
	}
	// ... use page.Accounts
	if page.Paging == nil || fireblocks.Deref(page.Paging.After) == "" {
		break
	}
	params.After = page.Paging.After
}
```

Some endpoints (for example `GetTransactions`) return the next page in the `next-page` response header instead. Read it with `fireblocks.OnResponse`.

### Unions (`oneOf` / `anyOf`)

Polymorphic fields are union types. Build them with `New<Type>From<Variant>` and read them with `As<Variant>`. Unions with a discriminator also have `Discriminator()` and `ValueByDiscriminator()`:

```go
v, err := quote.Via.ValueByDiscriminator()
switch via := v.(type) {
case fireblocks.DirectAccessResponse:
	// ...
case fireblocks.AccountAccessResponse:
	// ...
}
```

The raw JSON is always available via `Raw()`, so no data is lost even if the spec is incomplete.

### Errors

Any non-2xx response is returned as a `*fireblocks.APIError`:

```go
_, err := client.Vaults.GetVaultAccount(ctx, "42")
if apiErr, ok := fireblocks.AsAPIError(err); ok {
	log.Printf("status=%d code=%d message=%q request_id=%s",
		apiErr.StatusCode, apiErr.Code, apiErr.Message, apiErr.RequestID)
}
if fireblocks.IsNotFound(err) { /* ... */ }
```

Include `RequestID` in any Fireblocks support ticket.

### Retries

The client retries with exponential backoff and jitter, and honours `Retry-After`. It makes 2 retries by default (`WithMaxRetries`, or `WithRetries` per call).

| Condition | GET / PUT / DELETE | POST / PATCH | POST / PATCH with `WithIdempotencyKey` |
|---|---|---|---|
| 429 Too Many Requests | retried | retried | retried |
| 500 / 502 / 503 / 504 | retried | **not retried** | retried |
| Network error | retried | **not retried** | retried |

This means a transfer is never created twice by the SDK. Set an idempotency key on POST calls to get retries back safely.

### Per-request options

```go
client.Vaults.GetVaultAccount(ctx, "0",
	fireblocks.WithHeader("X-Custom", "1"),
	fireblocks.WithRetries(0),
	fireblocks.OnResponse(func(r *http.Response) {
		log.Println(r.Header.Get("X-Request-ID"))
	}),
)
```

### Webhooks

```go
pub, _ := fireblocks.ParseRSAPublicKeyPEM(fireblocksWebhookPublicKey)
err := fireblocks.VerifyWebhookSignature(body, r.Header.Get("Fireblocks-Signature"), pub)
```

See [examples/webhook-server](examples/webhook-server/main.go).

## Configuration

| Option | Default | Description |
|---|---|---|
| `WithBaseURL(url)` | `ProductionBaseURL` | `SandboxBaseURL`, `EUBaseURL`, `EU2BaseURL` or a custom URL |
| `WithHTTPClient(hc)` | 60s timeout | Custom `*http.Client` (proxies, mTLS, tracing) |
| `WithMaxRetries(n)` | `2` | Maximum retries per request |
| `WithRetryBackoff(min, max)` | `500ms`, `8s` | Backoff bounds |
| `WithUserAgent(ua)` | | Prefix added to the `User-Agent` header |
| `WithLogger(*slog.Logger)` | none | Debug log for each attempt (bodies are never logged) |

`NewClientFromEnv` reads `FIREBLOCKS_API_KEY`, `FIREBLOCKS_SECRET_KEY` (PEM contents) or `FIREBLOCKS_SECRET_KEY_PATH`, and optionally `FIREBLOCKS_BASE_PATH`.

More detail is in the [guides](docs/README.md).

## Examples

| Example | Description |
|---|---|
| [list-vaults](examples/list-vaults/main.go) | Page through vault accounts and print balances |
| [create-transaction](examples/create-transaction/main.go) | Transfer between vaults and poll until final |
| [webhook-server](examples/webhook-server/main.go) | Verify and handle webhook notifications |

## Development

The models, services and API reference are generated from [`api/openapi.yaml`](api/openapi.yaml) by the generator in [`internal/codegen`](internal/codegen). To update to a new API version:

```sh
curl -o api/openapi.yaml <new spec URL>
make generate
make test
```

See [CONTRIBUTING.md](CONTRIBUTING.md) for details.

## Versioning

This module follows [semantic versioning](https://semver.org). While in `v0.x`, minor releases may contain breaking changes when the upstream API specification changes. `fireblocks.APISpecVersion` reports the spec version the release was generated from.

## License

[MIT](LICENSE)
