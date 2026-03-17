- **Purpose**: Reusable execution templates extracted from clean approvals.
- **Last updated**: 2026-03-17
- **Run**: codesight-project-config

## Templates
[PB-001] Template: Standard CLI Config Increment (Clean Pass)
1. Confirm task allowlist and keep diff strictly in-scope.
2. Wire arguments/flags in `cmd/cs` and keep behavior logic outside command parsing paths where possible.
3. Preserve deterministic output and existing error string contracts.
4. Add/extend command tests for happy path, validation errors, and path fallback/no-clobber behavior.
5. Run verification sequence: `go test ./cmd/cs -run <suite>`, `go test ./...`, `go build ./...`, `make build`.
6. Report known non-blocking baseline caveats separately (`make lint` baseline, Docker absence for integration tests).

[PB-002] Template: Command-Aware PreRun Safety
1. Audit `PersistentPreRunE` effects for every command before introducing shared runtime config loads.
2. Exempt bootstrap/setup commands (like `init`) from dependencies on pre-existing project config.
3. Add regression tests proving target-path behavior is independent from malformed CWD state.
