#!/usr/bin/env bash
# deploy.sh — Deploy the CCA factory and create a test auction on a local Anvil chain.
#
# Prerequisites:
#   - Anvil running on http://127.0.0.1:8545
#   - Foundry (forge) installed
#
# Usage:
#   ./deploy.sh
#
# Anvil's default private key (account 0):
PRIVATE_KEY="0xac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80"
RPC_URL="${RPC_URL:-http://127.0.0.1:8545}"

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

echo "==> Deploying CCA factory and creating test auction..."
forge script script/DeployAndCreateAuction.s.sol:DeployAndCreateAuction --rpc-url "$RPC_URL" --private-key "$PRIVATE_KEY" --broadcast

echo "==> Done. AuctionCreated event emitted on local chain."
