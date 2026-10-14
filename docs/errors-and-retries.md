# Errors, retries and timeouts

## Error types

| Error | When |
|---|---|
| `*fireblocks.APIError` | The API answered with a non-2xx status |
| wraps `fireblocks.ErrMissingParameter` | A required path parameter was empty, so no request was sent |
| `context.Canceled` / `context.DeadlineExceeded` | The context ended, including while waiting to retry |
| other wrapped errors | Network failures, invalid request bodies, signing failures, undecodable or oversized (> 64 MiB) responses |

```go
_, err := client.Vaults.GetVaultAccount(ctx, id)
switch {
case err == nil:
case fireblocks.IsNotFound(err):
	// 404
case fireblocks.IsRateLimited(err):
	// 429, after retries were exhausted
default:
	if apiErr, ok := fireblocks.AsAPIError(err); ok {
		log.Printf("fireblocks error %d (code %d): %s [request %s]",
			apiErr.StatusCode, apiErr.Code, apiErr.Message, apiErr.RequestID)
	}
}
```

`APIError` fields:

| Field | Source |
|---|---|
| `StatusCode` | HTTP status |
| `Code` | Fireblocks numeric error code ([list](https://developers.fireblocks.com/reference/api-error-codes)), 0 if absent |
| `Type` | Error category on newer endpoints (`VALIDATION`, `NOT_FOUND`, …) |
| `Message` | Human-readable message |
| `RequestID` | `X-Request-ID` header. Quote it to Fireblocks support |
| `Header`, `Body` | Raw response, for anything else |

## Retry policy

Each request is attempted up to `1 + maxRetries` times (default `maxRetries = 2`).

- **429** is always retried, because the server rejected the request without processing it.
- **500, 502, 503, 504 and network errors** are retried only when repeating the request is safe:
  - the method is GET, HEAD, OPTIONS, PUT or DELETE, or
  - the call sets `fireblocks.WithIdempotencyKey(key)`.

A plain `CreateTransaction` that hits a 503 is therefore returned to you instead of being sent again. Retrying could create a second transfer. With an idempotency key, Fireblocks deduplicates the request, so the SDK retries it.

The wait between attempts grows exponentially from 500 ms up to 8 s, with jitter. When a retried response carries a `Retry-After` header, the SDK waits that long instead, capped at 60 s. Errors that happen before anything is sent (encoding the body, signing the JWT) are never retried. Configure it with:

```go
fireblocks.WithMaxRetries(5)
fireblocks.WithRetryBackoff(200*time.Millisecond, 5*time.Second)
fireblocks.WithRetries(0) // per request
```

## Timeouts and cancellation

Every method takes a `context.Context`. Cancellation stops both in-flight requests and retry waits. The default HTTP client also has a 60 s timeout per attempt. Replace it with `WithHTTPClient`:

```go
fireblocks.WithHTTPClient(&http.Client{
	Timeout:   30 * time.Second,
	Transport: otelhttp.NewTransport(http.DefaultTransport), // e.g. tracing
})
```

## Logging

`WithLogger(slog.Default())` logs one debug record per attempt: method, path, attempt, status, duration and request ID. Request and response bodies, keys and tokens are never logged.
