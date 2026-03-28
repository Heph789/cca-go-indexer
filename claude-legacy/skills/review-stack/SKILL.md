---
name: review-stack
description: Walk a stack of PRs and run the full review pipeline on each — itemize review, triage, push deferred issues, fix in-PR issues, merge.
argument-hint: "parent issue URL or number"
---

# Review Stack

Walk the PR stack for the given parent issue (`$ARGUMENTS`) and run the full review pipeline on each PR that has not yet been reviewed and merged.

## Process

### 1. Build the PR List

1. Fetch the parent issue and identify the **parent feature branch** (from the issue body's `## Branch name` section). This is the merge target for all PRs in the stack.
2. Fetch the parent issue's sub-issue list in order.
3. For each sub-issue, check if it has an open or draft PR.
4. Build an ordered list of PRs from bottom of stack to top.

### 2. Walk the Stack Bottom-Up

Process each PR starting from the bottom of the stack (earliest branch). Bottom-up order is important because fixes to earlier branches may resolve issues that appear in later branches.

**Each PR is handled by its own subagent.** This keeps context focused and prevents one PR's review from polluting another's. Wait for each subagent to complete before moving to the next — the stack must be processed sequentially.

For each PR, spawn a subagent that performs steps a–f:

#### a. Itemize Review — `/itemize-review <PR>`

Review the PR, itemize the review, then post each item as a review note on the PR.

#### b. Triage — `/review-pr <PR>`

Run the review-pr skill to triage all review notes on this PR. This produces a triage doc at `local_ignored/<pr_identifier>_issues.md`.

#### c. Defer — `/push-review-issues <triage-doc>`

If the triage doc has items under "Sub-Issues to Defer" or "Standalone Issues", run the push-review-issues skill. This creates GitHub issues and inserts sub-issues into the parent's queue.

#### d. Fix — `/fix-pr-issues <triage-doc>`

If the triage doc has items under "Issues to Fix in This PR", run the fix-pr-issues skill. This implements each fix with TDD, pushes, and replies to the review notes.

#### e. Retarget and Merge

Before merging, retarget the PR's base branch to the **parent feature branch** (the branch for the parent issue, e.g., `indexer-api-happy-1`). Earlier merges in the stack will have already landed on this branch, so the PR's original base (the previous issue's branch) is stale.

```bash
gh pr edit <PR_NUMBER> --base <parent-feature-branch>
```

Then merge with regular merge (NOT squash — squash is only for merging into main):

```bash
gh pr merge <PR_NUMBER> --merge
```

Close the linked issue after merge:

```bash
gh issue close <ISSUE_NUMBER>
```

#### f. Report

Print a summary for this PR before moving to the next:
- Issues fixed (with commit SHAs)
- Issues already resolved in later branches
- Issues deferred (with new issue numbers)
- Invalid notes skipped

### 3. Stack Summary

After all PRs are processed, print a full summary:

```
Stack Review Complete
=====================
PRs reviewed: N
PRs merged: N
Issues fixed in-PR: N
Issues deferred as sub-issues: N
Standalone issues created: N
Issues already resolved downstream: N
Invalid notes: N
```

## Skipping PRs

- PRs that are already merged are skipped.
- If a PR's review feedback was left by the implementing agent (not a human reviewer), skip the itemize-review step — don't review your own review notes.

## Compaction

When compacting, keep these instructions, the PR list from step 1, and which PRs have been completed.
