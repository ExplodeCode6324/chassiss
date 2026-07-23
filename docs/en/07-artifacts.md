# Requirements, Architecture, Mission, and Task

[中文](../cn/07-需求架构任务书.md) | English

CHASSISS uses four Markdown artifact types to freeze project intent and execution contracts.

| Artifact | Default path | Responsibility |
| --- | --- | --- |
| Requirements | `docs/requirements.md` | Problem, required behavior, and success criteria |
| Architecture | `docs/architecture.md` | Boundaries, interfaces, state, security, and validation |
| Mission | `docs/missions/M###.md` | One outcome Master can accept independently |
| Task | `docs/tasks/M###-T###.md` | One atomic implementation unit |

Embedded templates and validators define the machine format.

## Obtain templates

```text
chassiss template get requirements \
  --output docs/requirements.md

chassiss template get architecture \
  --output docs/architecture.md

chassiss template get mission \
  --id M001 \
  --output docs/missions/M001.md

chassiss template get task \
  --id M001-T001 \
  --output docs/tasks/M001-T001.md
```

These commands require a Designer credential. Global options are omitted here.

## Document structure

Every artifact has YAML front matter for machine fields and a Markdown body for semantic content. Keep required sections from the template, replace placeholders, and run `artifact check`.

## Requirements

```yaml
kind: requirements
id: requirements
```

The body includes Problem, Required Behavior, Success Criteria, Scope, Constraints, and Decisions Required from Master.

Use stable IDs such as `REQ-001` and `SC-001`. Accepted Requirements produce a digest that later artifacts bind.

## Architecture

```yaml
kind: architecture
id: architecture
requirements_digest: <accepted-requirements-digest>
```

The body covers System Context, Components and Boundaries, Interfaces, Data and State, Security, Validation Strategy, Parallelization Boundaries, and Master decisions.

Boundaries should be precise enough to create non-overlapping Task paths and independent verification sources.

## Mission

```yaml
kind: mission
id: M001
requirements_digest: <accepted-requirements-digest>
architecture_digest: <accepted-architecture-digest>
task_ids:
  - M001-T001
```

A Mission defines one observable outcome, covered requirements, acceptance criteria, constraints, risks, and completion evidence. Every listed Task must be accepted before activation.

## Task

```yaml
kind: task
id: M001-T001
mission_id: M001
requirements_digest: <accepted-requirements-digest>
architecture_digest: <accepted-architecture-digest>
depends_on: []
allowed_paths:
  - src/component/**
budget:
  max_changed_files: 20
  max_diff_lines: 3000
  max_commits: 5
acceptance_checks:
  - id: CHECK-001
    argv:
      - go
      - test
      - ./...
    cwd: src
    env: {}
    timeout_seconds: 120
    verification_paths:
      - tests/**
```

The body covers Objective, Inputs and Assumptions, Forbidden and Out of Scope, Deliverables, Stop Conditions, and Reviewer Attention.

## Dependencies and paths

`depends_on` references Tasks from the same Mission. A dependency is satisfied after integration or cancellation. A superseded dependency follows its replacement chain.

`allowed_paths` uses project-relative controlled globs. The CLI computes the final changed-file set and rejects any out-of-scope path.

`verification_paths` cannot overlap `allowed_paths`. A Developer cannot change the evidence used by the same Task.

## Budgets

| Value | Meaning |
| --- | --- |
| `max_changed_files` | Maximum changed-file count |
| `max_diff_lines` | Added plus deleted text lines |
| `max_commits` | Commits from baseline to Submission |

Zero disables that dimension. An accepted Task freezes its budget.

## Submit and accept

```text
chassiss artifact check docs/tasks/M001-T001.md
chassiss artifact submit docs/tasks/M001-T001.md
```

The CLI records the exact file digest and artifact submission ID. Master reads the same content and accepts it.

```text
chassiss artifact context <artifact-submission-id>
chassiss artifact accept <artifact-submission-id>
```

Acceptance creates a formal baseline commit for the exact artifact. A changed file digest blocks acceptance.

Master actor must differ from `submitted_by`. Do not issue a Designer credential with actor `master`, because Master Root could not accept or reject that artifact.

Master may reject with an actionable reason.

```text
chassiss artifact reject <artifact-submission-id> \
  --reason <actionable-reason>
```

## Frozen contracts and replacement

An accepted active Task cannot expand its objective, dependencies, paths, budget, or Checks in place.

1. Block the old Task
2. Designer creates a new Task artifact
3. Master accepts the replacement
4. Orchestrator runs `task supersede`

The replacement belongs to the same Mission and enters its Task graph.

Owner and Developer cannot bypass the artifact workflow for registered documents.

See the [complete lifecycle](08-lifecycle.md) and [Checks](10-checks-and-verification.md).

