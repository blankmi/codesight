# Spec: codesight v2 Unified Code Intelligence CLI

## Overview

### Goal
Unify conceptual search, surgical symbol extraction, and cross-file code navigation in one CLI (`cs`) so agents can answer conceptual and symbol-tracing questions with fewer tool calls and fewer tokens.

### In-scope
- `cs extract` (tree-sitter symbol extraction, file and directory modes).
- `cs refs` (LSP-first reference search with grep fallback).
- `cs callers` (LSP call hierarchy traversal).
- LSP process lifecycle management for daemon reuse and idle shutdown.
- Docker-compatible LSP runtime behavior (in-container child processes, configurable state directory, scoped state reuse).
- Command/test/docs updates needed to ship the above while preserving existing behavior.

### Out-of-scope
- Replacing Grep for lexical search.
- MCP/server mode for `cs`.
- IDE plugin integrations.
- Remote/cloud indexing mode.
- Mandatory delivery of `cs implements` in this epic (stretch only).

### Clarifications (Resolved)
- OQ-001: symbol lookup is name-only in v2 with deterministic ambiguity errors and candidate listing.
- OQ-003: `cs callers` and `cs implements` fail fast when required LSP binaries are unavailable; only `cs refs` has grep fallback.
- OQ-004: extraction language scope is exactly `go`, `python`, `java`, `javascript`, `typescript`, `rust`, `cpp`, `xml`, `html`; C is out of scope in v2.
- OQ-005: `cs refs --kind` enum is fixed to `function|method|class|interface|type|constant`.

## Functional Requirements

### FR-01: `cs extract` CLI contract
- Preconditions:
  - User invokes `cs extract` with `-f <file-or-dir>` and `-s <symbol>`.
  - Target path exists and is readable.
- Behavior:
  - Command signature:
    - `cs extract -f <file> -s <symbol>`
    - `cs extract -f <file> -s <symbol> --format json`
  - `--format` supports exactly `raw` (default) and `json`.
  - Invalid/missing required flags return non-zero exit with actionable error text.

### FR-02: Symbol extraction for file targets
- Preconditions:
  - `-f` points to a supported source file.
- Behavior:
  - Parse file with tree-sitter and resolve named symbol.
  - Supported languages are exactly: Go, Python, Java, JavaScript, TypeScript, Rust, C++, XML, HTML.
  - C is not a required extraction language in v2.
  - `raw` output returns only symbol source code.
  - `json` output returns one object with fields:
    - `name`, `code`, `file_path`, `start_line`, `end_line`, `start_byte`, `end_byte`, `symbol_type`.
  - Symbol-not-found returns non-zero exit and a message containing `symbol not found`.

### FR-03: Symbol extraction for directory targets
- Preconditions:
  - `-f` points to a directory.
- Behavior:
  - Recursively scan supported source files for FR-02 languages.
  - Exclude common non-source/tooling directories (`.git`, `node_modules`, `vendor`, `__pycache__`, `.idea`, `.vscode`).
  - Stable deterministic path ordering across runs.
  - `raw` output prints each match grouped by file with header pattern:
    - `=== file: <path> ===`
  - `json` output returns an array of match objects with FR-02 fields.

### FR-04: `cs refs` CLI contract and LSP references
- Preconditions:
  - User invokes `cs refs <symbol>`; optional `--path <dir>` and `--kind <kind>`.
  - Target workspace path is readable.
- Behavior:
  - Command signature:
    - `cs refs <symbol>`
    - `cs refs <symbol> --path <dir>`
    - `cs refs <symbol> --kind <kind>`
  - Uses LSP `workspace/symbol` to resolve symbol by name before reference lookup.
  - If exactly one symbol definition matches, proceed with reference lookup.
  - If multiple symbol definitions match, exit non-zero and print deterministic candidate list sorted by file path.
  - Ambiguity error contract is exact: `ambiguous symbol "<name>" — <N> definitions found. Use --path to narrow scope.`
  - v2 does not support qualified symbol syntax (for example `pkg.Class.method`); symbol input is name-only.
  - Output is LLM-optimized line format:
    - `<relative-path>:<line>  ->  <code-snippet>`
    - Final summary line: `<N> references found`
  - `--kind` applies post-resolution filtering using this fixed enum: `function`, `method`, `class`, `interface`, `type`, `constant`.
  - `--kind` input matching is case-insensitive and normalized to lowercase.
  - Invalid `--kind` values fail non-zero with exact message:
    - `invalid kind "<value>" — allowed: function, method, class, interface, type, constant`

### FR-05: `cs refs` fallback when LSP unavailable
- Preconditions:
  - LSP server for detected project language is unavailable or cannot be started.
- Behavior:
  - Fallback to grep-based reference search.
  - Emit explicit precision note containing:
    - `(grep-based - install <lsp> for precise results)`
  - Keep output format compatible with FR-04 for downstream parsing.

### FR-06: `cs callers` CLI contract and call hierarchy
- Preconditions:
  - User invokes `cs callers <symbol>` with optional `--path <dir>` and `--depth <N>`.
- Behavior:
  - Command signature:
    - `cs callers <symbol>`
    - `cs callers <symbol> --path <dir>`
    - `cs callers <symbol> --depth 2`
  - Symbol disambiguation follows FR-04 name-only contract and ambiguity error output.
  - Default depth is `1`; depth must be positive integer.
  - Uses LSP call hierarchy APIs to return incoming callers recursively to requested depth.
  - No grep fallback is allowed for `callers`; when required LSP binary is unavailable, fail non-zero with exact format:
    - `cs callers: LSP required but <binary> not found. Install: <install command>`
  - Output format:
    - Root line: `<symbol> (<file>:<line>)`
    - Child lines prefixed by two-space indentation per depth and `<-` marker.
    - Final summary line: `<N> callers (depth <D>)`

### FR-07: LSP lifecycle and registry behavior
- Preconditions:
  - `refs` or `callers` command is invoked.
- Behavior:
  - On first invocation for a `(workspace root, language)` tuple, start language server as background process.
  - Reuse live process for subsequent calls.
  - Auto-stop after idle timeout; default is 10 minutes.
  - Persist PID/state metadata under `${CODESIGHT_STATE_DIR:-~/.codesight}`.
  - Detect and recover stale PID/session metadata automatically.
  - Registry maps language to executable and launch args.
  - When required LSP binaries are missing, commands fail non-zero with actionable install guidance that includes the missing binary name.

### FR-08: `cs implements` (stretch)
- Preconditions:
  - FR-04/FR-06 infra is complete and validated.
- Behavior:
  - Provide `cs implements <symbol>` to list type/interface implementations.
  - Symbol disambiguation follows FR-04 name-only contract and ambiguity error output.
  - No grep fallback is allowed for `implements`; when required LSP binary is unavailable, fail non-zero with exact format:
    - `cs implements: LSP required but <binary> not found. Install: <install command>`
  - Output format:
    - `<TypeName> (<path>)`
    - Final summary line: `<N> implementations`
- Notes:
  - Stretch; may ship in a later increment without blocking v2 base delivery.

### FR-09: Migration and docs
- Preconditions:
  - v2 commands are available in CLI.
- Behavior:
  - Update README and agent instructions to represent v2 command matrix.
  - Document that `cs extract` is recommended path for extraction in this project.
  - Preserve explicit guidance that Grep remains preferred for lexical search.

### FR-10: Docker runtime compatibility for LSP commands
- Preconditions:
  - `cs refs` or `cs callers` is executed in a containerized runtime.
- Behavior:
  - LSP server processes are launched as child processes in the same runtime as `cs` over stdio JSON-RPC (no remote/TCP LSP mode in v2).
  - `host.docker.internal` is treated as a network endpoint for Milvus/Ollama only, not as an LSP transport.
  - State-directory persistence (via `${CODESIGHT_STATE_DIR:-~/.codesight}` volume mounts) is recommended for warm-start performance but not required for correctness.

## Non-Functional Requirements

### NFR-01: Green build invariant
- Every merged task must leave repository in a passing state for:
  - `go build ./...`
  - `go test ./...`

### NFR-02: Backward compatibility
- Existing commands (`index`, `search`, `status`, `clear`) keep existing CLI contracts and behavior.
- Existing environment-variable based config for indexing/search remains intact.

### NFR-03: Deterministic output
- Directory scan and multi-match outputs are stable across runs given unchanged inputs.
- Reference and caller outputs are deterministically ordered (location order preferred).
- Ambiguity candidate lists are sorted by file path for stable reruns.

### NFR-04: Performance and responsiveness
- LSP server startup overhead is amortized by daemon reuse.
- Idle timeout cleanup prevents indefinite background process accumulation.
- In ephemeral containers without persisted state, cold-start overhead is acceptable; correctness must remain intact.

### NFR-05: Error quality and debuggability
- All user-facing failures include actionable text (missing binary, unsupported language, symbol missing, invalid flag values).
- Error messages preserve critical substring contracts required by tests.

### NFR-06: Security/process hygiene
- LSP processes are launched without shell interpolation vulnerabilities.
- PID/state files are written with user-only permissions where applicable.

### NFR-07: Docker compatibility
- LSP behavior is consistent between host and container runtimes.
- Scoped state keys (`workspace`, `language`) avoid collisions across mounted repos in shared containers.
- Missing-binary errors are explicit enough for container image hardening and CI diagnosis.

## Success (Definition of Done)
- FR-01 through FR-07 are implemented and verified.
- FR-10 Docker runtime compatibility requirements are implemented and verified.
- FR-08 is either completed or explicitly deferred as stretch with no regressions to FR-01..FR-07 and FR-10.
- FR-09 docs are updated to reflect shipping scope.
- All required task docs are completed and linked to FR IDs.
- Docker smoke criteria are satisfied:
  - persisted `${CODESIGHT_STATE_DIR}` volume path demonstrates warm reuse behavior.
  - ephemeral (non-persisted) state path demonstrates correct cold-start behavior.
- For every task and final epic verification:
  - `go build ./...` succeeds.
  - `go test ./...` passes.
