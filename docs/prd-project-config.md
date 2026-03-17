# PRD: Project-Level Configuration (`.codesight/`)

## Problem

Codesight currently uses 8 environment variables for all configuration. This has three issues:

1. **Not portable across machines.** `CODESIGHT_GRADLE_JAVA_HOME=/opt/homebrew/opt/openjdk@17/...` is macOS-specific. A team member on Linux has a different path. The setting can't be committed to the repo.

2. **Not project-scoped.** `CODESIGHT_EMBEDDING_MODEL=nomic-embed-text` applies globally. A Python ML repo might want a different model than a Java enterprise repo. Environment variables can't distinguish.

3. **LSP data pollutes the home directory.** `~/.codesight/jdtls-data/<hash>/` stores per-project LSP indexes in a global location. There's no way to tell which hash belongs to which project, and `cs clear` doesn't clean them up.

## Goal

Introduce a `.codesight/` directory at the project root that holds:
- **Project configuration** (LSP settings, embedding model, ignore overrides)
- **LSP workspace data** (jdtls project index, language server caches)
- **Index metadata** (optional — staleness info, last indexed commit)

Configuration is layered: project `.codesight/config.toml` overrides user `~/.codesight/config.toml` overrides built-in defaults. Environment variables override everything (for CI/Docker).

## Current Configuration

| Variable | Default | Project-specific? |
|----------|---------|-------------------|
| `CODESIGHT_DB_TYPE` | `milvus` | No — same backend everywhere |
| `CODESIGHT_DB_ADDRESS` | `localhost:19530` | Rarely — maybe staging vs prod |
| `CODESIGHT_DB_TOKEN` | (empty) | Rarely |
| `CODESIGHT_OLLAMA_HOST` | `http://127.0.0.1:11434` | No |
| `CODESIGHT_EMBEDDING_MODEL` | `nomic-embed-text` | **Yes** — different repos may want different models |
| `CODESIGHT_OLLAMA_MAX_INPUT_CHARS` | (auto) | **Yes** — depends on model |
| `CODESIGHT_STATE_DIR` | `~/.codesight` | No — global state root |
| `CODESIGHT_GRADLE_JAVA_HOME` | (none) | **Yes** — depends on project's JDK requirement |

Only 3 of 8 are truly project-specific. The rest are infrastructure settings.

## Proposed Directory Structure

```
my-project/
├── .codesight/
│   ├── config.toml          # Project-specific configuration
│   └── lsp/                 # LSP workspace data (gitignored)
│       └── java/            # Per-language LSP cache
│           └── jdtls-data/  # jdtls project index
├── .csignore                # Ignore patterns (existing)
├── .gitignore
└── src/
```

### `config.toml` — Project Configuration

```toml
# .codesight/config.toml

[lsp.java]
# JDK for Gradle/Maven builds (separate from jdtls runtime JDK)
gradle_java_home = "/usr/lib/jvm/java-17-openjdk"
# Additional jdtls arguments
args = ["-data", ".codesight/lsp/java/jdtls-data"]
# Request timeout override (default: 60s for jdtls, 10s for others)
timeout = "90s"

[lsp.go]
# gopls build flags
build_flags = ["-tags=integration"]

[embedding]
# Override embedding model for this project
model = "nomic-embed-text"
# Max input chars override
max_input_chars = 4096

[index]
# Auto-warm LSP on index
warm_lsp = true
```

### Layering and Precedence

```
env var > .codesight/config.toml (project) > ~/.codesight/config.toml (user) > defaults
```

- Environment variables always win (for CI, Docker, one-off overrides)
- Project config is committed to the repo (minus `lsp/` which is gitignored)
- User config provides personal defaults (e.g., custom Ollama host)

### What Goes in `.gitignore`

```gitignore
# .codesight/.gitignore
lsp/
```

The `lsp/` directory contains machine-specific cached data (jdtls indexes, gopls caches). The `config.toml` is intended to be committed so team members share the same LSP and embedding settings.

## Config Resolution

```go
// Pseudocode for config loading
func LoadConfig(projectPath string) Config {
    cfg := defaultConfig()

    // Layer 1: user config
    if home, err := os.UserHomeDir(); err == nil {
        cfg.merge(loadTOML(filepath.Join(home, ".codesight", "config.toml")))
    }

    // Layer 2: project config
    cfg.merge(loadTOML(filepath.Join(projectPath, ".codesight", "config.toml")))

    // Layer 3: environment variables (highest priority)
    cfg.mergeEnv()

    return cfg
}
```

## LSP Data Migration

Currently, jdtls data is stored at:
```
~/.codesight/jdtls-data/<sha256_hash>/
```

With project config, it moves to:
```
<project>/.codesight/lsp/java/jdtls-data/
```

Benefits:
- Clear ownership — the data lives with the project
- `rm -rf .codesight/lsp/` cleans up all LSP state for a project
- `cs clear <path>` can also clean LSP state alongside the vector index
- No orphaned hash directories in `~/.codesight/`

Fallback: if `.codesight/` doesn't exist or isn't writable, fall back to `~/.codesight/lsp-data/<hash>/` (current behavior).

## New Commands

### `cs init`

Creates `.codesight/config.toml` with sensible defaults for the detected project type:

```bash
cs init .
# Detects: Java (Gradle), creates:
#   .codesight/config.toml with [lsp.java] section
#   .codesight/.gitignore with lsp/
```

Auto-detection:
- `build.gradle.kts` / `pom.xml` → Java, suggest `gradle_java_home`
- `go.mod` → Go, suggest `build_flags` if tags are used
- `package.json` → TypeScript/JavaScript
- `Cargo.toml` → Rust

### `cs config`

Show effective configuration for a project (all layers merged):

```bash
cs config .
# embedding.model = nomic-embed-text (default)
# lsp.java.gradle_java_home = /usr/lib/jvm/java-17 (.codesight/config.toml)
# lsp.java.timeout = 60s (default)
# db.address = localhost:19530 (default)
```

## Scope

### Phase 1: Config file loading
- TOML parser (use `github.com/BurntSushi/toml` or `github.com/pelletier/go-toml`)
- Load user + project config, merge with env vars
- Replace `envOrDefault` calls with config lookups
- Backward compatible — env vars still work, config file is optional

### Phase 2: LSP data in project directory
- Move jdtls `-data` to `.codesight/lsp/java/jdtls-data/`
- Create `.codesight/.gitignore` automatically
- Update `cs clear` to optionally clean LSP state

### Phase 3: `cs init` and `cs config`
- Project type detection
- Template generation
- Config display

## Risks

- **TOML dependency**: Adds a new dependency. Mitigate by using a well-maintained library.
- **Config file conflicts**: Two developers with different JDKs. Mitigate by keeping machine-specific paths in user config (`~/.codesight/config.toml`), not project config.
- **Breaking change**: Existing `CODESIGHT_*` env vars must continue working. Config file is additive, never removes env var support.

## Out of Scope

- Per-directory config overrides (only project root and user home)
- Config file for `.csignore` rules (`.csignore` stays as a separate file)
- GUI/TUI for config editing
- Remote config (fetching from a server)
