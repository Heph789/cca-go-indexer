# Chained Branch System

This project uses a system where AI agents implement features by working through a linear chain of sub-issues, each building on the last. This document describes how the system works.

## Overview

A feature is decomposed into a **parent issue** with ordered **sub-issues**. An AI agent (via the `/implement-issues` skill) works through each sub-issue sequentially, producing a chain of branches and draft PRs. A human reviews the PRs afterward.

## Branch Naming Convention

```
<parent-branch>-/<issue-name>-<version>
```

The `-/` delimiter separates the base branch from the issue-specific name. The trailing `-N` is a **version number**. Version 1 is the first implementation attempt. If a major approach change earlier in the chain invalidates later work, we re-implement from that point forward, incrementing the version number for each re-implemented issue. For example, if `indexer-loop-happy` was first implemented as version 1 but needs to be redone after an approach change, the new branch would be `indexer-loop-happy-2`. All subsequent issues in the chain would also get new version-2 branches. The old version-1 chain remains for reference but is effectively abandoned.

Each branch is based on the previous issue's branch, forming a linear chain:

```
main
 └─ indexer-api-happy-1                          (parent issue branch)
     ├─ indexer-api-happy-1-/scaffold-1           (issue A — based on parent)
     ├─ indexer-api-happy-1-/indexer-loop-happy-1  (issue B — based on A)
     ├─ indexer-api-happy-1-/auction-handler-1     (issue C — based on B)
     ├─ indexer-api-happy-1-/api-auction-get-1     (issue D — based on C)
     ├─ indexer-api-happy-1-/pg-store-1            (issue E — based on D)
     ├─ indexer-api-happy-1-/repositories-1        (issue F — based on E)
     ├─ indexer-api-happy-1-/eth-client-1          (issue G — based on F)
     ├─ indexer-api-happy-1-/entrypoints-1         (issue H — based on G)
     └─ indexer-api-happy-1-/qa-happy-path-1       (issue I — based on H)
```

All issue branches share the same parent prefix, but each one is **based on the prior issue's branch**, not on the parent branch directly. This means commits accumulate along the chain.

### Versioning Example

This repo has gone through multiple iterations. The `indexer-1` chain with `simple-loop-1` sub-issues was an earlier attempt. When the approach changed, a new chain started as `indexer-api-happy-1` with its own sub-issues — effectively version 2 of the feature, with a new parent issue and fresh sub-issue branches all at version 1. If a specific sub-issue within a chain needed to be re-implemented (e.g., due to a review-driven approach change partway through), the version number on that issue and all subsequent issues would increment:

```
indexer-api-happy-1-/pg-store-1          (original)
indexer-api-happy-1-/pg-store-2          (re-implemented after approach change)
indexer-api-happy-1-/repositories-2      (subsequent issues also get version 2)
indexer-api-happy-1-/eth-client-2        (and so on down the chain)
```

## PR Structure

Each issue branch gets its own **draft PR**, with the base set to the previous issue's branch:

```
PR for issue B:  base=scaffold-1  head=indexer-loop-happy-1
PR for issue C:  base=indexer-loop-happy-1  head=auction-handler-1
```

This means each PR shows only the diff for that specific issue, even though the branch contains all prior work.

## Implementation Flow (per issue)

The `/implement-issues` skill drives this process:

1. **Triage** — skip closed issues, issues with existing PRs, or issues labeled `question`
2. **Branch** — create a new branch from the previous issue's branch
3. **Red** — write failing tests first (TDD)
4. **Green** — implement until tests pass, committing at each step
5. **Simplify** — clean up code, remove dead code
6. **PR** — create a draft PR against the previous issue's branch, label `pending review`, link to the issue

## Review Implications

Because branches form a linear chain, changes to an earlier branch can ripple forward:

- **Safe changes** on an early branch: additive changes (new files, new functions, comments) that don't alter existing interfaces or behavior. Later branches won't conflict.
- **Risky changes** on an early branch: renaming, restructuring, changing interfaces, or modifying code that later branches build on. These require rebasing all subsequent branches in the chain.

The `/review-pr` skill accounts for this when triaging review feedback — it checks whether issues are already resolved in later branches and considers rebase risk when deciding whether to fix in the current PR or defer to a new issue.

## Issue Queue and Ordering

The sub-issue list in the parent issue body defines the implementation order. The `/implement-issues` skill processes them top-to-bottom, skipping any that are closed or already have a PR.

### In-Progress Tracking

When the `/implement-issues` skill starts work on an issue, it labels it `in-progress`. The label is removed when the PR is created. This makes it possible to determine where in the chain the automated system currently is.

### Inserting Follow-On Issues

When review triage (via `/review-pr`) or other work produces new issues that need to be implemented, they are inserted into the parent issue's sub-issue list **right after** the currently `in-progress` issue. If no issue is `in-progress`, they go after the latest issue that already has a PR (the most recent predecessor). This ensures follow-on work is picked up next by the automated system, rather than being appended to the end of the queue.

Example: if issues A through F are listed and D is `in-progress`, a new follow-on issue X gets inserted between D and E:

```
- #30 A  (has PR)
- #31 B  (has PR)
- #32 C  (has PR)
- #33 D  (in-progress)
- #99 X  ← inserted here
- #34 E
- #35 F
```

## Scaffold Branches

Some chains start with a `scaffold` branch (e.g., `indexer-api-happy-1-/scaffold-1`). The scaffold provides a reference implementation for structure and design but is **not meant to be copied verbatim**. The implementation branches should prioritize design decisions already made in implemented code over the scaffold.

## Nesting

Branches can nest further. For example, `indexer-api-happy-1-/scaffold-1-/qa-setup-1` is a sub-chain off the scaffold branch. This follows the same convention — the full parent path is the prefix.
