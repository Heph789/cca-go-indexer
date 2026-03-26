package eth

import (
	"math/rand"
	"net/http"
	"time"
)

type RetryConfig struct {
	MaxRetries int
	BaseDelay  time.Duration
}

type retryTransport struct {
	base       http.RoundTripper
	maxRetries int
	baseDelay  time.Duration
}

func newHTTPClientWithRetry(cfg RetryConfig) *http.Client {
	return &http.Client{Transport: &retryTransport{
		base:       http.DefaultTransport,
		maxRetries: cfg.MaxRetries,
		baseDelay:  cfg.BaseDelay,
	}}
}

func (t *retryTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	var resp *http.Response
	var err error

	for attempt := 0; attempt <= t.maxRetries; attempt++ {
		if attempt > 0 {
			delay := t.baseDelay * time.Duration(1<<(attempt-1))
			jitter := 0.5 + rand.Float64()*0.5 // 0.5 to 1.0
			time.Sleep(time.Duration(float64(delay) * jitter))
		}

		resp, err = t.base.RoundTrip(req)
		if err != nil {
			continue
		}

		if !isRetryableStatus(resp.StatusCode) {
			return resp, nil
		}

		if attempt < t.maxRetries {
			resp.Body.Close()
		}
	}

	return resp, err
}

func isRetryableStatus(code int) bool {
	return code == http.StatusTooManyRequests ||
		code == http.StatusBadGateway ||
		code == http.StatusServiceUnavailable ||
		code == http.StatusGatewayTimeout
}
