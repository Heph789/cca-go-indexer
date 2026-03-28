---
name: implement-qa-gate
description: Implement a QA gate issue by designing verification experiments, building a test harness, and validating with a red-green phase against previous and current gate branches.
argument-hint: "GitHub QA gate issue URL or number"
---

# Implement QA Gate

Implement the QA gate issue: `$ARGUMENTS`

This skill handles `[QA]`-tagged issues. It is invoked by `/implement-issues` when it encounters a QA gate, or directly by the user.

QA gates are fundamentally different from implementation issues. You are not building product code — you are designing and building a **verification system** that proves a phase's goals were met.

## Process

### 1. Understand the Phase

Read the QA gate issue carefully. Extract:
- **Phase Goals** — what outcomes this phase was supposed to deliver
- **Constraints** — what the system should NOT do at this phase
- **Required Verifications** — non-negotiable scenarios that must be tested
- **Previous Gate** — the branch to use for red phase verification

Also read the implementation issues for this phase to understand what was actually built.

### 2. Design

Before writing any code, produce a **test plan** and present it to the user for confirmation. The plan should include:

#### Required Verifications
Directly from the issue. These are non-negotiable.

#### Agent-Designed Experiments
Design additional experiments beyond the required verifications. Your goal is to probe for gaps — things the phase claims to handle but might not. Think adversarially:
- What happens at boundaries? (empty inputs, max values, zero values)
- What happens with unexpected but valid inputs?
- What happens if operations are repeated? (idempotency)
- What state is left behind after errors?
- What assumptions does the code make that could be wrong?

You have free reign here. Be creative. The point is to catch things the implementation issues didn't anticipate.

#### Test Infrastructure
What you need to build: helpers, fixtures, docker-compose setup, seed data, mock services, etc. Identify what already exists (from previous gates or test utilities) and what needs to be created.

#### Expected Red Phase Failures
For each experiment, describe what you expect to happen when run against the previous gate's branch, and why. This forces you to think about what each experiment actually validates.

**Wait for user confirmation before proceeding.**

### 3. Build

Build the test harness and write all experiments (required + agent-designed).

- Use shared test infrastructure where it exists; create new helpers as needed
- Comment each experiment thoroughly — a reviewer should understand what's being verified and why without reading the implementation
- Commit incrementally: infrastructure first, then experiments in logical groups

### 4. Red Phase — Verify Against Previous Gate

Check out the previous QA gate's branch (or scaffold/parent branch for gate 1):

```bash
git stash
git checkout <previous-gate-branch>
git stash pop  # bring test files forward
```

Run the experiments. **They must fail.**

Validate that failures are for the right reasons:
- **Runtime failures are valid** — the behavior doesn't exist yet, so the test correctly fails
- **Compilation failures are NOT valid** — restructure the test to use interfaces or types that exist at the previous gate. The test should compile but fail at runtime.
- **Wrong-reason failures are NOT valid** — if a test fails because of a setup issue rather than missing behavior, fix the test

Document each failure: which test, what error, why it's the expected failure.

If any experiment **passes** at the previous gate, that experiment is flawed — it's not actually testing what this phase added. Rewrite it or remove it.

Return to the current gate's branch when done:

```bash
git checkout <current-gate-branch>
```

### 5. Green Phase — Verify Current Gate

Run all experiments on the current gate's branch. **They must pass.**

If an experiment fails here:
- This is a **product bug**, not a test bug. The phase's implementation has a gap.
- Do NOT modify the experiment to make it pass.
- Report the failure to the user with details on what's missing.

### 6. Commit and PR

Follow the same commit and PR process as `/implement-issues`:
- Commit the final test suite
- Create a draft PR
- Link to the QA gate issue

## When Invoked by `/implement-issues`

`/implement-issues` delegates to this skill when it encounters a `[QA]`-tagged issue. After this skill completes, control returns to `/implement-issues` for the next issue in the chain.

## Compaction

When compacting, keep these instructions, the test plan from step 2, and the current red/green phase status.
