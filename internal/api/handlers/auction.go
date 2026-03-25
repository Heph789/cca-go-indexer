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
	AuctionAddress         string `json:"auction_address"`
	Token                  string `json:"token"`
	Amount                 string `json:"amount"`
	Currency               string `json:"currency"`
	TokensRecipient        string `json:"tokens_recipient"`
	FundsRecipient         string `json:"funds_recipient"`
	StartBlock             uint64 `json:"start_block"`
	EndBlock               uint64 `json:"end_block"`
	ClaimBlock             uint64 `json:"claim_block"`
	TickSpacing            string `json:"tick_spacing"`
	ValidationHook         string `json:"validation_hook"`
	FloorPrice             string `json:"floor_price"`
	RequiredCurrencyRaised string `json:"required_currency_raised"`
	BlockNumber            uint64 `json:"block_number"`
	TxHash                 string `json:"tx_hash"`
	LogIndex               uint   `json:"log_index"`
}

// toAuctionResponse maps a domain Auction to its API representation.
// Addresses are lowercased hex with 0x prefix (EIP-55 checksum could
// be added here if needed by frontend clients).
func toAuctionResponse(a *cca.Auction) AuctionResponse {
	return AuctionResponse{
		AuctionAddress:         strings.ToLower(a.AuctionAddress.Hex()),
		Token:                  strings.ToLower(a.Token.Hex()),
		Amount:                 a.Amount.String(),
		Currency:               strings.ToLower(a.Currency.Hex()),
		TokensRecipient:        strings.ToLower(a.TokensRecipient.Hex()),
		FundsRecipient:         strings.ToLower(a.FundsRecipient.Hex()),
		StartBlock:             a.StartBlock,
		EndBlock:               a.EndBlock,
		ClaimBlock:             a.ClaimBlock,
		TickSpacing:            a.TickSpacing.String(),
		ValidationHook:         strings.ToLower(a.ValidationHook.Hex()),
		FloorPrice:             a.FloorPrice.String(),
		RequiredCurrencyRaised: a.RequiredCurrencyRaised.String(),
		BlockNumber:            a.BlockNumber,
		TxHash:                 a.TxHash.Hex(),
		LogIndex:               a.LogIndex,
	}
}

// Get handles GET /api/v1/auctions/{address}
// Returns a single auction by its on-chain contract address.
// The address path parameter is normalized to lowercase for lookup.
func (h *AuctionHandler) Get(w http.ResponseWriter, r *http.Request) {
	address := r.PathValue("address")
	if address == "" {
		httputil.WriteError(w, http.StatusBadRequest, "bad_request", "address is required") // feed: generally, I'd like our errors to be defined somewhere as opposed to writing out the error codes manually
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
