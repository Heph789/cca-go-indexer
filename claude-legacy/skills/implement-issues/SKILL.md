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

### 5. Simplify

Use the simplifier agent to clean up the code. Watch especially for dead code.

### 6. PR

Remove the `in-progress` label from the issue:

```bash
gh issue edit <ISSUE_NUMBER> --remove-label "in-progress"
```

Use /create-pr to create a **draft** PR based on the previous issue's branch. Label the PR as `pending review`. After creating the PR, link it to the related issue using the GitHub GraphQL API:

```bash
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

## Compaction

When compacting, only keep the instructions for this skill and relevant context for the issue currently being worked on.
