# Code exploration guidelines

Use `cs` as the default retrieval front door. Minimize full-file reads.

## Repository retrieval policy

1. Use `cs <query>` FIRST for all symbol lookups, references, callers, path discovery, stack-trace lookups, and code searches.
2. DO NOT use `rg`, `grep`, `find`, `cat`, or `ls -R` for first-pass retrieval when `cs` can answer.
3. Use shell retrieval ONLY if `cs` returns `not_found`, `ambiguous`, or explicitly instructs you to use `grep`/`tail` for large non-code files.
4. Use shell tools for execution tasks only: tests, builds, git, and patching.

## Unified retrieval

Use `cs <query>` (or `cs query <query>`) as the single entry point for all code intelligence:
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

## Targeted reading

Do not read full files by default. Instead:
- use `cs <symbol>` to get the definition, references, and callers in one call
- read full files only when surrounding context is required

Do not re-read files you have already seen in this conversation.

## Exploration pattern

For non-trivial tasks:
1. `cs <query>` to get symbol definition + ranked references + callers
2. follow `next_hint` in the Meta section if broader context is needed
3. expand to full-file reads only where necessary

Do not read multiple large files to build understanding.
Do not read a file "just to be sure" if you already have the information you need.

## Routing summary

| Intent | Tool |
|---|---|
| Any code retrieval (first pass) | `cs <query>` |
| Conceptual / architectural discovery | `cs search` |
| Raw symbol body | `cs extract` |
| Shell fallback (after cs says to) | `grep` / `tail` |
| Execution (tests, builds, git) | shell tools |
