- **Purpose**: Prioritized QA questions to close safety-critical ambiguities before autonomous code changes.
- **Last verified**: 2026-03-17
- **Source**: mixed

## Runtime

1. **[P0] Question**: Are worker agents allowed to run network-dependent commands (`cs index/search/status/clear`, integration tests) in normal task execution, or should they default to offline-safe work only?
- **Why it matters for agent safety**: Running these commands without approved runtime assumptions can create false failures or unsafe execution behavior.
- **Artifacts to update**: `/.context/engineering/build_test_run.md`, `/AGENTS.md`, `/.context/risk/risk_register.md`
- **Answer [reported]**: Agents normally run in docker containers in yolo mode, so they can run network-dependent commands.

2. **[P1] Question**: Is `go 1.25.7` in `go.mod` the minimum required toolchain for contributors/agents?
- **Why it matters for agent safety**: Wrong toolchain can break builds or produce incompatible changes.
- **Artifacts to update**: `/.context/project_facts.yaml`, `/.context/engineering/build_test_run.md`, `/AGENTS.md`
- **Answer [reported]**: `go 1.25.7` is the minimum required toolchain.

## Architecture

3. **[P0] Question**: Is `cmd/cs/main.go` intentionally the single command-wiring file, or should new command logic be split into separate files?
- **Why it matters for agent safety**: Incorrect placement increases conflict risk in the largest hotspot file.
- **Artifacts to update**: `/.context/architecture/module_map.md`, `/AGENTS.md`, `/.context/risk/risk_register.md`
- **Answer [reported]**: New command logic should be split into separate files moving forward.

4. **[P1] Question**: Are packages under `pkg/` considered stable public API for external consumers?
- **Why it matters for agent safety**: API-stability assumptions control refactor safety and compatibility expectations.
- **Artifacts to update**: `/.context/architecture/system_overview.md`, `/.context/architecture/module_map.md`, `/AGENTS.md`
- **Answer [reported]**: Only the CLI is treated as stable public API; early-stage work may still include CLI breaking changes.

## Data

5. **[P0] Question**: Should agents run `cs clear` or drop/recreate collections in shared environments?
- **Why it matters for agent safety**: Wrong assumption can cause data loss in shared systems.
- **Artifacts to update**: `/.context/codebase/entrypoints.md`, `/.context/risk/risk_register.md`, `/AGENTS.md`
- **Answer [reported]**: Team currently has no shared production `cs` index.

6. **[P1] Question**: Is a pinned Milvus/Ollama version matrix required for local/CI use?
- **Why it matters for agent safety**: Version ambiguity can create nondeterministic indexing and integration failures.
- **Artifacts to update**: `/.context/project_facts.yaml`, `/.context/engineering/build_test_run.md`, `/.context/codebase/dependency_report.md`
- **Answer [reported]**: No.

## Testing

7. **[P0] Question**: Is `go test ./...` sufficient, or must `make lint` and/or `make test-integration` also pass before merging?
- **Why it matters for agent safety**: Under-testing risks regressions; unclear gates block delivery.
- **Artifacts to update**: `/.context/engineering/build_test_run.md`, `/AGENTS.md`, `/.context/handoff/quality_checklist.md`
- **Answer [reported]**: All available tests should pass before merging unless stated otherwise.

8. **[P1] Question**: For `pkg/vectorstore` changes, should agents add tests first before behavior edits?
- **Why it matters for agent safety**: Low coverage in persistence code increases regression risk.
- **Artifacts to update**: `/.context/risk/risk_register.md`, `/AGENTS.md`, `/.context/handoff/quality_checklist.md`
- **Answer [reported]**: Yes.

## Deployment

9. **[P0] Question**: What is the branch/release policy for merges?
- **Why it matters for agent safety**: Agents need merge authority boundaries to avoid invalid workflows.
- **Artifacts to update**: `/.context/engineering/ci_cd.md`, `/AGENTS.md`, `/.context/risk/risk_register.md`
- **Answer [reported]**: Agents must not merge; branches are managed by the CK orchestrator.

10. **[P1] Question**: Should agents anticipate release packaging/signing/notarization tasks?
- **Why it matters for agent safety**: Wrong assumptions can trigger unsafe release-pipeline edits.
- **Artifacts to update**: `/.context/engineering/ci_cd.md`, `/.context/risk/risk_register.md`
- **Answer [reported]**: Release pipeline is a no-go area for agents.

## Ownership

11. **[P0] Question**: Should agents treat `docs/**`, `.prompts/**`, and `specs/**` as protected by default?
- **Why it matters for agent safety**: Accidental governance-doc edits can conflict with planning/runtime controls.
- **Artifacts to update**: `/AGENTS.md`, `/.context/risk/risk_register.md`, `/.context/architecture/module_map.md`
- **Answer [reported]**: `docs`, `.prompts`, and `specs` are protected; `agent-skills` can be adjusted only if required by task.

12. **[P1] Question**: Who owns dependency upgrade decisions, and should agents proactively upgrade dependencies?
- **Why it matters for agent safety**: Unowned upgrades can break compatibility and release predictability.
- **Artifacts to update**: `/.context/codebase/dependency_report.md`, `/AGENTS.md`, `/.context/risk/risk_register.md`
- **Answer [reported]**: Engineering owns upgrade decisions; agents may propose upgrades in feedback.
