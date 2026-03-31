---
name: create-issue
description: "Create a single GitHub issue following the project's issue template, optionally linking it as a sub-issue of a parent."
argument-hint: "description of what the issue should cover"
---

# Create Issue

Create a single GitHub issue based on the user's description: `$ARGUMENTS`

## Process

### 1. Gather Context

Read the relevant code, scaffold, or existing issues to understand what the issue should cover. Ask the user for clarification if the scope is ambiguous.

### 2. Draft

Draft the issue using the project's standard format. Show it to the user for confirmation before creating.

### 3. Create

Create the issue using `gh issue create` with `--title`, `--body`, and `--label`. Pass the body via HEREDOC.

## Branch Name Convention

- **Parent (top-level) issues:** `<short-name>-<number>` (e.g., `indexer-api-1`, `batch-processor-2`)
- **Sub-issues:** `<parent-branch>-/<sub-issue-name>` (e.g., `indexer-api-1-/rpc-client`, `indexer-api-1-/block-parser`)

## Issue Body Format

```markdown
**Phase: <phase name>**

## Description / Requirements
<what needs to be built and why>

## Branch name
`<branch-name>`

## Scaffold
<scaffold branch and relevant files, or "None">

## Unit Tests (TDD)
<bullet list of test cases to write before implementation>

## Integration Tests
<integration test cases, or "None">

## Dependencies
<external packages or internal work this depends on>

## Blocked by
<#N references to GitHub issues, or "None">

## Principles
<relevant design principles or constraints>

**Parent issue:** #N (if applicable)
**Next issue:** #N (if applicable)
```

### 4. Link as Sub-Issue (if parent specified)

If the user specifies a parent issue, link the new issue as a sub-issue using the GraphQL API:

```bash
PARENT_ID=$(gh issue view <PARENT_NUMBER> --json id -q .id)
CHILD_ID=$(gh issue view <NEW_ISSUE_NUMBER> --json id -q .id)
gh api graphql -f query='mutation {
  addSubIssue(input: {issueId: "'"$PARENT_ID"'", subIssueId: "'"$CHILD_ID"'"}) {
    issue { id }
  }
}'
```

To insert at a specific position, add `afterId`:

```bash
AFTER_ID=$(gh issue view <AFTER_ISSUE_NUMBER> --json id -q .id)
gh api graphql -f query='mutation {
  addSubIssue(input: {issueId: "'"$PARENT_ID"'", subIssueId: "'"$CHILD_ID"'", afterId: "'"$AFTER_ID"'"}) {
    issue { id }
  }
}'
```

Apply the appropriate phase label (`phase:happy-path`, `phase:resilience`, `phase:production`). Apply `qa` label if this is a QA gate issue.

### 5. Report

Print the created issue number, title, and URL.
