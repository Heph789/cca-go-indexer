---
name: review-pr
description: Triage PR review feedback — validate notes, group into issues, check if child branches already resolved them, and decide what to fix in this PR vs defer.
argument-hint: "PR number or URL (optional — defaults to current branch's PR)"
---

# Review PR

Triage review feedback on the PR for the current branch (or `$ARGUMENTS` if provided).

This repo uses a **chained branch system** where sub-issues of a feature are implemented sequentially, each branch based on the previous one. The naming convention is `<parent-branch>-/<issue-branch>`, forming a linear chain: `parent-/issue-A` → `parent-/issue-B` → `parent-/issue-C`, where each later branch is based on the one before it. A fix in an earlier branch may require rebases of all subsequent branches in the chain.

## Gather Review Notes

Collect all review feedback from these sources:

1. **GitHub PR** — fetch the PR's review comments, inline comments, and review bodies using `gh`.

## Triage Each Note

For every review note, decide: **valid** or **invalid**.

### Valid Notes

1. **Group** notes that share a root cause or theme into a single issue. If several notes all stem from the same underlying problem, that's one issue, not many.
2. **Write an issue doc** in `local_ignored/` named `<pr_identifier>_issues.md`. For each issue include:
   - Title
   - Description (with references to the original review notes)
   - Which review notes it addresses
3. **Check later branches in the chain** — list branches that build on the current branch (`git branch --list "<current-branch>-/*"` and any further descendants). For each, check if the issue is already resolved in a later branch's commits or PR. If so, mark the issue as **"already resolved"** with a link to the PR.
4. **Decide disposition** for unresolved issues:
   - **Fix in this PR** — if the fix is simple and won't cause refactors/rebases on later branches in the chain.
   - **Create sub-issue** — if the fix belongs to this feature but is complex or would force rebases on later branches in the chain. It will be inserted into the parent issue's queue. Note why.
   - **Create standalone issue** — if the fix is out of scope of the parent feature entirely (e.g., cross-cutting concern, tech debt, unrelated bug). It will be created as a root issue, not linked as a sub-issue. Note why.

### Invalid Notes

List them at the end of the issue doc with a brief explanation of why each is invalid.

## Output Format

The issue doc (`local_ignored/<pr_identifier>_issues.md`) should have these sections:

```markdown
# PR #<number> Review Triage

## Issues to Fix in This PR
<!-- issues that are simple, safe, and won't disrupt later branches in the chain -->

## Issues Already Resolved
<!-- resolved in a later branch in the chain — link to the PR -->

## Sub-Issues to Defer
<!-- in-scope for the feature but deferred — will be inserted into the parent issue's queue -->

## Standalone Issues
<!-- out of scope for the feature — will be created as root issues -->

## Invalid Notes
<!-- notes that don't need action, with reasoning -->
```

## Compaction

When compacting, keep only these instructions and the current state of the triage (which notes have been evaluated, current disposition decisions).
