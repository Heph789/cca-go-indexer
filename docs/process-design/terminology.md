# Terminology

## The Stack

A **stack** is a linear chain of branches and their associated PRs, all implementing sub-issues of a single parent issue. Each branch builds on the previous one, so commits accumulate along the chain.

```
feature branch (base)
 └─ branch A (stacked on feature branch)
     └─ branch B (stacked on A)
         └─ branch C (stacked on B)
             └─ branch D (stacked on C)
```

## Feature Branch

The **feature branch** (also called the **parent branch**) is the root of the stack. It is created from `main` and named after the parent issue (e.g., `indexer-api-happy-1`). All issue branches in the stack are eventually merged into this branch. When the entire feature is complete, the feature branch is squash-merged into `main`.

## Issue Branch

An **issue branch** is a single branch in the stack that implements one sub-issue. It is stacked on the previous issue branch (or on the feature branch if it's the first in the stack).

**Naming:** `<feature-branch>-/<issue-name>-<version>`

Example: `indexer-api-happy-1-/pg-store-1`

- `indexer-api-happy-1` — the feature branch prefix
- `-/` — delimiter separating the feature branch from the issue name
- `pg-store` — short name for this issue
- `-1` — implementation version (increments if the issue is re-implemented)

## PR

Each issue branch gets a **draft PR**. The PR's base branch is the previous issue branch in the stack (showing only that issue's diff). During the review-stack process, PRs are retargeted to the feature branch before merging.

## Parent Issue

The **parent issue** is the GitHub issue that tracks the entire feature. Its sub-issue list defines the implementation order for the stack. All issue branches correspond to sub-issues of the parent issue.

## Sub-Issue

A **sub-issue** is one unit of work within the parent issue. Each sub-issue maps to exactly one issue branch and one PR. Sub-issues are implemented sequentially, top-to-bottom as listed in the parent issue.

## Phase

A **phase** is a group of sub-issues that share a goal. Sub-issues are organized into sequential phases:

- **Phase 1 — Happy Path:** Minimum viable feature, no error handling
- **Phase 2 — Resilience:** Retries, error recovery, failure handling
- **Phase 3 — Production Readiness:** Middleware, probes, caching, polish

All Phase 1 sub-issues come before Phase 2, etc.

## QA Gate

A **QA gate** is a special sub-issue at the end of each phase. Instead of implementing product code, it designs and runs end-to-end verification experiments that prove the phase's goals were met. QA gates are tagged `[QA]` and use a different issue format and implementation skill (`/implement-qa-gate`) than regular sub-issues.

## Scaffold Branch

A **scaffold branch** is typically the first branch in the stack (e.g., `indexer-api-happy-1-/scaffold-1`). It contains structural pseudo-code — types, interfaces, function signatures, and TODO comments — that serves as a design reference for implementation. Scaffold code is not copied verbatim; implementation branches prioritize design decisions in implemented code over the scaffold.

## Version

The trailing number on an issue branch name (e.g., `-1`, `-2`). Version 1 is the first implementation attempt. If a major approach change invalidates work from a certain point forward, all affected branches get new versions:

```
indexer-api-happy-1-/pg-store-1          → version 1 (abandoned)
indexer-api-happy-1-/pg-store-2          → version 2 (re-implemented)
indexer-api-happy-1-/repositories-2      → downstream also re-versioned
```

## Upstream / Downstream

- **Upstream:** Branches earlier in the stack (closer to the feature branch). Changes to upstream branches affect all downstream branches.
- **Downstream:** Branches later in the stack (further from the feature branch). Downstream branches contain all commits from upstream branches.

## Previous Issue / Previous Branch

The **previous issue** (or **previous branch**) is the one immediately before the current issue in the stack. The current issue branch is stacked directly on top of the previous branch. For the first issue in the stack, the previous branch is the feature branch.

## Next Issue / Next Branch

The **next issue** (or **next branch**) is the one immediately after the current issue in the stack. It is stacked on top of the current branch.

## Triage Doc

A **triage doc** is a markdown file (in `local_ignored/`) produced by the `/review-pr` skill. It categorizes review feedback into: issues to fix in this PR, issues already resolved downstream, sub-issues to defer, standalone issues, and invalid notes. It is consumed by `/fix-pr-issues` and `/push-review-issues`.

## Conventions File

A **conventions file** (`docs/conventions.md`) accumulates project-specific patterns learned from review feedback. Entries describe what to do (and what not to do), with a reference to where the pattern was learned. Agents read it before starting implementation work. See `docs/process-design/ideas/stack-wide-review-fixes.md`.
