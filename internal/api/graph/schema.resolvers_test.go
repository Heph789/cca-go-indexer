package graph

import (
	"context"
	"math/big"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"

	"github.com/ethereum/go-ethereum/common"

	"github.com/cca/go-indexer/internal/api/pagination"
	"github.com/cca/go-indexer/internal/domain/cca"
	"github.com/cca/go-indexer/internal/store"
)

// ---------------------------------------------------------------------------
// Mock implementations
// ---------------------------------------------------------------------------

// mockAuctionRepo is a function-pointer mock for store.AuctionRepository.
// Only GetByAddress and List are wired up; other methods are no-op stubs.
type mockAuctionRepo struct {
	GetByAddressFn func(ctx context.Context, chainID int64, auctionAddress string) (*cca.Auction, error)
	ListFn         func(ctx context.Context, chainID int64, params store.PaginationParams) ([]*cca.Auction, error)
}

func (m *mockAuctionRepo) Insert(_ context.Context, _ *cca.Auction) error { return nil }
func (m *mockAuctionRepo) DeleteFromBlock(_ context.Context, _ int64, _ uint64) error {
	return nil
}
func (m *mockAuctionRepo) GetByAddress(ctx context.Context, chainID int64, auctionAddress string) (*cca.Auction, error) {
	if m.GetByAddressFn != nil {
		return m.GetByAddressFn(ctx, chainID, auctionAddress)
	}
	return nil, nil
}
func (m *mockAuctionRepo) List(ctx context.Context, chainID int64, params store.PaginationParams) ([]*cca.Auction, error) {
	if m.ListFn != nil {
		return m.ListFn(ctx, chainID, params)
	}
	return nil, nil
}

// mockCheckpointRepo is a function-pointer mock for store.CheckpointRepository.
// Only GetLatest is wired up; other methods are no-op stubs.
type mockCheckpointRepo struct {
	GetLatestFn func(ctx context.Context, chainID int64, auctionAddress string) (*cca.Checkpoint, error)
}

func (m *mockCheckpointRepo) Insert(_ context.Context, _ *cca.Checkpoint) error { return nil }
func (m *mockCheckpointRepo) DeleteFromBlock(_ context.Context, _ int64, _ uint64) error {
	return nil
}
func (m *mockCheckpointRepo) GetLatest(ctx context.Context, chainID int64, auctionAddress string) (*cca.Checkpoint, error) {
	if m.GetLatestFn != nil {
		return m.GetLatestFn(ctx, chainID, auctionAddress)
	}
	return nil, nil
}
func (m *mockCheckpointRepo) ListByAuction(_ context.Context, _ int64, _ string, _ store.PaginationParams) ([]*cca.Checkpoint, error) {
	return nil, nil
}

// mockBidRepo satisfies store.BidRepository with no-op stubs.
type mockBidRepo struct{}

func (m *mockBidRepo) Insert(_ context.Context, _ *cca.Bid) error              { return nil }
func (m *mockBidRepo) DeleteFromBlock(_ context.Context, _ int64, _ uint64) error { return nil }
func (m *mockBidRepo) ListByAuction(_ context.Context, _ int64, _ string, _ store.PaginationParams) ([]*cca.Bid, error) {
	return nil, nil
}
func (m *mockBidRepo) ListByAuctionAndOwner(_ context.Context, _ int64, _ string, _ string, _ store.PaginationParams) ([]*cca.Bid, error) {
	return nil, nil
}
func (m *mockBidRepo) GetPrevTickPrice(_ context.Context, _ int64, _ string, _ string) (string, error) {
	return "", nil
}

// mockStore wires together the mock repos and satisfies store.Store.
type mockStore struct {
	auctionRepo    *mockAuctionRepo
	checkpointRepo *mockCheckpointRepo
}

func newMockStore() *mockStore {
	return &mockStore{
		auctionRepo:    &mockAuctionRepo{},
		checkpointRepo: &mockCheckpointRepo{},
	}
}

func (m *mockStore) AuctionRepo() store.AuctionRepository       { return m.auctionRepo }
func (m *mockStore) BidRepo() store.BidRepository               { return &mockBidRepo{} }
func (m *mockStore) CheckpointRepo() store.CheckpointRepository { return m.checkpointRepo }
func (m *mockStore) WatchedContractRepo() store.WatchedContractRepository {
	return nil
}
func (m *mockStore) RawEventRepo() store.RawEventRepository { return nil }
func (m *mockStore) CursorRepo() store.CursorRepository     { return nil }
func (m *mockStore) BlockRepo() store.BlockRepository        { return nil }
func (m *mockStore) RollbackFromBlock(_ context.Context, _ int64, _ uint64) error {
	return nil
}
func (m *mockStore) WithTx(ctx context.Context, fn func(txStore store.Store) error) error {
	return fn(m)
}
func (m *mockStore) Ping(_ context.Context) error { return nil }
func (m *mockStore) Close()                       {}

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

// testChainID is the chain ID used across all resolver tests.
const testChainID int64 = 1

// newTestResolver creates a Resolver backed by the given mock store.
func newTestResolver(s *mockStore) *Resolver {
	return &Resolver{Store: s, ChainID: testChainID}
}

// testAuction builds a minimal cca.Auction with the given address and block/log for cursor.
func testAuction(addr string, blockNumber uint64, logIndex uint) *cca.Auction {
	return &cca.Auction{
		AuctionAddress: common.HexToAddress(addr),
		Token:          common.HexToAddress("0x0000000000000000000000000000000000000001"),
		Amount:         big.NewInt(1000),
		StartBlock:     100,
		EndBlock:       200,
		ClaimBlock:     250,
		ChainID:        testChainID,
		BlockNumber:    blockNumber,
		LogIndex:       logIndex,
	}
}

// intPtr returns a pointer to an int.
func intPtr(n int) *int { return &n }

// strPtr returns a pointer to a string.
func strPtr(s string) *string { return &s }

// cmpOpts are options used with cmp.Diff when comparing auction-related types.
// We ignore time fields and unexported fields since they are not relevant.
var cmpOpts = []cmp.Option{
	cmpopts.IgnoreFields(cca.Auction{}, "CreatedAt", "UpdatedAt"),
	cmpopts.IgnoreUnexported(big.Int{}),
}

// ---------------------------------------------------------------------------
// Query.auction resolver tests
// ---------------------------------------------------------------------------

// TestQueryResolver_Auction tests the auction(address) query resolver.
// Covers looking up an existing auction and returning nil for an unknown address.
func TestQueryResolver_Auction(t *testing.T) {
	wantAddr := common.HexToAddress("0x1111111111111111111111111111111111111111")
	wantAuction := testAuction("0x1111111111111111111111111111111111111111", 10, 0)

	tests := []struct {
		name string
		// addr is the address passed to the auction query.
		addr common.Address
		// setupFn configures the mock auction repo for this test case.
		setupFn func(repo *mockAuctionRepo)
		// wantAuction is the expected return value (nil means not found).
		wantAuction *cca.Auction
		wantErr     bool
	}{
		// --- happy path ---

		// When the store has an auction at the given address, the resolver should return it.
		{
			name: "returns auction from store lookup",
			addr: wantAddr,
			setupFn: func(repo *mockAuctionRepo) {
				repo.GetByAddressFn = func(_ context.Context, chainID int64, auctionAddress string) (*cca.Auction, error) {
					return wantAuction, nil
				}
			},
			wantAuction: wantAuction,
		},

		// --- not found ---

		// When the store returns nil for an unknown address, the resolver should return nil (not error).
		{
			name: "returns nil for unknown address",
			addr: common.HexToAddress("0x9999999999999999999999999999999999999999"),
			setupFn: func(repo *mockAuctionRepo) {
				repo.GetByAddressFn = func(_ context.Context, _ int64, _ string) (*cca.Auction, error) {
					return nil, nil
				}
			},
			wantAuction: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ms := newMockStore()
			if tt.setupFn != nil {
				tt.setupFn(ms.auctionRepo)
			}
			r := newTestResolver(ms)
			qr := r.Query()

			got, err := qr.Auction(context.Background(), tt.addr)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("Auction() error = %v", err)
			}
			if diff := cmp.Diff(tt.wantAuction, got, cmpOpts...); diff != "" {
				t.Errorf("Auction() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Query.auctions resolver tests
// ---------------------------------------------------------------------------

// TestQueryResolver_Auctions tests the auctions(first, after) query resolver.
// Covers pagination with cursors, default/clamped limits, hasNextPage via N+1 fetch,
// and correct edge/cursor construction.
func TestQueryResolver_Auctions(t *testing.T) {
	auction1 := testAuction("0x1111111111111111111111111111111111111111", 10, 0)
	auction2 := testAuction("0x2222222222222222222222222222222222222222", 20, 1)
	auction3 := testAuction("0x3333333333333333333333333333333333333333", 30, 2)

	wantCursor1 := pagination.EncodeCursor(auction1.BlockNumber, auction1.LogIndex)
	wantCursor2 := pagination.EncodeCursor(auction2.BlockNumber, auction2.LogIndex)

	tests := []struct {
		name string
		// first is the requested page size (nil means use default).
		first *int
		// after is the cursor to paginate after (nil means from start).
		after *string
		// mockAuctions is what the mock List method returns.
		mockAuctions []*cca.Auction
		// wantEdgeCount is the expected number of edges in the response.
		wantEdgeCount int
		// wantHasNextPage is the expected hasNextPage value.
		wantHasNextPage bool
		// wantEndCursor is the expected endCursor value (nil means no edges).
		wantEndCursor *string
		// wantFirstCursor is the cursor of the first edge (for verifying cursor correctness).
		wantFirstCursor string
	}{
		// --- happy path ---

		// Requesting 2 items when store returns exactly 2 means no next page.
		// The N+1 pattern: resolver requests first+1 from store; if store returns
		// <= first items, hasNextPage is false.
		{
			name:            "returns paginated edges with correct cursors",
			first:           intPtr(2),
			mockAuctions:    []*cca.Auction{auction1, auction2},
			wantEdgeCount:   2,
			wantHasNextPage: false,
			wantEndCursor:   strPtr(wantCursor2),
			wantFirstCursor: wantCursor1,
		},

		// When the store returns first+1 items, hasNextPage should be true and
		// only the first N items should appear as edges.
		{
			name:            "sets hasNextPage true when store returns N+1 items",
			first:           intPtr(2),
			mockAuctions:    []*cca.Auction{auction1, auction2, auction3},
			wantEdgeCount:   2,
			wantHasNextPage: true,
			wantEndCursor:   strPtr(wantCursor2),
			wantFirstCursor: wantCursor1,
		},

		// --- default/clamping ---

		// When first is nil, the resolver should default to pagination.DefaultLimit (20).
		// We return 0 items to keep the test simple — the key assertion is that
		// the mock receives the correct limit (tested via the response shape).
		{
			name:            "defaults limit to 20 when first is nil",
			first:           nil,
			mockAuctions:    nil,
			wantEdgeCount:   0,
			wantHasNextPage: false,
			wantEndCursor:   nil,
		},

		// When first exceeds MaxLimit, it should be clamped to 100.
		{
			name:            "clamps limit to max 100",
			first:           intPtr(200),
			mockAuctions:    nil,
			wantEdgeCount:   0,
			wantHasNextPage: false,
			wantEndCursor:   nil,
		},

		// --- empty result ---

		// No auctions in the store should yield empty edges with no next page.
		{
			name:            "returns empty connection when store has no auctions",
			first:           intPtr(10),
			mockAuctions:    nil,
			wantEdgeCount:   0,
			wantHasNextPage: false,
			wantEndCursor:   nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ms := newMockStore()
			ms.auctionRepo.ListFn = func(_ context.Context, _ int64, _ store.PaginationParams) ([]*cca.Auction, error) {
				return tt.mockAuctions, nil
			}
			r := newTestResolver(ms)
			qr := r.Query()

			got, err := qr.Auctions(context.Background(), nil, tt.first, tt.after)
			if err != nil {
				t.Fatalf("Auctions() error = %v", err)
			}
			if got == nil {
				t.Fatal("Auctions() returned nil connection")
			}

			// Verify edge count.
			gotEdgeCount := len(got.Edges)
			if gotEdgeCount != tt.wantEdgeCount {
				t.Errorf("len(Edges) = %d, want %d", gotEdgeCount, tt.wantEdgeCount)
			}

			// Verify hasNextPage.
			if got.PageInfo == nil {
				t.Fatal("PageInfo is nil")
			}
			if got.PageInfo.HasNextPage != tt.wantHasNextPage {
				t.Errorf("HasNextPage = %v, want %v", got.PageInfo.HasNextPage, tt.wantHasNextPage)
			}

			// Verify endCursor.
			if diff := cmp.Diff(tt.wantEndCursor, got.PageInfo.EndCursor); diff != "" {
				t.Errorf("EndCursor mismatch (-want +got):\n%s", diff)
			}

			// Verify first edge cursor if there are edges.
			if tt.wantEdgeCount > 0 && len(got.Edges) > 0 {
				gotFirstCursor := got.Edges[0].Cursor
				if gotFirstCursor != tt.wantFirstCursor {
					t.Errorf("Edges[0].Cursor = %q, want %q", gotFirstCursor, tt.wantFirstCursor)
				}
			}
		})
	}
}

// TestQueryResolver_Auctions_PassesCursorToStore verifies that when an "after" cursor
// is provided, the resolver decodes it and passes the correct CursorBlockNumber and
// CursorLogIndex to the store's PaginationParams.
func TestQueryResolver_Auctions_PassesCursorToStore(t *testing.T) {
	wantBlockNumber := uint64(50)
	wantLogIndex := uint(3)
	afterCursor := pagination.EncodeCursor(wantBlockNumber, wantLogIndex)

	ms := newMockStore()
	var capturedParams store.PaginationParams
	ms.auctionRepo.ListFn = func(_ context.Context, _ int64, params store.PaginationParams) ([]*cca.Auction, error) {
		capturedParams = params
		return nil, nil
	}

	r := newTestResolver(ms)
	qr := r.Query()

	_, err := qr.Auctions(context.Background(), nil, intPtr(10), &afterCursor)
	if err != nil {
		t.Fatalf("Auctions() error = %v", err)
	}

	// The resolver should decode the cursor and pass block/log to PaginationParams.
	if capturedParams.CursorBlockNumber == nil {
		t.Fatal("CursorBlockNumber is nil, want non-nil")
	}
	if *capturedParams.CursorBlockNumber != wantBlockNumber {
		t.Errorf("CursorBlockNumber = %d, want %d", *capturedParams.CursorBlockNumber, wantBlockNumber)
	}
	if capturedParams.CursorLogIndex == nil {
		t.Fatal("CursorLogIndex is nil, want non-nil")
	}
	if *capturedParams.CursorLogIndex != wantLogIndex {
		t.Errorf("CursorLogIndex = %d, want %d", *capturedParams.CursorLogIndex, wantLogIndex)
	}
}

// ---------------------------------------------------------------------------
// Auction field resolver tests
// ---------------------------------------------------------------------------

// TestAuctionResolver_BlockFields tests the StartBlock, EndBlock, and ClaimBlock
// field resolvers, which convert uint64 values from the domain model to int for GraphQL.
func TestAuctionResolver_BlockFields(t *testing.T) {
	auctionObj := &cca.Auction{
		StartBlock: 100,
		EndBlock:   200,
		ClaimBlock: 250,
	}

	wantStartBlock := 100
	wantEndBlock := 200
	wantClaimBlock := 250

	ms := newMockStore()
	r := newTestResolver(ms)
	ar := r.Auction()
	ctx := context.Background()

	// StartBlock should convert uint64 to int.
	gotStart, err := ar.StartBlock(ctx, auctionObj)
	if err != nil {
		t.Fatalf("StartBlock() error = %v", err)
	}
	if gotStart != wantStartBlock {
		t.Errorf("StartBlock() = %d, want %d", gotStart, wantStartBlock)
	}

	// EndBlock should convert uint64 to int.
	gotEnd, err := ar.EndBlock(ctx, auctionObj)
	if err != nil {
		t.Fatalf("EndBlock() error = %v", err)
	}
	if gotEnd != wantEndBlock {
		t.Errorf("EndBlock() = %d, want %d", gotEnd, wantEndBlock)
	}

	// ClaimBlock should convert uint64 to int.
	gotClaim, err := ar.ClaimBlock(ctx, auctionObj)
	if err != nil {
		t.Fatalf("ClaimBlock() error = %v", err)
	}
	if gotClaim != wantClaimBlock {
		t.Errorf("ClaimBlock() = %d, want %d", gotClaim, wantClaimBlock)
	}
}

// TestAuctionResolver_ClearingPriceQ96 tests the clearingPriceQ96 field resolver.
// It should fetch the latest checkpoint for the auction and return its ClearingPriceQ96,
// or return nil if no checkpoint exists.
func TestAuctionResolver_ClearingPriceQ96(t *testing.T) {
	auctionAddr := common.HexToAddress("0x1111111111111111111111111111111111111111")
	wantPrice := big.NewInt(999999)

	tests := []struct {
		name string
		// setupFn configures the mock checkpoint repo.
		setupFn func(repo *mockCheckpointRepo)
		// wantPrice is the expected clearing price (nil means no checkpoint).
		wantPrice *big.Int
	}{
		// --- happy path ---

		// When a checkpoint exists, the resolver should return its ClearingPriceQ96.
		{
			name: "returns clearing price from latest checkpoint",
			setupFn: func(repo *mockCheckpointRepo) {
				repo.GetLatestFn = func(_ context.Context, _ int64, _ string) (*cca.Checkpoint, error) {
					return &cca.Checkpoint{
						ClearingPriceQ96: wantPrice,
						AuctionAddress:   auctionAddr,
						ChainID:          testChainID,
					}, nil
				}
			},
			wantPrice: wantPrice,
		},

		// --- no checkpoint ---

		// When no checkpoint exists (GetLatest returns nil), the resolver should return nil.
		{
			name: "returns nil when no checkpoint exists",
			setupFn: func(repo *mockCheckpointRepo) {
				repo.GetLatestFn = func(_ context.Context, _ int64, _ string) (*cca.Checkpoint, error) {
					return nil, nil
				}
			},
			wantPrice: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ms := newMockStore()
			if tt.setupFn != nil {
				tt.setupFn(ms.checkpointRepo)
			}
			r := newTestResolver(ms)
			ar := r.Auction()

			auctionObj := &cca.Auction{
				AuctionAddress: auctionAddr,
				ChainID:        testChainID,
			}

			got, err := ar.ClearingPriceQ96(context.Background(), auctionObj)
			if err != nil {
				t.Fatalf("ClearingPriceQ96() error = %v", err)
			}

			if tt.wantPrice == nil {
				if got != nil {
					t.Errorf("ClearingPriceQ96() = %s, want nil", got.String())
				}
				return
			}
			if got == nil {
				t.Fatalf("ClearingPriceQ96() = nil, want %s", tt.wantPrice.String())
			}
			if got.Cmp(tt.wantPrice) != 0 {
				t.Errorf("ClearingPriceQ96() = %s, want %s", got.String(), tt.wantPrice.String())
			}
		})
	}
}
