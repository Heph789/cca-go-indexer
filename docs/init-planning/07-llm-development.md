# LLM Development Considerations

This project doubles as a testing ground for LLM-assisted development. The architecture and conventions should be optimized for both human and LLM contributors.

## Code Organization for LLM Productivity

### Small, focused files

LLMs reason better about files under ~300 lines. Split large files by responsibility rather than letting them grow unbounded. Each file should have a clear, single purpose.

### Clear package boundaries

The `internal/` package structure maps directly to architectural layers:
- `domain/` — pure types, no imports from other internal packages
- `store/` — only imports domain
- `indexer/` — imports domain, store, eth
- `api/` — imports domain, store
- `eth/` — imports domain

This DAG of dependencies makes it easy for an LLM to understand scope and impact of changes.

### Consistent patterns

Use the same patterns everywhere:
- Same error handling style
- Same constructor pattern (`New...()`)
- Same interface definition location (next to the consumer, not the implementer)
- Same test file structure

Consistency lets the LLM pattern-match from examples rather than re-learning per file.

## CLAUDE.md as the LLM Entry Point

Maintain a thorough `CLAUDE.md` at the project root covering:
- Build, test, and lint commands
- Project structure overview
- Key conventions (error handling, naming, patterns)
- What to do and what NOT to do
- Database migration workflow
- How to add a new event type (end-to-end checklist)

This is the single most impactful thing for LLM productivity.

## Test Coverage as a Feedback Loop

Good tests let LLMs verify their own changes. Prioritize:
- Integration tests that hit a real database
- Tests that cover the full pipeline (event → process → store → query)
- Clear test names that describe the scenario, not the implementation
- Table-driven tests where multiple cases share the same logic

An LLM that can run `go test ./...` and get a pass/fail signal is dramatically more effective than one working blind.

## Makefile / Taskfile

Provide simple commands for common workflows:

```makefile
make build          # compile
make test           # run all tests
make test-unit      # run unit tests only
make test-int       # run integration tests (requires docker)
make lint           # run linters
make migrate-up     # apply migrations
make migrate-new    # create a new migration
make generate       # regenerate ABI bindings
make docker-up      # start local dev environment
make docker-down    # stop local dev environment
```

These give the LLM concrete, reliable commands to use.

## Code Generation

Use code generation for boilerplate:
- ABI bindings from Solidity contracts (abigen or custom)
- SQL query types (sqlc) — generates type-safe Go from SQL queries
- Mocks (mockgen or moq) if needed

Generated code should be committed and clearly marked (e.g., `// Code generated ... DO NOT EDIT.`).

## Iterative Development Strategy

For LLM-driven development sessions:

1. **Start with interfaces** — define the contract before the implementation
2. **Write tests first** — gives the LLM a target to hit
3. **One package at a time** — complete and test each layer before moving to the next
4. **Commit frequently** — small, atomic commits give clear rollback points
5. **Review generated code** — LLMs can introduce subtle bugs; tests catch most but not all
