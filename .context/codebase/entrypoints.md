- **Purpose**: Inventory of executable entrypoints and their side effects/dependencies.
- **Last verified**: 2026-03-17
- **Source**: mixed

## Runtime Entrypoints

| Type | Location | Entrypoint | Behavior | Source |
|---|---|---|---|---|
| CLI binary | `cmd/cs/main.go` | `main()` -> Cobra `rootCmd.Execute()` | primary runtime surface | observed |
| Integration script | `scripts/test-integration.sh` | shell script | starts Milvus container + runs tagged integration test | observed |

## CLI Command Surface

| Command | Handler | Core dependencies | External dependency profile | Source |
|---|---|---|---|---|
| `index [path]` | `runIndex` | `pkg.Indexer`, splitter, ignore matcher | Milvus + Ollama + optional Git metadata | observed |
| `search <query>` | `runSearch` | `pkg.Searcher`, ignore matcher | Milvus + Ollama | observed |
| `extract -f -s` | `runExtract` | `pkg/extract` | local filesystem + tree-sitter libs | observed |
| `refs <symbol>` | `runRefs` | `pkg/lsp` refs engine | LSP preferred; grep fallback if unavailable | observed |
| `callers <symbol>` | `runCallers` | `pkg/lsp` callers engine | requires LSP binary; no grep fallback | observed |
| `implements <symbol>` | `runImplements` | `pkg/lsp` implements engine | requires LSP binary; no grep fallback | observed |
| `status [path]` | `runStatus` | metadata/staleness checks | Milvus + optional Git metadata | observed |
| `clear [path]` | `runClear` | collection existence/drop | Milvus (destructive operation) | observed |
| `completion` | Cobra-generated | shell completion script generation | no network dependency | observed |

## Non-Entrypoints (Explicitly Not Present)
- [observed] No HTTP server entrypoints in production code.
- [observed] No SOAP endpoints, message listeners, or scheduler/cron runtime entrypoints.
- [observed] No migration CLI or schema migration runner.

## Safety Notes for Agents
- [observed] `clear` drops collections and is destructive.
- [observed] `index/search/status/clear` are network-dependent and can fail when Milvus/Ollama are absent.
- [reported] Team reports no shared production `cs` index currently; destructive commands still require explicit task intent.
- [observed] `refs` precision depends on LSP availability (LSP results vs grep fallback).
- [observed] `callers` and `implements` fail without language server binaries.
