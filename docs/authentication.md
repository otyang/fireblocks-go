# Authentication

Fireblocks authenticates each API call with two headers:

- `X-API-Key`: the API user's key.
- `Authorization: Bearer <JWT>`: a JWT signed with the API user's RSA private key (RS256).

The JWT is bound to one request. It carries the request URI (path and query), a SHA-256 hash of the body, a random nonce and a lifetime under 30 seconds. The SDK builds and signs a new token for every attempt, including retries, so you never deal with tokens directly.

## Creating an API user

1. Generate a key pair and a CSR, as described in the [Fireblocks guide](https://developers.fireblocks.com/docs/manage-api-keys):

   ```sh
   openssl req -new -newkey rsa:4096 -nodes -keyout fireblocks_secret.key -out fireblocks.csr -subj '/O=<your workspace>'
   ```

2. Upload `fireblocks.csr` when you create the API user in the Fireblocks console, then copy the API key.
3. Keep `fireblocks_secret.key` secret. It is the credential.

## Constructors

### From a PEM key

```go
secret, err := os.ReadFile("fireblocks_secret.key")
client, err := fireblocks.NewClient(apiKey, secret)
```

PKCS #1 (`RSA PRIVATE KEY`) and PKCS #8 (`PRIVATE KEY`) keys are accepted. So are keys whose newlines were escaped as `\n`, which often happens when they are stored in environment variables or secret managers.

### From the environment

```go
client, err := fireblocks.NewClientFromEnv()
```

| Variable | Required | Meaning |
|---|---|---|
| `FIREBLOCKS_API_KEY` | yes | API key |
| `FIREBLOCKS_SECRET_KEY` | one of these two | PEM private key contents |
| `FIREBLOCKS_SECRET_KEY_PATH` | | Path to the PEM file |
| `FIREBLOCKS_BASE_PATH` | no | Base URL, e.g. `https://sandbox-api.fireblocks.io/v1` |

Options passed to `NewClientFromEnv` override the environment.

### From an HSM or KMS

`NewClientWithSigner` accepts any `crypto.Signer` that holds an RSA key. The SDK calls `Sign(rand, sha256Digest, crypto.SHA256)` and expects a PKCS #1 v1.5 signature:

```go
var signer crypto.Signer = myKMSKey // e.g. from a cloud KMS or PKCS #11 library
client, err := fireblocks.NewClientWithSigner(apiKey, signer)
```

## Environments

| Constant | URL |
|---|---|
| `fireblocks.ProductionBaseURL` (default) | `https://api.fireblocks.io/v1` |
| `fireblocks.SandboxBaseURL` | `https://sandbox-api.fireblocks.io/v1` |
| `fireblocks.EUBaseURL` | `https://eu-api.fireblocks.io/v1` |
| `fireblocks.EU2BaseURL` | `https://eu2-api.fireblocks.io/v1` |

```go
client, err := fireblocks.NewClient(apiKey, secret, fireblocks.WithBaseURL(fireblocks.SandboxBaseURL))
```

If a custom base URL has no path, `/v1` is appended.

## Clock skew

The token is valid for 29 seconds from the local clock's "now". If requests fail with 401 errors and the key is correct, check that the host's clock is synchronised (NTP).
