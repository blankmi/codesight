---
name: cs
description: Unified CodeSight workflow for semantic discovery (`search`), semantic index lifecycle (`index`, `status`, `clear`), tree-sitter symbol extraction (`extract`), LSP navigation (`refs`, `callers`, `implements`), project config (`init`, `config`), and LSP daemon operations (`lsp`).
---

# CodeSight (`cs`)

CodeSight provides unified code intelligence for large repositories: semantic search, semantic index lifecycle management, AST-based symbol extraction, project config helpers, and LSP-powered cross-file navigation.

## Core Commands

- `cs search "<query>" --path <dir>`: Semantic natural language search over indexed code.
- `cs status [path]`: Check whether the semantic index exists and whether it is stale.
- `cs index [path] [--branch <name>] [--commit <sha>] [--force]`: Build or refresh the semantic index.
- `cs clear [path]`: Remove the semantic index for a repository.
- `cs extract -f <file-or-dir> -s <symbol>`: Extract a named symbol using tree-sitter AST parsing.
- `cs refs <symbol> --path <dir>`: Find all references to a symbol (falls back to grep if LSP unavailable).
- `cs callers <symbol> --path <dir>`: Trace incoming call hierarchy.
- `cs implements <symbol> --path <dir>`: Find implementations of an interface or type.
- `cs init [path]`: Create `.codesight/config.toml` and `.codesight/.gitignore`.
- `cs config [path]`: Show effective CodeSight configuration values and their sources.
- `cs lsp warmup|status|restart [path]`, `cs lsp cleanup`: Manage LSP daemon state.

## Use by Intent

- Exact lexical lookup → Grep (not cs search)
- Conceptual discovery → `cs search`
- Semantic index lifecycle → `cs status`, `cs index`, `cs clear`
- Symbol extraction → `cs extract`
- Cross-file symbol navigation → `cs refs`, `cs callers`, `cs implements`
- Project setup / diagnosis → `cs init`, `cs config`
- LSP daemon maintenance → `cs lsp ...`

`cs search` is the discovery command. `cs index`, `cs status`, and `cs clear` are support commands for the semantic index it uses.

## Key Flags

- `cs search --path <dir>` — scope semantic discovery to a directory
- `--ext .go,.ts` — filter semantic search results by file extension
- `--limit N` — control number of semantic search results
- `cs extract --format` supports `raw` (default) and `json`
- `cs refs --kind` values: `function|method|class|interface|type|constant`
- `cs callers --depth` default is `1`, must be positive
- `cs init` is safe to re-run if config already exists
- `cs config` reports both value and provenance for each config key

## Search Patterns

Use natural language queries to find logic without knowing exact symbol names:
- `cs search "authentication middleware logic"`
- `cs search "database connection pool configuration" --ext .go`
- `cs search "error handling in the walker package"`

## Runtime Notes

- `cs search`, `cs status`, `cs index`, and `cs clear` require Ollama and Milvus.
- `cs extract`, `cs init`, and `cs config` are local filesystem operations.
- `cs refs`, `cs callers`, `cs implements`, and `cs lsp` run LSP servers as child processes over stdio.
- `cs refs` falls back to grep with a precision note when LSP is unavailable.
- `cs callers` and `cs implements` fail fast with install guidance when LSP binaries are missing.
- `${CODESIGHT_STATE_DIR:-~/.codesight}` persistence is recommended for warm starts, not required for correctness.

## Guardrails

- Always confirm `cs` results by reading source before final claims.
- Keep Grep as the first choice for exact text lookups.
- Use `cs status` and `cs index` to keep semantic discovery current.
- Treat `cs clear` as destructive.
- Do not read 5+ files to understand a feature — `cs search` ranks them for you.
- Prefer `cs extract` over full-file reads when only one symbol is needed.
