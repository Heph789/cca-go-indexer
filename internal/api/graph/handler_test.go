package graph

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cca/go-indexer/internal/domain/cca"
)

// cacheControlHeader is the HTTP header name used for cache directives.
const cacheControlHeader = "Cache-Control"

// TestHandler_CacheControlHeaders tests that the GraphQL handler sets
// appropriate Cache-Control headers based on the type of response.
// Successful data responses should be cacheable for one block time,
// while error responses and the playground page should not be cached.
func TestHandler_CacheControlHeaders(t *testing.T) {
	// validIntrospectionQuery is a lightweight introspection query that
	// does not require any store data, so we can test cache headers
	// without complex mock setup.
	validIntrospectionQuery := `{"query":"{ __schema { queryType { name } } }"}`

	// errorQuery asks for an auction that will trigger a store error,
	// producing a GraphQL error response.
	errorQuery := `{"query":"{ auction(address: \"0x0000000000000000000000000000000000000001\") { auctionAddress } }"}`

	tests := []struct {
		name string
		// method is the HTTP method for the request.
		method string
		// path is the request URL path (including query string if any).
		path string
		// body is the request body (empty for GET requests).
		body string
		// contentType is the Content-Type header for the request.
		contentType string
		// setupStore configures the mock store for this test case.
		// A nil setupStore uses the default (no-op) mock.
		setupStore func(ms *mockStore)
		// wantCacheHeader is the expected Cache-Control header value.
		// Empty string means the header should be absent.
		wantCacheHeader string
		// wantStatus is the expected HTTP status code.
		wantStatus int
	}{
		// --- successful data responses ---

		// A valid GraphQL query that returns data should include a
		// Cache-Control header so CDNs and browsers can cache the
		// response for one block time.
		{
			name:            "successful GraphQL response includes Cache-Control header",
			method:          http.MethodPost,
			path:            "/graphql",
			body:            validIntrospectionQuery,
			contentType:     "application/json",
			wantCacheHeader: CacheControlShortLived,
			wantStatus:      http.StatusOK,
		},

		// --- error responses ---

		// GraphQL error responses are transient (e.g., store failures,
		// invalid queries) and must NOT be cached. Caching an error
		// would serve stale errors to subsequent requests.
		{
			name:        "GraphQL error response does not include Cache-Control header",
			method:      http.MethodPost,
			path:        "/graphql",
			body:        errorQuery,
			contentType: "application/json",
			setupStore: func(ms *mockStore) {
				ms.auctionRepo.GetByAddressFn = func(_ context.Context, _ int64, _ string) (*cca.Auction, error) {
					return nil, fmt.Errorf("database connection lost")
				}
			},
			wantCacheHeader: "",
			wantStatus:      http.StatusOK,
		},

		// --- playground ---

		// The GraphQL Playground HTML page is a development tool and
		// should not be cached. Caching it could serve stale UI or
		// interfere with development workflows.
		{
			name:            "GET playground does not include Cache-Control header",
			method:          http.MethodGet,
			path:            "/graphql",
			wantCacheHeader: "",
			wantStatus:      http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ms := newMockStore()
			if tt.setupStore != nil {
				tt.setupStore(ms)
			}

			resolver := newTestResolver(ms)
			h := NewHandler(resolver)

			var req *http.Request
			if tt.body != "" {
				req = httptest.NewRequest(tt.method, tt.path, strings.NewReader(tt.body))
			} else {
				req = httptest.NewRequest(tt.method, tt.path, nil)
			}
			if tt.contentType != "" {
				req.Header.Set("Content-Type", tt.contentType)
			}

			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)

			// Verify the HTTP status code.
			if rec.Code != tt.wantStatus {
				t.Errorf("status code = %d, want %d", rec.Code, tt.wantStatus)
			}

			gotCacheHeader := rec.Header().Get(cacheControlHeader)

			if tt.wantCacheHeader != "" {
				// We expect a specific Cache-Control value.
				if gotCacheHeader != tt.wantCacheHeader {
					t.Errorf("Cache-Control = %q, want %q", gotCacheHeader, tt.wantCacheHeader)
				}
			} else {
				// We expect NO Cache-Control header at all.
				if gotCacheHeader != "" {
					t.Errorf("Cache-Control = %q, want header to be absent", gotCacheHeader)
				}
			}
		})
	}
}

// TestHandler_CacheControlNotSetOnGraphQLErrors verifies that when a GraphQL
// query returns errors in the response body, the Cache-Control header is not
// present. This uses a mock store that returns an error for auction lookups
// to trigger a GraphQL-level error (not an HTTP error).
func TestHandler_CacheControlNotSetOnGraphQLErrors(t *testing.T) {
	ms := newMockStore()
	// Configure the auction repo to return an error, which will cause
	// the GraphQL resolver to include an "errors" array in the response.
	ms.auctionRepo.GetByAddressFn = func(_ context.Context, _ int64, _ string) (*cca.Auction, error) {
		return nil, fmt.Errorf("simulated store failure")
	}

	resolver := newTestResolver(ms)
	h := NewHandler(resolver)

	queryBody := `{"query":"{ auction(address: \"0x0000000000000000000000000000000000000001\") { auctionAddress } }"}`
	req := httptest.NewRequest(http.MethodPost, "/graphql", strings.NewReader(queryBody))
	req.Header.Set("Content-Type", "application/json")

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	wantStatus := http.StatusOK
	if rec.Code != wantStatus {
		t.Fatalf("status code = %d, want %d", rec.Code, wantStatus)
	}

	// Parse the response to confirm it actually contains errors.
	var gqlResp struct {
		Errors []json.RawMessage `json:"errors"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&gqlResp); err != nil {
		t.Fatalf("failed to decode GraphQL response: %v", err)
	}

	wantErrorCount := 1
	if len(gqlResp.Errors) < wantErrorCount {
		t.Fatalf("expected at least %d GraphQL error(s), got %d", wantErrorCount, len(gqlResp.Errors))
	}

	// The Cache-Control header must be absent for error responses.
	gotCacheHeader := rec.Header().Get(cacheControlHeader)
	wantCacheHeader := ""
	if gotCacheHeader != wantCacheHeader {
		t.Errorf("Cache-Control = %q, want header to be absent", gotCacheHeader)
	}
}
