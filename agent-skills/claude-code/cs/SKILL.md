---
name: cs
description: Unified CodeSight workflow for semantic discovery (`search`), semantic index lifecycle (`index`, `status`, `clear`), tree-sitter symbol extraction (`extract`), LSP navigation (`refs`, `callers`, `implements`), project config (`init`, `config`), and LSP daemon operations (`lsp`).
allowed-tools: Bash(cs *)
---

# CodeSight (`cs`)

Use `cs` by intent:
- Exact lexical lookup -> Grep
- Conceptual discovery -> `cs search`
- Semantic index lifecycle -> `cs status`, `cs index`, `cs clear`
- Symbol extraction -> `cs extract`
- Cross-file symbol navigation -> `cs refs`, `cs callers`, `cs implements`
- Project setup / config inspection -> `cs init`, `cs config`
- LSP daemon maintenance -> `cs lsp ...`

`cs search` is the discovery command. `cs index`, `cs status`, and `cs clear` manage the index that `cs search` depends on.

For exact command signatures and examples, read [reference.md](reference.md).

## Quick workflow

1. **For conceptual discovery**, check status first:
   ```bash
   cs status .
   ```

2. **Index** if needed:
   ```bash
   cs index . --branch main --commit "$(git rev-parse HEAD)"
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

6. **Use setup and runtime helpers when needed**:
   ```bash
   cs init .
   cs config .
   cs lsp status .
   ```

## Key flags and contracts

- `cs search --path <dir>` — scope semantic discovery to a directory
- `--ext .go,.ts` — filter semantic search results by file extension
- `--limit N` — control number of semantic search results
- `cs extract --format` supports exactly `raw` (default) and `json`
- `cs refs --kind` allowed values: `function|method|class|interface|type|constant`
- `cs callers --depth` default is `1` and must be positive
- `cs init` is safe to re-run and leaves an existing `.codesight/config.toml` unchanged
- `cs config` prints effective config values and their provenance
- `cs lsp` supports `warmup`, `status`, `restart`, and `cleanup`

## Runtime notes for LSP commands

- `cs refs`, `cs callers`, `cs implements`, and `cs lsp` run LSP servers as child processes over stdio.
- No remote or TCP LSP mode is supported.
- `${CODESIGHT_STATE_DIR:-~/.codesight}` persistence is recommended for warm starts, not required for correctness.

## Behavioral notes

- Keep Grep as the preferred tool for exact text/identifier matching.
- `cs search` depends on the semantic index; use `cs status` and `cs index` to keep it current.
- `cs clear` is destructive and should only be used when the index should be removed or reset.
- Prefer `cs extract` over full-file reads when only one symbol is needed.
- `cs refs` can fallback to grep with a precision note when LSP is unavailable.
- `cs callers` and `cs implements` do not fallback to grep; they fail fast with install guidance if LSP binaries are missing.
