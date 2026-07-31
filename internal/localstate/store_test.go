package localstate

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

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

func TestStorePendingCanonicalHTMLCharactersRoundTrip(t *testing.T) {
	data := t.TempDir()
	store := Store{Paths: pathsFromData(data, filepath.Join(data, "cache"))}
	pending := reviewPendingWithFinding(t, "bar_end <= available_at <= decision_time")
	value := New()
	project := testProject()
	project.PendingOperations[pending.OperationID] = pending
	value.Projects["PRJ-TEST"] = project
	if err := store.Save(value); err != nil {
		t.Fatal(err)
	}
	first, err := os.ReadFile(store.Paths.State)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(first, []byte(`\u003c`)) ||
		!bytes.Contains(first, []byte("bar_end <= available_at <= decision_time")) {
		t.Fatalf("Store rewrote canonical pending HTML characters: %s", first)
	}
	loaded, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	actual := loaded.Projects["PRJ-TEST"].PendingOperations[pending.OperationID]
	if !bytes.Equal(actual.SemanticOperation, pending.SemanticOperation) ||
		!bytes.Equal(actual.CandidateEvidence, pending.CandidateEvidence) {
		t.Fatal("pending canonical bytes changed across Store restart")
	}
	if err := store.Save(loaded); err != nil {
		t.Fatal(err)
	}
	second, err := os.ReadFile(store.Paths.State)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) || len(second) == 0 || second[len(second)-1] != '\n' ||
		bytes.HasSuffix(second, []byte("\n\n")) {
		t.Fatal("Store output is not deterministic compact JSON with one trailing LF")
	}
}

func TestStoreRecoversExactLegacyPendingHTMLEscaping(t *testing.T) {
	for _, mode := range []string{"load", "update"} {
		t.Run(mode, func(t *testing.T) {
			data := t.TempDir()
			store := Store{Paths: pathsFromData(data, filepath.Join(data, "cache"))}
			pending := reviewPendingWithFinding(t, "bar_end <= available_at <= decision_time")
			pending.CandidateCommit = nil
			pending.Status = "prepared"
			pending.TargetRefs["refs/heads/main"] = ""
			value := New()
			project := testProject()
			project.PendingOperations[pending.OperationID] = pending
			value.Projects["PRJ-TEST"] = project
			writeLegacyHTMLState(t, store, value)

			if mode == "load" {
				if _, err := store.Load(); err != nil {
					t.Fatalf("restart recovery failed: %v", err)
				}
			} else {
				candidate := strings.Repeat("d", 40)
				if err := store.Update(func(local *State) error {
					project := local.Projects["PRJ-TEST"]
					current := project.PendingOperations[pending.OperationID]
					current.CandidateCommit = &candidate
					current.Status = "signed"
					current.TargetRefs["refs/heads/main"] = candidate
					project.PendingOperations[pending.OperationID] = current
					local.Projects["PRJ-TEST"] = project
					return nil
				}); err != nil {
					t.Fatalf("mutation recovery failed: %v", err)
				}
			}
			repaired, err := os.ReadFile(store.Paths.State)
			if err != nil {
				t.Fatal(err)
			}
			if bytes.Contains(repaired, []byte(`\u003c`)) ||
				!bytes.Contains(repaired, []byte("bar_end <= available_at <= decision_time")) {
				t.Fatalf("legacy pending was not persisted canonically: %s", repaired)
			}
			loaded, err := store.Load()
			if err != nil {
				t.Fatal(err)
			}
			current := loaded.Projects["PRJ-TEST"].PendingOperations[pending.OperationID]
			if mode == "update" && (current.Status != "signed" || current.CandidateCommit == nil) {
				t.Fatalf("recovered mutation was not persisted: %#v", current)
			}
		})
	}
}

func TestStoreRejectsNonExactOrTamperedLegacyPending(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*State, PendingOperation)
		raw    func([]byte) []byte
	}{
		{
			name: "non_exact_escape",
			raw: func(data []byte) []byte {
				return bytes.Replace(data, []byte(`\u003c`), []byte(`\u003C`), 1)
			},
		},
		{
			name: "digest_mismatched_content",
			mutate: func(value *State, pending PendingOperation) {
				project := value.Projects["PRJ-TEST"]
				current := project.PendingOperations[pending.OperationID]
				current.SemanticOperation = bytes.Replace(
					current.SemanticOperation,
					[]byte("bar_end <= available_at <= decision_time"),
					[]byte("bar_end != available_at != decision_time"), 1,
				)
				project.PendingOperations[pending.OperationID] = current
				value.Projects["PRJ-TEST"] = project
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			data := t.TempDir()
			store := Store{Paths: pathsFromData(data, filepath.Join(data, "cache"))}
			pending := reviewPendingWithFinding(t, "bar_end <= available_at <= decision_time")
			value := New()
			project := testProject()
			project.PendingOperations[pending.OperationID] = pending
			value.Projects["PRJ-TEST"] = project
			if test.mutate != nil {
				test.mutate(value, pending)
			}
			raw := legacyHTMLStateBytes(t, value)
			if test.raw != nil {
				raw = test.raw(raw)
			}
			writeLocalStateBytes(t, store, raw)
			if _, err := store.Load(); err == nil {
				t.Fatal("unsafe legacy pending unexpectedly recovered")
			} else if protocolError, ok := err.(*protocol.Error); !ok ||
				protocolError.Code != protocol.ErrLocalStateCorrupt {
				t.Fatalf("unexpected refusal: %T %v", err, err)
			}
		})
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

func TestStoreUpdateSerializesAcrossProcessesWithoutLosingPending(t *testing.T) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skip("cross-process flock regression targets macOS and Linux")
	}
	data := t.TempDir()
	store := Store{Paths: pathsFromData(data, filepath.Join(data, "cache"))}
	value := New()
	value.Projects["PRJ-TEST"] = testProject()
	if err := store.Save(value); err != nil {
		t.Fatal(err)
	}

	enteredFirst := filepath.Join(t.TempDir(), "entered-first")
	enteredSecond := filepath.Join(t.TempDir(), "entered-second")
	releaseFirst := filepath.Join(t.TempDir(), "release-first")
	first, firstOutput := storeUpdateHelperCommand(
		t, data, enteredFirst, releaseFirst,
		"OPR-B1ARZ3NDEKTSV4RRFFQ69G5FAV", "GRT-FIRST", "c",
	)
	if err := first.Start(); err != nil {
		t.Fatal(err)
	}
	firstDone := waitCommand(first)
	waitForPath(t, enteredFirst, 5*time.Second)

	second, secondOutput := storeUpdateHelperCommand(
		t, data, enteredSecond, "",
		"OPR-C1ARZ3NDEKTSV4RRFFQ69G5FAV", "GRT-SECOND", "d",
	)
	if err := second.Start(); err != nil {
		t.Fatal(err)
	}
	secondDone := waitCommand(second)
	time.Sleep(200 * time.Millisecond)
	if _, err := os.Stat(enteredSecond); err == nil {
		t.Fatal("second process entered Store.Update while the first held the lock")
	} else if !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if err := os.WriteFile(releaseFirst, []byte("release\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	waitForCommand(t, firstDone, firstOutput, 5*time.Second)
	waitForCommand(t, secondDone, secondOutput, 5*time.Second)

	loaded, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	pending := loaded.Projects["PRJ-TEST"].PendingOperations
	for _, operationID := range []string{
		"OPR-B1ARZ3NDEKTSV4RRFFQ69G5FAV",
		"OPR-C1ARZ3NDEKTSV4RRFFQ69G5FAV",
	} {
		if _, exists := pending[operationID]; !exists {
			t.Fatalf("concurrent Store.Update lost pending Operation %s: %#v", operationID, pending)
		}
	}
}

func TestStoreUpdateProcessHelper(t *testing.T) {
	if os.Getenv("CHASSISS_STORE_UPDATE_HELPER") != "1" {
		return
	}
	data := os.Getenv("CHASSISS_STORE_DATA")
	store := Store{Paths: pathsFromData(data, filepath.Join(data, "cache"))}
	operationID := os.Getenv("CHASSISS_STORE_OPERATION")
	pending := testPending(
		t, operationID, os.Getenv("CHASSISS_STORE_GRANT"),
		os.Getenv("CHASSISS_STORE_CANDIDATE"),
	)
	if err := store.Update(func(local *State) error {
		if err := os.WriteFile(
			os.Getenv("CHASSISS_STORE_ENTERED"), []byte("entered\n"), 0o600,
		); err != nil {
			return err
		}
		if release := os.Getenv("CHASSISS_STORE_RELEASE"); release != "" {
			deadline := time.Now().Add(5 * time.Second)
			for {
				if _, err := os.Stat(release); err == nil {
					break
				} else if !os.IsNotExist(err) {
					return err
				}
				if time.Now().After(deadline) {
					return os.ErrDeadlineExceeded
				}
				time.Sleep(10 * time.Millisecond)
			}
		}
		project := local.Projects["PRJ-TEST"]
		project.PendingOperations[operationID] = pending
		local.Projects["PRJ-TEST"] = project
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func storeUpdateHelperCommand(
	t *testing.T,
	data string,
	entered string,
	release string,
	operationID string,
	grantID string,
	candidate string,
) (*exec.Cmd, *bytes.Buffer) {
	t.Helper()
	command := exec.Command(os.Args[0], "-test.run=^TestStoreUpdateProcessHelper$")
	command.Env = append(os.Environ(),
		"CHASSISS_STORE_UPDATE_HELPER=1",
		"CHASSISS_STORE_DATA="+data,
		"CHASSISS_STORE_ENTERED="+entered,
		"CHASSISS_STORE_RELEASE="+release,
		"CHASSISS_STORE_OPERATION="+operationID,
		"CHASSISS_STORE_GRANT="+grantID,
		"CHASSISS_STORE_CANDIDATE="+candidate,
	)
	output := &bytes.Buffer{}
	command.Stdout = output
	command.Stderr = output
	return command, output
}

func waitCommand(command *exec.Cmd) <-chan error {
	done := make(chan error, 1)
	go func() {
		done <- command.Wait()
	}()
	return done
}

func waitForCommand(
	t *testing.T,
	done <-chan error,
	output *bytes.Buffer,
	timeout time.Duration,
) {
	t.Helper()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("helper process failed: %v\n%s", err, output.String())
		}
	case <-time.After(timeout):
		t.Fatal("helper process did not finish")
	}
}

func waitForPath(t *testing.T, path string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		if _, err := os.Stat(path); err == nil {
			return
		} else if !os.IsNotExist(err) {
			t.Fatal(err)
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", path)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func testProject() Project {
	return Project{
		GenesisCommit: strings.Repeat("a", 40),
		Identity:      Identity{Keys: map[string]Key{}, SelectedKeyID: ""},
		MinimumCheckpoint: Checkpoint{
			Commit:      strings.Repeat("a", 40),
			StateDigest: "sha256:" + strings.Repeat("b", 64),
		},
		PendingOperations: map[string]PendingOperation{},
		Remote:            Remote{},
		RepoInstances:     []RepoInstance{},
		RootFingerprint:   "SHA256:test",
		Worktrees:         map[string]Worktree{},
	}
}

func testPending(
	t *testing.T,
	operationID string,
	grantID string,
	candidateCharacter string,
) PendingOperation {
	t.Helper()
	operation := protocol.Operation{
		Schema: protocol.OperationSchema, OperationID: operationID,
		Action: "authority.grant-added", Project: "PRJ-TEST",
		Authority: "root:KEY-ROOT-01", Target: grantID,
		Preconditions: map[string]any{
			"grant_absent": true, "root_key_id": "KEY-ROOT-01",
		},
		Payload: map[string]any{
			"grant": map[string]any{
				"actor": "agent", "capabilities": []any{"task.start"},
				"key_id": "KEY-AGENT-01",
				"limits": map[string]any{
					"max_active_tasks": float64(1), "mode": "bounded",
				},
				"public_key": "fixture",
				"scope": map[string]any{
					"resources": []any{"*"}, "tasks": []any{"*"},
				},
			},
			"grant_id": grantID, "request_digest": nil,
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
	operationBytes, err := protocol.CanonicalJSON(operation)
	if err != nil {
		t.Fatal(err)
	}
	evidenceBytes, err := protocol.CanonicalJSON(evidence)
	if err != nil {
		t.Fatal(err)
	}
	candidate := strings.Repeat(candidateCharacter, 40)
	return PendingOperation{
		AuthorityKeyHandle: "file:/fixture/key", CandidateCommit: &candidate,
		CandidateEvidence: json.RawMessage(evidenceBytes), EvidenceDigest: evidenceDigest,
		ExpectedMain: parent, OperationDigest: operationDigest, OperationID: operationID,
		SemanticOperation: json.RawMessage(operationBytes), Status: "signed",
		TargetRefs: map[string]string{"refs/heads/main": candidate},
	}
}

func reviewPendingWithFinding(t *testing.T, finding string) PendingOperation {
	t.Helper()
	attemptDigest := "sha256:" + strings.Repeat("1", 64)
	operation := protocol.Operation{
		Schema: protocol.OperationSchema, OperationID: "OPR-01ARZ3NDEKTSV4RRFFQ69G5FAV",
		Action: "task.reviewed-indexed", Project: "PRJ-TEST",
		Authority: "grant:GRT-REVIEW-01", Target: "TASK-REVIEW",
		Preconditions: map[string]any{
			"attempt_digest": attemptDigest, "phase": "submitted",
		},
		Payload: map[string]any{
			"report": map[string]any{
				"findings": []any{map[string]any{"summary": finding}},
				"schema":   "chassiss.review-report/v1",
				"summary":  "request changes", "verdict": "request_changes",
			},
			"verdict": "request_changes",
		},
	}
	operationDigest, err := protocol.ObjectDigest("operation", operation)
	if err != nil {
		t.Fatal(err)
	}
	parent := strings.Repeat("a", 40)
	evidence := protocol.ExecutionEvidence{
		Schema: protocol.EvidenceSchema, OperationDigest: operationDigest,
		Action: operation.Action, Attempt: 1, Parent: &parent,
		Facts: map[string]any{
			"attempt_digest": attemptDigest, "check_results": []any{},
			"review_context": map[string]any{"reviewer_attention": []any{finding}},
		},
	}
	evidenceDigest, err := protocol.ObjectDigest("execution-evidence", evidence)
	if err != nil {
		t.Fatal(err)
	}
	semantic, err := protocol.CanonicalJSON(operation)
	if err != nil {
		t.Fatal(err)
	}
	candidateEvidence, err := protocol.CanonicalJSON(evidence)
	if err != nil {
		t.Fatal(err)
	}
	candidate := strings.Repeat("c", 40)
	return PendingOperation{
		AuthorityKeyHandle: "file:/fixture/reviewer-key", CandidateCommit: &candidate,
		CandidateEvidence: candidateEvidence, EvidenceDigest: evidenceDigest,
		ExpectedMain: parent, OperationDigest: operationDigest,
		OperationID: operation.OperationID, SemanticOperation: semantic,
		Status: "signed", TargetRefs: map[string]string{"refs/heads/main": candidate},
	}
}

func legacyHTMLStateBytes(t *testing.T, value *State) []byte {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return append(data, '\n')
}

func writeLegacyHTMLState(t *testing.T, store Store, value *State) {
	t.Helper()
	writeLocalStateBytes(t, store, legacyHTMLStateBytes(t, value))
}

func writeLocalStateBytes(t *testing.T, store Store, data []byte) {
	t.Helper()
	if err := os.MkdirAll(store.Paths.Data, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(store.Paths.State, data, 0o600); err != nil {
		t.Fatal(err)
	}
}
