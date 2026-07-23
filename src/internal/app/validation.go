package app

import (
	"fmt"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

var validPhases = stringSet([]string{"design", "execution", "idle"})
var validArtifactKinds = stringSet([]string{"requirements", "architecture", "mission", "task"})
var validArtifactStatuses = stringSet([]string{"submitted", "accepted", "rejected"})
var validMissionStatuses = stringSet([]string{"planned", "active", "blocked", "acceptance_pending", "completed"})
var validTaskStatuses = stringSet([]string{"planned", "ready", "claimed", "in_progress", "review_pending", "changes_requested", "approved", "integrated", "blocked", "cancelled", "superseded"})
var validSubmissionStatuses = stringSet([]string{"review_pending", "changes_requested", "approved", "integrated"})
var validReviewVerdicts = stringSet([]string{"approve", "request_changes"})

var newProjectDefaultTaskBudget = TaskBudget{MaxChangedFiles: 100, MaxDiffLines: 20000, MaxCommits: 20}

const maxReviewReportBytes = 64 * 1024

func validateReviewReport(report string) error {
	if strings.TrimSpace(report) == "" {
		return &CLIError{Code: "CHS-REVIEW-REPORT", Message: "review report must not be empty", ExitCode: 20}
	}
	if !utf8.ValidString(report) || len(report) > maxReviewReportBytes {
		return &CLIError{Code: "CHS-REVIEW-REPORT", Message: "review report must be valid UTF-8 and no larger than 64 KiB", ExitCode: 20}
	}
	return nil
}

func isActiveTaskStatus(status string) bool {
	return containsString([]string{"claimed", "in_progress", "review_pending", "changes_requested", "approved"}, status)
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

func validChangedFiles(files []string) bool {
	if len(files) == 0 {
		return false
	}
	for index, file := range files {
		clean := filepath.ToSlash(filepath.Clean(filepath.FromSlash(file)))
		if file == "" || file == "." || filepath.IsAbs(file) || strings.ContainsRune(file, '\x00') || clean != file || strings.HasPrefix(file, "../") || (index > 0 && files[index-1] >= file) {
			return false
		}
	}
	return true
}

func validateTaskBudgetDefinition(budget TaskBudget) error {
	if budget.MaxChangedFiles < 0 || budget.MaxChangedFiles > 1000000 || budget.MaxDiffLines < 0 || budget.MaxDiffLines > 1000000000 || budget.MaxCommits < 0 || budget.MaxCommits > 1000000 {
		return &CLIError{Code: "CHS-TASK-BUDGET", Message: "Task budget values must be non-negative and within supported limits", ExitCode: 10}
	}
	return nil
}

func taskBudgetEnabled(budget TaskBudget) bool {
	return budget.MaxChangedFiles > 0 || budget.MaxDiffLines > 0 || budget.MaxCommits > 0
}

func validateTaskBudget(budget TaskBudget, metrics ChangeMetrics) error {
	if err := validateTaskBudgetDefinition(budget); err != nil {
		return err
	}
	if metrics.ChangedFiles < 1 || metrics.AddedLines < 0 || metrics.DeletedLines < 0 || metrics.DiffLines != metrics.AddedLines+metrics.DeletedLines || metrics.Commits < 1 || metrics.BinaryFiles < 0 || metrics.BinaryFiles > metrics.ChangedFiles {
		return &CLIError{Code: "CHS-WORK-BUDGET-EVIDENCE", Message: "change metrics are incomplete or inconsistent", ExitCode: 40}
	}
	if budget.MaxChangedFiles > 0 && metrics.ChangedFiles > budget.MaxChangedFiles {
		return &CLIError{Code: "CHS-WORK-BUDGET-FILES", Message: fmt.Sprintf("change touches %d files; Task limit is %d", metrics.ChangedFiles, budget.MaxChangedFiles), ExitCode: 10, Remedy: []string{"split the change into a replacement Task or ask Master to accept a new Task budget"}}
	}
	if budget.MaxDiffLines > 0 && metrics.DiffLines > budget.MaxDiffLines {
		return &CLIError{Code: "CHS-WORK-BUDGET-LINES", Message: fmt.Sprintf("change has %d added/deleted lines; Task limit is %d", metrics.DiffLines, budget.MaxDiffLines), ExitCode: 10, Remedy: []string{"split the change into a replacement Task or ask Master to accept a new Task budget"}}
	}
	if budget.MaxCommits > 0 && metrics.Commits > budget.MaxCommits {
		return &CLIError{Code: "CHS-WORK-BUDGET-COMMITS", Message: fmt.Sprintf("change contains %d commits; Task limit is %d", metrics.Commits, budget.MaxCommits), ExitCode: 10, Remedy: []string{"create a clean replacement Task branch or ask Master to accept a new Task budget"}}
	}
	return nil
}

func validateState(config Config, state State) error {
	if config.Version != ConfigVersion {
		return &CLIError{Code: "CHS-SCHEMA-UNSUPPORTED", Message: "project schema is not supported by this CHASSISS version; initialize a new V2 project", ExitCode: 40}
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
	if state.Artifacts == nil || state.Missions == nil || state.Tasks == nil || state.Submissions == nil || state.Reviews == nil || state.Integrations == nil || state.Publications == nil || state.OwnerChanges == nil {
		return &CLIError{Code: "CHS-STATE-INVALID", Message: "state collections must not be null", ExitCode: 40}
	}
	if err := validateTaskBudgetDefinition(config.DefaultTaskBudget); err != nil {
		return stateError("CHS-STATE-CONFIG", "default Task budget is invalid")
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
		if err := validateTaskBudgetDefinition(task.Budget); err != nil {
			return stateError("CHS-STATE-TASK", "Task budget is invalid: "+id)
		}
		for _, check := range task.Checks {
			if err := validateCheckSpec(check); err != nil {
				return stateError("CHS-STATE-TASK", "Task acceptance check is invalid: "+id)
			}
			if err := validateIndependentVerification(task.AllowedPaths, check); err != nil {
				return stateError("CHS-STATE-TASK", "Task independent verification is invalid: "+id)
			}
		}
		mission, missionExists := state.Missions[task.MissionID]
		listed := missionExists && containsString(mission.TaskIDs, id)
		if task.ID != id || !missionExists || task.ArtifactID != id || (!listed && task.Status != "planned") {
			return stateError("CHS-STATE-TASK", "task identity or mission membership is invalid: "+id)
		}
		if _, ok := validTaskStatuses[task.Status]; !ok {
			return stateError("CHS-STATE-TASK", "unknown task status: "+task.Status)
		}
		if task.UpdatedAt.IsZero() || task.CheckResults == nil {
			return stateError("CHS-STATE-TASK", "task update data is incomplete: "+id)
		}
		checkSpecs := map[string]CheckSpec{}
		for _, check := range task.Checks {
			checkSpecs[check.ID] = check
		}
		for checkID, result := range task.CheckResults {
			spec, exists := checkSpecs[checkID]
			if !exists || result.ID != checkID || result.SpecDigest != checkSpecDigest(spec) || result.SnapshotDigest == "" || result.VerificationDigest == "" {
				return stateError("CHS-STATE-TASK", "Task check evidence is invalid: "+id)
			}
		}
		for _, dependency := range task.DependsOn {
			dep, ok := state.Tasks[dependency]
			if !ok || dep.MissionID != task.MissionID {
				return stateError("CHS-STATE-TASK", fmt.Sprintf("task %s has invalid dependency %s", id, dependency))
			}
			if task.Status == "ready" && !dependencySatisfied(state, dependency) {
				return stateError("CHS-STATE-TASK", fmt.Sprintf("ready task %s has unmet dependency %s", id, dependency))
			}
		}
		if containsString([]string{"planned", "ready"}, task.Status) && (task.Owner != "" || task.OwnerGrantID != "" || task.Branch != "" || task.Baseline != "" || task.WorktreePath != "" || task.WorktreeID != "" || task.WorktreeDigest != "") {
			return stateError("CHS-STATE-TASK", "unclaimed task contains ownership data: "+id)
		}
		needsOwner := containsString([]string{"claimed", "in_progress", "review_pending", "changes_requested", "approved", "integrated"}, task.Status)
		if task.Status == "blocked" && containsString([]string{"claimed", "in_progress", "review_pending", "changes_requested", "approved"}, task.PreviousStatus) {
			needsOwner = true
		}
		if needsOwner && (!validActor(task.Owner) || task.OwnerGrantID == "" || task.Branch == "" || task.Baseline == "") {
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
		if task.Status == "cancelled" && strings.TrimSpace(task.ClosureReason) == "" {
			return stateError("CHS-STATE-TASK", "cancelled task lacks a closure reason: "+id)
		}
		if task.Status == "superseded" {
			replacement, ok := state.Tasks[task.ReplacementID]
			if !ok || replacement.SupersedesID != task.ID || replacement.MissionID != task.MissionID {
				return stateError("CHS-STATE-TASK", "superseded task lacks a valid replacement: "+id)
			}
		} else if task.ReplacementID != "" {
			return stateError("CHS-STATE-TASK", "non-superseded task names a replacement: "+id)
		}
		if task.SupersedesID != "" {
			original, ok := state.Tasks[task.SupersedesID]
			if !ok || original.ReplacementID != task.ID || original.Status != "superseded" {
				return stateError("CHS-STATE-TASK", "replacement task has an invalid predecessor: "+id)
			}
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
		if !validChangedFiles(submission.ChangedFiles) {
			return stateError("CHS-STATE-SUBMISSION", "submission changed-file evidence is not canonical: "+id)
		}
		if _, ok := validSubmissionStatuses[submission.Status]; !ok {
			return stateError("CHS-STATE-SUBMISSION", "unknown submission status: "+submission.Status)
		}
		task, ok := state.Tasks[submission.TaskID]
		if !ok || task.Owner != submission.Actor || task.Baseline != submission.BaseCommit {
			return stateError("CHS-STATE-SUBMISSION", "submission does not match task ownership or baseline: "+id)
		}
		if submission.Metrics == nil {
			if taskBudgetEnabled(task.Budget) {
				return stateError("CHS-STATE-SUBMISSION", "budgeted submission lacks change metrics: "+id)
			}
		} else if submission.CommitMessage == "" || submission.Metrics.ChangedFiles != len(submission.ChangedFiles) || validateTaskBudget(task.Budget, *submission.Metrics) != nil {
			return stateError("CHS-STATE-SUBMISSION", "submission change metrics violate its Task budget: "+id)
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
		for _, spec := range task.Checks {
			result, ok := submission.Checks[spec.ID]
			if !ok || !result.Passed || result.SpecDigest != checkSpecDigest(spec) || result.SnapshotDigest == "" || result.VerificationDigest == "" {
				return stateError("CHS-STATE-SUBMISSION", "submission lacks independent check evidence: "+id)
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
		if err := validateReviewReport(review.Report); err != nil {
			return stateError("CHS-STATE-REVIEW", "review report is invalid: "+id)
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
			submissionResult := submission.Checks[spec.ID]
			if !ok || !result.Passed || result.SpecDigest != checkSpecDigest(spec) || result.SnapshotDigest != integration.IntegratedTree || result.VerificationDigest == "" || result.VerificationDigest != submissionResult.VerificationDigest {
				return stateError("CHS-STATE-INTEGRATION", "integration lacks merged-tree check evidence: "+id)
			}
		}
	}
	for id, publication := range state.Publications {
		if publication.ID != id || !containsString([]string{"github", "gitlab", "remote-git"}, publication.Target) || publication.Remote == "" || publication.RemoteURLDigest == "" || publication.Branch == "" || publication.Head == "" || publication.PublishedBy == "" || publication.CreatedAt.IsZero() {
			return stateError("CHS-STATE-PUBLICATION", "publication identity or evidence is incomplete: "+id)
		}
	}
	if len(state.OwnerChanges) == 0 {
		if state.LastOwnerChangeID != "" {
			return stateError("CHS-STATE-OWNER", "last Owner change points into an empty history")
		}
	} else {
		last, ok := state.OwnerChanges[state.LastOwnerChangeID]
		if !ok {
			return stateError("CHS-STATE-OWNER", "last Owner change does not exist")
		}
		for id, change := range state.OwnerChanges {
			if change.ID != id || !validActor(change.Actor) || change.CredentialID == "" || change.PreviousHead == "" || change.NewHead == "" ||
				change.PreviousHead == change.NewHead || change.TreeDigest == "" || change.CommitMessage == "" || change.CreatedAt.IsZero() {
				return stateError("CHS-STATE-OWNER", "Owner change identity or evidence is incomplete: "+id)
			}
			if err := validateOwnerReason(change.Reason); err != nil {
				return stateError("CHS-STATE-OWNER", "Owner change reason is invalid: "+id)
			}
			if !strings.HasPrefix(change.TreeDigest, "sha256:") || len(change.TreeDigest) != len("sha256:")+64 ||
				change.CommitMessage != ownerCommitMessage(change.Reason) || !validChangedFiles(change.ChangedFiles) ||
				change.Metrics.Commits != 1 || change.Metrics.ChangedFiles != len(change.ChangedFiles) ||
				validateTaskBudget(TaskBudget{}, change.Metrics) != nil {
				return stateError("CHS-STATE-OWNER", "Owner change evidence is inconsistent: "+id)
			}
			for _, file := range change.ChangedFiles {
				if ownerControlPath(file) {
					return stateError("CHS-STATE-OWNER", "Owner change modified control data: "+id)
				}
			}
			if change.CreatedAt.After(last.CreatedAt) {
				return stateError("CHS-STATE-OWNER", "last Owner change is not the newest history entry")
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
		for id, task := range state.Tasks {
			if task.MissionID != mission.ID {
				continue
			}
			if err := visit(id); err != nil {
				return err
			}
		}
	}
	for id := range state.Tasks {
		seen := map[string]bool{}
		current := id
		for current != "" {
			if seen[current] {
				return stateError("CHS-STATE-TASK-CYCLE", "task replacement graph contains a cycle at "+current)
			}
			seen[current] = true
			current = state.Tasks[current].ReplacementID
		}
	}
	return nil
}

func stateError(code, message string) error {
	return &CLIError{Code: code, Message: message, ExitCode: 40}
}
