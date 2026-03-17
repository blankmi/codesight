# Gap Analysis: codesight v2 Unified Code Intelligence CLI

## Current State

### Product behavior observed
- `cs` currently exposes four commands: `index`, `search`, `status`, `clear`.
- Semantic retrieval is implemented via embeddings (`pkg/indexer.go`, `pkg/searcher.go`) and Milvus (`pkg/vectorstore`).
- Tree-sitter is used only for indexing-time chunking (`pkg/splitter/treesitter.go`), not for user-facing symbol extraction.
- Cross-file reference tracing and call hierarchy navigation are not available in `cs`.
- Agent guidance currently recommends using standalone `symgrep extract` for symbol-level reads from large files.

### Code state observed
- CLI wiring lives in `cmd/cs/main.go`; there are no `extract`, `refs`, `callers`, or `implements` subcommands.
- No `pkg/extract` package exists.
- No `pkg/lsp` package exists.
- Existing tree-sitter language parser support in splitter is: Go, TS/JS, Python, Java, Rust, C/C++.
- Existing `LanguageFromExtension` includes many extensions, but parser-backed extraction support is limited to the parser set above.

## Desired State

### Required product behavior from PRD
- Add `cs extract -f <file> -s <symbol>` with `raw` (default) and `json` formats.
- `cs extract` must support file and directory targets and recursively search directories.
- `cs extract` should absorb current `symgrep extract` value and become the recommended default path in this repo.
- Add `cs refs <symbol>` with optional `--path` and `--kind` filters, powered by LSP for precise references.
- `cs refs` must fallback to grep-based results when LSP is unavailable and surface a precision note.
- Add `cs callers <symbol>` with `--depth` for call hierarchy traversal using LSP.
- Add background LSP lifecycle management (startup, reuse, idle shutdown; default idle timeout 10m; PID/state under `~/.codesight`).
- Keep existing commands (`index`, `search`, `status`, `clear`) working without regressions.

### Stretch behavior
- `cs implements <symbol>` (type hierarchy / interface implementation lookup) after refs/callers validation.

## Delta

### Net-new
- `pkg/extract/` package (tree-sitter symbol extraction API and output formatting).
- `pkg/lsp/` package with registry, lifecycle, JSON-RPC client, refs, callers, and optional implements features.
- New CLI command wiring and flags for `extract`, `refs`, `callers` (and optional `implements`).
- New test suites for extraction and LSP flows.
- New docs/migration guidance that repositions standalone `symgrep` as optional while recommending `cs extract`.

### Modified
- `cmd/cs/main.go` and command tests to register and validate new commands.
- `README.md` and agent instructions to reflect v2 tool selection (`cs search`, `cs extract`, `cs refs`, `cs callers`).
- Potential extension/language mapping where required to satisfy extraction language scope.

### Removed / Deprecated
- No runtime command removals in this epic.
- Documentation-level recommendation shift: standalone `symgrep extract` becomes secondary for this codebase.
