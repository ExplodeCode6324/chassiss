package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/ExplodeCode6324/chassiss/internal/cryptoutil"
	"github.com/ExplodeCode6324/chassiss/internal/gitstore"
	"github.com/ExplodeCode6324/chassiss/internal/protocol"
	"github.com/ExplodeCode6324/chassiss/internal/state"
	"github.com/ExplodeCode6324/chassiss/internal/verifier"
)

func TestArchitectureQuiescenceStructuredRefusal(t *testing.T) {
	blocked := true
	if err := requireArchitectureQuiescence(&state.State{Tasks: map[string]state.TaskState{
		"TASK-READY": {Phase: "ready", Blocked: &blocked},
		"TASK-DONE":  {Phase: "closed"},
	}}); err != nil {
		t.Fatalf("blocked-ready Task is quiescent: %v", err)
	}

	err := requireArchitectureQuiescence(&state.State{Tasks: map[string]state.TaskState{
		"TASK-002": {Phase: "active", Actor: "builder-b", Blocked: &blocked},
		"TASK-001": {Phase: "approved", Actor: "builder-a"},
	}})
	protocolError := requireCLIProtocolError(
		t, err, protocol.ErrTaskbookNotQuiescent, protocol.CategoryConflict,
	)
	if protocolError.OperationID != "" || protocolError.CurrentHead != "" ||
		len(protocolError.Remediation) != 0 {
		t.Fatalf("quiescence refusal exposed an operation or remediation: %#v", protocolError)
	}
	expected := []map[string]any{
		{"actor": "builder-a", "phase": "approved", "task": "TASK-001"},
		{"actor": "builder-b", "phase": "active", "task": "TASK-002"},
	}
	if !reflect.DeepEqual(protocolError.Details["in_flight_tasks"], expected) {
		t.Fatalf("in-flight details are not canonical: %#v", protocolError.Details)
	}
}

func TestArchitectureUpdateRejectsStaleTaskbookSidecarWithoutOperation(t *testing.T) {
	ctx, project, _ := newLocalPublishProject(t, "PRJ-ARCH-STALE")
	beforeHead := project.Verified.Head
	candidate := filepath.Join(t.TempDir(), "architecture.yaml")
	var stdout, stderr bytes.Buffer
	runJSON(t, []string{"architecture", "draft", "--output", candidate}, &stdout, &stderr)

	metadata, err := readDraftMetadata(candidate, "architecture")
	if err != nil {
		t.Fatal(err)
	}
	stale := strings.Repeat("7", 40)
	metadata.TaskbookBlob = &stale
	data, err := protocol.CanonicalJSON(metadata)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(candidate+".chassiss.json", append(data, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}

	failure, exit := runJSONFailure(t, []string{
		"architecture", "update", "--file", candidate, "--reason", "stale binding",
	}, &stdout, &stderr)
	if exit != 6 || failure.Operation != nil || failure.Error == nil ||
		failure.Error.Code != protocol.ErrTaskbookStale ||
		failure.Error.Category != protocol.CategoryConflict || failure.Error.Retryable ||
		failure.Error.OperationID != nil || len(failure.Error.Remediation) != 0 ||
		len(failure.Error.Details) != 0 {
		t.Fatalf("unexpected stale-sidecar envelope: exit=%d envelope=%#v", exit, failure)
	}
	after, err := loadProject(ctx, "", false)
	if err != nil {
		t.Fatal(err)
	}
	if after.Verified.Head != beforeHead || len(after.LocalProject.PendingOperations) != 0 {
		t.Fatalf("stale sidecar changed main or pending state: before=%s after=%s pending=%#v",
			beforeHead, after.Verified.Head, after.LocalProject.PendingOperations)
	}
}

func requireCLIProtocolError(
	t *testing.T,
	err error,
	code string,
	category protocol.Category,
) *protocol.Error {
	t.Helper()
	var protocolError *protocol.Error
	if !errors.As(err, &protocolError) {
		t.Fatalf("expected structured protocol error, got %T: %v", err, err)
	}
	if protocolError.Code != code || protocolError.Category != category || protocolError.Retryable {
		t.Fatalf("unexpected protocol error: %#v", protocolError)
	}
	if protocolError.OperationID != "" || len(protocolError.Remediation) != 0 {
		t.Fatalf("refusal unexpectedly exposed an operation or remediation: %#v", protocolError)
	}
	return protocolError
}

func cloneArchitectureState(t *testing.T, source *state.State) *state.State {
	t.Helper()
	data, err := json.Marshal(source)
	if err != nil {
		t.Fatal(err)
	}
	var result state.State
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}
	return &result
}

func unsignedStateCommit(
	t *testing.T,
	ctx context.Context,
	runner gitstore.Runner,
	parent string,
	baseTree gitstore.TreeMap,
	shared *state.State,
	ordinary map[string]gitstore.Entry,
) string {
	t.Helper()
	stateData, err := state.Encode(shared, "sha1")
	if err != nil {
		t.Fatal(err)
	}
	stateBlob, err := runner.HashBlob(ctx, stateData)
	if err != nil {
		t.Fatal(err)
	}
	tree := cloneTreeMap(baseTree)
	tree[".chassiss/state.json"] = gitstore.Entry{Mode: "100644", OID: stateBlob}
	for path, entry := range ordinary {
		tree[path] = entry
	}
	treeOID, err := runner.WriteTree(ctx, tree)
	if err != nil {
		t.Fatal(err)
	}
	result, err := runner.RunWithInput(ctx, []byte("CAS fixture\n"), map[string]string{
		"GIT_AUTHOR_NAME": "Test", "GIT_AUTHOR_EMAIL": "test@example.invalid",
		"GIT_COMMITTER_NAME": "Test", "GIT_COMMITTER_EMAIL": "test@example.invalid",
	}, "commit-tree", treeOID, "-p", parent)
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(result.Stdout))
}

func compatibleArchitecturePlan(
	t *testing.T,
	project *projectContext,
	shared *state.State,
	grantID string,
	fingerprint string,
) (transitionPlan, gitstore.TreeMap, string) {
	t.Helper()
	ctx := context.Background()
	baseTree, err := currentTree(ctx, project)
	if err != nil {
		t.Fatal(err)
	}
	oldData, err := project.Runner.ReadBlob(ctx, shared.Project.Architecture.BlobOID)
	if err != nil {
		t.Fatal(err)
	}
	newData := []byte(strings.Replace(
		string(oldData),
		"Canonical encoding, verification, authorization, reducers, and state.",
		"Canonical signed encoding, verification, authorization, reducers, and state.",
		1,
	))
	newBlob, err := project.Runner.HashBlob(ctx, newData)
	if err != nil {
		t.Fatal(err)
	}
	operation := protocol.Operation{
		Schema: protocol.OperationSchema, OperationID: "OPR-A1ARZ3NDEKTSV4RRFFQ69G5FAV",
		Action: "architecture.updated-compatible", Project: shared.Project.ID,
		Authority: "grant:" + grantID, Target: project.Verified.Architecture.ID,
		Preconditions: map[string]any{
			"all_tasks_quiescent": true,
			"architecture_blob":   shared.Project.Architecture.BlobOID,
			"taskbook_blob":       shared.Project.Taskbook.BlobOID,
		},
		Payload: map[string]any{"candidate_blob": newBlob, "reason": "CAS fixture"},
	}
	digest, err := protocol.ObjectDigest("operation", operation)
	if err != nil {
		t.Fatal(err)
	}
	parent := project.Verified.Head
	evidence := protocol.ExecutionEvidence{
		Schema: protocol.EvidenceSchema, OperationDigest: digest,
		Action: operation.Action, Attempt: 1, Parent: &parent,
		Facts: map[string]any{
			"new_blob": newBlob, "old_blob": shared.Project.Architecture.BlobOID,
			"semantic_diff": map[string]any{}, "taskbook_blob": shared.Project.Taskbook.BlobOID,
		},
	}
	facts := state.ReduceFacts{
		ObjectFormat: "sha1", ParentCommit: parent, SignerFingerprint: fingerprint,
		TargetResources: []string{"module:core"},
	}
	next, err := state.Reduce(shared, operation, evidence, facts)
	if err != nil {
		t.Fatal(err)
	}
	candidateTree := cloneTreeMap(baseTree)
	candidateTree["docs/architecture.yaml"] = gitstore.Entry{Mode: "100644", OID: newBlob}
	return transitionPlan{
		Operation: operation, Evidence: evidence, NextState: next, Tree: candidateTree,
		ReduceFacts: facts,
	}, baseTree, newBlob
}

func TestCompatibleArchitectureCASMatrix(t *testing.T) {
	ctx, original, _ := newLocalPublishProject(t, "PRJ-ARCH-CAS")
	generated, err := cryptoutil.GenerateFileKey(t.TempDir(), "KEY-ARCHITECT-01")
	if err != nil {
		t.Fatal(err)
	}
	grantID := "GRT-ARCHITECT-01"
	baseState := cloneArchitectureState(t, original.Verified.State)
	grant := state.Grant{
		Actor: "architect", Capabilities: []string{"architecture.update"},
		KeyID: "KEY-ARCHITECT-01", PublicKey: generated.PublicKey,
		Limits: state.Limits{Mode: "unbounded"},
		Scope:  state.Scope{Tasks: []string{"*"}, Resources: []string{"*"}},
	}
	baseState.Authority.Grants[grantID] = grant
	previous := &projectContext{
		Runner: original.Runner,
		Verified: &verifier.Result{
			Architecture: original.Verified.Architecture, Taskbook: original.Verified.Taskbook,
			Head: original.Verified.Head, ObjectFormat: "sha1", State: baseState,
		},
	}
	plan, baseTree, newBlob := compatibleArchitecturePlan(
		t, previous, baseState, grantID, generated.Fingerprint,
	)
	operationBefore := plan.Operation

	newContext := func(shared *state.State, head string) *projectContext {
		return &projectContext{
			Runner: original.Runner,
			Verified: &verifier.Result{
				Architecture: original.Verified.Architecture, Taskbook: original.Verified.Taskbook,
				Head: head, ObjectFormat: "sha1", State: shared,
			},
		}
	}

	t.Run("authority-only drift retries", func(t *testing.T) {
		currentState := cloneArchitectureState(t, baseState)
		currentState.Authority.Grants["GRT-ARCHITECT-02"] = grant
		head := unsignedStateCommit(t, ctx, original.Runner, previous.Verified.Head, baseTree, currentState, nil)
		rebased, err := rebaseTransitionPlan(ctx, previous, newContext(currentState, head), plan)
		if err != nil {
			t.Fatal(err)
		}
		if rebased.Evidence.Attempt != 2 || rebased.Evidence.Parent == nil ||
			*rebased.Evidence.Parent != head ||
			rebased.NextState.Project.Architecture.BlobOID != newBlob ||
			rebased.NextState.Project.Taskbook.BlobOID != baseState.Project.Taskbook.BlobOID ||
			rebased.NextState.Authority.Grants["GRT-ARCHITECT-02"].Actor != "architect" ||
			!reflect.DeepEqual(rebased.Operation, operationBefore) {
			t.Fatalf("authority retry changed stable semantics or lost current state: %#v", rebased)
		}
	})

	t.Run("ready to active fails closed", func(t *testing.T) {
		currentState := cloneArchitectureState(t, baseState)
		ids := make([]string, 0, len(currentState.Tasks))
		for id := range currentState.Tasks {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		currentState.Tasks[ids[0]] = state.TaskState{
			Actor: "builder", Base: previous.Verified.Head, Phase: "active",
			Contract: &state.Contract{
				ArchitectureBlob: baseState.Project.Architecture.BlobOID,
				TaskbookBlob:     baseState.Project.Taskbook.BlobOID,
			},
		}
		head := unsignedStateCommit(t, ctx, original.Runner, previous.Verified.Head, baseTree, currentState, nil)
		_, err := rebaseTransitionPlan(ctx, previous, newContext(currentState, head), plan)
		protocolError := requireCLIProtocolError(
			t, err, protocol.ErrTaskbookNotQuiescent, protocol.CategoryConflict,
		)
		expected := []map[string]any{{"actor": "builder", "phase": "active", "task": ids[0]}}
		if !reflect.DeepEqual(protocolError.Details["in_flight_tasks"], expected) {
			t.Fatalf("quiescence retry details are not sorted/exact: %#v", protocolError.Details)
		}
	})

	t.Run("Taskbook drift fails closed", func(t *testing.T) {
		currentState := cloneArchitectureState(t, baseState)
		taskbookData, err := original.Runner.ReadBlob(ctx, baseState.Project.Taskbook.BlobOID)
		if err != nil {
			t.Fatal(err)
		}
		changedTaskbook := append(append([]byte(nil), taskbookData...), '\n')
		changedBlob, err := original.Runner.HashBlob(ctx, changedTaskbook)
		if err != nil {
			t.Fatal(err)
		}
		currentState.Project.Taskbook.BlobOID = changedBlob
		head := unsignedStateCommit(t, ctx, original.Runner, previous.Verified.Head, baseTree, currentState, map[string]gitstore.Entry{
			"docs/taskbook.yaml": {Mode: "100644", OID: changedBlob},
		})
		_, err = rebaseTransitionPlan(ctx, previous, newContext(currentState, head), plan)
		protocolError := requireCLIProtocolError(
			t, err, protocol.ErrTaskbookStale, protocol.CategoryConflict,
		)
		if len(protocolError.Details) != 0 {
			t.Fatalf("unexpected Taskbook stale details: %#v", protocolError.Details)
		}
	})

	t.Run("Architecture target drift conflicts", func(t *testing.T) {
		currentState := cloneArchitectureState(t, baseState)
		changedData := []byte("architecture drift\n")
		changedBlob, err := original.Runner.HashBlob(ctx, changedData)
		if err != nil {
			t.Fatal(err)
		}
		currentState.Project.Architecture.BlobOID = changedBlob
		head := unsignedStateCommit(t, ctx, original.Runner, previous.Verified.Head, baseTree, currentState, map[string]gitstore.Entry{
			"docs/architecture.yaml": {Mode: "100644", OID: changedBlob},
		})
		_, err = rebaseTransitionPlan(ctx, previous, newContext(currentState, head), plan)
		protocolError := requireCLIProtocolError(
			t, err, protocol.ErrCandidateConflict, protocol.CategoryConflict,
		)
		if protocolError.Details["path"] != "docs/architecture.yaml" {
			t.Fatalf("unexpected Architecture conflict details: %#v", protocolError.Details)
		}
	})

	t.Run("ordinary tree drift conflicts", func(t *testing.T) {
		currentState := cloneArchitectureState(t, baseState)
		ordinaryBlob, err := original.Runner.HashBlob(ctx, []byte("ordinary drift\n"))
		if err != nil {
			t.Fatal(err)
		}
		head := unsignedStateCommit(t, ctx, original.Runner, previous.Verified.Head, baseTree, currentState, map[string]gitstore.Entry{
			"README.md": {Mode: "100644", OID: ordinaryBlob},
		})
		_, err = rebaseTransitionPlan(ctx, previous, newContext(currentState, head), plan)
		protocolError := requireCLIProtocolError(
			t, err, protocol.ErrCandidateConflict, protocol.CategoryConflict,
		)
		if !reflect.DeepEqual(protocolError.Details["paths"], []string{"README.md"}) {
			t.Fatalf("ordinary drift paths are not canonical: %#v", protocolError.Details)
		}
	})

	mainHead, err := original.Runner.Resolve(ctx, "refs/heads/main")
	if err != nil {
		t.Fatal(err)
	}
	if mainHead != original.Verified.Head || !reflect.DeepEqual(plan.Operation, operationBefore) {
		t.Fatalf("CAS planning mutated main or stable Operation: main=%s operation=%#v", mainHead, plan.Operation)
	}
}
