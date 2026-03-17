- **Purpose**: Low-blast-radius transition steps from archaeology to first agentic implementation.
- **Last verified**: 2026-03-17
- **Source**: mixed

1. **Fix one existing lint baseline issue in tests (single-file scope), such as unchecked `Encode` error in `pkg/embedding/ollama_test.go`.**
- **Why safe**: test-only, behavior-preserving, and easy to revert.
- **Read first**: `/.context/engineering/build_test_run.md`, `/.context/risk/risk_register.md`.
- **Verify success**: `make lint` reports one fewer issue and `go test ./...` remains green.

2. **Add first direct unit test(s) for `pkg/vectorstore` helper behavior without network calls.**
- **Why safe**: isolated package hardening with no production runtime behavior change.
- **Read first**: `/.context/architecture/module_map.md`, `/.context/risk/risk_register.md`.
- **Verify success**: `go test ./pkg/vectorstore` and `go test ./...` pass.

3. **Add one CLI regression test covering destructive `cs clear` behavior contracts (without dropping a real collection).**
- **Why safe**: command test-only hardening, no schema/data mutation in shared systems.
- **Read first**: `/.context/codebase/entrypoints.md`, `/.context/risk/risk_register.md`.
- **Verify success**: `go test ./cmd/cs -run Clear` (or full `go test ./cmd/cs`) passes.

4. **Add a focused test for `pkg/ignore` handling of `.csignore` relative vs absolute path cases.**
- **Why safe**: contained logic in a mature module; no multi-module blast radius.
- **Read first**: `/.context/architecture/module_map.md`, `/.context/engineering/build_test_run.md`.
- **Verify success**: `go test ./pkg/ignore` and `go test ./...` pass.

5. **Prepare (but do not merge) a dependency-refresh proposal branch for one indirect cluster (`x/sys`, `x/net`, `protobuf`, `grpc`).**
- **Why safe**: isolated change set with clear rollback path and no feature mixing.
- **Read first**: `/.context/codebase/dependency_report.md`, `/.context/engineering/ci_cd.md`.
- **Verify success**: isolated dependency-only diff plus green `go test ./...`, `make build`, and `make lint`.
