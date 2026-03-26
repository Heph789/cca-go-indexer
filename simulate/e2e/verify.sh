#!/usr/bin/env bash
# verify.sh — Verify that the indexer processed events and the API serves them.
#
# Usage:
#   ./verify.sh <auction_address> [api_port]
#
# Checks:
#   1. Health endpoint returns 200
#   2. GET /api/v1/auctions/{address} returns the indexed auction
#   3. Response fields are non-empty and plausible

set -euo pipefail

AUCTION_ADDRESS="${1:?Usage: verify.sh <auction_address> [api_port]}"
API_PORT="${2:-8080}"
API_BASE="http://127.0.0.1:$API_PORT"

PASS=0
FAIL=0

check() {
    local desc="$1"
    local result="$2"
    if [[ "$result" == "true" ]]; then
        echo "  PASS: $desc"
        ((PASS++))
    else
        echo "  FAIL: $desc"
        ((FAIL++))
    fi
}

echo "--- Verification ---"
echo ""

# --- Check 1: Health endpoint ---
echo "[1] Health check"
HEALTH_STATUS=$(curl -sf -o /dev/null -w "%{http_code}" "$API_BASE/health" || echo "000")
check "GET /health returns 200" "$( [[ "$HEALTH_STATUS" == "200" ]] && echo true || echo false )"

# --- Check 2: Auction endpoint ---
echo ""
echo "[2] Auction lookup: $AUCTION_ADDRESS"
AUCTION_RESPONSE=$(curl -sf "$API_BASE/api/v1/auctions/$AUCTION_ADDRESS" || echo "")

if [[ -z "$AUCTION_RESPONSE" ]]; then
    echo "  FAIL: GET /api/v1/auctions/{address} returned empty or errored"
    FAIL=$((FAIL + 1))
else
    # Parse JSON fields using grep/sed (no jq dependency required, but use jq if available)
    if command -v jq &> /dev/null; then
        DATA=$(echo "$AUCTION_RESPONSE" | jq -r '.data')
        RESP_ADDR=$(echo "$DATA" | jq -r '.auction_address')
        RESP_TOKEN=$(echo "$DATA" | jq -r '.token')
        RESP_AMOUNT=$(echo "$DATA" | jq -r '.amount')
        RESP_START_BLOCK=$(echo "$DATA" | jq -r '.start_block')
        RESP_END_BLOCK=$(echo "$DATA" | jq -r '.end_block')
        RESP_FLOOR_PRICE=$(echo "$DATA" | jq -r '.floor_price')
    else
        echo "  (jq not found — using basic checks)"
        RESP_ADDR=$(echo "$AUCTION_RESPONSE" | grep -o '"auction_address":"[^"]*"' | head -1 | cut -d'"' -f4)
        RESP_TOKEN=$(echo "$AUCTION_RESPONSE" | grep -o '"token":"[^"]*"' | head -1 | cut -d'"' -f4)
        RESP_AMOUNT=$(echo "$AUCTION_RESPONSE" | grep -o '"amount":"[^"]*"' | head -1 | cut -d'"' -f4)
        RESP_START_BLOCK="unknown"
        RESP_END_BLOCK="unknown"
        RESP_FLOOR_PRICE="unknown"
    fi

    LOWER_AUCTION=$(echo "$AUCTION_ADDRESS" | tr '[:upper:]' '[:lower:]')
    check "auction_address matches" "$( [[ "$RESP_ADDR" == "$LOWER_AUCTION" ]] && echo true || echo false )"
    check "token is non-empty" "$( [[ -n "$RESP_TOKEN" && "$RESP_TOKEN" != "null" ]] && echo true || echo false )"
    check "amount is non-zero" "$( [[ -n "$RESP_AMOUNT" && "$RESP_AMOUNT" != "0" && "$RESP_AMOUNT" != "null" ]] && echo true || echo false )"

    if command -v jq &> /dev/null; then
        check "start_block > 0" "$( [[ "$RESP_START_BLOCK" -gt 0 ]] 2>/dev/null && echo true || echo false )"
        check "end_block > start_block" "$( [[ "$RESP_END_BLOCK" -gt "$RESP_START_BLOCK" ]] 2>/dev/null && echo true || echo false )"
        check "floor_price is non-zero" "$( [[ -n "$RESP_FLOOR_PRICE" && "$RESP_FLOOR_PRICE" != "0" && "$RESP_FLOOR_PRICE" != "null" ]] && echo true || echo false )"
    fi
fi

# --- Summary ---
echo ""
echo "--- Results: $PASS passed, $FAIL failed ---"

if [[ $FAIL -gt 0 ]]; then
    exit 1
fi
