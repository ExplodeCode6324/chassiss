package app

import (
	"fmt"
	"strings"
)

var validPhases = stringSet([]string{"design", "execution", "idle"})
var validArtifactKinds = stringSet([]string{"requirements", "architecture", "mission", "task"})
var validArtifactStatuses = stringSet([]string{"submitted", "accepted", "rejected"})
var validMissionStatuses = stringSet([]string{"planned", "active", "blocked", "acceptance_pending", "completed"})
var validTaskStatuses = stringSet([]string{"planned", "ready", "claimed", "in_progress", "review_pending", "changes_requested", "approved", "integrated", "blocked", "cancelled", "superseded"})
var validSubmissionStatuses = stringSet([]string{"review_pending", "changes_requested", "approved", "integrated"})
var validReviewVerdicts = stringSet([]string{"approve", "request_changes"})

func isActiveTaskStatus(status string) bool {
	return containsString([]string{"claimed", "in_progress", "review_pending", "changes_requested", "approved", "blocked"}, status)
}

func isClosedTaskStatus(status string) bool {
	return containsString([]string{"integrated", "cancelled", "superseded"}, status)
}

func requireMissionExecutable(state State, missionID string) error {
	mission, ok := state.Missions[missionID]
	if !ok || state.ActiveMission != missionID {
		return &CLIError{Code: "CHS-MISSION-NOT-ACTIVE", Message: "task does not belong to the active mission", ExitCode: 10}
	}
	if mission.Status == "blocked" {
		return &CLIError{Code: "CHS-MISSION-BLOCKED", Message: "mission is blocked; downstream progress is disabled", ExitCode: 10, Remedy: []string{"inspect the block reason", "resume the mission after Master resolves the cause"}}
	}
	if mission.Status != "active" {
		return &CLIError{Code: "CHS-MISSION-NOT-ACTIVE", Message: "mission is not executable", ExitCode: 10}
	}
	return nil
}

func validateState(config Config, state State) error {
	if config.Version != ConfigVersion {
		return &CLIError{Code: "CHS-SCHEMA-V1-UNSUPPORTED", Message: "Config V1 is not supported; initialize a V2 project", ExitCode: 40}
	}
	if state.Version != StateVersion || state.ProjectID != config.ProjectID || state.Revision < 1 {
		return &CLIError{Code: "CHS-STATE-INVALID", Message: "state version, project, or revision is invalid", ExitCode: 40}
	}
	if _, ok := validPhases[state.Phase]; !ok {
		return &CLIError{Code: "CHS-STATE-PHASE", Message: "unknown project phase: " + state.Phase, ExitCode: 40}
	}
	if state.Baseline == "" || state.UpdatedAt.IsZero() || state.UpdatedBy == "" {
		return &CLIError{Code: "CHS-STATE-INVALID", Message: "state baseline or update identity is missing", ExitCode: 40}
	}
	if state.Artifacts == nil || state.Missions == nil || state.Tasks == nil || state.Submissions == nil || state.Reviews == nil || state.Integrations == nil {
		return &CLIError{Code: "CHS-STATE-INVALID", Message: "state collections must not be null", ExitCode: 40}
	}

	for id, artifact := range state.Artifacts {
		if id == "" || artifact.ID != id {
			return stateError("CHS-STATE-ARTIFACT", "artifact map key does not match artifact ID")
		}
		if _, ok := validArtifactKinds[artifact.Kind]; !ok {
			return stateError("CHS-STATE-ARTIFACT", "unknown artifact kind: "+artifact.Kind)
		}
		if _, ok := validArtifactStatuses[artifact.Status]; !ok {
			return stateError("CHS-STATE-ARTIFACT", "unknown artifact status: "+artifact.Status)
		}
		if artifact.Path == "" || artifact.Digest == "" || artifact.SubmissionID == "" || artifact.SubmittedBy == "" || artifact.UpdatedAt.IsZero() {
			return stateError("CHS-STATE-ARTIFACT", "artifact is missing provenance: "+id)
		}
		if artifact.Status == "accepted" && (artifact.AcceptedBy == "" || artifact.AcceptedCommit == "") {
			return stateError("CHS-STATE-ARTIFACT", "accepted artifact lacks immutable evidence: "+id)
		}
		if artifact.Status == "rejected" && (artifact.RejectedBy == "" || strings.TrimSpace(artifact.RejectionReason) == "") {
			return stateError("CHS-STATE-ARTIFACT", "rejected artifact lacks evidence: "+id)
		}
	}

	activeCount := 0
	for id, mission := range state.Missions {
		if mission.ID != id || mission.ArtifactID != id {
			return stateError("CHS-STATE-MISSION", "mission identity is inconsistent: "+id)
		}
		if _, ok := validMissionStatuses[mission.Status]; !ok {
			return stateError("CHS-STATE-MISSION", "unknown mission status: "+mission.Status)
		}
		if mission.UpdatedAt.IsZero() {
			return stateError("CHS-STATE-MISSION", "mission update timestamp is missing: "+id)
		}
		if mission.Status == "active" || mission.Status == "blocked" || mission.Status == "acceptance_pending" {
			activeCount++
			if state.ActiveMission != id {
				return stateError("CHS-STATE-MISSION", "active mission pointer is inconsistent")
			}
		}
		if mission.Status == "blocked" && (mission.PreviousStatus != "active" || strings.TrimSpace(mission.BlockReason) == "") {
			return stateError("CHS-STATE-MISSION", "blocked mission lacks resumable evidence: "+id)
		}
		if mission.Status == "completed" && state.ActiveMission == id {
			return stateError("CHS-STATE-MISSION", "completed mission cannot remain active")
		}
		seen := map[string]bool{}
		for _, taskID := range mission.TaskIDs {
			if taskID == "" || seen[taskID] {
				return stateError("CHS-STATE-MISSION", "mission contains an empty or duplicate task ID: "+id)
			}
			seen[taskID] = true
			if task, exists := state.Tasks[taskID]; exists && task.MissionID != id {
				return stateError("CHS-STATE-TASK", "task belongs to a different mission: "+taskID)
			}
		}
	}
	if activeCount > 1 || (activeCount == 0 && state.ActiveMission != "") {
		return stateError("CHS-STATE-MISSION", "only one mission may be active")
	}
	if state.Phase == "execution" && activeCount != 1 {
		return stateError("CHS-STATE-PHASE", "execution phase requires one active mission")
	}
	if state.Phase != "execution" && activeCount != 0 {
		return stateError("CHS-STATE-PHASE", "active mission requires execution phase")
	}

	activeTasks := make([]TaskState, 0)
	worktreePaths := map[string]string{}
	worktreeIDs := map[string]string{}
	worktreeDigests := map[string]string{}
	for id, task := range state.Tasks {
		mission, missionExists := state.Missions[task.MissionID]
		if task.ID != id || !missionExists || !containsString(mission.TaskIDs, id) || task.ArtifactID != id {
			return stateError("CHS-STATE-TASK", "task identity or mission membership is invalid: "+id)
		}
		if _, ok := validTaskStatuses[task.Status]; !ok {
			return stateError("CHS-STATE-TASK", "unknown task status: "+task.Status)
		}
		if task.UpdatedAt.IsZero() || task.CheckResults == nil {
			return stateError("CHS-STATE-TASK", "task update data is incomplete: "+id)
		}
		for _, dependency := range task.DependsOn {
			dep, ok := state.Tasks[dependency]
			if !ok || dep.MissionID != task.MissionID {
				return stateError("CHS-STATE-TASK", fmt.Sprintf("task %s has invalid dependency %s", id, dependency))
			}
			if task.Status == "ready" && dep.Status != "integrated" {
				return stateError("CHS-STATE-TASK", fmt.Sprintf("ready task %s has unmet dependency %s", id, dependency))
			}
		}
		if containsString([]string{"planned", "ready"}, task.Status) && (task.Owner != "" || task.Branch != "" || task.Baseline != "" || task.WorktreePath != "" || task.WorktreeID != "" || task.WorktreeDigest != "") {
			return stateError("CHS-STATE-TASK", "unclaimed task contains ownership data: "+id)
		}
		needsOwner := containsString([]string{"claimed", "in_progress", "review_pending", "changes_requested", "approved", "integrated"}, task.Status)
		if task.Status == "blocked" && containsString([]string{"claimed", "in_progress", "review_pending", "changes_requested", "approved"}, task.PreviousStatus) {
			needsOwner = true
		}
		if needsOwner && (task.Owner == "" || task.Branch == "" || task.Baseline == "") {
			return stateError("CHS-STATE-TASK", "active task lacks owner, branch, or baseline: "+id)
		}
		needsWorktree := containsString([]string{"in_progress", "review_pending", "changes_requested", "approved"}, task.Status)
		if task.Status == "blocked" && containsString([]string{"in_progress", "review_pending", "changes_requested", "approved"}, task.PreviousStatus) {
			needsWorktree = true
		}
		if needsWorktree && (task.WorktreePath == "" || task.WorktreeID == "" || task.WorktreeDigest == "") {
			return stateError("CHS-STATE-WORKTREE", "active work task lacks worktree identity: "+id)
		}
		if task.WorktreePath != "" {
			if owner, exists := worktreePaths[task.WorktreePath]; exists && owner != id {
				return stateError("CHS-STATE-WORKTREE", "worktree path is bound to multiple tasks")
			}
			worktreePaths[task.WorktreePath] = id
		}
		if task.WorktreeID != "" {
			if owner, exists := worktreeIDs[task.WorktreeID]; exists && owner != id {
				return stateError("CHS-STATE-WORKTREE", "worktree identity is bound to multiple tasks")
			}
			worktreeIDs[task.WorktreeID] = id
		}
		if task.WorktreeDigest != "" {
			if owner, exists := worktreeDigests[task.WorktreeDigest]; exists && owner != id {
				return stateError("CHS-STATE-WORKTREE", "worktree binding digest is bound to multiple tasks")
			}
			worktreeDigests[task.WorktreeDigest] = id
		}
		if task.Status == "blocked" && (task.PreviousStatus == "" || strings.TrimSpace(task.BlockReason) == "") {
			return stateError("CHS-STATE-TASK", "blocked task lacks resumable evidence: "+id)
		}
		if isActiveTaskStatus(task.Status) {
			activeTasks = append(activeTasks, task)
		}
		if task.SubmissionID != "" {
			submission, ok := state.Submissions[task.SubmissionID]
			if !ok || submission.TaskID != task.ID {
				return stateError("CHS-STATE-SUBMISSION", "task submission reference is inconsistent: "+id)
			}
		}
	}
	if len(activeTasks) > config.WIPLimit {
		return stateError("CHS-STATE-WIP", "active task count exceeds project WIP limit")
	}
	for left := 0; left < len(activeTasks); left++ {
		for right := left + 1; right < len(activeTasks); right++ {
			if pathsOverlap(activeTasks[left].AllowedPaths, activeTasks[right].AllowedPaths) {
				return stateError("CHS-STATE-PATH-CONFLICT", "active task write scopes overlap")
			}
		}
	}
	if err := validateTaskDAGs(state); err != nil {
		return err
	}

	for id, submission := range state.Submissions {
		if submission.ID != id || submission.TaskID == "" || submission.Actor == "" || submission.BaseCommit == "" || submission.HeadCommit == "" || submission.Digest == "" || submission.CreatedAt.IsZero() {
			return stateError("CHS-STATE-SUBMISSION", "submission identity or evidence is incomplete: "+id)
		}
		if _, ok := validSubmissionStatuses[submission.Status]; !ok {
			return stateError("CHS-STATE-SUBMISSION", "unknown submission status: "+submission.Status)
		}
		task, ok := state.Tasks[submission.TaskID]
		if !ok || task.Owner != submission.Actor || task.Baseline != submission.BaseCommit {
			return stateError("CHS-STATE-SUBMISSION", "submission does not match task ownership or baseline: "+id)
		}
		digest, err := calculateSubmissionDigest(submission)
		if err != nil || digest != submission.Digest {
			return stateError("CHS-STATE-SUBMISSION", "submission digest is invalid: "+id)
		}
		if containsString([]string{"approved", "changes_requested", "integrated"}, submission.Status) {
			review, ok := state.Reviews[submission.ReviewID]
			if !ok || review.SubmissionID != id || review.SubmissionDigest != submission.Digest {
				return stateError("CHS-STATE-REVIEW", "submission review reference is inconsistent: "+id)
			}
		}
		if submission.Status == "integrated" {
			integration, ok := state.Integrations[submission.IntegrationID]
			if !ok || integration.SubmissionID != id {
				return stateError("CHS-STATE-INTEGRATION", "integrated submission lacks integration evidence: "+id)
			}
		}
	}

	for id, review := range state.Reviews {
		if review.ID != id || review.SubmissionID == "" || review.SubmissionDigest == "" || review.Reviewer == "" || review.CreatedAt.IsZero() {
			return stateError("CHS-STATE-REVIEW", "review identity or evidence is incomplete: "+id)
		}
		if _, ok := validReviewVerdicts[review.Verdict]; !ok {
			return stateError("CHS-STATE-REVIEW", "unknown review verdict: "+review.Verdict)
		}
		submission, ok := state.Submissions[review.SubmissionID]
		if !ok || submission.Digest != review.SubmissionDigest || submission.Actor == review.Reviewer {
			return stateError("CHS-STATE-REVIEW", "review is not bound to an independent submission: "+id)
		}
	}

	for id, integration := range state.Integrations {
		if integration.ID != id || integration.SubmissionID == "" || integration.SubmissionHead == "" || integration.PreviousHead == "" || integration.IntegratedHead == "" || integration.IntegratedTree == "" || integration.Checks == nil || integration.IntegratedBy == "" || integration.CreatedAt.IsZero() {
			return stateError("CHS-STATE-INTEGRATION", "integration identity or evidence is incomplete: "+id)
		}
		submission, ok := state.Submissions[integration.SubmissionID]
		if !ok || submission.Status != "integrated" || submission.IntegrationID != id || submission.HeadCommit != integration.SubmissionHead {
			return stateError("CHS-STATE-INTEGRATION", "integration does not match submission: "+id)
		}
		review, ok := state.Reviews[submission.ReviewID]
		if !ok || review.Verdict != "approve" || review.Reviewer != integration.IntegratedBy || review.SubmissionDigest != submission.Digest {
			return stateError("CHS-STATE-INTEGRATION", "integration is not backed by the approving review: "+id)
		}
		for _, spec := range state.Tasks[submission.TaskID].Checks {
			result, ok := integration.Checks[spec.ID]
			if !ok || !result.Passed || result.SpecDigest != checkSpecDigest(spec) || result.SnapshotDigest != integration.IntegratedTree {
				return stateError("CHS-STATE-INTEGRATION", "integration lacks merged-tree check evidence: "+id)
			}
		}
	}
	return nil
}

func validateTaskDAGs(state State) error {
	for _, mission := range state.Missions {
		visiting := map[string]bool{}
		visited := map[string]bool{}
		var visit func(string) error
		visit = func(id string) error {
			if visiting[id] {
				return stateError("CHS-STATE-TASK-CYCLE", "task dependency graph contains a cycle at "+id)
			}
			if visited[id] {
				return nil
			}
			task, ok := state.Tasks[id]
			if !ok {
				return nil // A mission may be submitted before all task artifacts exist.
			}
			visiting[id] = true
			for _, dependency := range task.DependsOn {
				if err := visit(dependency); err != nil {
					return err
				}
			}
			visiting[id] = false
			visited[id] = true
			return nil
		}
		for _, id := range mission.TaskIDs {
			if err := visit(id); err != nil {
				return err
			}
		}
	}
	return nil
}

func stateError(code, message string) error {
	return &CLIError{Code: code, Message: message, ExitCode: 40}
}
