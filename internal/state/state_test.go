package state

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/ExplodeCode6324/chassiss/internal/protocol"
	"golang.org/x/crypto/ssh"
)

func publicKey(t *testing.T) string {
	t.Helper()
	public, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	key, err := ssh.NewPublicKey(public)
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(ssh.MarshalAuthorizedKey(key)))
}

func TestReadableTemplateAndCanonicalRoundTrip(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "docs", "templates", "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	data = []byte(strings.Replace(string(data), "ssh-ed25519 <root-public-key>", publicKey(t), 1))
	value, err := ParseReadable(data, "sha1")
	if err != nil {
		t.Fatal(err)
	}
	canonical, err := Encode(value, "sha1")
	if err != nil {
		t.Fatal(err)
	}
	if canonical[len(canonical)-1] != '\n' || canonical[len(canonical)-2] == '\n' {
		t.Fatal("canonical State must have exactly one trailing LF")
	}
	parsed, err := Parse(canonical, "sha1")
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Project.ID != "PRJ-EXAMPLE" {
		t.Fatalf("unexpected Project %s", parsed.Project.ID)
	}
}

func TestSparseTaskValidation(t *testing.T) {
	falseValue := false
	invalid := []TaskState{
		{Phase: "ready", Actor: "agent"},
		{Phase: "active"},
		{Phase: "submitted", Actor: "agent-a", Base: strings.Repeat("a", 40), Contract: &Contract{
			ArchitectureBlob: strings.Repeat("b", 40), TaskbookBlob: strings.Repeat("c", 40),
		}},
		{Phase: "closed", Blocked: &falseValue},
	}
	for index, value := range invalid {
		if err := value.Validate("sha1"); err == nil {
			t.Errorf("fixture %d unexpectedly passed", index)
		}
	}
}

func compatibleArchitectureFixture(
	t *testing.T,
	tasks map[string]TaskState,
) (*State, protocol.Operation, protocol.ExecutionEvidence, ReduceFacts) {
	t.Helper()
	architectureBlob := strings.Repeat("a", 40)
	taskbookBlob := strings.Repeat("b", 40)
	parentOID := strings.Repeat("8", 40)
	parent := &State{
		Schema: protocol.StateSchema, Protocol: protocol.ProtocolID,
		Project: Project{
			ID:           "PRJ-COMPATIBLE",
			Architecture: &BlobRef{Path: "docs/architecture.yaml", BlobOID: architectureBlob},
			Taskbook:     &TaskbookRef{Path: "docs/taskbook.yaml", BlobOID: taskbookBlob, ID: "TASKBOOK-001"},
		},
		Authority: Authority{
			Root: Root{KeyID: "KEY-ROOT-01", PublicKey: publicKey(t)},
			Grants: map[string]Grant{"GRT-ARCHITECT-01": {
				Actor: "architect", KeyID: "KEY-ARCHITECT-01", PublicKey: publicKey(t),
				Capabilities: []string{"architecture.update"},
				Scope:        Scope{Tasks: []string{"*"}, Resources: []string{"*"}},
				Limits:       Limits{Mode: "unbounded"},
			}},
		},
		Tasks: tasks,
	}
	operation := protocol.Operation{
		Schema: protocol.OperationSchema, OperationID: "OPR-91ARZ3NDEKTSV4RRFFQ69G5FAV",
		Action: "architecture.updated-compatible", Project: parent.Project.ID,
		Authority: "grant:GRT-ARCHITECT-01", Target: "ARCHITECTURE-001",
		Preconditions: map[string]any{
			"all_tasks_quiescent": true, "architecture_blob": architectureBlob,
			"taskbook_blob": taskbookBlob,
		},
		Payload: map[string]any{"candidate_blob": strings.Repeat("c", 40), "reason": "compatible update"},
	}
	digest, err := protocol.ObjectDigest("operation", operation)
	if err != nil {
		t.Fatal(err)
	}
	evidence := protocol.ExecutionEvidence{
		Schema: protocol.EvidenceSchema, OperationDigest: digest,
		Action: operation.Action, Attempt: 1, Parent: &parentOID,
		Facts: map[string]any{
			"new_blob": strings.Repeat("c", 40), "old_blob": architectureBlob,
			"semantic_diff": map[string]any{}, "taskbook_blob": taskbookBlob,
		},
	}
	return parent, operation, evidence, ReduceFacts{
		ObjectFormat: "sha1", ParentCommit: parentOID,
		TargetResources: []string{"module:core"},
	}
}

func inFlightTaskFixture(phase string, blocked bool) TaskState {
	task := TaskState{
		Phase: phase, Actor: "builder", Base: strings.Repeat("d", 40),
		Contract: &Contract{
			ArchitectureBlob: strings.Repeat("a", 40),
			TaskbookBlob:     strings.Repeat("b", 40),
		},
	}
	if blocked {
		value := true
		task.Blocked = &value
	}
	if phase == "submitted" || phase == "approved" {
		task.Attempt = &Attempt{
			Head: strings.Repeat("e", 40), EvidenceDigest: "sha256:" + strings.Repeat("1", 64),
			SubmitterKeyFingerprint: "SHA256:submitter",
		}
	}
	if phase == "approved" {
		task.Review = &Review{
			CandidateTree: strings.Repeat("f", 40), ContextDigest: "sha256:" + strings.Repeat("2", 64),
			KeyFingerprint: "SHA256:reviewer", KeyID: "KEY-REVIEWER-01",
			ReportDigest: "sha256:" + strings.Repeat("3", 64), ReviewMain: strings.Repeat("9", 40),
			Reviewer: "reviewer",
		}
	}
	return task
}

func TestCompatibleArchitectureReducerQuiescenceAndBinding(t *testing.T) {
	blocked := true
	quiescent := map[string]TaskState{
		"TASK-001": {Phase: "ready", Blocked: &blocked},
		"TASK-002": {Phase: "closed"},
		"TASK-003": {Phase: "cancelled"},
		"TASK-004": {Phase: "superseded"},
	}
	parent, operation, evidence, facts := compatibleArchitectureFixture(t, quiescent)
	next, err := Reduce(parent, operation, evidence, facts)
	if err != nil {
		t.Fatalf("quiescent compatible update rejected: %v", err)
	}
	if next.Project.Architecture.BlobOID != strings.Repeat("c", 40) ||
		next.Project.Taskbook.BlobOID != parent.Project.Taskbook.BlobOID ||
		!reflect.DeepEqual(next.Tasks, parent.Tasks) {
		t.Fatalf("compatible update changed more than Architecture: %#v", next)
	}

	for _, test := range []struct {
		name    string
		phase   string
		blocked bool
	}{
		{name: "active", phase: "active"},
		{name: "submitted", phase: "submitted"},
		{name: "approved", phase: "approved"},
		{name: "blocked-active", phase: "active", blocked: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			parent, operation, evidence, facts := compatibleArchitectureFixture(t, map[string]TaskState{
				"TASK-001": inFlightTaskFixture(test.phase, test.blocked),
			})
			if _, err := Reduce(parent, operation, evidence, facts); err == nil ||
				!strings.Contains(err.Error(), protocol.ErrTaskbookNotQuiescent) {
				t.Fatalf("phase %s was not rejected uniformly: %v", test.phase, err)
			}
		})
	}

	parent, operation, evidence, facts = compatibleArchitectureFixture(t, map[string]TaskState{
		"TASK-001": {Phase: "ready"},
	})
	operation.Preconditions["taskbook_blob"] = strings.Repeat("7", 40)
	digest, err := protocol.ObjectDigest("operation", operation)
	if err != nil {
		t.Fatal(err)
	}
	evidence.OperationDigest = digest
	if _, err := Reduce(parent, operation, evidence, facts); err == nil ||
		!strings.Contains(err.Error(), protocol.ErrTaskbookStale) {
		t.Fatalf("stale Taskbook precondition was not rejected: %v", err)
	}

	parent, operation, evidence, facts = compatibleArchitectureFixture(t, map[string]TaskState{
		"TASK-001": {Phase: "ready"},
	})
	evidence.Facts["taskbook_blob"] = strings.Repeat("7", 40)
	if _, err := Reduce(parent, operation, evidence, facts); err == nil ||
		!strings.Contains(err.Error(), protocol.ErrTaskbookStale) {
		t.Fatalf("stale Taskbook evidence was not rejected: %v", err)
	}
}

func TestBootstrapReducerEstablishesFirstArchitecture(t *testing.T) {
	rootPublic := publicKey(t)
	agentPublic := publicKey(t)
	sourceCommit := strings.Repeat("1", 40)
	sourceTree := strings.Repeat("2", 40)
	historyBlob := strings.Repeat("3", 40)
	bootstrap := protocol.Operation{
		Schema: protocol.OperationSchema, OperationID: "OPR-01BRZ3NDEKTSV4RRFFQ69G5FAV",
		Action: "project.bootstrap", Project: "PRJ-BOOTSTRAP",
		Authority: "root:KEY-ROOT-BOOTSTRAP", Target: "PRJ-BOOTSTRAP",
		Preconditions: map[string]any{},
		Payload: map[string]any{
			"project_id": "PRJ-BOOTSTRAP", "root_key_id": "KEY-ROOT-BOOTSTRAP",
			"root_public_key": rootPublic, "source_commit": sourceCommit,
			"source_history_blob": historyBlob, "source_object_format": "sha1",
			"source_tree": sourceTree,
		},
	}
	current, err := Reduce(nil, bootstrap, evidenceFor(t, bootstrap, nil, map[string]any{
		"initial_tree": strings.Repeat("4", 40), "source_commit": sourceCommit,
		"source_history_blob": historyBlob, "source_tree": sourceTree,
	}), ReduceFacts{ObjectFormat: "sha1"})
	if err != nil {
		t.Fatal(err)
	}
	if current.Project.Architecture != nil || current.Project.Source == nil ||
		current.Project.Source.Commit != sourceCommit {
		t.Fatalf("unexpected bootstrap projection: %#v", current.Project)
	}
	grant := Grant{
		Actor: "architect", Capabilities: []string{"architecture.establish"},
		KeyID: "KEY-ARCHITECT-01", Limits: Limits{Mode: "unbounded"},
		PublicKey: agentPublic, Scope: Scope{Tasks: []string{"*"}, Resources: []string{"*"}},
	}
	current = reduceStep(t, current, protocol.Operation{
		Schema: protocol.OperationSchema, OperationID: "OPR-11BRZ3NDEKTSV4RRFFQ69G5FAV",
		Action: "authority.grant-added", Project: "PRJ-BOOTSTRAP",
		Authority: "root:KEY-ROOT-BOOTSTRAP", Target: "GRT-ARCHITECT-01",
		Preconditions: map[string]any{"grant_absent": true, "root_key_id": "KEY-ROOT-BOOTSTRAP"},
		Payload: map[string]any{
			"grant": toMap(t, grant), "grant_id": "GRT-ARCHITECT-01", "request_digest": nil,
		},
	}, map[string]any{}, ReduceFacts{})
	architectureBlob := strings.Repeat("5", 40)
	current = reduceStep(t, current, protocol.Operation{
		Schema: protocol.OperationSchema, OperationID: "OPR-21BRZ3NDEKTSV4RRFFQ69G5FAV",
		Action: "architecture.established", Project: "PRJ-BOOTSTRAP",
		Authority: "grant:GRT-ARCHITECT-01", Target: "ARCHITECTURE-001",
		Preconditions: map[string]any{"architecture": nil, "taskbook": nil},
		Payload: map[string]any{
			"candidate_blob": architectureBlob, "reason": "audited source",
		},
	}, map[string]any{"new_blob": architectureBlob}, ReduceFacts{
		TargetResources: []string{"module:root"}, RequireGlobalScope: true,
	})
	if current.Project.Architecture == nil ||
		current.Project.Architecture.BlobOID != architectureBlob ||
		current.Project.Source == nil || current.Project.Source.Commit != sourceCommit {
		t.Fatalf("Architecture establish lost bootstrap facts: %#v", current.Project)
	}
}

func TestReducerLifecycle(t *testing.T) {
	rootPublic := publicKey(t)
	agentPublic := publicKey(t)
	architectureBlob := strings.Repeat("a", 40)
	taskbookBlob := strings.Repeat("b", 40)
	genesisOperation := protocol.Operation{
		Schema: protocol.OperationSchema, OperationID: "OPR-01ARZ3NDEKTSV4RRFFQ69G5FAV",
		Action: "project.genesis", Project: "PRJ-EXAMPLE",
		Authority: "root:KEY-ROOT-01", Target: "PRJ-EXAMPLE",
		Preconditions: map[string]any{},
		Payload: map[string]any{
			"architecture_blob": architectureBlob,
			"project_id":        "PRJ-EXAMPLE",
			"root_key_id":       "KEY-ROOT-01",
			"root_public_key":   rootPublic,
			"taskbook_blob":     taskbookBlob,
		},
	}
	genesisEvidence := evidenceFor(t, genesisOperation, nil, map[string]any{
		"architecture_blob": architectureBlob,
		"initial_tree":      strings.Repeat("c", 40),
		"taskbook_blob":     taskbookBlob,
	})
	current, err := Reduce(nil, genesisOperation, genesisEvidence, ReduceFacts{
		ObjectFormat: "sha1", GenesisTaskbookID: "TASKBOOK-001",
		GenesisReadyTasks: []string{"TASK-001"},
	})
	if err != nil {
		t.Fatal(err)
	}

	capabilities := []string{
		"integration.apply", "review.attest", "task.block", "task.resume",
		"task.start", "task.submit",
	}
	sort.Strings(capabilities)
	maxActive := int64(1)
	grant := Grant{
		Actor: "agent-one", Capabilities: capabilities, KeyID: "KEY-AGENT-01",
		Limits:    Limits{Mode: "bounded", MaxActiveTasks: &maxActive},
		PublicKey: agentPublic,
		Scope:     Scope{Tasks: []string{"TASK-*"}, Resources: []string{"module:*"}},
	}
	grantMap := toMap(t, grant)
	current = reduceStep(t, current, protocol.Operation{
		Schema: protocol.OperationSchema, OperationID: "OPR-11ARZ3NDEKTSV4RRFFQ69G5FAV",
		Action: "authority.grant-added", Project: "PRJ-EXAMPLE",
		Authority: "root:KEY-ROOT-01", Target: "GRT-AGENT-01",
		Preconditions: map[string]any{"grant_absent": true, "root_key_id": "KEY-ROOT-01"},
		Payload:       map[string]any{"grant": grantMap, "grant_id": "GRT-AGENT-01", "request_digest": nil},
	}, map[string]any{}, ReduceFacts{})

	base := strings.Repeat("d", 40)
	current = reduceStep(t, current, protocol.Operation{
		Schema: protocol.OperationSchema, OperationID: "OPR-21ARZ3NDEKTSV4RRFFQ69G5FAV",
		Action: "task.started", Project: "PRJ-EXAMPLE",
		Authority: "grant:GRT-AGENT-01", Target: "TASK-001",
		Preconditions: map[string]any{
			"architecture_blob": architectureBlob, "phase": "ready", "taskbook_blob": taskbookBlob,
		},
		Payload: map[string]any{},
	}, map[string]any{
		"actor": "agent-one", "architecture_blob": architectureBlob,
		"base": base, "taskbook_blob": taskbookBlob,
	}, ReduceFacts{TaskResources: []string{"module:core"}})

	current = reduceStep(t, current, protocol.Operation{
		Schema: protocol.OperationSchema, OperationID: "OPR-31ARZ3NDEKTSV4RRFFQ69G5FAV",
		Action: "task.blocked", Project: "PRJ-EXAMPLE",
		Authority: "grant:GRT-AGENT-01", Target: "TASK-001",
		Preconditions: map[string]any{"blocked": false, "phase": "active"},
		Payload:       map[string]any{"reason": "dependency unavailable"},
	}, map[string]any{}, ReduceFacts{TaskResources: []string{"module:core"}})
	if current.Tasks["TASK-001"].Blocked == nil {
		t.Fatal("Task was not blocked")
	}

	current = reduceStep(t, current, protocol.Operation{
		Schema: protocol.OperationSchema, OperationID: "OPR-41ARZ3NDEKTSV4RRFFQ69G5FAV",
		Action: "task.resumed", Project: "PRJ-EXAMPLE",
		Authority: "grant:GRT-AGENT-01", Target: "TASK-001",
		Preconditions: map[string]any{"blocked": true, "phase": "active"},
		Payload:       map[string]any{"reason": nil},
	}, map[string]any{}, ReduceFacts{TaskResources: []string{"module:core"}})

	head := strings.Repeat("e", 40)
	submission := map[string]any{
		"architecture_blob":    architectureBlob,
		"base":                 base,
		"changed_paths_count":  int64(1),
		"changed_paths_digest": "sha256:" + strings.Repeat("1", 64),
		"check_results":        []any{},
		"head":                 head,
		"schema":               "chassiss.submission-evidence/v1",
		"submitter": map[string]any{
			"actor": "agent-one", "key_fingerprint": mustFingerprint(t, agentPublic),
		},
		"task":          "TASK-001",
		"taskbook_blob": taskbookBlob,
		"tree":          strings.Repeat("f", 40),
	}
	current = reduceStep(t, current, protocol.Operation{
		Schema: protocol.OperationSchema, OperationID: "OPR-51ARZ3NDEKTSV4RRFFQ69G5FAV",
		Action: "task.submitted", Project: "PRJ-EXAMPLE",
		Authority: "grant:GRT-AGENT-01", Target: "TASK-001",
		Preconditions: map[string]any{
			"actor": "agent-one", "architecture_blob": architectureBlob, "base": base,
			"phase": "active", "taskbook_blob": taskbookBlob,
		},
		Payload: map[string]any{"head": head},
	}, map[string]any{"submission_evidence": submission}, ReduceFacts{
		TaskResources: []string{"module:core"}, ChangedPaths: 1,
	})

	attemptDigest, err := AttemptDigest("TASK-001", current.Tasks["TASK-001"])
	if err != nil {
		t.Fatal(err)
	}
	report := map[string]any{
		"findings": []any{},
		"results": map[string]any{
			"architecture": "conformant", "contract": "pass",
			"integration": "pass", "requirements": "pass",
		},
		"reviewer_attention_responses": []any{},
		"schema":                       "chassiss.review-report/v1",
		"summary":                      "accepted",
		"verdict":                      "approve",
	}
	reviewMain := strings.Repeat("1", 40)
	candidate := strings.Repeat("2", 40)
	context := map[string]any{
		"artifact_tree": strings.Repeat("3", 40), "architecture_blob": architectureBlob,
		"attempt_head": head, "candidate_tree": candidate, "check_results": []any{},
		"requires_closure": []any{}, "review_main": reviewMain,
		"schema": "chassiss.review-context/v1", "task": "TASK-001",
		"task_base": base, "taskbook_blob": taskbookBlob,
	}
	current = reduceStep(t, current, protocol.Operation{
		Schema: protocol.OperationSchema, OperationID: "OPR-61ARZ3NDEKTSV4RRFFQ69G5FAV",
		Action: "task.reviewed-indexed", Project: "PRJ-EXAMPLE",
		Authority: "grant:GRT-AGENT-01", Target: "TASK-001",
		Preconditions: map[string]any{"attempt_digest": attemptDigest, "phase": "submitted"},
		Payload:       map[string]any{"report": report, "verdict": "approve"},
	}, map[string]any{
		"attempt_digest": attemptDigest, "check_results": []any{}, "review_context": context,
	}, ReduceFacts{TaskResources: []string{"module:core"}})
	if current.Tasks["TASK-001"].Phase != "approved" {
		t.Fatal("Task was not approved")
	}
	if current.Audit == nil || len(current.Audit.Reviews) != 1 ||
		current.Audit.Reviews[0].OperationID != "OPR-61ARZ3NDEKTSV4RRFFQ69G5FAV" {
		t.Fatalf("Review audit index is missing: %#v", current.Audit)
	}

	review := current.Tasks["TASK-001"].Review
	current = reduceStep(t, current, protocol.Operation{
		Schema: protocol.OperationSchema, OperationID: "OPR-71ARZ3NDEKTSV4RRFFQ69G5FAV",
		Action: "integration.applied", Project: "PRJ-EXAMPLE",
		Authority: "grant:GRT-AGENT-01", Target: "TASK-001",
		Preconditions: map[string]any{
			"attempt_digest": attemptDigest, "phase": "approved",
			"review_context_digest": review.ContextDigest, "review_report_digest": review.ReportDigest,
		},
		Payload: map[string]any{},
	}, map[string]any{
		"attempt_head": head, "candidate_tree": candidate, "check_results": []any{},
		"drift_classification": map[string]any{}, "review_context_digest": review.ContextDigest,
		"review_report_digest": review.ReportDigest,
	}, ReduceFacts{TaskResources: []string{"module:core"}})
	if current.Tasks["TASK-001"] != (TaskState{Phase: "closed"}) {
		t.Fatalf("unexpected closed projection: %#v", current.Tasks["TASK-001"])
	}
}

func reduceStep(t *testing.T, parent *State, operation protocol.Operation, evidenceFacts map[string]any, facts ReduceFacts) *State {
	t.Helper()
	parentOID := strings.Repeat(string('a'+rune(operation.OperationID[4]%6)), 40)
	evidence := evidenceFor(t, operation, &parentOID, evidenceFacts)
	facts.ObjectFormat = "sha1"
	facts.ParentCommit = parentOID
	next, err := Reduce(parent, operation, evidence, facts)
	if err != nil {
		t.Fatalf("%s: %v", operation.Action, err)
	}
	return next
}

func evidenceFor(t *testing.T, operation protocol.Operation, parent *string, facts map[string]any) protocol.ExecutionEvidence {
	t.Helper()
	digest, err := protocol.ObjectDigest("operation", operation)
	if err != nil {
		t.Fatal(err)
	}
	return protocol.ExecutionEvidence{
		Schema: protocol.EvidenceSchema, OperationDigest: digest,
		Action: operation.Action, Attempt: 1, Parent: parent, Facts: facts,
	}
}

func toMap(t *testing.T, value any) map[string]any {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var result map[string]any
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}
	return result
}

func mustFingerprint(t *testing.T, public string) string {
	t.Helper()
	key, _, _, _, err := ssh.ParseAuthorizedKey([]byte(public))
	if err != nil {
		t.Fatal(err)
	}
	return ssh.FingerprintSHA256(key)
}
