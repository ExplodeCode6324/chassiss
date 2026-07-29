package state

import (
	"fmt"
	"sort"
	"strings"

	"github.com/ExplodeCode6324/chassiss/internal/cryptoutil"
	"github.com/ExplodeCode6324/chassiss/internal/protocol"
)

func (state *State) Validate(objectFormat string) error {
	if state.Schema != protocol.StateSchema || state.Protocol != protocol.ProtocolID {
		return fmt.Errorf("State schema/protocol must be %s/%s", protocol.StateSchema, protocol.ProtocolID)
	}
	if err := protocol.ValidateID(protocol.IDProject, state.Project.ID); err != nil {
		return err
	}
	if state.Project.Architecture.Path != "docs/architecture.yaml" {
		return fmt.Errorf("Architecture path must be docs/architecture.yaml")
	}
	if err := protocol.ValidateOID(state.Project.Architecture.BlobOID, objectFormat); err != nil {
		return fmt.Errorf("Architecture blob: %w", err)
	}
	if state.Project.Taskbook == nil {
		if len(state.Tasks) != 0 {
			return fmt.Errorf("State tasks must be empty when project.taskbook is null")
		}
	} else {
		if state.Project.Taskbook.Path != "docs/taskbook.yaml" {
			return fmt.Errorf("Taskbook path must be docs/taskbook.yaml")
		}
		if err := protocol.ValidateID(protocol.IDTaskbook, state.Project.Taskbook.ID); err != nil {
			return err
		}
		if err := protocol.ValidateOID(state.Project.Taskbook.BlobOID, objectFormat); err != nil {
			return fmt.Errorf("Taskbook blob: %w", err)
		}
	}
	if err := protocol.ValidateID(protocol.IDKey, state.Authority.Root.KeyID); err != nil {
		return err
	}
	if _, err := cryptoutil.ParseEd25519PublicKey(state.Authority.Root.PublicKey); err != nil {
		return fmt.Errorf("Root public key: %w", err)
	}
	if state.Authority.Grants == nil {
		return fmt.Errorf("authority.grants must be an object")
	}
	keyActors := map[string]string{
		state.Authority.Root.KeyID: "root",
	}
	fingerprintActors := make(map[string]string)
	rootFingerprint, _ := cryptoutil.Fingerprint(state.Authority.Root.PublicKey)
	fingerprintActors[rootFingerprint] = "root"
	for id, grant := range state.Authority.Grants {
		if err := protocol.ValidateID(protocol.IDGrant, id); err != nil {
			return err
		}
		if err := grant.Validate(); err != nil {
			return fmt.Errorf("%s: %w", id, err)
		}
		if actor, exists := keyActors[grant.KeyID]; exists && actor != grant.Actor {
			return fmt.Errorf("key ID %s maps to more than one actor", grant.KeyID)
		}
		keyActors[grant.KeyID] = grant.Actor
		fingerprint, _ := cryptoutil.Fingerprint(grant.PublicKey)
		if actor, exists := fingerprintActors[fingerprint]; exists && actor != grant.Actor {
			return fmt.Errorf("key fingerprint %s maps to more than one actor", fingerprint)
		}
		fingerprintActors[fingerprint] = grant.Actor
	}
	if state.Tasks == nil {
		return fmt.Errorf("tasks must be an object")
	}
	for id, task := range state.Tasks {
		if err := protocol.ValidateID(protocol.IDTask, id); err != nil {
			return err
		}
		if err := task.Validate(objectFormat); err != nil {
			return fmt.Errorf("%s: %w", id, err)
		}
	}
	if state.Audit != nil {
		if err := state.Audit.Validate(objectFormat); err != nil {
			return fmt.Errorf("audit: %w", err)
		}
	}
	return nil
}

func (audit AuditIndex) Validate(objectFormat string) error {
	if len(audit.Reviews) == 0 && len(audit.Failures) == 0 {
		return fmt.Errorf("empty audit index is prohibited")
	}
	operations := make(map[string]struct{}, len(audit.Reviews)+len(audit.Failures))
	for index, review := range audit.Reviews {
		if err := review.Validate(); err != nil {
			return fmt.Errorf("review %d: %w", index, err)
		}
		if _, exists := operations[review.OperationID]; exists {
			return fmt.Errorf("duplicate audit operation %s", review.OperationID)
		}
		operations[review.OperationID] = struct{}{}
	}
	for index, failure := range audit.Failures {
		if err := failure.Validate(objectFormat); err != nil {
			return fmt.Errorf("failure %d: %w", index, err)
		}
		if _, exists := operations[failure.OperationID]; exists {
			return fmt.Errorf("duplicate audit operation %s", failure.OperationID)
		}
		operations[failure.OperationID] = struct{}{}
	}
	return nil
}

func (review ReviewIndex) Validate() error {
	if err := protocol.ValidateID(protocol.IDTaskbook, review.Taskbook); err != nil {
		return err
	}
	if err := protocol.ValidateID(protocol.IDTask, review.Task); err != nil {
		return err
	}
	if err := protocol.ValidateOperationID(review.OperationID); err != nil {
		return err
	}
	if err := protocol.ValidateID(protocol.IDGrant, review.GrantID); err != nil {
		return err
	}
	if err := protocol.ValidateID(protocol.IDKey, review.KeyID); err != nil {
		return err
	}
	if err := protocol.ValidateActor(review.Reviewer); err != nil {
		return err
	}
	if review.Verdict != "approve" && review.Verdict != "request_changes" {
		return fmt.Errorf("review verdict is invalid")
	}
	for _, digest := range []string{review.AttemptDigest, review.ContextDigest, review.ReportDigest} {
		if err := protocol.ValidateDigest(digest); err != nil {
			return err
		}
	}
	if !strings.HasPrefix(review.KeyFingerprint, "SHA256:") {
		return fmt.Errorf("reviewer key fingerprint must use OpenSSH SHA256 format")
	}
	return nil
}

func (failure AttemptFailureIndex) Validate(objectFormat string) error {
	if err := protocol.ValidateID(protocol.IDTaskbook, failure.Taskbook); err != nil {
		return err
	}
	if err := protocol.ValidateID(protocol.IDTask, failure.Task); err != nil {
		return err
	}
	if err := protocol.ValidateOperationID(failure.OperationID); err != nil {
		return err
	}
	if err := protocol.ValidateActor(failure.Actor); err != nil {
		return err
	}
	if err := protocol.ValidateID(protocol.IDGrant, failure.AgentGrantID); err != nil {
		return err
	}
	if err := protocol.ValidateID(protocol.IDKey, failure.AgentKeyID); err != nil {
		return err
	}
	switch failure.Phase {
	case "active", "submitted", "approved":
	default:
		return fmt.Errorf("failure phase is not an active Agent phase")
	}
	if strings.TrimSpace(failure.Code) == "" || strings.TrimSpace(failure.Summary) == "" ||
		len(failure.Code) > 128 || len(failure.Summary) > 2048 {
		return fmt.Errorf("failure code or summary is invalid")
	}
	if err := protocol.ValidateDigest(failure.ChangedPathsDigest); err != nil {
		return err
	}
	for _, oid := range []string{failure.WorkHead, failure.WorkTree} {
		if err := protocol.ValidateOID(oid, objectFormat); err != nil {
			return err
		}
	}
	return nil
}

func (grant Grant) Validate() error {
	if err := protocol.ValidateActor(grant.Actor); err != nil {
		return err
	}
	if err := protocol.ValidateID(protocol.IDKey, grant.KeyID); err != nil {
		return err
	}
	if _, err := cryptoutil.ParseEd25519PublicKey(grant.PublicKey); err != nil {
		return err
	}
	if len(grant.Capabilities) == 0 || !sortedUnique(grant.Capabilities) {
		return fmt.Errorf("capabilities must be non-empty, sorted, and unique")
	}
	for _, capability := range grant.Capabilities {
		if err := protocol.ValidateCapability(capability); err != nil {
			return err
		}
	}
	if len(grant.Scope.Tasks) == 0 || !sortedUnique(grant.Scope.Tasks) {
		return fmt.Errorf("Task scope must be non-empty, sorted, and unique")
	}
	for _, pattern := range grant.Scope.Tasks {
		if err := validateTaskScope(pattern); err != nil {
			return err
		}
	}
	if len(grant.Scope.Resources) == 0 || !sortedUnique(grant.Scope.Resources) {
		return fmt.Errorf("Resource scope must be non-empty, sorted, and unique")
	}
	for _, pattern := range grant.Scope.Resources {
		if err := validateResourceScope(pattern); err != nil {
			return err
		}
	}
	switch grant.Limits.Mode {
	case "bounded":
		if grant.Limits.MaxActiveTasks == nil && grant.Limits.MaxChangedPaths == nil {
			return fmt.Errorf("bounded limits require at least one numeric limit")
		}
		for _, value := range []*int64{grant.Limits.MaxActiveTasks, grant.Limits.MaxChangedPaths} {
			if value != nil && (*value < 1 || *value > 2147483647) {
				return fmt.Errorf("bounded limits must be in [1,2147483647]")
			}
		}
	case "unbounded":
		if grant.Limits.MaxActiveTasks != nil || grant.Limits.MaxChangedPaths != nil {
			return fmt.Errorf("unbounded limits prohibit numeric limits")
		}
	default:
		return fmt.Errorf("limit mode must be bounded or unbounded")
	}
	return nil
}

func (task TaskState) Validate(objectFormat string) error {
	if task.Blocked != nil && !*task.Blocked {
		return fmt.Errorf("blocked:false is prohibited")
	}
	switch task.Phase {
	case "ready":
		if task.Actor != "" || task.Base != "" || task.Contract != nil || task.Attempt != nil || task.Review != nil {
			return fmt.Errorf("ready Task contains fields outside its sparse projection")
		}
	case "active":
		if err := task.validateActive(objectFormat); err != nil {
			return err
		}
		if task.Attempt != nil || task.Review != nil {
			return fmt.Errorf("active Task cannot contain attempt or review")
		}
	case "submitted":
		if err := task.validateActive(objectFormat); err != nil {
			return err
		}
		if task.Attempt == nil || task.Review != nil {
			return fmt.Errorf("submitted Task requires attempt and prohibits review")
		}
		if err := task.Attempt.Validate(objectFormat); err != nil {
			return err
		}
	case "approved":
		if err := task.validateActive(objectFormat); err != nil {
			return err
		}
		if task.Attempt == nil || task.Review == nil {
			return fmt.Errorf("approved Task requires attempt and review")
		}
		if err := task.Attempt.Validate(objectFormat); err != nil {
			return err
		}
		if err := task.Review.Validate(objectFormat); err != nil {
			return err
		}
	case "closed", "cancelled", "superseded":
		if task.Blocked != nil || task.Actor != "" || task.Base != "" || task.Contract != nil ||
			task.Attempt != nil || task.Review != nil {
			return fmt.Errorf("terminal Task must contain only phase")
		}
	default:
		return fmt.Errorf("unknown Task phase %q", task.Phase)
	}
	return nil
}

func (task TaskState) validateActive(objectFormat string) error {
	if task.Actor == "" || task.Base == "" || task.Contract == nil {
		return fmt.Errorf("active projection requires actor, base, and contract")
	}
	if err := protocol.ValidateActor(task.Actor); err != nil {
		return err
	}
	if err := protocol.ValidateOID(task.Base, objectFormat); err != nil {
		return fmt.Errorf("Task base: %w", err)
	}
	if err := protocol.ValidateOID(task.Contract.ArchitectureBlob, objectFormat); err != nil {
		return fmt.Errorf("frozen Architecture blob: %w", err)
	}
	if err := protocol.ValidateOID(task.Contract.TaskbookBlob, objectFormat); err != nil {
		return fmt.Errorf("frozen Taskbook blob: %w", err)
	}
	return nil
}

func (attempt Attempt) Validate(objectFormat string) error {
	if err := protocol.ValidateOID(attempt.Head, objectFormat); err != nil {
		return err
	}
	if err := protocol.ValidateDigest(attempt.EvidenceDigest); err != nil {
		return err
	}
	if !strings.HasPrefix(attempt.SubmitterKeyFingerprint, "SHA256:") {
		return fmt.Errorf("submitter key fingerprint must use OpenSSH SHA256 format")
	}
	return nil
}

func (review Review) Validate(objectFormat string) error {
	for label, oid := range map[string]string{
		"candidate tree": review.CandidateTree,
		"review main":    review.ReviewMain,
	} {
		if err := protocol.ValidateOID(oid, objectFormat); err != nil {
			return fmt.Errorf("%s: %w", label, err)
		}
	}
	for _, digest := range []string{review.ContextDigest, review.ReportDigest} {
		if err := protocol.ValidateDigest(digest); err != nil {
			return err
		}
	}
	if err := protocol.ValidateActor(review.Reviewer); err != nil {
		return err
	}
	if err := protocol.ValidateID(protocol.IDKey, review.KeyID); err != nil {
		return err
	}
	if !strings.HasPrefix(review.KeyFingerprint, "SHA256:") {
		return fmt.Errorf("reviewer key fingerprint must use OpenSSH SHA256 format")
	}
	return nil
}

func sortedUnique(values []string) bool {
	return sort.StringsAreSorted(values) && !hasDuplicate(values)
}

func hasDuplicate(values []string) bool {
	for index := 1; index < len(values); index++ {
		if values[index] == values[index-1] {
			return true
		}
	}
	return false
}

func validateTaskScope(pattern string) error {
	if pattern == "*" {
		return nil
	}
	if strings.Count(pattern, "*") > 1 || (strings.Contains(pattern, "*") && !strings.HasSuffix(pattern, "*")) {
		return fmt.Errorf("invalid Task scope %q", pattern)
	}
	if strings.HasSuffix(pattern, "*") {
		prefix := strings.TrimSuffix(pattern, "*")
		if prefix == "" || !strings.HasPrefix(prefix, "TASK-") {
			return fmt.Errorf("invalid Task prefix scope %q", pattern)
		}
		return nil
	}
	return protocol.ValidateID(protocol.IDTask, pattern)
}

func validateResourceScope(pattern string) error {
	if pattern == "*" {
		return nil
	}
	if strings.Count(pattern, ":") != 1 {
		return fmt.Errorf("invalid Resource scope %q", pattern)
	}
	kind, key, _ := strings.Cut(pattern, ":")
	switch kind {
	case "module", "api", "schema", "dependency", "config":
	default:
		return fmt.Errorf("invalid Resource type %q", kind)
	}
	if key == "*" {
		return nil
	}
	if strings.Contains(key, "*") || key == "" {
		return fmt.Errorf("invalid Resource scope %q", pattern)
	}
	for _, r := range key {
		if !((r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-') {
			return fmt.Errorf("invalid Resource key in scope %q", pattern)
		}
	}
	return nil
}
