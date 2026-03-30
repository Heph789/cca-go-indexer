package graph

import (
	"net/http"

	"github.com/99designs/gqlgen/graphql/handler"
)

// NewHandler returns an http.Handler that serves the GraphQL endpoint.
// It uses gqlgen's default server with the provided resolver for dependency injection.
func NewHandler(resolver *Resolver) http.Handler {
	return handler.NewDefaultServer(NewExecutableSchema(Config{Resolvers: resolver}))
}
