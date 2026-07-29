package authority

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/ExplodeCode6324/chassiss/internal/cryptoutil"
	"github.com/ExplodeCode6324/chassiss/internal/protocol"
	"github.com/ExplodeCode6324/chassiss/internal/state"
)

const RequestSchema = "chassiss.grant-request/v1"
const ProofNamespace = "chassiss-grant-request"

type GrantRequest struct {
	Actor                 string       `json:"actor"`
	KeyID                 string       `json:"key_id"`
	Nonce                 string       `json:"nonce"`
	ProjectID             string       `json:"project_id"`
	Proof                 Proof        `json:"proof"`
	PublicKey             string       `json:"public_key"`
	RequestedCapabilities []string     `json:"requested_capabilities"`
	RequestedLimits       state.Limits `json:"requested_limits"`
	RequestedScope        state.Scope  `json:"requested_scope"`
	Schema                string       `json:"schema"`
}

type Proof struct {
	Algorithm string `json:"algorithm"`
	Namespace string `json:"namespace"`
	Signature string `json:"signature"`
}

type RequestBody struct {
	Actor                 string       `json:"actor"`
	KeyID                 string       `json:"key_id"`
	Nonce                 string       `json:"nonce"`
	ProjectID             string       `json:"project_id"`
	PublicKey             string       `json:"public_key"`
	RequestedCapabilities []string     `json:"requested_capabilities"`
	RequestedLimits       state.Limits `json:"requested_limits"`
	RequestedScope        state.Scope  `json:"requested_scope"`
	Schema                string       `json:"schema"`
}

func NewRequest(projectID, actor, keyID, publicKey, handle string, capabilities []string, scope state.Scope, limits state.Limits) (*GrantRequest, error) {
	operationID, err := protocol.NewOperationID()
	if err != nil {
		return nil, err
	}
	request := &GrantRequest{
		Actor: actor, KeyID: keyID, Nonce: strings.TrimPrefix(operationID, "OPR-"),
		ProjectID: projectID, PublicKey: publicKey,
		RequestedCapabilities: sortedUnique(capabilities),
		RequestedLimits:       limits,
		RequestedScope: state.Scope{
			Resources: sortedUnique(scope.Resources), Tasks: sortedUnique(scope.Tasks),
		},
		Schema: RequestSchema,
		Proof:  Proof{Algorithm: "ssh-ed25519", Namespace: ProofNamespace},
	}
	if err := request.validateBody(); err != nil {
		return nil, err
	}
	digest, err := protocol.ObjectDigest("grant-request-body", request.Body())
	if err != nil {
		return nil, err
	}
	rawDigest, err := hex.DecodeString(strings.TrimPrefix(digest, "sha256:"))
	if err != nil {
		return nil, err
	}
	signature, err := cryptoutil.SignSSHSIG(handle, ProofNamespace, rawDigest)
	if err != nil {
		return nil, err
	}
	request.Proof.Signature = signature
	if err := request.Validate(); err != nil {
		return nil, err
	}
	return request, nil
}

func (request GrantRequest) Body() RequestBody {
	return RequestBody{
		Actor: request.Actor, KeyID: request.KeyID, Nonce: request.Nonce,
		ProjectID: request.ProjectID, PublicKey: request.PublicKey,
		RequestedCapabilities: request.RequestedCapabilities,
		RequestedLimits:       request.RequestedLimits, RequestedScope: request.RequestedScope,
		Schema: request.Schema,
	}
}

func (request GrantRequest) Validate() error {
	if err := request.validateBody(); err != nil {
		return err
	}
	if request.Proof.Algorithm != "ssh-ed25519" ||
		request.Proof.Namespace != ProofNamespace || request.Proof.Signature == "" {
		return fmt.Errorf("Grant Request proof metadata is invalid")
	}
	digest, err := protocol.ObjectDigest("grant-request-body", request.Body())
	if err != nil {
		return err
	}
	rawDigest, err := hex.DecodeString(strings.TrimPrefix(digest, "sha256:"))
	if err != nil {
		return err
	}
	return cryptoutil.VerifySSHSIG(request.PublicKey, ProofNamespace, request.Proof.Signature, rawDigest)
}

func (request GrantRequest) validateBody() error {
	if request.Schema != RequestSchema {
		return fmt.Errorf("Grant Request schema must be %s", RequestSchema)
	}
	if err := protocol.ValidateID(protocol.IDProject, request.ProjectID); err != nil {
		return err
	}
	if err := protocol.ValidateID(protocol.IDKey, request.KeyID); err != nil {
		return err
	}
	if err := protocol.ValidateActor(request.Actor); err != nil {
		return err
	}
	if err := protocol.ValidateOperationID("OPR-" + request.Nonce); err != nil {
		return fmt.Errorf("Grant Request nonce is invalid")
	}
	temporary := state.Grant{
		Actor: request.Actor, Capabilities: request.RequestedCapabilities,
		KeyID: request.KeyID, Limits: request.RequestedLimits,
		PublicKey: request.PublicKey, Scope: request.RequestedScope,
	}
	return temporary.Validate()
}

func (request GrantRequest) Digest() (string, error) {
	return protocol.ObjectDigest("grant-request", request)
}

func EncodeRequest(request GrantRequest) ([]byte, error) {
	if err := request.Validate(); err != nil {
		return nil, err
	}
	data, err := protocol.CanonicalJSON(request)
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

func DecodeRequest(data []byte) (*GrantRequest, error) {
	if _, err := protocol.ParseCanonicalInput(data); err != nil {
		return nil, err
	}
	var request GrantRequest
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		return nil, err
	}
	if err := request.Validate(); err != nil {
		return nil, err
	}
	return &request, nil
}

func DeveloperCapabilities() []string {
	return []string{
		"task.block", "task.cancel", "task.release",
		"task.resume", "task.start", "task.submit",
	}
}

func ApplyProfile(profile string, capabilities []string, limits state.Limits) ([]string, state.Limits, error) {
	switch profile {
	case "":
		return sortedUnique(capabilities), limits, nil
	case "developer":
		capabilities = append(capabilities, DeveloperCapabilities()...)
		if limits.Mode == "unbounded" {
			return nil, state.Limits{}, fmt.Errorf("developer profile expands to bounded max_active_tasks=1")
		}
		if limits.Mode == "" || (limits.Mode == "bounded" && limits.MaxActiveTasks == nil && limits.MaxChangedPaths == nil) {
			value := int64(1)
			limits = state.Limits{Mode: "bounded", MaxActiveTasks: &value}
		}
		return sortedUnique(capabilities), limits, nil
	default:
		return nil, state.Limits{}, fmt.Errorf("unknown Grant profile %q", profile)
	}
}

func sortedUnique(values []string) []string {
	result := append([]string(nil), values...)
	sort.Strings(result)
	write := 0
	for _, value := range result {
		if write == 0 || result[write-1] != value {
			result[write] = value
			write++
		}
	}
	return result[:write]
}
