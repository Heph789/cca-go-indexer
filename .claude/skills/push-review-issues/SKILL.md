---
name: push-review-issues
description: Create GitHub issues from a review triage doc and insert them into the parent issue's sub-issue queue at the right position.
argument-hint: "path to triage doc (e.g. local_ignored/pr47_issues.md)"
---

# Push Review Issues

Read the review triage doc at `$ARGUMENTS` and create GitHub issues for deferred items. The triage doc has two types of deferred issues:

- **Sub-Issues to Defer** — in-scope for the feature; created as sub-issues of the parent and inserted into the queue.
- **Standalone Issues** — out of scope for the feature; created as root issues with no parent relationship.

## Process

### 1. Parse

Read the triage doc. Extract issues from both the "Sub-Issues to Defer" and "Standalone Issues" sections — title, description, reasoning, and type (sub-issue vs standalone).

For sub-issues: identify the parent issue by looking at the PR referenced in the triage doc header, then finding the parent issue from the PR's linked issue.

### 2. Confirm

Show the user a numbered list of issues to be created, grouped by type:
- **Sub-issues** (will be linked to parent, inserted into queue)
- **Standalone issues** (root issues, no parent)

Ask for confirmation before creating anything.

### 3. Determine Insertion Point (sub-issues only)

Find where to insert new sub-issues in the parent's sub-issue list. The insertion point is **NOT** the issue the review feedback came from — it is the furthest-down-the-list issue that meets one of these criteria:

1. **First choice:** Find the sub-issue labeled `in-progress` — insert after it.
2. **Fallback:** If no issue is `in-progress`, walk the parent's sub-issue list top-to-bottom and find the **last** sub-issue that already has an open or draft PR. Insert after that one.

To verify: fetch the parent issue's sub-issue list, then for each sub-issue (starting from the bottom), check if it has the `in-progress` label or an associated PR. The first match scanning bottom-up is your insertion point.

**Example:** If the review came from issue C's PR, but issues D and E also have PRs, the insertion point is after E — not after C.

```
- #30 A  (has PR)
- #31 B  (has PR)
- #32 C  (has PR) ← review feedback came from here
- #33 D  (has PR)
- #34 E  (has PR) ← insertion point is HERE
- #99 X  ← new sub-issue goes here
- #35 F
- #36 G
```

### 4. Create Issues

Create each issue sequentially using `gh issue create` with `--title`, `--body`, and `--label`. Pass the body via HEREDOC.

- **Sub-issues:** Apply the same phase label as the PR's issue.
- **Standalone issues:** Apply relevant labels (e.g., `enhancement`, `bug`, `tech-debt`) based on the issue content. Do not apply phase labels.

Issue body format (include all sections, leave empty ones as "None"):

```markdown
**Phase: <phase name>**

## Description / Requirements
<description, referencing the original review note(s) and PR>

## Branch name
`<parent-branch>-/<issue-name>-<version>`

## Scaffold
<scaffold branch and relevant files, if applicable>

## Unit Tests (TDD)
<test cases to write>

## Integration Tests
<integration test cases, or "None">

## Dependencies
<what this issue depends on>

## Blocked by
<#N references to created GitHub issues>

## Principles
<guiding principles for implementation>

**Parent issue:** #N
**Sub-issues:** <#A, #B, #C if applicable>
**Next issue:** <#N if applicable>
```

### 5. Set Sub-Issue Relationships (sub-issues only)

Skip this step for standalone issues. For sub-issues, link each as a sub-issue of the parent at the correct position:

```bash
PARENT_ID=$(gh issue view <PARENT_NUMBER> --json id -q .id)
CHILD_ID=$(gh issue view <NEW_ISSUE_NUMBER> --json id -q .id)
AFTER_ID=$(gh issue view <AFTER_ISSUE_NUMBER> --json id -q .id)
gh api graphql -f query='mutation {
  addSubIssue(input: {issueId: "'"$PARENT_ID"'", subIssueId: "'"$CHILD_ID"'", afterId: "'"$AFTER_ID"'"}) {
    issue { id }
  }
}'
```

For multiple new issues, chain them: the first goes after the insertion point, the second goes after the first, etc.

### 6. Update Next Issue Links (sub-issues only)

After inserting sub-issues into the chain, update the **Next issue** field in the issue body of the predecessor (the issue the new sub-issue was inserted after). Use `gh issue edit` to replace its `**Next issue:**` line to point to the newly created issue. If multiple sub-issues were inserted, each one's **Next issue** should point to the one after it, and the last inserted sub-issue's **Next issue** should point to whatever the predecessor's original next issue was.

Example: inserting X between D and E:
- D's **Next issue** changes from `#E` → `#X`
- X's **Next issue** is set to `#E`

### 7. Update Triage Doc

Update the triage doc's "Issues to Defer" section with the created GitHub issue numbers and URLs.

### 8. Report

Print a summary table: GitHub issue number, title, and URL.
