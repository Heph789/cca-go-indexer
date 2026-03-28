# QA Gate Red Phase

## Problem

Each phase of a feature ends with a QA gate issue that runs end-to-end tests verifying that phase's work. But a passing test suite at gate N doesn't prove the tests are meaningful — they could be vacuously passing (e.g., testing a no-op, asserting on defaults, or not actually exercising the new code paths). There's no mechanism to confirm the tests would have *failed* without the phase's implementation.

## Proposal: Run forward gates backward

When writing the QA gate for phase N, also run its e2e test framework at the QA gate for phase N-1 — where the phase N features don't exist yet. Those tests **must fail**. This is TDD's red-green applied at the integration level.

### Concrete example (from issue #29)

```
Phase 1 — Happy Path     → QA gate #37
Phase 2 — Resilience     → QA gate #41
Phase 3 — Production     → QA gate #45
```

When building QA gate #41 (resilience):
1. Write the resilience e2e tests (retry recovery, reorg handling, etc.)
2. Run them at the phase 1 QA gate branch (#37's branch) → they **must fail** because resilience features haven't been implemented yet
3. Run them at the phase 2 QA gate branch (#41's branch) → they **must pass**

If the resilience tests pass at the phase 1 gate, something is wrong — either the tests aren't actually testing resilience behavior, or the assertions are too weak.

### How it works in the issue chain

Each QA gate issue gets a new section:

```markdown
## Red Phase Verification
Run the following against the **previous QA gate's branch** and confirm they FAIL:
- <list of test files or test functions from this gate>

Expected failures:
- `TestRetryRecovery` — no retry transport exists at phase 1
- `TestReorgRollback` — no reorg detection exists at phase 1
```

The implementing agent:
1. Checks out the previous QA gate's branch
2. Copies or references the current gate's test files
3. Runs them — confirms they fail (and fails for the *right reasons*, not just compilation errors)
4. Checks out the current gate's branch
5. Runs them — confirms they pass

### Edge cases

- **Gate 1 (happy path)** has no previous gate. Its red phase is against the scaffold branch or parent branch — before any implementation exists.
- **Compilation failures** don't count as a valid red phase. The tests should compile at the previous gate (they may use interfaces/types that already exist) but fail at runtime because the behavior isn't implemented. If they can't compile, the test needs to be restructured to use the interface rather than concrete types that don't exist yet.
- **Shared test infrastructure** (helpers, fixtures, docker-compose setup) should live in a test utilities package that's available at all gates. Only the test *assertions* should be gate-specific.

### What this catches

- Tests that assert on zero values or defaults and pass vacuously
- Tests that mock too aggressively and never touch real behavior
- Tests whose setup accidentally provides the behavior being tested
- Copy-paste tests that look right but don't exercise the code path they claim to

## Separate QA issues from implementation issues

### Problem

QA gate issues currently use the same format as implementation issues (Description, Branch name, Unit Tests, Integration Tests, etc.). But QA work is fundamentally different — it's not implementing a feature, it's *designing a verification system* for a phase's goals. The rigid issue format constrains the agent to a predefined test list when it should be thinking creatively about what could go wrong.

### Proposal: QA gate issue format

QA gates get their own format that emphasizes phase goals and gives the agent freedom to design experiments:

```markdown
## N. [QA] <phase name> verification

### Phase Goals
What this phase set out to accomplish. Stated as outcomes, not code.
(e.g., "The indexer processes batches of blocks, decodes AuctionCreated events, persists them to Postgres, and serves them via a GET endpoint.")

### Constraints
What the system should NOT do at this phase.
(e.g., "No retry logic exists. No reorg handling. A single RPC failure should surface as an error, not be retried.")

### Required Verifications
Specific end-to-end scenarios that MUST be tested. These come from the issue plan and are non-negotiable.
- Indexer processes a range of blocks and persists decoded events
- API returns persisted auctions with correct field mapping

### Agent-Designed Experiments
The implementing agent should design additional experiments beyond the required verifications. The goal is to probe for gaps — things the phase claims to handle but might not. The agent has free reign here. Examples of what it might try:
- Feed the indexer a block with zero matching events — does it still advance the cursor?
- Query the API for a non-existent auction — does it 404 or panic?
- Run the indexer twice on the same range — are writes idempotent?

### Red Phase Verification
Run this gate's tests against the **previous QA gate's branch** and confirm they FAIL.
Expected failures and reasons listed here.

### Previous Gate
Link to the previous phase's QA gate branch (for red phase verification).
```

### Key differences from implementation issues

| | Implementation Issue | QA Gate Issue |
|---|---|---|
| **Goal** | Build a specific piece of functionality | Verify a phase's goals were met |
| **Tests** | Predefined test cases (TDD) | Required verifications + agent-designed experiments |
| **Agent freedom** | Follow the spec closely | Encouraged to invent scenarios |
| **Format sections** | Scaffold, Unit Tests, Dependencies | Phase Goals, Constraints, Experiments |
| **Red phase** | Tests fail → implement → tests pass | Tests fail at previous gate → pass at current gate |

### How the `/issues` skill changes

The skill currently generates QA gates using the same template as implementation issues. With this change:
- Implementation issues keep their current format
- QA gates use the new format above
- The `/issues` skill describes the phase goals and constraints; the agent fills in experiments at implementation time
- QA gates are still marked `[QA]` (replacing `[DRAFT]`) for labeling in the issue tracker

## Separate `implement-qa-gate` skill

### Problem

The `/implement-issues` skill enforces a strict red-green-simplify TDD cycle: write failing unit tests → implement product code to make them pass → simplify. QA gates don't fit this flow. There's no product code being written — the output is a verification system. Forcing QA work through the implementation skill leads to an awkward mismatch where the agent tries to shoehorn experiment design into a unit-test-first workflow.

### Proposal: `implement-qa-gate` skill

A separate skill with its own flow, invoked when `/implement-issues` encounters a `[QA]`-tagged issue (or invoked directly).

**Flow:**

```
1. Design    — read phase goals and constraints, design test infrastructure and experiments
2. Build     — implement the test harness, fixtures, and experiments
3. Red       — run against previous gate's branch → confirm failures (for the right reasons)
4. Green     — run against current gate's branch → confirm passes
5. Commit/PR — same as implement-issues
```

### How each step differs from implementation

| Step | `implement-issues` | `implement-qa-gate` |
|---|---|---|
| **1. Design** | N/A — tests are predefined in the issue | Agent reads phase goals, required verifications, and constraints. Designs both the test infrastructure and its own experiments. |
| **2. Build** | N/A — goes straight to writing tests | Build the harness: helpers, fixtures, docker-compose, seed data. Then write the experiments. No product code. |
| **3. Red** | Write unit tests that fail against missing implementation | Check out previous gate's branch. Run experiments. Confirm they fail at runtime (not compilation). Verify failures are for the right reasons. |
| **4. Green** | Implement product code until tests pass | Check out current gate's branch. Run experiments. Confirm they all pass. No product code changes — if they fail, the *phase's implementation* has a bug, not the QA gate. |
| **5. Commit/PR** | Same | Same |

### Key details

- **Step 1 (Design) produces a plan.** Before writing any code, the agent outputs a test plan: what infrastructure it needs, what experiments it will run (required + invented), and what it expects to fail at the previous gate and why. This plan is shown to the user for confirmation before proceeding.
- **Step 3 (Red) validates the experiments, not the product.** If an experiment passes at the previous gate, the experiment is flawed — it needs to be rewritten, not the product code.
- **Step 4 (Green) validates the product, not the experiments.** If an experiment fails at the current gate, the phase's implementation has a gap — the agent should report this rather than modifying the experiment to pass.
- **Agent-designed experiments** happen in step 1. The agent has free reign to invent scenarios beyond the required verifications. The issue format gives it phase goals and constraints; the agent figures out how to probe the boundaries.

### How `/implement-issues` delegates

When `/implement-issues` encounters an issue tagged `[QA]`, it hands off to `/implement-qa-gate` instead of running its own red-green-simplify cycle. The QA skill handles that issue, then control returns to `/implement-issues` for the next issue in the chain.
