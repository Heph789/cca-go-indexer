#!/usr/bin/env python3
"""Production readiness QA: Cache-Control header experiments.

Verifies that cache headers are correct, well-formed, and only present
on the right responses.

Experiments:
  1. 200 auction response has Cache-Control with correct directives
  2. Directive validation: public, max-age=86400, immutable are well-formed
  3. 404 auction response has NO Cache-Control header
  4. 400 auction response has NO Cache-Control header
  5. No cache leak: 200 followed by 404 — the 404 must not carry cache headers
  6. Response immutability: two GETs to the same auction return identical bodies
"""

import sys

from helpers import (
    API_PORT,
    ANVIL_PORT,
    cleanup,
    start_anvil,
    deploy_contracts,
    truncate_all,
    start_indexer,
    wait_for_cursor,
    start_api,
    http_request,
    parse_cache_control,
    check,
    reset_results,
    print_summary,
)


def main() -> int:
    reset_results()
    api_url = f"http://127.0.0.1:{API_PORT}"
    rpc_url = f"http://127.0.0.1:{ANVIL_PORT}"

    try:
        print("=========================================")
        print("  Production QA: Cache-Control Headers")
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
        print("--- Experiment 1: Cache-Control present on 200 ---")
        resp = http_request(f"{api_url}/api/v1/auctions/{auction}")
        check("Status is 200", resp.status == 200)
        cc_raw = resp.header("Cache-Control")
        check("Cache-Control header present on 200", cc_raw is not None)

        print()
        print("--- Experiment 2: Directive validation ---")
        cc = parse_cache_control(cc_raw)
        # 'public' must be present (not 'private' which would break CDN caching)
        check("Directive 'public' present", cc.get("public") is True)
        check("Directive 'private' absent", "private" not in cc)
        # max-age must be a valid integer
        max_age = cc.get("max-age")
        check("Directive 'max-age' present", max_age is not None)
        try:
            max_age_int = int(max_age)
            check("max-age is valid integer", True)
            check("max-age equals 86400", max_age_int == 86400)
        except (TypeError, ValueError):
            check("max-age is valid integer", False)
            check("max-age equals 86400", False)
        # immutable must be a standalone directive (not a substring)
        check("Directive 'immutable' present", cc.get("immutable") is True)
        # no-store must NOT be present (would conflict with caching)
        check("Directive 'no-store' absent on auction 200", "no-store" not in cc)

        print()
        print("--- Experiment 3: No Cache-Control on 404 ---")
        resp = http_request(
            f"{api_url}/api/v1/auctions/0x0000000000000000000000000000000000000000"
        )
        check("Status is 404", resp.status == 404)
        check(
            "Cache-Control absent on 404",
            resp.header("Cache-Control") is None,
        )

        print()
        print("--- Experiment 4: No Cache-Control on 400 ---")
        resp = http_request(f"{api_url}/api/v1/auctions/not-a-valid-address")
        check("Status is 400", resp.status == 400)
        check(
            "Cache-Control absent on 400",
            resp.header("Cache-Control") is None,
        )

        print()
        print("--- Experiment 5: No cache leak across requests ---")
        # First, a 200 with cache headers
        resp1 = http_request(f"{api_url}/api/v1/auctions/{auction}")
        check("First request is 200", resp1.status == 200)
        check("First request has Cache-Control", resp1.header("Cache-Control") is not None)
        # Then, a 404 that must NOT inherit cache headers
        resp2 = http_request(
            f"{api_url}/api/v1/auctions/0x0000000000000000000000000000000000000000"
        )
        check("Second request is 404", resp2.status == 404)
        check(
            "404 after 200 has no Cache-Control (no leak)",
            resp2.header("Cache-Control") is None,
        )

        print()
        print("--- Experiment 6: Response immutability ---")
        # Two GETs to the same auction should return byte-identical bodies,
        # confirming the 'immutable' cache directive is safe to trust.
        resp_a = http_request(f"{api_url}/api/v1/auctions/{auction}")
        resp_b = http_request(f"{api_url}/api/v1/auctions/{auction}")
        check("Both requests return 200", resp_a.status == 200 and resp_b.status == 200)
        check(
            "Response bodies are byte-identical",
            resp_a.body == resp_b.body,
        )

        print()
        return print_summary("Cache-Control Headers")

    finally:
        cleanup()


if __name__ == "__main__":
    sys.exit(main())
