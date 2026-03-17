---
name: cs
description: Unified CodeSight workflow for semantic discovery, symbol extraction, and LSP symbol navigation with the `cs` CLI.
allowed-tools: Bash(cs *)
---

# CodeSight (`cs`) v2

Use `cs` by intent:
- Exact lexical lookup -> Grep
- Conceptual discovery -> `cs search`
- Symbol extraction -> `cs extract`
- Cross-file symbol navigation -> `cs refs`, `cs callers`, `cs implements`

## Quick workflow

1. **For conceptual discovery**, check status first:
   ```bash
   cs status .
   ```

2. **Index** if needed:
   ```bash
   cs index .
   ```

3. **Search** with natural language:
   ```bash
   cs search "your query" --path . --limit 10
   ```

4. **Extract a symbol** (recommended extraction path in this repository):
   ```bash
   cs extract -f ./pkg/lsp/refs.go -s NormalizeRefKind
   cs extract -f ./pkg/lsp/refs.go -s NormalizeRefKind --format json
   ```

5. **Navigate by symbol**:
   ```bash
   cs refs NormalizeRefKind --path . --kind function
   cs callers runSearch --path . --depth 2
   cs implements Store --path .
   ```

## Key flags and contracts

- `--path <dir>` — scope search to a directory
- `--ext .go,.ts` — filter semantic search results by file extension
- `--limit N` — control number of semantic search results
- `cs extract --format` supports exactly `raw` (default) and `json`
- `cs refs --kind` allowed values: `function|method|class|interface|type|constant`
- `cs callers --depth` default is `1` and must be positive

## Runtime notes for LSP commands

- `cs refs`, `cs callers`, and `cs implements` run LSP servers as child processes over stdio.
- No remote/TCP LSP mode is supported in v2.
- `${CODESIGHT_STATE_DIR:-~/.codesight}` persistence is recommended for warm starts, not required for correctness.
- `host.docker.internal` is for Milvus/Ollama access, not LSP transport.

## Behavioral notes

- Keep Grep as the preferred tool for exact text/identifier matching.
- In this repository, prefer `cs extract` over standalone `symgrep extract`.
- `cs refs` can fallback to grep with a precision note when LSP is unavailable.
- `cs callers` and `cs implements` do not fallback to grep; they fail fast with install guidance if LSP binaries are missing.
