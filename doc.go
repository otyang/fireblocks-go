// Package fireblocks is an unofficial Go client for the Fireblocks API
// (https://developers.fireblocks.com).
//
// The API surface (models and service methods) is generated from the official
// OpenAPI specification; authentication, retries and error handling are
// written by hand. The package has no dependencies outside the standard library.
//
// # Getting started
//
//	key, _ := os.ReadFile("fireblocks_secret.key")
//	client, err := fireblocks.NewClient(apiKey, key, fireblocks.WithBaseURL(fireblocks.SandboxBaseURL))
//	if err != nil {
//		log.Fatal(err)
//	}
//	account, err := client.Vaults.GetVaultAccount(ctx, "0")
//
// Endpoints are grouped into services that mirror the API reference, such as
// Client.Vaults, Client.Transactions or Client.Webhooks. Each method takes a
// context, the path parameters and required query parameters as positional
// arguments, a typed request body when the endpoint has one, a *XxxParams
// struct for optional parameters (nil is fine), and optional RequestOption values.
//
// # Authentication
//
// Every request is signed with a short-lived RS256 JWT bound to the request
// URI and body hash, as Fireblocks requires. Use NewClient with a PEM key,
// NewClientWithSigner to keep the key in an HSM or KMS, or NewClientFromEnv.
//
// # Optional values and unions
//
// Optional scalar fields are pointers; use Ptr to fill them. Fields defined
// with oneOf/anyOf are union types that keep the raw JSON. Build one with its
// New<Type>From<Variant> constructor and read it with the As<Variant> methods.
// Unions with a discriminator also provide Discriminator and ValueByDiscriminator.
//
// # Errors and retries
//
// Non-2xx responses are returned as *APIError, which carries the status, the
// Fireblocks error code, the message and the X-Request-ID.
//
// Requests are retried with exponential backoff (2 retries by default, see
// WithMaxRetries). 429 responses are always retried. Network errors and
// 500/502/503/504 responses are retried only when it is safe: for GET, HEAD,
// OPTIONS, PUT and DELETE, or when an idempotency key is set with
// WithIdempotencyKey. This prevents, for example, a transaction from being
// created twice.
package fireblocks

//go:generate go run -C internal/codegen . -spec ../../api/openapi.yaml -out ../..
