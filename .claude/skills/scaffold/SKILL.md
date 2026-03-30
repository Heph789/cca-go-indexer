---
name: scaffold
description: Build a structural code scaffold from a user's description without running, testing, or building. Produces pseudo-code-like files with types, interfaces, function signatures, and explanatory comments.
argument-hint: "description of what to scaffold"
disable-model-invocation: true
---

# Scaffold Skill

Generate a **structural code scaffold** from the user's description: `$ARGUMENTS`

## Rules

1. **No execution.** Do not run, test, build, or compile anything. Do not use the Bash tool.
2. **Structure over implementation.** Write types, interfaces, structs, function/method signatures, and constants. Use `// TODO:` or placeholder comments for bodies that need real logic.
3. **Comment thoroughly.** Every file, type, field, function, and non-trivial block should have a comment explaining *what* it does and *why* it exists. The scaffold is a communication artifact — a reader should understand the full design just by reading the comments, without needing external docs or context.
4. **Match the project.** Detect the repo's language, module system, and conventions (package names, directory layout, naming style) and follow them.
5. **Minimal files.** Only create files that are necessary. Prefer fewer, well-organized files over many small ones.
6. **No tests.** Do not generate test files unless the user explicitly asks.
7. **No dependencies.** Do not add imports for packages that aren't already in the project unless the user's description clearly requires them. Mark external deps with `// DEPENDENCY: <pkg>` comments.

## Process

1. **Understand** — Read the user's description. If it references existing code, read those files first.
2. **Plan** — Decide which files and top-level constructs are needed. Share this plan briefly with the user before writing.
3. **Design doc** — Write `docs/tmp/<feature-name>/DESIGN.md` covering:
   - Overview and motivation
   - Key design decisions and trade-offs
   - Data flow / component interaction
   - Open questions (if any)
4. **Schema DDL** (if applicable) — If the feature involves database tables or schema changes, write `docs/tmp/<feature-name>/schema.sql` with the DDL. Include comments on each table and column explaining its purpose.
5. **Scaffold** — Write each code file with:
   - Package/module declarations
   - Imports (existing deps only, or marked new ones)
   - Type/struct/interface definitions with field comments
   - Function/method signatures with `// TODO:` bodies or simple pseudo-code
   - Constants and configuration values
6. **Summarize** — List the files created and what each one is for.
