#!/usr/bin/env python3
"""Run all production readiness QA experiments sequentially."""

import sys

import run_middleware
import run_probes
import run_cache

SCENARIOS = [
    ("Middleware Chain", run_middleware.main),
    ("Health & Readiness Probes", run_probes.main),
    ("Cache-Control Headers", run_cache.main),
]


def main() -> int:
    print("=========================================")
    print("  Production Readiness QA: All Experiments")
    print("=========================================")
    print()

    results = []
    for name, fn in SCENARIOS:
        exit_code = fn()
        results.append((name, exit_code))
        print()

    print("=========================================")
    print("  Aggregate Results")
    print("=========================================")
    all_passed = True
    for name, code in results:
        status = "PASSED" if code == 0 else "FAILED"
        print(f"  {name}: {status}")
        if code != 0:
            all_passed = False

    print()
    if all_passed:
        print("  ALL EXPERIMENTS PASSED")
    else:
        print("  SOME EXPERIMENTS FAILED")
    print("=========================================")

    return 0 if all_passed else 1


if __name__ == "__main__":
    sys.exit(main())
