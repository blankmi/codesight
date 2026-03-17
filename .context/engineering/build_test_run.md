- **Purpose**: Canonical build/test/run workflow and developer environment requirements.
- **Last verified**: 2026-03-17
- **Source**: mixed

## Quick Commands
- [verified] Build: `make build`
- [verified] Unit tests: `go test ./...`
- [verified] Make test wrapper: `make test`
- [verified] Smoke run: `bin/cs --help`
- [verified] Lint: `make lint`
- [observed] Integration tests: `make test-integration` (Docker + Milvus required; skip in agent containers)

## Verified Results (2026-03-17)
- [verified] `make build` passed and produced `bin/cs`.
- [verified] `go test ./...` passed across all packages.
- [verified] `make test` passed.
- [verified] `make lint` passed.
- [verified] `bin/cs --help` passed and listed expected commands.
- [verified] `bin/cs status /workspace/repo` passed and reported stale index metadata (`e3f64f1` vs `bce794b`).
- [verified] `bin/cs index /workspace/repo --branch docs/context-kernel --commit bce794b` failed because Ollama endpoint `127.0.0.1:11434` was unavailable.
- [verified] `docker info` failed (`docker` command not found), so `make test-integration` could not be executed here.

## Prerequisites
- [reported] Minimum Go toolchain is `1.25.7`.
- [observed] Semantic index/search/status/clear require Milvus at `${CODESIGHT_DB_ADDRESS:-localhost:19530}`.
- [observed] Embedding requests require Ollama at `${CODESIGHT_OLLAMA_HOST:-http://127.0.0.1:11434}`.
- [observed] `refs/callers/implements` require language server binaries (`gopls`, `pylsp`, `jdtls`, `typescript-language-server`, `rust-analyzer`, `clangd`).
- [observed] Integration test script requires Docker daemon + CLI.
- [reported] Network-dependent commands are allowed in normal agent runtime containers.

## Environment Variables

| Variable | Purpose | Default | Source |
|---|---|---|---|
| `CODESIGHT_DB_TYPE` | vector store backend | `milvus` | observed (`cmd/cs/main.go`) |
| `CODESIGHT_DB_ADDRESS` | Milvus address | `localhost:19530` | observed |
| `CODESIGHT_DB_TOKEN` | Milvus token | empty | observed |
| `CODESIGHT_OLLAMA_HOST` | Ollama endpoint | `http://127.0.0.1:11434` | observed |
| `CODESIGHT_EMBEDDING_MODEL` | embedding model name | `nomic-embed-text` | observed |
| `CODESIGHT_OLLAMA_MAX_INPUT_CHARS` | max embedding input characters | adaptive/default behavior | observed |
| `CODESIGHT_STATE_DIR` | LSP state root | `${HOME}/.codesight` | observed (`pkg/lsp/lifecycle.go`) |
| `CODESIGHT_GRADLE_JAVA_HOME` | optional Gradle JDK override for jdtls | unset | observed (`cmd/cs/main.go`) |
| `CODESIGHT_INTEGRATION_DB_ADDRESS` | integration test Milvus address | unset | observed (`pkg/integration_milvus_test.go`) |

## Required Validation Sequence for Agent Changes
1. [reported] Run all available checks before merge unless task says otherwise.
2. [observed] Minimum baseline sequence: `go test ./...`, `make build`, `make lint`.
3. [observed] `make test-integration` requires Docker + Milvus and is NOT available in agent containers. Only run on the host machine for Milvus/index/search/vectorstore changes.

## Known Runtime Caveats
- [observed] `refs` may fallback to grep when LSP is unavailable; `callers` and `implements` fail fast without LSP.
- [observed] Integration tests are build-tagged and not part of default `go test ./...`.
- [reported] Team reports no shared production `cs` index currently; destructive index operations still require explicit intent.

## Open Items
- [observed] `TODO:` identify authoritative non-release CI gate source for GitLab-hosted repository.
