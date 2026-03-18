- **Purpose**: Reusable execution templates extracted from clean approvals.
- **Last updated**: 2026-03-17
- **Run**: codesight-project-config, lsp-performance-persistent-client-connections

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

[PB-003] Template: LSP Runtime Routing Increment (Clean Pass)
1. Keep command handlers focused on flag/arg wiring and route connector behavior through `cmd/cs` runtime helpers.
2. Preserve platform contracts explicitly: Unix daemon path on supported systems and deterministic disabled-daemon behavior on Windows.
3. Extend command-level tests for `refs/callers/implements` to lock output/error contracts while changing runtime plumbing.
4. Run targeted LSP suites (`go test ./cmd/cs -run 'Refs|Callers|Implements'`, `go test ./pkg/lsp -run 'Lifecycle|Daemon'`) plus baseline project checks (`go test ./...`, `go build ./...`, `make build`, `make lint`).

[PB-004] Template: Daemon UX + Warmup Increment (Clean Pass)
1. Introduce warmup/status hints only behind explicit runtime conditions (cold start, daemon unavailability, index state).
2. Verify hints and warnings across command surfaces (`warmup`, `refs`, `index`) so guidance is consistent and non-noisy.
3. Keep behavior deterministic in tests by asserting both presence and suppression paths for warnings/tips.
