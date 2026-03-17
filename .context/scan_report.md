- **Purpose**: Phase 1 repository scan summary for Archaeologist context bootstrap.
- **Last verified**: 2026-03-17
- **Source**: mixed

## 1.1 Build & Tooling
- [observed] Build system is Go modules + Makefile (`go.mod`, `Makefile`).
- [observed] Primary targets: `build`, `test`, `test-integration`, `lint`, plus cross-compile targets (`build-linux-amd64`, `build-darwin-*`).
- [observed] Dependency management is `go.mod` + `go.sum`; no multi-module workspace (`go.work`) and no convention-plugin layer.
- [verified] `make build` passes and produces `bin/cs`.
- [verified] `go test ./...` and `make test` pass.
- [verified] `make lint` fails on baseline (`errcheck` in tests and `unused` helper in `cmd/cs/callers_command_test.go`).
- [verified] `bin/cs --help` runs successfully.

## 1.2 Module Structure
- [observed] Single Go module: `github.com/blankbytes/codesight`.
- [observed] Runtime packages: `cmd/cs`, `pkg`, `pkg/embedding`, `pkg/extract`, `pkg/ignore`, `pkg/lsp`, `pkg/splitter`, `pkg/vectorstore`.
- [observed] File concentration: `pkg` (55 files), `docs` (18), `cmd` (6), `.prompts` (9), `agent-skills` (6).
- [observed] Dependency flow (`go list -f ... ./...`): `cmd/cs` -> `pkg`/`pkg/*`; `pkg` -> `embedding/splitter/vectorstore/ignore`; `pkg/lsp` -> `pkg/ignore`; `pkg/extract` -> `pkg/ignore`.

## 1.3 Entrypoints
- [observed] Primary runtime entrypoint: `cmd/cs/main.go` (`main()` + Cobra command tree).
- [observed] CLI commands: `index`, `search`, `extract`, `refs`, `callers`, `implements`, `status`, `clear`, plus `completion`.
- [observed] Integration entrypoint: `scripts/test-integration.sh` (starts Milvus in Docker, runs tagged integration test).
- [observed] No HTTP/SOAP/message/scheduler runtime entrypoints found in production code (`rg` pattern scan).

## 1.4 Data & Persistence
- [observed] Vector persistence uses Milvus (`pkg/vectorstore/milvus.go`).
- [observed] Embedding provider is Ollama HTTP API (`pkg/embedding/ollama.go`).
- [observed] LSP lifecycle state persists to `${CODESIGHT_STATE_DIR:-~/.codesight}` (`pkg/lsp/lifecycle.go`).
- [observed] No schema migration framework/files (Flyway/Liquibase/SQL migration streams) present.

## 1.5 API Surface
- [reported] Only CLI is considered stable public API; early-stage development may include CLI breaking changes if task-scoped.
- [reported] `pkg/*` APIs are not treated as stable external contracts.
- [observed] No REST/gRPC/SOAP server API surface in repo.

## 1.6 UI / Frontend Patterns
- [observed] Project is CLI-only; no frontend framework, SPA, or server-side template UI.
- [observed] `/docs/frontend/` is not present.

## 1.7 Quality Gates
- [observed] Local gates defined in `Makefile`: build, test, lint, integration test.
- [verified] Unit gate (`go test ./...`) passes.
- [verified] Lint gate currently fails on baseline issues.
- [verified] Docker CLI is unavailable in this environment (`docker: command not found`), so integration gate could not be executed here.
- [reported] Policy: all available tests should pass before merging unless explicitly stated otherwise.

## 1.8 CI/CD
- [observed] Only in-repo workflow is `.github/workflows/release.yml`.
- [observed] Workflow triggers on `v*` tags and manual dispatch, builds Linux/macOS artifacts, and publishes GitHub Release assets.
- [verified] Git remote is GitLab (`origin https://gitlab.dev.evia.de:4443/...`).
- [reported] Agents must not merge branches; CK orchestrator manages branch lifecycle.
- [reported] Release pipeline is no-go for agents unless explicitly task-scoped.

## 1.9 Protected Paths (expanded)
- [reported] Protected: `docs/**`, `.prompts/**`, `specs/**`.
- [observed] Protected release infra: `.github/workflows/release.yml`.
- [observed] Generated artifacts path: `bin/**`.
- [reported] `agent-skills/**` is tool-owned and editable only for explicitly scoped skill/tooling tasks.

## 1.10 Conventions & Patterns
- [observed] Command pattern: parse/wire in `cmd/cs`, delegate behavior to `pkg/*` engines.
- [reported] New command logic should be split into separate `cmd/cs/*.go` files (reduce `main.go` hotspot growth).
- [observed] Error handling convention uses contextual wrapping (`fmt.Errorf("...: %w", err)`).
- [observed] Runtime configuration is environment-variable based (`CODESIGHT_*`).
- [observed] Ignore handling must go through `pkg/ignore` matcher APIs.
- [observed] LSP contract: `refs` may fallback to grep; `callers` and `implements` fail fast without LSP.

## 1.11 Hotspots & Risk
- [observed] Files >500 LOC: `cmd/cs/main.go` (1145), `pkg/extract/extract.go` (643), `cmd/cs/callers_command_test.go` (534), `pkg/lsp/refs_test.go` (514), `cmd/cs/refs_command_test.go` (513), `pkg/lsp/client.go` (511).
- [observed] `pkg/vectorstore` has zero `_test.go` files.
- [reported] For `pkg/vectorstore` behavior changes, tests should be added first.
- [verified] Dependency drift exists (`grpc`, `x/net`, `x/sys`, `x/text`, `protobuf` behind latest; `github.com/golang/protobuf` flagged deprecated).
- [observed] Active-work mismatch risk: `docs/prd/.../status.md` is all `PENDING` while code already includes related features.
