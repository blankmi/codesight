---
name: cs
description: Use the CodeSight `cs` CLI for unified code intelligence: semantic discovery (`search`), symbol extraction (`extract`), and LSP navigation (`refs`, `callers`, `implements`). Trigger when tasks need fast repo discovery without broad file reads, deterministic symbol extraction, or cross-file symbol tracing.
---

# Cs

## Overview

Use `cs` to route by intent instead of defaulting to broad file reads:
- lexical lookup -> Grep
- conceptual discovery -> `cs search`
- symbol extraction -> `cs extract`
- references/call hierarchy/implementations -> `cs refs` / `cs callers` / `cs implements`

In sandboxed Codex sessions, only commands that talk to Milvus/Ollama (`index/search/status/clear`) usually require network escalation.

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

3. For conceptual discovery, check index state first.
- Run `cs status <path>`.
- If not indexed or stale, run `cs index <path> --branch <branch> --commit <sha>`.
- Use `--force` only when re-index is explicitly needed.

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

7. Respect LSP runtime constraints for navigation commands.
- `cs refs`, `cs callers`, and `cs implements` run language servers as child processes over stdio.
- No remote/TCP LSP mode is supported.
- `${CODESIGHT_STATE_DIR:-~/.codesight}` persistence is recommended for warm starts, not required for correctness.
- `host.docker.internal` is for Milvus/Ollama endpoints, not LSP transport.

## Codex Execution Rule

When invoking `cs index`, `cs search`, `cs status`, or `cs clear` in sandboxed Codex sessions, set:
- `sandbox_permissions: "require_escalated"`
- `justification`: ask to allow network access for CodeSight DB queries

After first approval, request persistent prefix rules to avoid repeated prompts:
- `["cs", "search"]`
- `["cs", "status"]`
- Optional: `["cs", "index"]`, `["cs", "clear"]`

## Command Patterns

- Check status: `cs status <path>`
- Build or refresh index: `cs index <path> --branch <branch> --commit <sha>`
- Semantic search: `cs search "<query>" --path <path> --limit 10`
- Narrow by language: `cs search "<query>" --path <path> --ext .go,.ts`
- Symbol extraction: `cs extract -f <file-or-dir> -s <symbol> [--format raw|json]`
- References: `cs refs <symbol> [--path <dir>] [--kind function|method|class|interface|type|constant]`
- Incoming callers: `cs callers <symbol> [--path <dir>] [--depth <n>]`
- Implementations: `cs implements <symbol> [--path <dir>]`
- Clear index: `cs clear <path>`

## Guardrails

- Always confirm `cs` results by reading source before final claims.
- Keep Grep as the first choice for exact text lookups.
- In this repository, use `cs extract` as the default symbol extraction path.
- Re-index when repository HEAD changes significantly or when stale status is reported.
- If `cs search` returns no useful hits, retry with alternate wording before falling back to manual tree-wide scans.
- `cs refs` may fallback to grep with a precision note when LSP is unavailable.
- `cs callers` and `cs implements` do not fallback to grep; they fail fast with install guidance when LSP binaries are missing.
