package localstate

import (
	"bytes"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/ExplodeCode6324/chassiss/internal/protocol"
)

const Schema = "chassiss.local/v1"

type State struct {
	Projects map[string]Project `json:"projects"`
	Schema   string             `json:"schema"`
}

type Project struct {
	GenesisCommit     string                      `json:"genesis_commit"`
	Identity          Identity                    `json:"identity"`
	MinimumCheckpoint Checkpoint                  `json:"minimum_checkpoint"`
	PendingOperations map[string]PendingOperation `json:"pending_operations"`
	Remote            Remote                      `json:"remote"`
	RepoInstances     []RepoInstance              `json:"repo_instances"`
	RootFingerprint   string                      `json:"root_fingerprint"`
	Worktrees         map[string]Worktree         `json:"worktrees"`
}

type Identity struct {
	Keys          map[string]Key `json:"keys"`
	SelectedKeyID string         `json:"selected_key_id"`
}

type Key struct {
	PrivateKeyHandle string `json:"private_key_handle"`
}

type Checkpoint struct {
	Commit      string `json:"commit"`
	StateDigest string `json:"state_digest"`
}

type Remote struct {
	URL            string `json:"url"`
	URLFingerprint string `json:"url_fingerprint"`
}

type RepoInstance struct {
	CreatedByCLI     bool   `json:"created_by_cli"`
	GitDir           string `json:"git_dir"`
	LastVerifiedHead string `json:"last_verified_head"`
	RepoPath         string `json:"repo_path"`
}

type Worktree struct {
	Actor         string `json:"actor"`
	Base          string `json:"base"`
	Branch        string `json:"branch"`
	Dirty         bool   `json:"dirty"`
	Head          string `json:"head"`
	Path          string `json:"path"`
	PublishedHead string `json:"published_head,omitempty"`
	TaskID        string `json:"task_id"`
	Orphan        bool   `json:"orphan,omitempty"`
}

type PendingOperation struct {
	AuthorityKeyHandle string            `json:"authority_key_handle"`
	CandidateCommit    *string           `json:"candidate_commit"`
	CandidateEvidence  json.RawMessage   `json:"candidate_evidence"`
	EvidenceDigest     string            `json:"evidence_digest"`
	ExpectedMain       string            `json:"expected_main"`
	OperationDigest    string            `json:"operation_digest"`
	OperationID        string            `json:"operation_id"`
	SemanticOperation  json.RawMessage   `json:"semantic_operation"`
	Status             string            `json:"status"`
	TargetRefs         map[string]string `json:"target_refs"`
}

func New() *State {
	return &State{Schema: Schema, Projects: map[string]Project{}}
}

func (state *State) Validate() error {
	if state.Schema != Schema {
		return protocol.NewError(protocol.ErrLocalStateCorrupt, protocol.CategoryLocal, "Local State schema is not chassiss.local/v1.")
	}
	if state.Projects == nil {
		return protocol.NewError(protocol.ErrLocalStateCorrupt, protocol.CategoryLocal, "Local State projects must be an object.")
	}
	for projectID, project := range state.Projects {
		if err := protocol.ValidateID(protocol.IDProject, projectID); err != nil {
			return protocol.WrapError(protocol.ErrLocalStateCorrupt, protocol.CategoryLocal, "Local State contains an invalid Project ID.", err)
		}
		objectFormat, err := objectFormatForOID(project.GenesisCommit)
		if err != nil {
			return corrupt("Local State Genesis commit is invalid.", err)
		}
		if err := protocol.ValidateOID(project.MinimumCheckpoint.Commit, objectFormat); err != nil {
			return corrupt("Local State minimum checkpoint commit is invalid.", err)
		}
		if err := protocol.ValidateDigest(project.MinimumCheckpoint.StateDigest); err != nil {
			return corrupt("Local State minimum checkpoint digest is invalid.", err)
		}
		if !strings.HasPrefix(project.RootFingerprint, "SHA256:") ||
			len(strings.TrimPrefix(project.RootFingerprint, "SHA256:")) == 0 {
			return corrupt("Local State Root fingerprint is invalid.", nil)
		}
		if project.Identity.Keys == nil || project.PendingOperations == nil || project.Worktrees == nil {
			return protocol.NewError(protocol.ErrLocalStateCorrupt, protocol.CategoryLocal, "Local State Project mappings must be objects.")
		}
		for keyID, key := range project.Identity.Keys {
			if err := protocol.ValidateID(protocol.IDKey, keyID); err != nil {
				return corrupt("Local State contains an invalid Key ID.", err)
			}
			if !strings.HasPrefix(key.PrivateKeyHandle, "file:") ||
				strings.TrimPrefix(key.PrivateKeyHandle, "file:") == "" {
				return corrupt("Local State contains an unsupported or empty private-key handle.", nil)
			}
		}
		if project.Identity.SelectedKeyID != "" {
			if _, exists := project.Identity.Keys[project.Identity.SelectedKeyID]; !exists {
				return corrupt("Local State selected Key ID is not present in identity.keys.", nil)
			}
		}
		if project.Remote.URL == "" {
			if project.Remote.URLFingerprint != "" {
				return corrupt("Local State remote fingerprint must be empty when remote URL is empty.", nil)
			}
		} else if project.Remote.URLFingerprint != protocol.DigestBytes([]byte(project.Remote.URL)) {
			return corrupt("Local State remote URL fingerprint does not match its URL.", nil)
		}
		repositories := make(map[string]struct{}, len(project.RepoInstances))
		for _, instance := range project.RepoInstances {
			if !filepath.IsAbs(instance.RepoPath) || !filepath.IsAbs(instance.GitDir) {
				return corrupt("Local State repository paths must be absolute.", nil)
			}
			if err := protocol.ValidateOID(instance.LastVerifiedHead, objectFormat); err != nil {
				return corrupt("Local State repository checkpoint is invalid.", err)
			}
			clean := filepath.Clean(instance.RepoPath)
			if _, exists := repositories[clean]; exists {
				return corrupt("Local State contains a duplicate repository instance.", nil)
			}
			repositories[clean] = struct{}{}
		}
		for operationID, pending := range project.PendingOperations {
			if err := protocol.ValidateOperationID(operationID); err != nil || operationID != pending.OperationID {
				return protocol.NewError(protocol.ErrLocalStateCorrupt, protocol.CategoryLocal, "Pending Operation ID is invalid or mismatched.")
			}
			switch pending.Status {
			case "prepared", "signed", "push-unknown", "published", "reconciled", "failed":
			default:
				return protocol.NewError(protocol.ErrLocalStateCorrupt, protocol.CategoryLocal, "Pending Operation status is invalid.")
			}
			if err := validatePending(pending, projectID, objectFormat); err != nil {
				return err
			}
		}
		for taskID, worktree := range project.Worktrees {
			if err := protocol.ValidateID(protocol.IDTask, taskID); err != nil || taskID != worktree.TaskID {
				return corrupt("Local State Worktree Task ID is invalid or mismatched.", err)
			}
			if err := protocol.ValidateActor(worktree.Actor); err != nil {
				return corrupt("Local State Worktree actor is invalid.", err)
			}
			if !filepath.IsAbs(worktree.Path) || !strings.HasPrefix(worktree.Branch, "refs/heads/chassiss/work/") {
				return corrupt("Local State Worktree path or branch is invalid.", nil)
			}
			for label, oid := range map[string]string{
				"base": worktree.Base, "head": worktree.Head,
			} {
				if err := protocol.ValidateOID(oid, objectFormat); err != nil {
					return corrupt("Local State Worktree "+label+" is invalid.", err)
				}
			}
			if worktree.PublishedHead != "" {
				if err := protocol.ValidateOID(worktree.PublishedHead, objectFormat); err != nil {
					return corrupt("Local State Worktree published head is invalid.", err)
				}
			}
		}
	}
	return nil
}

func objectFormatForOID(value string) (string, error) {
	switch len(value) {
	case 40:
		if err := protocol.ValidateOID(value, "sha1"); err != nil {
			return "", err
		}
		return "sha1", nil
	case 64:
		if err := protocol.ValidateOID(value, "sha256"); err != nil {
			return "", err
		}
		return "sha256", nil
	default:
		return "", fmt.Errorf("Git OID must contain 40 or 64 hexadecimal characters")
	}
}

func validatePending(pending PendingOperation, projectID, objectFormat string) error {
	if strings.TrimSpace(pending.AuthorityKeyHandle) == "" {
		return corrupt("Pending Operation has no authority key handle.", nil)
	}
	if err := protocol.ValidateOID(pending.ExpectedMain, objectFormat); err != nil {
		return corrupt("Pending Operation expected main is invalid.", err)
	}
	if err := protocol.ValidateDigest(pending.OperationDigest); err != nil {
		return corrupt("Pending Operation digest is invalid.", err)
	}
	if err := protocol.ValidateDigest(pending.EvidenceDigest); err != nil {
		return corrupt("Pending Evidence digest is invalid.", err)
	}
	var operation protocol.Operation
	if err := decodeCanonical(pending.SemanticOperation, &operation); err != nil {
		return corrupt("Pending Semantic Operation is invalid.", err)
	}
	if err := operation.Validate(); err != nil {
		return corrupt("Pending Semantic Operation is invalid.", err)
	}
	if operation.Project != projectID || operation.OperationID != pending.OperationID {
		return corrupt("Pending Semantic Operation identity is mismatched.", nil)
	}
	operationDigest, err := protocol.ObjectDigest("operation", operation)
	if err != nil || operationDigest != pending.OperationDigest {
		return corrupt("Pending Semantic Operation digest is mismatched.", err)
	}
	var evidence protocol.ExecutionEvidence
	if err := decodeCanonical(pending.CandidateEvidence, &evidence); err != nil {
		return corrupt("Pending Execution Evidence is invalid.", err)
	}
	if evidence.Schema != protocol.EvidenceSchema || evidence.Action != operation.Action ||
		evidence.OperationDigest != pending.OperationDigest || evidence.Attempt < 1 || evidence.Attempt > 3 {
		return corrupt("Pending Execution Evidence binding is invalid.", nil)
	}
	evidenceDigest, err := protocol.ObjectDigest("execution-evidence", evidence)
	if err != nil || evidenceDigest != pending.EvidenceDigest {
		return corrupt("Pending Execution Evidence digest is mismatched.", err)
	}
	if pending.CandidateCommit != nil {
		if err := protocol.ValidateOID(*pending.CandidateCommit, objectFormat); err != nil {
			return corrupt("Pending candidate commit is invalid.", err)
		}
	} else if pending.Status != "prepared" {
		return corrupt("Pending signed Operation has no candidate commit.", nil)
	}
	if len(pending.TargetRefs) == 0 {
		return corrupt("Pending Operation target refs must not be empty.", nil)
	}
	for ref, oid := range pending.TargetRefs {
		if !strings.HasPrefix(ref, "refs/") || strings.ContainsAny(ref, " \t\r\n") {
			return corrupt("Pending Operation target ref is invalid.", nil)
		}
		if oid != "" {
			if err := protocol.ValidateOID(oid, objectFormat); err != nil {
				return corrupt("Pending Operation target OID is invalid.", err)
			}
		}
	}
	return nil
}

func decodeCanonical(data []byte, target any) error {
	value, err := protocol.ParseCanonicalInput(data)
	if err != nil {
		return err
	}
	canonical, err := protocol.CanonicalJSON(value)
	if err != nil {
		return err
	}
	if !bytes.Equal(canonical, data) {
		index := 0
		for index < len(canonical) && index < len(data) && canonical[index] == data[index] {
			index++
		}
		return fmt.Errorf(
			"JSON is not canonical (length %d, canonical length %d, first mismatch %d)",
			len(data), len(canonical), index,
		)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	return decoder.Decode(target)
}

// canonicalizeLegacyPendingHTML recovers only the exact representation emitted
// when encoding/json re-encoded an otherwise canonical RawMessage with its
// historical default HTML escaping. State.Validate subsequently rechecks the
// typed Operation/Evidence bindings and both recorded digests before the State
// can be returned or persisted.
func canonicalizeLegacyPendingHTML(state *State) bool {
	recovered := false
	for projectID, project := range state.Projects {
		for operationID, pending := range project.PendingOperations {
			semantic, semanticRecovered := canonicalizeLegacyHTMLRaw(pending.SemanticOperation)
			evidence, evidenceRecovered := canonicalizeLegacyHTMLRaw(pending.CandidateEvidence)
			if semanticRecovered {
				pending.SemanticOperation = semantic
			}
			if evidenceRecovered {
				pending.CandidateEvidence = evidence
			}
			if semanticRecovered || evidenceRecovered {
				project.PendingOperations[operationID] = pending
				recovered = true
			}
		}
		state.Projects[projectID] = project
	}
	return recovered
}

func canonicalizeLegacyHTMLRaw(data json.RawMessage) (json.RawMessage, bool) {
	value, err := protocol.ParseCanonicalInput(data)
	if err != nil {
		return data, false
	}
	canonical, err := protocol.CanonicalJSON(value)
	if err != nil || bytes.Equal(canonical, data) {
		return data, false
	}
	legacy, err := json.Marshal(json.RawMessage(canonical))
	if err != nil || !bytes.Equal(legacy, data) {
		return data, false
	}
	return json.RawMessage(append([]byte(nil), canonical...)), true
}

func corrupt(message string, cause error) *protocol.Error {
	if cause == nil {
		return protocol.NewError(protocol.ErrLocalStateCorrupt, protocol.CategoryLocal, message)
	}
	result := protocol.WrapError(protocol.ErrLocalStateCorrupt, protocol.CategoryLocal, message, cause)
	result.Details["reason"] = cause.Error()
	return result
}
