# CodeSight (`cs`) — code intelligence CLI

## Quick reference

```bash
cs list -f path/to/package/           # module map: all symbols with file, type, LOC
cs list -f path/to/file.go            # symbols in one file
cs MyFunction                         # definition + refs + callers (no file path needed)
cs search "auth middleware" --path .   # conceptual / architectural question
cs extract -f path/to/file.go -s Func # re-extract from a known file
cs implements MyInterface              # find implementations
cs callers MyFunc --depth 2            # caller chain
cs refs MySymbol                       # all references
cs check path/to/file.go              # syntax-error check
```

## Workflow example

```bash
cs list -f internal/orchestrator/                    # 1. see structure + file sizes
cs Orchestrator                                      # 2. targeted symbol detail
sed -n '1,200p' internal/orchestrator/orchestrator.go # 3. read files for implementation
```

## Do

- Use `cs list -f <dir>` first to see module structure and file sizes.
- Use `cs <symbol>` for targeted symbol questions.
- Read files directly for broad understanding or implementation.

## Don't

- Don't chain `cs` calls — if you need more after 2-3 calls, read the file.
- Don't re-read what `cs` already showed you.
