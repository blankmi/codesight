- **Purpose**: Risk inventory and avoidance guidance for low-blast-radius agent changes.
- **Last verified**: 2026-03-17
- **Source**: mixed

## Risk Register

| ID | Risk | Severity | Evidence | Mitigation | Source |
|---|---|---|---|---|---|
| R-001 | Monolithic command wiring hotspot in `cmd/cs/main.go` increases merge/regression risk | High | file is 1145 lines | keep edits narrow and add/adjust command tests | observed |
| R-002 | `pkg/vectorstore` has no direct unit tests | High | 0 `_test.go` files | add tests first before behavior changes | mixed (observed + reported) |
| R-003 | Network-dependent commands fail when Milvus/Ollama are unavailable | High | `cs index` failed locally with Ollama connection refused | include explicit runtime dependency checks in task verification | verified |
| R-004 | Planning/coordination docs are stale vs code reality | Medium | `docs/prd/.../status.md` remains all `PENDING` | treat docs as protected unless explicitly task-scoped | observed |
| R-005 | Non-release CI gate location is ambiguous | Medium | only release workflow in repo + GitLab remote | confirm authoritative non-release CI before merge-gate automation | mixed |
| R-006 | Transitive dependency drift is significant | Medium | `go list -m -u` shows lag in grpc/protobuf/x/* deps | isolate dependency upgrades into dedicated tasks | verified |
| R-007 | `cs clear` is destructive and drops collections | High | `runClear` invokes collection drop | require explicit intent; never run implicitly in automation | observed |
| R-008 | LSP behavior varies by language-server availability | Medium | refs fallback vs callers/implements fail-fast | encode LSP assumptions in plans and tests | observed |
| R-009 | Lint baseline is currently non-green | Medium | `make lint` reports baseline `errcheck`/`unused` findings | separate baseline cleanup from feature tasks | verified |
| R-010 | Agent-scope drift into protected governance files | High | maintainers flagged docs/specs/prompts/release as protected | enforce protected-path checks before edits | reported |

## Hotspots (>500 lines)
- [observed] `cmd/cs/main.go` (1145)
- [observed] `pkg/extract/extract.go` (643)
- [observed] `cmd/cs/callers_command_test.go` (534)
- [observed] `pkg/lsp/refs_test.go` (514)
- [observed] `cmd/cs/refs_command_test.go` (513)
- [observed] `pkg/lsp/client.go` (511)

## Protected Paths (Avoid Unless Task-Scoped)
- [reported] `docs/**` (coordination and planning docs)
- [reported] `.prompts/**` (workflow contracts)
- [reported] `specs/**` (context-kernel runtime specs)
- [observed] `.github/workflows/release.yml` (release automation)
- [observed] `bin/**` (generated binaries; regenerate instead of editing)
- [reported] `agent-skills/**` is editable only for explicitly scoped skill/tooling tasks

## Active Work Items to Avoid
- [observed] `docs/prd/codesight-v2-unified-code-intelligence-cli/status.md` marks planning and task tracks as `PENDING`.
- [observed] Codebase already contains implemented features referenced by those planning docs, indicating doc/code drift.
- [observed] Default safe policy: avoid editing `docs/**` unless the task explicitly targets documentation.
