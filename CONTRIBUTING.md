# Contributing

Thanks for helping improve fireblocks-go!

## Layout

```
.
├── api/openapi.yaml        Fireblocks OpenAPI specification (input)
├── internal/codegen/       Code generator (separate Go module)
├── *.gen.go                Generated models and services (do not edit)
├── docs/api/               Generated API reference (do not edit)
├── client.go auth.go ...   Hand-written runtime
├── docs/                   Guides
└── examples/               Runnable examples
```

## Workflow

1. Fork and create a branch.
2. Make your change:
   - **Runtime behaviour** (auth, retries, errors): edit the hand-written files and add tests.
   - **Generated code**: change `internal/codegen` and run `make generate`. Never hand-edit `*.gen.go`.
   - **New API version**: replace `api/openapi.yaml` and run `make generate`.
3. Run `make all` (generate, fmt, vet, test). Optionally run `make lint` with [golangci-lint](https://golangci-lint.run).
4. Commit the generated files together with the change that produced them. CI fails if they are out of date.
5. Update `CHANGELOG.md` under **Unreleased**.

## Guidelines

- No new runtime dependencies. The library depends only on the standard library.
- Keep retries safe: never retry a non-idempotent request unless an idempotency key is set.
- Never log or include secrets (keys, JWTs, request bodies) in errors.
- Tests must not call the real Fireblocks API. Use the fake server in `helpers_test.go`.

## Releasing

1. Move the **Unreleased** entries in `CHANGELOG.md` under a new version heading.
2. Update `Version` in `client.go`.
3. Tag and push: `git tag v0.x.y && git push origin v0.x.y`.
