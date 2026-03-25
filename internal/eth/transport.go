package eth

import (
	"net/http"
	"time"
)

// RetryConfig controls the retry behavior of the HTTP transport.
type RetryConfig struct {
	MaxRetries int
	BaseDelay  time.Duration
}

// retryTransport wraps an http.RoundTripper and retries on transient failures
// using exponential backoff. This keeps retry logic out of the ethclient
// interface methods and handles it transparently at the HTTP level.
type retryTransport struct {
	base       http.RoundTripper
	maxRetries int
	baseDelay  time.Duration
}

// newHTTPClientWithRetry returns an http.Client that automatically retries
// transient HTTP errors (429, 502, 503, 504) with exponential backoff.
func newHTTPClientWithRetry(cfg RetryConfig) *http.Client {
	// TODO:
	// Return &http.Client{Transport: &retryTransport{
	//     base:       http.DefaultTransport,
	//     maxRetries: cfg.MaxRetries,
	//     baseDelay:  cfg.BaseDelay,
	// }}
	panic("not implemented")
}

func (t *retryTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// TODO:
	// 1. Loop up to maxRetries+1 attempts
	// 2. Call t.base.RoundTrip(req)
	// 3. If success and non-retryable status, return
	// 4. If retryable, close resp.Body, sleep with exponential backoff + jitter:
	//    delay = baseDelay * 2^attempt * random(0.5, 1.0)
	//    Jitter prevents thundering herd when multiple instances retry simultaneously.
	// 5. Return last response/error after exhausting retries
	//
	// With defaults (5 retries, 500ms base), worst-case budget is ~24s:
	//   500ms + 1s + 2s + 4s + 8s = 15.5s (plus jitter up to ~24s)
	panic("not implemented")
}

func isRetryableStatus(code int) bool {
	return code == http.StatusTooManyRequests ||
		code == http.StatusBadGateway ||
		code == http.StatusServiceUnavailable ||
		code == http.StatusGatewayTimeout
}
