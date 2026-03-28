---
name: review-pr
description: Triage PR review feedback — validate notes, group into issues, check if child branches already resolved them, and decide what to fix in this PR vs defer. Outputs a triage doc for other skills to act on.
argument-hint: "PR number or URL (optional — defaults to current branch's PR)"
---

# Review PR

Triage review feedback on the PR for the current branch (or `$ARGUMENTS` if provided).

This repo uses a **stacked PR system managed by Graphite** where sub-issues of a feature are implemented sequentially, each branch stacked on the previous one. The naming convention is `<parent-branch>-/<issue-branch>`, forming a linear stack. Graphite tracks the stack metadata and handles cascade rebasing when upstream branches change.

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
3. **Check later branches in the stack.** In this stacked workflow, "later branches" are NOT branches nested under the current branch name — they are the branches for subsequent issues in the parent issue's sub-issue list.

   To find them:
   1. From the PR, find the linked issue number.
   2. Find the parent issue (listed in the issue body as `**Parent issue:** #N`).
   3. Get the parent's sub-issue list in order.
   4. Find the current issue's position in that list.
   5. Every sub-issue **after** it in the list is a "later" issue. Check its branch name (from the issue body's `## Branch name` section).
   6. For each later branch that exists locally or has a PR, check if the review issue is already resolved in that branch's commits (`git log <current-branch>..<later-branch>`) or PR diff.

   If resolved in a later branch, mark the issue as **"already resolved"** with a link to the PR.
4. **Decide disposition** for unresolved issues:
   - **Fix in this PR** — the default choice. Since Graphite handles cascade rebasing via `gt stack restack`, even changes that modify interfaces or rename symbols can be fixed in place without manual rebase pain. Prefer this unless the fix is truly out of scope or would be a large, risky refactor.
   - **Create sub-issue** — if the fix is large enough to warrant its own PR (significant new functionality, major refactor) or is clearly a separate unit of work, even though it belongs to this feature. It will be inserted into the parent issue's queue. Note why.
   - **Create standalone issue** — if the fix is out of scope of the parent feature entirely (e.g., cross-cutting concern, tech debt, unrelated bug). It will be created as a root issue, not linked as a sub-issue. Note why.

### Disposition Guidelines

With Graphite managing the stack, the old concern about "risky changes causing manual rebases of downstream branches" is largely eliminated. The decision should now be based on:

- **Scope:** Does this fix belong in this PR's issue, or is it separate work?
- **Size:** Is it small enough to be a quick fix, or large enough to warrant its own review cycle?
- **Risk:** Would the change introduce significant regression risk that warrants isolated testing?

When in doubt, disposition as "fix in this PR."

### Invalid Notes

List them at the end of the issue doc with a brief explanation of why each is invalid.

## Output Format

The issue doc (`local_ignored/<pr_identifier>_issues.md`) should have these sections:

```markdown
# PR #<number> Review Triage

## Issues to Fix in This PR
<!-- default disposition — fix here unless there's a clear reason not to -->

## Issues Already Resolved
<!-- resolved in a later branch in the stack — link to the PR -->

## Sub-Issues to Defer
<!-- in-scope but large enough to warrant a separate PR -->

## Standalone Issues
<!-- out of scope for the feature — will be created as root issues -->

## Invalid Notes
<!-- notes that don't need action, with reasoning -->
```

## Next Steps

After triage is complete, hand the triage doc to the appropriate skill:

- **`/fix-pr-issues <path>`** — implements the "Issues to Fix in This PR" items, then restacks and submits the stack.
- **`/push-review-issues <path>`** — creates GitHub issues for deferred sub-issues and standalone issues.

Do not implement fixes yourself. This skill's job ends when the triage doc is written.

## Compaction

When compacting, keep only these instructions and the current state of the triage (which notes have been evaluated, current disposition decisions).
