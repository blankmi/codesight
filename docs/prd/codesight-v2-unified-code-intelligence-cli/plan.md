# Implementation Plan

## Ordered Tasks

1. TK-001 - Build extraction core package (`pkg/extract`) with file/directory modes and output format contracts.
2. TK-002 - Wire `cs extract` command and flag validation in CLI.
3. TK-003 - Add LSP registry and daemon lifecycle management (`pkg/lsp/registry.go`, `pkg/lsp/lifecycle.go`) with `CODESIGHT_STATE_DIR`, `(workspace, language)` scoping, and stale-state recovery.
4. TK-004 - Add JSON-RPC LSP client transport and protocol primitives (`pkg/lsp/client.go`).
5. TK-005 - Implement `pkg/lsp/refs.go` (LSP references + grep fallback + formatter).
6. TK-006 - Wire `cs refs` command and CLI-level tests, including missing-binary install guidance validation.
7. TK-007 - Implement `pkg/lsp/callers.go` (depth traversal, formatting, cycle/dup guards).
8. TK-008 - Wire `cs callers` command and CLI-level tests, including missing-binary install guidance validation.
9. TK-009 - Implement stretch `pkg/lsp/implements.go` and optional CLI wiring.
10. TK-010 - Update docs/migration artifacts and agent instructions for v2 command set, including Docker runtime model and state-volume guidance.

## Dependency Graph

- TK-001 -> TK-002
- TK-003 -> TK-004 -> TK-005 -> TK-006
- TK-004 -> TK-007 -> TK-008
- TK-003 -> TK-007 (shared lifecycle and process reuse)
- TK-003 -> TK-009
- TK-004 -> TK-009
- TK-006 -> TK-010
- TK-008 -> TK-010
- TK-009 -> TK-010 (only when stretch is shipped)

## Dependency Rationale (Non-obvious)

- TK-004 depends on TK-003 because transport-level client behavior is coupled to lifecycle-managed server processes.
- TK-007 depends on TK-004 even though both are LSP features because callers requires call hierarchy protocol support and request plumbing from the generic client.
- TK-010 is last to avoid doc churn before command names, flags, output contracts, and fallback text are stable.
- TK-006 and TK-008 rely on TK-003 decisions so command-level error behavior can surface correct missing-binary and state-dir guidance.

## Task Slicing and Green Build Rule

- Every task is vertically sliced with code + tests in the same task.
- No task may intentionally leave compile or test failures for a later task.
- If a test scenario cannot execute until a downstream wiring task, include a compiling skipped test using:
  - `t.Skip("blocked by TK-XXX: <reason>")`
- Mandatory gate for every task:
  - `go build ./...`
  - `go test ./...`

## Docker Verification Criteria

- Smoke scenario A (persisted state): run LSP commands with `${CODESIGHT_STATE_DIR}` mounted to a Docker volume; expect warm reuse on subsequent invocations.
- Smoke scenario B (ephemeral state): run LSP commands without persisted state; expect cold start but correct command behavior and outputs.
