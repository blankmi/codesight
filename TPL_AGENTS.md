# Code exploration guidelines

`cs` (CodeSight) is installed. It is a budget-controlled code intelligence CLI.

## Retrieval policy

Use `cs` as the FIRST tool for any code question. Do not use `grep`, `find`, `cat`, or `rg` for initial discovery.

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

## CRITICAL: Do not loop

- After ONE `cs <symbol>` call, STOP and use the result. Do not call `cs` again for the same or related symbols.
- If you need broader context, use `--depth 2` or `--budget large` on the FIRST call.
- **Maximum 3 cs calls per question.** If you haven't found the answer in 3 calls, switch to `rg` or file reads.

## Rules

1. Trust cs output. Do not re-verify with grep or file reads.
2. Follow `next_hint` in the Meta section of cs output when you need more context.
3. Fall back to `rg`/file reads ONLY when cs returns `not_found`, `ambiguous`, or you need non-code files.
