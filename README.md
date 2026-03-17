# codesight (`cs`)

Unified code intelligence CLI for large codebases. `cs` combines semantic discovery (`search`), surgical symbol extraction (`extract`), and LSP-powered navigation (`refs`, `callers`, `implements`) while preserving existing index lifecycle workflows (`index`, `status`, `clear`).

> **Benchmark results (Opus 4.6, 250K LOC Java codebase, 24 agent invocations):**
> - Conceptual queries ("how does auth work?"): **51% faster, 25% cheaper, 69% fewer tool calls** vs no instructions
> - `cs search` + `cs extract` replaces blind 56-call Agent exploration with targeted 17-call workflows
> - `cs extract` reduces file-reading from 22-32 `Read` calls (entire files) to 7-8 targeted symbol extractions (**75% fewer**)
> - Lexical/reference/symbol queries: Grep is already optimal — instructions neither help nor hurt
> - Total across all query types: **22% cost reduction, 40% faster**
>
> Grep handles exact lookups. `cs search` fills the conceptual gap: "how does X work?" across a large codebase. `cs extract` provides surgical symbol reads without loading entire files. `cs refs`/`callers`/`implements` add LSP-powered cross-file navigation.

## How it works

`cs` has three independent subsystems — each can be used standalone:

### Semantic search (`cs index`, `cs search`, `cs status`, `cs clear`)

Requires Ollama + Milvus.

1. **Walk** — traverses the repo respecting `.gitignore` and `.csignore`
2. **Split** — extracts functions, classes, methods, and types using tree-sitter AST parsing (falls back to line-based chunking for unsupported languages)
3. **Embed** — generates vectors via Ollama (default: `nomic-embed-text`)
4. **Store** — inserts chunks + vectors into Milvus
5. **Search** — embeds a natural-language query and returns the most relevant code chunks

### Symbol extraction (`cs extract`)

No external dependencies. Uses tree-sitter AST parsing to extract a named symbol (function, class, method, type) from a file or directory. Supports 9 languages.

In benchmarks, agents with `cs extract` replaced 22-32 `Read` calls (which load entire files) with 7-8 targeted `cs extract` calls that return only the requested symbol. On a 250K LOC codebase, this reduced file-reading tool calls by **75%** during conceptual queries.

### LSP navigation (`cs refs`, `cs callers`, `cs implements`)

Requires a language server binary (e.g. `gopls`, `jdtls`, `pylsp`). Manages LSP server lifecycles as child processes over stdio. `cs refs` falls back to grep when the LSP is unavailable; `cs callers` and `cs implements` fail fast with install guidance.

These commands replace the manual grep→read→grep exploration loops agents use to trace cross-file dependencies. Not yet measured in benchmarks, but available in the toolset and included in the agent instructions.

## Supported languages

| Feature | Languages |
|---|---|
| AST-aware chunking (index/search) | Go, TypeScript, JavaScript, Python, Java, Rust, C, C++ |
| Symbol extraction (extract) | Go, TypeScript, JavaScript, Python, Java, Rust, C++, XML, HTML |
| LSP navigation (refs/callers/implements) | Go (`gopls`), Python (`pylsp`), Java (`jdtls`), TypeScript/JavaScript (`typescript-language-server`), Rust (`rust-analyzer`), C/C++ (`clangd`) |

All other languages get line-based chunking with overlap for indexing.

## Quick start

### Prerequisites

`cs extract` works out of the box — no external dependencies.

For semantic search (`cs index`, `cs search`):
- [Ollama](https://ollama.com) running locally with an embedding model
- [Milvus](https://milvus.io) (standalone Docker container)

For LSP navigation (`cs refs`, `cs callers`, `cs implements`):
- A language server for your language (e.g. `gopls`, `jdtls`, `pylsp`)
- `cs refs` falls back to grep if no LSP is available

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

`cs` also supports a root-level `.csignore` file. Its patterns are additive with `.gitignore` and apply to indexing, search result filtering, extraction, refs fallback/LSP result filtering, and LSP language detection within the command target root.

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
├── vectorstore/          # Semantic search: vector storage
│   ├── store.go          #   Store interface
│   └── milvus.go         #   Milvus implementation
├── embedding/            # Semantic search: embeddings
│   ├── embedding.go      #   Provider interface
│   └── ollama.go         #   Ollama HTTP client
├── splitter/             # Semantic search: code chunking
│   ├── splitter.go       #   Splitter interface + Chunk type
│   ├── treesitter.go     #   AST-aware splitter
│   └── fallback.go       #   Line-based fallback
├── extract/              # Symbol extraction (cs extract)
│   ├── extract.go        #   Tree-sitter AST symbol resolver
│   ├── languages.go      #   Language detection + parser registry
│   └── types.go          #   SymbolMatch output type
├── lsp/                  # LSP navigation (cs refs/callers/implements)
│   ├── client.go         #   JSON-RPC stdio client
│   ├── lifecycle.go      #   Per-workspace LSP daemon management
│   ├── registry.go       #   Language → LSP binary mapping
│   ├── refs.go           #   Find references engine + grep fallback
│   ├── callers.go        #   Incoming call hierarchy engine
│   └── implements.go     #   Type hierarchy / subtypes engine
├── ignore/               # .gitignore + .csignore rule engine
│   └── matcher.go        #   Unified ignore pattern matcher
├── indexer.go            # Indexing pipeline
├── searcher.go           # Search pipeline
├── version.go            # Index versioning + staleness (commit + ignore fingerprint)
└── walker.go             # .gitignore/.csignore-aware file walker
```

Packages are under `pkg/` (not `internal/`) so other tools can import codesight as a library.

## Index lifecycle

- **One index per project** — collection name is `cs_{sha256(abs_path)[:8]}`
- **Re-index on stale** — compares indexed commit SHA with current HEAD
- **Clean slate** — re-indexing drops and recreates the collection (no stale chunks from deleted files)
- `cs status` reports staleness so agents know the index may be slightly behind

## Agent Integration

Agent tool selection is driven by **project-level instruction files**, not skills. Benchmark data confirms that instructions in these files are loaded into the agent's context automatically and directly influence which tools the agent picks. Skills are optional supplementary reference but are not required.

### Step 1: Add project instructions

Add the following to your project's instruction file. This is the primary mechanism that makes agents use `cs`.

| Agent | Instruction file | Loaded automatically |
|---|---|---|
| Claude Code | `CLAUDE.md` | Yes — loaded into context on every turn |
| Gemini CLI | `GEMINI.md` | Yes — loaded hierarchically from workspace dirs |
| Codex | `AGENTS.md` | Yes — loaded from project root to cwd on each run |

```markdown
# Tool Selection

- **Search** → `Grep`. Always start here for text, identifiers, patterns, class names.
- **Understand** → `cs search "<query>"` via Bash. Use for conceptual questions when you don't know which files matter.
- **Extract** → `cs extract -f <file> -s <symbol>` via Bash. Use instead of Read when you need one symbol from a file >200 lines.
- **References** → `cs refs <symbol> --path <dir>` via Bash. Find all references to a symbol across files.
- **Call hierarchy** → `cs callers <symbol> --path <dir>` via Bash. Trace who calls a function.
- **Implementations** → `cs implements <symbol> --path <dir>` via Bash. Find implementations of an interface/type.
- **Find files** → `Glob`.

Do NOT use cs search for exact-match lookups. Do NOT read 5+ files to understand a feature — cs search ranks them for you.
```

> [!IMPORTANT]
> Add instructions at the **project level**, not globally. `cs` depends on per-project context (index + workspace path), so enabling it globally can trigger commands in unrelated repos.

### Step 2: Allow `cs` commands without prompting

Agents require user approval before running shell commands by default. Without explicit permission, `cs` commands are blocked at runtime even if the instructions tell the agent to use them — the agent falls back to Grep+Read silently. Benchmarks confirmed this: instructions alone produced 20.5 tool calls per conceptual query, but adding the permission allowlist dropped it to 17.3.

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

**Codex** — add a [prefix rule](https://developers.openai.com/codex/rules/) to project-level `.codex/rules/*.rules` or `~/.codex/rules/default.rules`:

```python
prefix_rule(
    pattern = ["cs"],
    decision = "allow",
)
```

> [!NOTE]
> Codex restricts outbound network access by default. Commands that talk to Milvus/Ollama (`cs index`, `cs search`, `cs status`, `cs clear`) will fail until the agent requests a network escalation. The error message is designed for the agent to recognize and escalate automatically.

**Gemini CLI** — add a [policy rule](https://github.com/google-gemini/gemini-cli/blob/main/docs/reference/policy-engine.md) to `.gemini/policies/cs.toml` (project-level) or `~/.gemini/policies/cs.toml` (global):

```toml
[[rule]]
toolName = "run_shell_command"
commandPrefix = "cs"
decision = "allow"
priority = 100
```

### Step 3 (optional): Install skill files

Pre-built skill files provide extended reference documentation for each agent. These are **not required** for tool routing — the project instructions from Step 1 are sufficient. Skills may help agents with flag details and edge cases.

| Agent | Skill file | Install |
|---|---|---|
| Claude Code | `agent-skills/claude-code/cs/SKILL.md` | Copy into `.claude/skills/cs` (project) or `~/.claude/skills/cs` (global) |
| Gemini CLI | `agent-skills/gemini/cs/SKILL.md` | Copy into `.gemini/skills/cs` (project) or `~/.gemini/skills/cs` (global) |
| Codex | `agent-skills/codex/cs/SKILL.md` | Reference from `AGENTS.md` or place alongside project instructions |

### Verification

Ask the agent:
- `Where is authentication handled?` — should start with `cs search`, not broad file reads
- `Extract SecurityConfig from SecurityConfig.java` — should use `cs extract`
- `Who calls processPayment?` — should use `cs refs` or `cs callers`

If it starts with broad grep/file reads for conceptual queries, check that both the instruction file (Step 1) and the permission allowlist (Step 2) are in place.

### Tool routing summary

| Action | Tool | Why |
|---|---|---|
| Search for text/identifiers | Grep | Models already use Grep efficiently — no instruction needed |
| Understand a feature/flow | `cs search` | Embedding-based ranking finds relevant files without reading everything |
| Read one symbol from a large file | `cs extract` | AST-based extraction (`raw`/`json`) with deterministic directory traversal |
| Find references for a symbol | `cs refs` | LSP-first precision with grep fallback when LSP is unavailable |
| Trace incoming call hierarchy | `cs callers` | LSP call hierarchy with explicit depth control |
| List type/interface implementations | `cs implements` | LSP type hierarchy lookup |

## License
MIT
