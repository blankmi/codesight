# codesight (`cs`)

Semantic code search for large codebases. Indexes source code using AST-aware chunking and embeddings, stores vectors in Milvus, and provides fast natural-language search over code.

Built for AI coding agents that burn tokens on exploratory grep/glob searches. Give them `cs search` instead.

## How it works

1. **Walk** — traverses the repo respecting `.gitignore`
2. **Split** — extracts functions, classes, methods, and types using tree-sitter AST parsing (falls back to line-based chunking for unsupported languages)
3. **Embed** — generates vectors via Ollama (default: `nomic-embed-text`)
4. **Store** — inserts chunks + vectors into Milvus
5. **Search** — embeds a natural-language query and returns the most relevant code chunks

## Supported languages

AST-aware chunking: **Go, TypeScript, JavaScript, Python, Java, Rust, C, C++**

All other languages get line-based chunking with overlap.

## Quick start

### Prerequisites

- [Ollama](https://ollama.com) running locally with an embedding model
- [Milvus](https://milvus.io) (standalone Docker container)

```bash
# Start Milvus
docker run -d --name milvus-standalone \
  -p 19530:19530 -p 9091:9091 \
  -e DEPLOY_MODE=STANDALONE \
  -e ETCD_USE_EMBED=true \
  -e ETCD_DATA_DIR=/var/lib/milvus/etcd \
  -e COMMON_STORAGETYPE=local \
  -v milvus_data:/var/lib/milvus \
  milvusdb/milvus:v2.6.4 milvus run standalone

# Pull embedding model
ollama pull nomic-embed-text
```

### Build

```bash
make build
# binary at bin/cs
```

### Test

```bash
# unit tests
make test

# integration test (starts Milvus in Docker, indexes fixture code, runs real queries)
make test-integration

# or run the script directly
./scripts/test-integration.sh

# keep the container for debugging
CODESIGHT_KEEP_MILVUS_CONTAINER=1 ./scripts/test-integration.sh
```

### Index a codebase

```bash
cs index /path/to/repo --branch main --commit $(git rev-parse HEAD)
```

### Search

```bash
cs search "authentication middleware"
cs search "database connection pool" --ext .go
cs search "error handling" --limit 5
```

### Check status

```bash
cs status /path/to/repo
```

### Clear index

```bash
cs clear /path/to/repo
```

## Output format

Search results are plain text, optimized for minimal token usage:

```
[1] internal/auth/middleware.go:42-86 (score: 0.87)
    function — func AuthMiddleware(next http.Handler) http.Handler {

[2] internal/db/pool.go:15-52 (score: 0.82)
    function — func NewConnectionPool(cfg PoolConfig) (*Pool, error) {
```

## Configuration

All configuration is via environment variables:

| Variable                       | Description          | Default                  |
|--------------------------------|----------------------|--------------------------|
| `CODESIGHT_DB_TYPE`            | Vector store backend | `milvus`                 |
| `CODESIGHT_DB_ADDRESS`         | Milvus address       | `localhost:19530`        |
| `CODESIGHT_DB_TOKEN`           | Milvus auth token    | (empty)                  |
| `CODESIGHT_EMBEDDING_PROVIDER` | Embedding provider   | `ollama`                 |
| `CODESIGHT_EMBEDDING_MODEL`    | Embedding model name | `nomic-embed-text`       |
| `CODESIGHT_OLLAMA_HOST`        | Ollama endpoint      | `http://127.0.0.1:11434` |

## Architecture

```
pkg/
├── vectorstore/
│   ├── store.go          # Store interface
│   └── milvus.go         # Milvus implementation
├── embedding/
│   ├── embedding.go      # Provider interface
│   └── ollama.go         # Ollama HTTP client
├── splitter/
│   ├── splitter.go       # Splitter interface + Chunk type
│   ├── treesitter.go     # AST-aware splitter
│   └── fallback.go       # Line-based fallback
├── indexer.go            # Indexing pipeline
├── searcher.go           # Search pipeline
├── version.go            # Index versioning + staleness
└── walker.go             # .gitignore-aware file walker
```

Packages are under `pkg/` (not `internal/`) so other tools can import codesight as a library.

## Index lifecycle

- **One index per project** — collection name is `cs_{sha256(abs_path)[:8]}`
- **Re-index on stale** — compares indexed commit SHA with current HEAD
- **Clean slate** — re-indexing drops and recreates the collection (no stale chunks from deleted files)
- `cs status` reports staleness so agents know the index may be slightly behind

## Agent Integration

Pre-built skill files for popular coding agents are in `agent-skills/`:

| Agent       | File                                   | How to install                                                                                          |
|-------------|----------------------------------------|---------------------------------------------------------------------------------------------------------|
| Claude Code | `agent-skills/claude-code/cs/SKILL.md` | Copy `agent-skills/claude-code/cs` into `.claude/skills/cs` (project) or `~/.claude/skills/cs` (global) |
| Gemini      | `agent-skills/gemini/gemini.skill`     | `gemini skills install agent-skills/gemini/gemini.skill --scope workspace`                              |
| Codex       | `agent-skills/codex/cs/SKILL.md`       | Copy `agent-skills/codex/cs` into `$CODEX_HOME/skills/cs`                                               |

> [!IMPORTANT]
> These skills require project-specific setup (indexing the source code). They should be installed per-project rather than globally to ensure the agent uses the correct index for the current codebase.
