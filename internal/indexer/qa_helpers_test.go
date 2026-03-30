package indexer

import (
	"context"
	"reflect"
	"unsafe"

	"github.com/ethereum/go-ethereum/common"
)

// listCaughtUpFunc is the function signature for ListCaughtUpFn on mockWatchedContractRepo.
type listCaughtUpFunc = func(ctx context.Context, chainID int64, globalCursor uint64) ([]common.Address, error)

// setupWatchedContracts configures the mock store to return the given addresses
// from ListCaughtUp. On branches without the watched contract framework (where
// mockStore has no watchedContractRepo field), this is a no-op.
func setupWatchedContracts(s *mockStore, addrs []common.Address) {
	setListCaughtUpFn(s, func(_ context.Context, _ int64, _ uint64) ([]common.Address, error) {
		return addrs, nil
	})
}

// setupWatchedContractsDynamic configures the mock store with a dynamic
// ListCaughtUp function. On branches without the watched contract framework,
// this is a no-op.
func setupWatchedContractsDynamic(s *mockStore, provider func() []common.Address) {
	setListCaughtUpFn(s, func(_ context.Context, _ int64, _ uint64) ([]common.Address, error) {
		return provider(), nil
	})
}

// setListCaughtUpFn uses reflection to set the ListCaughtUpFn on the mock
// store's watchedContractRepo, if it exists. All access is done through
// reflection to avoid compile-time dependency on types that may not exist
// on all branches.
func setListCaughtUpFn(s *mockStore, fn listCaughtUpFunc) {
	storeVal := reflect.ValueOf(s).Elem()

	// Check if mockStore has a watchedContractRepo field.
	repoField := storeVal.FieldByName("watchedContractRepo")
	if !repoField.IsValid() || repoField.IsNil() {
		return
	}

	// Get the underlying struct (dereference the pointer).
	// Use unsafe to access the unexported pointer field.
	repoPtr := reflect.NewAt(repoField.Type(), unsafe.Pointer(repoField.UnsafeAddr())).Elem()
	repoStruct := repoPtr.Elem()

	// Find and set the ListCaughtUpFn field on the repo struct.
	fnField := repoStruct.FieldByName("ListCaughtUpFn")
	if !fnField.IsValid() {
		return
	}

	// Set the function using unsafe to bypass unexported field restrictions.
	fnFieldSettable := reflect.NewAt(fnField.Type(), unsafe.Pointer(fnField.UnsafeAddr())).Elem()
	fnFieldSettable.Set(reflect.ValueOf(fn))
}
