package cli

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/ExplodeCode6324/chassiss/internal/protocol"
)

type Envelope struct {
	AvailableActions []AvailableAction `json:"available_actions"`
	Command          string            `json:"command"`
	Error            *ErrorBody        `json:"error"`
	Extensions       map[string]any    `json:"extensions"`
	Identity         *IdentityBody     `json:"identity"`
	OK               bool              `json:"ok"`
	Operation        *OperationBody    `json:"operation"`
	Project          *ProjectBody      `json:"project"`
	Result           any               `json:"result"`
	Schema           string            `json:"schema"`
	Snapshot         *SnapshotBody     `json:"snapshot"`
	Warnings         []Warning         `json:"warnings"`
}

type ProjectBody struct {
	ID              string `json:"id"`
	Protocol        string `json:"protocol"`
	RootFingerprint string `json:"root_fingerprint"`
}

type SnapshotBody struct {
	ArchitectureBlob string  `json:"architecture_blob"`
	MainCommit       string  `json:"main_commit"`
	Offline          bool    `json:"offline"`
	StateDigest      string  `json:"state_digest"`
	TaskbookBlob     *string `json:"taskbook_blob"`
	Trust            string  `json:"trust"`
}

type IdentityBody struct {
	Actor          string         `json:"actor"`
	Capabilities   []string       `json:"capabilities"`
	GrantID        string         `json:"grant_id"`
	KeyFingerprint string         `json:"key_fingerprint"`
	KeyID          string         `json:"key_id"`
	Limits         map[string]any `json:"limits"`
	Scope          map[string]any `json:"scope"`
}

type OperationBody struct {
	Commit          string `json:"commit"`
	EvidenceAttempt int64  `json:"evidence_attempt"`
	EvidenceDigest  string `json:"evidence_digest"`
	OperationDigest string `json:"operation_digest"`
	OperationID     string `json:"operation_id"`
	Status          string `json:"status"`
}

type AvailableAction struct {
	Action     string   `json:"action"`
	Argv       []string `json:"argv"`
	Capability string   `json:"capability"`
	Target     string   `json:"target"`
}

type Warning struct {
	Code    string         `json:"code"`
	Details map[string]any `json:"details"`
	Message string         `json:"message"`
}

type ErrorBody struct {
	Category    protocol.Category      `json:"category"`
	Code        string                 `json:"code"`
	CurrentHead *string                `json:"current_head"`
	Details     map[string]any         `json:"details"`
	Message     string                 `json:"message"`
	OperationID *string                `json:"operation_id"`
	Remediation []protocol.Remediation `json:"remediation"`
	Retryable   bool                   `json:"retryable"`
}

func baseEnvelope(command string) Envelope {
	return Envelope{
		AvailableActions: []AvailableAction{},
		Command:          command, Error: nil, Extensions: map[string]any{},
		Identity: nil, OK: true, Operation: nil, Project: nil,
		Result: map[string]any{}, Schema: protocol.CLISchema, Snapshot: nil,
		Warnings: []Warning{},
	}
}

func errorEnvelope(command string, err error) (Envelope, int) {
	envelope := baseEnvelope(command)
	envelope.OK = false
	protocolError, ok := err.(*protocol.Error)
	if !ok {
		protocolError = protocol.WrapError("CHS_INTERNAL", protocol.CategoryUnsupported, "An internal invariant failed.", err)
	}
	var head, operationID *string
	if protocolError.CurrentHead != "" {
		value := protocolError.CurrentHead
		head = &value
	}
	if protocolError.OperationID != "" {
		value := protocolError.OperationID
		operationID = &value
	}
	details := protocolError.Details
	if details == nil {
		details = map[string]any{}
	}
	remediation := protocolError.Remediation
	if remediation == nil {
		remediation = []protocol.Remediation{}
	}
	envelope.Error = &ErrorBody{
		Category: protocolError.Category, Code: protocolError.Code,
		CurrentHead: head, Details: details, Message: protocolError.Message,
		OperationID: operationID, Remediation: remediation,
		Retryable: protocolError.Retryable,
	}
	return envelope, protocolError.ExitCode()
}

func emit(envelope Envelope, asJSON bool, stdout, stderr io.Writer) int {
	exitCode := 0
	if !envelope.OK {
		switch envelope.Error.Category {
		case protocol.CategoryUsage:
			exitCode = 2
		case protocol.CategoryLocal:
			exitCode = 3
		case protocol.CategoryTrust, protocol.CategoryProtocol:
			exitCode = 4
		case protocol.CategoryAuthorization:
			exitCode = 5
		case protocol.CategoryValidation, protocol.CategoryConflict:
			exitCode = 6
		case protocol.CategoryCheck, protocol.CategoryReview:
			exitCode = 7
		case protocol.CategoryNetwork:
			exitCode = 8
		default:
			exitCode = 9
		}
	}
	if asJSON {
		encoder := json.NewEncoder(stdout)
		encoder.SetEscapeHTML(false)
		if err := encoder.Encode(envelope); err != nil {
			fmt.Fprintln(stderr, "failed to encode CLI response:", err)
			return 9
		}
		return exitCode
	}
	if !envelope.OK {
		fmt.Fprintf(stderr, "%s: %s\n", envelope.Error.Code, envelope.Error.Message)
		for _, remediation := range envelope.Error.Remediation {
			fmt.Fprintf(stderr, "  %s: %v\n", remediation.Description, remediation.Argv)
		}
		return exitCode
	}
	data, err := json.MarshalIndent(envelope.Result, "", "  ")
	if err != nil {
		fmt.Fprintln(stderr, "failed to render CLI response:", err)
		return 9
	}
	fmt.Fprintln(stdout, string(data))
	for _, warning := range envelope.Warnings {
		fmt.Fprintf(stderr, "%s: %s\n", warning.Code, warning.Message)
	}
	return 0
}
