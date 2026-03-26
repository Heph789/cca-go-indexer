#!/usr/bin/env bash
# run.sh — Full end-to-end QA: start Anvil, deploy contracts, run indexer + API, verify.
#
# Prerequisites:
#   - Foundry installed (anvil, forge)
#   - Go installed
#   - Postgres running with DATABASE_URL accessible
#   - Reference submodule initialized: git submodule update --init --recursive
#
# Usage:
#   ./run.sh
#
# Environment overrides:
#   DATABASE_URL  — Postgres connection string (default: postgres://cca:cca@localhost:5432/cca_indexer?sslmode=disable)
#   API_PORT      — Port for the API server (default: 8080)

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
CHAIN_DIR="$SCRIPT_DIR/../chain"

DATABASE_URL="${DATABASE_URL:-postgres://cca:cca@localhost:5432/cca_indexer?sslmode=disable}"
API_PORT="${API_PORT:-8080}"
ANVIL_PORT="${ANVIL_PORT:-8545}"
RPC_URL="http://127.0.0.1:$ANVIL_PORT"
ANVIL_PRIVATE_KEY="0xac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80"

# Cleanup function to kill background processes on exit
cleanup() {
    echo "==> Cleaning up..."
    [[ -n "${ANVIL_PID:-}" ]] && kill "$ANVIL_PID" 2>/dev/null || true
    [[ -n "${INDEXER_PID:-}" ]] && kill "$INDEXER_PID" 2>/dev/null || true
    [[ -n "${API_PID:-}" ]] && kill "$API_PID" 2>/dev/null || true
    wait 2>/dev/null || true
}
trap cleanup EXIT

echo "========================================="
echo "  CCA Go Indexer — E2E QA"
echo "========================================="

# --- Step 1: Start Anvil ---
echo ""
echo "==> Starting Anvil on port $ANVIL_PORT..."
anvil --port "$ANVIL_PORT" --silent &
ANVIL_PID=$!
sleep 2

# Verify Anvil is running
if ! curl -sf "$RPC_URL" -X POST -H "Content-Type: application/json" -d '{"jsonrpc":"2.0","method":"eth_blockNumber","params":[],"id":1}' > /dev/null; then
    echo "ERROR: Anvil failed to start"
    exit 1
fi
echo "    Anvil running (PID $ANVIL_PID)"

# --- Step 2: Deploy contracts ---
echo ""
echo "==> Deploying CCA factory and creating test auction..."
DEPLOY_OUTPUT=$(cd "$CHAIN_DIR" && RPC_URL="$RPC_URL" ./deploy.sh 2>&1)
echo "$DEPLOY_OUTPUT"

# Parse factory and auction addresses from forge output
FACTORY_ADDRESS=$(echo "$DEPLOY_OUTPUT" | grep -i "Factory deployed to:" | awk '{print $NF}')
AUCTION_ADDRESS=$(echo "$DEPLOY_OUTPUT" | grep -i "Auction created at:" | awk '{print $NF}')

if [[ -z "$FACTORY_ADDRESS" || -z "$AUCTION_ADDRESS" ]]; then
    echo "ERROR: Failed to parse contract addresses from deploy output"
    exit 1
fi
echo "    Factory: $FACTORY_ADDRESS"
echo "    Auction: $AUCTION_ADDRESS"

# --- Step 3: Start the indexer ---
echo ""
echo "==> Starting indexer..."
cd "$REPO_ROOT"
DATABASE_URL="$DATABASE_URL" \
RPC_URL="$RPC_URL" \
CHAIN_ID=31337 \
FACTORY_ADDRESS="$FACTORY_ADDRESS" \
START_BLOCK=0 \
CONFIRMATIONS=0 \
POLL_INTERVAL=1s \
BLOCK_BATCH_SIZE=100 \
MAX_BLOCK_RANGE=2000 \
LOG_LEVEL=info \
LOG_FORMAT=text \
go run ./cmd/indexer &
INDEXER_PID=$!
echo "    Indexer running (PID $INDEXER_PID)"

# --- Step 4: Start the API ---
echo ""
echo "==> Starting API on port $API_PORT..."
DATABASE_URL="$DATABASE_URL" \
CHAIN_ID=31337 \
PORT="$API_PORT" \
LOG_LEVEL=info \
LOG_FORMAT=text \
go run ./cmd/api &
API_PID=$!
echo "    API running (PID $API_PID)"

# --- Step 5: Wait for indexing to complete ---
echo ""
echo "==> Waiting for indexer to process events..."
sleep 10

# --- Step 6: Verify ---
echo ""
echo "==> Running verification..."
"$SCRIPT_DIR/verify.sh" "$AUCTION_ADDRESS" "$API_PORT"

EXIT_CODE=$?
if [[ $EXIT_CODE -eq 0 ]]; then
    echo ""
    echo "========================================="
    echo "  QA PASSED"
    echo "========================================="
else
    echo ""
    echo "========================================="
    echo "  QA FAILED"
    echo "========================================="
fi

exit $EXIT_CODE
