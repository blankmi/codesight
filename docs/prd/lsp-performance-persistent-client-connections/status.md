# Status: LSP Performance — Persistent Client Connections

**Phase**: IMPROVEMENT

## Planning

| Item | Status | Notes |
|---|---|---|
| gap_analysis.md | DONE | Updated with resolved platform/config decisions |
| spec.md | DONE | FR/NFR updated with resolved open-question answers |
| plan.md | DONE | Task sequence remains valid for all three phases |
| tasks/ | DONE | Task specs updated with concrete resolved constraints |
| open_questions.md | DONE | All previously blocking questions marked RESOLVED |

## QA

| Gate | Status | Notes |
|---|---|---|
| Build gate (`go build ./...`) | DONE | Executed and passing across merged tasks |
| Test gate (`go test ./...`) | DONE | Executed and passing across merged tasks |
| Project checks (`make build`, `make lint`) | DONE | Executed and passing across merged tasks |
| Contract regression checks | DONE | refs/callers/implements output/error invariants validated in task reviews |

## Tasks

| ID | Title | Status | Depends On |
|---|---|---|---|
| TK-001 | Extend lifecycle state model for daemon metadata | DONE | — |
| TK-002 | Implement daemon server and proxy runtime | DONE | TK-001 |
| TK-003 | Implement daemon client connector and recovery | DONE | TK-002 |
| TK-004 | Wire refs/callers/implements to daemon connector | DONE | TK-003 |
| TK-005 | Add configurable daemon idle timeout | DONE | TK-002 |
| TK-006 | Add Java build-change detection and Gradle sync suppression | DONE | TK-003 |
| TK-007 | Add warmup command, index warmup hook, and tip behavior | DONE | TK-004, TK-006 |

## Improvement

| Reflect | Curate | Promote |
|---|---|---|
| DONE | DONE | DONE |
