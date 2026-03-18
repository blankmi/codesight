# Spec: LSP Performance — Persistent Client Connections

## Overview

### Goal

Reduce repeat LSP command latency for the same workspace/language by reusing warm language-server processes across `cs` CLI invocations.

### In-Scope

- Phase 1: Persistent daemon process and socket-based client connection for `refs`, `callers`, and `implements` on Linux/macOS.
- Phase 1: Daemon idle shutdown, stale PID/socket recovery, and state persistence updates.
- Phase 2: Java Gradle re-sync suppression when tracked build files are unchanged.
- Phase 3: `cs warmup <path>`, `cs index` optional warmup hook, and cold-start tip messaging.
- Backward-compatible preservation of existing output and error contracts.
- Windows fallback behavior: keep legacy per-invocation LSP startup path for this epic.

### Out-of-Scope

- Concurrent multiplexing of multiple simultaneous `cs` clients through one daemon.
- Remote daemon transport (TCP/WebSocket) and non-local sockets.
- Windows named-pipe daemon implementation.
- New LSP feature APIs beyond existing refs/callers/implements usage.
- Release pipeline or non-LSP command behavior changes.

---

## Functional Requirements

### FR-01: Daemon Identity and Addressing

- **Preconditions**: Workspace root resolves to an absolute, canonical path and language is supported by `pkg/lsp.Registry`.
- **Behavior**:
  - Compute a deterministic state key from `(workspace_root, language)`.
  - Daemon runtime state for that key includes PID, socket path, workspace root, language, start time, and last-used time.
  - Socket path format: `<state_root>/lsp/<state_key>.sock`.
- **Invariant**: Exactly one daemon state record exists per `(workspace_root, language)` key.

### FR-02: Connect, Reuse, and Recovery

- **Preconditions**: A command requires LSP transport (`refs`, `callers`, or `implements`) and runtime is Linux/macOS.
- **Behavior**:
  - Attempt to connect to an existing daemon socket for the state key.
  - If state/socket is stale (dead PID, missing socket, invalid state file), remove stale artifacts and relaunch daemon.
  - Update `last_used_unix_nano` on successful connect and on request activity.
- **Invariant**: A failed stale-state attempt may retry once after cleanup; no infinite relaunch loop.

### FR-03: Daemon JSON-RPC Proxying

- **Preconditions**: Daemon has launched and owns child language-server stdin/stdout.
- **Behavior**:
  - Proxy JSON-RPC frames between socket client and LSP stdio transport.
  - Preserve request/response IDs and message framing (`Content-Length` protocol contract).
  - Allow only one active socket client at a time.
- **Contract**: Concurrent connection attempts must fail deterministically with error containing substring `lsp daemon busy`.

### FR-04: Command Wiring Compatibility

- **Preconditions**: `cs refs|callers|implements` invoked.
- **Behavior**:
  - Linux/macOS: replace per-invocation `startRefsLSPClient` startup with daemon connector path.
  - Windows: preserve legacy per-invocation startup path.
  - Preserve these behavior contracts:
    - `refs` keeps grep fallback behavior and note format `(grep-based - install <binary> for precise results)`.
    - `callers` and `implements` continue fail-fast behavior when required LSP binary is missing.
    - Existing deterministic output ordering and ambiguity formatting remain unchanged.
- **Invariant**: Existing command tests asserting exact strings remain green.

### FR-05: Idle Timeout and Graceful Shutdown

- **Preconditions**: Daemon has no client activity for configured idle duration.
- **Behavior**:
  - Default idle timeout is `10m`.
  - Effective timeout source order:
    - `CODESIGHT_LSP_DAEMON_IDLE_TIMEOUT` (highest precedence)
    - `lsp.daemon.idle_timeout` in project/user TOML config
    - hard default `10m`
  - Timeout value must parse as positive duration (`> 0`).
  - On idle timeout, daemon sends LSP `shutdown` then `exit`; if unresponsive, force-kill process and remove state/socket.
- **Contract**: Invalid timeout values fail config load with message containing `invalid lsp.daemon.idle_timeout`.

### FR-06: Java Gradle Re-sync Suppression (Phase 2)

- **Preconditions**: Language is Java and daemon performs a cold start.
- **Behavior**:
  - Persist tracked Java build-file metadata for the workspace.
  - Tracked files are exactly:
    - `build.gradle.kts`
    - `build.gradle`
    - `settings.gradle.kts`
    - `settings.gradle`
  - Maven files (`pom.xml`) are excluded in this epic.
  - If tracked metadata is unchanged since last successful daemon start, include initialize option:
    - `settings.java.import.gradle.enabled = false`
  - If changed or no baseline exists, do not set this override.
- **Invariant**: Existing `gradle.java.home` initialize-option behavior remains merge-safe and preserved.

### FR-07: `cs warmup <path>` Command (Phase 3)

- **Preconditions**: CLI invocation of `cs warmup [path]` with optional path (default current directory).
- **Behavior**:
  - Resolve workspace root with same path canonicalization used by refs/callers/implements.
  - Detect supported language and initialize daemon without executing refs/callers/implements queries.
  - Success output (exact): `LSP warmup ready (<language>): <workspace_root>`.
- **Edge Cases**:
  - Unsupported-language workspace returns non-error informational output (exact):
    - `No supported LSP language detected for warmup`

### FR-08: Optional Warmup From `cs index` (Phase 3)

- **Preconditions**: `cs index` invoked and effective config `index.warm_lsp=true`.
- **Behavior**:
  - If project language detection resolves Java, trigger daemon warmup in background.
  - Index command success/failure is independent of warmup result.
- **Contract**: Warmup failure logs warning to stderr containing substring `lsp warmup failed` and does not fail `cs index`.

### FR-09: Slow Cold-Start User Hint (Phase 3)

- **Preconditions**: `cs refs` cold-starts Java daemon and startup latency exceeds `5s`.
- **Behavior**:
  - Emit exactly: `Tip: run 'cs warmup .' to pre-start the language server`
  - Do not emit this hint for warm calls, Windows legacy mode, or non-Java flows.

### FR-10: Green-Build Task Invariant

- **Preconditions**: Any implementation task derived from this spec.
- **Behavior**:
  - Each task includes required production + test updates in the same task slice.
  - Each task leaves repository passing `go build ./...` and `go test ./...`.
- **Invariant**: No task may intentionally break compilation/tests for a later task to repair.

### FR-11: Platform Fallback Contract

- **Preconditions**: Runtime OS is Windows.
- **Behavior**:
  - Daemon socket mode is disabled.
  - LSP commands use legacy per-invocation startup path with unchanged user-facing contracts.
- **Invariant**: Windows builds and tests continue to compile/run without Unix-socket dependencies.

---

## Non-Functional Requirements

### NFR-01: Performance

- Warm-call target for same workspace/language:
  - Java (`jdtls`): `<1s`
  - Go (`gopls`): `<300ms`
- Cold-start budget remains acceptable for Phase 1 and improves in Phase 2.

### NFR-02: Reliability

- Stale PID/socket state auto-recovers without manual cleanup.
- Daemon process lifetime is bounded by idle timeout.

### NFR-03: Backward Compatibility

- Existing deterministic output lines and error strings for refs/callers/implements remain unchanged unless explicitly specified in this spec.

### NFR-04: Security and Local Isolation

- State directories remain user-only (`0700`).
- State files and socket metadata remain user-only (`0600` where applicable).
- Daemon transport remains local-only.

### NFR-05: Portability

- Linux/macOS use Unix-socket daemon mode.
- Windows uses legacy non-daemon mode in this epic.
- Repository remains green for `go build ./...` and `go test ./...` across supported platforms.

---

## Success / Definition of Done

1. `go build ./...` succeeds.
2. `go test ./...` passes.
3. Repeated `cs refs` / `cs callers` / `cs implements` on warm Linux/macOS Java workspaces reuse daemon state instead of spawning fresh LSP processes.
4. `refs` grep fallback contract and note format are preserved.
5. `callers` and `implements` missing-LSP exact error contracts are preserved.
6. Daemon stale PID/socket recovery works without manual cleanup.
7. Idle-timeout shutdown behavior is covered by automated tests.
8. Java unchanged-build path sets `java.import.gradle.enabled=false` and changed-build path does not.
9. `cs warmup` command works per output/error contracts.
10. `cs index` optional warmup path does not regress index command success semantics.
11. Windows runtime behavior remains legacy per-invocation startup with unchanged command contracts.
