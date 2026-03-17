# Open Questions

All previously blocking questions are now resolved.

## OQ-001 (RESOLVED)
- Question: How is `<symbol>` disambiguated when multiple definitions share the same name across files/packages (for `refs`, `callers`, and stretch `implements`)?
- Resolution:
  - v2 uses name-only lookup.
  - Symbol resolution first queries `workspace/symbol` by name.
  - If exactly one definition matches, proceed.
  - If multiple definitions match, return a deterministic candidate list sorted by file path and exit non-zero with:
    - `ambiguous symbol "<name>" — <N> definitions found. Use --path to narrow scope.`
  - `cs callers` also supports `--path` for narrowing.
  - Qualified-name syntax (for example `pkg.Class.method`) is out of scope for v2.
- Impact addressed: FR-04, FR-06, FR-08 and tasks TK-005/TK-007/TK-009.

## OQ-002 (RESOLVED)
- Question: Is LSP lifecycle state global-singleton, per-language, per-workspace, or per-(workspace, language)?
- Resolution: Scope lifecycle state per `(workspace root, language)` under `${CODESIGHT_STATE_DIR:-~/.codesight}` with stale PID/session recovery.
- Impact addressed: FR-07 and tasks TK-003/TK-006/TK-008.

## OQ-003 (RESOLVED)
- Question: When LSP is unavailable, should `cs callers` (and stretch `cs implements`) fallback to grep-like heuristics or fail fast?
- Resolution: Fail fast with actionable install guidance. Only `cs refs` gets grep fallback.
- Error format:
  - `cs callers: LSP required but <binary> not found. Install: <install command>`
  - `cs implements: LSP required but <binary> not found. Install: <install command>`
- Impact addressed: FR-06, FR-08 and tasks TK-007/TK-008/TK-009.

## OQ-004 (RESOLVED)
- Question: What is the exact extraction language scope for v2?
- Resolution: Match symgrep language scope exactly:
  - Go, Python, Java, JavaScript, TypeScript, Rust, C++, XML, HTML.
  - C is out of scope for v2 extraction requirements.
- Impact addressed: FR-02, FR-03 and task TK-001.

## OQ-005 (RESOLVED)
- Question: What is the canonical allowed enum and matching semantics for `cs refs --kind <kind>`?
- Resolution:
  - Allowed kinds: `function`, `method`, `class`, `interface`, `type`, `constant`.
  - Case-insensitive input matching; normalized lowercase behavior.
  - Invalid values fail non-zero with:
    - `invalid kind "<value>" — allowed: function, method, class, interface, type, constant`
- Impact addressed: FR-04 and tasks TK-005/TK-006.
