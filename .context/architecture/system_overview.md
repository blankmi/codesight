- **Purpose**: High-level architecture map for safe feature placement and change scoping.
- **Last verified**: 2026-03-17
- **Source**: mixed

## Identity
- [reported] Project name: `Codesight`.
- [observed] Runtime type: single-binary Go CLI (`cmd/cs/main.go`) with reusable engines under `pkg/*`.
- [reported] Stable public surface: CLI only.
- [reported] `pkg/*` packages are not treated as stable external API contracts.

## Layer Model

```text
User shell / agent
  -> cmd/cs (Cobra command layer)
    -> pkg (index/search orchestration)
      -> pkg/splitter + pkg/walker + pkg/ignore (file discovery/chunking)
      -> pkg/embedding (Ollama embedding client)
      -> pkg/vectorstore (Milvus persistence)
    -> pkg/extract (tree-sitter symbol extraction)
    -> pkg/lsp (refs/callers/implements over JSON-RPC stdio)
      -> external language servers (gopls, pylsp, jdtls, tsserver, rust-analyzer, clangd)
```

- [observed] Command wiring remains centralized in `cmd/cs/main.go`.
- [observed] `pkg/indexer.go` orchestrates walk -> split -> embed -> insert.
- [observed] `pkg/searcher.go` orchestrates query embed -> vector search -> formatted output.
- [observed] `pkg/extract` and `pkg/lsp` are engine layers consumed by CLI handlers.

## External Integrations

| Integration | Usage | Runtime impact | Source |
|---|---|---|---|
| Milvus | semantic vector storage | index/search/status/clear fail without connectivity | observed (`pkg/vectorstore/milvus.go`, `cmd/cs/main.go`) |
| Ollama | embedding generation | index/search fail when endpoint/model unavailable | observed (`pkg/embedding/ollama.go`) |
| Language servers | refs/callers/implements | `refs` may fallback to grep; callers/implements fail fast | observed (`pkg/lsp/*.go`, `cmd/cs/main.go`) |
| Git executable | commit metadata for index/status | degraded metadata/staleness checks if unavailable | observed (`cmd/cs/main.go`) |
| Docker (test-only) | Milvus integration harness | integration script cannot run without Docker daemon | observed (`scripts/test-integration.sh`) |

## State and Data Boundaries
- [observed] Collection naming is path-hash based (`pkg/version.go`).
- [observed] Index metadata stores branch/commit/ignore fingerprint/file+chunk counts (`pkg/vectorstore/store.go`).
- [observed] Ignore fingerprint combines defaults + `.gitignore` + `.csignore` + extra patterns (`pkg/ignore/matcher.go`).
- [observed] LSP lifecycle metadata is persisted under `${CODESIGHT_STATE_DIR:-~/.codesight}` (`pkg/lsp/lifecycle.go`).

## Dominant Architectural Patterns
- [observed] Command-handler split: CLI parsing/wiring in `cmd/cs`; behavior in `pkg/*`.
- [observed] Deterministic output and error text are test-locked in command and engine tests.
- [observed] Fail-safe LSP behavior: grep fallback for `refs`; strict dependency on LSP for `callers`/`implements`.
- [observed] Environment-first runtime configuration via `CODESIGHT_*` variables.

## Open Items
- [observed] `TODO:` identify authoritative non-release CI gate source for GitLab-hosted repo.
