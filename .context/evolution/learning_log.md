- **Purpose**: Run-level lessons captured for future execution quality improvements.
- **Last updated**: 2026-03-17
- **Run**: codesight-project-config, lsp-performance-persistent-client-connections

## Entries
[L-001] Task: TK-001 | Type: Success | Lesson: Keeping changes exactly inside the allowed file list enabled first-pass approval with no rework.
[L-002] Task: TK-002 | Type: Success | Lesson: Config-loading refactors in `cmd/cs/main.go` stay stable when env-callsite removals are paired with command-level tests.
[L-003] Task: TK-003 | Type: Success | Lesson: Path-preference changes pass cleanly when tests cover both preferred and fallback locations plus file append behavior.
[L-004] Task: TK-004 | Type: Failure | Lesson: Global pre-run config loading can break `cs init <path>` by coupling to malformed CWD config; command-aware skip logic is required.
[L-005] Task: TK-004 | Type: Success | Lesson: Rework cleared after adding `init` runtime-config skip handling and regression tests for target-path isolation and Rust detection.
[L-006] Task: TK-005 | Type: Failure | Lesson: Single-file scope drift (`docs/prd/.../tasks/TK-005.md`) triggered immediate review rejection despite correct feature behavior.
[L-007] Task: TK-005 | Type: Success | Lesson: Rebase/cherry-pick to task-allowlisted files restored approval on the next iteration.
[L-008] Task: TK-001 | Type: Success | Lesson: Repeated review evidence shows a reusable preflight pattern: scope gate plus full verification summary reduces churn.
[L-009] Task: TK-001 | Type: Failure | Lesson: Trusting persisted `socket_path` payloads in daemon state created an unsafe cleanup path that could target arbitrary files.
[L-010] Task: TK-001 | Type: Success | Lesson: Deriving socket paths from trusted state inputs (`statePath` + `stateKey`) and testing tampered payloads closed the cleanup safety gap.
[L-011] Task: TK-002 | Type: Failure | Lesson: Unix-only daemon/lifecycle tests in cross-platform files broke Windows test compilation and portability expectations.
[L-012] Task: TK-002 | Type: Success | Lesson: Splitting tests with `windows`/`!windows` build tags and adding explicit `ErrDaemonDisabled` assertions restored platform-safe coverage.
[L-013] Task: TK-003 | Type: Failure | Lesson: Generated binary artifact (`lsp.test.exe`) in task diff caused immediate scope rejection despite correct implementation changes.
[L-014] Task: TK-003 | Type: Success | Lesson: Rebase/cherry-pick plus pre-review diff hygiene resolved scope drift and returned the task to clean approval.
[L-015] Task: TK-004 | Type: Success | Lesson: Runtime connector wiring changes passed cleanly when command-level contract tests for `refs/callers/implements` were updated in the same slice.
[L-016] Task: TK-005 | Type: Failure | Lesson: Runtime config plumbing for daemon idle timeout needed a command/runtime test using the real factory; stubbed call sites hid this regression risk.
[L-017] Task: TK-005 | Type: Success | Lesson: Verifying idle-timeout wiring via state/socket cleanup and shutdown/exit signaling checks produced durable coverage for runtime behavior.
[L-018] Task: TK-006 | Type: Success | Lesson: Java build-change detection remained stable when Gradle-file tracking and unchanged-path suppression behavior were asserted directly in tests.
[L-019] Task: TK-007 | Type: Success | Lesson: Warmup and cold-start hint behavior shipped cleanly when warnings/tips were validated across warmup, refs, and index command paths.

## Promotion Candidate
- Status: PENDING_HUMAN_APPROVAL
- Pattern: Task-scope gate discipline now appears at least 7 times across logged runs (scope hygiene successes and scope-drift rework recoveries).
- AGENTS.md proposal: "Before requesting review, run `git diff --name-only <task-base>...HEAD` and confirm every file matches the task allowlist; remove generated artifacts from the diff (for example `*.test`, `*.exe`) before submitting."
