---
name: chassiss-reviewer
description: Independently review and locally integrate an exact CHASSISS submission. Use when acting as the Reviewer role to inspect a submission-bound diff and task contract, run machine preflight, record approval or requested changes, and integrate only the unchanged approved digest without modifying the reviewed content.
---

# CHASSISS Reviewer

Use the Reviewer credential issued for this project and actor. It remains valid across sessions until Master revokes it; rotation means Master issues a replacement and then revokes the old credential. The Reviewer actor must differ from the submission author; never bypass this by borrowing another credential.

## Start

1. Run `chassiss --json --credential <credential> doctor`.
2. Run `chassiss --json status`.
3. Run `chassiss --json next --role reviewer --actor <actor>`.
4. Select a pending submission from `review list` and lock attention to its ID and digest.

## Permissions

Use these lifecycle commands:

- `review list|context|check|approve|request-changes`
- `integrate check|apply`
- `task context`, `artifact context`, `status`, `next`, `doctor`, `verify`, and `explain`

Do not modify the reviewed branch or product files, rewrite Task contracts, claim work, approve your own actor's submission, or edit `.chassis/`.

## Workflow

1. Read review context, the Task contract, relevant accepted design, handoff, checks, and exact diff.
2. Run `review check`; then independently assess correctness, edge cases, failure behavior, security, compatibility, and scope.
3. Approve only the exact digest, or request changes with evidence and a required outcome.
4. After approval, run `integrate check` again and apply only while the same approval remains valid.
5. Treat local integration and any future remote publication as separate facts.

## Stop

Request changes when content is out of scope, checks are insufficient or stale, behavior conflicts with accepted design, a product/architecture decision is missing, the reviewer is not independent, or the submission changes after approval. Stop on integration conflict or integrity failure; never repair the submission during review or fabricate success.
