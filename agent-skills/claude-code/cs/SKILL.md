---
name: cs
description: Semantic code search using CodeSight. Use when searching for code by meaning rather than exact text — e.g. "authentication middleware", "database connection pool", "error handling". Requires the `cs` CLI to be installed.
allowed-tools: Bash(cs *)
---

# CodeSight Semantic Search

Use the `cs` CLI for natural-language code search over indexed repositories.

## Quick workflow

1. **Check status** before searching:
   ```bash
   cs status .
   ```

2. **Index** if needed:
   ```bash
   cs index .
   ```

3. **Search** with natural language:
   ```bash
   cs search "your query" --path . --limit 10
   ```

## Key flags

- `--path <dir>` — scope search to a directory
- `--ext .go,.ts` — filter by file extensions
- `--limit N` — control number of results (default varies)

## Search strategy

- Start with broad semantic queries, then narrow with `--ext` and `--limit`
- Use domain-specific terms: "authentication middleware" not "auth", "database connection pool" not "db"
- Add technical qualifiers: "handler", "middleware", "interface", "migration", "serializer"

## Output format

Results show file path, line range, match score (0-1), and a code snippet:

```
[1] internal/auth/middleware.go:42-86 (score: 0.87)
    function — func AuthMiddleware(next http.Handler) http.Handler {
```

For complete CLI reference including all commands and options, see [reference.md](reference.md).
