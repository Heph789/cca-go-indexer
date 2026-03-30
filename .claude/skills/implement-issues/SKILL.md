---
name: implement-issues
description: Sequentially implement GitHub sub-issues from a parent issue using TDD (red-green-simplify), creating Graphite-tracked branches, commits, and stacked draft PRs for each.
argument-hint: "GitHub parent issue URL"
---

# Implement Issues

Implement the sub-issues of the given parent issue: `$ARGUMENTS`

Work through each sub-issue sequentially in order. Do not combine work from separate issues or parallelize work across issues. A scaffold is referenced in the parent issue — DO NOT COPY IMPLEMENTATION CODE FROM THE SCAFFOLD. It is only a reference for structure and design. Prioritize design decisions already made in implemented code over the scaffold.

## Prerequisites

This skill requires the [Graphite CLI](https://graphite.dev/docs/graphite-cli) (`gt`) to be installed and authenticated. Graphite manages the stacked branch chain, handles cascade rebasing, and submits stacked PRs.

## Process (per issue)

### 0. Triage

Before starting work on an issue, check its state using `gh`:

- **Closed:** Skip it, move to the next issue.
- **Has an open or draft PR:** Skip it, move to the next issue.
- **Labeled `question`:** Stop the entire process and wait for further instruction from the user.
- **Title starts with `[QA]`:** This is a QA gate issue. Skip the normal implementation steps (2–6) and follow the **QA Gate Handling** section below instead.

### 1. Mark In-Progress

Add the `in-progress` label to the issue:

```bash
gh issue edit <ISSUE_NUMBER> --add-label "in-progress"
```

### 2. Branch

Create a new Graphite-tracked branch from the previous issue's branch:

```bash
gt branch create <branch-name>
```

This automatically tracks the parent branch in Graphite's stack metadata, so later operations like `gt stack restack` and `gt stack submit` work correctly.

### 3. Red Phase — Tests First

Use the `go-tester` agent to create tests before any implementation. This agent writes table-driven tests with thorough comments so reviewers can quickly understand what's being tested and why. Create commits along the way and after finishing all tests. Ensure the tests run and fail.

### 4. Green Phase — Implementation

Use a subagent to implement the issue such that the tests pass. This subagent should implement test by test, committing at each step.

**Commenting standards:**
- Every exported function and method gets a Go doc comment explaining what it does, its parameters, and its return values.
- Every package gets a doc comment in `doc.go` (or at the top of the primary file) explaining the package's purpose and how it fits into the system.
- Non-obvious internal logic gets inline comments explaining *why*, not *what*.

### 5. Simplify

Use the simplifier agent to clean up the code. Watch especially for dead code.

## Subagent Discipline

Steps 3, 4, and 5 MUST be performed by subagents — never in the implementor's own context. If a subagent call fails due to an API error or transient failure, retry the subagent call. Do NOT fall back to doing the work inline.

### 6. PR

Remove the `in-progress` label from the issue:

```bash
gh issue edit <ISSUE_NUMBER> --remove-label "in-progress"
```

Submit the stack to create or update PRs for all branches in the stack:

```bash
gt stack submit --draft
```

This creates a draft PR for the current branch (and updates any existing PRs in the stack). Graphite automatically sets the correct base branch and adds a stack overview to the PR description.

After submitting, add the `pending review` label:

```bash
gh pr edit <PR_NUMBER> --add-label "pending review"
```

**Link the PR to the issue.** Because stacked PRs merge into other feature branches (not `main`), GitHub's `Closes #N` keyword will NOT auto-close issues. Instead:

1. **PR body reference.** The PR body MUST contain `Addresses #<ISSUE_NUMBER>` so the PR and issue are cross-referenced in GitHub's UI.

2. **Close the issue explicitly** after the PR is merged:

```bash
gh issue close <ISSUE_NUMBER> --repo <OWNER/REPO>
```

Do NOT close the issue when creating the draft PR — only after it is merged.

### 7. Next

Move on to the next sub-issue.

## QA Gate Handling

When triage identifies a `[QA]` issue, delegate it entirely to a subagent to preserve the main agent's context. The subagent runs the full QA gate process.

### Subagent Setup

Launch a subagent with:
- The full contents of the `/implement-qa-gate` skill instructions
- The QA gate issue URL/number
- The current branch name (so it knows where to create the gate branch)
- The previous gate branch name (for red-phase verification)

The subagent handles everything: branch creation, test plan design, building the harness, red/green phases, and PR submission.

### After the Subagent Completes

The main agent:
1. Verifies the gate branch and PR were created
2. Moves on to the next sub-issue

If the subagent fails, retry it. Do NOT fall back to running the QA gate inline.

## Restacking After Changes

If a previous branch in the stack is updated (e.g., from review feedback), restack all downstream branches:

```bash
gt stack restack
```

This rebases all branches in the stack on top of their updated parents. Graphite handles the cascade automatically — no manual per-branch rebasing needed.

## Merging

When PRs are approved and ready to merge, use Graphite to merge the stack bottom-up:

```bash
gt stack submit  # ensure all PRs are up to date
```

As each PR merges, Graphite automatically retargets the next PR's base to the correct branch. You can also merge individual PRs and then run `gt stack restack` to update the rest of the chain.

## Compaction

When compacting, only keep the instructions for this skill and relevant context for the issue currently being worked on.
