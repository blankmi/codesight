# Spec: `.codesight/` Project Configuration

## Overview

### Goal
Introduce a `.codesight/` directory at the project root that provides layered, file-based configuration for Codesight. This replaces the current environment-variable-only configuration model with a three-tier system (defaults < user config < project config < env vars) while maintaining full backward compatibility.

### In-Scope
- TOML-based configuration file format (`config.toml`)
- Config struct and layered loading logic (`pkg/config`)
- Migration of `envOrDefault` / `os.Getenv` call sites to config struct
- LSP workspace data relocation to `.codesight/lsp/`
- `cs init` command for project scaffolding
- `cs config` command for effective configuration display
- Automatic `.codesight/.gitignore` generation

### Out-of-Scope
- Per-directory config overrides (only project root and user home)
- Config file for `.csignore` rules (stays as separate file)
- GUI/TUI for config editing
- Remote config (fetching from a server)
- Database/infrastructure settings in project config (db_type, db_address, db_token, ollama_host remain infra-only)

---

## Functional Requirements

### FR-01: Config File Format
- **Precondition**: None.
- **Behavior**: Configuration files use TOML format at two locations:
  - User: `~/.codesight/config.toml`
  - Project: `<project_root>/.codesight/config.toml`
- **Sections**:
  ```toml
  [lsp.java]
  gradle_java_home = "/usr/lib/jvm/java-17-openjdk"
  args = ["-data", ".codesight/lsp/java/jdtls-data"]
  timeout = "90s"

  [lsp.go]
  build_flags = ["-tags=integration"]

  [embedding]
  model = "nomic-embed-text"
  max_input_chars = 4096

  [index]
  warm_lsp = true
  ```
- **Validation**: Unknown top-level sections produce a warning on stderr. Unknown keys within known sections produce a warning. Malformed TOML produces an error and aborts.

### FR-02: Config Layering and Precedence
- **Precondition**: None.
- **Behavior**: Configuration is resolved in this order (later wins):
  1. Built-in defaults (hardcoded in `pkg/config`)
  2. User config (`~/.codesight/config.toml`) — optional, missing file is not an error
  3. Project config (`<project>/.codesight/config.toml`) — optional, missing file is not an error
  4. Environment variables (`CODESIGHT_*`) — highest precedence
- **Invariant**: If no config files exist and no env vars are set, behavior is identical to the current codebase.
- **Env var mapping**:
  | Env Var | Config Key |
  |---------|-----------|
  | `CODESIGHT_EMBEDDING_MODEL` | `embedding.model` |
  | `CODESIGHT_OLLAMA_MAX_INPUT_CHARS` | `embedding.max_input_chars` |
  | `CODESIGHT_GRADLE_JAVA_HOME` | `lsp.java.gradle_java_home` |
  | `CODESIGHT_DB_TYPE` | `db.type` |
  | `CODESIGHT_DB_ADDRESS` | `db.address` |
  | `CODESIGHT_DB_TOKEN` | `db.token` |
  | `CODESIGHT_OLLAMA_HOST` | `embedding.ollama_host` |
  | `CODESIGHT_STATE_DIR` | `state_dir` |

### FR-03: Config Struct
- **Precondition**: None.
- **Behavior**: A `Config` struct in `pkg/config` holds all resolved configuration. All subsystems receive config values from this struct rather than calling `os.Getenv()` directly.
- **Invariant**: The `Config` struct is the single source of truth for runtime configuration after loading completes.

### FR-04: LSP Data in Project Directory
- **Precondition**: `.codesight/` directory exists at project root.
- **Behavior**:
  - jdtls data is stored at `<project>/.codesight/lsp/java/jdtls-data/` instead of `~/.codesight/jdtls-data/<hash>/`.
  - Directories are created on demand with `0o700` permissions.
  - A `.codesight/.gitignore` file containing `lsp/` is created automatically if it does not exist.
- **Fallback**: If `.codesight/` does not exist or the path is not writable, fall back to `~/.codesight/jdtls-data/<hash>/` (current behavior).

### FR-05: `cs init` Command
- **Precondition**: Target path is a valid directory.
- **Behavior**:
  - Creates `.codesight/config.toml` with sections appropriate for the detected project type.
  - Creates `.codesight/.gitignore` with `lsp/` entry.
  - Auto-detection rules:
    - `build.gradle.kts` or `build.gradle` or `pom.xml` present -> include `[lsp.java]` section
    - `go.mod` present -> include `[lsp.go]` section
    - `Cargo.toml` present -> include `[lsp.rust]` section (placeholder)
    - `package.json` present -> include `[lsp.typescript]` section (placeholder)
  - If `.codesight/config.toml` already exists, print a message and do not overwrite.
- **Output format**:
  ```
  Created .codesight/config.toml
  Created .codesight/.gitignore
  ```
  Or if already exists:
  ```
  .codesight/config.toml already exists, skipping
  ```

### FR-06: `cs config` Command
- **Precondition**: Target path is a valid directory.
- **Behavior**:
  - Loads the full layered config for the given path.
  - Prints each effective config key, its value, and its source (one of: `default`, `~/.codesight/config.toml`, `.codesight/config.toml`, env var name).
- **Output format** (one line per key, sorted alphabetically):
  ```
  db.address = localhost:19530 (default)
  embedding.model = nomic-embed-text (.codesight/config.toml)
  lsp.java.gradle_java_home = /usr/lib/jvm/java-17 (CODESIGHT_GRADLE_JAVA_HOME)
  ```

### FR-07: Backward Compatibility
- **Precondition**: Existing `CODESIGHT_*` environment variables are set.
- **Behavior**: All existing env vars continue to work identically. Config files are purely additive. A user who never creates a config file sees zero behavior change.
- **Invariant**: No existing test may break due to the introduction of config file support.

---

## Non-Functional Requirements

### NFR-01: Performance
- Config loading must add < 5ms to CLI startup (TOML parsing of small files is negligible).
- Config is loaded once at startup, not per-operation.

### NFR-02: Security
- Config files must not contain secrets by convention. Documentation should guide users to use env vars for tokens.
- File permissions on `.codesight/lsp/` directories must be `0o700` (user-only).

### NFR-03: Dependency Minimalism
- Only one new direct dependency for TOML parsing.
- Prefer `github.com/BurntSushi/toml` (well-maintained, widely used, stdlib-like API).

---

## Success / Definition of Done

1. `go build ./...` and `go test ./...` pass.
2. All 8 existing `CODESIGHT_*` env vars continue to work.
3. With no config files present, all behavior is identical to pre-change.
4. A project with `.codesight/config.toml` correctly overrides defaults.
5. A user config at `~/.codesight/config.toml` is applied when present.
6. Env vars override both config files.
7. `cs init .` creates appropriate config for detected project types.
8. `cs config .` displays effective configuration with provenance.
9. jdtls data is stored in `.codesight/lsp/java/jdtls-data/` when `.codesight/` exists.
10. Fallback to `~/.codesight/jdtls-data/<hash>/` when `.codesight/` is absent.
