# Gap Analysis: `.codesight/` Project Configuration

## Current State

### Configuration System
- All runtime configuration is driven by 8 `CODESIGHT_*` environment variables.
- A single `envOrDefault(key, defaultVal)` helper in `cmd/cs/main.go:265-270` handles defaults.
- Direct `os.Getenv()` calls are used for variables without defaults (`CODESIGHT_DB_TOKEN`, `CODESIGHT_GRADLE_JAVA_HOME`).
- No configuration file loading, no config structs, no TOML/YAML dependencies.

### LSP State Storage
- `jdtlsDataDir()` in `cmd/cs/main.go:875-889` stores jdtls data at `<CODESIGHT_STATE_DIR>/jdtls-data/<sha256[:16]>/`.
- `CODESIGHT_STATE_DIR` resolves via `pkg/lsp/lifecycle.go:ResolveStateDir()`, defaulting to `~/.codesight`.
- Hash-based directory names provide no visibility into which project owns which data.
- `cs clear` drops vector index collections but does not clean up LSP state directories.

### CLI Commands
- No `cs init` or `cs config` commands exist.
- Command surface: `index`, `search`, `extract`, `refs`, `callers`, `implements`, `status`, `clear`, `completion`.

## Desired State

### Configuration System
- Layered config: built-in defaults < `~/.codesight/config.toml` (user) < `.codesight/config.toml` (project) < env vars.
- TOML-based config files with typed sections: `[lsp.java]`, `[lsp.go]`, `[embedding]`, `[index]`.
- Config struct that all subsystems consume instead of ad-hoc `os.Getenv()` calls.
- Full backward compatibility: env vars still override everything; config files are optional.

### LSP State Storage
- jdtls data stored at `<project>/.codesight/lsp/java/jdtls-data/` when `.codesight/` exists.
- Automatic `.codesight/.gitignore` with `lsp/` entry.
- Fallback to `~/.codesight/jdtls-data/<hash>/` when `.codesight/` is absent or not writable.
- `cs clear` optionally cleans LSP state alongside vector index.

### CLI Commands
- `cs init .` — scaffolds `.codesight/config.toml` with project-type-detected defaults.
- `cs config .` — shows effective merged configuration with layer provenance.

## Delta

### Net-New
| Item | Description |
|------|-------------|
| `pkg/config` package | Config struct, TOML loading, layered merge, env override logic |
| `.codesight/config.toml` schema | TOML format with `[lsp.*]`, `[embedding]`, `[index]` sections |
| `cs init` command | `cmd/cs/init_command.go` — project-type detection, template generation |
| `cs config` command | `cmd/cs/config_command.go` — effective config display with provenance |
| TOML dependency | `github.com/BurntSushi/toml` (or `github.com/pelletier/go-toml/v2`) |
| `.codesight/.gitignore` auto-creation | Generated during `cs init` and LSP data path setup |

### Modified
| Item | Description |
|------|-------------|
| `cmd/cs/main.go` | Replace `envOrDefault`/`os.Getenv` calls with config struct lookups |
| `cmd/cs/main.go:jdtlsDataDir()` | Prefer `.codesight/lsp/java/jdtls-data/` with fallback |
| `cmd/cs/main.go:jdtlsInitOptions()` | Read `gradle_java_home` from config instead of env var |
| `cmd/cs/main.go:startRefsLSPClient()` | Read timeout from config |
| `pkg/lsp/lifecycle.go` | May need to accept config-driven state dir |
| `cmd/cs/main.go:runClear()` | Optional LSP state cleanup |

### Removed
- No code is removed. All existing env var behavior is preserved as the highest-precedence override layer.
