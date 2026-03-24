# codesight (`cs`)

Unified code intelligence CLI for large codebases. `cs` provides a single-call retrieval front door (`cs <query>`) that routes, ranks, and budgets code intelligence from symbol extraction, references, callers, and implementations — replacing multi-step agent retrieval. It also includes semantic discovery (`search`), index lifecycle management (`index`, `status`, `clear`), surgical symbol extraction (`extract`), LSP-powered navigation (`refs`, `callers`, `implements`), project configuration (`init`, `config`), and LSP daemon lifecycle tooling (`lsp`).

> **Benchmark results (Opus 4.6, 250K LOC Java codebase, 24 agent invocations):**
> - Conceptual queries ("how does auth work?"): **51% faster, 25% cheaper, 69% fewer tool calls** vs no instructions
> - `cs search` + `cs extract` replaced blind 56-call agent exploration with targeted 17-call workflows
> - `cs extract` reduced file-reading from 22-32 full-file reads to 7-8 targeted symbol extractions (**75% fewer**)
> - Lexical/reference/symbol queries: Grep was already optimal, so instructions neither helped nor hurt
> - Total across all query types: **22% cost reduction, 40% faster**
>
> Grep handles exact lookups. `cs search` fills the conceptual gap: "how does X work?" across a large codebase. `cs extract` reads one symbol without loading entire files. `cs refs`/`callers`/`implements` add cross-file navigation on top.

## How it works

`cs` has five complementary workflows:

### Unified retrieval (`cs <query>` / `cs query`)

No external services required for symbol, path, and text queries. LSP daemon recommended for references, callers, and implementations.

`cs <query>` is the default retrieval front door. It classifies the query (symbol, path, text, or AST), fetches the definition via Tree-sitter, resolves references/callers/implementations via LSP in parallel, scores and ranks evidence, slices long definitions to fit an adaptive line budget, and returns LLM-facing Markdown — all in one call.

```bash
cs Authenticate                       # symbol lookup + refs + callers
cs auth.Login --depth 2               # deeper caller expansion
cs pkg/auth.go                        # path discovery
cs "connection refused"               # text search
cs Authenticate --budget large        # more context
cs query Authenticate --mode symbol   # explicit routing
```

### Semantic search and index lifecycle (`cs search`, `cs index`, `cs status`, `cs clear`)

Requires Ollama + Milvus.

1. **Walk**: traverses the repo respecting `.gitignore` and `.csignore`
2. **Split**: extracts functions, classes, methods, and types with tree-sitter AST parsing, then falls back to line-based chunking for unsupported languages
3. **Embed**: generates vectors via Ollama (default: `bge-m3`)
4. **Store**: inserts chunks and vectors into Milvus
5. **Search**: embeds a natural-language query and returns the most relevant chunks

### Symbol extraction (`cs extract`)

No external services required. `cs extract` uses tree-sitter to extract a named symbol from a file or directory and returns either raw source or structured JSON.

In benchmarks, agents using `cs extract` replaced 22-32 full-file reads with 7-8 targeted symbol extractions during conceptual work.

### LSP navigation (`cs refs`, `cs callers`, `cs implements`)

Requires a language server binary such as `gopls`, `jdtls`, `pylsp`, `typescript-language-server`, `rust-analyzer`, or `clangd`.

- `cs refs` is LSP-first and falls back to grep if LSP startup fails or no supported LSP language is detected.
- `cs callers` and `cs implements` require LSP and fail fast with install guidance.
- On macOS and Linux, `cs` reuses background LSP daemons to avoid repeated cold starts.

### Project config and daemon operations (`cs init`, `cs config`, `cs lsp`)

- `cs init` scaffolds `.codesight/config.toml` and `.codesight/.gitignore`
- `cs config` prints the effective configuration and the source of each value
- `cs lsp warmup|status|restart|cleanup` manages warmed LSP daemons directly

## Supported languages

| Feature | Languages |
|---|---|
| AST-aware chunking (index/search) | Go, TypeScript, JavaScript, Python, Java, Rust, C, C++ |
| Symbol extraction (extract) | Go, TypeScript, JavaScript, Python, Java, Rust, C++, XML, HTML |
| LSP navigation and daemon management | Go (`gopls`), Python (`pylsp`), Java (`jdtls`), TypeScript/JavaScript (`typescript-language-server`), Rust (`rust-analyzer`), C/C++ (`clangd`) |

All other languages fall back to line-based chunking for indexing.

## Quick start

### Prerequisites

Works locally with no external services:
- `cs <query>` (symbol, path, and text modes)
- `cs extract`
- `cs init`
- `cs config`

Requires external services:
- Semantic search and index lifecycle (`cs index`, `cs search`, `cs status`, `cs clear`): [Ollama](https://ollama.com) + [Milvus](https://milvus.io)
- LSP navigation (`cs refs`, `cs callers`, `cs implements`, `cs lsp`): a language server for your language

Optional but recommended:
- Persist LSP daemon state in `CODESIGHT_STATE_DIR` or use the runtime default under `~/.codesight`

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

# Pull the default embedding model
ollama pull bge-m3
```

### Build and validate

```bash
make build
go test ./...
make lint

# Optional wrapper around tests
make test

# Integration coverage requires Docker + Milvus
make test-integration

# Smoke test the binary
bin/cs --help
```

### Initialize project config

```bash
cs init /path/to/repo
cs config /path/to/repo
```

`cs init` is safe to re-run. If `.codesight/config.toml` already exists, it leaves it unchanged.

### Index a codebase

```bash
cs index /path/to/repo --branch main --commit "$(git rev-parse HEAD)"
```

### Search

```bash
cs search "authentication middleware"
cs search "database connection pool" --path /path/to/repo --ext .go
cs search "error handling" --path /path/to/repo --limit 5
```

### Check status

```bash
cs status /path/to/repo
```

### Clear index

```bash
cs clear /path/to/repo
```

### Unified query

```bash
cs Authenticate                          # symbol + refs + callers
cs Authenticate --depth 2 --budget large # deeper callers, more context
cs pkg/auth.go                           # path discovery
cs "connection refused"                  # text search
cs query Authenticate --mode symbol      # explicit subcommand + mode
```

### Extract a symbol

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

### Find implementations

```bash
cs implements Store
cs implements Store --path ./pkg
```

### Manage LSP daemons

```bash
cs lsp warmup /path/to/repo
cs lsp status /path/to/repo
cs lsp restart /path/to/repo
cs lsp cleanup
```

## Command reference

All commands support `-v, --verbose`.

| Command | Signature | Notes |
|---|---|---|
| `cs <query>` | `cs <query> [--path <dir>] [--depth <n>] [--budget auto\|small\|medium\|large] [--mode auto\|symbol\|text\|ast\|path]` | Unified retrieval front door; also available as `cs query <query>` |
| `cs index` | `cs index [path] [--branch <name>] [--commit <sha>] [--force]` | Creates or refreshes the semantic index |
| `cs search` | `cs search <query> [--path <dir>] [--ext .go,.ts] [--limit <n>]` | Semantic discovery |
| `cs status` | `cs status [path]` | Reports index freshness |
| `cs clear` | `cs clear [path]` | Drops the index for a project |
| `cs extract` | `cs extract -f <file-or-dir> -s <symbol> [--format raw|json]` | AST-based symbol extraction |
| `cs refs` | `cs refs <symbol> [--path <dir>] [--kind <kind>]` | `--kind`: `function`, `method`, `class`, `interface`, `type`, `constant` |
| `cs callers` | `cs callers <symbol> [--path <dir>] [--depth <n>]` | Incoming call hierarchy; depth defaults to `1` |
| `cs implements` | `cs implements <symbol> [--path <dir>]` | Type or interface implementations |
| `cs init` | `cs init [path]` | Creates `.codesight/config.toml` and `.codesight/.gitignore` |
| `cs config` | `cs config [path]` | Prints effective config values and their provenance |
| `cs lsp` | `cs lsp [command]` | LSP daemon lifecycle operations |

### `cs lsp` subcommands

| Command | Signature | Notes |
|---|---|---|
| `cs lsp warmup` | `cs lsp warmup [path]` | Starts or reuses the daemon for the detected workspace language |
| `cs lsp status` | `cs lsp status [path]` | Shows daemon PID, health, uptime, and log path |
| `cs lsp restart` | `cs lsp restart [path]` | Restarts the daemon for the detected workspace language |
| `cs lsp cleanup` | `cs lsp cleanup` | Removes orphaned daemon artifacts |

## Search output format

Search results are plain text and intentionally compact:

```text
[1] internal/auth/middleware.go:42-86 (score: 0.87)
    function - func AuthMiddleware(next http.Handler) http.Handler {

[2] internal/db/pool.go:15-52 (score: 0.82)
    function - func NewConnectionPool(cfg PoolConfig) (*Pool, error) {
```

## Configuration

Codesight loads configuration in this order:

1. Built-in defaults
2. `~/.codesight/config.toml`
3. The nearest `.codesight/config.toml` from the target path upward
4. `CODESIGHT_*` environment variables

Use `cs config [path]` to inspect the final values and where they came from.

### Example project config

```toml
project_root = ".."

[embedding]
model = "bge-m3"
max_input_chars = 12000

[index]
warm_lsp = true

[lsp.daemon]
idle_timeout = "15m"
warmup_probe_timeout = "30s"

[lsp.go]
build_flags = ["-tags=integration"]

[lsp.java]
gradle_java_home = "/path/to/jdk"
timeout = "90s"
args = ["-Xms256m", "-Xmx2g"]
```

Notes:
- `project_root` is resolved relative to the `.codesight` directory unless set via `CODESIGHT_PROJECT_ROOT`.
- `index.warm_lsp` currently pre-warms Java workspaces after `cs index`; for other languages it is a no-op.
- `cs init` generates a language-aware starter config when it detects Go, Java, Rust, or TypeScript project files.
- `cs init` also creates `.codesight/.gitignore` with `lsp/` so daemon state is not committed.

### Environment variables

| Variable | Config key | Default / behavior |
|---|---|---|
| `CODESIGHT_DB_TYPE` | `db.type` | `milvus` |
| `CODESIGHT_DB_ADDRESS` | `db.address` | `localhost:19530` |
| `CODESIGHT_DB_TOKEN` | `db.token` | empty |
| `CODESIGHT_OLLAMA_HOST` | `embedding.ollama_host` | `http://127.0.0.1:11434` |
| `CODESIGHT_EMBEDDING_MODEL` | `embedding.model` | `bge-m3` |
| `CODESIGHT_OLLAMA_MAX_INPUT_CHARS` | `embedding.max_input_chars` | `0` means auto/default behavior |
| `CODESIGHT_STATE_DIR` | `state_dir` | runtime default is `${HOME}/.codesight` |
| `CODESIGHT_GRADLE_JAVA_HOME` | `lsp.java.gradle_java_home` | unset |
| `CODESIGHT_LSP_DAEMON_IDLE_TIMEOUT` | `lsp.daemon.idle_timeout` | `10m` |
| `CODESIGHT_LSP_DAEMON_WARMUP_PROBE_TIMEOUT` | `lsp.daemon.warmup_probe_timeout` | unset |
| `CODESIGHT_PROJECT_ROOT` | `project_root` | unset |

`cs index` auto-detects Ollama model context length when available, uses a conservative character budget, and retries with smaller limits on context overflow. `CODESIGHT_OLLAMA_MAX_INPUT_CHARS` only lowers that effective limit.

`cs` also supports a root-level `.csignore` file. Its patterns are additive with `.gitignore` and apply to indexing, search result filtering, extraction, refs fallback, LSP result filtering, and language detection within the target root.

## Docker and container runtime model

For `cs refs`, `cs callers`, `cs implements`, and `cs lsp`:

- LSP servers run as child processes in the same runtime as `cs`
- Transport is stdio JSON-RPC only
- `host.docker.internal` is relevant for Milvus and Ollama, not for LSP transport
- Persisting `CODESIGHT_STATE_DIR` allows warm daemon reuse across container runs

### Warm reuse

```bash
docker run --rm -it \
  -v "$(pwd):/workspace" \
  -v codesight_state:/state \
  -e CODESIGHT_STATE_DIR=/state \
  -w /workspace \
  your-image \
  cs refs NormalizeRefKind --path /workspace
```

### Cold start

```bash
docker run --rm -it \
  -v "$(pwd):/workspace" \
  -w /workspace \
  your-image \
  cs refs NormalizeRefKind --path /workspace
```

Without a mounted state directory, the commands are still correct; they just start from a cold LSP state each time.

## Index lifecycle

- One semantic index per project: collection name is `cs_<sha256(project_path)[:16]>`
- Re-index on stale metadata: commit SHA and ignore fingerprint are both checked
- Clean slate semantics: re-indexing drops and recreates the collection
- `cs status` reports whether the index is current or stale

## Architecture

```text
cmd/cs/                Cobra command wiring
pkg/
├── engine/            Unified query router, scoring, budgeting, and rendering
├── config/            Config loading, precedence, and provenance
├── vectorstore/       Semantic search storage (Milvus)
├── embedding/         Embedding provider integration (Ollama)
├── splitter/          AST-aware and fallback chunking
├── extract/           Symbol extraction engine
├── lsp/               Navigation engines and daemon lifecycle
├── ignore/            .gitignore + .csignore matching
├── indexer.go         Indexing pipeline
├── searcher.go        Search pipeline
├── version.go         Collection naming and staleness logic
└── walker.go          Ignore-aware filesystem traversal
```

Packages live under `pkg/`, not `internal/`, so other tools can import codesight as a library.

## Agent integration

Agent tool selection should usually be driven by project instruction files. Skills are optional reference material, not the main routing mechanism.

### Step 1: add project instructions

Different agents have different failure modes, so each template is tuned accordingly. Copy the matching template into the instruction file that your agent loads automatically:

| Agent | Instruction file | Template | Tuned for |
|---|---|---|---|
| Claude Code | `CLAUDE.md` | [TPL_CLAUDE.md](TPL_CLAUDE.md) | Tends to over-read for “confidence” and sometimes skips tools — template enforces `cs <query>` as the default retrieval front door |
| Codex | `AGENTS.md` | [TPL_AGENTS.md](TPL_AGENTS.md) | Tends to overuse `read_file` and chain grep-read loops — template enforces strict `cs`-first retrieval policy |
| Gemini CLI | `GEMINI.md` | [TPL_GEMINI.md](TPL_GEMINI.md) | Tends to overuse semantic search for simple lookups — template routes all first-pass retrieval through `cs <query>` |

All three templates share the same routing logic:

| Intent | Tool |
|---|---|
| Any code retrieval (first pass) | `cs <query>` |
| Conceptual / architectural discovery | `cs search “<query>” --path .` |
| Raw symbol body | `cs extract -f <file> -s <symbol>` |
| Shell fallback (after cs says to) | `grep` / `tail` |
| Execution (tests, builds, git) | shell tools |


> [!IMPORTANT]
> Keep context-dependent `cs` workflows project-scoped. `cs <query>`, `cs search`, `cs index`, `cs status`, `cs clear`, `cs refs`, `cs callers`, `cs implements`, and `cs lsp` depend on workspace-specific context such as project path, index state, and installed language servers, so those instructions belong in project-level files. Tree-sitter-based symbol extraction such as `cs extract` can be enabled in global instructions, but only if the global rule is explicit about that narrower scope and does not route agents into the project-context-dependent commands above.

### Step 2: allow `cs` commands

Agents generally need shell permission to run `cs`.

**Claude Code**: allow `Bash(cs *)` in project or user settings.

**Codex**: add a project or user `prefix_rule` for `["cs"]`.

**Gemini CLI**: add a policy rule that allows shell commands with the `cs` prefix.

If your agent runtime sandboxes network, semantic commands that talk to Milvus or Ollama may still need explicit network approval.

### Step 3: optional skill files

Prebuilt skill files are available if you want additional agent-facing reference docs:

| Agent | Skill file | Install |
|---|---|---|
| Claude Code | `agent-skills/claude-code/cs/SKILL.md` | Copy into `.claude/skills/cs` or `~/.claude/skills/cs` |
| Gemini CLI | `agent-skills/gemini/cs/SKILL.md` | Copy into `.gemini/skills/cs` or `~/.gemini/skills/cs` |
| Codex | `agent-skills/codex/cs/SKILL.md` | Reference from `AGENTS.md` or place alongside project instructions |

### Verification

Ask the agent:
- `Where is authentication handled?` -> should start with `cs Authenticate` or `cs search`
- `What does processPayment do?` -> should use `cs processPayment`
- `Who calls processPayment?` -> should use `cs processPayment --depth 2`
- `Extract SecurityConfig from SecurityConfig.java` -> should use `cs SecurityConfig` or `cs extract`

If the agent falls back to broad grep and full-file reads for first-pass retrieval, check both the project instructions and the shell permission allowlist.

## License

MIT
