package state

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/ExplodeCode6324/chassiss/internal/cryptoutil"
	"github.com/ExplodeCode6324/chassiss/internal/protocol"
)

// ReduceFacts are verified Git/contract facts supplied to the deterministic
// reducer. They are not accepted from a CLI caller without independent
// verification by the workflow layer.
type ReduceFacts struct {
	ObjectFormat             string
	ParentCommit             string
	SignerFingerprint        string
	TaskResources            []string
	TargetTasks              []string
	TargetResources          []string
	RequireGlobalScope       bool
	ActiveTasksForActor      int64
	ChangedPaths             int64
	EffectiveMaxChangedPaths *int64
	GenesisTaskbookID        string
	GenesisReadyTasks        []string
}

func Reduce(parent *State, operation protocol.Operation, evidence protocol.ExecutionEvidence, facts ReduceFacts) (*State, error) {
	if facts.ObjectFormat == "" {
		facts.ObjectFormat = "sha1"
	}
	if err := operation.Validate(); err != nil {
		return nil, fmt.Errorf("%s: %w", protocol.ErrOperationInvalid, err)
	}
	if err := evidence.Validate(operation, facts.ObjectFormat); err != nil {
		return nil, fmt.Errorf("%s: %w", protocol.ErrEvidenceInvalid, err)
	}
	if operation.Action == "project.genesis" || operation.Action == "project.bootstrap" {
		if parent != nil {
			return nil, fmt.Errorf("%s: Project bootstrap cannot have a parent State", protocol.ErrGenesisInvalid)
		}
		if operation.Action == "project.bootstrap" {
			return reduceBootstrap(operation, evidence, facts)
		}
		return reduceGenesis(operation, evidence, facts)
	}
	if parent == nil {
		return nil, fmt.Errorf("%s: non-Genesis action requires a parent State", protocol.ErrOperationInvalid)
	}
	if err := parent.Validate(facts.ObjectFormat); err != nil {
		return nil, fmt.Errorf("invalid parent State: %w", err)
	}
	if operation.Project != parent.Project.ID {
		return nil, fmt.Errorf("%s: operation Project does not match parent State", protocol.ErrProjectIDMismatch)
	}
	if evidence.Parent == nil || *evidence.Parent != facts.ParentCommit {
		return nil, fmt.Errorf("%s: Evidence parent does not match verified parent commit", protocol.ErrEvidenceInvalid)
	}
	principal, err := authorize(parent, operation, facts)
	if err != nil {
		return nil, err
	}
	if parent.Project.Architecture == nil &&
		operation.Action != "authority.grant-added" &&
		operation.Action != "authority.grant-revoked" &&
		operation.Action != "architecture.established" {
		return nil, fmt.Errorf("%s: bootstrap permits only authority changes and Architecture establish", protocol.ErrOperationInvalid)
	}
	next, err := cloneState(parent)
	if err != nil {
		return nil, err
	}

	switch operation.Action {
	case "architecture.established":
		err = reduceArchitectureEstablished(next, operation, evidence)
	case "architecture.updated":
		err = reduceArchitectureUpdated(next, operation, evidence)
	case "taskbook.opened":
		err = reduceTaskbookOpened(next, operation, evidence)
	case "taskbook.updated":
		err = reduceTaskbookUpdated(next, operation, evidence)
	case "taskbook.archived":
		err = reduceTaskbookArchived(next, operation)
	case "task.started":
		err = reduceTaskStarted(next, operation, evidence, principal)
	case "task.released":
		err = reduceTaskReleased(next, operation, evidence, principal)
	case "attempt.abandoned":
		err = reduceAttemptAbandoned(next, operation, evidence, facts.ObjectFormat)
	case "task.blocked":
		err = reduceTaskBlocked(next, operation)
	case "task.resumed":
		err = reduceTaskResumed(next, operation)
	case "task.submitted":
		err = reduceTaskSubmitted(next, operation, evidence, principal, facts)
	case "task.reviewed":
		err = reduceTaskReviewed(next, operation, evidence, principal, facts.ObjectFormat, false)
	case "task.reviewed-indexed":
		err = reduceTaskReviewed(next, operation, evidence, principal, facts.ObjectFormat, true)
	case "task.cancelled":
		err = reduceTaskTerminal(next, operation, evidence, "cancelled", facts.ObjectFormat)
	case "task.superseded":
		err = reduceTaskTerminal(next, operation, evidence, "superseded", facts.ObjectFormat)
	case "integration.applied":
		err = reduceIntegration(next, operation, facts.ObjectFormat)
	case "authority.grant-added":
		err = reduceGrantAdded(next, operation)
	case "authority.grant-revoked":
		err = reduceGrantRevoked(next, operation)
	case "owner.applied":
		err = reduceOwnerApplied(next, operation)
	default:
		err = fmt.Errorf("unsupported reducer action %s", operation.Action)
	}
	if err != nil {
		return nil, err
	}
	if err := next.Validate(facts.ObjectFormat); err != nil {
		return nil, fmt.Errorf("%s: next State invariant failed: %w", protocol.ErrReducerMismatch, err)
	}
	return next, nil
}

type principal struct {
	Root        bool
	GrantID     string
	KeyID       string
	Actor       string
	PublicKey   string
	Fingerprint string
	Grant       *Grant
}

func authorize(parent *State, operation protocol.Operation, facts ReduceFacts) (principal, error) {
	spec, _ := protocol.Action(operation.Action)
	if strings.HasPrefix(operation.Authority, "root:") {
		keyID := strings.TrimPrefix(operation.Authority, "root:")
		if keyID != parent.Authority.Root.KeyID {
			return principal{}, fmt.Errorf("%s: selected Root key is not current", protocol.ErrKeyMismatch)
		}
		if !spec.RootOnly && operation.Action != "task.superseded" {
			return principal{}, fmt.Errorf("%s: Root must use a Grant for this action", protocol.ErrCapabilityDenied)
		}
		fingerprint, _ := cryptoutil.Fingerprint(parent.Authority.Root.PublicKey)
		if facts.SignerFingerprint != "" && facts.SignerFingerprint != fingerprint {
			return principal{}, fmt.Errorf("%s: signer does not match selected Root", protocol.ErrKeyMismatch)
		}
		return principal{
			Root: true, KeyID: keyID, PublicKey: parent.Authority.Root.PublicKey,
			Fingerprint: fingerprint,
		}, nil
	}
	if spec.RootOnly {
		return principal{}, fmt.Errorf("%s: action requires Root authority", protocol.ErrCapabilityDenied)
	}
	grantID := strings.TrimPrefix(operation.Authority, "grant:")
	grant, exists := parent.Authority.Grants[grantID]
	if !exists {
		return principal{}, fmt.Errorf("%s: selected Grant is not current", protocol.ErrGrantNotFound)
	}
	if !contains(grant.Capabilities, spec.Capability) {
		return principal{}, fmt.Errorf("%s: Grant lacks %s", protocol.ErrCapabilityDenied, spec.Capability)
	}
	fingerprint, _ := cryptoutil.Fingerprint(grant.PublicKey)
	if facts.SignerFingerprint != "" && facts.SignerFingerprint != fingerprint {
		return principal{}, fmt.Errorf("%s: signer does not match selected Grant", protocol.ErrKeyMismatch)
	}
	targetTasks := facts.TargetTasks
	if spec.TaskAction {
		targetTasks = append([]string{operation.Target}, targetTasks...)
	}
	for _, task := range uniqueSorted(targetTasks) {
		if !matchesAnyTaskScope(task, grant.Scope.Tasks) {
			return principal{}, fmt.Errorf("%s: %s is outside Grant Task scope", protocol.ErrTaskScopeDenied, task)
		}
	}
	resources := append(append([]string(nil), facts.TaskResources...), facts.TargetResources...)
	for _, resource := range uniqueSorted(resources) {
		if !matchesAnyResourceScope(resource, grant.Scope.Resources) {
			return principal{}, fmt.Errorf("%s: %s is outside Grant Resource scope", protocol.ErrResourceScopeDenied, resource)
		}
	}
	if facts.RequireGlobalScope &&
		(!contains(grant.Scope.Tasks, "*") || !contains(grant.Scope.Resources, "*")) {
		return principal{}, fmt.Errorf("%s: action requires global Task and Resource scope", protocol.ErrTaskScopeDenied)
	}
	if operation.Action == "task.started" && grant.Limits.MaxActiveTasks != nil &&
		facts.ActiveTasksForActor+1 > *grant.Limits.MaxActiveTasks {
		return principal{}, fmt.Errorf("%s: max_active_tasks exceeded", protocol.ErrLimitExceeded)
	}
	if operation.Action == "task.submitted" {
		effective := grant.Limits.MaxChangedPaths
		if facts.EffectiveMaxChangedPaths != nil &&
			(effective == nil || *facts.EffectiveMaxChangedPaths < *effective) {
			effective = facts.EffectiveMaxChangedPaths
		}
		if effective != nil && facts.ChangedPaths > *effective {
			return principal{}, fmt.Errorf("%s: max_changed_paths exceeded", protocol.ErrLimitExceeded)
		}
	}
	return principal{
		GrantID: grantID, KeyID: grant.KeyID, Actor: grant.Actor,
		PublicKey: grant.PublicKey, Fingerprint: fingerprint, Grant: &grant,
	}, nil
}

func reduceGenesis(operation protocol.Operation, evidence protocol.ExecutionEvidence, facts ReduceFacts) (*State, error) {
	if operation.Authority != "root:"+stringValue(operation.Payload, "root_key_id") {
		return nil, fmt.Errorf("%s: Genesis authority must select its declared Root", protocol.ErrGenesisInvalid)
	}
	projectID := stringValue(operation.Payload, "project_id")
	if projectID != operation.Project || operation.Target != projectID {
		return nil, fmt.Errorf("%s: Genesis Project identifiers disagree", protocol.ErrGenesisInvalid)
	}
	if err := protocol.ValidateID(protocol.IDTaskbook, facts.GenesisTaskbookID); err != nil {
		return nil, fmt.Errorf("%s: %w", protocol.ErrGenesisInvalid, err)
	}
	architectureBlob := stringValue(operation.Payload, "architecture_blob")
	taskbookBlob := stringValue(operation.Payload, "taskbook_blob")
	if stringValue(evidence.Facts, "architecture_blob") != architectureBlob ||
		stringValue(evidence.Facts, "taskbook_blob") != taskbookBlob {
		return nil, fmt.Errorf("%s: Genesis blob evidence mismatch", protocol.ErrGenesisInvalid)
	}
	state := &State{
		Schema:   protocol.StateSchema,
		Protocol: protocol.ProtocolID,
		Project: Project{
			ID: projectID,
			Architecture: &BlobRef{
				Path: "docs/architecture.yaml", BlobOID: architectureBlob,
			},
			Taskbook: &TaskbookRef{
				Path: "docs/taskbook.yaml", BlobOID: taskbookBlob, ID: facts.GenesisTaskbookID,
			},
		},
		Authority: Authority{
			Root: Root{
				KeyID:     stringValue(operation.Payload, "root_key_id"),
				PublicKey: stringValue(operation.Payload, "root_public_key"),
			},
			Grants: map[string]Grant{},
		},
		Tasks: map[string]TaskState{},
	}
	for _, id := range uniqueSorted(facts.GenesisReadyTasks) {
		if err := protocol.ValidateID(protocol.IDTask, id); err != nil {
			return nil, err
		}
		state.Tasks[id] = TaskState{Phase: "ready"}
	}
	if err := state.Validate(facts.ObjectFormat); err != nil {
		return nil, fmt.Errorf("%s: %w", protocol.ErrGenesisInvalid, err)
	}
	return state, nil
}

func reduceBootstrap(operation protocol.Operation, evidence protocol.ExecutionEvidence, facts ReduceFacts) (*State, error) {
	if operation.Authority != "root:"+stringValue(operation.Payload, "root_key_id") {
		return nil, fmt.Errorf("%s: bootstrap authority must select its declared Root", protocol.ErrGenesisInvalid)
	}
	projectID := stringValue(operation.Payload, "project_id")
	if projectID != operation.Project || operation.Target != projectID {
		return nil, fmt.Errorf("%s: bootstrap Project identifiers disagree", protocol.ErrGenesisInvalid)
	}
	source := SourceRef{
		Commit:       stringValue(operation.Payload, "source_commit"),
		HistoryBlob:  stringValue(operation.Payload, "source_history_blob"),
		HistoryPath:  "docs/chassiss/onboarding/source-history.md",
		ObjectFormat: stringValue(operation.Payload, "source_object_format"),
		Tree:         stringValue(operation.Payload, "source_tree"),
	}
	if stringValue(evidence.Facts, "source_commit") != source.Commit ||
		stringValue(evidence.Facts, "source_history_blob") != source.HistoryBlob ||
		stringValue(evidence.Facts, "source_tree") != source.Tree {
		return nil, fmt.Errorf("%s: bootstrap source evidence mismatch", protocol.ErrGenesisInvalid)
	}
	next := &State{
		Schema: protocol.StateSchema, Protocol: protocol.ProtocolID,
		Project: Project{Architecture: nil, ID: projectID, Source: &source, Taskbook: nil},
		Authority: Authority{
			Root: Root{
				KeyID:     stringValue(operation.Payload, "root_key_id"),
				PublicKey: stringValue(operation.Payload, "root_public_key"),
			},
			Grants: map[string]Grant{},
		},
		Tasks: map[string]TaskState{},
	}
	if err := next.Validate(facts.ObjectFormat); err != nil {
		return nil, fmt.Errorf("%s: %w", protocol.ErrGenesisInvalid, err)
	}
	return next, nil
}

func reduceArchitectureEstablished(next *State, operation protocol.Operation, evidence protocol.ExecutionEvidence) error {
	if next.Project.Architecture != nil {
		return fmt.Errorf("%s: Architecture is already established", protocol.ErrArchitectureInvalid)
	}
	if next.Project.Source == nil || next.Project.Taskbook != nil || len(next.Tasks) != 0 ||
		operation.Preconditions["architecture"] != nil || operation.Preconditions["taskbook"] != nil {
		return fmt.Errorf("%s: Architecture establish requires an empty source bootstrap", protocol.ErrArchitectureInvalid)
	}
	newBlob := stringValue(evidence.Facts, "new_blob")
	if newBlob == "" || newBlob != stringValue(operation.Payload, "candidate_blob") {
		return fmt.Errorf("%s: candidate Architecture blob mismatch", protocol.ErrArchitectureInvalid)
	}
	if strings.TrimSpace(stringValue(operation.Payload, "reason")) == "" {
		return fmt.Errorf("%s: Architecture establish reason is required", protocol.ErrOperationInvalid)
	}
	next.Project.Architecture = &BlobRef{Path: "docs/architecture.yaml", BlobOID: newBlob}
	return nil
}

func reduceArchitectureUpdated(next *State, operation protocol.Operation, evidence protocol.ExecutionEvidence) error {
	if next.Project.Architecture == nil {
		return fmt.Errorf("%s: Architecture is not established", protocol.ErrArchitectureInvalid)
	}
	if next.Project.Taskbook != nil || operation.Preconditions["taskbook"] != nil {
		return fmt.Errorf("%s: Architecture update requires no active Taskbook", protocol.ErrTaskbookAlreadyActive)
	}
	if stringValue(operation.Preconditions, "architecture_blob") != next.Project.Architecture.BlobOID ||
		stringValue(evidence.Facts, "old_blob") != next.Project.Architecture.BlobOID {
		return fmt.Errorf("%s: Architecture blob precondition is stale", protocol.ErrArchitectureStale)
	}
	newBlob := stringValue(evidence.Facts, "new_blob")
	if newBlob == "" || newBlob != stringValue(operation.Payload, "candidate_blob") {
		return fmt.Errorf("%s: candidate Architecture blob mismatch", protocol.ErrArchitectureInvalid)
	}
	if strings.TrimSpace(stringValue(operation.Payload, "reason")) == "" {
		return fmt.Errorf("%s: Architecture update reason is required", protocol.ErrOperationInvalid)
	}
	next.Project.Architecture.BlobOID = newBlob
	return nil
}

func reduceTaskbookOpened(next *State, operation protocol.Operation, evidence protocol.ExecutionEvidence) error {
	if next.Project.Taskbook != nil || operation.Preconditions["taskbook"] != nil {
		return fmt.Errorf("%s: Taskbook is already active", protocol.ErrTaskbookAlreadyActive)
	}
	if stringValue(operation.Preconditions, "architecture_blob") != next.Project.Architecture.BlobOID ||
		stringValue(evidence.Facts, "architecture_blob") != next.Project.Architecture.BlobOID {
		return fmt.Errorf("%s: Architecture blob precondition is stale", protocol.ErrArchitectureStale)
	}
	blob := stringValue(evidence.Facts, "taskbook_blob")
	if blob == "" || blob != stringValue(operation.Payload, "candidate_blob") {
		return fmt.Errorf("%s: Taskbook candidate blob mismatch", protocol.ErrTaskbookInvalid)
	}
	id := stringValue(operation.Payload, "taskbook_id")
	if err := protocol.ValidateID(protocol.IDTaskbook, id); err != nil {
		return err
	}
	ready, err := stringArray(evidence.Facts["ready_tasks"])
	if err != nil {
		return fmt.Errorf("%s: ready_tasks: %w", protocol.ErrEvidenceInvalid, err)
	}
	next.Project.Taskbook = &TaskbookRef{ID: id, Path: "docs/taskbook.yaml", BlobOID: blob}
	next.Tasks = make(map[string]TaskState, len(ready))
	for _, taskID := range ready {
		if err := protocol.ValidateID(protocol.IDTask, taskID); err != nil {
			return err
		}
		next.Tasks[taskID] = TaskState{Phase: "ready"}
	}
	return nil
}

func reduceTaskbookUpdated(next *State, operation protocol.Operation, evidence protocol.ExecutionEvidence) error {
	if next.Project.Taskbook == nil {
		return fmt.Errorf("%s: no active Taskbook", protocol.ErrTaskbookNotActive)
	}
	if stringValue(operation.Preconditions, "taskbook_blob") != next.Project.Taskbook.BlobOID ||
		stringValue(evidence.Facts, "old_blob") != next.Project.Taskbook.BlobOID {
		return fmt.Errorf("%s: Taskbook precondition is stale", protocol.ErrTaskbookStale)
	}
	newBlob := stringValue(evidence.Facts, "new_blob")
	if newBlob == "" || newBlob != stringValue(operation.Payload, "candidate_blob") {
		return fmt.Errorf("%s: Taskbook candidate blob mismatch", protocol.ErrTaskbookInvalid)
	}
	diff, ok := evidence.Facts["semantic_diff"].(map[string]any)
	if !ok {
		return fmt.Errorf("%s: Taskbook semantic diff is missing", protocol.ErrEvidenceInvalid)
	}
	added, err := stringArray(diff["added_tasks"])
	if err != nil {
		return fmt.Errorf("%s: added_tasks: %w", protocol.ErrEvidenceInvalid, err)
	}
	for _, taskID := range added {
		if _, exists := next.Tasks[taskID]; exists {
			return fmt.Errorf("%s: added Task ID already exists", protocol.ErrTaskbookInvalid)
		}
		next.Tasks[taskID] = TaskState{Phase: "ready"}
	}
	next.Project.Taskbook.BlobOID = newBlob
	return nil
}

func reduceTaskbookArchived(next *State, operation protocol.Operation) error {
	if next.Project.Taskbook == nil {
		return fmt.Errorf("%s: no active Taskbook", protocol.ErrTaskbookNotActive)
	}
	if operation.Target != next.Project.Taskbook.ID {
		return fmt.Errorf("%s: archive target does not match the active Taskbook", protocol.ErrTaskbookClosureStale)
	}
	if stringValue(operation.Preconditions, "taskbook_blob") != next.Project.Taskbook.BlobOID ||
		!boolValue(operation.Preconditions, "all_tasks_terminal") {
		return fmt.Errorf("%s: archive preconditions are stale", protocol.ErrTaskbookClosureStale)
	}
	for _, task := range next.Tasks {
		if !isTerminal(task.Phase) {
			return fmt.Errorf("%s: Taskbook contains non-terminal Tasks", protocol.ErrTaskbookNotComplete)
		}
	}
	if _, ok := operation.Payload["closure_report"].(map[string]any); !ok {
		return fmt.Errorf("%s: closure report must be an object", protocol.ErrOperationInvalid)
	}
	next.Project.Taskbook = nil
	next.Tasks = map[string]TaskState{}
	return nil
}

func reduceTaskStarted(next *State, operation protocol.Operation, evidence protocol.ExecutionEvidence, signer principal) error {
	task, err := taskFor(next, operation.Target)
	if err != nil {
		return err
	}
	if task.Phase != "ready" || task.Blocked != nil || stringValue(operation.Preconditions, "phase") != "ready" {
		return fmt.Errorf("%s: Task is not unblocked ready", protocol.ErrTaskPhaseInvalid)
	}
	if signer.Actor != stringValue(evidence.Facts, "actor") {
		return fmt.Errorf("%s: starter actor does not match Grant actor", protocol.ErrTaskActorMismatch)
	}
	base := stringValue(evidence.Facts, "base")
	architectureBlob := stringValue(evidence.Facts, "architecture_blob")
	taskbookBlob := stringValue(evidence.Facts, "taskbook_blob")
	if next.Project.Taskbook == nil || architectureBlob != next.Project.Architecture.BlobOID ||
		taskbookBlob != next.Project.Taskbook.BlobOID ||
		architectureBlob != stringValue(operation.Preconditions, "architecture_blob") ||
		taskbookBlob != stringValue(operation.Preconditions, "taskbook_blob") {
		return fmt.Errorf("%s: frozen contract precondition is stale", protocol.ErrTaskbookStale)
	}
	next.Tasks[operation.Target] = TaskState{
		Phase: "active", Actor: signer.Actor, Base: base,
		Contract: &Contract{ArchitectureBlob: architectureBlob, TaskbookBlob: taskbookBlob},
	}
	return nil
}

func reduceTaskReleased(next *State, operation protocol.Operation, evidence protocol.ExecutionEvidence, signer principal) error {
	task, err := taskFor(next, operation.Target)
	if err != nil {
		return err
	}
	if task.Phase != "active" || task.Blocked != nil {
		return fmt.Errorf("%s: release requires an unblocked active Task", protocol.ErrTaskPhaseInvalid)
	}
	if signer.Actor != task.Actor || stringValue(operation.Preconditions, "actor") != task.Actor {
		return fmt.Errorf("%s: release actor mismatch", protocol.ErrTaskActorMismatch)
	}
	if stringValue(operation.Preconditions, "base") != task.Base ||
		stringValue(evidence.Facts, "base") != task.Base ||
		stringValue(evidence.Facts, "observed_work_head") != task.Base {
		return fmt.Errorf("%s: Work Head is not the frozen base", protocol.ErrReleaseHasChanges)
	}
	if strings.TrimSpace(stringValue(operation.Payload, "reason")) == "" {
		return fmt.Errorf("%s: release reason is required", protocol.ErrOperationInvalid)
	}
	next.Tasks[operation.Target] = TaskState{Phase: "ready"}
	return nil
}

func reduceAttemptAbandoned(next *State, operation protocol.Operation, evidence protocol.ExecutionEvidence, objectFormat string) error {
	task, err := taskFor(next, operation.Target)
	if err != nil {
		return err
	}
	switch task.Phase {
	case "active", "submitted", "approved":
	default:
		return fmt.Errorf("%s: abandon requires an active Agent phase", protocol.ErrTaskPhaseInvalid)
	}
	if task.Actor != stringValue(operation.Preconditions, "actor") ||
		task.Base != stringValue(operation.Preconditions, "base") ||
		task.Phase != stringValue(operation.Preconditions, "phase") {
		return fmt.Errorf("%s: abandon Task preconditions are stale", protocol.ErrAttemptStale)
	}
	agentGrantID := stringValue(operation.Preconditions, "agent_grant_id")
	agentKeyID := stringValue(operation.Preconditions, "agent_key_id")
	grant, exists := next.Authority.Grants[agentGrantID]
	if !exists || grant.Actor != task.Actor || grant.KeyID != agentKeyID {
		return fmt.Errorf("%s: abandoned Agent identity is not a current matching Grant", protocol.ErrGrantNotFound)
	}
	failure, ok := operation.Payload["failure"].(map[string]any)
	if !ok || stringValue(failure, "schema") != "chassiss.attempt-failure/v1" {
		return fmt.Errorf("%s: attempt failure record is invalid", protocol.ErrOperationInvalid)
	}
	if next.Project.Taskbook == nil ||
		stringValue(failure, "taskbook") != next.Project.Taskbook.ID ||
		stringValue(failure, "task") != operation.Target ||
		stringValue(failure, "phase") != task.Phase ||
		stringValue(failure, "actor") != task.Actor ||
		stringValue(failure, "agent_grant_id") != agentGrantID ||
		stringValue(failure, "agent_key_id") != agentKeyID {
		return fmt.Errorf("%s: attempt failure record does not match the parent State", protocol.ErrAttemptStale)
	}
	workHead := stringValue(evidence.Facts, "observed_work_head")
	workTree := stringValue(evidence.Facts, "observed_work_tree")
	changedDigest := stringValue(evidence.Facts, "changed_paths_digest")
	if stringValue(failure, "work_head") != workHead ||
		stringValue(failure, "work_tree") != workTree ||
		stringValue(failure, "changed_paths_digest") != changedDigest {
		return fmt.Errorf("%s: attempt failure evidence is inconsistent", protocol.ErrEvidenceInvalid)
	}
	index := AttemptFailureIndex{
		Actor: task.Actor, AgentGrantID: agentGrantID, AgentKeyID: agentKeyID,
		ChangedPathsDigest: changedDigest, Code: stringValue(failure, "code"),
		OperationID: operation.OperationID, Phase: task.Phase,
		Summary: stringValue(failure, "summary"), Task: operation.Target,
		Taskbook: next.Project.Taskbook.ID, WorkHead: workHead, WorkTree: workTree,
	}
	if err := index.Validate(objectFormat); err != nil {
		return fmt.Errorf("%s: %w", protocol.ErrOperationInvalid, err)
	}
	if strings.TrimSpace(stringValue(failure, "reason")) == "" {
		return fmt.Errorf("%s: attempt failure reason is required", protocol.ErrOperationInvalid)
	}
	if next.Audit == nil {
		next.Audit = &AuditIndex{}
	}
	next.Audit.Failures = append(next.Audit.Failures, index)
	next.Tasks[operation.Target] = TaskState{Phase: "ready"}
	return nil
}

func reduceTaskBlocked(next *State, operation protocol.Operation) error {
	task, err := taskFor(next, operation.Target)
	if err != nil {
		return err
	}
	if isTerminal(task.Phase) || task.Blocked != nil ||
		stringValue(operation.Preconditions, "phase") != task.Phase ||
		boolValue(operation.Preconditions, "blocked") {
		return fmt.Errorf("%s: Task cannot be blocked from its current state", protocol.ErrTaskPhaseInvalid)
	}
	if strings.TrimSpace(stringValue(operation.Payload, "reason")) == "" {
		return fmt.Errorf("%s: block reason is required", protocol.ErrOperationInvalid)
	}
	value := true
	task.Blocked = &value
	next.Tasks[operation.Target] = task
	return nil
}

func reduceTaskResumed(next *State, operation protocol.Operation) error {
	task, err := taskFor(next, operation.Target)
	if err != nil {
		return err
	}
	if isTerminal(task.Phase) || task.Blocked == nil || !*task.Blocked ||
		stringValue(operation.Preconditions, "phase") != task.Phase ||
		!boolValue(operation.Preconditions, "blocked") {
		return fmt.Errorf("%s: Task is not blocked in the declared phase", protocol.ErrTaskPhaseInvalid)
	}
	if value := operation.Payload["reason"]; value != nil {
		reason, ok := value.(string)
		if !ok || strings.TrimSpace(reason) == "" {
			return fmt.Errorf("%s: resume reason must be a non-empty string or null", protocol.ErrOperationInvalid)
		}
	}
	task.Blocked = nil
	next.Tasks[operation.Target] = task
	return nil
}

func reduceTaskSubmitted(next *State, operation protocol.Operation, evidence protocol.ExecutionEvidence, signer principal, facts ReduceFacts) error {
	task, err := taskFor(next, operation.Target)
	if err != nil {
		return err
	}
	if task.Phase != "active" || task.Blocked != nil ||
		stringValue(operation.Preconditions, "phase") != "active" {
		return fmt.Errorf("%s: submit requires an unblocked active Task", protocol.ErrTaskPhaseInvalid)
	}
	if signer.Actor != task.Actor || stringValue(operation.Preconditions, "actor") != task.Actor {
		return fmt.Errorf("%s: submitter actor mismatch", protocol.ErrTaskActorMismatch)
	}
	if stringValue(operation.Preconditions, "base") != task.Base ||
		task.Contract == nil ||
		stringValue(operation.Preconditions, "architecture_blob") != task.Contract.ArchitectureBlob ||
		stringValue(operation.Preconditions, "taskbook_blob") != task.Contract.TaskbookBlob {
		return fmt.Errorf("%s: submit frozen contract is stale", protocol.ErrAttemptStale)
	}
	submission, ok := evidence.Facts["submission_evidence"].(map[string]any)
	if !ok {
		return fmt.Errorf("%s: submission_evidence must be an object", protocol.ErrEvidenceInvalid)
	}
	head := stringValue(operation.Payload, "head")
	if stringValue(submission, "head") != head ||
		stringValue(submission, "base") != task.Base ||
		stringValue(submission, "task") != operation.Target ||
		stringValue(submission, "architecture_blob") != task.Contract.ArchitectureBlob ||
		stringValue(submission, "taskbook_blob") != task.Contract.TaskbookBlob {
		return fmt.Errorf("%s: Submission Evidence does not match frozen Task", protocol.ErrAttemptStale)
	}
	if count, ok := integerValue(submission["changed_paths_count"]); !ok || count != facts.ChangedPaths {
		return fmt.Errorf("%s: changed path count mismatch", protocol.ErrEvidenceInvalid)
	}
	evidenceDigest, err := protocol.ObjectDigest("submission-evidence", submission)
	if err != nil {
		return err
	}
	task.Phase = "submitted"
	task.Attempt = &Attempt{
		Head: head, EvidenceDigest: evidenceDigest,
		SubmitterKeyFingerprint: signer.Fingerprint,
	}
	next.Tasks[operation.Target] = task
	return nil
}

func reduceTaskReviewed(next *State, operation protocol.Operation, evidence protocol.ExecutionEvidence, signer principal, objectFormat string, appendIndex bool) error {
	task, err := taskFor(next, operation.Target)
	if err != nil {
		return err
	}
	if task.Blocked != nil || (task.Phase != "submitted" && task.Phase != "approved") ||
		stringValue(operation.Preconditions, "phase") != task.Phase || task.Attempt == nil {
		return fmt.Errorf("%s: review requires an unblocked submitted/approved Task", protocol.ErrTaskPhaseInvalid)
	}
	attemptDigest, err := AttemptDigest(operation.Target, task)
	if err != nil {
		return err
	}
	if stringValue(operation.Preconditions, "attempt_digest") != attemptDigest ||
		stringValue(evidence.Facts, "attempt_digest") != attemptDigest {
		return fmt.Errorf("%s: Review Attempt binding is stale", protocol.ErrAttemptStale)
	}
	verdict := stringValue(operation.Payload, "verdict")
	report, ok := operation.Payload["report"].(map[string]any)
	if !ok {
		return fmt.Errorf("%s: Review Report must be an object", protocol.ErrReviewReportInvalid)
	}
	if stringValue(report, "verdict") != verdict {
		return fmt.Errorf("%s: Report verdict mismatch", protocol.ErrReviewReportInvalid)
	}
	reportBytes, err := protocol.CanonicalJSON(report)
	if err != nil || len(reportBytes) > 65536 {
		return fmt.Errorf("%s: Review Report is invalid or too large", protocol.ErrReviewReportInvalid)
	}
	reportDigest, err := protocol.ObjectDigest("review-report", report)
	if err != nil {
		return err
	}
	context, ok := evidence.Facts["review_context"].(map[string]any)
	if !ok {
		return fmt.Errorf("%s: Review Context must be an object", protocol.ErrEvidenceInvalid)
	}
	contextDigest, err := protocol.ObjectDigest("review-context", context)
	if err != nil {
		return err
	}
	if next.Project.Taskbook == nil {
		return fmt.Errorf("%s: Review requires an active Taskbook", protocol.ErrTaskbookNotActive)
	}
	reviewIndex := ReviewIndex{
		AttemptDigest: stringValue(operation.Preconditions, "attempt_digest"),
		ContextDigest: contextDigest, GrantID: signer.GrantID,
		KeyFingerprint: signer.Fingerprint, KeyID: signer.KeyID,
		OperationID: operation.OperationID, ReportDigest: reportDigest,
		Reviewer: signer.Actor, Task: operation.Target,
		Taskbook: next.Project.Taskbook.ID, Verdict: verdict,
	}
	switch verdict {
	case "request_changes":
		task.Phase = "active"
		task.Attempt = nil
		task.Review = nil
	case "approve":
		if err := validateApprovalReport(report); err != nil {
			return fmt.Errorf("%s: %w", protocol.ErrReviewReportInvalid, err)
		}
		if stringValue(context, "task") != operation.Target ||
			stringValue(context, "attempt_head") != task.Attempt.Head ||
			stringValue(context, "task_base") != task.Base {
			return fmt.Errorf("%s: Review Context does not match Attempt", protocol.ErrReviewContextStale)
		}
		candidate := stringValue(context, "candidate_tree")
		reviewMain := stringValue(context, "review_main")
		if protocol.ValidateOID(candidate, objectFormat) != nil ||
			protocol.ValidateOID(reviewMain, objectFormat) != nil {
			return fmt.Errorf("%s: Review Context contains invalid Git OIDs", protocol.ErrReviewContextStale)
		}
		task.Phase = "approved"
		task.Review = &Review{
			CandidateTree: candidate, ContextDigest: contextDigest,
			KeyFingerprint: signer.Fingerprint, KeyID: signer.KeyID,
			ReportDigest: reportDigest, ReviewMain: reviewMain, Reviewer: signer.Actor,
		}
	default:
		return fmt.Errorf("%s: unknown review verdict %q", protocol.ErrReviewReportInvalid, verdict)
	}
	if appendIndex {
		if next.Audit == nil {
			next.Audit = &AuditIndex{}
		}
		next.Audit.Reviews = append(next.Audit.Reviews, reviewIndex)
	}
	next.Tasks[operation.Target] = task
	return nil
}

func reduceTaskTerminal(next *State, operation protocol.Operation, evidence protocol.ExecutionEvidence, phase, objectFormat string) error {
	task, err := taskFor(next, operation.Target)
	if err != nil {
		return err
	}
	if isTerminal(task.Phase) || stringValue(operation.Preconditions, "phase") != task.Phase {
		return fmt.Errorf("%s: Task is already terminal or phase is stale", protocol.ErrTaskPhaseInvalid)
	}
	if strings.TrimSpace(stringValue(operation.Payload, "reason")) == "" {
		return fmt.Errorf("%s: terminal reason is required", protocol.ErrOperationInvalid)
	}
	expectedAttempt := any(nil)
	if task.Attempt != nil {
		digest, err := AttemptDigest(operation.Target, task)
		if err != nil {
			return err
		}
		expectedAttempt = digest
	}
	if operation.Preconditions["attempt_digest"] != expectedAttempt ||
		evidence.Facts["attempt_digest"] != expectedAttempt {
		return fmt.Errorf("%s: terminal Attempt binding mismatch", protocol.ErrAttemptStale)
	}
	archiveRef, archiveHead := evidence.Facts["archive_ref"], evidence.Facts["archive_head"]
	if expectedAttempt == nil {
		if archiveRef != nil || archiveHead != nil {
			return fmt.Errorf("%s: archive facts must all be null without Attempt", protocol.ErrArchiveRefInvalid)
		}
	} else {
		ref, refOK := archiveRef.(string)
		head, headOK := archiveHead.(string)
		if !refOK || !headOK || head != task.Attempt.Head {
			return fmt.Errorf("%s: archive facts do not bind exact Attempt Head", protocol.ErrArchiveRefInvalid)
		}
		wantRef := "refs/chassiss/archive/" + operation.Target + "/" + strings.TrimPrefix(expectedAttempt.(string), "sha256:")
		if ref != wantRef || protocol.ValidateOID(head, objectFormat) != nil {
			return fmt.Errorf("%s: non-canonical Attempt Archive Ref", protocol.ErrArchiveRefInvalid)
		}
	}
	next.Tasks[operation.Target] = TaskState{Phase: phase}
	return nil
}

func reduceIntegration(next *State, operation protocol.Operation, objectFormat string) error {
	task, err := taskFor(next, operation.Target)
	if err != nil {
		return err
	}
	if task.Phase != "approved" || task.Blocked != nil || task.Attempt == nil || task.Review == nil ||
		stringValue(operation.Preconditions, "phase") != "approved" {
		return fmt.Errorf("%s: Integration requires an unblocked approved Task", protocol.ErrTaskPhaseInvalid)
	}
	attemptDigest, _ := AttemptDigest(operation.Target, task)
	if stringValue(operation.Preconditions, "attempt_digest") != attemptDigest ||
		stringValue(operation.Preconditions, "review_context_digest") != task.Review.ContextDigest ||
		stringValue(operation.Preconditions, "review_report_digest") != task.Review.ReportDigest {
		return fmt.Errorf("%s: Integration preconditions are stale", protocol.ErrReviewContextStale)
	}
	next.Tasks[operation.Target] = TaskState{Phase: "closed"}
	return nil
}

func reduceGrantAdded(next *State, operation protocol.Operation) error {
	if stringValue(operation.Preconditions, "root_key_id") != next.Authority.Root.KeyID ||
		!boolValue(operation.Preconditions, "grant_absent") {
		return fmt.Errorf("%s: Root/Grant precondition mismatch", protocol.ErrOperationInvalid)
	}
	grantID := stringValue(operation.Payload, "grant_id")
	if operation.Target != grantID {
		return fmt.Errorf("%s: Grant target mismatch", protocol.ErrOperationInvalid)
	}
	if _, exists := next.Authority.Grants[grantID]; exists {
		return fmt.Errorf("%s: Grant already exists", protocol.ErrOperationInvalid)
	}
	grantObject, ok := operation.Payload["grant"].(map[string]any)
	if !ok {
		return fmt.Errorf("%s: Grant must be an object", protocol.ErrOperationInvalid)
	}
	var grant Grant
	if err := decodeClosedObject(grantObject, &grant); err != nil {
		return fmt.Errorf("%s: %w", protocol.ErrOperationInvalid, err)
	}
	if err := grant.Validate(); err != nil {
		return err
	}
	next.Authority.Grants[grantID] = grant
	return nil
}

func reduceGrantRevoked(next *State, operation protocol.Operation) error {
	grantID := stringValue(operation.Payload, "grant_id")
	if operation.Target != grantID ||
		stringValue(operation.Preconditions, "grant_id") != grantID ||
		stringValue(operation.Preconditions, "root_key_id") != next.Authority.Root.KeyID {
		return fmt.Errorf("%s: Grant revoke precondition mismatch", protocol.ErrOperationInvalid)
	}
	if _, exists := next.Authority.Grants[grantID]; !exists {
		return fmt.Errorf("%s: Grant does not exist", protocol.ErrGrantNotFound)
	}
	if strings.TrimSpace(stringValue(operation.Payload, "reason")) == "" {
		return fmt.Errorf("%s: revoke reason is required", protocol.ErrOperationInvalid)
	}
	delete(next.Authority.Grants, grantID)
	return nil
}

func reduceOwnerApplied(next *State, operation protocol.Operation) error {
	if !boolValue(operation.Preconditions, "no_active_agent_workflow") {
		return fmt.Errorf("%s: active Agent workflow exists", protocol.ErrOwnerWorkflowActive)
	}
	for _, task := range next.Tasks {
		if task.Phase == "active" || task.Phase == "submitted" || task.Phase == "approved" {
			return fmt.Errorf("%s: active Agent workflow exists", protocol.ErrOwnerWorkflowActive)
		}
	}
	if strings.TrimSpace(stringValue(operation.Payload, "reason")) == "" ||
		strings.TrimSpace(stringValue(operation.Payload, "summary")) == "" {
		return fmt.Errorf("%s: Owner Apply reason and summary are required", protocol.ErrOperationInvalid)
	}
	return nil
}

func AttemptDigest(taskID string, task TaskState) (string, error) {
	if task.Attempt == nil || task.Contract == nil {
		return "", fmt.Errorf("Task has no current Attempt")
	}
	object := map[string]any{
		"architecture_blob": task.Contract.ArchitectureBlob,
		"base":              task.Base,
		"evidence_digest":   task.Attempt.EvidenceDigest,
		"head":              task.Attempt.Head,
		"schema":            "chassiss.attempt/v1",
		"task":              taskID,
		"taskbook_blob":     task.Contract.TaskbookBlob,
	}
	return protocol.ObjectDigest("attempt", object)
}

func taskFor(state *State, id string) (TaskState, error) {
	task, exists := state.Tasks[id]
	if !exists {
		return TaskState{}, fmt.Errorf("%s: Task %s does not exist", protocol.ErrReferenceNotFound, id)
	}
	return task, nil
}

func validateApprovalReport(report map[string]any) error {
	if stringValue(report, "schema") != "chassiss.review-report/v1" ||
		stringValue(report, "verdict") != "approve" ||
		strings.TrimSpace(stringValue(report, "summary")) == "" {
		return fmt.Errorf("approve Report has invalid schema/verdict/summary")
	}
	results, ok := report["results"].(map[string]any)
	if !ok ||
		stringValue(results, "requirements") != "pass" ||
		stringValue(results, "contract") != "pass" ||
		stringValue(results, "architecture") != "conformant" ||
		stringValue(results, "integration") != "pass" {
		return fmt.Errorf("approve Report results are not all passing")
	}
	findings, ok := report["findings"].([]any)
	if !ok {
		return fmt.Errorf("Report findings must be an array")
	}
	for _, value := range findings {
		finding, ok := value.(map[string]any)
		if !ok {
			return fmt.Errorf("Finding must be an object")
		}
		if stringValue(finding, "severity") == "blocking" {
			return fmt.Errorf("approve Report contains a blocking Finding")
		}
	}
	return nil
}

func cloneState(value *State) (*State, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var cloned State
	if err := json.Unmarshal(data, &cloned); err != nil {
		return nil, err
	}
	return &cloned, nil
}

func decodeClosedObject(value map[string]any, target any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	return decoder.Decode(target)
}

func stringValue(object map[string]any, key string) string {
	value, _ := object[key].(string)
	return value
}

func boolValue(object map[string]any, key string) bool {
	value, _ := object[key].(bool)
	return value
}

func integerValue(value any) (int64, bool) {
	switch number := value.(type) {
	case int64:
		return number, true
	case float64:
		if number != float64(int64(number)) {
			return 0, false
		}
		return int64(number), true
	case json.Number:
		integer, err := number.Int64()
		return integer, err == nil
	default:
		return 0, false
	}
}

func stringArray(value any) ([]string, error) {
	switch array := value.(type) {
	case []string:
		return uniqueSorted(array), nil
	case []any:
		result := make([]string, 0, len(array))
		for _, entry := range array {
			text, ok := entry.(string)
			if !ok {
				return nil, fmt.Errorf("array contains a non-string value")
			}
			result = append(result, text)
		}
		if len(uniqueSorted(result)) != len(result) {
			return nil, fmt.Errorf("array contains duplicates")
		}
		return uniqueSorted(result), nil
	default:
		return nil, fmt.Errorf("value is not an array")
	}
}

func isTerminal(phase string) bool {
	return phase == "closed" || phase == "cancelled" || phase == "superseded"
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func uniqueSorted(values []string) []string {
	result := append([]string(nil), values...)
	sort.Strings(result)
	if len(result) == 0 {
		return result
	}
	write := 1
	for read := 1; read < len(result); read++ {
		if result[read] != result[write-1] {
			result[write] = result[read]
			write++
		}
	}
	return result[:write]
}

func matchesAnyTaskScope(task string, patterns []string) bool {
	for _, pattern := range patterns {
		if pattern == "*" || pattern == task ||
			(strings.HasSuffix(pattern, "*") && strings.HasPrefix(task, strings.TrimSuffix(pattern, "*"))) {
			return true
		}
	}
	return false
}

func matchesAnyResourceScope(resource string, patterns []string) bool {
	kind, _, _ := strings.Cut(resource, ":")
	for _, pattern := range patterns {
		if pattern == "*" || pattern == resource || pattern == kind+":*" {
			return true
		}
	}
	return false
}
