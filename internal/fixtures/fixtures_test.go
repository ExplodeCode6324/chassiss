package fixtures

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ExplodeCode6324/chassiss/internal/contracts"
	"github.com/ExplodeCode6324/chassiss/internal/protocol"
	"github.com/ExplodeCode6324/chassiss/internal/state"
)

func fixturePath(parts ...string) string {
	all := append([]string{"..", "..", "fixtures"}, parts...)
	return filepath.Join(all...)
}

func readFixture(t *testing.T, parts ...string) []byte {
	t.Helper()
	data, err := os.ReadFile(fixturePath(parts...))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestCanonicalJSONVectors(t *testing.T) {
	var cases []struct {
		Name      string `json:"name"`
		Input     string `json:"input"`
		Canonical string `json:"canonical"`
		Error     string `json:"error"`
	}
	if err := json.Unmarshal(readFixture(t, "canonical-json", "cases.json"), &cases); err != nil {
		t.Fatal(err)
	}
	for _, test := range cases {
		t.Run(test.Name, func(t *testing.T) {
			value, err := protocol.ParseCanonicalInput([]byte(test.Input))
			if test.Error != "" {
				if err == nil || !strings.Contains(err.Error(), test.Error) {
					t.Fatalf("wanted error containing %q, got %v", test.Error, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			actual, err := protocol.CanonicalJSON(value)
			if err != nil {
				t.Fatal(err)
			}
			if string(actual) != test.Canonical {
				t.Fatalf("canonical mismatch\nwant %s\n got %s", test.Canonical, actual)
			}
		})
	}
}

func TestDigestVectors(t *testing.T) {
	var cases []struct {
		Name       string          `json:"name"`
		ObjectType string          `json:"object_type"`
		Value      json.RawMessage `json:"value"`
		Expected   string          `json:"expected"`
	}
	if err := json.Unmarshal(readFixture(t, "digests", "cases.json"), &cases); err != nil {
		t.Fatal(err)
	}
	for _, test := range cases {
		t.Run(test.Name, func(t *testing.T) {
			value, err := protocol.ParseCanonicalInput(test.Value)
			if err != nil {
				t.Fatal(err)
			}
			actual, err := protocol.ObjectDigest(test.ObjectType, value)
			if err != nil {
				t.Fatal(err)
			}
			if actual != test.Expected {
				t.Fatalf("digest mismatch\nwant %s\n got %s", test.Expected, actual)
			}
		})
	}
}

func TestArchitectureAndTaskbookVectors(t *testing.T) {
	architecture, err := contracts.ParseArchitecture(
		readFixture(t, "architecture-valid", "minimal.yaml"),
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := contracts.ParseArchitecture(
		readFixture(t, "architecture-invalid", "requires-cycle.yaml"),
	); err == nil {
		t.Fatal("cyclic Architecture unexpectedly passed")
	}
	if _, err := contracts.ParseTaskbook(
		readFixture(t, "taskbook-valid", "minimal.yaml"), architecture,
	); err != nil {
		t.Fatal(err)
	}
	if _, err := contracts.ParseTaskbook(
		readFixture(t, "taskbook-invalid", "duplicate-check-id.yaml"), architecture,
	); err == nil {
		t.Fatal("Taskbook with duplicate Check ID unexpectedly passed")
	}
}

func TestStateVectors(t *testing.T) {
	if _, err := state.Parse(readFixture(t, "state-valid", "genesis.json"), "sha1"); err != nil {
		t.Fatal(err)
	}
	if _, err := state.Parse(readFixture(t, "state-invalid", "terminal-extra-field.json"), "sha1"); err == nil {
		t.Fatal("terminal State with an extra actor unexpectedly passed")
	}
}

func TestGenesisReducerVector(t *testing.T) {
	var operation protocol.Operation
	if err := json.Unmarshal(
		readFixture(t, "reducers", "genesis-success", "operation.json"), &operation,
	); err != nil {
		t.Fatal(err)
	}
	var evidence protocol.ExecutionEvidence
	if err := json.Unmarshal(
		readFixture(t, "reducers", "genesis-success", "evidence.json"), &evidence,
	); err != nil {
		t.Fatal(err)
	}
	var facts struct {
		ObjectFormat      string   `json:"object_format"`
		TaskbookID        string   `json:"genesis_taskbook_id"`
		GenesisReadyTasks []string `json:"genesis_ready_tasks"`
	}
	if err := json.Unmarshal(
		readFixture(t, "reducers", "genesis-success", "facts.json"), &facts,
	); err != nil {
		t.Fatal(err)
	}
	next, err := state.Reduce(nil, operation, evidence, state.ReduceFacts{
		ObjectFormat:      facts.ObjectFormat,
		GenesisTaskbookID: facts.TaskbookID,
		GenesisReadyTasks: facts.GenesisReadyTasks,
	})
	if err != nil {
		t.Fatal(err)
	}
	actual, err := state.Encode(next, facts.ObjectFormat)
	if err != nil {
		t.Fatal(err)
	}
	expected := readFixture(t, "reducers", "genesis-success", "expected.state.json")
	if !bytes.Equal(actual, expected) {
		t.Fatalf("next State mismatch\nwant %s\n got %s", expected, actual)
	}
	digest, err := state.Digest(next, facts.ObjectFormat)
	if err != nil {
		t.Fatal(err)
	}
	expectedDigest := strings.TrimSpace(string(
		readFixture(t, "reducers", "genesis-success", "expected.digest"),
	))
	if digest != expectedDigest {
		t.Fatalf("State digest mismatch\nwant %s\n got %s", expectedDigest, digest)
	}
}

func TestGenesisFailureVector(t *testing.T) {
	var vector struct {
		Operation     protocol.Operation `json:"operation"`
		EvidenceFacts map[string]any     `json:"evidence_facts"`
		GitFacts      struct {
			ObjectFormat      string   `json:"object_format"`
			TaskbookID        string   `json:"genesis_taskbook_id"`
			GenesisReadyTasks []string `json:"genesis_ready_tasks"`
		} `json:"git_facts"`
		ExpectedErrorCode string `json:"expected_error_code"`
	}
	if err := json.Unmarshal(
		readFixture(t, "reducers", "genesis-project-mismatch", "vector.json"), &vector,
	); err != nil {
		t.Fatal(err)
	}
	digest, err := protocol.ObjectDigest("operation", vector.Operation)
	if err != nil {
		t.Fatal(err)
	}
	evidence := protocol.ExecutionEvidence{
		Schema:          protocol.EvidenceSchema,
		OperationDigest: digest,
		Action:          vector.Operation.Action,
		Attempt:         1,
		Parent:          nil,
		Facts:           vector.EvidenceFacts,
	}
	_, err = state.Reduce(nil, vector.Operation, evidence, state.ReduceFacts{
		ObjectFormat:      vector.GitFacts.ObjectFormat,
		GenesisTaskbookID: vector.GitFacts.TaskbookID,
		GenesisReadyTasks: vector.GitFacts.GenesisReadyTasks,
	})
	if err == nil || !strings.Contains(err.Error(), vector.ExpectedErrorCode) {
		t.Fatalf("wanted %s, got %v", vector.ExpectedErrorCode, err)
	}
}

func TestScenarioManifests(t *testing.T) {
	directories := []string{
		"signatures", "mainline-valid", "mainline-invalid", "drift",
		"integration", "local-recovery",
	}
	for _, directory := range directories {
		t.Run(directory, func(t *testing.T) {
			var scenarios []string
			if err := json.Unmarshal(
				readFixture(t, directory, "scenarios.json"), &scenarios,
			); err != nil {
				t.Fatal(err)
			}
			if len(scenarios) == 0 {
				t.Fatal("scenario manifest must not be empty")
			}
			seen := make(map[string]struct{}, len(scenarios))
			for _, scenario := range scenarios {
				if strings.TrimSpace(scenario) == "" {
					t.Fatal("scenario name must not be empty")
				}
				if _, exists := seen[scenario]; exists {
					t.Fatalf("duplicate scenario %q", scenario)
				}
				seen[scenario] = struct{}{}
			}
		})
	}
}
