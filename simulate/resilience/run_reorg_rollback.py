#!/usr/bin/env python3
"""Scenario 3: Reorg rollback leaves the database in a consistent state.

Proves that when the chain reorganizes, the indexer detects the reorg,
rolls back stale data, and re-indexes the new canonical chain correctly.

Uses Anvil's evm_snapshot / evm_revert to simulate a chain reorg:
1. Index some blocks
2. Snapshot the chain
3. Mine more blocks, let indexer process them
4. Revert to snapshot (chain rewinds)
5. Mine new blocks at the same heights (different hashes)
6. Verify indexer detected reorg, rolled back, and re-indexed
"""

import sys

import helpers


def main() -> int:
    helpers.reset_results()
    anvil_url = f"http://127.0.0.1:{helpers.ANVIL_PORT}"

    print("=========================================")
    print("  Resilience QA: Reorg Rollback")
    print("=========================================")
    print()

    try:
        # --- Setup ---
        helpers.truncate_all()
        helpers.start_anvil()
        factory_addr, _ = helpers.deploy_contracts(anvil_url)
        indexer_proc = helpers.start_indexer(
            rpc_url=anvil_url,
            factory_addr=factory_addr,
            poll_interval="1s",
            confirmations=0,
        )

        # Wait for indexer to process deployment blocks
        deploy_head = helpers.get_block_number(anvil_url)
        print(f"\n==> Waiting for indexer to reach block {deploy_head}...")
        helpers.wait_for_cursor(deploy_head, timeout=30)
        print(f"    Indexer caught up to deployment head ({deploy_head})")

        # --- Take snapshot ---
        print("\n==> Taking EVM snapshot...")
        snapshot_id = helpers.evm_snapshot(anvil_url)
        snapshot_block = helpers.get_block_number(anvil_url)
        print(f"    Snapshot taken at block {snapshot_block} (id: {snapshot_id})")

        # Record hashes of blocks at/before snapshot (should be unchanged after reorg)
        pre_snapshot_hashes = helpers.query_block_hashes(
            from_block=1,
            to_block=snapshot_block,
        )

        # --- Mine post-snapshot blocks and let indexer process them ---
        print("\n==> Mining 5 blocks post-snapshot...")
        helpers.mine_blocks(anvil_url, 5)
        post_mine_head = helpers.get_block_number(anvil_url)
        print(f"    Chain head now at block {post_mine_head}")

        print(f"==> Waiting for indexer to reach block {post_mine_head}...")
        helpers.wait_for_cursor(post_mine_head, timeout=30)

        # Record the hashes the indexer stored for these blocks (will be stale after reorg)
        old_hashes = helpers.query_block_hashes(
            from_block=snapshot_block + 1,
            to_block=post_mine_head,
        )
        print(f"    Indexer processed blocks {snapshot_block + 1}-{post_mine_head}")
        print(f"    Recorded {len(old_hashes)} block hashes (will become stale)")

        # --- Revert chain ---
        print("\n==> Reverting chain to snapshot...")
        helpers.evm_revert(anvil_url, snapshot_id)
        reverted_head = helpers.get_block_number(anvil_url)
        print(f"    Chain reverted to block {reverted_head}")

        # --- Mine new blocks (different hashes at same heights) ---
        blocks_to_mine = (post_mine_head - snapshot_block) + 2  # mine a couple extra
        print(f"\n==> Mining {blocks_to_mine} new blocks (new canonical chain)...")
        helpers.mine_blocks(anvil_url, blocks_to_mine)
        new_head = helpers.get_block_number(anvil_url)
        print(f"    New chain head at block {new_head}")

        # --- Wait for indexer to detect reorg and re-index ---
        # Wait for new_head (not post_mine_head) because cursor is already at
        # post_mine_head from the first pass. We need the indexer to detect the
        # reorg, roll back, and re-index past the new chain head.
        print(f"\n==> Waiting for indexer to re-index to new head {new_head}...")
        helpers.wait_for_cursor(new_head, timeout=45)
        print(f"    Indexer re-indexed to block {new_head}")

        # --- Verify ---
        print("\n--- Verification ---\n")

        cursor_final, _ = helpers.query_cursor()
        helpers.check(
            f"Cursor is past the reorg point (at {cursor_final}, expected >= {post_mine_head})",
            cursor_final >= post_mine_head,
        )

        helpers.check(
            "Indexer process is still running",
            indexer_proc.poll() is None,
        )

        # Block hashes in the reorg'd range should differ from what we recorded
        new_db_hashes = helpers.query_block_hashes(
            from_block=snapshot_block + 1,
            to_block=post_mine_head,
        )

        reorged_blocks_changed = all(
            new_db_hashes.get(bn) != old_hashes.get(bn)
            for bn in old_hashes
            if bn in new_db_hashes
        )
        helpers.check(
            "Reorg'd block hashes in DB differ from pre-revert hashes",
            reorged_blocks_changed and len(old_hashes) > 0,
        )

        # No gaps in indexed blocks from 1 to cursor
        all_hashes = helpers.query_block_hashes(from_block=1, to_block=cursor_final)
        expected_blocks = set(range(1, cursor_final + 1))
        actual_blocks = set(all_hashes.keys())
        missing = expected_blocks - actual_blocks
        helpers.check(
            f"No gaps in indexed blocks (1 to {cursor_final}, {len(all_hashes)} blocks)",
            len(missing) == 0,
        )
        if missing:
            print(f"    Missing blocks: {sorted(missing)}")

        # Pre-snapshot blocks should be unchanged
        current_pre_snapshot = helpers.query_block_hashes(
            from_block=1,
            to_block=snapshot_block,
        )
        pre_snapshot_match = all(
            current_pre_snapshot.get(bn) == pre_snapshot_hashes.get(bn)
            for bn in pre_snapshot_hashes
        )
        helpers.check(
            "Pre-snapshot block hashes unchanged",
            pre_snapshot_match and len(pre_snapshot_hashes) > 0,
        )

        return helpers.print_summary("Reorg Rollback")

    except Exception as e:
        print(f"\nERROR: {e}")
        return 1
    finally:
        helpers.cleanup()


if __name__ == "__main__":
    sys.exit(main())
