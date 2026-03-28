---
name: issues-to-github
description: Parse an issues markdown file and create GitHub issues from it, preserving parent/child relationships and blocked-by links.
argument-hint: "path to issues.md"
disable-model-invocation: true
---

# Issues to GitHub

Read the issues markdown file at `$ARGUMENTS` and create GitHub issues from it using the `gh` CLI.

## Process

1. **Parse.** Read the markdown file. Extract each issue (H2 headers), its phase (H1 headers), and all sections. Build a map of issue title → sequential index for relationship resolution.
2. **Confirm.** Show the user a numbered list of issues to be created (title, phase, blocked-by) and ask for confirmation before creating anything.
3. **Create parent first.** Create the parent issue. Record its GitHub issue number.
4. **Create in order.** Create each issue sequentially. After each creation, record the GitHub issue number so subsequent issues can reference it in `Blocked by` and relationship fields.
5. **Set sub-issue relationships.** After each child issue is created, use the GitHub GraphQL API to add it as a sub-issue of the parent.
6. **Report.** Print a summary table: GitHub issue number, title, phase, and URL.

## GitHub Issue Body Format

Map the markdown sections to the GitHub issue body:

```markdown
**Phase: <phase name>**

## Description / Requirements
<content>

## Branch name
<content>

## Scaffold
<content>

## Unit Tests (TDD)
<content>

## Integration Tests
<content>

## Dependencies
<content>

## Blocked by
<#N references to created GitHub issues>

## Principles
<content>

**Parent issue:** #N
**Sub-issues:** #A, #B, #C (if applicable)
**Next issue:** #N (if applicable)
```

## Sub-Issue GraphQL

After creating a child issue, set the sub-issue relationship:

```bash
gh api graphql -f query='mutation {
  addSubIssue(input: {issueId: "<PARENT_NODE_ID>", subIssueId: "<CHILD_NODE_ID>"}) {
    issue { id }
  }
}'
```

Get node IDs from `gh issue view <number> --json id -q .id`.

## Rules

1. **Use `gh issue create`.** Use `--title`, `--body`, and `--label` flags. Pass the body via HEREDOC for correct formatting.
2. **Sub-issue relationships.** Use the GitHub GraphQL API (see above) to set parent/child relationships. Do not rely on markdown references alone.
3. **Labels.** Apply a label for the phase (`phase:happy-path`, `phase:resilience`, `phase:production`). Create labels with `gh label create` if they don't exist. Apply `question` label to QA gate issues marked `[DRAFT]`.
4. **Blocked by.** Replace issue titles in `Blocked by` sections with `#N` references to the actual created GitHub issues.
5. **Dry run first.** Always show the plan and wait for user confirmation before creating any issues.
6. **Report.** After creation, print a summary table: GitHub issue number, title, phase, and URL.
