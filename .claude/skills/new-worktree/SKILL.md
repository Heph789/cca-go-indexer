---
name: new-worktree
description: "Create a git worktree for a branch and open it in VS Code."
argument-hint: "branch name"
model: haiku
---

# New Worktree

Create a git worktree for the given branch and open it in VS Code.

Worktrees are stored in the parent directory of the current working directory (i.e., alongside sibling worktrees).

1. Determine the worktree path: `../<branch-name>` (use the last path segment if the branch name contains slashes).
2. Create the worktree:
   ```bash
   git worktree add ../<worktree-dir> <branch-name>
   ```
   If the branch doesn't exist yet, create it from the current HEAD:
   ```bash
   git worktree add -b <branch-name> ../<worktree-dir>
   ```
3. Open in VS Code:
   ```bash
   code ../<worktree-dir>
   ```
4. Print the worktree path.
