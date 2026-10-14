# Working with types

Every schema in the Fireblocks OpenAPI specification becomes a Go type. Names follow Go conventions (`vaultAccountId` becomes `VaultAccountID`). JSON tags keep the wire names.

## Optional and required fields

| Field kind | Required | Optional |
|---|---|---|
| string, number, bool, enum | `T` | `*T` with `omitempty` |
| object, union | `*T` | `*T` with `omitempty` |
| array, map | `[]T` / `map[string]T` | same, with `omitempty` |

A nullable required scalar is also a pointer. Two generic helpers keep this pleasant:

```go
fireblocks.Ptr("value")        // *string
fireblocks.Deref(resp.Name)    // string, "" if nil
```

Optional query and header parameters live in a per-method `XxxParams` struct. All of its fields are optional, and passing `nil` sends none of them.

## Numbers

`number` maps to `float64`, `integer` to `int64` (or `int32` for `format: int32`). Amounts are usually strings in the API (for example `VaultAsset.Total`). Keep them as strings, or parse them with `math/big` or a decimal library to avoid rounding.

Date-time values are kept as `string`, exactly as the API sends them. Parse them with `time.Parse(time.RFC3339, s)` where you need to.

## Enums

String enums become a named string type with one constant per value:

```go
type TransferPeerPathType string

const (
	TransferPeerPathTypeVaultAccount TransferPeerPathType = "VAULT_ACCOUNT"
	TransferPeerPathTypeExchangeAccount TransferPeerPathType = "EXCHANGE_ACCOUNT"
	// ...
)
```

Values the spec doesn't list yet still decode fine (the type is just a string), and you can convert any string: `fireblocks.TransferPeerPathType("NEW_TYPE")`.

## Unions

`oneOf` and `anyOf` schemas become a struct that stores the raw JSON:

```go
// Build
amount := fireblocks.NewTransactionRequestAmountFromString("0.5")

// Read
s, err := amount.AsString()

// Inspect
raw := amount.Raw() // json.RawMessage
```

When the spec defines a discriminator, the union also provides:

```go
kind, err := account.Discriminator()        // e.g. "VAULT_ACCOUNT"
v, err := account.ValueByDiscriminator()    // decoded into the matching variant type
```

Without a discriminator, an `As<Variant>` method only checks that the JSON can be decoded into the variant type. For object variants that is true of almost any object, so look at the decoded fields (or `Raw()`) to decide which variant you received.

`allOf` compositions are flattened into a single struct with the fields of every part.

A `oneOf` alternative of just `null` makes the field nullable instead of creating a union.

## Files

Multipart uploads take a `*fireblocks.File`:

```go
err := client.Trading.UploadOrderRequirementFile(ctx, orderID, &fireblocks.OrderRequirementFileUpload{
	FileKey: "proofOfAddress",
	File:    &fireblocks.File{Name: "bill.pdf", ContentType: "application/pdf", Content: f},
})
```

## Free-form fields

Schemas without a defined shape decode into `map[string]any` (objects) or `any`. Use `json.Marshal` and `json.Unmarshal` to move them into your own types.
