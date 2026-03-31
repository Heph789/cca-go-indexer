#!/usr/bin/env python3
"""Clearing Price QA: end-to-end verification of CheckpointUpdated indexing and GraphQL queries.

Required Verifications (from issue #102):
  1. Indexer processes CheckpointUpdated events, persists to Postgres, advances cursors
  2. GraphQL auction(address) returns auction with clearingPriceQ96 from latest checkpoint
  3. GraphQL auctions(first: 5) returns paginated results with valid cursor for next page
  4. clearingPriceQ96 returns nil for an auction with no checkpoints
  5. Replaying the same CheckpointUpdated event is idempotent (ON CONFLICT DO NOTHING)

Agent-Designed Experiments:
  6. clearingPriceQ96 reflects the LATEST checkpoint when multiple exist
  7. Pagination cursor from first page can be used to fetch a second page
"""

import sys
import time

from helpers import (
    API_PORT,
    ANVIL_PORT,
    CHAIN_ID,
    DATABASE_URL,
    cleanup,
    start_anvil,
    deploy_contracts,
    truncate_all,
    start_indexer,
    start_api,
    wait_for_cursor,
    mine_blocks,
    get_block_number,
    mine_to_start_block,
    submit_bid,
    graphql_query,
    query_checkpoints,
    count_checkpoints,
    check,
    reset_results,
    print_summary,
    _psql,
)


AUCTION_QUERY = """
query AuctionQuery($address: Address!) {
  auction(address: $address) {
    auctionAddress
    token
    amount
    currency
    startBlock
    endBlock
    claimBlock
    tickSpacing
    floorPrice
    clearingPriceQ96
  }
}
"""

AUCTIONS_QUERY = """
query AuctionsQuery($first: Int, $after: String) {
  auctions(first: $first, after: $after) {
    edges {
      node {
        auctionAddress
        clearingPriceQ96
      }
      cursor
    }
    pageInfo {
      hasNextPage
      endCursor
    }
  }
}
"""


def seed_checkpoint(auction_address: str, block_number: int, clearing_price: str,
                    cumulative_mps: int = 200000, tx_block_number: int = 2) -> bool:
    """Insert a checkpoint row directly into Postgres.

    Used for GraphQL-focused experiments that should not depend on
    the indexer's ability to capture CheckpointUpdated events.
    Returns True on success, False if the table doesn't exist.
    """
    try:
        _psql(
            f"INSERT INTO event_cca_checkpoint_updated "
            f"(chain_id, auction_address, block_number, clearing_price_q96, cumulative_mps, "
            f"tx_block_number, tx_block_time, tx_hash, log_index) "
            f"VALUES ({CHAIN_ID}, '{auction_address.lower()}', {block_number}, "
            f"'{clearing_price}', {cumulative_mps}, {tx_block_number}, "
            f"NOW(), "
            f"'0x{'0' * 63}{block_number:01x}', 0) "
            f"ON CONFLICT DO NOTHING",
        )
        return True
    except RuntimeError:
        return False


def main() -> int:
    reset_results()
    rpc_url = f"http://127.0.0.1:{ANVIL_PORT}"

    try:
        print("=========================================")
        print("  Clearing Price QA: E2E Verification")
        print("=========================================")
        print()

        # --- Setup: deploy contracts ---
        start_anvil()
        factory, auction = deploy_contracts(rpc_url)
        truncate_all()

        # Start the indexer to pick up AuctionCreated.
        start_indexer(rpc_url, factory)

        # Wait for the indexer to process deployment blocks.
        # Deployment uses 1 block on Anvil with default settings.
        wait_for_cursor(target=1, timeout=30)

        # Start the API for GraphQL queries.
        start_api()

        # ------------------------------------------------------------------
        # Experiment 4: clearingPriceQ96 is nil with no checkpoints
        # ------------------------------------------------------------------
        # This runs BEFORE any checkpoint data exists. The auction was
        # indexed via AuctionCreated, but no CheckpointUpdated has fired.
        print()
        print("--- Experiment 4: clearingPriceQ96 is nil with no checkpoints ---")
        resp = graphql_query(AUCTION_QUERY, {"address": auction})
        check("GraphQL auction query succeeds", "errors" not in resp)
        auction_data = resp.get("data", {}).get("auction")
        check("Auction found in GraphQL response", auction_data is not None)
        if auction_data:
            check(
                "clearingPriceQ96 is null before any checkpoints",
                auction_data.get("clearingPriceQ96") is None,
            )
            check(
                "auctionAddress is set",
                auction_data.get("auctionAddress") is not None
                and len(auction_data["auctionAddress"]) == 42,
            )
            start_block = auction_data.get("startBlock", 0)
            check("startBlock > 0", start_block > 0)
        else:
            check("clearingPriceQ96 is null before any checkpoints", False)
            check("auctionAddress is set", False)
            start_block = 10
            check("startBlock > 0", False)

        # ------------------------------------------------------------------
        # Experiment 1: Indexer processes CheckpointUpdated events
        # ------------------------------------------------------------------
        # Submit a bid on chain to emit CheckpointUpdated. The indexer
        # should capture it IF the auction address is in its filter set.
        # This verifies the full pipeline: chain event -> indexer -> Postgres.
        print()
        print("--- Experiment 1: CheckpointUpdated indexing pipeline ---")

        if auction_data:
            start_block = auction_data["startBlock"]
        mine_to_start_block(rpc_url, start_block)
        submit_bid(rpc_url, auction)

        # Wait for the indexer to process the bid block.
        current_block = get_block_number(rpc_url)
        print(f"    Current chain block: {current_block}")
        wait_for_cursor(target=current_block, timeout=30)

        # Check if the CheckpointUpdated event was persisted.
        checkpoints = query_checkpoints(auction)
        check("At least one checkpoint row in Postgres", len(checkpoints) >= 1)
        if checkpoints:
            cp = checkpoints[0]
            check(
                "Checkpoint auction_address matches",
                cp["auction_address"] == auction.lower(),
            )
            check(
                "Checkpoint clearing_price_q96 is non-zero numeric",
                cp["clearing_price_q96"] != "0" and cp["clearing_price_q96"].isdigit(),
            )
            check(
                "Checkpoint chain_id matches",
                cp["chain_id"] == CHAIN_ID,
            )
        else:
            check("Checkpoint auction_address matches", False)
            check("Checkpoint clearing_price_q96 is non-zero numeric", False)
            check("Checkpoint chain_id matches", False)

        # Verify cursor has advanced past the bid submission block.
        cursor_row = _psql(
            f"SELECT last_block FROM indexer_cursors WHERE chain_id = {CHAIN_ID}",
        )
        cursor_block = int(cursor_row) if cursor_row else 0
        check(
            "Indexer cursor advanced to at least the bid block",
            cursor_block >= current_block,
        )

        # ------------------------------------------------------------------
        # Experiment 2: GraphQL auction(address) returns clearingPriceQ96
        # ------------------------------------------------------------------
        # Seed a checkpoint directly via SQL so this experiment is
        # independent of whether the indexer pipeline captured the event.
        print()
        print("--- Experiment 2: GraphQL auction(address) with clearingPriceQ96 ---")
        seed_price = "1000000000000000"
        seed_checkpoint(auction, block_number=50, clearing_price=seed_price)

        resp = graphql_query(AUCTION_QUERY, {"address": auction})
        check("GraphQL auction query succeeds", "errors" not in resp)
        auction_data = resp.get("data", {}).get("auction")
        check("Auction found", auction_data is not None)
        if auction_data:
            clearing_price = auction_data.get("clearingPriceQ96")
            check(
                "clearingPriceQ96 is non-null after checkpoint",
                clearing_price is not None,
            )
            check(
                "clearingPriceQ96 is a string (BigInt scalar)",
                clearing_price is not None and isinstance(clearing_price, str),
            )
        else:
            check("clearingPriceQ96 is non-null after checkpoint", False)
            check("clearingPriceQ96 is a string (BigInt scalar)", False)

        # ------------------------------------------------------------------
        # Experiment 3: GraphQL auctions(first: 5) with pagination
        # ------------------------------------------------------------------
        print()
        print("--- Experiment 3: GraphQL auctions(first: 5) pagination ---")
        resp = graphql_query(AUCTIONS_QUERY, {"first": 5})
        has_errors = "errors" in resp
        check("GraphQL auctions query succeeds", not has_errors)
        if has_errors:
            errs = resp.get("errors", [])
            if errs:
                print(f"    GraphQL error: {errs[0].get('message', 'unknown')}")
        conn = resp.get("data", {}).get("auctions") if resp.get("data") else None
        check("auctions connection returned", conn is not None)
        if conn:
            edges = conn.get("edges", [])
            check("At least one auction edge returned", len(edges) >= 1)
            page_info = conn.get("pageInfo", {})
            check("pageInfo present", "hasNextPage" in page_info)
            check("hasNextPage is false with one auction", page_info.get("hasNextPage") is False)
            if edges:
                check(
                    "Edge has cursor string",
                    isinstance(edges[0].get("cursor"), str) and len(edges[0]["cursor"]) > 0,
                )
                check(
                    "Edge node has auctionAddress",
                    edges[0].get("node", {}).get("auctionAddress") is not None,
                )
                check(
                    "endCursor matches last edge cursor",
                    page_info.get("endCursor") == edges[-1].get("cursor"),
                )
            else:
                check("Edge has cursor string", False)
                check("Edge node has auctionAddress", False)
                check("endCursor matches last edge cursor", False)
        else:
            check("At least one auction edge returned", False)
            check("pageInfo present", False)
            check("hasNextPage is false with one auction", False)
            check("Edge has cursor string", False)
            check("Edge node has auctionAddress", False)
            check("endCursor matches last edge cursor", False)

        # ------------------------------------------------------------------
        # Experiment 5: Idempotent replay (ON CONFLICT DO NOTHING)
        # ------------------------------------------------------------------
        # Seed a checkpoint, then re-insert it with a different clearing price.
        # The ON CONFLICT DO NOTHING clause should preserve the original value.
        # We verify through GraphQL to ensure the application layer handles this.
        print()
        print("--- Experiment 5: Idempotent checkpoint replay ---")
        original_price = "5555555555"
        replay_price = "9999999999"
        seed_checkpoint(auction, block_number=200, clearing_price=original_price)
        count_before = count_checkpoints(auction)
        check("Baseline: at least 1 checkpoint exists", count_before >= 1)

        # Re-insert with the SAME primary key but different clearing_price.
        seed_checkpoint(auction, block_number=200, clearing_price=replay_price)
        count_after = count_checkpoints(auction)
        check(
            "Checkpoint count unchanged after replay (idempotent)",
            count_after == count_before,
        )

        # Verify through GraphQL that the original price is preserved.
        # GetLatest returns the checkpoint with the highest block_number.
        # block_number=200 is higher than the seeded 50, so it should be returned.
        resp = graphql_query(AUCTION_QUERY, {"address": auction})
        check("GraphQL query succeeds after replay", "errors" not in resp)
        auction_data = resp.get("data", {}).get("auction")
        if auction_data:
            check(
                "clearingPriceQ96 shows original value (replay did not overwrite)",
                auction_data.get("clearingPriceQ96") == original_price,
            )
        else:
            check("clearingPriceQ96 shows original value (replay did not overwrite)", False)

        # ------------------------------------------------------------------
        # Experiment 6: Latest checkpoint wins when multiple exist
        # ------------------------------------------------------------------
        print()
        print("--- Experiment 6: clearingPriceQ96 reflects latest checkpoint ---")
        # Insert a checkpoint with a very high block_number and a known price.
        # GetLatest orders by block_number DESC, so this should be returned.
        known_price = "99999999999999999999"
        seed_checkpoint(auction, block_number=999, clearing_price=known_price)

        resp = graphql_query(AUCTION_QUERY, {"address": auction})
        auction_data = resp.get("data", {}).get("auction")
        if auction_data:
            check(
                "clearingPriceQ96 reflects the latest (highest block) checkpoint",
                auction_data.get("clearingPriceQ96") == known_price,
            )
        else:
            check("clearingPriceQ96 reflects the latest (highest block) checkpoint", False)

        # ------------------------------------------------------------------
        # Experiment 7: Pagination cursor works for next page
        # ------------------------------------------------------------------
        print()
        print("--- Experiment 7: Pagination cursor for next page ---")
        resp = graphql_query(AUCTIONS_QUERY, {"first": 1})
        has_errors = "errors" in resp
        check("First page query succeeds", not has_errors)
        if has_errors:
            errs = resp.get("errors", [])
            if errs:
                print(f"    GraphQL error: {errs[0].get('message', 'unknown')}")
        conn = resp.get("data", {}).get("auctions") if resp.get("data") else None
        if conn:
            edges = conn.get("edges", [])
            page_info = conn.get("pageInfo", {})
            if edges:
                end_cursor = page_info.get("endCursor")
                check(
                    "endCursor is a non-empty string",
                    isinstance(end_cursor, str) and len(end_cursor) > 0,
                )
                # Use the cursor for a second page query.
                resp2 = graphql_query(AUCTIONS_QUERY, {"first": 1, "after": end_cursor})
                check("Second page query succeeds", "errors" not in resp2)
                conn2 = resp2.get("data", {}).get("auctions") if resp2.get("data") else None
                if conn2:
                    edges2 = conn2.get("edges", [])
                    check("Second page is empty (only 1 auction)", len(edges2) == 0)
                else:
                    check("Second page is empty (only 1 auction)", False)
            else:
                check("endCursor is a non-empty string", False)
                check("Second page query succeeds", False)
                check("Second page is empty (only 1 auction)", False)
        else:
            check("endCursor is a non-empty string", False)
            check("Second page query succeeds", False)
            check("Second page is empty (only 1 auction)", False)

        # ------------------------------------------------------------------
        # Summary
        # ------------------------------------------------------------------
        print()
        return print_summary("Clearing Price E2E")

    finally:
        cleanup()


if __name__ == "__main__":
    sys.exit(main())
