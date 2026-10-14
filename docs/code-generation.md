# Code generation

The SDK has two parts:

| Part | Files | Edited by |
|---|---|---|
| Runtime | `client.go`, `auth.go`, `request.go`, `errors.go`, `values.go`, `webhook.go` | hand |
| API surface | `*.gen.go`, `docs/api/*.md` | `internal/codegen` |

Never edit `*.gen.go` or `docs/api/` by hand. Change the generator, or the spec, and regenerate.

## Running the generator

```sh
make generate      # same as: go generate ./...
```

The generator is its own Go module (`internal/codegen/go.mod`), so its YAML dependency never reaches users of the SDK. Output is deterministic: running it twice produces identical files, and CI checks that the committed code is up to date.

Flags (see `go run -C internal/codegen . -h`):

| Flag | Default | Meaning |
|---|---|---|
| `-spec` | `api/openapi.yaml` | Input specification |
| `-out` | `.` | Output directory for Go files |
| `-docs` | `docs/api` | Markdown output, relative to `-out` |
| `-include-internal` | `false` | Also generate operations marked `x-internal: true` |

## Mapping rules

| OpenAPI | Go |
|---|---|
| tag | service type `XxxService` and field `Client.Xxx` (`(Beta)` is dropped unless it would clash) |
| `operationId` | method name |
| path / required query / required header params | positional arguments |
| optional query / header params | `XxxParams` struct |
| `Idempotency-Key` header | `WithIdempotencyKey` request option |
| JSON request body | `body *T` argument |
| `multipart/form-data` body | `body *T`, `format: binary` fields become `*File` |
| first 2xx JSON response | return value |
| object | struct (inline objects are named `<Parent><Field>`) |
| `allOf` | flattened struct |
| `oneOf` / `anyOf` | union type (`null` alternatives make the field nullable) |
| string `enum` | named string type with constants |
| `additionalProperties` | `map[string]T` |

## Updating the API version

1. Replace `api/openapi.yaml` with the new specification.
2. Run `make generate`, then `make test`.
3. Review the diff of `*.gen.go`. Renamed or removed schemas are breaking changes.
4. Note the new spec version in `CHANGELOG.md`.
