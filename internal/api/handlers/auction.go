package handlers

import (
	"log/slog"
	"net/http"
	"strings"

	"github.com/cca/go-indexer/internal/api/httputil"
	"github.com/cca/go-indexer/internal/domain/cca"
	"github.com/cca/go-indexer/internal/store"
)

// AuctionHandler serves auction-related API endpoints.
// Depends on the Store for data access and ChainID to scope queries
// to the correct chain.
type AuctionHandler struct {
	Store   store.Store
	ChainID int64
	Logger  *slog.Logger
}

// AuctionResponse is the JSON representation of an auction.
// Separate from the domain type so the API contract is decoupled
// from internal representation — fields can be renamed, omitted,
// or formatted differently without changing domain code.
type AuctionResponse struct {
	AuctionAddress string `json:"auction_address"`
	TokenOut       string `json:"token_out"`
	CurrencyIn     string `json:"currency_in"`
	Owner          string `json:"owner"`
	StartTime      uint64 `json:"start_time"`
	EndTime        uint64 `json:"end_time"`
	BlockNumber    uint64 `json:"block_number"`
	TxHash         string `json:"tx_hash"`
	LogIndex       uint   `json:"log_index"`
	CreatedAt      string `json:"created_at"` // RFC3339
}

// toAuctionResponse maps a domain Auction to its API representation.
// Addresses are lowercased hex with 0x prefix (EIP-55 checksum could
// be added here if needed by frontend clients).
func toAuctionResponse(a *cca.Auction) AuctionResponse {
	return AuctionResponse{
		AuctionAddress: strings.ToLower(a.AuctionAddress.Hex()),
		TokenOut:       strings.ToLower(a.TokenOut.Hex()),
		CurrencyIn:     strings.ToLower(a.CurrencyIn.Hex()),
		Owner:          strings.ToLower(a.Owner.Hex()),
		StartTime:      a.StartTime,
		EndTime:        a.EndTime,
		BlockNumber:    a.BlockNumber,
		TxHash:         a.TxHash.Hex(),
		LogIndex:       a.LogIndex,
		CreatedAt:      a.CreatedAt.UTC().Format("2006-01-02T15:04:05Z"),
	}
}

// Get handles GET /api/v1/auctions/{address}
// Returns a single auction by its on-chain contract address.
// The address path parameter is normalized to lowercase for lookup.
func (h *AuctionHandler) Get(w http.ResponseWriter, r *http.Request) {
	address := r.PathValue("address")
	if address == "" {
		httputil.WriteError(w, http.StatusBadRequest, "bad_request", "address is required")
		return
	}

	// Basic validation — Ethereum addresses are 42 chars (0x + 40 hex).
	if !isValidAddress(address) {
		httputil.WriteError(w, http.StatusBadRequest, "bad_request", "invalid ethereum address")
		return
	}

	auction, err := h.Store.AuctionRepo().GetByAddress(r.Context(), h.ChainID, strings.ToLower(address))
	if err != nil {
		h.Logger.Error("failed to get auction", "address", address, "error", err)
		httputil.WriteError(w, http.StatusInternalServerError, "internal_error", "failed to fetch auction")
		return
	}
	if auction == nil {
		httputil.WriteNotFound(w, "auction")
		return
	}

	resp := toAuctionResponse(auction)
	httputil.WriteJSON(w, http.StatusOK, httputil.Response{Data: resp})
}

// isValidAddress performs a basic check that s looks like an Ethereum address.
// Does NOT validate checksum — just format.
func isValidAddress(s string) bool {
	if len(s) != 42 {
		return false
	}
	if !strings.HasPrefix(s, "0x") && !strings.HasPrefix(s, "0X") {
		return false
	}
	// TODO: validate hex characters
	return true
}
