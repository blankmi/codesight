# codesight (`cs`)

Semantic code search for large codebases. Indexes source code using AST-aware chunking and embeddings, stores vectors in Milvus, and provides fast natural-language search over code.

> **Benchmark note:** A/B testing (88 agent invocations, codebases up to 250K LOC, Sonnet 4.6) showed `cs search` saves **14.5% tokens on conceptual queries** by surfacing relevant files from the semantic index instead of agents reading 30+ files blind. 
> Grep already handles lexical/reference search optimally — `cs search` fills the gap Grep can't: "how does X work?" across a large codebase. Shorter instructions (7 lines) outperformed verbose ones (29 lines) by 15 percentage points.

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
| `CODESIGHT_OLLAMA_MAX_INPUT_CHARS` | Optional cap for Ollama embed input chars (must be positive int) | (auto-detected/default) |

`cs index` auto-detects Ollama model context length when available, uses a conservative character budget, and adaptively retries with smaller limits on context-length overflow errors. `CODESIGHT_OLLAMA_MAX_INPUT_CHARS` can only lower that effective limit as a safety cap.

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

### Making agents use `cs search` reliably

Installing the skill alone is not enough — agents will default to built-in tools (grep, glob, file reads) unless you either:
1. add an explicit instruction to the agent's config, or
2. directly tell the agent in your prompt to use `cs search`.

To make an agent reliably use `cs search`, add the following instruction to the agent's **project-level** config file:

| Agent       | Config file                          |
|-------------|--------------------------------------|
| Claude Code | `CLAUDE.md`                          |
| Gemini      | `GEMINI.md`                          |
| Codex       | `AGENTS.md` or `AGENTS.override.md`  |

```markdown
Use `cs search "<query>"` via Bash for conceptual questions when you don't know which files matter.
Use Grep for exact text, identifiers, patterns, and class names.
Do NOT use cs search for exact-match lookups.
Do NOT read 5+ files to understand a feature — cs search ranks them for you.
```
> [!IMPORTANT]
> Add this instruction to the **project-level** config file if you want it to apply automatically. `cs` depends on a per-project index, so enabling it globally would make agents try to use `cs search` in unrelated projects that have not been indexed.

> [!NOTE]
> Skills and referenced files are passive — agents may not follow them reliably. Instructions placed directly in the agent's config file are loaded into the agent's context automatically and have the strongest influence on tool selection behavior.

#### Quick verification

Ask the agent something like:
- `Where is authentication handled?`
- `Find the code that creates the database connection pool.`
- `Search for JWT validation logic.`

A correct run should start with `cs search`. If the agent starts with `grep`, `glob`, or broad file reads for these semantic queries, the config instruction is either missing or too weak.

### Use `codesight` together with `symgrep`

Combine `cs search` for semantic discovery with [symgrep](https://github.com/blankmi/symgrep) `extract` for surgical code reading from large files. The recommended agent config above already includes both tools — `cs search` for understanding, `symgrep extract` for reading less.

Each tool stays in its lane:

| Action | Tool | Why |
|---|---|---|
| Search for text/identifiers | Grep | Models already use Grep efficiently — no instruction needed |
| Understand a feature/flow | `cs search` | Embedding-based ranking finds relevant files without reading everything |
| Read one symbol from a large file | `symgrep extract` | Avoids loading 200+ irrelevant lines into context |

### Allowlisting `cs` for autonomous use

By default, coding agents require user approval before running shell commands. To let an agent use `cs` without prompting each time, add it to the agent's permission allowlist.

**Claude Code** — add to `.claude/settings.json` (project-level) or `~/.claude/settings.json` (global):

```json
{
  "permissions": {
    "allow": [
      "Bash(cs *)"
    ]
  }
}
```

See the [Claude Code permissions docs](https://docs.anthropic.com/en/docs/claude-code/settings#permissions) for more details on permission rules and scoping.

**Codex** — add to `~/.codex/rules/default.rules` (or project-level `.codex/rules/*.rules`):

```python
# Allow direct cs invocations outside the sandbox without approval prompts.
prefix_rule(
    pattern = ["cs"],
    decision = "allow",
)
```

See the [Codex rules docs](https://developers.openai.com/codex/rules) for rule syntax and scope details.

> [!IMPORTANT]
> **Network Escalation:** Codex restricts outbound network access by default. Since `cs` needs to talk to Milvus and Ollama, it will initially fail with a timeout error:
> `connecting to the vector store timed out after 1s; network access may be blocked in this sandbox...`
>
> This error is specifically designed to be recognized by the agent, which will then request a **network escalation**. Once you approve the escalation, the agent will have the necessary access to perform searches. See the [Codex security docs](https://developers.openai.com/codex/agent-approvals-security/#network-access) for more details.

**Gemini CLI** — add to `.gemini/policies/cs.toml` (project-level) or `~/.gemini/policies/cs.toml` (global):

```toml
# Allow skill activation without confirmation
[[rule]]
toolName = "activate_skill"
decision = "allow"
priority = 100

# Allow cs command execution without confirmation
[[rule]]
toolName = "run_shell_command"
commandPrefix = "cs"
decision = "allow"
priority = 100
```
## License
MIT
