package workflow

import (
	"context"
	"strings"
	"testing"

	"github.com/ExplodeCode6324/chassiss/internal/contracts"
)

func TestCheckContextAndResults(t *testing.T) {
	spec := contracts.CheckSpec{
		ID: "CHECK-001", Argv: []string{"true"}, Cwd: ".", TimeoutSeconds: 5,
	}
	task := "TASK-001"
	binding := CheckBinding{
		ArchitectureBlob: strings.Repeat("a", 40),
		Head:             strings.Repeat("b", 40), Phase: "submission", Task: &task,
		TaskbookBlob: strings.Repeat("c", 40), Tree: strings.Repeat("d", 40),
	}
	results, _, err := RunChecks(context.Background(), t.TempDir(), []contracts.CheckSpec{spec}, binding)
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateCheckResults([]contracts.CheckSpec{spec}, results, binding, true); err != nil {
		t.Fatal(err)
	}
	wrong := *results[0].ExitCode
	wrong++
	results[0].ExitCode = &wrong
	if err := ValidateCheckResults([]contracts.CheckSpec{spec}, results, binding, true); err == nil {
		t.Fatal("invalid pass exit code unexpectedly accepted")
	}
}

func TestCappedBufferReportsFullWriteAndBoundsStorage(t *testing.T) {
	buffer := cappedBuffer{limit: maxCheckLogBytes}
	payload := []byte(strings.Repeat("x", maxCheckLogBytes+100))

	written, err := buffer.Write(payload)
	if err != nil {
		t.Fatal(err)
	}
	if written != len(payload) {
		t.Fatalf("reported write length %d, want %d", written, len(payload))
	}
	if buffer.Len() != maxCheckLogBytes {
		t.Fatalf("stored %d bytes, want cap %d", buffer.Len(), maxCheckLogBytes)
	}

	written, err = buffer.Write([]byte("discarded"))
	if err != nil {
		t.Fatal(err)
	}
	if written != len("discarded") {
		t.Fatalf("reported write length %d after reaching cap", written)
	}
	if buffer.Len() != maxCheckLogBytes {
		t.Fatalf("buffer grew past cap to %d bytes", buffer.Len())
	}
}

func TestReviewReportApprovalRules(t *testing.T) {
	report := ReviewReport{
		Schema: ReviewReportSchema, Verdict: "approve", Summary: "accepted",
		Results: ReviewResults{
			Architecture: "conformant", Contract: "pass",
			Integration: "pass", Requirements: "pass",
		},
		Findings: []Finding{},
		ReviewerAttentionResponses: []AttentionResponse{
			{Attention: "inspect state", Response: "verified"},
		},
	}
	if err := report.Validate([]string{"inspect state"}, map[string]contracts.Resource{}); err != nil {
		t.Fatal(err)
	}
	report.Findings = []Finding{{
		Category: "contract", Paths: []string{}, Resources: []string{},
		Severity: "blocking", Summary: "broken",
	}}
	if err := report.Validate([]string{"inspect state"}, map[string]contracts.Resource{}); err == nil {
		t.Fatal("blocking approve unexpectedly accepted")
	}
}
