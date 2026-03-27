# Stacked Branch System

This project uses a stacked PR workflow (managed by [Graphite](https://graphite.dev)) where AI agents implement features by working through a linear chain of sub-issues, each building on the last. This document describes how the system works.

## Overview

A feature is decomposed into a **parent issue** with ordered **sub-issues**. An AI agent (via the `/implement-issues` skill) works through each sub-issue sequentially, producing a stack of branches and draft PRs. A human reviews the PRs afterward. Graphite manages the stack metadata, handles cascade rebasing, and automates PR submission.

## Branch Naming Convention

```
<parent-branch>-/<issue-name>-<version>
```

The `-/` delimiter separates the base branch from the issue-specific name. The trailing `-N` is a **version number**. Version 1 is the first implementation attempt. If a major approach change earlier in the chain invalidates later work, we re-implement from that point forward, incrementing the version number for each re-implemented issue. For example, if `indexer-loop-happy` was first implemented as version 1 but needs to be redone after an approach change, the new branch would be `indexer-loop-happy-2`. All subsequent issues in the chain would also get new version-2 branches. The old version-1 chain remains for reference but is effectively abandoned.

Each branch is stacked on the previous issue's branch, forming a linear chain:

```
main
 └─ indexer-api-happy-1                          (parent issue branch)
     ├─ indexer-api-happy-1-/scaffold-1           (issue A — stacked on parent)
     ├─ indexer-api-happy-1-/indexer-loop-happy-1  (issue B — stacked on A)
     ├─ indexer-api-happy-1-/auction-handler-1     (issue C — stacked on B)
     ├─ indexer-api-happy-1-/api-auction-get-1     (issue D — stacked on C)
     ├─ indexer-api-happy-1-/pg-store-1            (issue E — stacked on D)
     ├─ indexer-api-happy-1-/repositories-1        (issue F — stacked on E)
     ├─ indexer-api-happy-1-/eth-client-1          (issue G — stacked on F)
     ├─ indexer-api-happy-1-/entrypoints-1         (issue H — stacked on G)
     └─ indexer-api-happy-1-/qa-happy-path-1       (issue I — stacked on H)
```

All issue branches share the same parent prefix, but each one is **stacked on the prior issue's branch**, not on the parent branch directly. This means commits accumulate along the chain.

### Versioning Example

This repo has gone through multiple iterations. The `indexer-1` chain with `simple-loop-1` sub-issues was an earlier attempt. When the approach changed, a new chain started as `indexer-api-happy-1` with its own sub-issues — effectively version 2 of the feature, with a new parent issue and fresh sub-issue branches all at version 1. If a specific sub-issue within a chain needed to be re-implemented (e.g., due to a review-driven approach change partway through), the version number on that issue and all subsequent issues would increment:

```
indexer-api-happy-1-/pg-store-1          (original)
indexer-api-happy-1-/pg-store-2          (re-implemented after approach change)
indexer-api-happy-1-/repositories-2      (subsequent issues also get version 2)
indexer-api-happy-1-/eth-client-2        (and so on down the chain)
```

## Graphite Stack Management

[Graphite CLI](https://graphite.dev/docs/graphite-cli) (`gt`) manages the stack:

- **`gt branch create <name>`** — creates a new branch stacked on the current branch, with Graphite tracking the parent relationship.
- **`gt stack submit --draft`** — creates or updates draft PRs for all branches in the stack. Graphite sets the correct base branch for each PR and adds a stack overview to PR descriptions.
- **`gt stack restack`** — rebases all branches in the stack on top of their updated parents. Use this after making changes to an earlier branch (e.g., addressing review feedback).
- **`gt stack test`** — runs tests across all branches in the stack.

### Why Graphite?

Before Graphite, changes to an earlier branch required manually rebasing every downstream branch — tedious and error-prone with 10+ branches in a chain. This made review feedback expensive to address in-place, so the workflow biased toward deferring fixes to new sub-issues. With Graphite's `gt stack restack`, cascade rebasing is a single command, making it practical to fix feedback directly in the branch where it belongs.

## PR Structure

Each issue branch gets its own **draft PR**, with the base set to the previous issue's branch:

```
PR for issue B:  base=scaffold-1  head=indexer-loop-happy-1
PR for issue C:  base=indexer-loop-happy-1  head=auction-handler-1
```

This means each PR shows only the diff for that specific issue, even though the branch contains all prior work. Graphite adds a stack navigation section to each PR description showing where it sits in the chain.

## Implementation Flow (per issue)

The `/implement-issues` skill drives this process:

1. **Triage** — skip closed issues, issues with existing PRs, or issues labeled `question`
2. **Branch** — `gt branch create` a new branch stacked on the previous issue's branch
3. **Red** — write failing tests first (TDD)
4. **Green** — implement until tests pass, committing at each step
5. **Simplify** — clean up code, remove dead code
6. **PR** — `gt stack submit --draft` to create/update PRs, label `pending review`, link to the issue

## Review and Feedback

Because Graphite handles cascade rebasing, review feedback on any PR in the stack can be addressed in place:

1. **Fix** the issue on the relevant branch
2. **Restack** with `gt stack restack` to cascade the change forward
3. **Submit** with `gt stack submit` to update all PRs

The `/review-pr` skill triages feedback and defaults to fixing in the current PR. Issues are only deferred to new sub-issues when they represent genuinely separate units of work (not because of rebase difficulty).

### When to Still Defer

- The fix is large enough to warrant its own review cycle
- The fix is out of scope for the current feature
- The fix introduces significant regression risk that warrants isolated testing

## Merging

When the stack is approved:

1. Merge PRs bottom-up (starting from the base of the stack)
2. Graphite automatically retargets each subsequent PR's base as its predecessor merges
3. Alternatively, use `gt stack submit` after each merge to ensure PRs stay current

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
