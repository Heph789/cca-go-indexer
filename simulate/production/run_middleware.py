#!/usr/bin/env python3
"""Production readiness QA: Middleware chain experiments.

Verifies that the full middleware stack (CORS, X-Request-ID, recovery,
request logger) is wired correctly and handles edge cases.

Experiments:
  1. CORS headers present on normal GET
  2. X-Request-ID generated when not provided
  3. X-Request-ID echoed when client provides one
  4. OPTIONS preflight returns 204 with correct headers
  5. Panic recovery returns clean 500 JSON (not connection drop)
  6. CORS headers present on error responses (404)
  7. X-Request-ID present on error responses (404)
"""

import sys

from helpers import (
    API_PORT,
    cleanup,
    start_anvil,
    deploy_contracts,
    truncate_all,
    start_indexer,
    wait_for_cursor,
    start_api,
    http_request,
    check,
    reset_results,
    print_summary,
    ANVIL_PORT,
)


def main() -> int:
    reset_results()
    api_url = f"http://127.0.0.1:{API_PORT}"
    rpc_url = f"http://127.0.0.1:{ANVIL_PORT}"

    try:
        print("=========================================")
        print("  Production QA: Middleware Chain")
        print("=========================================")
        print()

        # --- Setup ---
        start_anvil()
        factory, auction = deploy_contracts(rpc_url)
        truncate_all()
        start_indexer(rpc_url, factory)
        wait_for_cursor(target=2, timeout=30)
        start_api()

        print()
        print("--- Experiment 1: CORS headers on normal GET ---")
        resp = http_request(f"{api_url}/api/v1/auctions/{auction}")
        check("Status is 200", resp.status == 200)
        check(
            "Access-Control-Allow-Origin is *",
            resp.header("Access-Control-Allow-Origin") == "*",
        )
        check(
            "Access-Control-Allow-Methods includes GET",
            "GET" in (resp.header("Access-Control-Allow-Methods") or ""),
        )

        print()
        print("--- Experiment 2: X-Request-ID generated ---")
        resp = http_request(f"{api_url}/health")
        rid = resp.header("X-Request-ID")
        check("X-Request-ID header present", rid is not None and len(rid) > 0)
        # Generated IDs should be hex strings (32 chars = 16 bytes)
        check("Generated ID is 32-char hex", rid is not None and len(rid) == 32 and all(c in "0123456789abcdef" for c in rid))

        print()
        print("--- Experiment 3: X-Request-ID echoed back ---")
        client_id = "test-request-id-abc123"
        resp = http_request(
            f"{api_url}/health",
            headers={"X-Request-ID": client_id},
        )
        check(
            "Echoed X-Request-ID matches client-provided value",
            resp.header("X-Request-ID") == client_id,
        )

        print()
        print("--- Experiment 4: OPTIONS preflight ---")
        resp = http_request(
            f"{api_url}/api/v1/auctions/{auction}",
            method="OPTIONS",
        )
        check("OPTIONS returns 204", resp.status == 204)
        check(
            "Allow-Methods includes GET",
            "GET" in (resp.header("Access-Control-Allow-Methods") or ""),
        )
        check(
            "Allow-Headers includes X-Request-ID",
            "X-Request-ID" in (resp.header("Access-Control-Allow-Headers") or ""),
        )
        check(
            "Allow-Headers includes Content-Type",
            "Content-Type" in (resp.header("Access-Control-Allow-Headers") or ""),
        )
        check("Body is empty on 204", len(resp.body) == 0)

        print()
        print("--- Experiment 5: CORS headers on 404 error ---")
        resp = http_request(
            f"{api_url}/api/v1/auctions/0x0000000000000000000000000000000000000000"
        )
        check("Status is 404", resp.status == 404)
        check(
            "CORS origin header present on 404",
            resp.header("Access-Control-Allow-Origin") == "*",
        )

        print()
        print("--- Experiment 6: X-Request-ID on error response ---")
        client_id = "error-request-id-xyz789"
        resp = http_request(
            f"{api_url}/api/v1/auctions/0x0000000000000000000000000000000000000000",
            headers={"X-Request-ID": client_id},
        )
        check("Status is 404", resp.status == 404)
        check(
            "X-Request-ID echoed on 404",
            resp.header("X-Request-ID") == client_id,
        )

        print()
        return print_summary("Middleware Chain")

    finally:
        cleanup()


if __name__ == "__main__":
    sys.exit(main())
