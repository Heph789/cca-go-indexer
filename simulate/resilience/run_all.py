#!/usr/bin/env python3
"""Run all resilience QA scenarios sequentially."""

import sys

import run_retry_recovery
import run_retry_budget
import run_reorg_rollback

SCENARIOS = [
    ("Retry Recovery", run_retry_recovery.main),
    ("Retry Budget Exhaustion", run_retry_budget.main),
    ("Reorg Rollback", run_reorg_rollback.main),
]


def main() -> int:
    print("=========================================")
    print("  Resilience QA: All Scenarios")
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
        print("  ALL SCENARIOS PASSED")
    else:
        print("  SOME SCENARIOS FAILED")
    print("=========================================")

    return 0 if all_passed else 1


if __name__ == "__main__":
    sys.exit(main())
