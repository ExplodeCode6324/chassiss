package app

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"reflect"
	"sort"
	"strings"
	"time"
)

// Event payloads deliberately contain only the facts needed for one domain
// transition. Derived fields such as actor, role, revision and timestamps come
// from the signed event envelope.
type projectInitializedPayload struct {
	Config   Config `json:"config"`
	Baseline string `json:"baseline"`
}

type artifactSubmittedPayload struct {
	Artifact ArtifactState `json:"artifact"`
	Mission  *MissionState `json:"mission,omitempty"`
	Task     *TaskState    `json:"task,omitempty"`
}

type artifactAcceptedPayload struct {
	ArtifactID     string `json:"artifact_id"`
	AcceptedCommit string `json:"accepted_commit"`
}

type artifactRejectedPayload struct {
	ArtifactID string `json:"artifact_id"`
	Reason     string `json:"reason"`
}

type missionPayload struct {
	MissionID string `json:"mission_id"`
}

type missionBlockedPayload struct {
	MissionID string `json:"mission_id"`
	Reason    string `json:"reason"`
}

type missionAcceptancePayload struct {
	MissionID string `json:"mission_id"`
	Evidence  string `json:"evidence"`
}

type taskClaimedPayload struct {
	TaskID   string `json:"task_id"`
	Owner    string `json:"owner"`
	Branch   string `json:"branch"`
	Baseline string `json:"baseline"`
}

type taskBlockedPayload struct {
	TaskID string `json:"task_id"`
	Reason string `json:"reason"`
}

type taskPayload struct {
	TaskID string `json:"task_id"`
}

type workOpenedPayload struct {
	TaskID         string `json:"task_id"`
	WorktreePath   string `json:"worktree_path"`
	WorktreeID     string `json:"worktree_id"`
	WorktreeDigest string `json:"worktree_digest"`
	Branch         string `json:"branch"`
	Head           string `json:"head"`
}

type workCheckedPayload struct {
	TaskID  string        `json:"task_id"`
	Results []CheckResult `json:"results"`
}

type workCheckpointedPayload struct {
	TaskID     string `json:"task_id"`
	Checkpoint string `json:"checkpoint"`
}

type workSubmittedPayload struct {
	Submission Submission `json:"submission"`
}

type reviewRecordedPayload struct {
	ReviewID     string `json:"review_id"`
	SubmissionID string `json:"submission_id"`
	Report       string `json:"report"`
}

type integrationAppliedPayload struct {
	IntegrationID  string                 `json:"integration_id"`
	SubmissionID   string                 `json:"submission_id"`
	SubmissionHead string                 `json:"submission_head"`
	PreviousHead   string                 `json:"previous_head"`
	IntegratedHead string                 `json:"integrated_head"`
	IntegratedTree string                 `json:"integrated_tree"`
	Checks         map[string]CheckResult `json:"checks"`
}

func marshalPayload(value any) (json.RawMessage, error) {
	data, err := canonicalJSON(value)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(data), nil
}

func decodePayload(data json.RawMessage, output any) error {
	if len(data) == 0 {
		return &CLIError{Code: "CHS-INTEGRITY-PAYLOAD", Message: "event payload is missing", ExitCode: 40}
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(output); err != nil {
		return &CLIError{Code: "CHS-INTEGRITY-PAYLOAD", Message: "event payload is invalid: " + err.Error(), ExitCode: 40}
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return &CLIError{Code: "CHS-INTEGRITY-PAYLOAD", Message: "event payload contains trailing data", ExitCode: 40}
	}
	return nil
}

func payloadResource(event Event, value string) error {
	if value == "" || event.Resource != value {
		return &CLIError{Code: "CHS-INTEGRITY-RESOURCE", Message: "event resource does not match its payload", ExitCode: 40}
	}
	return nil
}

func reduceEvent(config Config, previous State, event Event) (State, error) {
	if event.Version != EventVersion {
		return State{}, &CLIError{Code: "CHS-SCHEMA-V1-UNSUPPORTED", Message: "Event V1 is not supported; initialize a V2 project", ExitCode: 40}
	}
	if event.ProjectID != config.ProjectID || event.Sequence != previous.Revision+1 {
		return State{}, &CLIError{Code: "CHS-INTEGRITY-EVENTS", Message: "event project or sequence is invalid", ExitCode: 40}
	}
	if event.Actor == "" || event.Role == "" || event.CredentialID == "" || event.OccurredAt.IsZero() {
		return State{}, &CLIError{Code: "CHS-INTEGRITY-EVENTS", Message: "event identity or timestamp is invalid", ExitCode: 40}
	}

	var next State
	var err error
	if event.Type == "project.initialized" {
		if previous.Revision != 0 {
			return State{}, &CLIError{Code: "CHS-TRANSITION-PROJECT", Message: "project initialization must be the first event", ExitCode: 40}
		}
		var payload projectInitializedPayload
		if err := decodePayload(event.Payload, &payload); err != nil {
			return State{}, err
		}
		if event.Role != "master" || event.Actor != "master" || event.Resource != config.ProjectID || !reflect.DeepEqual(payload.Config, config) || payload.Baseline == "" {
			return State{}, &CLIError{Code: "CHS-TRANSITION-PROJECT", Message: "project initialization payload is inconsistent", ExitCode: 40}
		}
		next = State{
			Version: StateVersion, ProjectID: config.ProjectID, Phase: "design", Baseline: payload.Baseline,
			Artifacts: map[string]ArtifactState{}, Missions: map[string]MissionState{}, Tasks: map[string]TaskState{},
			Submissions: map[string]Submission{}, Reviews: map[string]Review{}, Integrations: map[string]Integration{},
		}
	} else {
		if previous.Revision == 0 {
			return State{}, &CLIError{Code: "CHS-TRANSITION-PROJECT", Message: "first event must initialize the project", ExitCode: 40}
		}
		next, err = cloneState(previous)
		if err != nil {
			return State{}, err
		}
		if err := applyEventPayload(config, previous, &next, event); err != nil {
			return State{}, err
		}
	}

	next.Version = StateVersion
	next.ProjectID = config.ProjectID
	next.Revision = event.Sequence
	next.UpdatedAt = event.OccurredAt
	next.UpdatedBy = event.Actor
	if err := validateTransition(config, previous, next, event); err != nil {
		return State{}, err
	}
	if err := validateState(config, next); err != nil {
		return State{}, err
	}
	return next, nil
}

func applyEventPayload(config Config, previous State, next *State, event Event) error {
	switch event.Type {
	case "artifact.submitted":
		var payload artifactSubmittedPayload
		if err := decodePayload(event.Payload, &payload); err != nil {
			return err
		}
		artifact := payload.Artifact
		if err := payloadResource(event, artifact.ID); err != nil {
			return err
		}
		if event.Role != "designer" || artifact.ID == "" || artifact.Kind == "" || artifact.Path == "" || artifact.Digest == "" || artifact.SubmissionID == "" {
			return transitionError(event, "artifact submission is incomplete")
		}
		if current, ok := previous.Artifacts[artifact.ID]; ok && current.Status == "accepted" && current.Digest != artifact.Digest {
			return transitionError(event, "accepted artifact is frozen")
		}
		artifact.Status = "submitted"
		artifact.SubmittedBy = event.Actor
		artifact.AcceptedBy, artifact.AcceptedCommit, artifact.RejectedBy, artifact.RejectionReason = "", "", "", ""
		artifact.UpdatedAt = event.OccurredAt
		next.Artifacts[artifact.ID] = artifact
		switch artifact.Kind {
		case "requirements", "architecture":
			if payload.Mission != nil || payload.Task != nil {
				return transitionError(event, "design artifact cannot create mission or task state")
			}
		case "mission":
			if payload.Mission == nil || payload.Task != nil || payload.Mission.ID != artifact.ID || payload.Mission.ArtifactID != artifact.ID {
				return transitionError(event, "mission artifact payload is inconsistent")
			}
			mission := *payload.Mission
			mission.Status = "planned"
			mission.AcceptanceEvidence, mission.BlockReason, mission.PreviousStatus = "", "", ""
			mission.UpdatedAt = event.OccurredAt
			next.Missions[mission.ID] = mission
		case "task":
			if payload.Task == nil || payload.Mission != nil || payload.Task.ID != artifact.ID || payload.Task.ArtifactID != artifact.ID {
				return transitionError(event, "task artifact payload is inconsistent")
			}
			task := *payload.Task
			task.Status = "planned"
			task.Owner, task.Branch, task.Baseline, task.WorktreePath, task.WorktreeID, task.WorktreeDigest, task.Checkpoint, task.BlockReason, task.PreviousStatus, task.SubmissionID = "", "", "", "", "", "", "", "", "", ""
			task.CheckResults = map[string]CheckResult{}
			task.UpdatedAt = event.OccurredAt
			next.Tasks[task.ID] = task
		default:
			return transitionError(event, "unknown artifact kind")
		}
	case "artifact.accepted":
		var payload artifactAcceptedPayload
		if err := decodePayload(event.Payload, &payload); err != nil {
			return err
		}
		if err := payloadResource(event, payload.ArtifactID); err != nil {
			return err
		}
		artifact, ok := previous.Artifacts[payload.ArtifactID]
		if event.Role != "master" || !ok || artifact.Status != "submitted" || payload.AcceptedCommit == "" || artifact.SubmittedBy == event.Actor {
			return transitionError(event, "artifact is not independently acceptable")
		}
		artifact.Status = "accepted"
		artifact.AcceptedBy = event.Actor
		artifact.AcceptedCommit = payload.AcceptedCommit
		artifact.UpdatedAt = event.OccurredAt
		next.Artifacts[artifact.ID] = artifact
		next.Baseline = payload.AcceptedCommit
	case "artifact.rejected":
		var payload artifactRejectedPayload
		if err := decodePayload(event.Payload, &payload); err != nil {
			return err
		}
		if err := payloadResource(event, payload.ArtifactID); err != nil {
			return err
		}
		artifact, ok := previous.Artifacts[payload.ArtifactID]
		if event.Role != "master" || !ok || artifact.Status != "submitted" || strings.TrimSpace(payload.Reason) == "" || artifact.SubmittedBy == event.Actor {
			return transitionError(event, "artifact is not independently rejectable")
		}
		artifact.Status = "rejected"
		artifact.RejectedBy = event.Actor
		artifact.RejectionReason = payload.Reason
		artifact.UpdatedAt = event.OccurredAt
		next.Artifacts[artifact.ID] = artifact
	case "mission.activated":
		var payload missionPayload
		if err := decodePayload(event.Payload, &payload); err != nil {
			return err
		}
		if err := payloadResource(event, payload.MissionID); err != nil {
			return err
		}
		mission, ok := previous.Missions[payload.MissionID]
		if event.Role != "orchestrator" || !ok || previous.ActiveMission != "" || mission.Status != "planned" || previous.Artifacts[mission.ID].Status != "accepted" {
			return transitionError(event, "mission is not activatable")
		}
		if err := taskGraphIssues(mission, previous.Tasks); err != nil {
			return err
		}
		for _, taskID := range mission.TaskIDs {
			if previous.Artifacts[taskID].Status != "accepted" {
				return transitionError(event, "mission task is not accepted")
			}
			task := next.Tasks[taskID]
			if len(task.DependsOn) == 0 {
				task.Status = "ready"
			}
			task.UpdatedAt = event.OccurredAt
			next.Tasks[taskID] = task
		}
		mission.Status = "active"
		mission.UpdatedAt = event.OccurredAt
		next.Missions[mission.ID] = mission
		next.ActiveMission = mission.ID
		next.Phase = "execution"
	case "mission.blocked":
		var payload missionBlockedPayload
		if err := decodePayload(event.Payload, &payload); err != nil {
			return err
		}
		if err := payloadResource(event, payload.MissionID); err != nil {
			return err
		}
		mission, ok := previous.Missions[payload.MissionID]
		if event.Role != "orchestrator" || !ok || previous.ActiveMission != mission.ID || mission.Status != "active" || strings.TrimSpace(payload.Reason) == "" {
			return transitionError(event, "mission is not blockable")
		}
		mission.PreviousStatus = mission.Status
		mission.Status = "blocked"
		mission.BlockReason = payload.Reason
		mission.UpdatedAt = event.OccurredAt
		next.Missions[mission.ID] = mission
	case "mission.resumed":
		var payload missionPayload
		if err := decodePayload(event.Payload, &payload); err != nil {
			return err
		}
		if err := payloadResource(event, payload.MissionID); err != nil {
			return err
		}
		mission, ok := previous.Missions[payload.MissionID]
		if event.Role != "orchestrator" || !ok || previous.ActiveMission != mission.ID || mission.Status != "blocked" || mission.PreviousStatus != "active" {
			return transitionError(event, "mission is not resumable")
		}
		mission.Status, mission.PreviousStatus, mission.BlockReason = "active", "", ""
		mission.UpdatedAt = event.OccurredAt
		next.Missions[mission.ID] = mission
	case "mission.acceptance_submitted":
		var payload missionAcceptancePayload
		if err := decodePayload(event.Payload, &payload); err != nil {
			return err
		}
		if err := payloadResource(event, payload.MissionID); err != nil {
			return err
		}
		mission, ok := previous.Missions[payload.MissionID]
		if event.Role != "orchestrator" || !ok || previous.ActiveMission != mission.ID || mission.Status != "active" || strings.TrimSpace(payload.Evidence) == "" {
			return transitionError(event, "mission acceptance is not submittable")
		}
		for _, taskID := range mission.TaskIDs {
			if previous.Tasks[taskID].Status != "integrated" {
				return transitionError(event, "mission has incomplete tasks")
			}
		}
		mission.Status = "acceptance_pending"
		mission.AcceptanceEvidence = payload.Evidence
		mission.UpdatedAt = event.OccurredAt
		next.Missions[mission.ID] = mission
	case "mission.completed":
		var payload missionPayload
		if err := decodePayload(event.Payload, &payload); err != nil {
			return err
		}
		if err := payloadResource(event, payload.MissionID); err != nil {
			return err
		}
		mission, ok := previous.Missions[payload.MissionID]
		if event.Role != "master" || !ok || previous.ActiveMission != mission.ID || mission.Status != "acceptance_pending" || mission.AcceptanceEvidence == "" {
			return transitionError(event, "mission is not completable")
		}
		mission.Status = "completed"
		mission.UpdatedAt = event.OccurredAt
		next.Missions[mission.ID] = mission
		next.ActiveMission = ""
		next.Phase = "idle"
	case "task.claimed", "task.assigned":
		var payload taskClaimedPayload
		if err := decodePayload(event.Payload, &payload); err != nil {
			return err
		}
		if err := payloadResource(event, payload.TaskID); err != nil {
			return err
		}
		task, ok := previous.Tasks[payload.TaskID]
		if !ok || task.Status != "ready" || task.MissionID != previous.ActiveMission || payload.Owner == "" || payload.Branch == "" || payload.Baseline == "" {
			return transitionError(event, "task is not claimable")
		}
		if event.Type == "task.claimed" && (event.Role != "orchestrator" || payload.Owner != event.Actor) {
			return transitionError(event, "task claim owner is invalid")
		}
		if event.Type == "task.assigned" && event.Role != "orchestrator" {
			return transitionError(event, "task assignment actor is invalid")
		}
		if err := requireMissionExecutable(previous, task.MissionID); err != nil {
			return err
		}
		if activeWIP(previous) >= config.WIPLimit {
			return transitionError(event, "project WIP limit is reached")
		}
		for otherID, other := range previous.Tasks {
			if otherID != task.ID && isActiveTaskStatus(other.Status) && pathsOverlap(task.AllowedPaths, other.AllowedPaths) {
				return transitionError(event, "task path conflicts with another active task")
			}
		}
		task.Owner, task.Branch, task.Baseline, task.Status = payload.Owner, payload.Branch, payload.Baseline, "claimed"
		task.WorktreePath, task.WorktreeID, task.WorktreeDigest = "", "", ""
		task.CheckResults = map[string]CheckResult{}
		task.UpdatedAt = event.OccurredAt
		next.Tasks[task.ID] = task
	case "task.blocked", "work.blocked":
		var payload taskBlockedPayload
		if err := decodePayload(event.Payload, &payload); err != nil {
			return err
		}
		if err := payloadResource(event, payload.TaskID); err != nil {
			return err
		}
		task, ok := previous.Tasks[payload.TaskID]
		if !ok || isClosedTaskStatus(task.Status) || strings.TrimSpace(payload.Reason) == "" {
			return transitionError(event, "task is not blockable")
		}
		if event.Type == "work.blocked" && (event.Role != "developer" || task.Owner != event.Actor) {
			return transitionError(event, "developer cannot block this task")
		}
		if event.Type == "task.blocked" && event.Role != "orchestrator" {
			return transitionError(event, "only orchestrator can block task")
		}
		task.PreviousStatus = task.Status
		task.Status = "blocked"
		task.BlockReason = payload.Reason
		task.UpdatedAt = event.OccurredAt
		next.Tasks[task.ID] = task
	case "task.resumed":
		var payload taskPayload
		if err := decodePayload(event.Payload, &payload); err != nil {
			return err
		}
		if err := payloadResource(event, payload.TaskID); err != nil {
			return err
		}
		task, ok := previous.Tasks[payload.TaskID]
		if event.Role != "orchestrator" || !ok || task.Status != "blocked" || task.PreviousStatus == "" {
			return transitionError(event, "task is not resumable")
		}
		if err := requireMissionExecutable(previous, task.MissionID); err != nil {
			return err
		}
		task.Status, task.PreviousStatus, task.BlockReason = task.PreviousStatus, "", ""
		task.UpdatedAt = event.OccurredAt
		next.Tasks[task.ID] = task
	case "work.opened":
		var payload workOpenedPayload
		if err := decodePayload(event.Payload, &payload); err != nil {
			return err
		}
		if err := payloadResource(event, payload.TaskID); err != nil {
			return err
		}
		task, ok := previous.Tasks[payload.TaskID]
		if event.Role != "developer" || !ok || task.Owner != event.Actor || !containsString([]string{"claimed", "changes_requested"}, task.Status) || payload.WorktreePath == "" || payload.WorktreeID == "" || payload.WorktreeDigest == "" || payload.Branch != task.Branch || payload.Head == "" {
			return transitionError(event, "task is not openable by this developer")
		}
		if err := requireMissionExecutable(previous, task.MissionID); err != nil {
			return err
		}
		task.Status = "in_progress"
		task.WorktreePath = payload.WorktreePath
		task.WorktreeID = payload.WorktreeID
		task.WorktreeDigest = payload.WorktreeDigest
		task.UpdatedAt = event.OccurredAt
		next.Tasks[task.ID] = task
	case "work.checked":
		var payload workCheckedPayload
		if err := decodePayload(event.Payload, &payload); err != nil {
			return err
		}
		if err := payloadResource(event, payload.TaskID); err != nil {
			return err
		}
		task, ok := previous.Tasks[payload.TaskID]
		if event.Role != "developer" || !ok || task.Owner != event.Actor || task.Status != "in_progress" || len(payload.Results) == 0 {
			return transitionError(event, "task checks cannot be recorded")
		}
		if err := requireMissionExecutable(previous, task.MissionID); err != nil {
			return err
		}
		known := map[string]CheckSpec{}
		for _, check := range task.Checks {
			known[check.ID] = check
		}
		seen := map[string]bool{}
		for _, result := range payload.Results {
			check, exists := known[result.ID]
			if !exists || seen[result.ID] || result.SnapshotDigest == "" || result.SpecDigest != checkSpecDigest(check) {
				return transitionError(event, "check result does not match task contract")
			}
			seen[result.ID] = true
			result.CheckedAt = event.OccurredAt
			task.CheckResults[result.ID] = result
		}
		task.UpdatedAt = event.OccurredAt
		next.Tasks[task.ID] = task
	case "work.checkpointed":
		var payload workCheckpointedPayload
		if err := decodePayload(event.Payload, &payload); err != nil {
			return err
		}
		if err := payloadResource(event, payload.TaskID); err != nil {
			return err
		}
		task, ok := previous.Tasks[payload.TaskID]
		if event.Role != "developer" || !ok || task.Owner != event.Actor || task.Status != "in_progress" {
			return transitionError(event, "task checkpoint cannot be recorded")
		}
		if err := requireMissionExecutable(previous, task.MissionID); err != nil {
			return err
		}
		task.Checkpoint = payload.Checkpoint
		task.UpdatedAt = event.OccurredAt
		next.Tasks[task.ID] = task
	case "work.submitted":
		var payload workSubmittedPayload
		if err := decodePayload(event.Payload, &payload); err != nil {
			return err
		}
		submission := payload.Submission
		if err := payloadResource(event, submission.TaskID); err != nil {
			return err
		}
		task, ok := previous.Tasks[submission.TaskID]
		if event.Role != "developer" || !ok || task.Owner != event.Actor || task.Status != "in_progress" || submission.ID == "" || submission.BaseCommit != task.Baseline || submission.HeadCommit == "" {
			return transitionError(event, "submission does not match active task")
		}
		if err := requireMissionExecutable(previous, task.MissionID); err != nil {
			return err
		}
		for _, file := range submission.ChangedFiles {
			if !allowedFile(task.AllowedPaths, file) {
				return transitionError(event, "submission contains out-of-scope file")
			}
		}
		submission.Actor = event.Actor
		submission.Status = "review_pending"
		submission.ReviewID, submission.IntegrationID = "", ""
		submission.CreatedAt = event.OccurredAt
		digest, err := calculateSubmissionDigest(submission)
		if err != nil {
			return err
		}
		if payload.Submission.Digest != "" && payload.Submission.Digest != digest {
			return transitionError(event, "submission digest is invalid")
		}
		submission.Digest = digest
		if _, exists := previous.Submissions[submission.ID]; exists {
			return transitionError(event, "submission ID already exists")
		}
		task.Status = "review_pending"
		task.SubmissionID = submission.ID
		task.UpdatedAt = event.OccurredAt
		next.Tasks[task.ID] = task
		next.Submissions[submission.ID] = submission
	case "review.approved", "review.changes_requested":
		var payload reviewRecordedPayload
		if err := decodePayload(event.Payload, &payload); err != nil {
			return err
		}
		if err := payloadResource(event, payload.SubmissionID); err != nil {
			return err
		}
		submission, ok := previous.Submissions[payload.SubmissionID]
		if event.Role != "reviewer" || !ok || submission.Status != "review_pending" || submission.Actor == event.Actor || payload.ReviewID == "" {
			return transitionError(event, "submission is not independently reviewable")
		}
		if _, exists := previous.Reviews[payload.ReviewID]; exists {
			return transitionError(event, "review ID already exists")
		}
		verdict, status := "approve", "approved"
		if event.Type == "review.changes_requested" {
			verdict, status = "request_changes", "changes_requested"
		}
		review := Review{ID: payload.ReviewID, SubmissionID: submission.ID, SubmissionDigest: submission.Digest, Reviewer: event.Actor, Verdict: verdict, Report: payload.Report, CreatedAt: event.OccurredAt}
		next.Reviews[review.ID] = review
		submission.Status, submission.ReviewID = status, review.ID
		next.Submissions[submission.ID] = submission
		task := next.Tasks[submission.TaskID]
		task.Status = status
		task.UpdatedAt = event.OccurredAt
		next.Tasks[task.ID] = task
	case "integration.applied":
		var payload integrationAppliedPayload
		if err := decodePayload(event.Payload, &payload); err != nil {
			return err
		}
		if err := payloadResource(event, payload.SubmissionID); err != nil {
			return err
		}
		submission, ok := previous.Submissions[payload.SubmissionID]
		if event.Role != "reviewer" || !ok || submission.Status != "approved" || payload.IntegrationID == "" || payload.PreviousHead == "" || payload.IntegratedHead == "" || payload.IntegratedTree == "" || payload.SubmissionHead != submission.HeadCommit {
			return transitionError(event, "submission is not integratable")
		}
		if err := requireMissionExecutable(previous, previous.Tasks[submission.TaskID].MissionID); err != nil {
			return err
		}
		review, ok := previous.Reviews[submission.ReviewID]
		if !ok || review.Verdict != "approve" || review.SubmissionDigest != submission.Digest || review.Reviewer != event.Actor || payload.PreviousHead != previous.Baseline {
			return transitionError(event, "integration evidence is inconsistent")
		}
		if _, exists := previous.Integrations[payload.IntegrationID]; exists {
			return transitionError(event, "integration ID already exists")
		}
		checks := map[string]CheckResult{}
		for _, spec := range previous.Tasks[submission.TaskID].Checks {
			result, ok := payload.Checks[spec.ID]
			if !ok || !result.Passed || result.SpecDigest != checkSpecDigest(spec) || result.SnapshotDigest != payload.IntegratedTree {
				return transitionError(event, "integration result lacks a passed merged-tree check")
			}
			result.CheckedAt = event.OccurredAt
			checks[result.ID] = result
		}
		integration := Integration{ID: payload.IntegrationID, SubmissionID: submission.ID, SubmissionHead: payload.SubmissionHead, PreviousHead: payload.PreviousHead, IntegratedHead: payload.IntegratedHead, IntegratedTree: payload.IntegratedTree, Checks: checks, IntegratedBy: event.Actor, CreatedAt: event.OccurredAt}
		next.Integrations[integration.ID] = integration
		submission.Status, submission.IntegrationID = "integrated", integration.ID
		next.Submissions[submission.ID] = submission
		task := next.Tasks[submission.TaskID]
		task.Status = "integrated"
		task.WorktreePath = ""
		task.WorktreeID = ""
		task.WorktreeDigest = ""
		task.UpdatedAt = event.OccurredAt
		next.Tasks[task.ID] = task
		next.Baseline = payload.IntegratedHead
		for otherID, other := range next.Tasks {
			if other.Status != "planned" || other.MissionID != next.ActiveMission {
				continue
			}
			ready := true
			for _, dependency := range other.DependsOn {
				if next.Tasks[dependency].Status != "integrated" {
					ready = false
					break
				}
			}
			if ready {
				other.Status = "ready"
				other.UpdatedAt = event.OccurredAt
				next.Tasks[otherID] = other
			}
		}
	default:
		return &CLIError{Code: "CHS-INTEGRITY-EVENTS", Message: "unknown event type: " + event.Type, ExitCode: 40}
	}
	return nil
}

func transitionError(event Event, message string) error {
	return &CLIError{Code: "CHS-TRANSITION-" + strings.ToUpper(strings.ReplaceAll(event.Type, ".", "-")), Message: message, ExitCode: 40}
}

func validateTransition(config Config, previous, next State, event Event) error {
	if next.Revision != previous.Revision+1 || next.Revision != event.Sequence || next.ProjectID != event.ProjectID || next.UpdatedBy != event.Actor || !next.UpdatedAt.Equal(event.OccurredAt) {
		return &CLIError{Code: "CHS-TRANSITION-INVALID", Message: "event did not produce the required state envelope", ExitCode: 40}
	}
	if previous.Revision > 0 && (next.Version != previous.Version || next.ProjectID != previous.ProjectID) {
		return &CLIError{Code: "CHS-TRANSITION-INVALID", Message: "event changed immutable state identity", ExitCode: 40}
	}
	return nil
}

func eventPayloadFromCandidate(previous, candidate State, eventType, resource string) (any, error) {
	switch eventType {
	case "artifact.submitted":
		artifact, ok := candidate.Artifacts[resource]
		if !ok {
			return nil, fmt.Errorf("artifact payload %s is missing", resource)
		}
		payload := artifactSubmittedPayload{Artifact: artifact}
		if artifact.Kind == "mission" {
			mission := candidate.Missions[resource]
			payload.Mission = &mission
		}
		if artifact.Kind == "task" {
			task := candidate.Tasks[resource]
			payload.Task = &task
		}
		return payload, nil
	case "artifact.accepted":
		artifact := candidate.Artifacts[resource]
		return artifactAcceptedPayload{ArtifactID: resource, AcceptedCommit: artifact.AcceptedCommit}, nil
	case "artifact.rejected":
		artifact := candidate.Artifacts[resource]
		return artifactRejectedPayload{ArtifactID: resource, Reason: artifact.RejectionReason}, nil
	case "mission.activated", "mission.resumed", "mission.completed":
		return missionPayload{MissionID: resource}, nil
	case "mission.blocked":
		return missionBlockedPayload{MissionID: resource, Reason: candidate.Missions[resource].BlockReason}, nil
	case "mission.acceptance_submitted":
		return missionAcceptancePayload{MissionID: resource, Evidence: candidate.Missions[resource].AcceptanceEvidence}, nil
	case "task.claimed", "task.assigned":
		task := candidate.Tasks[resource]
		return taskClaimedPayload{TaskID: resource, Owner: task.Owner, Branch: task.Branch, Baseline: task.Baseline}, nil
	case "task.blocked", "work.blocked":
		return taskBlockedPayload{TaskID: resource, Reason: candidate.Tasks[resource].BlockReason}, nil
	case "task.resumed":
		return taskPayload{TaskID: resource}, nil
	case "work.opened":
		task := candidate.Tasks[resource]
		return workOpenedPayload{TaskID: resource, WorktreePath: task.WorktreePath, WorktreeID: task.WorktreeID, WorktreeDigest: task.WorktreeDigest, Branch: task.Branch, Head: task.Baseline}, nil
	case "work.checked":
		before := previous.Tasks[resource].CheckResults
		after := candidate.Tasks[resource].CheckResults
		ids := make([]string, 0)
		for id, result := range after {
			if old, ok := before[id]; !ok || !reflect.DeepEqual(old, result) {
				ids = append(ids, id)
			}
		}
		sort.Strings(ids)
		results := make([]CheckResult, 0, len(ids))
		for _, id := range ids {
			results = append(results, after[id])
		}
		return workCheckedPayload{TaskID: resource, Results: results}, nil
	case "work.checkpointed":
		return workCheckpointedPayload{TaskID: resource, Checkpoint: candidate.Tasks[resource].Checkpoint}, nil
	case "work.submitted":
		task := candidate.Tasks[resource]
		submission := candidate.Submissions[task.SubmissionID]
		submission.Actor = ""
		submission.Status = ""
		submission.ReviewID = ""
		submission.IntegrationID = ""
		submission.CreatedAt = time.Time{}
		submission.Digest = ""
		return workSubmittedPayload{Submission: submission}, nil
	case "review.approved", "review.changes_requested":
		submission := candidate.Submissions[resource]
		review := candidate.Reviews[submission.ReviewID]
		return reviewRecordedPayload{ReviewID: review.ID, SubmissionID: resource, Report: review.Report}, nil
	case "integration.applied":
		submission := candidate.Submissions[resource]
		integration := candidate.Integrations[submission.IntegrationID]
		return integrationAppliedPayload{IntegrationID: integration.ID, SubmissionID: resource, SubmissionHead: integration.SubmissionHead, PreviousHead: integration.PreviousHead, IntegratedHead: integration.IntegratedHead, IntegratedTree: integration.IntegratedTree, Checks: integration.Checks}, nil
	default:
		return nil, &CLIError{Code: "CHS-INTEGRITY-EVENTS", Message: "unknown event type: " + eventType, ExitCode: 40}
	}
}
