# Security policy

## Reporting a vulnerability

Please **do not** open a public issue for security problems. Report them privately through [GitHub security advisories](https://github.com/otyang/fireblocks-go/security/advisories/new). You should get a response within 7 days.

Vulnerabilities in the Fireblocks platform itself should go to Fireblocks directly.

## Handling credentials

- Treat the API private key like a password. Store it in a secret manager or an HSM/KMS (see `NewClientWithSigner`), never in source control.
- The SDK never logs keys, JWTs or request and response bodies.
- Use the sandbox environment (`SandboxBaseURL`) for development and tests.
