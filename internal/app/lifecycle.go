package app

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

func activateMission(root, missionID string, principal Principal, expected int64) (State, State, error) {
	config, _, state, err := loadProject(root)
	if err != nil {
		return State{}, State{}, err
	}
	expected, err = effectiveExpected(state, expected)
	if err != nil {
		return State{}, State{}, err
	}
	if state.ActiveMission != "" {
		return State{}, State{}, &CLIError{Code: "CHS-MISSION-ACTIVE", Message: "another mission is already active", ExitCode: 10}
	}
	mission, ok := state.Missions[missionID]
	if !ok || state.Artifacts[missionID].Status != "accepted" || mission.Status != "planned" {
		return State{}, State{}, &CLIError{Code: "CHS-MISSION-NOT-READY", Message: "mission is not accepted and planned", ExitCode: 10}
	}
	if err := taskGraphIssues(mission, state.Tasks); err != nil {
		return State{}, State{}, err
	}
	for _, taskID := range mission.TaskIDs {
		if state.Artifacts[taskID].Status != "accepted" {
			return State{}, State{}, &CLIError{Code: "CHS-MISSION-TASK-NOT-ACCEPTED", Message: "mission task is not accepted: " + taskID, ExitCode: 10}
		}
	}
	previous, next, _, err := updateState(root, principal, "mission.activated", missionID, expected, func(next *State) error {
		mission := next.Missions[missionID]
		mission.Status = "active"
		mission.UpdatedAt = timeNow()
		next.Missions[missionID] = mission
		next.ActiveMission = missionID
		next.Phase = "execution"
		for _, taskID := range mission.TaskIDs {
			task := next.Tasks[taskID]
			if len(task.DependsOn) == 0 {
				task.Status = "ready"
			}
			task.UpdatedAt = timeNow()
			next.Tasks[taskID] = task
		}
		return nil
	})
	_ = config
	return previous, next, err
}

func missionBlock(root, missionID, reason string, principal Principal, expected int64) (State, State, MissionState, error) {
	_, _, state, err := loadProject(root)
	if err != nil {
		return State{}, State{}, MissionState{}, err
	}
	expected, err = effectiveExpected(state, expected)
	if err != nil {
		return State{}, State{}, MissionState{}, err
	}
	mission, ok := state.Missions[missionID]
	if !ok || state.ActiveMission != missionID || mission.Status != "active" {
		return State{}, State{}, MissionState{}, &CLIError{Code: "CHS-MISSION-NOT-ACTIVE", Message: "only the active mission can be blocked", ExitCode: 10}
	}
	if strings.TrimSpace(reason) == "" {
		return State{}, State{}, MissionState{}, &CLIError{Code: "CHS-MISSION-REASON", Message: "mission block requires a reason", ExitCode: 20}
	}
	mission.PreviousStatus = mission.Status
	mission.Status = "blocked"
	mission.BlockReason = reason
	mission.UpdatedAt = timeNow()
	previous, next, _, err := updateState(root, principal, "mission.blocked", missionID, expected, func(next *State) error {
		next.Missions[missionID] = mission
		return nil
	})
	return previous, next, mission, err
}

func missionResume(root, missionID string, principal Principal, expected int64) (State, State, MissionState, error) {
	_, _, state, err := loadProject(root)
	if err != nil {
		return State{}, State{}, MissionState{}, err
	}
	expected, err = effectiveExpected(state, expected)
	if err != nil {
		return State{}, State{}, MissionState{}, err
	}
	mission, ok := state.Missions[missionID]
	if !ok || state.ActiveMission != missionID || mission.Status != "blocked" || mission.PreviousStatus != "active" {
		return State{}, State{}, MissionState{}, &CLIError{Code: "CHS-MISSION-NOT-BLOCKED", Message: "mission is not resumable", ExitCode: 10}
	}
	mission.Status = mission.PreviousStatus
	mission.PreviousStatus = ""
	mission.BlockReason = ""
	mission.UpdatedAt = timeNow()
	previous, next, _, err := updateState(root, principal, "mission.resumed", missionID, expected, func(next *State) error {
		next.Missions[missionID] = mission
		return nil
	})
	return previous, next, mission, err
}

func activeWIP(state State) int {
	count := 0
	for _, task := range state.Tasks {
		if containsString([]string{"claimed", "in_progress", "review_pending", "changes_requested", "approved", "blocked"}, task.Status) {
			count++
		}
	}
	return count
}

func literalPatternRoot(pattern string) string {
	pattern = strings.TrimPrefix(filepath.ToSlash(pattern), "./")
	parts := strings.Split(pattern, "/")
	var roots []string
	for _, part := range parts {
		if strings.Contains(part, "*") {
			break
		}
		roots = append(roots, part)
	}
	return strings.Join(roots, "/")
}

func pathsOverlap(left, right []string) bool {
	for _, a := range left {
		for _, b := range right {
			aroot, broot := literalPatternRoot(a), literalPatternRoot(b)
			if a == b || matchAllowed(a, b) || matchAllowed(b, a) || aroot == "" || broot == "" || aroot == broot || strings.HasPrefix(aroot, broot+"/") || strings.HasPrefix(broot, aroot+"/") {
				return true
			}
		}
	}
	return false
}

func taskClaimOrAssign(root, taskID, owner string, principal Principal, expected int64, assign bool) (State, State, TaskState, error) {
	config, _, state, err := loadProject(root)
	if err != nil {
		return State{}, State{}, TaskState{}, err
	}
	expected, err = effectiveExpected(state, expected)
	if err != nil {
		return State{}, State{}, TaskState{}, err
	}
	task, ok := state.Tasks[taskID]
	if !ok || task.Status != "ready" || task.MissionID != state.ActiveMission {
		return State{}, State{}, TaskState{}, &CLIError{Code: "CHS-TASK-NOT-READY", Message: "task is not ready in the active mission", ExitCode: 10}
	}
	if activeWIP(state) >= config.WIPLimit {
		return State{}, State{}, TaskState{}, &CLIError{Code: "CHS-TASK-WIP", Message: "project WIP limit is reached", ExitCode: 10}
	}
	if owner == "" {
		owner = principal.Actor
	}
	for otherID, other := range state.Tasks {
		if otherID == taskID || !containsString([]string{"claimed", "in_progress", "review_pending", "changes_requested", "approved", "blocked"}, other.Status) {
			continue
		}
		if pathsOverlap(task.AllowedPaths, other.AllowedPaths) {
			return State{}, State{}, TaskState{}, &CLIError{Code: "CHS-TASK-PATH-CONFLICT", Message: fmt.Sprintf("task write scope overlaps active task %s", otherID), ExitCode: 10}
		}
	}
	baseline, err := gitHead(root)
	if err != nil {
		return State{}, State{}, TaskState{}, err
	}
	task.Owner = owner
	task.Branch = "chassiss/" + strings.ToLower(taskID)
	task.Baseline = baseline
	task.Status = "claimed"
	task.CheckResults = map[string]CheckResult{}
	task.UpdatedAt = timeNow()
	eventType := "task.claimed"
	if assign {
		eventType = "task.assigned"
	}
	previous, next, _, err := updateState(root, principal, eventType, taskID, expected, func(next *State) error {
		next.Tasks[taskID] = task
		return nil
	})
	return previous, next, task, err
}

func taskBlock(root, taskID, reason string, principal Principal, expected int64, eventType string) (State, State, TaskState, error) {
	_, _, state, err := loadProject(root)
	if err != nil {
		return State{}, State{}, TaskState{}, err
	}
	expected, err = effectiveExpected(state, expected)
	if err != nil {
		return State{}, State{}, TaskState{}, err
	}
	task, ok := state.Tasks[taskID]
	if !ok || containsString([]string{"integrated", "cancelled", "superseded"}, task.Status) {
		return State{}, State{}, TaskState{}, &CLIError{Code: "CHS-TASK-CLOSED", Message: "task cannot be blocked", ExitCode: 10}
	}
	if principal.Role == "developer" && task.Owner != principal.Actor {
		return State{}, State{}, TaskState{}, &CLIError{Code: "CHS-AUTH-TASK", Message: "developer can block only an assigned task", ExitCode: 11}
	}
	task.PreviousStatus = task.Status
	task.Status = "blocked"
	task.BlockReason = reason
	task.UpdatedAt = timeNow()
	previous, next, _, err := updateState(root, principal, eventType, taskID, expected, func(next *State) error {
		next.Tasks[taskID] = task
		return nil
	})
	return previous, next, task, err
}

func taskResume(root, taskID string, principal Principal, expected int64) (State, State, TaskState, error) {
	_, _, state, err := loadProject(root)
	if err != nil {
		return State{}, State{}, TaskState{}, err
	}
	expected, err = effectiveExpected(state, expected)
	if err != nil {
		return State{}, State{}, TaskState{}, err
	}
	task, ok := state.Tasks[taskID]
	if !ok || task.Status != "blocked" || task.PreviousStatus == "" {
		return State{}, State{}, TaskState{}, &CLIError{Code: "CHS-TASK-NOT-BLOCKED", Message: "task is not resumable", ExitCode: 10}
	}
	task.Status = task.PreviousStatus
	task.PreviousStatus = ""
	task.BlockReason = ""
	task.UpdatedAt = timeNow()
	previous, next, _, err := updateState(root, principal, "task.resumed", taskID, expected, func(next *State) error {
		next.Tasks[taskID] = task
		return nil
	})
	return previous, next, task, err
}

func workOpen(root, taskID string, principal Principal, expected int64) (State, State, TaskState, error) {
	config, _, state, err := loadProject(root)
	if err != nil {
		return State{}, State{}, TaskState{}, err
	}
	expected, err = effectiveExpected(state, expected)
	if err != nil {
		return State{}, State{}, TaskState{}, err
	}
	task, ok := state.Tasks[taskID]
	if !ok || task.Owner != principal.Actor || !containsString([]string{"claimed", "changes_requested"}, task.Status) {
		return State{}, State{}, TaskState{}, &CLIError{Code: "CHS-WORK-NOT-ASSIGNED", Message: "task is not assigned to this developer or cannot be opened", ExitCode: 11}
	}
	clean, status, err := gitClean(root)
	if err != nil {
		return State{}, State{}, TaskState{}, err
	}
	if !clean {
		return State{}, State{}, TaskState{}, &CLIError{Code: "CHS-WORK-DIRTY", Message: "worktree must be clean before opening task: " + status, ExitCode: 10}
	}
	if _, err := git(root, "show-ref", "--verify", "--quiet", "refs/heads/"+task.Branch); err == nil {
		if _, err := git(root, "checkout", task.Branch); err != nil {
			return State{}, State{}, TaskState{}, err
		}
	} else {
		if _, err := git(root, "checkout", "-b", task.Branch, task.Baseline); err != nil {
			return State{}, State{}, TaskState{}, err
		}
	}
	task.Status = "in_progress"
	task.UpdatedAt = timeNow()
	previous, next, _, err := updateState(root, principal, "work.opened", taskID, expected, func(next *State) error {
		next.Tasks[taskID] = task
		return nil
	})
	_ = config
	return previous, next, task, err
}

func runTaskCheck(root, taskID, checkID string, all bool, principal Principal, expected int64) (State, State, []CheckResult, error) {
	_, _, state, err := loadProject(root)
	if err != nil {
		return State{}, State{}, nil, err
	}
	expected, err = effectiveExpected(state, expected)
	if err != nil {
		return State{}, State{}, nil, err
	}
	task, ok := state.Tasks[taskID]
	if !ok || task.Owner != principal.Actor || task.Status != "in_progress" {
		return State{}, State{}, nil, &CLIError{Code: "CHS-WORK-NOT-ACTIVE", Message: "task is not active for this developer", ExitCode: 11}
	}
	var selected []CheckSpec
	for _, check := range task.Checks {
		if all || check.ID == checkID {
			selected = append(selected, check)
		}
	}
	if len(selected) == 0 {
		return State{}, State{}, nil, &CLIError{Code: "CHS-WORK-CHECK-NOT-FOUND", Message: "requested acceptance check does not exist", ExitCode: 20}
	}
	results := make([]CheckResult, 0, len(selected))
	for _, check := range selected {
		arguments := strings.Fields(check.Command)
		if len(arguments) == 0 {
			return State{}, State{}, nil, &CLIError{Code: "CHS-WORK-CHECK-INVALID", Message: "acceptance check command is empty", ExitCode: 10}
		}
		output, runErr := runCommand(root, arguments[0], arguments[1:]...)
		result := CheckResult{ID: check.ID, Command: check.Command, Passed: runErr == nil, CheckedAt: timeNow(), Output: output}
		if runErr != nil {
			result.ExitCode = 1
			result.Output = runErr.Error()
		}
		if len(result.Output) > 4000 {
			result.Output = result.Output[len(result.Output)-4000:]
		}
		results = append(results, result)
	}
	snapshotDigest, err := gitWorktreeDigest(root)
	if err != nil {
		return State{}, State{}, nil, err
	}
	for index := range results {
		results[index].SnapshotDigest = snapshotDigest
	}
	previous, next, _, err := updateState(root, principal, "work.checked", taskID, expected, func(next *State) error {
		task := next.Tasks[taskID]
		if task.CheckResults == nil {
			task.CheckResults = map[string]CheckResult{}
		}
		for _, result := range results {
			task.CheckResults[result.ID] = result
		}
		task.UpdatedAt = timeNow()
		next.Tasks[taskID] = task
		return nil
	})
	return previous, next, results, err
}

func workCheckpoint(root, taskID, checkpoint string, principal Principal, expected int64) (State, State, TaskState, error) {
	_, _, state, err := loadProject(root)
	if err != nil {
		return State{}, State{}, TaskState{}, err
	}
	expected, err = effectiveExpected(state, expected)
	if err != nil {
		return State{}, State{}, TaskState{}, err
	}
	task, ok := state.Tasks[taskID]
	if !ok || task.Owner != principal.Actor || task.Status != "in_progress" {
		return State{}, State{}, TaskState{}, &CLIError{Code: "CHS-WORK-NOT-ACTIVE", Message: "task is not active for this developer", ExitCode: 11}
	}
	task.Checkpoint = checkpoint
	task.UpdatedAt = timeNow()
	previous, next, _, err := updateState(root, principal, "work.checkpointed", taskID, expected, func(next *State) error {
		next.Tasks[taskID] = task
		return nil
	})
	return previous, next, task, err
}

func workSubmit(root, taskID, handoff string, principal Principal, expected int64) (State, State, Submission, error) {
	_, _, state, err := loadProject(root)
	if err != nil {
		return State{}, State{}, Submission{}, err
	}
	expected, err = effectiveExpected(state, expected)
	if err != nil {
		return State{}, State{}, Submission{}, err
	}
	task, ok := state.Tasks[taskID]
	if !ok || task.Owner != principal.Actor || task.Status != "in_progress" {
		return State{}, State{}, Submission{}, &CLIError{Code: "CHS-WORK-NOT-ACTIVE", Message: "task is not active for this developer", ExitCode: 11}
	}
	branch, err := currentBranch(root)
	if err != nil || branch != task.Branch {
		return State{}, State{}, Submission{}, &CLIError{Code: "CHS-WORK-BRANCH", Message: "current branch is not the task branch", ExitCode: 10}
	}
	files, err := gitWorkingFiles(root)
	if err != nil {
		return State{}, State{}, Submission{}, err
	}
	if len(files) == 0 {
		return State{}, State{}, Submission{}, &CLIError{Code: "CHS-WORK-NO-CHANGES", Message: "task has no content changes to submit", ExitCode: 10}
	}
	for _, file := range files {
		if !allowedFile(task.AllowedPaths, file) {
			return State{}, State{}, Submission{}, &CLIError{Code: "CHS-WORK-SCOPE", Message: "changed file is outside allowed_paths: " + file, ExitCode: 10}
		}
	}
	snapshotDigest, err := gitWorktreeDigest(root)
	if err != nil {
		return State{}, State{}, Submission{}, err
	}
	if err := validateTaskChecks(task, snapshotDigest); err != nil {
		return State{}, State{}, Submission{}, err
	}
	head, err := gitCommit(root, "Complete "+taskID, files...)
	if err != nil {
		return State{}, State{}, Submission{}, err
	}
	changed, err := gitChangedFiles(root, task.Baseline, head)
	if err != nil {
		return State{}, State{}, Submission{}, err
	}
	id, err := newID("SUB")
	if err != nil {
		return State{}, State{}, Submission{}, err
	}
	submission := Submission{
		ID: id, TaskID: taskID, Actor: principal.Actor, BaseCommit: task.Baseline, HeadCommit: head,
		ChangedFiles: changed, Checks: task.CheckResults, Handoff: handoff, Status: "review_pending", CreatedAt: timeNow(),
	}
	submission.Digest, err = calculateSubmissionDigest(submission)
	if err != nil {
		return State{}, State{}, Submission{}, err
	}
	previous, next, _, err := updateState(root, principal, "work.submitted", taskID, expected, func(next *State) error {
		task := next.Tasks[taskID]
		task.Status = "review_pending"
		task.SubmissionID = id
		task.UpdatedAt = timeNow()
		next.Tasks[taskID] = task
		next.Submissions[id] = submission
		return nil
	})
	return previous, next, submission, err
}

func validateTaskChecks(task TaskState, snapshotDigest string) error {
	for _, check := range task.Checks {
		result, ok := task.CheckResults[check.ID]
		if !ok || !result.Passed {
			return &CLIError{Code: "CHS-WORK-CHECKS", Message: "required check has not passed: " + check.ID, ExitCode: 10}
		}
		if result.SnapshotDigest == "" || result.SnapshotDigest != snapshotDigest {
			return &CLIError{Code: "CHS-WORK-CHECKS-STALE", Message: "files changed after required check: " + check.ID, ExitCode: 10, Remedy: []string{"rerun chassiss work check after the final content change"}}
		}
	}
	return nil
}

func reviewCheck(root, submissionID string) (Submission, TaskState, []string, error) {
	_, _, state, err := loadProject(root)
	if err != nil {
		return Submission{}, TaskState{}, nil, err
	}
	submission, ok := state.Submissions[submissionID]
	if !ok {
		return Submission{}, TaskState{}, nil, &CLIError{Code: "CHS-REVIEW-NOT-FOUND", Message: "submission not found", ExitCode: 10}
	}
	digest, err := calculateSubmissionDigest(submission)
	if err != nil {
		return Submission{}, TaskState{}, nil, err
	}
	if digest != submission.Digest {
		return Submission{}, TaskState{}, nil, &CLIError{Code: "CHS-REVIEW-DIGEST", Message: "submission manifest no longer matches its digest", ExitCode: 40}
	}
	task := state.Tasks[submission.TaskID]
	files, err := gitChangedFiles(root, submission.BaseCommit, submission.HeadCommit)
	if err != nil {
		return Submission{}, TaskState{}, nil, err
	}
	if strings.Join(files, "\x00") != strings.Join(submission.ChangedFiles, "\x00") {
		return Submission{}, TaskState{}, nil, &CLIError{Code: "CHS-REVIEW-DIFF", Message: "submission diff no longer matches its manifest", ExitCode: 40}
	}
	for _, file := range files {
		if !allowedFile(task.AllowedPaths, file) {
			return Submission{}, TaskState{}, nil, &CLIError{Code: "CHS-REVIEW-SCOPE", Message: "submission contains an out-of-scope file: " + file, ExitCode: 10}
		}
	}
	for _, check := range task.Checks {
		if result, ok := submission.Checks[check.ID]; !ok || !result.Passed {
			return Submission{}, TaskState{}, nil, &CLIError{Code: "CHS-REVIEW-CHECKS", Message: "submission lacks passed check: " + check.ID, ExitCode: 10}
		}
	}
	return submission, task, files, nil
}

func calculateSubmissionDigest(submission Submission) (string, error) {
	manifest := struct {
		ID           string                 `json:"id"`
		TaskID       string                 `json:"task_id"`
		Actor        string                 `json:"actor"`
		BaseCommit   string                 `json:"base_commit"`
		HeadCommit   string                 `json:"head_commit"`
		ChangedFiles []string               `json:"changed_files"`
		Checks       map[string]CheckResult `json:"checks"`
		Handoff      string                 `json:"handoff"`
		CreatedAt    time.Time              `json:"created_at"`
	}{
		ID: submission.ID, TaskID: submission.TaskID, Actor: submission.Actor,
		BaseCommit: submission.BaseCommit, HeadCommit: submission.HeadCommit,
		ChangedFiles: submission.ChangedFiles, Checks: submission.Checks,
		Handoff: submission.Handoff, CreatedAt: submission.CreatedAt,
	}
	data, err := canonicalJSON(manifest)
	if err != nil {
		return "", err
	}
	return digestBytes(data), nil
}

func recordReview(root, submissionID, verdict, report string, principal Principal, expected int64) (State, State, Review, error) {
	submission, _, _, err := reviewCheck(root, submissionID)
	if err != nil {
		return State{}, State{}, Review{}, err
	}
	_, _, state, err := loadProject(root)
	if err != nil {
		return State{}, State{}, Review{}, err
	}
	expected, err = effectiveExpected(state, expected)
	if err != nil {
		return State{}, State{}, Review{}, err
	}
	if submission.Actor == principal.Actor {
		return State{}, State{}, Review{}, &CLIError{Code: "CHS-REVIEW-INDEPENDENCE", Message: "reviewer cannot review their own submission", ExitCode: 11}
	}
	if submission.Status != "review_pending" {
		return State{}, State{}, Review{}, &CLIError{Code: "CHS-REVIEW-STATE", Message: "submission is not pending review", ExitCode: 10}
	}
	id, err := newID("REV")
	if err != nil {
		return State{}, State{}, Review{}, err
	}
	review := Review{ID: id, SubmissionID: submissionID, SubmissionDigest: submission.Digest, Reviewer: principal.Actor, Verdict: verdict, Report: report, CreatedAt: timeNow()}
	eventType := "review.approved"
	if verdict == "request_changes" {
		eventType = "review.changes_requested"
	}
	previous, next, _, err := updateState(root, principal, eventType, submissionID, expected, func(next *State) error {
		next.Reviews[id] = review
		submission := next.Submissions[submissionID]
		submission.ReviewID = id
		task := next.Tasks[submission.TaskID]
		if verdict == "approve" {
			submission.Status = "approved"
			task.Status = "approved"
		} else {
			submission.Status = "changes_requested"
			task.Status = "changes_requested"
		}
		next.Submissions[submissionID] = submission
		task.UpdatedAt = timeNow()
		next.Tasks[task.ID] = task
		return nil
	})
	return previous, next, review, err
}

func integrateSubmission(root, submissionID string, principal Principal, expected int64) (State, State, Integration, error) {
	config, _, state, err := loadProject(root)
	if err != nil {
		return State{}, State{}, Integration{}, err
	}
	expected, err = effectiveExpected(state, expected)
	if err != nil {
		return State{}, State{}, Integration{}, err
	}
	submission, ok := state.Submissions[submissionID]
	if !ok || submission.Status != "approved" {
		return State{}, State{}, Integration{}, &CLIError{Code: "CHS-INTEGRATION-NOT-APPROVED", Message: "submission is not approved", ExitCode: 10}
	}
	task := state.Tasks[submission.TaskID]
	review := state.Reviews[submission.ReviewID]
	if review.SubmissionDigest != submission.Digest || review.Verdict != "approve" || review.Reviewer != principal.Actor {
		return State{}, State{}, Integration{}, &CLIError{Code: "CHS-INTEGRATION-REVIEW", Message: "integration must be performed by the approving Reviewer against the same digest", ExitCode: 11}
	}
	clean, status, err := gitClean(root)
	if err != nil {
		return State{}, State{}, Integration{}, err
	}
	if !clean {
		return State{}, State{}, Integration{}, &CLIError{Code: "CHS-INTEGRATION-DIRTY", Message: "worktree must be clean before integration: " + status, ExitCode: 10}
	}
	if _, err := git(root, "checkout", config.DefaultBranch); err != nil {
		return State{}, State{}, Integration{}, err
	}
	previousHead, err := gitHead(root)
	if err != nil {
		return State{}, State{}, Integration{}, err
	}
	mergeArgs := []string{"-c", "user.name=CHASSISS Reviewer", "-c", "user.email=reviewer@chassiss.local", "merge", "--no-ff", "-m", "Integrate " + task.ID, task.Branch}
	if _, err := git(root, mergeArgs...); err != nil {
		_, _ = git(root, "merge", "--abort")
		return State{}, State{}, Integration{}, &CLIError{Code: "CHS-INTEGRATION-CONFLICT", Message: "local integration failed: " + err.Error(), ExitCode: 12, Retryable: false}
	}
	newHead, err := gitHead(root)
	if err != nil {
		return State{}, State{}, Integration{}, err
	}
	id, err := newID("INT")
	if err != nil {
		return State{}, State{}, Integration{}, err
	}
	integration := Integration{ID: id, SubmissionID: submissionID, PreviousHead: previousHead, IntegratedHead: newHead, IntegratedBy: principal.Actor, CreatedAt: timeNow()}
	previous, next, _, err := updateState(root, principal, "integration.applied", submissionID, expected, func(next *State) error {
		next.Integrations[id] = integration
		submission := next.Submissions[submissionID]
		submission.Status = "integrated"
		submission.IntegrationID = id
		next.Submissions[submissionID] = submission
		task := next.Tasks[submission.TaskID]
		task.Status = "integrated"
		task.UpdatedAt = timeNow()
		next.Tasks[task.ID] = task
		next.Baseline = newHead
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
				other.UpdatedAt = timeNow()
				next.Tasks[otherID] = other
			}
		}
		return nil
	})
	return previous, next, integration, err
}

func submitMissionAcceptance(root, missionID, evidence string, principal Principal, expected int64) (State, State, MissionState, error) {
	_, _, state, err := loadProject(root)
	if err != nil {
		return State{}, State{}, MissionState{}, err
	}
	expected, err = effectiveExpected(state, expected)
	if err != nil {
		return State{}, State{}, MissionState{}, err
	}
	mission, ok := state.Missions[missionID]
	if !ok || state.ActiveMission != missionID || mission.Status != "active" {
		return State{}, State{}, MissionState{}, &CLIError{Code: "CHS-MISSION-NOT-ACTIVE", Message: "mission is not active", ExitCode: 10}
	}
	for _, taskID := range mission.TaskIDs {
		if state.Tasks[taskID].Status != "integrated" {
			return State{}, State{}, MissionState{}, &CLIError{Code: "CHS-MISSION-INCOMPLETE", Message: "mission task is not integrated: " + taskID, ExitCode: 10}
		}
	}
	mission.Status = "acceptance_pending"
	mission.AcceptanceEvidence = evidence
	mission.UpdatedAt = timeNow()
	previous, next, _, err := updateState(root, principal, "mission.acceptance_submitted", missionID, expected, func(next *State) error {
		next.Missions[missionID] = mission
		return nil
	})
	return previous, next, mission, err
}

func acceptMission(root, missionID string, principal Principal, expected int64) (State, State, MissionState, error) {
	_, _, state, err := loadProject(root)
	if err != nil {
		return State{}, State{}, MissionState{}, err
	}
	expected, err = effectiveExpected(state, expected)
	if err != nil {
		return State{}, State{}, MissionState{}, err
	}
	mission, ok := state.Missions[missionID]
	if !ok || state.ActiveMission != missionID || mission.Status != "acceptance_pending" || mission.AcceptanceEvidence == "" {
		return State{}, State{}, MissionState{}, &CLIError{Code: "CHS-MISSION-NOT-PENDING", Message: "mission acceptance is not pending", ExitCode: 10}
	}
	mission.Status = "completed"
	mission.UpdatedAt = timeNow()
	previous, next, _, err := updateState(root, principal, "mission.completed", missionID, expected, func(next *State) error {
		next.Missions[missionID] = mission
		next.ActiveMission = ""
		next.Phase = "idle"
		return nil
	})
	return previous, next, mission, err
}

func readTextFile(root, path string) (string, error) {
	absolute, err := pathWithin(root, path)
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(absolute)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}

func stateSummary(state State) map[string]any {
	ready, active, blocked, review := []string{}, []string{}, []string{}, []string{}
	for _, id := range sortedTaskIDs(state.Tasks) {
		task := state.Tasks[id]
		switch task.Status {
		case "ready":
			ready = append(ready, id)
		case "claimed", "in_progress", "changes_requested", "approved":
			active = append(active, id)
		case "blocked":
			blocked = append(blocked, id)
		case "review_pending":
			review = append(review, id)
		}
	}
	return map[string]any{
		"phase": state.Phase, "revision": state.Revision, "baseline": state.Baseline, "active_mission": state.ActiveMission,
		"ready_tasks": ready, "active_tasks": active, "blocked_tasks": blocked, "review_tasks": review,
	}
}

func nextActions(state State, role, actor string) []string {
	actions := []string{}
	switch role {
	case "designer":
		if state.Artifacts["requirements"].Status == "" {
			actions = append(actions, "template.get requirements")
		} else if state.Artifacts["requirements"].Status == "accepted" && state.Artifacts["architecture"].Status == "" {
			actions = append(actions, "template.get architecture")
		} else if state.ActiveMission == "" {
			actions = append(actions, "artifact.submit mission-or-task")
		}
	case "orchestrator":
		if state.ActiveMission == "" {
			for id, mission := range state.Missions {
				if mission.Status == "planned" && state.Artifacts[id].Status == "accepted" {
					actions = append(actions, "mission.activate "+id)
				}
			}
		} else {
			mission := state.Missions[state.ActiveMission]
			if mission.Status == "blocked" {
				actions = append(actions, "mission.resume "+mission.ID)
			} else {
				allIntegrated := true
				for _, id := range mission.TaskIDs {
					task := state.Tasks[id]
					if task.Status != "integrated" {
						allIntegrated = false
					}
					if task.Status == "ready" {
						actions = append(actions, "task.claim "+id, "task.assign "+id)
					}
				}
				if allIntegrated && mission.Status == "active" {
					actions = append(actions, "mission.submit-acceptance "+mission.ID)
				}
			}
		}
	case "developer":
		for _, id := range sortedTaskIDs(state.Tasks) {
			task := state.Tasks[id]
			if task.Owner != actor {
				continue
			}
			switch task.Status {
			case "claimed", "changes_requested":
				actions = append(actions, "work.open "+id)
			case "in_progress":
				actions = append(actions, "work.check "+id)
				checksPassed := true
				for _, check := range task.Checks {
					if result, ok := task.CheckResults[check.ID]; !ok || !result.Passed || result.SnapshotDigest == "" {
						checksPassed = false
						break
					}
				}
				if checksPassed {
					actions = append(actions, "work.submit "+id)
				}
			}
		}
	case "reviewer":
		for id, submission := range state.Submissions {
			if submission.Actor == actor {
				continue
			}
			if submission.Status == "review_pending" {
				actions = append(actions, "review.check "+id, "review.approve "+id, "review.request-changes "+id)
			} else if submission.Status == "approved" {
				actions = append(actions, "integrate.apply "+id)
			}
		}
	case "master":
		for _, id := range sortedArtifactIDs(state.Artifacts) {
			if state.Artifacts[id].Status == "submitted" {
				actions = append(actions, "artifact.accept "+state.Artifacts[id].SubmissionID, "artifact.reject "+state.Artifacts[id].SubmissionID)
			}
		}
		if state.ActiveMission != "" && state.Missions[state.ActiveMission].Status == "acceptance_pending" {
			actions = append(actions, "mission.accept "+state.ActiveMission)
		}
	}
	sort.Strings(actions)
	return actions
}

func trimOutput(value string) string {
	value = strings.TrimSpace(value)
	if len(value) > 4000 {
		return value[len(value)-4000:]
	}
	return value
}
