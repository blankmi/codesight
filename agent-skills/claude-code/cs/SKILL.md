---
name: cs
description: "Budget-controlled code intelligence CLI. Always start with `cs <symbol>` — it finds the file, extracts the definition, and returns refs + callers in one call. Use specialized subcommands (cs implements, cs refs, cs callers) for specific needs. Do NOT use cs extract unless you already know the file path."
allowed-tools: Bash(cs *)
---

# CodeSight (`cs`)

## Retrieval policy

Use `cs` as the FIRST tool for any code question. Do not use `rg`, `grep`, `find`, or `cat` for initial discovery.

`cs` already controls its own output size. Run it directly — never pipe through `head`, `tail`, or `grep`.

## Default: use `cs <symbol>`

**Always start with `cs <symbol>`** — it finds the file, extracts the definition, and returns refs + callers in one call. You do NOT need to know the file path.

```bash
cs storeSupplierDeleteList    # finds file, shows definition + refs + callers
cs StartPageViewBean          # same — no file path needed
```

## Specialized subcommands (only when needed)

| Need | Command |
|---|---|
| Interface/abstract implementations | `cs implements <type>` |
| All references to a symbol | `cs refs <symbol>` |
| Caller chain (who calls X) | `cs callers <symbol> --depth 2` |
| Re-extract from a KNOWN file path | `cs extract -f <file> -s <symbol>` |
| Conceptual / architectural question | `cs search "<question>" --path .` |
| Need more context on a symbol | `cs <symbol> --depth 2 --budget large` |

Do NOT use `cs extract` unless you already have the file path from a previous cs call. Use `cs <symbol>` instead.

For full command reference, read [reference.md](reference.md).

## STOP AFTER EACH CALL

cs output is ranked and budgeted. After each call:

1. READ the result. It contains the definition, references, and callers you need.
2. ANSWER the question if the result is sufficient. Do NOT make another cs call.
3. Only make a follow-up call if the result says `not_found` or `ambiguous`, or you need a DIFFERENT symbol.

**Maximum 3 cs calls per question.** If 3 calls haven't answered the question, switch to `rg` or file reads.

Do NOT:
- Use `cs extract` without a known file path — use `cs <symbol>` instead
- Chain `cs query → cs query` for the same or related symbols
- Pipe cs output through `head` or `tail`

## Key flags

- `--depth <n>` — caller expansion depth (default 1)
- `--budget auto|small|medium|large` — output size target
- `--path <dir>` — scope to a subdirectory
- `--mode auto|symbol|text|ast|path` — override query classification

## Fallback

Use `rg` or file reads ONLY when:
- cs returns `not_found` or `ambiguous`
- cs output explicitly says to use shell tools
- You need non-code files (configs, logs, docs)
