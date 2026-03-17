# PRD: codesight v2 — Unified Code Intelligence CLI

## Problem

AI coding agents waste tokens in two ways:

1. **Conceptual queries** — agents read 30+ files to understand how a feature works. `cs search` (embeddings) solves this, saving 14.5% on a 250K LOC codebase.
2. **Cross-file navigation** — agents can't precisely trace references, call chains, or type hierarchies. Grep gives false positives; reading files manually is expensive. Nothing solves this today.

Meanwhile, `symgrep` exists as a separate tool for AST-based symbol extraction. Benchmarking showed its *search* features (`list`) are worse than Grep, but its *extraction* (`extract`) has real value on large files. Maintaining two separate CLIs creates confusion for agents and users.

## Proposal

Merge symgrep's extraction into codesight and add LSP-powered navigation. One CLI, four actions:

```
cs search "how does auth work"                    # existing — embeddings
cs extract <file> <symbol>                        # new — from symgrep
cs refs <symbol>                                  # new — LSP: find references
cs callers <symbol>                               # new — LSP: call hierarchy
```

## Background

### Benchmark data (88 agent invocations, up to 250K LOC)

| Tool | Token impact | Evidence |
|---|---|---|
| `cs search` (embeddings) | **+14.5% savings** on conceptual queries | Agents read 27 files instead of 38 |
| `symgrep list` (AST search) | **-76.6%** (harmful) | Bash overhead makes it slower than Grep |
| `symgrep extract` (AST extraction) | **+9.7% savings** on symbol queries (v3) | Avoids reading full file |
| Grep (native tool) | Already optimal for lexical/reference search | Models use it without being told |
| Cross-file navigation | Not available — biggest unsolved cost | C1 query: 51-71K tokens tracing flows |

### What agents need (ranked by token impact)

1. **Semantic search** — find relevant files for "how does X work?" → `cs search` (exists)
2. **Cross-file navigation** — "who calls X?", "what implements Y?" → nothing today
3. **Surgical extraction** — read one symbol from a 500-line file → `symgrep extract` (exists, separate CLI)
4. **Text search** — find identifiers, patterns → Grep (already optimal, no tool needed)

## Requirements

### R1: `cs extract` — absorb symgrep extraction

Absorb symgrep's `extract` subcommand into cs. Uses tree-sitter AST parsing (same as symgrep today).

```
cs extract -f <file> -s <symbol>
cs extract -f <file> -s <symbol> --format json
```

**Behavior:**
- Parse file with tree-sitter, find the named symbol, return its source code
- Support same languages as symgrep: Go, Python, Java, JS/TS, Rust, C++, XML, HTML
- Output formats: `raw` (default — just the code) and `json` (with line numbers, byte offsets)
- When file is a directory, search recursively (same as symgrep's directory mode)

**Why in cs:** Agents already have `cs` in their workflow. Adding `extract` to cs eliminates the need to install and configure a second tool. The tree-sitter dependency already exists in cs (used by the AST-aware splitter).

**Migration:** symgrep continues to work standalone. cs extract is a superset. Document in symgrep README that cs is the recommended path for new users.

### R2: `cs refs` — find all references (LSP)

Find all references to a symbol across the project. Uses a language server for precision.

```
cs refs <symbol>                          # search current project
cs refs <symbol> --path <dir>             # search specific directory
cs refs <symbol> --kind method            # filter by symbol kind
```

**Output (optimized for LLM consumption):**
```
BuyerProcessInteractorImpl.java:142  →  saveDueDiligence(userContext, dueDiligenceDTO)
FinalCheckInteractorImpl.java:98     →  saveDueDiligence(userContext, dueDiligenceDTO)
SupplierProcessInteractorImpl.java:85 → saveDueDiligence(userContext, dueDiligenceDTO)
3 references found
```

**Why LSP over Grep:**
- Grep `saveDueDiligence` matches comments, strings, and unrelated methods with the same name
- LSP resolves the actual binding — only returns call sites of *this specific* method
- Benchmark estimate: would reduce conceptual query costs by 30-50% (27 tool calls → 3-5)

**LSP lifecycle:**
- First `cs refs` call starts the language server as a background daemon in the same runtime as `cs` (same host/container), communicating over stdio JSON-RPC
- Subsequent calls reuse the running server (fast)
- Server auto-stops after idle timeout (configurable, default 10 minutes)
- Lifecycle state is stored under `${CODESIGHT_STATE_DIR:-~/.codesight}`
- Lifecycle state is scoped per `(workspace root, language)` and stale PID/session state is auto-recovered
- If the required language server binary is missing, `cs refs` fails non-zero with actionable install guidance that includes the missing binary name

### R3: `cs callers` — call hierarchy (LSP)

Trace the call chain for a method — who calls it, and who calls the callers.

```
cs callers <symbol>                       # immediate callers
cs callers <symbol> --depth 2             # 2 levels deep
```

**Output:**
```
saveDueDiligence (BaseDueDiligenceInteractorImpl.java:106)
  ← completeDataInputStep (BuyerProcessInteractorImpl.java:142)
    ← BuyerController.completeDataInput (BuyerController.java:67)
  ← completeSupplierStep (SupplierProcessInteractorImpl.java:194)
    ← SupplierProcessController.complete (SupplierProcessController.java:88)
  ← completeDueDiligence (FinalCheckInteractorImpl.java:215)
    ← FinalCheckController.completeFinalCheck (FinalCheckController.java:54)
6 callers (depth 2)
```

**Why this matters:** The C1 benchmark query ("how does DD flow work end-to-end?") cost 51-71K tokens because the agent had to manually trace the flow across 10+ files. `cs callers startDueDiligenceProcess --depth 3` would give the full flow in one call.

`cs callers` uses the same lifecycle/state model as `cs refs` and fails fast with actionable install guidance (including missing binary name) when LSP is unavailable.

### R4: `cs implements` — type hierarchy (LSP, stretch goal)

Find all implementations of an interface.

```
cs implements DueDiligenceHandler
```

**Output:**
```
BuyerProcessInteractorImpl (dd-backend/.../process/BuyerProcessInteractorImpl.java)
SupplierProcessInteractorImpl (dd-backend/.../supplier/SupplierProcessInteractorImpl.java)
AssessmentInteractorImpl (dd-backend/.../process/assessment/AssessmentInteractorImpl.java)
FinalCheckInteractorImpl (dd-backend/.../process/FinalCheckInteractorImpl.java)
BaseDueDiligenceInteractorImpl (dd-backend/.../duediligence/BaseDueDiligenceInteractorImpl.java)
5 implementations
```

Stretch goal — implement after R2/R3 are validated.

### R5: Docker runtime compatibility (LSP)

Make LSP navigation Docker-friendly without introducing remote LSP networking in v2.

**Behavior:**
- `cs` launches language servers as child processes in the same runtime as the CLI (host process or container process), not over remote TCP
- `host.docker.internal` remains for Milvus/Ollama network access only
- LSP state root is configurable via `CODESIGHT_STATE_DIR` (default: `~/.codesight`)
- State keys are scoped per `(workspace root, language)` to avoid collisions in multi-repo and multi-language usage
- Stale PID/session state must be detected and repaired automatically
- In ephemeral containers, cold start is acceptable; with a mounted state volume, warm reuse is expected and recommended

## Language server strategy

### Supported languages (initial)

| Language | LSP server | Notes |
|---|---|---|
| Java | Eclipse JDT LS | Most common in target codebases; requires project build files (gradle/maven) |
| TypeScript/JavaScript | tsserver | Ships with Node.js |
| Go | gopls | Already standard in Go ecosystem |
| Python | pylsp / pyright | pyright preferred for type accuracy |

### How it works

1. `cs refs` detects the project language from file extensions and build files
2. If the LSP for that language is installed in the current runtime, cs starts it as a background child process
3. cs communicates via JSON-RPC over stdin/stdout (standard LSP protocol)
4. Results are formatted into the compact output format shown above
5. Server stays alive for subsequent calls, keyed by `(workspace root, language)`; killed after idle timeout
6. Lifecycle state is read/written under `${CODESIGHT_STATE_DIR:-~/.codesight}` to allow optional persistence in containerized runs

### Fallback

If no LSP is available for the project language, `cs refs` falls back to Grep-based reference finding (same accuracy as today, but wrapped in the cs CLI for consistency). Output includes a note: `(grep-based — install <lsp> for precise results)`.

### Docker Runtime Model

- `cs` runs language servers in-process as child executables inside the same container where `cs` runs.
- Language servers must therefore be installed in that container image/runtime.
- `host.docker.internal` is for network services only (`Milvus`, `Ollama`), not for LSP.
- Persisting `${CODESIGHT_STATE_DIR:-~/.codesight}` via a Docker volume is recommended for warm-start performance but not required for correctness.

## Architecture

```
cmd/cs/
├── main.go               # existing — add extract, refs, callers subcommands
pkg/
├── searcher.go            # existing — embedding search
├── indexer.go             # existing — indexing pipeline
├── extract/
│   ├── extract.go         # tree-sitter symbol extraction (from symgrep)
│   └── languages.go       # language → tree-sitter grammar registry
├── lsp/
│   ├── client.go          # LSP JSON-RPC client
│   ├── lifecycle.go       # server start/stop/idle management
│   ├── refs.go            # textDocument/references
│   ├── callers.go         # callHierarchy/incomingCalls
│   └── registry.go        # language → LSP server binary mapping
├── splitter/              # existing
├── embedding/             # existing
├── vectorstore/           # existing
└── walker.go              # existing
```

The `extract` package ports symgrep's parser logic into codesight. The `lsp` package is entirely new.

## CLI summary (v2)

| Command | Source | Action |
|---|---|---|
| `cs index` | existing | Index codebase into vector store |
| `cs search` | existing | Semantic search via embeddings |
| `cs status` | existing | Show index status |
| `cs clear` | existing | Remove index |
| `cs extract` | new (from symgrep) | Extract a symbol from a file via tree-sitter |
| `cs refs` | new (LSP) | Find all references to a symbol |
| `cs callers` | new (LSP) | Show call hierarchy |
| `cs implements` | new (LSP, stretch) | Show type hierarchy |

## Agent instructions (v2)

```markdown
# Tool Selection

- Search → Grep. Always start here for text, identifiers, patterns, class names.
- Understand → cs search "<query>" via Bash. Use for conceptual questions when you don't know which files matter.
- Navigate → cs refs <symbol> or cs callers <symbol> via Bash. Use to trace references and call chains across files.
- Extract → cs extract -f <file> -s <symbol> via Bash. Use instead of Read when you need one symbol from a file >200 lines.
- Find files → Glob.

Do NOT use cs search for exact-match lookups.
Do NOT read 5+ files to understand a feature — cs search and cs callers give you the map.
```

## Phasing

| Phase | Scope | Estimated effort |
|---|---|---|
| **Phase 1** | `cs extract` — port symgrep extraction into cs | Small — code exists, needs packaging |
| **Phase 2** | `cs refs` — LSP client + reference finding + Docker-safe lifecycle state | Medium — LSP protocol, server lifecycle |
| **Phase 3** | `cs callers` — call hierarchy via LSP | Small — builds on Phase 2 LSP client |
| **Phase 4** | `cs implements` — type hierarchy (stretch) | Small — builds on Phase 2 |

Phase 1 can ship independently. Phases 2-3 ship together (LSP infra + both commands). Phase 4 follows.

## Success metrics

Rerun the benchmark (same 8 search queries, SDB2Cloud 250K LOC, Sonnet) with v2 instructions:

| Metric | Current (v3 instructions) | Target (v2 with LSP) |
|---|---|---|
| Conceptual query tokens (C1) | 51,214 | <30,000 (cs callers reduces exploration) |
| Symbol query tokens (S1) | 11,394 | <8,000 (cs extract avoids full Read) |
| Overall savings vs baseline | +2.0% | >+15% |
| Cross-file edit correctness | 4/4 (100%) | 4/4 (maintain) |

## Non-goals

- **MCP server** — cs stays a CLI tool invoked via Bash. No permanent context overhead.
- **Replacing Grep** — models already use Grep optimally. cs doesn't compete on text search.
- **IDE integration** — cs is for headless AI agents, not editor plugins.
- **Remote/cloud index** — local-first only (Milvus + filesystem).
- **Remote/TCP LSP mode** — out of scope for v2; LSP runs as local child processes.
