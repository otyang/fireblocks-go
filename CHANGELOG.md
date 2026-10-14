# Changelog

All notable changes to this project are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and the project uses
[Semantic Versioning](https://semver.org/).

## [Unreleased]

## [0.1.0] - 2026-10-14

### Added

- Client covering 488 operations across 57 services, generated from Fireblocks OpenAPI specification v1.6.2.
- Typed models, string enums with constants, and union types for `oneOf`/`anyOf` with discriminator support.
- Fireblocks JWT (RS256) request signing with PEM keys or any `crypto.Signer` (HSM/KMS).
- `NewClientFromEnv` for configuration from environment variables.
- Retries with exponential backoff and `Retry-After` support that never repeat non-idempotent requests without an idempotency key.
- `APIError` with status, Fireblocks error code, message and request ID.
- Request options: idempotency keys, extra headers and query parameters, per-call retries, response hooks.
- Webhook signature verification.
- Generated API reference in `docs/api`, plus guides and examples.

[Unreleased]: https://github.com/otyang/fireblocks-go/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/otyang/fireblocks-go/releases/tag/v0.1.0
