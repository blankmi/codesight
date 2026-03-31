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
| Fast syntax-error feedback for one or more files | `cs check <path> [path...]` |


Do NOT use `cs extract` unless you already have the file path from a previous cs call. Use `cs <symbol>` instead.

## CRITICAL: Do not loop

- After the first `cs <symbol>` call, use the result directly. Do not chain repeated guessed-symbol `cs` calls for the same question.
- If `cs` already gave you the file, definition, refs, or callers you need, stop there. Do not re-open that same file with `sed`, `nl`, `cat`, or `rg`.
- If you already know the file or the symptom is file-driven, use `cs extract` with the known path only when `cs` did not already show the required source lines.
- For code files, prefer another targeted `cs` call (`cs extract`, `cs refs`, `cs callers`, `cs search`) over `sed` or `rg`.
- If you need more source from a known code file, use another `cs extract -f <file> -s <symbol>` call instead of `sed` or `cat`.
- If you need broader context, use `--depth 2` or `--budget large` on the FIRST call.
- Do not switch from `cs` to `sed` or `rg` just because you already made a few `cs` calls.

## Rules

1. Trust cs output. Do not re-verify with grep or file reads.
2. Follow `next_hint` in the Meta section of cs output when you need more context.
3. Fall back to `rg`/file reads ONLY when `cs` returns `not_found`, `ambiguous`, or you need non-code files.
