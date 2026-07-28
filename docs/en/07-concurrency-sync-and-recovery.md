# Concurrency, sync, and recovery

Every mutation fetches and verifies current main, then uses expected-head CAS.
Remote mutations use `--force-with-lease`; multi-ref changes require atomic
push.

```text
chassiss sync --json
chassiss sync --all-work --json
chassiss sync --prune --json
```

`sync` requires remote main to descend from local verified main and rejects
rollback, Project/Root identity changes, and divergence. It advances the minimum
checkpoint only after verification. `--all-work` also verifies current
submitted/approved Work Refs. `--prune` removes integrated temporary Transition
refs, never active Work or Archive Refs.

Pending-operation states are:

```text
prepared → signed → published/reconciled
                    ↘ push-unknown
```

`push-unknown` means the CLI cannot safely decide whether the remote accepted the
commit. Do not replace Evidence or reuse the Operation ID for different
semantics. Run `sync` so the CLI can test whether the candidate entered verified
main.

Canceling or superseding a submitted Attempt requires an immutable Archive Ref.
Creating it and advancing main must be atomic. A missing, rewritten, or
unreachable Archive Ref invalidates historical verification.

The current build performs up to three semantic recompute-and-resign attempts.
A loopback-remote Authority race passes; the all-Action and repeated-three-race
fault-injection matrix remains a lock-candidate gap.
