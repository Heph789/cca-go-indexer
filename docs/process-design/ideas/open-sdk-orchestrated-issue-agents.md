# Open: SDK-Orchestrated Issue Agents

## Problem

The current `implement-issues` skill runs each sub-issue as a subagent, but subagents can't spawn their own subagents. This means per-issue work is flattened — no explore agents, test runners, or parallel work within a single issue. All issues also share the parent's context window, accumulating noise.

## Proposal: Agent SDK orchestrator

Replace the subagent-per-issue pattern with a Python orchestrator using the Claude Agent SDK. Each issue gets a full top-level `query()` call with its own context window and full subagent capabilities.

### Architecture

```
orchestrator.py (Agent SDK)
  ├── reads parent issue, identifies sub-issues
  ├── for each sub-issue:
  │     query(
  │       prompt="implement issue #123 using TDD...",
  │       options=ClaudeAgentOptions(
  │         allowed_tools=["Read","Edit","Write","Bash","Agent",...],
  │         permission_mode="acceptEdits"
  │       )
  │     )
  │     → full Claude Code session with subagent support
  └── collects results, reports status
```

### What this solves

- **Fresh context per issue**: No accumulated noise from prior issues
- **Full subagent depth**: Each issue's agent can spawn explore agents, test runners, etc.
- **Programmatic permission control**: `allowed_tools` and `permission_mode` replace interactive prompts
- **Parallelism**: Multiple issues could run concurrently via `asyncio`

### Alternatives considered

- **`claude -p` per issue**: Simpler but permissions are awkward — tool calls get denied with no way to prompt. Would need `--dangerously-skip-permissions` or comprehensive `--allowedTools` lists.
- **`claude -p` with `--resume`**: Could chain multi-turn interactions per issue, but still has the permissions problem.
- **Hermes Agent as orchestrator**: Full agent platform (Nous Research) with terminal tools, skills, memory, and messaging integrations. Could shell out to `claude` CLI. Overkill — adds a second agent runtime, second memory system, second skill format. The overlap with Claude Code's existing capabilities isn't worth the migration cost.

### Open questions

- How does the Agent SDK handle `.claude/` settings and skills? Does it load them from the working directory like the CLI?
- What's the streaming event schema? Need to understand how to detect success/failure per issue.
- Can we pass session context (branch name, prior decisions) without bloating the prompt?
- How to handle partial failures — skip and continue, or stop the sequence?
- Could this orchestrator itself be invoked as a Claude Code skill, or does it need to live outside the Claude Code loop?
