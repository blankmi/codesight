# Gap Analysis: LSP Performance — Persistent Client Connections

## Current State

- `cs refs`, `cs callers`, and `cs implements` each start a new language server process per invocation via `startRefsLSPClient()` in `cmd/cs/main.go`.
- The command handlers defer `client.Shutdown(...)`, so the process and stdio transport are torn down at the end of every command.
- `jdtls` already reuses a persistent `-data` directory (`jdtlsDataDir(...)`), but process startup/import/index cost is still paid on every call.
- `pkg/lsp/lifecycle.go` tracks PID-based lifecycle state and idle timeout metadata, but CLI command execution is not wired to reuse that process state.
- There is no daemon socket transport, no cross-invocation JSON-RPC bridge, no `cs warmup` command, and no `cs index` warmup hook.
- Windows currently follows the same per-invocation startup path as Linux/macOS and has no platform-specific daemon strategy.
- Existing behavior contracts are test-locked:
  - `refs` may fallback to grep with note format `(grep-based - install <binary> for precise results)`.
  - `callers` and `implements` fail fast without LSP binaries and must preserve exact error strings.

## Desired State

- Reuse warm LSP processes across CLI invocations on Linux/macOS by introducing a persistent daemon keyed by `(workspace, language)`.
- Route CLI invocations through a local socket client to the daemon, which owns child-process stdio and JSON-RPC forwarding.
- Recover from stale PID/socket state and shut daemons down after idle timeout.
- Preserve existing command output/error contracts while reducing warm-call latency (target: `<1s` for warm Java calls).
- Keep Windows on the legacy per-invocation runtime path in this epic, with unchanged command contracts.
- Add warmup paths:
  - `cs warmup <path>` for explicit pre-start.
  - Optional warmup from `cs index` when enabled by config.
- Add Phase 2 optimization for Java cold starts by skipping Gradle re-sync when build files are unchanged.
- Make daemon idle timeout configurable via `lsp.daemon.idle_timeout` with `CODESIGHT_LSP_DAEMON_IDLE_TIMEOUT` override and `10m` default.
- Define Java build-change watch set as `build.gradle.kts`, `build.gradle`, `settings.gradle.kts`, and `settings.gradle`.

## Delta

### Net-New

- Daemon server implementation in `pkg/lsp` (socket listener + LSP stdio ownership + JSON-RPC proxy).
- Daemon client/connector implementation in `pkg/lsp`.
- New CLI warmup command in `cmd/cs`.
- Daemon-specific tests for lifecycle, stale recovery, and proxy behavior.

### Modified

- `cmd/cs` refs/callers/implements execution flow to use daemon connection instead of per-call process startup.
- LSP lifecycle state schema to persist socket path and daemon metadata required for reconnect/recovery.
- Java initialize option construction to support Gradle re-sync suppression on unchanged builds.
- Config plumbing to expose daemon idle-timeout control with TOML + env precedence.
- Platform branch behavior so Linux/macOS use daemon mode while Windows preserves legacy startup.

### Removed

- Per-invocation LSP process model as the primary runtime path for refs/callers/implements.
- Unused/deferred lifecycle wiring assumptions in tests once command integration is completed.
