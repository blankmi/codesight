---
name: cs
description: Use the Codesight `cs` CLI to index and semantically search code in any repository. Trigger when tasks require fast code discovery, natural-language code search, narrowing results by extension, checking index freshness, or reducing exploratory grep/glob scans.
---

# Cs

## Overview

Use `cs` as the primary discovery tool for large codebases. Index once, search iteratively, and return precise file/line references.

## Workflow

1. Resolve the target repository path.
- Use the user's path if provided.
- Otherwise use the current working directory.

2. Check index state first.
- Run `cs status <path>`.
- If not indexed or stale, run `cs index <path> --branch <branch> --commit <sha>`.
- Use `--force` only when re-index is explicitly needed.

3. Search with a broad query, then narrow.
- Start with `cs search "<natural language query>" --path <path>`.
- Add `--ext` for language narrowing (example: `.go,.ts`).
- Tune `--limit` based on task breadth.

4. Iterate query phrasing using intent terms.
- Prefer domain terms over filenames (for example: `retry backoff`, `jwt middleware`, `database transaction rollback`).
- If results are too broad, add technical qualifiers (`handler`, `interface`, `middleware`, `migration`, `serializer`).

5. Validate findings in source files before answering.
- Open the top result files and confirm behavior.
- Return concise references with file and line ranges.

## Command Patterns

- Check status: `cs status <path>`
- Build or refresh index: `cs index <path> --branch <branch> --commit <sha>`
- Semantic search: `cs search "<query>" --path <path> --limit 10`
- Narrow by language: `cs search "<query>" --path <path> --ext .go,.ts`
- Clear index: `cs clear <path>`

## Guardrails

- Always confirm `cs` results by reading source before final claims.
- Prefer `cs search` as the first-pass discovery tool; use grep-style search for exact tokens only after semantic narrowing.
- Re-index when repository HEAD changes significantly or when stale status is reported.
- If `cs search` returns no useful hits, retry with alternate wording before falling back to manual tree-wide scans.
