package handlers

import (
	"encoding/hex"
	"net/http"
	"strings"

	"github.com/cca/go-indexer/internal/api"
	"github.com/cca/go-indexer/internal/api/httputil"
	"github.com/cca/go-indexer/internal/domain/cca"
	"github.com/cca/go-indexer/internal/store"
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
		TxHash:                 strings.ToLower(a.TxHash.Hex()),
		LogIndex:               a.LogIndex,
	}
}

func (h *AuctionHandler) Get(w http.ResponseWriter, r *http.Request) {
	log := api.LoggerFromContext(r.Context())

	address := r.PathValue("address")
	if !isValidAddress(address) {
		httputil.WriteError(w, http.StatusBadRequest, httputil.CodeBadRequest, "invalid address")
		return
	}

	address = strings.ToLower(address)

	auction, err := h.Store.AuctionRepo().GetByAddress(r.Context(), h.ChainID, address)
	if err != nil {
		log.Error("failed to get auction", "error", err, "address", address)
		httputil.WriteError(w, http.StatusInternalServerError, httputil.CodeInternalError, "internal error")
		return
	}

	if auction == nil {
		httputil.WriteNotFound(w, "auction")
		return
	}

	httputil.WriteJSON(w, http.StatusOK, httputil.Response{Data: toAuctionResponse(auction)})
}

func isValidAddress(s string) bool {
	if len(s) != 42 {
		return false
	}
	if !strings.HasPrefix(s, "0x") && !strings.HasPrefix(s, "0X") {
		return false
	}
	_, err := hex.DecodeString(s[2:])
	return err == nil
}
