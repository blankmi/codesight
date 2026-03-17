# codesight (`cs`)

Unified code intelligence CLI for large codebases. `cs` combines semantic discovery (`search`), surgical symbol extraction (`extract`), and LSP-powered navigation (`refs`, `callers`, `implements`) while preserving existing index lifecycle workflows (`index`, `status`, `clear`).

> **Benchmark note:** A/B testing (88 agent invocations, codebases up to 250K LOC, Sonnet 4.6) showed `cs search` saves **14.5% tokens on conceptual queries** by surfacing relevant files from the semantic index instead of agents reading 30+ files blind. 
> Grep already handles lexical search optimally. `cs search` fills the conceptual gap Grep can't: "how does X work?" across a large codebase. Shorter instructions (7 lines) outperformed verbose ones (29 lines) by 15 percentage points.

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

### Extract a symbol (recommended in this repo)

```bash
cs extract -f ./pkg/lsp/refs.go -s NormalizeRefKind
cs extract -f ./pkg/lsp/refs.go -s NormalizeRefKind --format json
```

### Find references

```bash
cs refs NormalizeRefKind
cs refs NormalizeRefKind --path ./pkg/lsp
cs refs NormalizeRefKind --kind function
```

### Find callers

```bash
cs callers runSearch
cs callers runSearch --path ./cmd/cs --depth 2
```

### Find implementations (stretch command; shipped in this repository)

```bash
cs implements Store
cs implements Store --path ./pkg
```

## v2 command matrix

| Command | Signature | Notes |
|---|---|---|
| `cs index` | `cs index [path] [--branch <name>] [--commit <sha>] [--force]` | Existing workflow unchanged |
| `cs search` | `cs search <query> [--path <dir>] [--ext .go,.ts] [--limit <n>]` | Semantic discovery |
| `cs status` | `cs status [path]` | Existing workflow unchanged |
| `cs clear` | `cs clear [path]` | Existing workflow unchanged |
| `cs extract` | `cs extract -f <file-or-dir> -s <symbol> [--format raw|json]` | `--format` supports exactly `raw` (default) or `json` |
| `cs refs` | `cs refs <symbol> [--path <dir>] [--kind <kind>]` | `--kind` allowed: `function`, `method`, `class`, `interface`, `type`, `constant` |
| `cs callers` | `cs callers <symbol> [--path <dir>] [--depth <n>]` | Depth default `1`; must be positive |
| `cs implements` | `cs implements <symbol> [--path <dir>]` | Stretch command delivered here; no grep fallback |

## v2 tool selection policy

In this repository:
- Use Grep for exact text, identifiers, and lexical pattern matching.
- Use `cs search` for conceptual discovery when you do not yet know which files matter.
- Use `cs extract` for symbol extraction (this replaces standalone `symgrep extract` as the default path here).
- Use `cs refs`, `cs callers`, and `cs implements` for cross-file symbol navigation.

`cs extract` supports: Go, Python, Java, JavaScript, TypeScript, Rust, C++, XML, and HTML.

## Search output format

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
| `CODESIGHT_STATE_DIR`          | LSP lifecycle state root (`refs/callers/implements`) | `${HOME}/.codesight` |

`cs index` auto-detects Ollama model context length when available, uses a conservative character budget, and adaptively retries with smaller limits on context-length overflow errors. `CODESIGHT_OLLAMA_MAX_INPUT_CHARS` can only lower that effective limit as a safety cap.

## Docker runtime model for LSP commands

For `cs refs`, `cs callers`, and `cs implements`:
- LSP servers run as child processes in the same container/runtime as `cs`.
- Transport is stdio JSON-RPC only (no remote/TCP LSP mode in v2).
- `host.docker.internal` is for Milvus/Ollama network endpoints only, not LSP transport.
- `${CODESIGHT_STATE_DIR:-~/.codesight}` persistence is recommended for warm starts, but not required for correctness.

### Mode A: persisted state (warm reuse)

```bash
docker run --rm -it \
  -v "$(pwd):/workspace" \
  -v codesight_state:/state \
  -e CODESIGHT_STATE_DIR=/state \
  -w /workspace \
  your-image \
  cs refs NormalizeRefKind --path /workspace
```

Run the same command again in the same mounted volume to reuse warmed lifecycle state.

### Mode B: ephemeral state (cold start, still correct)

```bash
docker run --rm -it \
  -v "$(pwd):/workspace" \
  -w /workspace \
  your-image \
  cs refs NormalizeRefKind --path /workspace
```

Without a mounted state directory, each container starts cold but command contracts and output remain the same.

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
> Install these skills per-project so agents use the correct workspace context. Semantic search commands (`cs search`) require project indexing; extraction/navigation commands do not.

### Making agents follow the v2 tool policy

Installing the skill alone is not enough. Agents can still default to built-in tools (grep, glob, broad file reads) unless you also add explicit project instructions.

To make an agent follow the v2 command policy, add the following to the agent's **project-level** config file:

| Agent       | Config file                          |
|-------------|--------------------------------------|
| Claude Code | `CLAUDE.md`                          |
| Gemini      | `GEMINI.md`                          |
| Codex       | `AGENTS.md` or `AGENTS.override.md`  |

```markdown
Use `cs search "<query>"` via Bash for conceptual questions when you don't know which files matter.
Use Grep for exact text, identifiers, patterns, and class names.
Use `cs extract -f <file-or-dir> -s <symbol>` for symbol extraction in this repository.
Use `cs refs <symbol>` for cross-file references (`--kind`: function|method|class|interface|type|constant).
Use `cs callers <symbol>` for incoming call hierarchy.
Use `cs implements <symbol>` for type/interface implementation lookup.
Do not use remote/TCP LSP mode; `cs` runs local child LSP servers over stdio.
```
> [!IMPORTANT]
> Add this instruction to the **project-level** config file if you want it to apply automatically. `cs` depends on per-project context (index + workspace path), so enabling it globally can trigger commands in unrelated repos.

> [!NOTE]
> Skills and referenced files are passive — agents may not follow them reliably. Instructions placed directly in the agent's config file are loaded into the agent's context automatically and have the strongest influence on tool selection behavior.

#### Quick verification

Ask the agent something like:
- `Where is authentication handled?`
- `Extract NormalizeRefKind from pkg/lsp/refs.go.`
- `Find refs for NormalizeRefKind.`
- `Who calls runSearch at depth 2?`

A correct run should route by intent: conceptual queries start with `cs search`, extraction uses `cs extract`, and symbol navigation uses `cs refs`/`cs callers`. If it starts with broad grep/file reads for those intents, the instruction is missing or too weak.

### Migration note: `symgrep extract` to `cs extract`

Earlier guidance used standalone `symgrep extract` for symbol reads. In v2 for this repository, prefer `cs extract`.

Each tool stays in its lane:

| Action | Tool | Why |
|---|---|---|
| Search for text/identifiers | Grep | Models already use Grep efficiently — no instruction needed |
| Understand a feature/flow | `cs search` | Embedding-based ranking finds relevant files without reading everything |
| Read one symbol from a file or directory | `cs extract` | Built-in extraction contract (`raw`/`json`) with deterministic directory traversal |
| Find references for a symbol | `cs refs` | LSP-first precision with grep fallback note when LSP is unavailable |
| Trace incoming call hierarchy | `cs callers` | LSP call hierarchy with explicit depth control |
| List type/interface implementations | `cs implements` | LSP type hierarchy lookup (stretch command delivered in this repository) |

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
