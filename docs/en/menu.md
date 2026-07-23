# CHASSISS documentation map

[中文](../cn/文档目录.md) | English

This map records chapter order, content boundaries, and maintenance scope. Articles may be split or combined as the documentation grows, while the mechanisms listed here should remain covered.

## 1. [What CHASSISS is](01-overview.md)

Design philosophy, supported use cases, non-goals, the boundary between agent judgment and deterministic CLI enforcement, and the failures CHASSISS can and cannot prevent.

## 2. [Five-minute quickstart](02-quickstart.md)

Skill and CLI installation, Master Root creation, Greenfield and Brownfield initialization, the minimum role set, the first `bootstrap`, and one complete Mission and Task.

## 3. [Recommended agent topology](03-agent-topology.md)

Master, Designer, Orchestrator, Developer, and Reviewer responsibilities, the isolated Designer session, a combined Build Agent, an independent Review Agent, parallel Developers, and the experimental autonomous Master mode.

## 4. [Installation, platforms, and version synchronization](04-installation-and-sync.md)

Skill layout, bundled CLI selection and checksums, macOS and Linux support, Windows write limitations, the current local Git backend, remote choices, and future backend boundaries.

## 5. [Root, role credentials, and secret distribution](05-credentials.md)

Master Root and role credentials, role issuance, the Build Agent's shared actor, Base64 armor, storage and distribution, revocation and rotation, scopes, validity windows, and the unique Owner grant.

## 6. [The agent bootstrap entry point](06-bootstrap.md)

`principal`, `policy`, `capabilities`, `available_actions`, `context_requests`, revision binding, refresh conditions, and why an agent cannot self-declare its role.

## 7. [Requirements, Architecture, Mission, and Task](07-artifacts.md)

Artifact responsibilities, dependencies, embedded templates, YAML front matter, Markdown bodies, digest binding, independent acceptance, frozen contracts, and replacement.

## 8. [Complete development lifecycle](08-lifecycle.md)

Planning, Mission activation, Task assignment, Developer work, design consistency review, independent review, integration, Mission evidence, and Master acceptance.

## 9. [Task worktrees, scope, and budgets](09-task-worktrees-and-budgets.md)

Linked worktrees, `allowed_paths`, dependencies, parallel boundaries, file and diff budgets, preflight, stop conditions, release, cancellation, blocking, and superseding.

## 10. [Checks and independent verification sources](10-checks-and-verification.md)

Structured commands, shell policy, environment and working-directory boundaries, verification paths, Developer evidence, Reviewer validation, Integration reruns, and semantic review.

## 11. [Review, integration, and audit](11-review-and-integration.md)

Actor independence, immutable Submission digests, mechanical checks, semantic verdicts, review reports, change-request history, candidate integration, merged-tree evidence, and formal baseline advancement.

## 12. [Concurrency, locks, and state conflicts](12-concurrency-and-conflicts.md)

Advisory locks, State and Trust CAS, WIP limits, path conflicts, stale-lock handling, `bootstrap` refresh, and safe automation retries.

## 13. [`.chassis`, events, and state projection](13-control-state.md)

Configuration, Trust, State, events, operations, submissions, worktrees, cache, lock ownership, the event source of truth, and files that must never be edited manually.

## 14. [Crash recovery and integrity blocking](14-recovery-and-integrity.md)

Workflow, authorization, and publish journals, durable phases, deterministic recovery, `CHS-INTEGRITY-BLOCKED`, and actions that must stop during an ambiguous state.

## 15. [Publishing and multi-machine collaboration](15-publish-and-multi-machine.md)

Integration and publication as separate facts, fast-forward publication, endpoint binding, five-minute monitoring, version synchronization, and the current single-control-end limitation.

## 16. [Human Owner takeover](16-owner-takeover.md)

Owner and Root separation, quiescence, formal baseline checks, uncommitted human changes, protected paths, signed audit history, rotation, recovery, and publication.

## 17. [CLI command reference](17-cli-reference.md)

Global options and every command group, organized by role and lifecycle phase. Machine-readable command schemas remain available through `bootstrap`.

## 18. [Error handling and troubleshooting](18-troubleshooting.md)

Stable error codes, diagnostic categories, authentication and scope failures, revision conflicts, failed Checks, worktree damage, remote divergence, integrity blocks, safe retries, and Master escalation.

## 19. [Security model and known risks](19-security-model.md)

The cooperative-agent threat model, shared-user limitations, long-lived credentials, old Trust copies, replaced CLIs, chat and clipboard exposure, current controls, and future secret-isolation work.

## 20. [Protocol, versions, and compatibility](20-protocol-and-compatibility.md)

API, Config, State, Event, Trust, Role Policy, Bootstrap, signing bytes, golden vectors, legacy rejection, migration requirements, and backend compatibility.

## 21. [Examples, automation, and FAQ](21-examples-automation-faq.md)

Greenfield, Brownfield, Owner, change requests, budgets, parallel Tasks, recovery, publication, cron templates, common questions, autonomous Master experiments, and high-quality issue reports.

## 22. [Test record, known issues, and improvement directions](22-testing-and-improvements.md)

Test environments and results, reproducible failures, current limitations and workarounds, security and synchronization gaps, platform coverage, release engineering, user experience, performance, and maintenance priorities.
