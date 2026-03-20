---
name: cs
description: Use the CodeSight `cs` CLI for semantic discovery (`search`), semantic index lifecycle (`index`, `status`, `clear`), tree-sitter symbol extraction (`extract`), LSP navigation (`refs`, `callers`, `implements`), project config (`init`, `config`), and LSP daemon operations (`lsp`). Trigger when tasks need conceptual repo discovery, targeted symbol reads, cross-file symbol tracing, or CodeSight config/runtime diagnosis.
---

# CodeSight (`cs`)

Use `cs` by intent instead of defaulting to broad file reads:
- Exact text, identifiers, file names, and lexical patterns -> Grep
- Conceptual discovery -> `cs search`
- Semantic index lifecycle -> `cs status`, `cs index`, `cs clear`
- Symbol extraction -> `cs extract`
- Cross-file symbol navigation -> `cs refs`, `cs callers`, `cs implements`
- Project setup and config inspection -> `cs init`, `cs config`
- LSP daemon maintenance -> `cs lsp warmup`, `cs lsp status`, `cs lsp restart`, `cs lsp cleanup`

`cs search` is the discovery command. `cs index`, `cs status`, and `cs clear` manage the semantic index that `cs search` depends on.

## Workflow

1. Resolve the target repository path.
- Use the user's path if provided.
- Otherwise use the current working directory.

2. Choose command by task intent.
- Exact text / identifier / pattern match: use Grep (not `cs search`).
- "How does X work?" or feature discovery: use `cs search`.
- "Show symbol Y": use `cs extract -f <file-or-dir> -s <symbol>`.
- "Where is symbol Y referenced?": use `cs refs <symbol>`.
- "Who calls symbol Y?": use `cs callers <symbol>`.
- "Who implements type/interface Y?": use `cs implements <symbol>`.
- "Set up CodeSight for this repo": use `cs init [path]`.
- "Show me the effective CodeSight config": use `cs config [path]`.
- "Warm or inspect the LSP daemon": use `cs lsp ...`.

3. For conceptual discovery, manage index lifecycle first.
- Run `cs status <path>`.
- If not indexed or stale, run `cs index <path> --branch <branch> --commit <sha>`.
- Use `--force` only when re-index is explicitly needed.
- Treat `cs clear <path>` as destructive. Only use it when the user explicitly wants the semantic index removed or reset.

4. Search with a broad query, then narrow.
- Start with `cs search "<natural language query>" --path <path>`.
- Add `--ext` for language narrowing (example: `.go,.ts`).
- Tune `--limit` based on task breadth.

5. Iterate query phrasing using intent terms.
- Prefer domain terms over filenames (for example: `retry backoff`, `jwt middleware`, `database transaction rollback`).
- If results are too broad, add technical qualifiers (`handler`, `interface`, `middleware`, `migration`, `serializer`).

6. Validate findings in source files before answering.
- Open the top result files and confirm behavior.
- Return concise references with file and line ranges.

7. Respect runtime constraints.
- `cs search`, `cs index`, `cs status`, and `cs clear` require Milvus and Ollama.
- `cs extract`, `cs init`, and `cs config` are local filesystem operations.
- `cs refs`, `cs callers`, `cs implements`, and `cs lsp` run language servers as child processes over stdio.
- `cs refs` may fall back to grep when LSP is unavailable.
- `cs callers` and `cs implements` do not fall back to grep.
- No remote or TCP LSP mode is supported.
- `${CODESIGHT_STATE_DIR:-~/.codesight}` persistence is recommended for warm starts, not required for correctness.

## Codex Execution Rule

In sandboxed Codex sessions:
- `cs search`, `cs index`, `cs status`, and `cs clear` usually need network access to reach Milvus or Ollama.
- `cs extract`, `cs init`, `cs config`, `cs refs`, `cs callers`, `cs implements`, and `cs lsp` usually stay inside the filesystem and local process sandbox, assuming required binaries are installed.
- If a semantic command fails because of sandboxed network access, rerun it with:
  - `sandbox_permissions: "require_escalated"`
  - `justification`: ask to allow CodeSight to reach the local Milvus or Ollama service

After first approval, request a persistent shell prefix rule for `["cs"]` if the user wants CodeSight commands to run without repeated shell-approval prompts.

## Command Patterns

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

- Always confirm `cs` results by reading source before final claims.
- Keep Grep as the first choice for exact text lookups.
- Use `cs extract` instead of broad file reads when the task only needs one symbol.
- Re-index when repository HEAD changes significantly or when stale status is reported.
- If `cs search` returns no useful hits, retry with alternate wording before falling back to manual tree-wide scans.
- Treat `cs clear` as destructive.
- `cs refs` may fall back to grep with a precision note when LSP is unavailable.
- `cs callers` and `cs implements` fail fast with install guidance when LSP binaries are missing.
