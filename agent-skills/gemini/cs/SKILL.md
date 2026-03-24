---
name: cs
description: Unified CodeSight workflow. Use `cs <query>` as the default retrieval front door for symbol lookups, references, callers, path discovery, and text search. Falls back to `cs search` for conceptual discovery, `cs extract` for raw symbol bodies, and individual navigation commands when needed.
---

# CodeSight (`cs`)

CodeSight provides unified code intelligence for large repositories. `cs <query>` is the single-call retrieval front door that routes, ranks, and budgets code intelligence — replacing multi-step retrieval.

## Repository retrieval policy

1. Use `cs <query>` FIRST for all symbol lookups, references, callers, path discovery, stack-trace lookups, and code searches.
2. DO NOT use `grep`, `find`, `cat`, or `ls -R` for first-pass retrieval when `cs` can answer.
3. Use shell retrieval ONLY if `cs` returns `not_found`, `ambiguous`, or explicitly instructs you to use `grep`/`tail` for large non-code files.
4. Use shell tools for execution tasks only: tests, builds, git, and patching.

## Core Commands

- `cs <query> [--path <dir>] [--depth <n>] [--budget auto|small|medium|large] [--mode auto|symbol|text|ast|path]`: Unified retrieval front door.
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

| Intent | Tool |
|---|---|
| Any code retrieval (first pass) | `cs <query>` |
| Conceptual / architectural discovery | `cs search` |
| Raw symbol body | `cs extract` |
| Semantic index lifecycle | `cs status`, `cs index`, `cs clear` |
| Project setup / diagnosis | `cs init`, `cs config` |
| LSP daemon maintenance | `cs lsp ...` |
| Shell fallback (after cs says to) | `grep` / `tail` |
| Execution (tests, builds, git) | shell tools |

## Unified Retrieval Examples

```bash
# Symbol lookup with references and callers
cs Authenticate

# Deeper caller expansion with more context
cs Authenticate --depth 2 --budget large

# Path discovery
cs pkg/auth.go

# Text / error search
cs "connection refused"

# Explicit mode override
cs query Authenticate --mode symbol
```

## Key Flags

- `cs <query> --path <dir>` — scope retrieval to a subdirectory
- `cs <query> --depth <n>` — caller/dependency expansion depth (default 1)
- `cs <query> --budget auto|small|medium|large` — output size target (default auto)
- `cs <query> --mode auto|symbol|text|ast|path` — override query classification (default auto)
- `cs search --ext .go,.ts` — filter semantic search results by file extension
- `cs search --limit N` — control number of semantic search results
- `cs extract --format` supports `raw` (default) and `json`
- `cs refs --kind` values: `function|method|class|interface|type|constant`
- `cs callers --depth` default is `1`, must be positive

## Runtime Notes

- `cs <query>` works locally for symbol, path, and text queries; LSP daemon recommended for references/callers/implements.
- `cs search`, `cs status`, `cs index`, and `cs clear` require Ollama and Milvus.
- `cs extract`, `cs init`, and `cs config` are local filesystem operations.
- `cs refs`, `cs callers`, `cs implements`, and `cs lsp` run LSP servers as child processes over stdio.
- `cs refs` falls back to grep with a precision note when LSP is unavailable.
- `cs callers` and `cs implements` fail fast with install guidance when LSP binaries are missing.
- `${CODESIGHT_STATE_DIR:-~/.codesight}` persistence is recommended for warm starts, not required for correctness.

## Guardrails

- Use `cs <query>` as the default first step for any code investigation.
- Follow the `next_hint` in the Meta section for deeper exploration.
- Do not use `grep` or `find` for first-pass code retrieval when `cs` can answer.
- Do not use `cs search` for simple lookups that `cs <query>` can answer directly.
- Use `cs status` and `cs index` to keep semantic discovery current.
- Treat `cs clear` as destructive.
- Prefer `cs extract` over full-file reads when only one symbol's raw body is needed.
