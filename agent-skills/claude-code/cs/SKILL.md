---
name: cs
description: Unified CodeSight workflow. Use `cs <query>` as the default retrieval front door for symbol lookups, references, callers, path discovery, and text search. Falls back to `cs search` for conceptual discovery, `cs extract` for raw symbol bodies, and individual navigation commands when needed.
allowed-tools: Bash(cs *)
---

# CodeSight (`cs`)

## Repository retrieval policy

1. Use `cs <query>` FIRST for all symbol lookups, references, callers, path discovery, stack-trace lookups, and code searches.
2. DO NOT use `rg`, `grep`, `find`, `cat`, or `ls -R` for first-pass retrieval when `cs` can answer.
3. Use shell retrieval ONLY if `cs` returns `not_found`, `ambiguous`, or explicitly instructs you to use `grep`/`tail` for large non-code files.
4. Use shell tools for execution tasks only: tests, builds, git, and patching.

## Use by intent

| Intent | Tool |
|---|---|
| Any code retrieval (first pass) | `cs <query>` |
| Conceptual / architectural discovery | `cs search` |
| Raw symbol body | `cs extract` |
| Semantic index lifecycle | `cs status`, `cs index`, `cs clear` |
| Project setup / config inspection | `cs init`, `cs config` |
| LSP daemon maintenance | `cs lsp ...` |
| Shell fallback (after cs says to) | `grep` / `tail` |
| Execution (tests, builds, git) | shell tools |

For exact command signatures and examples, read [reference.md](reference.md).

## Quick workflow

1. **For any code investigation**, start with unified retrieval:
   ```bash
   cs Authenticate                       # symbol + refs + callers
   cs auth.Login --depth 2               # deeper caller expansion
   cs pkg/auth.go                        # path discovery
   cs "connection refused"               # text search
   cs Authenticate --budget large        # more context
   ```

2. **For conceptual discovery** (requires Milvus + Ollama), check status first:
   ```bash
   cs status .
   cs index . --branch main --commit "$(git rev-parse HEAD)"
   cs search "your query" --path . --limit 10
   ```

3. **Extract a symbol** when you need the full raw body:
   ```bash
   cs extract -f ./pkg/lsp/refs.go -s NormalizeRefKind
   cs extract -f ./pkg/lsp/refs.go -s NormalizeRefKind --format json
   ```

4. **Use setup and runtime helpers when needed**:
   ```bash
   cs init .
   cs config .
   cs lsp status .
   ```

## Key flags and contracts

- `cs <query> --path <dir>` — scope retrieval to a subdirectory
- `cs <query> --depth <n>` — caller/dependency expansion depth (default 1)
- `cs <query> --budget auto|small|medium|large` — output size target
- `cs <query> --mode auto|symbol|text|ast|path` — override query classification
- `cs search --path <dir>` — scope semantic discovery to a directory
- `--ext .go,.ts` — filter semantic search results by file extension
- `--limit N` — control number of semantic search results
- `cs extract --format` supports exactly `raw` (default) and `json`
- `cs refs --kind` allowed values: `function|method|class|interface|type|constant`
- `cs callers --depth` default is `1` and must be positive
- `cs init` is safe to re-run and leaves an existing `.codesight/config.toml` unchanged
- `cs config` prints effective config values and their provenance
- `cs lsp` supports `warmup`, `status`, `restart`, and `cleanup`

## Runtime notes

- `cs <query>` works locally for symbol, path, and text queries; LSP daemon recommended for references/callers/implements enrichment.
- `cs search`, `cs status`, `cs index`, and `cs clear` require Ollama and Milvus.
- `cs extract`, `cs init`, and `cs config` are local filesystem operations.
- `cs refs`, `cs callers`, `cs implements`, and `cs lsp` run LSP servers as child processes over stdio.
- `${CODESIGHT_STATE_DIR:-~/.codesight}` persistence is recommended for warm starts, not required for correctness.

## Behavioral notes

- Use `cs <query>` as the default first step for any code investigation.
- Follow the `next_hint` in the Meta section of `cs` output for deeper exploration.
- Fall back to `cs search` only for broad conceptual/architectural questions.
- Fall back to `cs extract` only when you need the complete raw body of a symbol.
- Use shell retrieval only when `cs` explicitly instructs you to (e.g., for large non-code files).
- `cs clear` is destructive and should only be used when the index should be removed or reset.
