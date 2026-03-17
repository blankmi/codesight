- **Purpose**: Catalog of concrete failure patterns that caused rework.
- **Last updated**: 2026-03-17
- **Run**: codesight-project-config

## Entries
[AP-001] Context: `cmd/cs` runtime config pre-run for `init` | Bad: `PersistentPreRunE` loaded config from CWD for all commands, causing `cs init <target>` to fail when unrelated CWD config was malformed. | Fix: Skip runtime config load for `init`, resolve path from target arg, and lock behavior with `TestInit_TargetPathIgnoresMalformedWorkingDirConfig`.
[AP-002] Context: task scope hygiene (`cmd/cs` config command work) | Bad: Out-of-scope doc file (`docs/prd/codesight-project-config/tasks/TK-005.md`) was included in task diff. | Fix: Run a scope gate before review and rebase/cherry-pick so the diff includes only allowlisted task files.
