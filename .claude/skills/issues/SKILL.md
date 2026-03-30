---
name: issues
description: Turn a scaffold and its associated tmp docs into a markdown file of implementation issues following the project's issue format.
argument-hint: "feature area or path to scaffold files"
disable-model-invocation: true
---

# Issues Skill

Generate a **markdown issue plan** from a scaffold and its associated docs for the changes on this branch compared to its base branch. If this branch is named <BASE>-/scaffold-<VERSION>, then the base branch is named <BASE>

These issues are to be implemented one at a time, each one scoped to a single, testable, reviewable, PR. The branch name format should be <BASE>-/<SHORT_ISSUE_NAME>-<IMPLEMENTATION_VERSION>

The iteration version is to organize different implementation attempts of the same issue

## Output

Write a single markdown file to `docs/tmp/<feature>/issues.md`.

## Issue Format

Each issue must contain these sections in order:

```markdown
## <issue number>. <short issue title>

### Description / Requirements
What needs to be built and why. Reference the scaffold files and types involved.

### Branch name
List the branch name to use when working on this issue

### Scaffold
List the branch name of the scaffold, and files in the scaffold to reference for this issue

### Unit Tests (TDD)
Bullet list of test cases to write *before* implementation. Each test should be one sentence describing the assertion.

### Integration Tests
Tests to insure this issue's work is integrated with prior work.

### Dependencies
Could be external packages, or internal work

### Blocked by

### Principles
Relevant design principles or constraints from the project docs (e.g., "atomic batches", "idempotent writes"). Only include principles that apply to this specific issue.
```

## Phasing

Issues are grouped into sequential phases. Each phase builds on the last. Use H1 headers to separate phases, with a short paragraph explaining the phase's goal.

```markdown
# Phase 1 — Happy Path

<1-2 sentence description of what this phase achieves>

---

## 1. First issue
...

## 2. Second issue
...

# Phase 2 — Resilience

<1-2 sentence description>

---

## 5. Fifth issue
...
```

Issue numbers are sequential across all phases (not reset per phase). Each phase ends with an Automated QA gate issue.

### Phase 1 — Happy Path
The minimum set of issues to get the feature working end-to-end. Assume valid inputs, successful calls, and a clean environment. No error handling, no retries, no edge cases. The goal is a working vertical slice as fast as possible.

**Ordering within Phase 1:**
1. **Consumers first, mocked.** Start with the components that consume interfaces (e.g., the indexer loop, API handlers). Test them against mock implementations of their dependencies. This validates the design before writing any concrete implementation.
2. **Concrete implementations.** Then implement the real dependencies (database, RPC client, repositories). These are the things the consumers call.
3. **Wiring capstone.** End with an issue that connects everything — config loading, entry points, end-to-end startup. This proves the system works as a whole.

### Phase 2 — Resilience
Make the system survive real-world conditions: retries, error recovery, reorg handling, graceful degradation. Cover what happens when RPC calls fail, DB connections drop, or the chain reorganizes. Reference specific decisions from `docs/tmp/` considerations docs.

### Phase 3 — Production Readiness
Polish for deployment: middleware (CORS, request ID, logging, recovery), health/readiness probes, caching headers, and any remaining considerations from `docs/tmp/` that were deferred. These are independently testable and don't change core behavior.

Within each phase, issues are ordered by dependency (Blocked by). Across phases, all Phase 1 issues come before Phase 2, etc.

### Automated QA Gates

The last issue of each phase is an **Automated QA** issue. Mark these with `[DRAFT]` in the title so they can be labeled accordingly in the issue tracker. The description explains what the gate should verify. Include integration tests that exercise the phase's work end-to-end.

```markdown
## N. [DRAFT] Automated QA: <phase name> verification

### Phase Goals
What outcomes this phase was supposed to deliver, stated as observable behaviors (not code changes). E.g., "Indexer discovers new contracts via factory events and begins polling them for domain events."

### Constraints
What the system should NOT do at this phase. E.g., "No retry logic, no reorg handling, no error recovery — those are Phase 2."

### Required Verifications
Specific end-to-end scenarios that MUST be tested. Each should be one sentence describing an observable assertion. E.g.:
- Indexer picks up a BidSubmitted event and persists a Bid record with correct fields
- API returns bids filtered by auction address

### Previous Gate
The branch to check out for red-phase verification. For the first gate this is the parent/scaffold branch; for subsequent gates it is the previous gate's branch. E.g., `bid-auction-1-/qa-watched-contracts-1`

### Branch name
...

### Blocked by
<last implementation issue of this phase>
```

## Rules

1. **Read first.** Read the scaffold files, architecture docs, considerations docs, and any other `docs/tmp/` files for the feature area before generating issues.
2. **One issue per unit of work.** Split along natural boundaries: one handler, one middleware layer, one endpoint. Issues should result in single PRs of testable, reviewable work. Exception: multiple small, same-shaped implementations that share a test harness (e.g., four repository types that are each a single SQL query) can be grouped into one issue.
3. **Consumer first order.** Sequence issues so that consumers come first, tested against mocked interfaces. Concrete implementations come after. The final happy-path issue wires everything together.
4. **Happy path first.** Phase 1 issues should have zero error handling, zero retries, zero edge cases. Just the straightforward working path. Defer all hardening to later phases.
5. **Use a parent issue.** Create one top-level parent issue that summarizes the full feature. All other issues should reference it as their parent.
6. **No implementation.** Do not write code, run commands, or modify scaffold files.
7. **Ground in the scaffold.** Every issue must reference specific files, types, or functions from the scaffold. Do not invent work that isn't represented in the scaffold.
8. **Keep it concise.** Descriptions should be 2-4 sentences. Test cases should be one line each. Avoid boilerplate.
