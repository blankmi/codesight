# Code exploration guidelines

`cs` (CodeSight) is installed. It is a budget-controlled code intelligence CLI.

## Retrieval policy

Use `cs` via Bash as the FIRST tool for any code question.

`cs` already controls its own output size. Run it directly — never pipe through `head`, `tail`, or `grep`:
```bash
# correct
cs refs DateUtil
# wrong — do not truncate cs output
cs refs DateUtil | head -80
```

## Default: use `cs <symbol>`

**Always start with `cs <symbol>`** — it finds the file, extracts the definition, and returns refs + callers in one call. You do NOT need to know the file path.

```bash
# This is almost always the right first call:
cs storeSupplierDeleteList
cs StartPageViewBean
cs DateUtil
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

## CRITICAL: Call limits

- **Maximum 3 cs calls per question.** After 3 calls, switch to Grep/Read.
- After ONE `cs` call for a symbol, STOP and use the result. Do not call `cs` again for the same symbol.
- If you need broader context, use `--depth 2` or `--budget large` on the FIRST call. Do not make a second call.

## Rules

1. Trust cs output — even if the result is small (e.g. "1 references found"), that IS the complete answer. Do not re-query the same symbol for confirmation.
2. Do NOT chain `cs query → cs query` for the same or related symbols.
3. Follow `next_hint` in the Meta section of cs output when you need more context.
4. Fall back to Grep/Read ONLY when cs returns `not_found`, `ambiguous`, or you need non-code files.
