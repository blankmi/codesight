- **Purpose**: Workspace safety rules and navigation map for AI coding agents.
- **Last verified**: 2026-03-17
- **Source**: mixed

## Core Documentation
- Architecture: `/.context/architecture/system_overview.md`, `/.context/architecture/module_map.md`
- Engineering: `/.context/engineering/build_test_run.md`, `/.context/engineering/ci_cd.md`
- Codebase: `/.context/codebase/entrypoints.md`, `/.context/codebase/dependency_report.md`
- Risk and QA: `/.context/risk/risk_register.md`, `/.context/qa/questions.md`, `/.context/project_facts.yaml`, `/.context/handoff/quality_checklist.md`

## Project Identity
- [reported] Codesight is a unified code intelligence CLI for large codebases.
- [reported] Primary language is Go (`go 1.25.7` minimum toolchain).
- [observed] Runtime surface is Cobra command wiring in `cmd/cs`.
- [observed] Core engines live in `pkg/*` (`index/search/extract/lsp/vectorstore/embedding`).
- [observed] External runtime dependencies include Milvus, Ollama, and language servers.

## Guardrails
- Protected paths unless task explicitly requires edits:
  - `.github/workflows/release.yml` (release pipeline; no-go for routine tasks)
  - `specs/**` (context-kernel orchestration specs)
  - `docs/**` (coordination/planning docs)
  - `.prompts/**` (agent workflow contracts)
  - `bin/**` (generated binaries; regenerate instead of editing)
- Conditionally protected paths:
  - `agent-skills/**` is tool-owned; edit only for explicitly scoped skill/tooling tasks.
- Before any change:
  1. Read `/.context/architecture/module_map.md` and `/.context/risk/risk_register.md`.
  2. Confirm entrypoint and side effects in `/.context/codebase/entrypoints.md`.
  3. Follow validation workflow in `/.context/engineering/build_test_run.md`.
- Code style rules:
  - Keep CLI parsing/wiring in `cmd/cs`; place core behavior in `pkg/*`.
  - Split new command logic into dedicated `cmd/cs/*.go` files; avoid growing `cmd/cs/main.go`.
  - Preserve deterministic output/error strings that tests lock down.
  - Use explicit error wrapping: `fmt.Errorf("...: %w", err)`.
  - Keep runtime config through existing `CODESIGHT_*` env vars.
- Testing rules:
  - Update/add tests in the same package as behavior changes.
  - If touching `cmd/cs`, update command tests in `cmd/cs/*_test.go`.
  - If touching `pkg/vectorstore`, add tests first before behavior changes.
  - Run all available checks before handoff: `go test ./...`, `make build`, `make lint`. Note: `make test-integration` requires Docker + Milvus and is NOT available in agent containers.
- Data/index rules:
  - Treat `cs clear` and collection drops as destructive.
  - Do not add schema/migration-like behavior without explicit task scope.
  - Avoid changing index metadata semantics unless compatibility work is explicitly requested.
- Branch/release rules:
  - Agents must not merge branches; CK orchestrator manages merges.
  - Release pipeline changes are out of scope unless explicitly requested.

## Key Patterns
- Command handler pattern: parse/validate in `cmd/cs`, delegate behavior to `pkg/*`, print engine output.
- Ignore rules pattern: use `pkg/ignore` matcher APIs; do not duplicate ignore logic.
- LSP behavior pattern: `refs` may fallback to grep; `callers` and `implements` must fail fast without LSP.
- Path handling pattern: resolve absolute and symlink-safe paths before traversal.

## Framework-Specific Checklists
- Adding/changing a CLI command:
  1. Wire flags/args in `cmd/cs` (prefer command-specific file over `main.go`).
  2. Put core behavior in `pkg/...`, not inline in command handlers.
  3. Add command tests for happy path + validation errors.
  4. Run required validation checks from `/.context/engineering/build_test_run.md`.
- Changing extraction behavior (`pkg/extract`):
  1. Preserve `raw|json` output contracts and deterministic ordering.
  2. Keep ignore handling through matcher APIs.
  3. Update extraction tests and language routing coverage.
- Changing LSP behavior (`pkg/lsp`):
  1. Preserve missing-binary guidance and ambiguity error contracts.
  2. Preserve deterministic ordering and cycle/duplicate protections.
  3. Update engine tests and CLI pass-through tests.
