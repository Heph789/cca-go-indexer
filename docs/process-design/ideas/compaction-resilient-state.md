# Compaction-Resilient State

## Problem

When `implement-issues` runs a long session, Claude Code's automatic compaction can fire mid-workflow. The compaction hint ("keep only instructions and relevant context for the current issue") helps, but it's not reliable — the agent can lose its place in the TDD cycle (red/green/simplify), which sub-issue it's on, and what decisions it made. Recovery requires the user to re-orient the agent manually.

GitHub issues and Git branches provide durable external state, but there's a gap between "issue has `in-progress` label" and "agent knows it already wrote the tests and is in the green phase."

## Inspiration

The [claude-coordinator](https://github.com/dennisonbertram/claude-coordinator) project externalizes all operational state to disk (`.coord/` directory) so compaction is a non-event. Key ideas:

- **Context packet**: a compressed summary written proactively, read on startup
- **Task ledger**: machine-readable status for all tasks
- **Smallest sufficient context**: don't read anything unless specifically needed
- **Structured subagent output**: workers return rigid formats, not freeform prose

We don't need most of that — GitHub issues are already our task ledger, and our skill/agent separation handles roles. But the checkpoint concept fills a real gap.

## Proposal

### 1. State file in `implement-issues`

After each phase transition, `implement-issues` writes a small state file:

```
local_ignored/implement-state.json
```

```json
{
  "parent_issue": "#42",
  "current_issue": "#45",
  "current_issue_url": "https://github.com/owner/repo/issues/45",
  "phase": "green",
  "branch": "feat-indexer-/block-fetcher-v1",
  "tests_written": ["pkg/fetcher/fetcher_test.go"],
  "last_updated": "2026-03-28T14:30:00Z"
}
```

On startup, `implement-issues` reads this file first. If it exists and matches the parent issue being worked on, resume from the recorded phase instead of starting from scratch.

### 2. Structured subagent output

Define a return format for `go-tester` and the implementation subagent so `implement-issues` can checkpoint reliably without keeping full subagent output in context:

- Files created/modified
- Test count and names
- Pass/fail status

This also reduces context consumption from subagent results, delaying compaction.

### 3. Smallest-sufficient-context rule

Make explicit in `implement-issues`: only read the current issue + its direct blocked-by issues. Don't read the full issue plan, scaffold, or sibling issues unless specifically needed. This delays compaction by keeping the context window lean.

## Open Questions

- Should the state file live in `local_ignored/` (not version-controlled) or `.coord/` (tracked)? Leaning toward `local_ignored/` since it's ephemeral session state.
- Should other long-running skills (e.g., `push-review-issues`) also checkpoint, or is this only worth the complexity for `implement-issues`?
- Can we detect compaction after the fact (e.g., by checking if a known variable is still in context) to trigger a re-read of the state file?
