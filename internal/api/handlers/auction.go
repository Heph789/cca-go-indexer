package handlers

import (
	"net/http"
	"strings"

	"github.com/cca/go-indexer/internal/api"
	"github.com/cca/go-indexer/internal/api/httputil"
	"github.com/cca/go-indexer/internal/domain/cca"
	"github.com/cca/go-indexer/internal/store"
)

// Ensure imports are used.
var (
	_ = strings.ToLower
	_ = api.LoggerFromContext
	_ = httputil.WriteJSON
	_ http.ResponseWriter
)

type AuctionHandler struct {
	Store   store.Store
	ChainID int64
}

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

func toAuctionResponse(a *cca.Auction) AuctionResponse {
	panic("not implemented")
}

func (h *AuctionHandler) Get(w http.ResponseWriter, r *http.Request) {
	panic("not implemented")
}

func isValidAddress(s string) bool {
	panic("not implemented")
}
