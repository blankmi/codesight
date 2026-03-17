- **Purpose**: Dependency baseline, staleness pressure, and dependency-related risks.
- **Last verified**: 2026-03-17
- **Source**: mixed

## Key Dependencies (from `go.mod`)

| Module | Current | Role | Update snapshot | Source |
|---|---|---|---|---|
| `github.com/spf13/cobra` | `v1.10.2` | CLI framework | no update reported in targeted check | mixed |
| `github.com/milvus-io/milvus-sdk-go/v2` | `v2.4.2` | vector DB client | no update reported in targeted check | mixed |
| `github.com/smacker/go-tree-sitter` | `v0.0.0-20240827094217-dd81d9e9be82` | AST parsing/chunking/extract | no update reported in targeted check | mixed |
| `google.golang.org/grpc` (indirect) | `v1.48.0` | Milvus/grpc transport stack | `v1.79.2` available | mixed |
| `google.golang.org/protobuf` (indirect) | `v1.33.0` | protobuf runtime | `v1.36.11` available | mixed |
| `golang.org/x/net` (indirect) | `v0.17.0` | networking utilities | `v0.52.0` available | mixed |
| `golang.org/x/sys` (indirect) | `v0.13.0` | OS primitives | `v0.42.0` available | mixed |
| `golang.org/x/text` (indirect) | `v0.13.0` | text/i18n primitives | `v0.35.0` available | mixed |
| `github.com/golang/protobuf` (indirect) | `v1.5.2` | legacy protobuf compatibility | `v1.5.4` available; module marked deprecated | mixed |

## Tooling Dependencies
- [observed] Lint tool is invoked ad hoc via `golangci-lint@v1.64.5` (`Makefile`).
- [observed] No repo-local `golangci-lint` config file was found (`.golangci.yml/.yaml/.toml` absent).

## Verified Dependency Checks
- [verified] Ran `go list -m -u all` and confirmed broad transitive dependency drift.
- [verified] Ran targeted `go list -m -u` checks for direct and high-risk transitive modules listed above.

## Known Risks
- [verified] Transitive dependency lag is substantial in grpc/protobuf/x/* ecosystem.
- [observed] Deprecated `github.com/golang/protobuf` remains in graph (likely transitive).
- [observed] No automated dependency or vulnerability scanning workflow is defined in repo CI.
- [reported] Dependency upgrade decisions are owned by the engineering team.

## Agent Upgrade Policy
- [reported] Agents may propose dependency upgrades in feedback.
- [reported] Agents should not perform broad unscoped dependency refreshes unless explicitly requested.
- [observed] Keep dependency bumps isolated from feature work to simplify rollback.

## Suggested Upgrade Strategy (future scoped task)
1. [observed] Start with low-blast-radius indirect upgrades (`x/sys`, `x/net`, `protobuf`, `grpc`) in a dedicated branch.
2. [observed] Re-run `go test ./...`, `make build`, `make lint`, and `make test-integration` (when Docker is available) after each upgrade cluster.
3. [observed] Validate Milvus integration behavior before merging dependency changes.
