# Status: `.codesight/` Project Configuration

**Phase**: IMPROVEMENT

## Planning

| Item | Status |
|------|--------|
| gap_analysis.md | DONE |
| spec.md | DONE |
| plan.md | DONE |
| tasks/ | DONE |
| open_questions.md | DONE (no blockers) |

## QA

| Check | Status |
|-------|--------|
| Tasks compile-safe | VERIFIED |
| Dependency graph acyclic | VERIFIED |
| Backward compatibility addressed | VERIFIED |

## Tasks

| ID | Title | Status | Depends On |
|----|-------|--------|------------|
| TK-001 | Add TOML dependency and create `pkg/config` | DONE | — |
| TK-002 | Wire config loading into CLI and replace env var call sites | DONE | TK-001 |
| TK-003 | Relocate jdtls data to `.codesight/lsp/` with fallback | DONE | TK-002 |
| TK-004 | Add `cs init` command with project-type detection | DONE | TK-001 |
| TK-005 | Add `cs config` command for effective config display | DONE | TK-002 |

## Improvement

| Reflect | Curate | Promote |
|---------|--------|---------|
| DONE | DONE | DONE |

Promote output drafted as an AGENTS rule proposal and is `PENDING_HUMAN_APPROVAL`.
