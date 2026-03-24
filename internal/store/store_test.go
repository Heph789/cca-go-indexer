package store_test

import (
	"testing"

	"github.com/cca/go-indexer/internal/store"
)

// TestStoreInterfaceHasClose verifies that the Store interface includes a
// Close() method. This is a compile-time contract: if Close() is missing from
// the interface, this test file will not compile.
func TestStoreInterfaceHasClose(t *testing.T) {
	// Obtain a nil store.Store value. We only care that the method exists on
	// the interface; we do not call it.
	var s store.Store
	// Assign Close to a variable so the compiler proves the method exists.
	_ = s.Close
}
