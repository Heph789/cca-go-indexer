#!/usr/bin/env python3
"""Scenario 1: Indexer recovers from a transient RPC outage.

Proves that when the RPC endpoint goes down temporarily, the indexer's
retry transport and loop retry budget allow it to survive and resume
indexing once the endpoint recovers.
"""

import sys
import time

import helpers


def main() -> int:
    helpers.reset_results()
    anvil_url = f"http://127.0.0.1:{helpers.ANVIL_PORT}"
    proxy_url = f"http://127.0.0.1:{helpers.PROXY_PORT}"

    print("=========================================")
    print("  Resilience QA: Retry Recovery")
    print("=========================================")
    print()

    try:
        # --- Setup ---
        helpers.truncate_all()
        helpers.start_anvil()
        factory_addr, _ = helpers.deploy_contracts(anvil_url)
        helpers.start_proxy(anvil_url)
        indexer_proc = helpers.start_indexer(
            rpc_url=proxy_url,
            factory_addr=factory_addr,
            poll_interval="1s",
        )

        # Wait for indexer to process deployment blocks
        deploy_head = helpers.get_block_number(anvil_url)
        print(f"\n==> Waiting for indexer to reach block {deploy_head}...")
        helpers.wait_for_cursor(deploy_head, timeout=30)
        cursor_before, _ = helpers.query_cursor()
        print(f"    Cursor at block {cursor_before}")

        # --- Inject fault ---
        print("\n==> Enabling fault injection (503s)...")
        helpers.fault_on(proxy_url)

        # Mine blocks while indexer can't see them
        print("==> Mining 5 blocks on Anvil (indexer is blind)...")
        helpers.mine_blocks(anvil_url, 5)
        new_head = helpers.get_block_number(anvil_url)
        print(f"    Chain head now at block {new_head}")

        # Let the indexer hit some retries (but not exhaust budget)
        print("==> Waiting 3s for indexer to experience retries...")
        time.sleep(3)

        # --- Remove fault ---
        print("\n==> Disabling fault injection...")
        helpers.fault_off(proxy_url)

        # Wait for indexer to catch up
        print(f"==> Waiting for indexer to reach block {new_head}...")
        helpers.wait_for_cursor(new_head, timeout=30)

        # --- Verify ---
        print("\n--- Verification ---\n")

        cursor_after, _ = helpers.query_cursor()
        helpers.check(
            f"Cursor advanced past outage (at {cursor_after}, expected >= {new_head})",
            cursor_after >= new_head,
        )

        helpers.check(
            "Indexer process is still running",
            indexer_proc.poll() is None,
        )

        max_block = helpers.query_max_block()
        helpers.check(
            f"Blocks mined during outage are indexed (max_block={max_block}, expected >= {new_head})",
            max_block >= new_head,
        )

        return helpers.print_summary("Retry Recovery")

    except Exception as e:
        print(f"\nERROR: {e}")
        return 1
    finally:
        helpers.cleanup()


if __name__ == "__main__":
    sys.exit(main())
