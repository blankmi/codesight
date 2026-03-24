---
name: cs
description: Use the CodeSight `cs` CLI for code intelligence. `cs <query>` is the default retrieval front door for symbol lookups, references, callers, path discovery, and text search. Falls back to `cs search` for conceptual discovery, `cs extract` for raw symbol bodies, and individual commands when needed. Trigger when tasks need code retrieval, symbol tracing, conceptual discovery, or CodeSight config/runtime diagnosis.
---

# CodeSight (`cs`)

## Repository retrieval policy

1. Use `cs <query>` FIRST for all symbol lookups, references, callers, path discovery, stack-trace lookups, and code searches.
2. DO NOT use `grep`, `find`, `cat`, or `ls -R` for first-pass retrieval when `cs` can answer.
3. Use shell retrieval ONLY if `cs` returns `not_found`, `ambiguous`, or explicitly instructs you to use `grep`/`tail` for large non-code files.
4. Use shell tools for execution tasks only: tests, builds, git, and patching.

## Workflow

1. Resolve the target repository path.
- Use the user's path if provided.
- Otherwise use the current working directory.

2. Start with unified retrieval for any code investigation.
- Symbol lookup: `cs Authenticate`
- References + callers + implements: `cs auth.Login --depth 2`
- Path discovery: `cs pkg/auth.go`
- Text / error search: `cs "connection refused"`
- More context: `cs Authenticate --budget large`

3. Follow `next_hint` in the Meta section of `cs` output for deeper exploration.

4. Fall back to individual commands only when unified retrieval is insufficient.
- "How does X work?" or feature discovery: use `cs search`.
- Raw symbol body: use `cs extract -f <file-or-dir> -s <symbol>`.
- Standalone unranked references: use `cs refs <symbol>`.
- Standalone unranked callers: use `cs callers <symbol>`.
- Standalone implementations: use `cs implements <symbol>`.
- "Set up CodeSight for this repo": use `cs init [path]`.
- "Show me the effective CodeSight config": use `cs config [path]`.
- "Warm or inspect the LSP daemon": use `cs lsp ...`.

5. For conceptual discovery, manage index lifecycle first.
- Run `cs status <path>`.
- If not indexed or stale, run `cs index <path> --branch <branch> --commit <sha>`.
- Use `--force` only when re-index is explicitly needed.
- Treat `cs clear <path>` as destructive.

6. Respect runtime constraints.
- `cs <query>` works locally for symbol, path, and text queries; LSP daemon recommended for references/callers/implements.
- `cs search`, `cs index`, `cs status`, and `cs clear` require Milvus and Ollama.
- `cs extract`, `cs init`, and `cs config` are local filesystem operations.
- `cs refs`, `cs callers`, `cs implements`, and `cs lsp` run language servers as child processes over stdio.
- `cs refs` may fall back to grep when LSP is unavailable.
- `cs callers` and `cs implements` do not fall back to grep.
- `${CODESIGHT_STATE_DIR:-~/.codesight}` persistence is recommended for warm starts, not required for correctness.

## Codex Execution Rule

In sandboxed Codex sessions:
- `cs search`, `cs index`, `cs status`, and `cs clear` usually need network access to reach Milvus or Ollama.
- `cs <query>`, `cs extract`, `cs init`, `cs config`, `cs refs`, `cs callers`, `cs implements`, and `cs lsp` usually stay inside the filesystem and local process sandbox, assuming required binaries are installed.
- If a semantic command fails because of sandboxed network access, rerun it with:
  - `sandbox_permissions: "require_escalated"`
  - `justification`: ask to allow CodeSight to reach the local Milvus or Ollama service

After first approval, request a persistent shell prefix rule for `["cs"]` if the user wants CodeSight commands to run without repeated shell-approval prompts.

## Command Patterns

- Unified retrieval: `cs <query> [--path <dir>] [--depth <n>] [--budget auto|small|medium|large] [--mode auto|symbol|text|ast|path]`
- Discovery: `cs search "<query>" [--path <dir>] [--ext .go,.ts] [--limit <n>]`
- Index status: `cs status [path]`
- Build or refresh index: `cs index [path] [--branch <name>] [--commit <sha>] [--force]`
- Clear index: `cs clear [path]`
- Symbol extraction: `cs extract -f <file-or-dir> -s <symbol> [--format raw|json]`
- References: `cs refs <symbol> [--path <dir>] [--kind function|method|class|interface|type|constant]`
- Incoming callers: `cs callers <symbol> [--path <dir>] [--depth <n>]`
- Implementations: `cs implements <symbol> [--path <dir>]`
- Initialize repo config: `cs init [path]`
- Show effective config: `cs config [path]`
- LSP warmup: `cs lsp warmup [path]`
- LSP status: `cs lsp status [path]`
- LSP restart: `cs lsp restart [path]`
- LSP cleanup: `cs lsp cleanup`

## Guardrails

- Use `cs <query>` as the default first step for any code investigation.
- Follow the `next_hint` in the Meta section for deeper exploration.
- Do not use `grep` or `find` for first-pass code retrieval when `cs` can answer.
- Use `cs extract` instead of broad file reads when the task only needs one symbol's raw body.
- Re-index when repository HEAD changes significantly or when stale status is reported.
- If `cs <query>` returns `not_found`, try alternate phrasing or `cs search` before falling back to shell tools.
- Treat `cs clear` as destructive.
