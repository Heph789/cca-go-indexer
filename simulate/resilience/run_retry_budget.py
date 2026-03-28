#!/usr/bin/env python3
"""Scenario 2: Indexer exits cleanly after exhausting retry budget.

Proves that when the RPC endpoint is permanently down, the indexer exits
with a non-zero status after maxLoopRetries (5) consecutive failures,
and the database remains in a consistent state.
"""

import sys
import time

import helpers


def main() -> int:
    helpers.reset_results()
    anvil_url = f"http://127.0.0.1:{helpers.ANVIL_PORT}"
    proxy_url = f"http://127.0.0.1:{helpers.PROXY_PORT}"

    print("=========================================")
    print("  Resilience QA: Retry Budget Exhaustion")
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
            poll_interval="100ms",
        )

        # Wait for indexer to process deployment blocks
        deploy_head = helpers.get_block_number(anvil_url)
        print(f"\n==> Waiting for indexer to reach block {deploy_head}...")
        helpers.wait_for_cursor(deploy_head, timeout=30)
        cursor_before, hash_before = helpers.query_cursor()
        max_block_before = helpers.query_max_block()
        print(f"    Cursor at block {cursor_before}")

        # --- Inject permanent fault ---
        print("\n==> Enabling permanent fault injection (503s)...")
        helpers.fault_on(proxy_url)

        # Mine blocks so the indexer tries to advance (sees new head but can't fetch)
        # Actually with fault on, BlockNumber() itself will fail, so mining isn't needed.
        # The indexer will fail on the very first RPC call each loop iteration.

        # Wait for indexer to exhaust retry budget and exit
        print("==> Waiting for indexer to exit (retry budget exhaustion)...")
        fault_start = time.monotonic()
        exit_code = helpers.wait_for_exit(indexer_proc, timeout=60)
        fault_duration = time.monotonic() - fault_start
        print(f"    Indexer exited with code {exit_code} after {fault_duration:.1f}s")

        # --- Verify ---
        print("\n--- Verification ---\n")

        helpers.check(
            "Indexer process has exited",
            indexer_proc.poll() is not None,
        )

        helpers.check(
            f"Exit code is non-zero (got {exit_code})",
            exit_code != 0,
        )

        # The indexer should have retried multiple times before exiting,
        # taking at least a few seconds. An instant crash (<1s) means
        # there's no retry budget — the indexer died on the first error.
        helpers.check(
            f"Indexer retried before exiting (survived {fault_duration:.1f}s, expected >= 1s)",
            fault_duration >= 1.0,
        )

        cursor_after, hash_after = helpers.query_cursor()
        helpers.check(
            f"Cursor unchanged (before={cursor_before}, after={cursor_after})",
            cursor_after == cursor_before,
        )

        helpers.check(
            f"Cursor hash unchanged (before={hash_before}, after={hash_after})",
            hash_after == hash_before,
        )

        max_block_after = helpers.query_max_block()
        helpers.check(
            f"No new blocks beyond cursor (max_block={max_block_after}, cursor={cursor_before})",
            max_block_after <= cursor_before,
        )

        return helpers.print_summary("Retry Budget Exhaustion")

    except Exception as e:
        print(f"\nERROR: {e}")
        return 1
    finally:
        helpers.cleanup()


if __name__ == "__main__":
    sys.exit(main())
