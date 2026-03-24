# Code navigation policy

Use `cs` as the default retrieval front door.
Avoid reading full files unless necessary.

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

## When to use other cs commands

Fall back to individual commands only when the unified query is insufficient:
- `cs search "<question>" --path .` for semantic / conceptual discovery (requires Milvus + Ollama)
- `cs extract -f <file> -s <symbol>` for raw symbol extraction when you need the full body
- `cs refs`, `cs callers`, `cs implements` for standalone navigation when you need unranked, unbudgeted output

## Reading discipline

Do NOT:
- open many full files during exploration
- read large files before trying `cs <query>` first
- read files sequentially when they can be batched
- use `grep` or `find` when `cs` can answer the same question

Before reading, identify all needed files first. Batch reads in parallel instead of reading one file at a time.

For any code investigation, follow this order strictly:
1. `cs <query>` to get symbol + ranked references + callers
2. follow `next_hint` if broader context is needed
3. `cs search` for conceptual discovery if the symbol is unknown
4. full file read only if `cs` output is insufficient

## Routing summary

| Intent | Tool |
|---|---|
| Any code retrieval (first pass) | `cs <query>` |
| Conceptual / architectural discovery | `cs search` |
| Raw symbol body | `cs extract` |
| Shell fallback (after cs says to) | `grep` / `tail` |
| Execution (tests, builds, git) | shell tools |
