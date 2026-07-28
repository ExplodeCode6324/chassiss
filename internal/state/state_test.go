package state

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"os"
	"path/filepath"
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
		Action: "task.reviewed", Project: "PRJ-EXAMPLE",
		Authority: "grant:GRT-AGENT-01", Target: "TASK-001",
		Preconditions: map[string]any{"attempt_digest": attemptDigest, "phase": "submitted"},
		Payload:       map[string]any{"report": report, "verdict": "approve"},
	}, map[string]any{
		"attempt_digest": attemptDigest, "check_results": []any{}, "review_context": context,
	}, ReduceFacts{TaskResources: []string{"module:core"}})
	if current.Tasks["TASK-001"].Phase != "approved" {
		t.Fatal("Task was not approved")
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
