package eth

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

// mockRoundTripper records calls and returns configurable responses.
type mockRoundTripper struct {
	responses []*http.Response
	errors    []error
	calls     int
	callTimes []time.Time
	bodies    []string // request bodies captured on each call
}

func (m *mockRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	m.callTimes = append(m.callTimes, time.Now())
	if req.Body != nil {
		b, _ := io.ReadAll(req.Body)
		m.bodies = append(m.bodies, string(b))
	} else {
		m.bodies = append(m.bodies, "")
	}
	i := m.calls
	m.calls++
	if i < len(m.responses) {
		return m.responses[i], m.errors[i]
	}
	return nil, fmt.Errorf("unexpected call %d", i)
}

func makeResponse(statusCode int) *http.Response {
	return &http.Response{
		StatusCode: statusCode,
		Body:       io.NopCloser(strings.NewReader("")),
	}
}

func TestRetryTransport_RetriesOn429(t *testing.T) {
	mock := &mockRoundTripper{
		responses: []*http.Response{
			makeResponse(429),
			makeResponse(429),
			makeResponse(200),
		},
		errors: []error{nil, nil, nil},
	}

	transport := &retryTransport{
		base:       mock,
		maxRetries: 3,
		baseDelay:  1 * time.Millisecond,
	}

	req, _ := http.NewRequest("GET", "http://example.com", nil)
	resp, err := transport.RoundTrip(req)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}
	if mock.calls != 3 {
		t.Errorf("expected 3 calls, got %d", mock.calls)
	}
}

func TestRetryTransport_RetriesOn5xxStatuses(t *testing.T) {
	for _, code := range []int{502, 503, 504} {
		t.Run(fmt.Sprintf("status_%d", code), func(t *testing.T) {
			mock := &mockRoundTripper{
				responses: []*http.Response{
					makeResponse(code),
					makeResponse(200),
				},
				errors: []error{nil, nil},
			}

			transport := &retryTransport{
				base:       mock,
				maxRetries: 3,
				baseDelay:  1 * time.Millisecond,
			}

			req, _ := http.NewRequest("GET", "http://example.com", nil)
			resp, err := transport.RoundTrip(req)
			if err != nil {
				t.Fatalf("expected no error, got: %v", err)
			}
			if resp.StatusCode != 200 {
				t.Errorf("expected status 200, got %d", resp.StatusCode)
			}
			if mock.calls != 2 {
				t.Errorf("expected 2 calls, got %d", mock.calls)
			}
		})
	}
}

func TestRetryTransport_NoRetryOnNonRetryableStatuses(t *testing.T) {
	for _, code := range []int{200, 400, 404} {
		t.Run(fmt.Sprintf("status_%d", code), func(t *testing.T) {
			mock := &mockRoundTripper{
				responses: []*http.Response{
					makeResponse(code),
				},
				errors: []error{nil},
			}

			transport := &retryTransport{
				base:       mock,
				maxRetries: 3,
				baseDelay:  1 * time.Millisecond,
			}

			req, _ := http.NewRequest("GET", "http://example.com", nil)
			resp, err := transport.RoundTrip(req)
			if err != nil {
				t.Fatalf("expected no error, got: %v", err)
			}
			if resp.StatusCode != code {
				t.Errorf("expected status %d, got %d", code, resp.StatusCode)
			}
			if mock.calls != 1 {
				t.Errorf("expected 1 call, got %d", mock.calls)
			}
		})
	}
}

func TestRetryTransport_ReturnsLastResponseAfterExhaustingRetries(t *testing.T) {
	mock := &mockRoundTripper{
		responses: []*http.Response{
			makeResponse(429),
			makeResponse(429),
			makeResponse(429),
			makeResponse(429),
		},
		errors: []error{nil, nil, nil, nil},
	}

	transport := &retryTransport{
		base:       mock,
		maxRetries: 3,
		baseDelay:  1 * time.Millisecond,
	}

	req, _ := http.NewRequest("GET", "http://example.com", nil)
	resp, err := transport.RoundTrip(req)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if resp.StatusCode != 429 {
		t.Errorf("expected last response status 429, got %d", resp.StatusCode)
	}
	// 1 initial + 3 retries = 4 total calls
	if mock.calls != 4 {
		t.Errorf("expected 4 calls (1 initial + 3 retries), got %d", mock.calls)
	}
}

func TestRetryTransport_BackoffIncreasesExponentially(t *testing.T) {
	mock := &mockRoundTripper{
		responses: []*http.Response{
			makeResponse(429),
			makeResponse(429),
			makeResponse(429),
			makeResponse(200),
		},
		errors: []error{nil, nil, nil, nil},
	}

	baseDelay := 10 * time.Millisecond
	transport := &retryTransport{
		base:       mock,
		maxRetries: 3,
		baseDelay:  baseDelay,
	}

	req, _ := http.NewRequest("GET", "http://example.com", nil)
	_, err := transport.RoundTrip(req)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if len(mock.callTimes) != 4 {
		t.Fatalf("expected 4 call times, got %d", len(mock.callTimes))
	}

	// Verify delays increase: each gap should be larger than the previous.
	// With jitter (0.5x-1.0x), the minimum expected delays are:
	//   retry 1: baseDelay * 0.5 = 5ms
	//   retry 2: baseDelay * 2 * 0.5 = 10ms
	//   retry 3: baseDelay * 4 * 0.5 = 20ms
	for i := 1; i < len(mock.callTimes); i++ {
		gap := mock.callTimes[i].Sub(mock.callTimes[i-1])
		// Each delay should be at least 0.5x of baseDelay * 2^(i-1)
		minExpected := time.Duration(float64(baseDelay) * float64(int(1)<<(i-1)) * 0.4) // generous tolerance
		if gap < minExpected {
			t.Errorf("retry %d: delay %v is less than minimum expected %v", i, gap, minExpected)
		}
	}

	// Verify second gap is larger than first gap (exponential increase).
	gap1 := mock.callTimes[2].Sub(mock.callTimes[1])
	gap0 := mock.callTimes[1].Sub(mock.callTimes[0])
	if gap1 <= gap0 {
		t.Errorf("expected second delay (%v) > first delay (%v) for exponential backoff", gap1, gap0)
	}
}

func TestRetryTransport_JitterWithinExpectedBounds(t *testing.T) {
	// Run multiple iterations to verify jitter stays within 0.5x to 1.0x bounds.
	baseDelay := 20 * time.Millisecond
	iterations := 20

	for i := 0; i < iterations; i++ {
		mock := &mockRoundTripper{
			responses: []*http.Response{
				makeResponse(429),
				makeResponse(200),
			},
			errors: []error{nil, nil},
		}

		transport := &retryTransport{
			base:       mock,
			maxRetries: 1,
			baseDelay:  baseDelay,
		}

		req, _ := http.NewRequest("GET", "http://example.com", nil)
		_, err := transport.RoundTrip(req)
		if err != nil {
			t.Fatalf("iteration %d: expected no error, got: %v", i, err)
		}

		if len(mock.callTimes) != 2 {
			t.Fatalf("iteration %d: expected 2 call times, got %d", i, len(mock.callTimes))
		}

		gap := mock.callTimes[1].Sub(mock.callTimes[0])
		// For retry 0: full delay = baseDelay * 2^0 = baseDelay
		// Jitter bounds: 0.5 * baseDelay to 1.0 * baseDelay
		minDelay := time.Duration(float64(baseDelay) * 0.4) // slightly below 0.5x for timing tolerance
		maxDelay := time.Duration(float64(baseDelay) * 1.2)  // slightly above 1.0x for timing tolerance
		if gap < minDelay || gap > maxDelay {
			t.Errorf("iteration %d: delay %v outside expected jitter bounds [%v, %v]", i, gap, minDelay, maxDelay)
		}
	}
}

func TestNewHTTPClientWithRetry(t *testing.T) {
	cfg := RetryConfig{
		MaxRetries: 5,
		BaseDelay:  500 * time.Millisecond,
	}

	client := newHTTPClientWithRetry(cfg)
	if client == nil {
		t.Fatal("expected non-nil http.Client")
	}

	transport, ok := client.Transport.(*retryTransport)
	if !ok {
		t.Fatalf("expected transport to be *retryTransport, got %T", client.Transport)
	}
	if transport.maxRetries != cfg.MaxRetries {
		t.Errorf("expected maxRetries %d, got %d", cfg.MaxRetries, transport.maxRetries)
	}
	if transport.baseDelay != cfg.BaseDelay {
		t.Errorf("expected baseDelay %v, got %v", cfg.BaseDelay, transport.baseDelay)
	}
	if transport.base == nil {
		t.Error("expected non-nil base transport")
	}
}

func TestRetryTransport_PreservesRequestBodyAcrossRetries(t *testing.T) {
	mock := &mockRoundTripper{
		responses: []*http.Response{
			makeResponse(429),
			makeResponse(429),
			makeResponse(200),
		},
		errors: []error{nil, nil, nil},
	}

	transport := &retryTransport{
		base:       mock,
		maxRetries: 3,
		baseDelay:  1 * time.Millisecond,
	}

	body := `{"jsonrpc":"2.0","method":"eth_blockNumber","params":[],"id":1}`
	req, _ := http.NewRequest("POST", "http://example.com", strings.NewReader(body))
	resp, err := transport.RoundTrip(req)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}
	if mock.calls != 3 {
		t.Errorf("expected 3 calls, got %d", mock.calls)
	}
	// Every attempt must have received the full request body.
	for i, b := range mock.bodies {
		if b != body {
			t.Errorf("attempt %d: expected body %q, got %q", i, body, b)
		}
	}
}

func TestRetryTransport_RespectsContextCancellation(t *testing.T) {
	mock := &mockRoundTripper{
		responses: []*http.Response{
			makeResponse(429),
			makeResponse(429),
			makeResponse(200),
		},
		errors: []error{nil, nil, nil},
	}

	transport := &retryTransport{
		base:       mock,
		maxRetries: 3,
		baseDelay:  5 * time.Second, // long delay so cancellation fires first
	}

	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, "GET", "http://example.com", nil)

	// Cancel after a short delay (before the backoff sleep finishes).
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	_, err := transport.RoundTrip(req)
	elapsed := time.Since(start)

	// Should return quickly with context error, not wait for full backoff.
	if err == nil || err != context.Canceled {
		t.Errorf("expected context.Canceled error, got: %v", err)
	}
	if elapsed > 1*time.Second {
		t.Errorf("expected fast return on context cancellation, took %v", elapsed)
	}
}

func TestIsRetryableStatus(t *testing.T) {
	tests := []struct {
		code int
		want bool
	}{
		{200, false},
		{201, false},
		{400, false},
		{401, false},
		{403, false},
		{404, false},
		{429, true},
		{500, false},
		{502, true},
		{503, true},
		{504, true},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("status_%d", tt.code), func(t *testing.T) {
			got := isRetryableStatus(tt.code)
			if got != tt.want {
				t.Errorf("isRetryableStatus(%d) = %v, want %v", tt.code, got, tt.want)
			}
		})
	}
}
