package graph

import (
	"net/http"

	"github.com/99designs/gqlgen/graphql/handler"
	"github.com/99designs/gqlgen/graphql/playground"
)

// NewHandler returns an http.Handler that serves the GraphQL endpoint.
// GET requests without a query receive the GraphQL Playground UI;
// all other requests are handled by the gqlgen server.
func NewHandler(resolver *Resolver) http.Handler {
	srv := handler.NewDefaultServer(NewExecutableSchema(Config{Resolvers: resolver}))
	pg := playground.Handler("GraphQL Playground", "/graphql")

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Query().Get("query") == "" {
			pg.ServeHTTP(w, r)
			return
		}
		srv.ServeHTTP(w, r)
	})
}
