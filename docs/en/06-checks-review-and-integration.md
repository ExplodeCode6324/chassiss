# Checks, Review, and Integration

A Check is an argv array, never a shell string. Its execution context binds:

```text
Task + phase + exact head/tree + Architecture blob + Taskbook blob
```

stdout, stderr, exit status, duration, and binding enter the Check Result. The
historical verifier validates signed results and bindings without rerunning old
commands.

## Review

Prepare an exact candidate:

```text
chassiss review TASK-001 \
  --prepare \
  --output /outside/review-context.json \
  --report-output /outside/review-report.json \
  --json
```

The Reviewer inspects the candidate, Requirements, Architecture, findings, and
reviewer-attention items. Prepare returns the input schema and a hydrated Report
template with one response slot per frozen attention item. The Reviewer
completes that `chassiss.review-report/v1` and runs:

```text
chassiss review TASK-001 \
  --verdict approve \
  --report /outside/review-report.json \
  --json
```

`request_changes` returns to a sparse active projection. Passing Checks never
create automatic approval. A same-Actor or same-key Review remains protocol
valid but produces a warning that must be shown to Master.

The full Report remains in signed history; State stores only a compact audit
index. Resolve it with `review list TASK-001` and
`review show TASK-001 --operation OPR-...`.

## Integration

```text
chassiss integrate TASK-001 --json
```

The CLI reclassifies drift from Review main to current main. Only unrelated
drift can continue; relevant, unknown, or conflicting drift requires a new
Review. Frozen Checks rerun in an isolated candidate worktree. The final commit
has:

```text
parent[0] = exact current main
parent[1] = exact Attempt Head
tree      = exact recomputed candidate
```

Success leaves only `phase=closed` in State and starts safe cleanup of the Work
Ref and worktree.
