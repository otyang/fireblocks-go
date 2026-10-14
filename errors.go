package fireblocks

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

// ErrMissingParameter is returned (wrapped) when a required path parameter is
// empty. The request is not sent.
var ErrMissingParameter = errors.New("fireblocks: missing required path parameter")

// APIError is returned when the Fireblocks API responds with a non-2xx status.
//
// Use errors.As (or AsAPIError) to inspect it:
//
//	var apiErr *fireblocks.APIError
//	if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound { ... }
type APIError struct {
	// StatusCode is the HTTP status code.
	StatusCode int
	// Code is the numeric Fireblocks error code, when the response contains one.
	// See https://developers.fireblocks.com/reference/api-error-codes.
	Code int
	// Type is the error category some endpoints return (e.g. "VALIDATION").
	Type string
	// Message is the human-readable error message.
	Message string
	// RequestID is the X-Request-ID response header. Include it in support tickets.
	RequestID string
	// Header holds the response headers.
	Header http.Header
	// Body is the raw response body.
	Body []byte
}

func (e *APIError) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "fireblocks: %d %s", e.StatusCode, http.StatusText(e.StatusCode))
	if e.Message != "" {
		b.WriteString(": " + e.Message)
	}
	var extra []string
	if e.Code != 0 {
		extra = append(extra, "code "+strconv.Itoa(e.Code))
	}
	if e.Type != "" {
		extra = append(extra, "type "+e.Type)
	}
	if e.RequestID != "" {
		extra = append(extra, "request id "+e.RequestID)
	}
	if len(extra) > 0 {
		b.WriteString(" (" + strings.Join(extra, ", ") + ")")
	}
	return b.String()
}

// AsAPIError returns the *APIError in err's chain, if any.
func AsAPIError(err error) (*APIError, bool) {
	var e *APIError
	ok := errors.As(err, &e)
	return e, ok
}

func hasStatus(err error, status int) bool {
	e, ok := AsAPIError(err)
	return ok && e.StatusCode == status
}

// IsNotFound reports whether err is an APIError with status 404.
func IsNotFound(err error) bool { return hasStatus(err, http.StatusNotFound) }

// IsUnauthorized reports whether err is an APIError with status 401.
func IsUnauthorized(err error) bool { return hasStatus(err, http.StatusUnauthorized) }

// IsRateLimited reports whether err is an APIError with status 429.
func IsRateLimited(err error) bool { return hasStatus(err, http.StatusTooManyRequests) }

func newAPIError(resp *http.Response, body []byte) *APIError {
	e := &APIError{
		StatusCode: resp.StatusCode,
		RequestID:  resp.Header.Get("X-Request-ID"),
		Header:     resp.Header,
		Body:       body,
	}
	// Fireblocks uses a few error shapes:
	//   {"message": "...", "code": 1234}
	//   {"error": {"type": "VALIDATION", "message": "..."}}
	//   {"error": "...", "message": "..."}
	var raw map[string]json.RawMessage
	if json.Unmarshal(body, &raw) != nil {
		var s string
		if json.Unmarshal(body, &s) != nil {
			s = string(body)
		}
		e.Message = truncate(strings.TrimSpace(s), maxErrorMessageLen)
		return e
	}
	e.Message = jsonString(raw["message"])
	e.Code = jsonInt(raw["code"])
	if inner, ok := raw["error"]; ok {
		var obj map[string]json.RawMessage
		if json.Unmarshal(inner, &obj) == nil {
			e.Type = jsonString(obj["type"])
			if e.Message == "" {
				e.Message = jsonString(obj["message"])
			}
			if e.Code == 0 {
				e.Code = jsonInt(obj["code"])
			}
		} else if e.Message == "" {
			e.Message = jsonString(inner)
		}
	}
	if e.Code == 0 {
		e.Code = jsonInt(raw["errorCode"])
	}
	return e
}

// maxErrorMessageLen bounds the message taken from a non-JSON error body
// (for example an HTML page from a proxy).
const maxErrorMessageLen = 512

// truncate shortens s to at most n bytes without splitting a UTF-8 sequence.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return strings.ToValidUTF8(s[:n], "")
}

func jsonString(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	return ""
}

func jsonInt(raw json.RawMessage) int {
	var f float64
	if json.Unmarshal(raw, &f) == nil {
		return int(f)
	}
	if n, err := strconv.Atoi(jsonString(raw)); err == nil {
		return n
	}
	return 0
}
