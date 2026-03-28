package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/google/go-cmp/cmp"

	"github.com/cca/go-indexer/internal/api/httputil"
	"github.com/cca/go-indexer/internal/domain/cca"
	"github.com/cca/go-indexer/internal/store"
)

type mockAuctionRepo struct {
	GetByAddressFn func(ctx context.Context, chainID int64, auctionAddress string) (*cca.Auction, error)
}

func (m *mockAuctionRepo) Insert(ctx context.Context, auction *cca.Auction) error {
	return nil
}

func (m *mockAuctionRepo) DeleteFromBlock(ctx context.Context, chainID int64, fromBlock uint64) error {
	return nil
}

func (m *mockAuctionRepo) GetByAddress(ctx context.Context, chainID int64, auctionAddress string) (*cca.Auction, error) {
	if m.GetByAddressFn != nil {
		return m.GetByAddressFn(ctx, chainID, auctionAddress)
	}
	return nil, nil
}

type mockStore struct {
	auctionRepo *mockAuctionRepo
}

func (m *mockStore) AuctionRepo() store.AuctionRepository  { return m.auctionRepo }
func (m *mockStore) RawEventRepo() store.RawEventRepository { return nil }
func (m *mockStore) CursorRepo() store.CursorRepository     { return nil }
func (m *mockStore) BlockRepo() store.BlockRepository       { return nil }
func (m *mockStore) WithTx(ctx context.Context, fn func(txStore store.Store) error) error {
	return fn(m)
}
func (m *mockStore) Close() {}

const testChainID int64 = 324

func validAddress() string {
	return "0x1234567890abcdef1234567890abcdef12345678"
}

func newTestAuction() *cca.Auction {
	return &cca.Auction{
		AuctionAddress:         common.HexToAddress("0xABcdEF1234567890abCDef1234567890AbCdEf12"),
		Token:                  common.HexToAddress("0x1111111111111111111111111111111111111111"),
		Amount:                 big.NewInt(1000000),
		Currency:               common.HexToAddress("0x2222222222222222222222222222222222222222"),
		TokensRecipient:        common.HexToAddress("0x3333333333333333333333333333333333333333"),
		FundsRecipient:         common.HexToAddress("0x4444444444444444444444444444444444444444"),
		StartBlock:             100,
		EndBlock:               200,
		ClaimBlock:             300,
		TickSpacing:            big.NewInt(60),
		ValidationHook:         common.HexToAddress("0x5555555555555555555555555555555555555555"),
		FloorPrice:             big.NewInt(500),
		RequiredCurrencyRaised: big.NewInt(9999),
		ChainID:                testChainID,
		BlockNumber:            50,
		TxHash:                 common.HexToHash("0xdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef"),
		LogIndex:               3,
	}
}

func setupMux(s store.Store) *http.ServeMux {
	handler := &AuctionHandler{Store: s, ChainID: testChainID}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/auctions/{address}", handler.Get)
	return mux
}

func TestAuctionHandler_Get(t *testing.T) {
	t.Run("returns 200 with auction data for valid address", func(t *testing.T) {
		auction := newTestAuction()
		ms := &mockStore{auctionRepo: &mockAuctionRepo{
			GetByAddressFn: func(ctx context.Context, chainID int64, addr string) (*cca.Auction, error) {
				return auction, nil
			},
		}}
		mux := setupMux(ms)

		req := httptest.NewRequest(http.MethodGet, "/api/v1/auctions/"+validAddress(), nil)
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
		}

		var resp httputil.Response
		resp.Data = &AuctionResponse{}
		if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
			t.Fatalf("decode error: %v", err)
		}

		ar := resp.Data.(*AuctionResponse)
		if ar.StartBlock != auction.StartBlock {
			t.Errorf("expected StartBlock %d, got %d", auction.StartBlock, ar.StartBlock)
		}
		if ar.Amount != auction.Amount.String() {
			t.Errorf("expected Amount %q, got %q", auction.Amount.String(), ar.Amount)
		}
	})

	t.Run("returns 404 for non-existent auction", func(t *testing.T) {
		ms := &mockStore{auctionRepo: &mockAuctionRepo{
			GetByAddressFn: func(ctx context.Context, chainID int64, addr string) (*cca.Auction, error) {
				return nil, nil
			},
		}}
		mux := setupMux(ms)

		req := httptest.NewRequest(http.MethodGet, "/api/v1/auctions/"+validAddress(), nil)
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, req)

		if rr.Code != http.StatusNotFound {
			t.Fatalf("expected 404, got %d: %s", rr.Code, rr.Body.String())
		}

		var errBody httputil.ErrorBody
		if err := json.NewDecoder(rr.Body).Decode(&errBody); err != nil {
			t.Fatalf("decode error: %v", err)
		}
		if errBody.Error.Code != httputil.CodeNotFound {
			t.Errorf("expected code %q, got %q", httputil.CodeNotFound, errBody.Error.Code)
		}
	})

	t.Run("returns 400 for invalid address wrong length", func(t *testing.T) {
		ms := &mockStore{auctionRepo: &mockAuctionRepo{}}
		mux := setupMux(ms)

		req := httptest.NewRequest(http.MethodGet, "/api/v1/auctions/0xabc", nil)
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, req)

		if rr.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d: %s", rr.Code, rr.Body.String())
		}
	})

	t.Run("returns 400 for invalid address missing 0x prefix", func(t *testing.T) {
		ms := &mockStore{auctionRepo: &mockAuctionRepo{}}
		mux := setupMux(ms)

		// 42 chars total but no 0x prefix — exercises prefix validation, not length
		req := httptest.NewRequest(http.MethodGet, "/api/v1/auctions/xx1234567890abcdef1234567890abcdef12345678", nil)
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, req)

		if rr.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d: %s", rr.Code, rr.Body.String())
		}
	})

	t.Run("returns 400 when address is empty", func(t *testing.T) {
		ms := &mockStore{auctionRepo: &mockAuctionRepo{}}
		handler := &AuctionHandler{Store: ms, ChainID: testChainID}

		req := httptest.NewRequest(http.MethodGet, "/api/v1/auctions/", nil)
		rr := httptest.NewRecorder()
		handler.Get(rr, req)

		if rr.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d: %s", rr.Code, rr.Body.String())
		}
	})

	t.Run("returns 500 when store errors", func(t *testing.T) {
		ms := &mockStore{auctionRepo: &mockAuctionRepo{
			GetByAddressFn: func(ctx context.Context, chainID int64, addr string) (*cca.Auction, error) {
				return nil, errors.New("db connection lost")
			},
		}}
		mux := setupMux(ms)

		req := httptest.NewRequest(http.MethodGet, "/api/v1/auctions/"+validAddress(), nil)
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, req)

		if rr.Code != http.StatusInternalServerError {
			t.Fatalf("expected 500, got %d: %s", rr.Code, rr.Body.String())
		}

		var errBody httputil.ErrorBody
		if err := json.NewDecoder(rr.Body).Decode(&errBody); err != nil {
			t.Fatalf("decode error: %v", err)
		}
		if errBody.Error.Code != httputil.CodeInternalError {
			t.Errorf("expected code %q, got %q", httputil.CodeInternalError, errBody.Error.Code)
		}
	})

	t.Run("normalizes address to lowercase", func(t *testing.T) {
		mixedCase := "0xABcdEF1234567890abCDef1234567890AbCdEf12"
		var queriedAddr string
		ms := &mockStore{auctionRepo: &mockAuctionRepo{
			GetByAddressFn: func(ctx context.Context, chainID int64, addr string) (*cca.Auction, error) {
				queriedAddr = addr
				return nil, nil
			},
		}}
		mux := setupMux(ms)

		req := httptest.NewRequest(http.MethodGet, "/api/v1/auctions/"+mixedCase, nil)
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, req)

		expected := strings.ToLower(mixedCase)
		if queriedAddr != expected {
			t.Errorf("expected store queried with %q, got %q", expected, queriedAddr)
		}
		// Should still return 404 since mock returns nil
		if rr.Code != http.StatusNotFound {
			t.Fatalf("expected 404, got %d", rr.Code)
		}
	})

	t.Run("response body wraps in data envelope", func(t *testing.T) {
		auction := newTestAuction()
		ms := &mockStore{auctionRepo: &mockAuctionRepo{
			GetByAddressFn: func(ctx context.Context, chainID int64, addr string) (*cca.Auction, error) {
				return auction, nil
			},
		}}
		mux := setupMux(ms)

		req := httptest.NewRequest(http.MethodGet, "/api/v1/auctions/"+validAddress(), nil)
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
		}

		var raw map[string]json.RawMessage
		if err := json.NewDecoder(rr.Body).Decode(&raw); err != nil {
			t.Fatalf("decode error: %v", err)
		}
		if _, ok := raw["data"]; !ok {
			t.Fatal("expected response to have 'data' key")
		}
		if len(raw) != 1 {
			t.Errorf("expected exactly 1 top-level key, got %d", len(raw))
		}
	})
}

func TestToAuctionResponse(t *testing.T) {
	t.Run("maps all fields correctly", func(t *testing.T) {
		auction := newTestAuction()
		resp := toAuctionResponse(auction)

		want := AuctionResponse{
			AuctionAddress:         strings.ToLower(auction.AuctionAddress.Hex()),
			Token:                  strings.ToLower(auction.Token.Hex()),
			Amount:                 auction.Amount.String(),
			Currency:               strings.ToLower(auction.Currency.Hex()),
			TokensRecipient:        strings.ToLower(auction.TokensRecipient.Hex()),
			FundsRecipient:         strings.ToLower(auction.FundsRecipient.Hex()),
			StartBlock:             auction.StartBlock,
			EndBlock:               auction.EndBlock,
			ClaimBlock:             auction.ClaimBlock,
			TickSpacing:            auction.TickSpacing.String(),
			ValidationHook:         strings.ToLower(auction.ValidationHook.Hex()),
			FloorPrice:             auction.FloorPrice.String(),
			RequiredCurrencyRaised: auction.RequiredCurrencyRaised.String(),
			BlockNumber:            auction.BlockNumber,
			TxHash:                 strings.ToLower(auction.TxHash.Hex()),
			LogIndex:               auction.LogIndex,
		}

		if diff := cmp.Diff(want, resp); diff != "" {
			t.Errorf("toAuctionResponse() mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("lowercases addresses", func(t *testing.T) {
		auction := newTestAuction()
		resp := toAuctionResponse(auction)

		addresses := []struct {
			name string
			val  string
		}{
			{"AuctionAddress", resp.AuctionAddress},
			{"Token", resp.Token},
			{"Currency", resp.Currency},
			{"TokensRecipient", resp.TokensRecipient},
			{"FundsRecipient", resp.FundsRecipient},
			{"ValidationHook", resp.ValidationHook},
			{"TxHash", resp.TxHash},
		}
		for _, a := range addresses {
			if a.val != strings.ToLower(a.val) {
				t.Errorf("%s should be lowercase, got %q", a.name, a.val)
			}
		}
	})

	t.Run("converts big.Int to string", func(t *testing.T) {
		auction := newTestAuction()
		resp := toAuctionResponse(auction)

		if resp.Amount != auction.Amount.String() {
			t.Errorf("Amount: expected %q, got %q", auction.Amount.String(), resp.Amount)
		}
		if resp.TickSpacing != auction.TickSpacing.String() {
			t.Errorf("TickSpacing: expected %q, got %q", auction.TickSpacing.String(), resp.TickSpacing)
		}
		if resp.FloorPrice != auction.FloorPrice.String() {
			t.Errorf("FloorPrice: expected %q, got %q", auction.FloorPrice.String(), resp.FloorPrice)
		}
		if resp.RequiredCurrencyRaised != auction.RequiredCurrencyRaised.String() {
			t.Errorf("RequiredCurrencyRaised: expected %q, got %q", auction.RequiredCurrencyRaised.String(), resp.RequiredCurrencyRaised)
		}
	})
}

