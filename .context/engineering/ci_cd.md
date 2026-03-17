- **Purpose**: CI/CD topology, trigger rules, environments, and release flow constraints.
- **Last verified**: 2026-03-17
- **Source**: mixed

## Pipeline Inventory
- [observed] Only one workflow file exists: `.github/workflows/release.yml`.
- [observed] No dedicated PR-validation workflow is defined in repo.
- [verified] Git remote is GitLab (`origin https://gitlab.dev.evia.de:4443/...`), while release automation is defined in GitHub Actions.

## Release Workflow (`release.yml`)

| Stage | Trigger | Behavior | Output | Source |
|---|---|---|---|---|
| `build` matrix | tag push `v*` or manual dispatch | builds Linux amd64 + macOS amd64 + macOS arm64 binaries via Make targets | per-platform workflow artifacts | observed |
| `release` | tag refs or manual dispatch with `release_tag` | downloads artifacts, generates SHA256 checksums, publishes GitHub Release | binaries + `checksums.txt` | observed |

- [observed] Workflow permissions: `contents: write`.
- [observed] Manual inputs: `build_ref`, `release_tag`.
- [observed] Linux build installs Zig; macOS build normalizes SDK env.

## Environments and Targets
- [observed] Build runners: `ubuntu-latest`, `macos-latest`.
- [observed] Artifact outputs:
  - [observed] `cs-linux-amd64` -> `bin/cs-linux-amd64`
  - [observed] `cs-darwin-amd64` -> `bin/cs-darwin-amd64`
  - [observed] `cs-darwin-arm64` -> `bin/cs-darwin-arm64`
- [observed] Release publication uses `softprops/action-gh-release@v2`.

## Branch and Merge Policy
- [reported] Agents must not merge branches; CK orchestrator manages branch lifecycle.
- [observed] Release workflow is tag/manual only; no branch gate is codified in this repo.
- [observed] `TODO:` clarify authoritative non-release CI location and required merge checks for GitLab-hosted workflow.

## Agent Guardrails for CI/CD Files
- [reported] Release pipeline is no-go for agents unless task explicitly targets release automation.
- [observed] Treat `.github/workflows/release.yml` as protected in routine implementation tasks.
- [observed] Treat cross-build sections of `Makefile` as release-coupled; avoid incidental edits.
- [observed] Until non-release CI is clarified, use local `go test ./...`, `make build`, and `make lint` as baseline validation.
