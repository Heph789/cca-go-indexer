---
name: git
description: "Guidelines for all git and gh operations in this project."
disable-model-invocation: true
---

# Git

## Rules

1. **Squash merge into main.** When merging a PR into `main`, always use squash merge (`gh pr merge --squash`). Other base branches should ALWAYS use regular merge.
2. **`Closes #N` doesn't work on stacked PRs.** GitHub only auto-closes issues from closing keywords when merging into the default branch. Since stacked PRs merge into other feature branches, use `Addresses #N` in the PR body for cross-referencing, and close issues explicitly with `gh issue close` after merge.
3. **Use Graphite for stack operations.** Branch creation (`gt branch create`), PR submission (`gt stack submit`), and rebasing (`gt stack restack`) should use the Graphite CLI, not raw git commands.
