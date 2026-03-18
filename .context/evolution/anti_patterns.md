- **Purpose**: Catalog of concrete failure patterns that caused rework.
- **Last updated**: 2026-03-17
- **Run**: codesight-project-config, lsp-performance-persistent-client-connections

## Entries
[AP-001] Context: `cmd/cs` runtime config pre-run for `init` | Bad: `PersistentPreRunE` loaded config from CWD for all commands, causing `cs init <target>` to fail when unrelated CWD config was malformed. | Fix: Skip runtime config load for `init`, resolve path from target arg, and lock behavior with `TestInit_TargetPathIgnoresMalformedWorkingDirConfig`.
[AP-002] Context: task scope hygiene (`cmd/cs` config command work) | Bad: Out-of-scope doc file (`docs/prd/codesight-project-config/tasks/TK-005.md`) was included in task diff. | Fix: Run a scope gate before review and rebase/cherry-pick so the diff includes only allowlisted task files.
[AP-003] Context: `pkg/lsp` daemon state cleanup safety | Bad: Lifecycle cleanup trusted persisted `socket_path` from JSON state, allowing mismatched/tainted paths to influence delete targets. | Fix: Derive socket path from trusted state location/key only and ignore persisted `socket_path` payloads during normalization/cleanup.
[AP-004] Context: `pkg/lsp` daemon/lifecycle portability tests | Bad: Unix-only helper usage in shared tests caused `GOOS=windows` test compilation failures. | Fix: Split tests by platform (`//go:build !windows` and `//go:build windows`) and add Windows-specific disabled-daemon expectations.
[AP-005] Context: task scope hygiene (`pkg/lsp` daemon client work) | Bad: Generated artifact `lsp.test.exe` was accidentally committed in task diff and triggered review rejection. | Fix: Add pre-review artifact cleanup and rerun scope gate so only allowlisted source files remain.
[AP-006] Context: `cmd/cs` runtime-to-daemon timeout wiring | Bad: Connector factory timeout plumbing changed without command-level real-factory coverage because tests stubbed the constructor path. | Fix: Add runtime test that exercises the real connector factory with configured short timeout and asserts idle shutdown effects.
