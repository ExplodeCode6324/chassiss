package localstate

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ExplodeCode6324/chassiss/internal/protocol"
)

func TestStoreRoundTrip(t *testing.T) {
	data := t.TempDir()
	store := Store{Paths: pathsFromData(data, filepath.Join(data, "cache"))}
	state := New()
	state.Projects["PRJ-TEST"] = Project{
		GenesisCommit: strings.Repeat("a", 40),
		Identity:      Identity{Keys: map[string]Key{}, SelectedKeyID: ""},
		MinimumCheckpoint: Checkpoint{
			Commit: strings.Repeat("a", 40), StateDigest: "sha256:" + strings.Repeat("b", 64),
		},
		PendingOperations: map[string]PendingOperation{},
		Remote:            Remote{},
		RepoInstances:     []RepoInstance{},
		RootFingerprint:   "SHA256:test",
		Worktrees:         map[string]Worktree{},
	}
	if err := store.Save(state); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Projects["PRJ-TEST"].GenesisCommit != strings.Repeat("a", 40) {
		t.Fatal("local State changed after round trip")
	}
}

func TestUnknownGrantCacheFieldRejected(t *testing.T) {
	data := t.TempDir()
	store := Store{Paths: pathsFromData(data, filepath.Join(data, "cache"))}
	if err := os.MkdirAll(data, 0o700); err != nil {
		t.Fatal(err)
	}
	raw := `{"projects":{},"schema":"chassiss.local/v1","grant_id":"GRT-CACHED"}`
	if err := os.WriteFile(store.Paths.State, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Load(); err == nil {
		t.Fatal("unknown cached Grant field unexpectedly accepted")
	}
}

func TestPendingOperationRoundTrip(t *testing.T) {
	data := t.TempDir()
	store := Store{Paths: pathsFromData(data, filepath.Join(data, "cache"))}
	operation := protocol.Operation{
		Schema: protocol.OperationSchema, OperationID: "OPR-01ARZ3NDEKTSV4RRFFQ69G5FAV",
		Action: "authority.grant-added", Project: "PRJ-TEST",
		Authority: "root:KEY-ROOT-01", Target: "GRT-TEST",
		Preconditions: map[string]any{"grant_absent": true, "root_key_id": "KEY-ROOT-01"},
		Payload: map[string]any{
			"grant": map[string]any{
				"actor": "agent", "capabilities": []any{"task.start"},
				"key_id":     "KEY-AGENT-01",
				"limits":     map[string]any{"max_active_tasks": float64(1), "mode": "bounded"},
				"public_key": "fixture",
				"scope":      map[string]any{"resources": []any{"*"}, "tasks": []any{"*"}},
			},
			"grant_id": "GRT-TEST", "request_digest": nil,
		},
	}
	operationDigest, err := protocol.ObjectDigest("operation", operation)
	if err != nil {
		t.Fatal(err)
	}
	parent := strings.Repeat("a", 40)
	evidence := protocol.ExecutionEvidence{
		Schema: protocol.EvidenceSchema, OperationDigest: operationDigest,
		Action: operation.Action, Attempt: 1, Parent: &parent, Facts: map[string]any{},
	}
	evidenceDigest, err := protocol.ObjectDigest("execution-evidence", evidence)
	if err != nil {
		t.Fatal(err)
	}
	operationBytes, _ := protocol.CanonicalJSON(operation)
	evidenceBytes, _ := protocol.CanonicalJSON(evidence)
	commit := strings.Repeat("c", 40)
	value := New()
	value.Projects["PRJ-TEST"] = Project{
		GenesisCommit: strings.Repeat("a", 40),
		Identity: Identity{Keys: map[string]Key{
			"KEY-ROOT-01": {PrivateKeyHandle: "file:/fixture/key"},
		}, SelectedKeyID: "KEY-ROOT-01"},
		MinimumCheckpoint: Checkpoint{
			Commit: strings.Repeat("a", 40), StateDigest: "sha256:" + strings.Repeat("b", 64),
		},
		PendingOperations: map[string]PendingOperation{
			operation.OperationID: {
				AuthorityKeyHandle: "file:/fixture/key", CandidateCommit: &commit,
				CandidateEvidence: json.RawMessage(evidenceBytes), EvidenceDigest: evidenceDigest,
				ExpectedMain: parent, OperationDigest: operationDigest, OperationID: operation.OperationID,
				SemanticOperation: json.RawMessage(operationBytes), Status: "signed",
				TargetRefs: map[string]string{"refs/heads/main": commit},
			},
		},
		Remote:          Remote{},
		RepoInstances:   []RepoInstance{},
		RootFingerprint: "SHA256:test",
		Worktrees:       map[string]Worktree{},
	}
	if err := store.Save(value); err != nil {
		t.Fatal(err)
	}
}
