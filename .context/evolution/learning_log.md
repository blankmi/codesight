- **Purpose**: Run-level lessons captured for future execution quality improvements.
- **Last updated**: 2026-03-17
- **Run**: codesight-project-config

## Entries
[L-001] Task: TK-001 | Type: Success | Lesson: Keeping changes exactly inside the allowed file list enabled first-pass approval with no rework.
[L-002] Task: TK-002 | Type: Success | Lesson: Config-loading refactors in `cmd/cs/main.go` stay stable when env-callsite removals are paired with command-level tests.
[L-003] Task: TK-003 | Type: Success | Lesson: Path-preference changes pass cleanly when tests cover both preferred and fallback locations plus file append behavior.
[L-004] Task: TK-004 | Type: Failure | Lesson: Global pre-run config loading can break `cs init <path>` by coupling to malformed CWD config; command-aware skip logic is required.
[L-005] Task: TK-004 | Type: Success | Lesson: Rework cleared after adding `init` runtime-config skip handling and regression tests for target-path isolation and Rust detection.
[L-006] Task: TK-005 | Type: Failure | Lesson: Single-file scope drift (`docs/prd/.../tasks/TK-005.md`) triggered immediate review rejection despite correct feature behavior.
[L-007] Task: TK-005 | Type: Success | Lesson: Rebase/cherry-pick to task-allowlisted files restored approval on the next iteration.
[L-008] Task: TK-001 | Type: Success | Lesson: Repeated review evidence shows a reusable preflight pattern: scope gate plus full verification summary reduces churn.

## Promotion Candidate
- Status: PENDING_HUMAN_APPROVAL
- Pattern: Task-scope gate discipline appears 5 times in this run (TK-001, TK-002, TK-003, TK-005 rejection, TK-005 approval).
- AGENTS.md proposal: "Before requesting review, run a task-scope gate (`git diff --name-only <task-base>`) and confirm every changed file matches the task allowlist; if not, rebase/cherry-pick to isolate scope."
