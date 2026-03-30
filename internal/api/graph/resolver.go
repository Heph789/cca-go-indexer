package graph

import (
	"github.com/cca/go-indexer/internal/store"
)

// Resolver is the root resolver. It holds dependencies shared across all
// resolvers and implements the gqlgen ResolverRoot interface.
type Resolver struct {
	// Store provides access to all domain repositories.
	Store store.Store
	// ChainID is the default chain ID for queries that don't specify one.
	ChainID int64
}
