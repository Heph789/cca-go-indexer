---
name: sync-claude
description: Update the current working branch's .claude/ directory by merging in the process-design branch's .claude/ directory. Defaults to accepting process-design's version on conflicts.
argument-hint: "(optional) 'ours' to prefer working branch on conflicts"
---

# Sync Claude Config

Merge the `.claude/` directory from the `process-design` branch into the current working branch.

## Conflict Strategy

- **Default (theirs):** When files conflict, accept the `process-design` branch's version. This ensures process improvements propagate cleanly.
- **If `$ARGUMENTS` contains `ours`:** Prefer the working branch's version on conflicts instead.

## Process

### 1. Preflight

Confirm the current branch is not `process-design` itself. If it is, abort — there's nothing to sync.

Check for uncommitted changes in `.claude/`. If any exist, abort and ask the user to commit or stash first.

### 2. Merge

Use a targeted checkout to bring in the `.claude/` directory from `process-design`:

```bash
git checkout process-design -- .claude/
```

This stages the `process-design` version of every file in `.claude/`.

### 3. Handle Conflicts (if using ours strategy)

If the user specified `ours`, instead of a blanket checkout, do a tree-level merge:

```bash
git diff process-design -- .claude/ | git apply --3way
```

If conflicts arise, resolve them in favor of the working branch's version.

### 4. Review

Show the user what changed:

```bash
git diff --cached -- .claude/
```

Ask for confirmation before committing.

### 5. Commit

Commit the changes:

```bash
git commit -m "sync .claude/ from process-design"
```
