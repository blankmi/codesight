# CodeSight CLI Reference

## Commands

### `cs index <path>`

Index a repository for semantic search.

```bash
# Index with auto-detected git info
cs index /path/to/repo

# Specify branch and commit
cs index /path/to/repo --branch main --commit abc123

# Force re-index even if up to date
cs index /path/to/repo --force
```

### `cs search <query>`

Search indexed code with natural language.

```bash
# Basic search
cs search "authentication middleware"

# Search with path context
cs search "database connection" --path /path/to/repo

# Filter by file extensions
cs search "error handling" --ext .go,.ts

# Limit number of results
cs search "retry logic" --limit 5

# Combine options
cs search "JWT validation" --path /app --ext .js,.ts --limit 10
```

### `cs status <path>`

Check if a repository is indexed and up to date.

```bash
cs status /path/to/repo
```

### `cs clear <path>`

Remove the index for a repository.

```bash
cs clear /path/to/repo
```

## Supported Languages

**AST-aware chunking** (better accuracy):
- Go, TypeScript, JavaScript, Python, Java, Rust, C, C++

**Line-based chunking** (all other languages):
- Uses overlapping line windows for context

## Examples

```bash
# Index current project
cs index .

# Search for authentication logic
cs search "JWT token validation and refresh"

# Find database migrations in Python files
cs search "database schema migration" --ext .py

# Look for error handling in Go code
cs search "error wrapping and context" --ext .go --limit 20

# Check if re-indexing needed
cs status .

# Clear and re-index
cs clear . && cs index . --force
```
