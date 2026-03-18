# Plan: LSP Performance — Persistent Client Connections

## Task Order

| ID | Task | Type | Depends On |
|---|---|---|---|
| TK-001 | Extend LSP lifecycle state model for daemon socket metadata and recovery primitives | logic | — |
| TK-002 | Implement LSP daemon server (socket listener + stdio proxy + idle shutdown) | logic | TK-001 |
| TK-003 | Implement daemon client/connector with launch + stale-state recovery flow | logic | TK-002 |
| TK-004 | Wire refs/callers/implements command runtime to daemon connector while preserving contracts | logic | TK-003 |
| TK-005 | Add configurable daemon idle timeout through config/env plumbing | logic | TK-002 |
| TK-006 | Add Java build-change detection and Gradle re-sync suppression policy | logic | TK-003 |
| TK-007 | Add `cs warmup`, `cs index` warmup hook, and slow cold-start tip behavior | logic | TK-004, TK-006 |

## Dependency Graph

```text
TK-001
  -> TK-002
      -> TK-003
          -> TK-004
          -> TK-006
      -> TK-005
TK-004 + TK-006
  -> TK-007
```

## Dependency Rationale

- **TK-002 depends on TK-001**: daemon server needs persisted socket/PID metadata and stale cleanup primitives.
- **TK-003 depends on TK-002**: client connector requires concrete daemon startup/connect protocol.
- **TK-004 depends on TK-003**: command handlers can only switch transport after connector API is stable.
- **TK-005 depends on TK-002**: idle-timeout configurability is applied to daemon runtime behavior.
- **TK-006 depends on TK-003**: Phase 2 policy hooks into daemon cold-start/initialize flow.
- **TK-007 depends on TK-004 and TK-006**: warmup/index/tip UX depends on daemon wiring plus Java cold-start classification behavior.
