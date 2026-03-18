# Open Questions: LSP Performance — Persistent Client Connections

## Open Blocking Questions

No open blocking questions remain as of 2026-03-17.

## Resolved Questions

### OQ-001 [RESOLVED]

- **Question**: Should daemon idle timeout be configured via TOML key (`lsp.daemon.idle_timeout`), env var only, or both?
- **Impact**: FR-05 and TK-005 configuration contract.
- **Suggested owner**: product
- **Resolution**: Support both (`lsp.daemon.idle_timeout` + `CODESIGHT_LSP_DAEMON_IDLE_TIMEOUT` override) with default `10m`.
- **Default assumption**: Not applicable after resolution.

### OQ-002 [RESOLVED]

- **Question**: For Windows builds, should we implement named pipes now, or explicitly disable daemon mode and keep legacy per-invocation startup?
- **Impact**: NFR-05 and TK-002/TK-004 platform strategy.
- **Suggested owner**: tech
- **Resolution**: Linux/macOS ship daemon mode; Windows falls back to legacy per-invocation LSP path with unchanged command contracts.
- **Default assumption**: Not applicable after resolution.

### OQ-003 [RESOLVED]

- **Question**: Is the build-change detector limited to `build.gradle.kts`, or must it include `build.gradle`, `settings.gradle(.kts)`, and Maven `pom.xml`?
- **Impact**: FR-06 and TK-006 behavior/test matrix.
- **Suggested owner**: domain expert
- **Resolution**: Track `build.gradle.kts`, `build.gradle`, `settings.gradle.kts`, and `settings.gradle`; exclude Maven from this epic.
- **Default assumption**: Not applicable after resolution.

### OQ-004 [RESOLVED]

- **Question**: Should this epic deliver all three phases in one implementation stream, or should Phase 2/3 be split into follow-up epics after Phase 1 lands?
- **Impact**: Scope and sequencing of TK-006/TK-007.
- **Suggested owner**: product
- **Resolution**: Keep all phases in this epic, with Phase 1 as mandatory baseline and Phase 2/3 included in planned task sequence.
- **Default assumption**: Not applicable after resolution.
