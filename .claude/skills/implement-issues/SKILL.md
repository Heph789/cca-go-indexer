---
name: implement-issues
description: Sequentially implement GitHub sub-issues from a parent issue using TDD (red-green-simplify), creating branches, commits, and draft PRs for each.
argument-hint: "GitHub parent issue URL"
---

# Implement Issues

Implement the sub-issues of the given parent issue: `$ARGUMENTS`

Work through each sub-issue sequentially in order. Do not combine work from separate issues or parallelize work across issues. A scaffold is referenced in the parent issue — DO NOT COPY IMPLEMENTATION CODE FROM THE SCAFFOLD. It is only a reference for structure and design. Prioritize design decisions already made in implemented code over the scaffold.

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

Checkout a new branch from the previous issue's branch using the naming convention defined by the issue.

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

### 6. PR

Remove the `in-progress` label from the issue:

```bash
gh issue edit <ISSUE_NUMBER> --remove-label "in-progress"
```

Use /create-pr to create a **draft** PR based on the previous issue's branch. Label the PR as `pending review`.

**Link the PR to the issue.** Because stacked PRs merge into other feature branches (not `main`), GitHub's `Closes #N` keyword will NOT auto-close issues. Instead:

1. **PR body reference.** The PR body MUST contain `Addresses #<ISSUE_NUMBER>` so the PR and issue are cross-referenced in GitHub's UI.

2. **Close the issue explicitly** after the PR is merged:

```bash
gh issue close <ISSUE_NUMBER> --repo <OWNER/REPO>
```

Do NOT close the issue when creating the draft PR — only after it is merged.

### 7. Next

Move on to the next sub-issue.

## Compaction

When compacting, only keep the instructions for this skill and relevant context for the issue currently being worked on.
