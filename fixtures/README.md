# CHASSISS v1 conformance fixtures

These files are protocol data, not Go-specific snapshots. JSON intended as a
protocol object is canonical UTF-8 JSON; State files have exactly one trailing
LF. Test-only keys must never be used for a real Project.

`go test ./...` consumes the canonicalization, digest, contract, State, and
reducer vectors directly. The scenario manifests name the security, topology,
drift, integration, and recovery cases exercised by repository-backed tests.

