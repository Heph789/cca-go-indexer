---
name: fix-pr-issues
description: Implement fixes from a review triage doc's "Issues to Fix in This PR" section using TDD in subagents, then restack, submit, and reply to review notes.
argument-hint: "path to triage doc (e.g. local_ignored/pr47_issues.md)"
---

# Fix PR Issues

Read the review triage doc at `$ARGUMENTS` and implement the fixes listed under "Issues to Fix in This PR."

This skill only handles in-PR fixes. Deferred items (sub-issues, standalone issues) are handled by `/push-review-issues`.

## Process

### 1. Parse

Read the triage doc. Extract each issue from the "Issues to Fix in This PR" section — title, description, and which review notes it addresses.

### 2. Checkout

Checkout the branch for the PR referenced in the triage doc header. Confirm you're on the correct branch before making changes.

### 3. Fix Each Issue

Work through each issue sequentially. Each issue is implemented in its own subagent following a TDD approach:

#### Red Phase — Tests First

Use a subagent to update or add tests that reflect the reviewer's feedback. If the fix changes behavior, the new/updated tests should fail against the current code. Commit the test changes.

#### Green Phase — Implementation

Use a subagent to implement the fix such that all tests pass. Keep fixes minimal and focused on what the reviewer asked for. Commit the implementation.

#### Simplify

Use the simplifier agent to clean up the code touched by this fix. Commit if there are changes.

Do not batch unrelated fixes together. Each issue gets its own red-green-simplify cycle with its own commits.

### 4. Restack

After all fixes are committed, cascade the changes to downstream branches:

```bash
gt stack restack
```

### 5. Verify Downstream

Check that downstream branches still compile and pass tests. If restack caused conflicts that couldn't be auto-resolved, report them to the user rather than guessing at resolutions.

### 6. Submit

Update all PRs in the stack:

```bash
gt stack submit
```

### 7. Reply to Review Notes

For each fixed issue, reply to the original review note(s) on the PR explaining what was changed. Use `gh` to post the reply:

```bash
gh api repos/{owner}/{repo}/pulls/{pr}/comments/{comment_id}/replies -f body='<reply>'
```

Keep replies concise — state what was fixed and reference the commit SHA. If a single issue addresses multiple review notes, reply to each one.

### 8. Update Triage Doc

Mark each fixed issue in the triage doc as completed with the commit SHA.

### 9. Report

Print a summary: which issues were fixed, commit SHAs, review notes replied to, and whether downstream branches are clean.

## Compaction

When compacting, keep only these instructions and the list of remaining unfixed issues from the triage doc.
