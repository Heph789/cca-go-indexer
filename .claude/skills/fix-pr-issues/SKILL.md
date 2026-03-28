---
name: fix-pr-issues
description: Implement fixes from a review triage doc's "Issues to Fix in This PR" section using TDD in subagents, then push and reply to review notes.
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

Use the `go-tester` agent to update or add tests that reflect the reviewer's feedback. If the fix changes behavior, the new/updated tests should fail against the current code. **Commit the test changes before moving to implementation.**

#### Green Phase — Implementation

Use a subagent to implement the fix such that all tests pass. Keep fixes minimal and focused on what the reviewer asked for. **Commit after each meaningful step** — if the fix touches multiple files or involves distinct logical changes, commit each one separately rather than batching into a single commit. At minimum, commit once after all tests pass.

#### Simplify

Use the simplifier agent to clean up the code touched by this fix. Commit if there are changes.

**Commit discipline:** Do not batch unrelated fixes together. Each issue gets its own red-green-simplify cycle. Commit at least once per phase (red, green, simplify), and more often if the work within a phase is significant.

### 4. Push

After all fixes are committed, push the branch:

```bash
git push
```

### 5. Reply to Review Notes

For each fixed issue, reply to the original review note(s) on the PR explaining what was changed. Use `gh` to post the reply:

```bash
gh api repos/{owner}/{repo}/pulls/{pr}/comments/{comment_id}/replies -f body='<reply>'
```

Keep replies concise — state what was fixed and reference the commit SHA. If a single issue addresses multiple review notes, reply to each one.

### 6. Update Triage Doc

Mark each fixed issue in the triage doc as completed with the commit SHA.

### 7. Report

Print a summary: which issues were fixed, commit SHAs, and review notes replied to.

## Compaction

When compacting, keep only these instructions and the list of remaining unfixed issues from the triage doc.
