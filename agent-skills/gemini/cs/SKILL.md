---
name: cs
description: Semantic code search using CodeSight (`cs`). Use when you need to search for code by meaning, find relevant functions or classes, or understand how specific features are implemented in a large codebase.
---

# CodeSight (`cs`)

CodeSight provides semantic code search for large repositories by indexing source code using AST-aware chunking and vector embeddings.

## Core Commands

- `cs index <path>`: Index the codebase. Automatically re-indexes if the index is stale (checks HEAD commit).
- `cs search "<query>"`: Perform a natural language search over the indexed code.
- `cs status <path>`: Check if the index is up-to-date or stale.
- `cs clear <path>`: Remove the index for a repository.

## Search Options

- `--ext <extension>`: Filter search results by file extension (e.g., `--ext .go`).
- `--limit <number>`: Limit the number of search results (default is usually 10).

## Effective Search Patterns

Use natural language queries to find logic without knowing exact symbol names:
- `cs search "authentication middleware logic"`
- `cs search "database connection pool configuration" --ext .go`
- `cs search "error handling in the walker package"`

## Troubleshooting

- **Stale Index**: If results seem outdated, run `cs status .`. If it reports stale, run `cs index .`.
- **Backend**: Ensure Ollama and Milvus are running as described in the project documentation.
