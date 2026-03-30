package graph

import (
	"bytes"
	"net/http"

	"github.com/99designs/gqlgen/graphql/handler"
	"github.com/99designs/gqlgen/graphql/playground"
)

// CacheControlShortLived is the Cache-Control header for successful GraphQL
// responses. On-chain data changes at block frequency (~12 seconds on Ethereum
// mainnet), so a short cache avoids stale data while reducing load.
const CacheControlShortLived = "public, max-age=12"

// NewHandler returns an http.Handler that serves the GraphQL endpoint.
// GET requests without a query receive the GraphQL Playground UI;
// all other requests are handled by the gqlgen server.
// Successful GraphQL data-only responses include a short-lived Cache-Control
// header; responses containing errors are not cached.
func NewHandler(resolver *Resolver) http.Handler {
	srv := handler.NewDefaultServer(NewExecutableSchema(Config{Resolvers: resolver}))
	pg := playground.Handler("GraphQL Playground", "/graphql")

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Query().Get("query") == "" {
			pg.ServeHTTP(w, r)
			return
		}

		// Buffer the response so we can inspect it before sending headers.
		buf := &bufferedWriter{header: w.Header(), body: &bytes.Buffer{}}
		srv.ServeHTTP(buf, r)

		// gqlgen may not call WriteHeader explicitly; default to 200.
		status := buf.status
		if status == 0 {
			status = http.StatusOK
		}

		// Set cache header only for successful, error-free responses.
		if status >= 200 && status < 300 && !bytes.Contains(buf.body.Bytes(), []byte(`"errors"`)) {
			w.Header().Set("Cache-Control", CacheControlShortLived)
		}

		w.WriteHeader(status)
		w.Write(buf.body.Bytes())
	})
}

// bufferedWriter captures the response body and status code so the handler
// can inspect the response before committing headers.
type bufferedWriter struct {
	header http.Header
	body   *bytes.Buffer
	status int
}

func (w *bufferedWriter) Header() http.Header         { return w.header }
func (w *bufferedWriter) WriteHeader(code int)         { w.status = code }
func (w *bufferedWriter) Write(b []byte) (int, error)  { return w.body.Write(b) }
