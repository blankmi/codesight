---
name: cs
description: Unified CodeSight workflow for semantic discovery, symbol extraction, and LSP symbol navigation with the `cs` CLI.
---

# CodeSight (`cs`) v2

CodeSight provides unified code intelligence for large repositories: semantic search, AST-based symbol extraction, and LSP-powered cross-file navigation.

## Core Commands

- `cs index <path>`: Index the codebase. Automatically re-indexes if stale (checks HEAD commit and ignore rules).
- `cs search "<query>" --path <dir>`: Semantic natural language search over indexed code.
- `cs extract -f <file-or-dir> -s <symbol>`: Extract a named symbol using tree-sitter AST parsing.
- `cs refs <symbol> --path <dir>`: Find all references to a symbol (falls back to grep if LSP unavailable).
- `cs callers <symbol> --path <dir>`: Trace incoming call hierarchy.
- `cs implements <symbol> --path <dir>`: Find implementations of an interface or type.
- `cs status <path>`: Check if the index is up-to-date or stale.
- `cs clear <path>`: Remove the index for a repository.

## Use by Intent

- Exact lexical lookup → Grep (not cs search)
- Conceptual discovery → `cs search`
- Symbol extraction → `cs extract`
- Cross-file symbol navigation → `cs refs`, `cs callers`, `cs implements`

## Key Flags

- `--path <dir>` — scope to a directory
- `--ext .go,.ts` — filter semantic search results by file extension
- `--limit N` — control number of semantic search results
- `cs extract --format` supports `raw` (default) and `json`
- `cs refs --kind` values: `function|method|class|interface|type|constant`
- `cs callers --depth` default is `1`, must be positive

## Search Patterns

Use natural language queries to find logic without knowing exact symbol names:
- `cs search "authentication middleware logic"`
- `cs search "database connection pool configuration" --ext .go`
- `cs search "error handling in the walker package"`

## Runtime Notes

- `cs refs`, `cs callers`, and `cs implements` run LSP servers as child processes over stdio.
- `cs refs` falls back to grep with a precision note when LSP is unavailable.
- `cs callers` and `cs implements` fail fast with install guidance when LSP binaries are missing.
- Ensure Ollama and Milvus are running for `index`, `search`, `status`, and `clear` commands.

## Guardrails

- Always confirm `cs` results by reading source before final claims.
- Keep Grep as the first choice for exact text lookups.
- Do not read 5+ files to understand a feature — `cs search` ranks them for you.
- Re-index when stale status is reported.
