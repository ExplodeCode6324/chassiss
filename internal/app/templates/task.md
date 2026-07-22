---
kind: task
id: M000-T000
mission_id: M000
requirements_digest: REPLACE_REQUIREMENTS_DIGEST
architecture_digest: REPLACE_ARCHITECTURE_DIGEST
depends_on: []
allowed_paths:
  - REPLACE_ALLOWED_PATH/**
budget:
  max_changed_files: REPLACE_MAX_CHANGED_FILES
  max_diff_lines: REPLACE_MAX_DIFF_LINES
  max_commits: REPLACE_MAX_COMMITS
acceptance_checks:
  - id: CHECK-001
    argv: ["REPLACE_COMMAND", "REPLACE_ARGUMENT"]
    cwd: "."
    env: {}
    timeout_seconds: 120
---
# Task M000-T000

## Objective

<replace with one result a single Agent session can complete>

## Inputs and Assumptions

- <replace with precise inputs and assumptions>

## Forbidden and Out of Scope

- <replace with explicit exclusions>

## Deliverables

- <replace with files or observable behavior>

## Stop Conditions

- Stop if the task requires a design change or a path outside allowed_paths.

## Reviewer Attention

- <replace with the highest-risk behavior to review>
