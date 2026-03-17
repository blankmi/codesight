# Kernel-Based Agentic Workflow

## Overview

This workflow implements the **Context Kernel** architecture — a deterministic execution framework wrapped around nondeterministic language models. Agents operate as stateless processors embedded inside a strictly controlled state machine where the LLM generates, the system validates, and only validated output changes state.

The system is driven by three foundational elements:

1. **The Kernel (`AGENTS.md`)**: System invariants — the rules that never change (stack, constraints, patterns, verification contracts). Must stay under 100 lines.
2. **The Brain (`/.context/`)**: Evolving knowledge — what the system learns about the codebase (architecture observations, dependency topology, failure patterns). All facts are classified by provenance.
3. **The State Machine**: The dynamic state of the current work (phase, task status, blockers). Governed by either File-State or Git-State mode.

### Core Principles

- **Deterministic Control Flow**: The model is probabilistic, but the system must not be. Every agent action occurs within explicit state transitions with hard verification contracts and clear escalation rules.
- **Stateless Execution**: Agents do not retain context between runs. Each execution begins fresh with the Kernel, the Brain, a scoped task definition, and the current repository snapshot. All persistence is externalized through file artifacts.
- **Separation of Concerns**: Work is divided into specialized roles (Archaeologist, Planner, Worker, Reviewer, Reflector & Curator) to prevent error propagation.
- **Fact Classification**: All knowledge in the Brain is classified as `Observed` (verified by code inspection), `Reported` (documented but unverified), or `Verified` (confirmed via execution). This prevents truth drift — upgrading speculation into truth.

---

## Architecture

```mermaid
graph TB
%% Core Memory Layers
    Kernel[AGENTS.md <br> The Kernel]
    Brain[/.context/ <br> The Brain]
    Status[State Machine <br> status.md or Git]

%% Phase 0: Discovery
    subgraph Phase0 [Phase 0: The Archaeologist]
        direction TB
        Arch[Archaeologist]
        Arch -->|Generates| Kernel
        Arch -->|Populates| Brain
    end

%% Phase 1: Planning
    subgraph Phase1 [Phase 1: The Planner]
        direction TB
        Human -->|Request| PRD[Planner Agent]
        PRD -->|Generates| Spec[Spec & Tasks]
        HumanQA[Human QA] -->|Resolves| Questions[open_questions.md]
    end

%% Phase 2: Execution
    subgraph Phase2 [Phase 2: The Worker]
        direction TB
        Worker -->|Reads| Spec
        Worker -->|Executes| TaskBranch[Task Branch]
        Worker -->|Verifies| Build[Build / Test / Lint]
    end

%% Phase 3: Validation
    subgraph Phase3 [Phase 3: The Reviewer]
        direction TB
        Reviewer -->|Validates| TaskBranch
        Reviewer -->|Merges| Main[Epic Branch]
        HumanRev[Human Visual Gate] -.->|UI Tasks Only| Reviewer
    end

%% Phase 4: Evolution
    subgraph Phase4 [Phase 4: The Reflector & Curator]
        direction TB
        ReflectorCurator[Reflector & Curator] -->|Analyzes| Main
        ReflectorCurator -->|Updates| Evolution[/.context/evolution/]
        ReflectorCurator -.->|Proposes| Kernel
    end

%% Flow
    Phase0 --> Phase1
    Phase1 --> Phase2
    Phase2 --> Phase3
    Phase3 --> Phase4

%% Shared Data Access
    Kernel -.-> Phase1
    Kernel -.-> Phase2
    Kernel -.-> Phase3
    Brain -.-> Phase1
    Brain -.-> Phase2
    Brain -.-> Phase3
    Status <--> Phase1
    Status <--> Phase2
    Status <--> Phase3
```

---

## State Management

The architecture supports two operational modes for tracking system state. **Only one mode governs at a time** to prevent split-brain progression logic.

### File-State Mode

A `status.md` file in `docs/prd/EPIC_SLUG/` becomes authoritative. Agents update state explicitly, and the file provides a single source of truth for task status, review outcomes, and workflow progression.

### Git-State Mode

Git history becomes the canonical state machine. Branch names encode task identifiers. Commit messages record state transitions. Status files become derived artifacts.

### `status.md` Schema (File-State Mode)

```markdown
# Epic Status: <EPIC_SLUG>

## 1. Global State

- **Phase**: PLANNING | EXECUTION | DONE
- **Last Updated**: YYYY-MM-DD
- **Epic Branch**: f-prd-<EPIC_SLUG>

## 2. Planning

| Artifact          | Status | Notes |
|-------------------|--------|-------|
| spec.md           | DONE   |       |
| open_questions.md | DONE   | All resolved |

## 3. Tasks

| Task   | Type      | Status             | Branch               | Findings |
|--------|-----------|--------------------|----------------------|----------|
| TK-001 | logic     | MERGED             | f-<epic>-TK-001-slug | —        |
| TK-002 | ui        | HUMAN_VERIFICATION | f-<epic>-TK-002-slug | —        |
| TK-003 | migration | PENDING            | —                    | —        |

## 4. Improvement

| Task   | Reflection |
|--------|------------|
| TK-001 | DONE       |

```

### Valid Status Transitions

1. `PENDING` → `IN_PROGRESS` (Worker starts)
2. `IN_PROGRESS` → `READY_FOR_REVIEW` (Worker passes verification contract)
3. `READY_FOR_REVIEW` → `APPROVED` (Reviewer passes logic)
4. `READY_FOR_REVIEW` → `HUMAN_VERIFICATION` (Reviewer flags UI for human review)
5. `READY_FOR_REVIEW` → `CHANGES_REQUESTED` (Reviewer rejects)
6. `CHANGES_REQUESTED` → `IN_PROGRESS` (Worker begins rework)

---

## The Protocol Steps

### Phase 0: The Archaeologist (One-Time Discovery)

**Agent**: `archaeologist.md`

* **Mode**: Read-only. Does not modify production code, configs, or build files.
* **Action**: Systematically scans the repository to discover build system, module structure, entry points, conventions, protected paths, and quality gates.
* **QA Interview**: Generates a prioritized question list (at most 12 questions) to resolve ambiguities. Each question includes why it matters for agent safety.
* **Output**: Generates `AGENTS.md` (The Kernel), populates `/.context/` (The Brain), and produces a handoff plan with safe first steps.
* **Result**: The project is "Agent-Ready".

### Phase 1: The Planner

**Agent**: `planner.md`

- **Input**: User request (raw description, ticket, or feature request)
- **Context**: Reads the Kernel and the Brain to ground the request
- **Action**: Generates `gap_analysis.md`, `spec.md`, `plan.md`, and atomic task files (`tasks/TK-###.md`)
- **Constraint**: If ambiguities exist, creates `open_questions.md` and halts execution
- **Human Loop**: A human resolves questions in `open_questions.md`. The Planner re-runs to incorporate answers into specs and tasks.
- **Exit Criteria**: `open_questions.md` is empty or fully resolved, and `status.md` Phase is set to `EXECUTION`

#### HF-xxx Feedback Blocks

```markdown
## HF-01 — <short description>

- **Feedback**: What is wrong, unclear, or missing?
- **Impact**: TK-xxx, FR-xx — affected tasks or requirements
- **Resolution**: Expected behavior or required change

**Status**: OPEN | RESOLVED

```

### Phase 2: The Worker (Execution Loop)

**Agent**: `worker.md`

* **Input**: Picks first `PENDING` task from `status.md`.
* **Constraints**:
  - Dedicated branch per task (`f-<epic>-TK-###-<slug>`)
  - Only declared files may be modified
  - No implicit refactoring or opportunistic improvements
  - No cross-task scope blending
* **Action**:
  1. Reads the Kernel and the Brain for context.
  2. Switches to task branch.
  3. Implements strict requirements from the task file.
  4. Runs verification contract (build, test, lint).
* **Rework**: If `status.md` says `CHANGES_REQUESTED`, reads the "Findings" column and fixes *only* those issues.
* **Failure Handling**: Attempts repair within defined limits. If unresolvable, escalates with full diagnostic context. No silent degradation.
* **Exit Criteria**: Verification contract passes. Status updated to `READY_FOR_REVIEW`.

**Parallelism**: Multiple Workers may execute concurrently only when their file scopes do not overlap. This avoids merge conflicts and cross-task contamination while increasing throughput.

### Phase 3: The Reviewer (Validation Gate)

**Agent**: `reviewer.md`

* **Input**: Git diff of the task branch.
* **Action**:

1. **Scope Integrity**: Were only declared files modified?
2. **Forbidden Paths**: Were protected directories touched? (Automatic Fail)
3. **Architectural Compliance**: Do changes respect system invariants? Any security anti-patterns?
4. **Contract Satisfaction**: Does the code meet the task specification?

* **Decision Logic**:
  - **Logic/Migration**: If safe and correct → `APPROVED`, merges branch, updates `status.md`.
  - **UI/Mixed**: If safe and correct → `HUMAN_VERIFICATION`. Visual correctness remains outside automated validation.
  - **Issues**: If unsafe or incorrect → `CHANGES_REQUESTED`, appends specific issues to `status.md`.

**Agent**: `human.md` (If UI)

* **Action**: Verifies visual behavior against a specific checklist.
* **Output**: `PASS` (triggers Reviewer to merge) or `FAIL` (triggers Reviewer to request changes).

### Phase 4: The Reflector & Curator (Evolution)

**Agent**: `reflector.md`

* **Trigger**: Task status becomes `MERGED`.
* **Action**:

1. **Reflect**: Analyzes execution trace — clean pass, rework triggers, review rejections, escalation frequency.
2. **Curate**: Adds structured learning entry to `/.context/evolution/` with trigger event, evidence, impact scope, root cause, and proposed mitigation.
3. **Promote**: If a pattern recurs 3+ times with sufficient evidence, drafts a Kernel update. Kernel evolution requires human approval and a versioned update with clear rationale.

---

## Commit Strategy

Agents use specific prefixes to maintain a clean audit trail:

| Prefix    | Actor     | Meaning                       |
|-----------|-----------|-------------------------------|
| `PRD:`    | Planner   | Spec generation or update     |
| `QA:`     | Human     | Answering open questions      |
| `TK-###:` | Worker    | Implementation code           |
| `REVIEW:` | Reviewer  | Merging or requesting changes |
| `EVOLVE:` | Curator   | Updating context/memory files |

---

## Orchestration Checklist

**To Start a New Epic:**

1. [ ] Run **Planner**: "I want to add feature X..."
2. [ ] Review `docs/prd/X/open_questions.md`. Answer them.
3. [ ] Re-run **Planner** to finalize specs.

**To Execute (Repeat per Task):**

1. [ ] Check `status.md` for next `PENDING` task.
2. [ ] Run **Worker** on that task.
3. [ ] Run **Reviewer** on the result.
4. [ ] (If UI) Run **Human** verification.
5. [ ] Run **Reflector** to save lessons learned.
