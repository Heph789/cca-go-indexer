#!/usr/bin/env python3
"""Production readiness QA: Health and readiness probe experiments.

Verifies that probes behave correctly under various DB states.

Experiments:
  1. /health returns 200 with DB up
  2. /ready returns 200 with DB up
  3. /ready returns 503 when DB is down
  4. /health returns 200 even when DB is down
  5. /ready recovers to 200 after DB restart (flap test)
  6. /health has Cache-Control: no-store
  7. /ready has Cache-Control: no-store (both 200 and 503)
"""

import sys

from helpers import (
    API_PORT,
    cleanup,
    start_api,
    http_request,
    parse_cache_control,
    stop_postgres,
    start_postgres,
    check,
    reset_results,
    print_summary,
)


def _safe_json(resp) -> dict:
    """Parse JSON body, returning empty dict on failure."""
    try:
        return resp.json()
    except Exception:
        return {}


def main() -> int:
    reset_results()
    api_url = f"http://127.0.0.1:{API_PORT}"
    pg_was_stopped = False

    try:
        print("=========================================")
        print("  Production QA: Health & Readiness Probes")
        print("=========================================")
        print()

        # --- Setup: start API with DB up ---
        start_api()

        print()
        print("--- Experiment 1: /health returns 200 with DB up ---")
        resp = http_request(f"{api_url}/health")
        check("/health status is 200", resp.status == 200)
        body = _safe_json(resp)
        check("/health body has status=ok", body.get("status") == "ok")

        print()
        print("--- Experiment 2: /ready returns 200 with DB up ---")
        resp = http_request(f"{api_url}/ready")
        check("/ready status is 200", resp.status == 200)
        body = _safe_json(resp)
        check("/ready body has status=ready", body.get("status") == "ready")

        print()
        print("--- Experiment 3: /ready returns 503 when DB is down ---")
        stop_postgres()
        pg_was_stopped = True
        resp = http_request(f"{api_url}/ready")
        check("/ready status is 503", resp.status == 503)
        body = _safe_json(resp)
        check("/ready body has status=not_ready", body.get("status") == "not_ready")

        print()
        print("--- Experiment 4: /health returns 200 even when DB is down ---")
        resp = http_request(f"{api_url}/health")
        check("/health status is 200 with DB down", resp.status == 200)

        print()
        print("--- Experiment 5: /ready recovers after DB restart ---")
        start_postgres()
        pg_was_stopped = False
        # pgxpool may need a few seconds to re-establish connections
        # after Postgres restarts. Poll up to 10s for readiness.
        import time
        ready_recovered = False
        deadline = time.time() + 10
        while time.time() < deadline:
            resp = http_request(f"{api_url}/ready")
            if resp.status == 200:
                ready_recovered = True
                break
            time.sleep(1)
        check("/ready status is 200 after DB restart", ready_recovered)

        print()
        print("--- Experiment 6: /health has Cache-Control: no-store ---")
        resp = http_request(f"{api_url}/health")
        cc = parse_cache_control(resp.header("Cache-Control"))
        check("/health Cache-Control contains no-store", cc.get("no-store") is True)

        print()
        print("--- Experiment 7: /ready has Cache-Control: no-store ---")
        resp = http_request(f"{api_url}/ready")
        cc = parse_cache_control(resp.header("Cache-Control"))
        check("/ready 200 Cache-Control contains no-store", cc.get("no-store") is True)

        # Also check 503 response has no-store
        stop_postgres()
        pg_was_stopped = True
        resp = http_request(f"{api_url}/ready")
        cc = parse_cache_control(resp.header("Cache-Control"))
        check("/ready 503 Cache-Control contains no-store", cc.get("no-store") is True)

        # Restore Postgres
        start_postgres()
        pg_was_stopped = False

        print()
        return print_summary("Health & Readiness Probes")

    finally:
        if pg_was_stopped:
            try:
                start_postgres()
            except Exception:
                print("WARNING: Failed to restart Postgres — please restart manually")
        cleanup()


if __name__ == "__main__":
    sys.exit(main())
