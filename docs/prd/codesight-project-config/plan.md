# Plan: `.codesight/` Project Configuration

## Task Order

| ID | Task | Type | Depends On |
|----|------|------|------------|
| TK-001 | Add TOML dependency and create `pkg/config` with Config struct, TOML loading, and layered merge | logic | — |
| TK-002 | Wire config loading into CLI entrypoint and replace `envOrDefault` / `os.Getenv` call sites | logic | TK-001 |
| TK-003 | Relocate jdtls data to `.codesight/lsp/` with fallback and auto-gitignore | logic | TK-002 |
| TK-004 | Add `cs init` command with project-type detection | logic | TK-001 |
| TK-005 | Add `cs config` command for effective config display | logic | TK-002 |

## Dependency Graph

```
TK-001 (pkg/config)
  ├── TK-002 (wire into CLI) ──┬── TK-003 (LSP data relocation)
  │                            └── TK-005 (cs config command)
  └── TK-004 (cs init command)
```

## Dependency Rationale

- **TK-002 depends on TK-001**: The CLI wiring task consumes the `Config` struct and `LoadConfig()` function that TK-001 creates.
- **TK-003 depends on TK-002**: LSP data relocation reads config values (e.g., project path awareness) that are available only after the CLI is wired to the config system.
- **TK-004 depends on TK-001**: `cs init` generates `config.toml` files and needs the config schema/defaults defined in `pkg/config`, but does not need the CLI wiring from TK-002.
- **TK-005 depends on TK-002**: `cs config` calls `LoadConfig()` and displays the merged result with provenance, requiring the full wiring from TK-002.
