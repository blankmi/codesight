# CodeSight CLI Reference

## Commands

### `cs search <query>`

Semantic discovery over indexed code.

```bash
# Basic discovery
cs search "authentication middleware"

# Search with path context
cs search "database connection" --path /path/to/repo

# Filter by file extensions
cs search "error handling" --path /path/to/repo --ext .go,.ts

# Limit number of results
cs search "retry logic" --path /path/to/repo --limit 5
```

### `cs status [path]`

Check whether the semantic index exists and whether it is stale.

```bash
cs status /path/to/repo
```

### `cs index <path>`

Build or refresh the semantic index used by `cs search`.

```bash
# Index with explicit metadata
cs index /path/to/repo --branch main --commit abc123

# Index current repo
cs index /path/to/repo

# Force re-index even if up to date
cs index /path/to/repo --force
```

### `cs clear [path]`

Remove the semantic index for a repository. This is destructive.

```bash
cs clear /path/to/repo
```

### `cs extract -f <file-or-dir> -s <symbol>`

Extract a named symbol using tree-sitter AST parsing.

```bash
cs extract -f ./pkg/lsp/refs.go -s NormalizeRefKind
cs extract -f ./pkg/lsp/refs.go -s NormalizeRefKind --format json
```

### `cs refs <symbol>`

Find references for a symbol. Falls back to grep if LSP is unavailable.

```bash
cs refs NormalizeRefKind
cs refs NormalizeRefKind --path ./pkg/lsp
cs refs NormalizeRefKind --kind function
```

### `cs callers <symbol>`

Trace incoming callers for a symbol. Requires LSP.

```bash
cs callers runSearch
cs callers runSearch --path ./cmd/cs --depth 2
```

### `cs implements <symbol>`

Find implementations of a type or interface. Requires LSP.

```bash
cs implements Store
cs implements Store --path ./pkg
```

### `cs init [path]`

Create `.codesight/config.toml` and `.codesight/.gitignore` for a repo.

```bash
cs init /path/to/repo
```

### `cs config [path]`

Show effective CodeSight configuration values and their provenance.

```bash
cs config /path/to/repo
```

### `cs lsp ...`

Manage warmed LSP daemons directly.

```bash
cs lsp warmup /path/to/repo
cs lsp status /path/to/repo
cs lsp restart /path/to/repo
cs lsp cleanup
```

## Supported Languages

**AST-aware chunking**:
- Go, TypeScript, JavaScript, Python, Java, Rust, C, C++

**Symbol extraction**:
- Go, TypeScript, JavaScript, Python, Java, Rust, C++, XML, HTML

**LSP navigation**:
- Go, Python, Java, TypeScript/JavaScript, Rust, C/C++

## Examples

```bash
# Search for authentication logic
cs status .
cs index . --branch main --commit "$(git rev-parse HEAD)"
cs search "JWT token validation and refresh" --path .

# Find database migrations in Python files
cs search "database schema migration" --path . --ext .py

# Look for error handling in Go code
cs search "error wrapping and context" --path . --ext .go --limit 20

# Extract one symbol instead of reading the whole file
cs extract -f ./pkg/lsp/refs.go -s NormalizeRefKind

# Inspect effective config
cs config .

# Check daemon status for LSP-backed commands
cs lsp status .
```
