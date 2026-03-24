package store_test

import (
	"testing"

	"github.com/cca/go-indexer/internal/store"
)

// TestStoreInterfaceHasClose verifies that the Store interface includes a
// Close() method. This is a compile-time contract: if Close() is missing from
// the interface, this test file will not compile.
func TestStoreInterfaceHasClose(t *testing.T) {
	// Use a method expression so the compiler proves Close exists on the
	// interface without requiring a non-nil receiver.
	var _ func(store.Store) = store.Store.Close
}
