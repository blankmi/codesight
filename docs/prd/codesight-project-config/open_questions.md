# Open Questions: `.codesight/` Project Configuration

No blocking questions identified. All requirements are sufficiently specified in the PRD to proceed with implementation.

## Resolved During Planning

| # | Question | Resolution |
|---|----------|------------|
| 1 | Which TOML library to use? | `github.com/BurntSushi/toml` — well-maintained, stdlib-like API, recommended in PRD |
| 2 | Should `cs clear` clean LSP state? | Deferred to a follow-up task. PRD says "optionally"; initial scope only relocates data. |
| 3 | Should infra settings (db_type, db_address, etc.) be in project config? | Yes, included in config struct for completeness and env var mapping, but documentation should discourage committing infra settings to project config files. |
