- **Purpose**: Module/package inventory, dependency graph, and placement rules for safe edits.
- **Last verified**: 2026-03-17
- **Source**: mixed

## Package Inventory

| Module/Path | Role | Approx files | Tests | Notes | Source |
|---|---|---:|---:|---|---|
| `cmd/cs` | CLI entrypoint and command wiring | 6 | 5 | `main.go` is main hotspot (1145 lines) | observed |
| `pkg` | Core indexing/search orchestration and helpers | 55 | 16 | Includes integration-tag test | observed |
| `pkg/embedding` | Embedding interface + Ollama client | 3 | 1 | Network-bound embedding behavior | observed |
| `pkg/vectorstore` | Vector store abstraction + Milvus implementation | 2 | 0 | Runtime-critical, currently no direct tests | observed |
| `pkg/splitter` | AST/fallback chunking + language mapping | 5 | 2 | Shared across index + LSP | observed |
| `pkg/ignore` | Unified ignore matcher + fingerprinting | 2 | 1 | Shared across index/search/extract/lsp | observed |
| `pkg/extract` | Symbol extraction engine (`raw`/`json`) | 8 | 1 | Tree-sitter multi-language extraction | observed |
| `pkg/lsp` | LSP registry/lifecycle/client/refs/callers/implements | 16 | 7 | Navigation subsystem with external LS deps | observed |
| `scripts` | Ops scripts (integration test harness) | 1 | n/a | Docker-dependent script | observed |
| `.github/workflows` | Release automation | 1 | n/a | Tag/manual release only | observed |
| `.prompts` | Agent workflow prompt contracts | 9 | n/a | Coordination-sensitive | observed |
| `docs` | Product/PRD/planning docs | 18 | n/a | Coordination-sensitive | observed |
| `specs` | Context-kernel runtime specs | 1 | n/a | Orchestration-sensitive | observed |

## Internal Dependency Graph

```text
cmd/cs
  -> pkg
    -> pkg/embedding
    -> pkg/vectorstore
    -> pkg/splitter
    -> pkg/ignore
  -> pkg/extract
    -> pkg/ignore
  -> pkg/lsp
    -> pkg/ignore
    -> pkg/splitter
```

- [observed] Graph derived from `go list -f '{{.ImportPath}}|{{join .Imports ","}}' ./...`.

## File Placement Rules (Agents Must Follow)
- [reported] New command logic should be split into dedicated `cmd/cs/*.go` files instead of expanding `cmd/cs/main.go`.
- [observed] CLI parsing/flag wiring belongs in `cmd/cs`; behavior implementations belong in `pkg/*`.
- [observed] Index/search orchestration changes belong in `pkg/indexer.go` and `pkg/searcher.go`, not command handlers.
- [observed] Embedding-provider logic belongs in `pkg/embedding`; vector-backend logic belongs in `pkg/vectorstore`.
- [observed] Ignore behavior changes belong only in `pkg/ignore`; call sites must consume matcher APIs.
- [observed] Symbol extraction behavior belongs in `pkg/extract`; avoid leaking CLI concerns into extractor engine.
- [observed] LSP transport/lifecycle/feature behavior belongs in `pkg/lsp`; CLI should pass options and print outputs only.
- [observed] Integration automation belongs in `scripts/`; release automation belongs in `.github/workflows/`.

## Packaging and Artifact Model
- [observed] Packaging type is a single executable binary (`bin/cs`) built from `./cmd/cs`.
- [observed] Cross-build artifacts: `bin/cs-linux-amd64`, `bin/cs-darwin-amd64`, `bin/cs-darwin-arm64`.

## Placement Guardrails
- [reported] Treat `docs/**`, `.prompts/**`, and `specs/**` as protected unless task explicitly requires edits.
- [observed] Treat `.github/workflows/release.yml` and `bin/**` as protected in routine tasks.
- [reported] `agent-skills/**` is tool-owned; edit only for explicitly scoped skill/tooling work.
