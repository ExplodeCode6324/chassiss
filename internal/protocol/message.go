package protocol

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

type Trailers struct {
	Protocol        int
	Action          string
	Project         string
	StateDigest     string
	Authority       string
	OperationID     string
	OperationDigest string
	EvidenceDigest  string
	Task            string
}

type TransitionMessage struct {
	Operation Operation
	Evidence  ExecutionEvidence
	Trailers  Trailers
}

func BuildTransitionMessage(operation Operation, evidence ExecutionEvidence, stateDigest string, objectFormat string) (string, error) {
	if err := operation.Validate(); err != nil {
		return "", err
	}
	if err := evidence.Validate(operation, objectFormat); err != nil {
		return "", err
	}
	if err := ValidateDigest(stateDigest); err != nil {
		return "", err
	}
	operationBytes, err := CanonicalJSON(operation)
	if err != nil {
		return "", err
	}
	evidenceBytes, err := CanonicalJSON(evidence)
	if err != nil {
		return "", err
	}
	operationDigest, _ := ObjectDigest("operation", operation)
	evidenceDigest, _ := ObjectDigest("execution-evidence", evidence)

	var out strings.Builder
	fmt.Fprintf(&out, "CHASSISS %s\n\n", operation.Action)
	out.WriteString("-----BEGIN CHASSISS OPERATION-----\n")
	out.WriteString(base64.RawURLEncoding.EncodeToString(operationBytes))
	out.WriteString("\n-----END CHASSISS OPERATION-----\n\n")
	out.WriteString("-----BEGIN CHASSISS EXECUTION EVIDENCE-----\n")
	out.WriteString(base64.RawURLEncoding.EncodeToString(evidenceBytes))
	out.WriteString("\n-----END CHASSISS EXECUTION EVIDENCE-----\n\n")
	fmt.Fprintf(&out, "CHASSISS-Protocol: 1\n")
	fmt.Fprintf(&out, "CHASSISS-Action: %s\n", operation.Action)
	fmt.Fprintf(&out, "CHASSISS-Project: %s\n", operation.Project)
	fmt.Fprintf(&out, "CHASSISS-State-Digest: %s\n", stateDigest)
	fmt.Fprintf(&out, "CHASSISS-Authority: %s\n", operation.Authority)
	fmt.Fprintf(&out, "CHASSISS-Operation-ID: %s\n", operation.OperationID)
	fmt.Fprintf(&out, "CHASSISS-Operation-Digest: %s\n", operationDigest)
	fmt.Fprintf(&out, "CHASSISS-Evidence-Digest: %s\n", evidenceDigest)
	if spec, _ := Action(operation.Action); spec.TaskAction {
		fmt.Fprintf(&out, "CHASSISS-Task: %s\n", operation.Target)
	}
	return out.String(), nil
}

var messagePattern = regexp.MustCompile(`(?s)\ACHASSISS ([a-z.-]+)\n\n-----BEGIN CHASSISS OPERATION-----\n([A-Za-z0-9_-]+)\n-----END CHASSISS OPERATION-----\n\n-----BEGIN CHASSISS EXECUTION EVIDENCE-----\n([A-Za-z0-9_-]+)\n-----END CHASSISS EXECUTION EVIDENCE-----\n\n(.+)\z`)

func ParseTransitionMessage(message, objectFormat string) (TransitionMessage, error) {
	match := messagePattern.FindStringSubmatch(message)
	if match == nil {
		return TransitionMessage{}, fmt.Errorf("transition message does not use the exact v1 structure")
	}
	opBytes, err := base64.RawURLEncoding.DecodeString(match[2])
	if err != nil {
		return TransitionMessage{}, fmt.Errorf("invalid operation base64url: %w", err)
	}
	evidenceBytes, err := base64.RawURLEncoding.DecodeString(match[3])
	if err != nil {
		return TransitionMessage{}, fmt.Errorf("invalid evidence base64url: %w", err)
	}
	var operation Operation
	if err := unmarshalClosedJSON(opBytes, &operation); err != nil {
		return TransitionMessage{}, fmt.Errorf("operation: %w", err)
	}
	var evidence ExecutionEvidence
	if err := unmarshalClosedJSON(evidenceBytes, &evidence); err != nil {
		return TransitionMessage{}, fmt.Errorf("evidence: %w", err)
	}
	canonicalOp, _ := CanonicalJSON(operation)
	canonicalEvidence, _ := CanonicalJSON(evidence)
	if string(canonicalOp) != string(opBytes) || string(canonicalEvidence) != string(evidenceBytes) {
		return TransitionMessage{}, fmt.Errorf("operation or evidence block is not canonical JSON")
	}
	if operation.Action != match[1] {
		return TransitionMessage{}, fmt.Errorf("message subject action mismatch")
	}
	trailers, err := parseTrailers(match[4], operation)
	if err != nil {
		return TransitionMessage{}, err
	}
	if err := operation.Validate(); err != nil {
		return TransitionMessage{}, err
	}
	if err := evidence.Validate(operation, objectFormat); err != nil {
		return TransitionMessage{}, err
	}
	opDigest, _ := ObjectDigest("operation", operation)
	evidenceDigest, _ := ObjectDigest("execution-evidence", evidence)
	if trailers.OperationDigest != opDigest || trailers.EvidenceDigest != evidenceDigest {
		return TransitionMessage{}, fmt.Errorf("transition trailer digest mismatch")
	}
	return TransitionMessage{Operation: operation, Evidence: evidence, Trailers: trailers}, nil
}

func unmarshalClosedJSON(data []byte, target any) error {
	if _, err := ParseCanonicalInput(data); err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	decoder.UseNumber()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if decoder.More() {
		return fmt.Errorf("unexpected trailing JSON")
	}
	return nil
}

func parseTrailers(block string, operation Operation) (Trailers, error) {
	lines := strings.Split(strings.TrimSuffix(block, "\n"), "\n")
	spec, exists := Action(operation.Action)
	if !exists {
		return Trailers{}, fmt.Errorf("unknown action %q", operation.Action)
	}
	expectedCount := 8
	if spec.TaskAction {
		expectedCount++
	}
	if len(lines) != expectedCount {
		return Trailers{}, fmt.Errorf("expected %d exact trailers, got %d", expectedCount, len(lines))
	}
	expectedNames := []string{
		"CHASSISS-Protocol",
		"CHASSISS-Action",
		"CHASSISS-Project",
		"CHASSISS-State-Digest",
		"CHASSISS-Authority",
		"CHASSISS-Operation-ID",
		"CHASSISS-Operation-Digest",
		"CHASSISS-Evidence-Digest",
	}
	if spec.TaskAction {
		expectedNames = append(expectedNames, "CHASSISS-Task")
	}
	values := make(map[string]string, len(lines))
	for index, line := range lines {
		name, value, ok := strings.Cut(line, ": ")
		if !ok || name != expectedNames[index] || value == "" {
			return Trailers{}, fmt.Errorf("trailer %d must be %s with a non-empty value", index+1, expectedNames[index])
		}
		values[name] = value
	}
	protocolMajor, err := strconv.Atoi(values["CHASSISS-Protocol"])
	if err != nil || protocolMajor != 1 {
		return Trailers{}, fmt.Errorf("CHASSISS protocol major must be 1")
	}
	if values["CHASSISS-Action"] != operation.Action ||
		values["CHASSISS-Project"] != operation.Project ||
		values["CHASSISS-Authority"] != operation.Authority ||
		values["CHASSISS-Operation-ID"] != operation.OperationID {
		return Trailers{}, fmt.Errorf("transition trailers do not match the operation")
	}
	if spec.TaskAction && values["CHASSISS-Task"] != operation.Target {
		return Trailers{}, fmt.Errorf("CHASSISS-Task does not match the operation target")
	}
	for _, name := range []string{
		"CHASSISS-State-Digest",
		"CHASSISS-Operation-Digest",
		"CHASSISS-Evidence-Digest",
	} {
		if err := ValidateDigest(values[name]); err != nil {
			return Trailers{}, fmt.Errorf("%s: %w", name, err)
		}
	}
	return Trailers{
		Protocol:        protocolMajor,
		Action:          values["CHASSISS-Action"],
		Project:         values["CHASSISS-Project"],
		StateDigest:     values["CHASSISS-State-Digest"],
		Authority:       values["CHASSISS-Authority"],
		OperationID:     values["CHASSISS-Operation-ID"],
		OperationDigest: values["CHASSISS-Operation-Digest"],
		EvidenceDigest:  values["CHASSISS-Evidence-Digest"],
		Task:            values["CHASSISS-Task"],
	}, nil
}
