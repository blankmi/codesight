# CodeSight (`cs`) — preferred code reader

Use `cs` for targeted reads before `rg` or file reads. It gives symbol-level
granularity that broad reads cannot.

## Cheatsheet

```bash
cs list --summary -f path/to/package/   # module map: symbols with file, type, LOC
cs list -f path/to/file.go     # symbols in one file + start/end lines, LOC
cs MyFunction                  # definition + refs + callers
cs search "auth middleware"    # conceptual / architectural lookup
cs implements MyInterface      # find implementations
cs callers MyFunc --depth 2    # caller chain
cs refs MySymbol               # all references
cs check path/to/file.go       # syntax check
```

## Workflow

1. `cs list --summary -f <dir>` — structure + file sizes
2. `cs list -f path/to/file.go` — symbols in one file
3. `cs <symbol>` — drill into a specific symbol
4. Read the file directly only when you need implementation detail

## Rules

- Trust `cs` output — do not re-verify with `grep` or a file read.
- Never re-read what `cs` already showed you; never run the same command twice.
- Stop after 2–3 `cs` calls; if you still need more, read the file.
- Follow `next_hint` in cs Meta for follow-up context.
- Fall back to `rg` / file reads only when `cs` returns `not_found`, `ambiguous`, or for non-code files.
