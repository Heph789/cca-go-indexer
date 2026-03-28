---
name: git
description: "Guidelines for all git and gh operations in this project."
disable-model-invocation: true
---

# Git

## Rules

1. **Squash merge into main.** When merging a PR into `main`, always use squash merge (`gh pr merge --squash`). Other base branches should ALWAYS use regular merge.
2. **`Closes #N` doesn't work on stacked PRs.** GitHub only auto-closes issues from closing keywords when merging into the default branch. Since stacked PRs merge into other feature branches, use `Addresses #N` in the PR body for cross-referencing, and close issues explicitly with `gh issue close` after merge.
3. **Use the GraphQL API to get sub-issues.** Do NOT parse the issue body to find sub-issues — the body text may be stale or formatted inconsistently. Always use the API:

```bash
gh api graphql -f query='query {
  node(id: "<PARENT_ISSUE_NODE_ID>") {
    ... on Issue {
      subIssues(first: 50) {
        nodes {
          number
          title
          state
          labels(first: 10) { nodes { name } }
        }
      }
    }
  }
}'
```

Get the parent's node ID with `gh issue view <NUMBER> --json id -q .id`. The `subIssues` field returns sub-issues in their display order, which is the implementation order for the stack.
