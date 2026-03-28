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

Use a subagent to create tests before any implementation (table-driven development). Create commits along the way and after finishing all tests. Ensure the tests run and fail.

### 4. Green Phase — Implementation

Use a subagent to implement the issue such that the tests pass. This subagent should implement test by test, committing at each step.

### 5. Simplify

Use the simplifier agent to clean up the code. Watch especially for dead code.

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

After submitting, add the `pending review` label and link the PR to the issue:

```bash
gh pr edit <PR_NUMBER> --add-label "pending review"

# Get the PR node ID
PR_ID=$(gh pr view <PR_NUMBER> --json id -q .id)
# Get the issue node ID
ISSUE_ID=$(gh issue view <ISSUE_NUMBER> --json id -q .id)
# Link the PR to the issue
gh api graphql -f query='mutation {
  updatePullRequest(input: {pullRequestId: "'"$PR_ID"'", closingIssueIds: ["'"$ISSUE_ID"'"]}) {
    pullRequest { id }
  }
}'
```

### 7. Next

Move on to the next sub-issue.

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
