# Repository navigation rules

Use `cs` as the default retrieval front door. Do not default to broad operations.

## Repository retrieval policy

1. Use `cs <query>` FIRST for all symbol lookups, references, callers, path discovery, stack-trace lookups, and code searches.
2. DO NOT use `grep`, `find`, `cat`, or `ls -R` for first-pass retrieval when `cs` can answer.
3. Use shell retrieval ONLY if `cs` returns `not_found`, `ambiguous`, or explicitly instructs you to use `grep`/`tail` for large non-code files.
4. Use shell tools for execution tasks only: tests, builds, git, and patching.

## Unified retrieval

Use `cs <query>` (or `cs query <query>`) as the single entry point:
- symbol lookup: `cs Authenticate`
- references + callers + implements: `cs auth.Login --depth 2`
- path discovery: `cs pkg/auth.go`
- text / error search: `cs "connection refused"`
- broader context: `cs Authenticate --budget large`

`cs` routes internally, fetches ranked evidence, slices definitions to fit the context budget, and returns Markdown. One call replaces multi-step extract → refs → callers chains.

If results are too broad, adjust `--budget` or `--path` instead of processing all results.

## When to use other cs commands

Fall back to individual commands only when the unified query is insufficient:
- `cs search "<question>" --path .` for semantic / conceptual discovery (requires Milvus + Ollama)
- `cs extract -f <file> -s <symbol>` for raw symbol extraction when you need the full body
- `cs refs`, `cs callers`, `cs implements` for standalone navigation when you need unranked, unbudgeted output

## Discipline

Do not:
- use `grep` or `find` for code retrieval when `cs` can answer
- use `cs search` for simple lookups that `cs <query>` can answer directly
- read multiple full files without trying `cs <query>` first
- process large result sets without refining the query

## Routing summary

| Intent | Tool |
|---|---|
| Any code retrieval (first pass) | `cs <query>` |
| Conceptual / architectural discovery | `cs search` |
| Raw symbol body | `cs extract` |
| Shell fallback (after cs says to) | `grep` / `tail` |
| Execution (tests, builds, git) | shell tools |
