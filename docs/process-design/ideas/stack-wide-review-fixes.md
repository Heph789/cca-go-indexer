# Stack-Wide Review Fixes

## Problem

During review of a stacked PR chain, a reviewer notices a pattern issue in one PR (e.g., using a custom helper when a dependency already provides that function). The same anti-pattern likely exists in downstream PRs because they were written independently by the same agent. Today, the fix only applies to the branch where it was caught — downstream branches keep the bad pattern, and the agent has no mechanism to avoid repeating it in future work.

## Proposal

Three pieces that work together:

### 1. New disposition in `review-pr`: "stack-wide fix"

A fourth option alongside "fix in this PR", "sub-issue", and "standalone". Used when a review note identifies a pattern that repeats across the stack.

The triage doc gets a new section:

```markdown
## Stack-Wide Fixes
<!-- pattern issues that repeat in downstream branches -->

### Use `dep.Foo()` instead of custom `helperFoo()`
- **Pattern:** any call to `helperFoo(...)` should be replaced with `dep.Foo(...)`
- **Branches affected:** (filled in after inspection)
- **Review notes:** #comment-123
```

During triage, for each stack-wide fix candidate, the agent inspects downstream branches to confirm the pattern exists there too and lists affected branches.

### 2. `conventions.md` — a living style guide

A version-controlled file (e.g., `docs/conventions.md`) that accumulates project-specific patterns learned from review feedback. Each entry is 2-3 lines: the rule, and where it was learned.

```markdown
# Conventions

## Prefer library functions over custom helpers
Use `eth.ParseLog()` from the eth package instead of writing custom log parsing helpers.
**Learned from:** PR #48 review — custom `decodeLog()` duplicated existing functionality.

## Use `errors.Join` for multi-error aggregation
Don't accumulate errors into a `[]error` slice with a custom `combineErrors()` — use `errors.Join()` from stdlib.
**Learned from:** PR #51 review
```

**How it propagates:** The convention entry is committed on the branch where the review caught the issue. When the stack is restacked, the file propagates to all downstream branches automatically — no extra machinery needed.

**How agents use it:**
- `implement-issues` and `fix-pr-issues` read it before starting work, alongside the scaffold
- `go-tester` reads it to know what patterns to test for
- `cascade-fix` appends to it when applying a stack-wide fix

**Why a file in the repo instead of auto-memory:**
- Project-level, not per-user — any agent in any conversation can read it
- Reviewable in PRs — conventions are themselves subject to review
- Version-controlled — propagates through the stack via restack

### 3. New skill: `cascade-fix`

Takes a triage doc (or its stack-wide fix section) and applies the fix across all downstream branches.

**Process:**
1. Fix the pattern in the current branch
2. Add a convention entry to `conventions.md` describing the pattern
3. Commit both the code fix and the convention entry
4. `gt stack restack` — convention file propagates to all downstream branches
5. For each downstream branch (in stack order):
   - Check out the branch
   - Read `conventions.md` to understand the pattern
   - Search for violations of the new convention
   - If found: fix, run tests, commit
   - If clean: skip
6. `gt stack submit` — update all PRs

**Key detail:** The convention file doubles as both the instruction set for the cascade fix and the long-term memory for future implementation. No duplication between "how to fix now" and "how to avoid later."

## How the pieces connect

```
Reviewer catches pattern in PR #3
        │
        ▼
/review-pr triages as "stack-wide fix"
        │
        ▼
/fix-pr-issues fixes it in branch #3
  + adds entry to conventions.md
  + commits both
        │
        ▼
/cascade-fix reads the convention,
  walks branches #4–#10,
  fixes each, commits, restacks, submits
        │
        ▼
Future /implement-issues reads conventions.md
  before writing any code → doesn't repeat the mistake
```
