# PRD: LSP Performance — Persistent Client Connections

## Problem

Every `cs refs`, `cs callers`, and `cs implements` invocation spawns a fresh language server process. For jdtls (Java), this means:

- ~2s JVM startup + OSGi framework boot
- ~3s Gradle project sync (even with cached `-data` directory)
- ~2s workspace/symbol index scan
- ~1s actual request + response

**Total: ~8s per call** on a 250K LOC project. This makes LSP commands impractical for interactive agent workflows where 3-5 `cs refs` calls in sequence would add 30-40 seconds.

For comparison, `gopls` cold-starts in ~1s and subsequent calls take <500ms because it uses a daemon mode. jdtls has no built-in daemon mode.

## Goal

Reduce repeat LSP command latency to **<1 second** for warm calls (same workspace, same language). First call may still be slow (~8-10s for jdtls cold start).

## Current Architecture

```
cs refs Target --path /project
  └─ detectRefsLanguage()           → "java"
  └─ startRefsLSPClient()
       └─ exec.Command("jdtls", "-data", ...)  ← NEW PROCESS EVERY CALL
       └─ stdin/stdout pipes
       └─ Initialize request
       └─ workspace/symbol request
       └─ textDocument/references request
  └─ client.Shutdown()              ← PROCESS KILLED
```

The old `Lifecycle` manager kept a background process alive by PID but never connected to its stdio — it was effectively a no-op. It was removed in commit `4fec733`.

## Proposed Architecture

### Phase 1: Persistent LSP Daemon with Stdio Multiplexing

```
cs refs Target --path /project
  └─ LSPDaemon.Connect(workspace, language)
       ├─ if running daemon exists → connect to Unix socket
       └─ else → spawn jdtls, hold stdio, listen on Unix socket
  └─ workspace/symbol request       ← REUSES WARM PROCESS
  └─ textDocument/references request
  └─ disconnect (daemon stays alive)
```

**Key changes:**

1. **LSP daemon holds the process and stdio pipes**
   - Start jdtls once per (workspace, language) pair
   - Hold stdin/stdout as `io.ReadWriteCloser`
   - Expose a Unix domain socket under `~/.codesight/lsp/<state_key>.sock`
   - Each `cs` CLI invocation connects to the socket

2. **JSON-RPC multiplexer on the daemon side**
   - The daemon proxies JSON-RPC between the Unix socket client and the jdtls stdio
   - Handles request/response ID mapping so multiple sequential `cs` calls can share one jdtls session
   - Single-client at a time (no concurrent multiplexing needed — `cs` calls are sequential)

3. **Idle timeout with graceful shutdown**
   - Daemon shuts down jdtls after 10 minutes of inactivity (configurable)
   - Stale PID/socket recovery on startup
   - State file tracks PID + socket path

4. **Warm-up on `cs index`**
   - When `cs index` runs on a Java project, optionally start the LSP daemon in the background
   - The user pays warm-up cost during indexing, not on first query
   - Controlled by project config (see companion PRD)

### Phase 2: Skip Gradle Re-sync for Read-Only Operations

Once the daemon persists the jdtls process:

- Pass `java.import.gradle.enabled: false` on subsequent Initialize calls to skip re-validation
- Only re-sync when the project's `build.gradle.kts` changes (detect via file modification time)
- This saves ~3s per cold start

### Phase 3: Pre-warm Heuristic

- On `cs refs` cold start, if jdtls takes >5s, print a hint:
  `"Tip: run 'cs warmup .' to pre-start the language server"`
- `cs warmup <path>` starts the daemon without executing a query
- Agent instructions can include `cs warmup` as a first step

## Performance Targets

| Scenario | Current | Phase 1 | Phase 2 |
|----------|---------|---------|---------|
| jdtls cold start | 8.6s | 8.6s (first call) | 5s (skip re-sync) |
| jdtls warm call | 8.6s | **<1s** | <1s |
| gopls cold start | ~1s | ~1s | ~1s |
| gopls warm call | ~1s | **<300ms** | <300ms |

## Implementation Scope

### Phase 1 (recommended first)
- New `pkg/lsp/daemon.go` — daemon process with Unix socket
- New `pkg/lsp/daemon_client.go` — client that connects to daemon socket
- Update `cmd/cs/main.go` — use daemon connect instead of `startRefsLSPClient`
- Update state file format to include socket path
- Tests: daemon lifecycle, concurrent connect attempts, stale socket recovery

### Phase 2
- Detect build file changes via mtime
- Pass `java.import.gradle.enabled: false` when build files unchanged

### Phase 3
- `cs warmup` command
- Optional warm-up from `cs index`

## Risks

- **Unix socket portability**: Works on macOS/Linux. Windows would need named pipes.
- **Zombie daemons**: If `cs` crashes, the daemon stays alive. Mitigated by idle timeout and stale PID recovery.
- **jdtls stability**: Long-running jdtls may accumulate memory or become unresponsive. Mitigate with health checks and automatic restart.
- **Multiple workspaces**: Each (workspace, language) pair gets its own daemon. 5 Java projects = 5 jdtls processes. Acceptable for developer machines.

## Out of Scope

- Concurrent multiplexing (multiple `cs` calls at the same time)
- Remote LSP connections (TCP/WebSocket)
- LSP protocol features beyond workspace/symbol, references, callHierarchy, typeHierarchy
