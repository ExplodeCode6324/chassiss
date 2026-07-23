# CHASSISS documentation

[中文](../cn/README.md) | English

This directory documents the complete CHASSISS mechanism. New users can start with the repository [English README](../../README.en.md), then open the chapter that matches their current work.

## Getting started

1. [What CHASSISS is](01-overview.md)
2. [Five-minute quickstart](02-quickstart.md)
3. [Recommended agent topology](03-agent-topology.md)
4. [Installation, platforms, and version synchronization](04-installation-and-sync.md)
5. [Root, role credentials, and secret distribution](05-credentials.md)
6. [The agent bootstrap entry point](06-bootstrap.md)

## Planning and delivery

7. [Requirements, Architecture, Mission, and Task](07-artifacts.md)
8. [Complete development lifecycle](08-lifecycle.md)
9. [Task worktrees, scope, and budgets](09-task-worktrees-and-budgets.md)
10. [Checks and independent verification sources](10-checks-and-verification.md)
11. [Review, integration, and audit](11-review-and-integration.md)
12. [Concurrency, locks, and state conflicts](12-concurrency-and-conflicts.md)

## Control, recovery, and collaboration

13. [`.chassis`, events, and state projection](13-control-state.md)
14. [Crash recovery and integrity blocking](14-recovery-and-integrity.md)
15. [Publishing and multi-machine collaboration](15-publish-and-multi-machine.md)
16. [Human Owner takeover](16-owner-takeover.md)

## Reference and maintenance

17. [CLI command reference](17-cli-reference.md)
18. [Error handling and troubleshooting](18-troubleshooting.md)
19. [Security model and known risks](19-security-model.md)
20. [Protocol, versions, and compatibility](20-protocol-and-compatibility.md)
21. [Examples, automation, and FAQ](21-examples-automation-faq.md)
22. [Test record, known issues, and improvement directions](22-testing-and-improvements.md)

The trusted CLI `bootstrap` output defines an agent's actual permissions, current actions, and command parameters. Embedded templates and validators define the machine format of artifacts.

[Documentation map](menu.md) records the scope of each chapter.
