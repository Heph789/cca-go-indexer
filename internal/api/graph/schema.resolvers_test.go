package graph

import (
	"context"
	"fmt"
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

// mockBidRepo is a function-pointer mock for store.BidRepository.
type mockBidRepo struct {
	GetPrevTickPriceFn func(ctx context.Context, chainID int64, auctionAddress string, maxPrice string) (string, error)
}

func (m *mockBidRepo) Insert(_ context.Context, _ *cca.Bid) error              { return nil }
func (m *mockBidRepo) DeleteFromBlock(_ context.Context, _ int64, _ uint64) error { return nil }
func (m *mockBidRepo) ListByAuction(_ context.Context, _ int64, _ string, _ store.PaginationParams) ([]*cca.Bid, error) {
	return nil, nil
}
func (m *mockBidRepo) ListByAuctionAndOwner(_ context.Context, _ int64, _ string, _ string, _ store.PaginationParams) ([]*cca.Bid, error) {
	return nil, nil
}
func (m *mockBidRepo) GetPrevTickPrice(ctx context.Context, chainID int64, auctionAddress string, maxPrice string) (string, error) {
	if m.GetPrevTickPriceFn != nil {
		return m.GetPrevTickPriceFn(ctx, chainID, auctionAddress, maxPrice)
	}
	return "", nil
}

// mockStore wires together the mock repos and satisfies store.Store.
type mockStore struct {
	auctionRepo    *mockAuctionRepo
	bidRepo        *mockBidRepo
	checkpointRepo *mockCheckpointRepo
}

func newMockStore() *mockStore {
	return &mockStore{
		auctionRepo:    &mockAuctionRepo{},
		bidRepo:        &mockBidRepo{},
		checkpointRepo: &mockCheckpointRepo{},
	}
}

func (m *mockStore) AuctionRepo() store.AuctionRepository       { return m.auctionRepo }
func (m *mockStore) BidRepo() store.BidRepository               { return m.bidRepo }
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

const testChainID int64 = 1

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

func intPtr(n int) *int       { return &n }
func strPtr(s string) *string { return &s }

var cmpOpts = []cmp.Option{
	cmpopts.IgnoreFields(cca.Auction{}, "CreatedAt", "UpdatedAt"),
	cmpopts.IgnoreUnexported(big.Int{}),
}

// ---------------------------------------------------------------------------
// Query.auction resolver tests
// ---------------------------------------------------------------------------

// TestQueryResolver_Auction tests the auction(address) query resolver.
func TestQueryResolver_Auction(t *testing.T) {
	wantAddr := common.HexToAddress("0x1111111111111111111111111111111111111111")
	wantAuction := testAuction("0x1111111111111111111111111111111111111111", 10, 0)

	tests := []struct {
		name        string
		addr        common.Address
		setupFn     func(repo *mockAuctionRepo)
		wantAuction *cca.Auction
		wantErr     bool
	}{
		{
			name: "returns auction from store lookup",
			addr: wantAddr,
			setupFn: func(repo *mockAuctionRepo) {
				repo.GetByAddressFn = func(_ context.Context, _ int64, _ string) (*cca.Auction, error) {
					return wantAuction, nil
				}
			},
			wantAuction: wantAuction,
		},
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

// TestQueryResolver_Auctions tests pagination, cursors, default/clamped limits,
// hasNextPage via N+1 fetch, and correct edge/cursor construction.
func TestQueryResolver_Auctions(t *testing.T) {
	auction1 := testAuction("0x1111111111111111111111111111111111111111", 10, 0)
	auction2 := testAuction("0x2222222222222222222222222222222222222222", 20, 1)
	auction3 := testAuction("0x3333333333333333333333333333333333333333", 30, 2)

	wantCursor1 := pagination.EncodeCursor(auction1.BlockNumber, auction1.LogIndex)
	wantCursor2 := pagination.EncodeCursor(auction2.BlockNumber, auction2.LogIndex)

	tests := []struct {
		name            string
		first           *int
		after           *string
		mockAuctions    []*cca.Auction
		wantEdgeCount   int
		wantHasNextPage bool
		wantEndCursor   *string
		wantFirstCursor string
	}{
		{
			name:            "returns paginated edges with correct cursors",
			first:           intPtr(2),
			mockAuctions:    []*cca.Auction{auction1, auction2},
			wantEdgeCount:   2,
			wantHasNextPage: false,
			wantEndCursor:   strPtr(wantCursor2),
			wantFirstCursor: wantCursor1,
		},
		{
			name:            "sets hasNextPage true when store returns N+1 items",
			first:           intPtr(2),
			mockAuctions:    []*cca.Auction{auction1, auction2, auction3},
			wantEdgeCount:   2,
			wantHasNextPage: true,
			wantEndCursor:   strPtr(wantCursor2),
			wantFirstCursor: wantCursor1,
		},
		{
			name:            "defaults limit to 20 when first is nil",
			first:           nil,
			mockAuctions:    nil,
			wantEdgeCount:   0,
			wantHasNextPage: false,
			wantEndCursor:   nil,
		},
		{
			name:            "clamps limit to max 100",
			first:           intPtr(200),
			mockAuctions:    nil,
			wantEdgeCount:   0,
			wantHasNextPage: false,
			wantEndCursor:   nil,
		},
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

			if len(got.Edges) != tt.wantEdgeCount {
				t.Errorf("len(Edges) = %d, want %d", len(got.Edges), tt.wantEdgeCount)
			}

			if got.PageInfo == nil {
				t.Fatal("PageInfo is nil")
			}
			if got.PageInfo.HasNextPage != tt.wantHasNextPage {
				t.Errorf("HasNextPage = %v, want %v", got.PageInfo.HasNextPage, tt.wantHasNextPage)
			}

			if diff := cmp.Diff(tt.wantEndCursor, got.PageInfo.EndCursor); diff != "" {
				t.Errorf("EndCursor mismatch (-want +got):\n%s", diff)
			}

			if tt.wantEdgeCount > 0 && len(got.Edges) > 0 {
				if got.Edges[0].Cursor != tt.wantFirstCursor {
					t.Errorf("Edges[0].Cursor = %q, want %q", got.Edges[0].Cursor, tt.wantFirstCursor)
				}
			}
		})
	}
}

// TestQueryResolver_Auctions_PassesCursorToStore verifies that an "after" cursor
// is decoded and passed as CursorBlockNumber/CursorLogIndex to the store.
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
// field resolvers that convert uint64 to int for GraphQL.
func TestAuctionResolver_BlockFields(t *testing.T) {
	auctionObj := &cca.Auction{
		StartBlock: 100,
		EndBlock:   200,
		ClaimBlock: 250,
	}

	ms := newMockStore()
	r := newTestResolver(ms)
	ar := r.Auction()
	ctx := context.Background()

	gotStart, err := ar.StartBlock(ctx, auctionObj)
	if err != nil {
		t.Fatalf("StartBlock() error = %v", err)
	}
	if gotStart != 100 {
		t.Errorf("StartBlock() = %d, want 100", gotStart)
	}

	gotEnd, err := ar.EndBlock(ctx, auctionObj)
	if err != nil {
		t.Fatalf("EndBlock() error = %v", err)
	}
	if gotEnd != 200 {
		t.Errorf("EndBlock() = %d, want 200", gotEnd)
	}

	gotClaim, err := ar.ClaimBlock(ctx, auctionObj)
	if err != nil {
		t.Fatalf("ClaimBlock() error = %v", err)
	}
	if gotClaim != 250 {
		t.Errorf("ClaimBlock() = %d, want 250", gotClaim)
	}
}

// TestAuctionResolver_ClearingPriceQ96 tests the clearingPriceQ96 field resolver.
func TestAuctionResolver_ClearingPriceQ96(t *testing.T) {
	auctionAddr := common.HexToAddress("0x1111111111111111111111111111111111111111")
	wantPrice := big.NewInt(999999)

	tests := []struct {
		name      string
		setupFn   func(repo *mockCheckpointRepo)
		wantPrice *big.Int
	}{
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

// ---------------------------------------------------------------------------
// Query.bidHint resolver tests
// ---------------------------------------------------------------------------

// TestQueryResolver_BidHint tests the bidHint(auctionAddress, maxPrice) query resolver.
func TestQueryResolver_BidHint(t *testing.T) {
	auctionAddr := common.HexToAddress("0x1111111111111111111111111111111111111111")
	maxPrice := big.NewInt(500000)
	wantPrevTickPrice := big.NewInt(400000)
	wantFloorPrice := big.NewInt(100000)

	tests := []struct {
		name             string
		setupBidRepo     func(repo *mockBidRepo)
		setupAuctionRepo func(repo *mockAuctionRepo)
		wantPrice        *big.Int
		wantErr          bool
	}{
		{
			name: "returns prevTickPrice from bid repo when bid exists below maxPrice",
			setupBidRepo: func(repo *mockBidRepo) {
				repo.GetPrevTickPriceFn = func(_ context.Context, _ int64, _ string, _ string) (string, error) {
					return "400000", nil
				}
			},
			wantPrice: wantPrevTickPrice,
		},
		{
			name: "falls back to auction floorPrice when no bid price exists below maxPrice",
			setupBidRepo: func(repo *mockBidRepo) {
				repo.GetPrevTickPriceFn = func(_ context.Context, _ int64, _ string, _ string) (string, error) {
					return "", nil
				}
			},
			setupAuctionRepo: func(repo *mockAuctionRepo) {
				repo.GetByAddressFn = func(_ context.Context, _ int64, _ string) (*cca.Auction, error) {
					return &cca.Auction{
						AuctionAddress: auctionAddr,
						FloorPrice:     wantFloorPrice,
						ChainID:        testChainID,
					}, nil
				}
			},
			wantPrice: wantFloorPrice,
		},
		{
			name: "returns error when auction not found and no prev tick price",
			setupBidRepo: func(repo *mockBidRepo) {
				repo.GetPrevTickPriceFn = func(_ context.Context, _ int64, _ string, _ string) (string, error) {
					return "", nil
				}
			},
			setupAuctionRepo: func(repo *mockAuctionRepo) {
				repo.GetByAddressFn = func(_ context.Context, _ int64, _ string) (*cca.Auction, error) {
					return nil, nil
				}
			},
			wantErr: true,
		},
		{
			name: "returns error when bid repo fails",
			setupBidRepo: func(repo *mockBidRepo) {
				repo.GetPrevTickPriceFn = func(_ context.Context, _ int64, _ string, _ string) (string, error) {
					return "", fmt.Errorf("db connection lost")
				}
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ms := newMockStore()
			if tt.setupBidRepo != nil {
				tt.setupBidRepo(ms.bidRepo)
			}
			if tt.setupAuctionRepo != nil {
				tt.setupAuctionRepo(ms.auctionRepo)
			}
			r := newTestResolver(ms)
			qr := r.Query()

			got, err := qr.BidHint(context.Background(), auctionAddr, maxPrice)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("BidHint() error = %v", err)
			}
			if got == nil {
				t.Fatal("BidHint() returned nil result")
			}
			if got.PrevTickPrice == nil {
				t.Fatal("BidHint().PrevTickPrice is nil, want non-nil")
			}
			if got.PrevTickPrice.Cmp(tt.wantPrice) != 0 {
				t.Errorf("BidHint().PrevTickPrice = %s, want %s", got.PrevTickPrice.String(), tt.wantPrice.String())
			}
		})
	}
}
