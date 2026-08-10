package protocol

import (
	"fmt"
	"sort"
	"strings"
)

type Operation struct {
	Schema        string         `json:"schema"`
	OperationID   string         `json:"operation_id"`
	Action        string         `json:"action"`
	Project       string         `json:"project"`
	Authority     string         `json:"authority"`
	Target        string         `json:"target"`
	Preconditions map[string]any `json:"preconditions"`
	Payload       map[string]any `json:"payload"`
}

type ExecutionEvidence struct {
	Schema          string         `json:"schema"`
	OperationDigest string         `json:"operation_digest"`
	Action          string         `json:"action"`
	Attempt         int64          `json:"attempt"`
	Parent          *string        `json:"parent"`
	Facts           map[string]any `json:"facts"`
}

func (op Operation) Validate() error {
	if op.Schema != OperationSchema {
		return fmt.Errorf("operation schema must be %s", OperationSchema)
	}
	if err := ValidateOperationID(op.OperationID); err != nil {
		return err
	}
	spec, ok := Action(op.Action)
	if !ok {
		return fmt.Errorf("unknown v1 action %q", op.Action)
	}
	if err := ValidateID(IDProject, op.Project); err != nil {
		return err
	}
	if op.Authority == "" || (!strings.HasPrefix(op.Authority, "root:") && !strings.HasPrefix(op.Authority, "grant:")) {
		return fmt.Errorf("authority must select root:<key-id> or grant:<grant-id>")
	}
	if spec.TaskAction {
		if err := ValidateID(IDTask, op.Target); err != nil {
			return err
		}
	}
	if op.Action == "architecture.established" || op.Action == "architecture.updated" ||
		op.Action == "architecture.updated-compatible" {
		if err := ValidateID(IDArchitecture, op.Target); err != nil {
			return err
		}
	}
	if err := exactObjectKeys(op.Payload, spec.PayloadFields); err != nil {
		return fmt.Errorf("payload: %w", err)
	}
	if err := exactObjectKeys(op.Preconditions, spec.PreconditionFields); err != nil {
		return fmt.Errorf("preconditions: %w", err)
	}
	return nil
}

func (evidence ExecutionEvidence) Validate(operation Operation, objectFormat string) error {
	if evidence.Schema != EvidenceSchema {
		return fmt.Errorf("execution evidence schema must be %s", EvidenceSchema)
	}
	if evidence.Action != operation.Action {
		return fmt.Errorf("evidence action does not match operation action")
	}
	if evidence.Attempt < 1 || evidence.Attempt > 3 {
		return fmt.Errorf("evidence attempt must be in [1,3]")
	}
	if operation.Action == "project.genesis" || operation.Action == "project.bootstrap" {
		if evidence.Parent != nil {
			return fmt.Errorf("Project bootstrap evidence parent must be null")
		}
	} else {
		if evidence.Parent == nil || ValidateOID(*evidence.Parent, objectFormat) != nil {
			return fmt.Errorf("non-Genesis evidence parent must be a full Git OID")
		}
	}
	expected, err := ObjectDigest("operation", operation)
	if err != nil {
		return err
	}
	if evidence.OperationDigest != expected {
		return fmt.Errorf("evidence operation digest does not match operation")
	}
	spec, _ := Action(operation.Action)
	if err := exactObjectKeys(evidence.Facts, spec.EvidenceFields); err != nil {
		return fmt.Errorf("facts: %w", err)
	}
	return nil
}

func exactObjectKeys(object map[string]any, fields []string) error {
	if object == nil {
		object = map[string]any{}
	}
	expected := append([]string(nil), fields...)
	actual := make([]string, 0, len(object))
	for key := range object {
		actual = append(actual, key)
	}
	sort.Strings(expected)
	sort.Strings(actual)
	if len(expected) != len(actual) {
		return fmt.Errorf("expected exact fields %v, got %v", expected, actual)
	}
	for index := range expected {
		if expected[index] != actual[index] {
			return fmt.Errorf("expected exact fields %v, got %v", expected, actual)
		}
	}
	return nil
}

func ValidateOID(value, objectFormat string) error {
	size := 40
	if objectFormat == "sha256" {
		size = 64
	}
	if len(value) != size {
		return fmt.Errorf("Git OID must have %d hexadecimal characters", size)
	}
	for _, r := range value {
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f')) {
			return fmt.Errorf("Git OID must use lowercase hexadecimal")
		}
	}
	return nil
}
