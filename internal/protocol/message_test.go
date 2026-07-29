package protocol

import (
	"strings"
	"testing"
)

func TestTransitionMessageRoundTrip(t *testing.T) {
	operation := Operation{
		Schema:      OperationSchema,
		OperationID: "OPR-01ARZ3NDEKTSV4RRFFQ69G5FAV",
		Action:      "task.blocked",
		Project:     "PRJ-EXAMPLE",
		Authority:   "grant:GRT-BUILDER-01",
		Target:      "TASK-001",
		Preconditions: map[string]any{
			"blocked": false,
			"phase":   "active",
		},
		Payload: map[string]any{"reason": "waiting for input"},
	}
	operationDigest, err := ObjectDigest("operation", operation)
	if err != nil {
		t.Fatal(err)
	}
	parent := strings.Repeat("a", 40)
	evidence := ExecutionEvidence{
		Schema:          EvidenceSchema,
		OperationDigest: operationDigest,
		Action:          operation.Action,
		Attempt:         1,
		Parent:          &parent,
		Facts:           map[string]any{},
	}
	stateDigest := "sha256:" + strings.Repeat("b", 64)
	message, err := BuildTransitionMessage(operation, evidence, stateDigest, "sha1")
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := ParseTransitionMessage(message, "sha1")
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Operation.OperationID != operation.OperationID {
		t.Fatalf("operation ID mismatch: %s", parsed.Operation.OperationID)
	}
	if parsed.Trailers.StateDigest != stateDigest {
		t.Fatalf("state digest mismatch: %s", parsed.Trailers.StateDigest)
	}
}

func TestTransitionMessageRejectsFreeText(t *testing.T) {
	operation := Operation{
		Schema: OperationSchema, OperationID: "OPR-01ARZ3NDEKTSV4RRFFQ69G5FAV",
		Action: "task.resumed", Project: "PRJ-EXAMPLE",
		Authority: "grant:GRT-BUILDER-01", Target: "TASK-001",
		Preconditions: map[string]any{"blocked": true, "phase": "active"},
		Payload:       map[string]any{"reason": nil},
	}
	digest, _ := ObjectDigest("operation", operation)
	parent := strings.Repeat("a", 40)
	evidence := ExecutionEvidence{
		Schema: EvidenceSchema, OperationDigest: digest, Action: operation.Action,
		Attempt: 1, Parent: &parent, Facts: map[string]any{},
	}
	message, err := BuildTransitionMessage(operation, evidence, "sha256:"+strings.Repeat("b", 64), "sha1")
	if err != nil {
		t.Fatal(err)
	}
	message = strings.Replace(message, "\n\nCHASSISS-Protocol:", "\n\nfree text\n\nCHASSISS-Protocol:", 1)
	if _, err := ParseTransitionMessage(message, "sha1"); err == nil {
		t.Fatal("expected free text rejection")
	}
}

func TestArchitectureOperationRequiresArchitectureTarget(t *testing.T) {
	operation := Operation{
		Schema: OperationSchema, OperationID: "OPR-01BRZ3NDEKTSV4RRFFQ69G5FAV",
		Action: "architecture.established", Project: "PRJ-EXAMPLE",
		Authority: "grant:GRT-ARCHITECT-01", Target: "TASK-001",
		Preconditions: map[string]any{"architecture": nil, "taskbook": nil},
		Payload: map[string]any{
			"candidate_blob": strings.Repeat("a", 40), "reason": "audited source",
		},
	}
	if err := operation.Validate(); err == nil {
		t.Fatal("Architecture operation unexpectedly accepted a non-Architecture target")
	}
}
