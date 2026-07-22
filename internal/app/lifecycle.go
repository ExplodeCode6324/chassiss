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
			if taskDependenciesSatisfied(*next, task) {
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
		if isActiveTaskStatus(task.Status) {
			count++
		}
	}
	return count
}

func dependencySatisfied(state State, taskID string) bool {
	seen := map[string]bool{}
	for taskID != "" && !seen[taskID] {
		seen[taskID] = true
		task, ok := state.Tasks[taskID]
		if !ok {
			return false
		}
		switch task.Status {
		case "integrated", "cancelled":
			return true
		case "superseded":
			taskID = task.ReplacementID
		default:
			return false
		}
	}
	return false
}

func taskDependenciesSatisfied(state State, task TaskState) bool {
	for _, dependency := range task.DependsOn {
		if !dependencySatisfied(state, dependency) {
			return false
		}
	}
	return true
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
	config, trust, state, err := loadProject(root)
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
	if err := requireMissionExecutable(state, task.MissionID); err != nil {
		return State{}, State{}, TaskState{}, err
	}
	if activeWIP(state) >= config.WIPLimit {
		return State{}, State{}, TaskState{}, &CLIError{Code: "CHS-TASK-WIP", Message: "project WIP limit is reached", ExitCode: 10}
	}
	if owner == "" {
		owner = principal.Actor
	}
	ownerGrant, ok := activeDeveloperGrant(trust, owner, timeNow())
	if !ok {
		return State{}, State{}, TaskState{}, &CLIError{Code: "CHS-TASK-OWNER-AUTH", Message: "task owner has no active Developer grant", ExitCode: 11, Remedy: []string{"issue an active Developer credential for the owner actor", "assign the Task to a different Developer actor"}}
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
	task.OwnerGrantID = ownerGrant.ID
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
	config, _, state, err := loadProject(root)
	if err != nil {
		return State{}, State{}, TaskState{}, err
	}
	expected, err = effectiveExpected(state, expected)
	if err != nil {
		return State{}, State{}, TaskState{}, err
	}
	var task TaskState
	previous, next, _, err := updateState(root, principal, "task.resumed", taskID, expected, func(next *State) error {
		if err := validateTaskResumeState(config, *next, taskID); err != nil {
			return err
		}
		if err := validateTaskResumeGit(root, *next, taskID); err != nil {
			return err
		}
		task = next.Tasks[taskID]
		task.Status = task.PreviousStatus
		task.PreviousStatus = ""
		task.BlockReason = ""
		task.UpdatedAt = timeNow()
		next.Tasks[taskID] = task
		return nil
	})
	return previous, next, task, err
}

func validateTaskResumeState(config Config, state State, taskID string) error {
	task, ok := state.Tasks[taskID]
	if !ok || task.Status != "blocked" || task.PreviousStatus == "" {
		return &CLIError{Code: "CHS-TASK-NOT-BLOCKED", Message: "task is not resumable", ExitCode: 10}
	}
	if err := requireMissionExecutable(state, task.MissionID); err != nil {
		return err
	}
	target := task.PreviousStatus
	if !containsString([]string{"planned", "ready", "claimed", "in_progress", "review_pending", "changes_requested", "approved"}, target) {
		return &CLIError{Code: "CHS-TASK-RESUME-STATE", Message: "task previous status is not safely resumable", ExitCode: 10}
	}
	if target != "planned" {
		for _, dependency := range task.DependsOn {
			if !dependencySatisfied(state, dependency) {
				return &CLIError{Code: "CHS-TASK-RESUME-DEPENDENCY", Message: "task dependency is no longer integrated: " + dependency, ExitCode: 10}
			}
		}
	}
	if target == "ready" || isActiveTaskStatus(target) {
		for otherID, other := range state.Tasks {
			if otherID != taskID && isActiveTaskStatus(other.Status) && pathsOverlap(task.AllowedPaths, other.AllowedPaths) {
				return &CLIError{Code: "CHS-TASK-RESUME-PATH-CONFLICT", Message: "task write scope now overlaps active task " + otherID, ExitCode: 10}
			}
		}
	}
	if isActiveTaskStatus(target) && activeWIP(state) >= config.WIPLimit {
		return &CLIError{Code: "CHS-TASK-RESUME-WIP", Message: "project WIP limit is full", ExitCode: 10}
	}
	if containsString([]string{"review_pending", "changes_requested", "approved"}, target) {
		submission, ok := state.Submissions[task.SubmissionID]
		if !ok || submission.TaskID != taskID || submission.Status != target {
			return &CLIError{Code: "CHS-TASK-RESUME-EVIDENCE", Message: "submission state no longer matches the blocked Task", ExitCode: 10}
		}
		digest, err := calculateSubmissionDigest(submission)
		if err != nil || digest != submission.Digest {
			return &CLIError{Code: "CHS-TASK-RESUME-EVIDENCE", Message: "submission digest is no longer valid", ExitCode: 10}
		}
		if target == "changes_requested" {
			review, ok := state.Reviews[submission.ReviewID]
			if !ok || review.Verdict != "request_changes" || review.SubmissionDigest != submission.Digest {
				return &CLIError{Code: "CHS-TASK-RESUME-EVIDENCE", Message: "change-request evidence is no longer valid", ExitCode: 10}
			}
		}
		if target == "approved" {
			review, ok := state.Reviews[submission.ReviewID]
			if !ok || review.Verdict != "approve" || review.SubmissionDigest != submission.Digest {
				return &CLIError{Code: "CHS-TASK-RESUME-EVIDENCE", Message: "approval evidence is no longer valid", ExitCode: 10}
			}
		}
	}
	return nil
}

func validateTaskResumeGit(root string, state State, taskID string) error {
	task := state.Tasks[taskID]
	target := task.PreviousStatus
	if !isActiveTaskStatus(target) {
		return nil
	}
	if _, err := git(root, "cat-file", "-e", task.Baseline+"^{commit}"); err != nil {
		return &CLIError{Code: "CHS-TASK-RESUME-BASELINE", Message: "task baseline commit is unavailable", ExitCode: 10}
	}
	branchHead, branchErr := git(root, "rev-parse", "--verify", "refs/heads/"+task.Branch)
	if target == "claimed" && branchErr != nil {
		return nil
	}
	if branchErr != nil {
		return &CLIError{Code: "CHS-TASK-RESUME-BRANCH", Message: "task branch is unavailable", ExitCode: 10}
	}
	if _, err := git(root, "merge-base", "--is-ancestor", task.Baseline, branchHead); err != nil {
		return &CLIError{Code: "CHS-TASK-RESUME-BASELINE", Message: "task branch no longer descends from its baseline", ExitCode: 10}
	}
	if target != "claimed" {
		worktreeRoot, err := taskWorktreeRoot(root, task)
		if err != nil {
			return err
		}
		worktreeHead, err := gitHead(worktreeRoot)
		if err != nil || worktreeHead != branchHead {
			return &CLIError{Code: "CHS-TASK-RESUME-BRANCH", Message: "task worktree and branch heads differ", ExitCode: 10}
		}
	}
	if containsString([]string{"review_pending", "changes_requested", "approved"}, target) {
		submission, _, _, err := reviewCheckState(root, state, task.SubmissionID)
		if err != nil || submission.HeadCommit != branchHead {
			return &CLIError{Code: "CHS-TASK-RESUME-EVIDENCE", Message: "submission or reviewed branch head is no longer valid", ExitCode: 10}
		}
	}
	return nil
}

func taskRelease(root, taskID string, principal Principal, expected int64) (State, State, TaskState, error) {
	_, _, state, err := loadProject(root)
	if err != nil {
		return State{}, State{}, TaskState{}, err
	}
	expected, err = effectiveExpected(state, expected)
	if err != nil {
		return State{}, State{}, TaskState{}, err
	}
	task, ok := state.Tasks[taskID]
	if !ok {
		return State{}, State{}, TaskState{}, &CLIError{Code: "CHS-TASK-NOT-FOUND", Message: "task not found", ExitCode: 10}
	}
	intent := taskReleaseOperationIntent{TaskID: taskID, Branch: task.Branch, Baseline: task.Baseline, WorktreePath: task.WorktreePath}
	previous, next, _, err := executeGitOperation(root, "task.release", "task.released", taskID, principal, expected, intent, func(current State) (preparedOperation, error) {
		currentTask, ok := current.Tasks[taskID]
		resumableBlocked := currentTask.Status == "blocked" && containsString([]string{"claimed", "in_progress"}, currentTask.PreviousStatus)
		if !ok || (!containsString([]string{"claimed", "in_progress"}, currentTask.Status) && !resumableBlocked) || currentTask.SubmissionID != "" {
			return preparedOperation{}, &CLIError{Code: "CHS-TASK-RELEASE-STATE", Message: "only an unsubmitted claimed or in-progress Task can be released", ExitCode: 10}
		}
		if err := requireMissionExecutable(current, currentTask.MissionID); err != nil {
			return preparedOperation{}, err
		}
		for _, submission := range current.Submissions {
			if submission.TaskID == taskID {
				return preparedOperation{}, &CLIError{Code: "CHS-TASK-RELEASE-SUBMISSION", Message: "Task with a submission cannot be released", ExitCode: 10}
			}
		}
		branchHead, branchErr := git(root, "rev-parse", "--verify", "refs/heads/"+currentTask.Branch)
		if branchErr == nil && branchHead != currentTask.Baseline {
			return preparedOperation{}, &CLIError{Code: "CHS-TASK-RELEASE-CHANGES", Message: "Task branch contains commits beyond its baseline", ExitCode: 10, Remedy: []string{"submit the Task or preserve it as blocked"}}
		}
		if branchErr != nil && currentTask.WorktreePath != "" {
			return preparedOperation{}, &CLIError{Code: "CHS-TASK-RELEASE-BRANCH", Message: "bound Task branch is missing", ExitCode: 40}
		}
		if currentTask.WorktreePath != "" {
			worktreeRoot, err := taskWorktreeRoot(root, currentTask)
			if err != nil {
				return preparedOperation{}, err
			}
			clean, status, err := gitClean(worktreeRoot)
			if err != nil {
				return preparedOperation{}, err
			}
			head, err := gitHead(worktreeRoot)
			if err != nil || !clean || head != currentTask.Baseline {
				return preparedOperation{}, &CLIError{Code: "CHS-TASK-RELEASE-CHANGES", Message: "Task worktree must be clean at its baseline before release: " + status, ExitCode: 10, Remedy: []string{"submit the Task or preserve it as blocked"}}
			}
		}
		after, err := captureGitOperationState(root)
		if err != nil {
			return preparedOperation{}, err
		}
		after.WorktreePath = currentTask.WorktreePath
		currentIntent := taskReleaseOperationIntent{TaskID: taskID, Branch: currentTask.Branch, Baseline: currentTask.Baseline, WorktreePath: currentTask.WorktreePath}
		return preparedOperation{
			Payload:  taskPayload{TaskID: taskID},
			GitAfter: after,
			ApplyGit: func() error {
				if currentTask.WorktreePath == "" {
					return nil
				}
				worktreePath, err := pathWithin(root, currentTask.WorktreePath)
				if err != nil {
					return err
				}
				_, err = git(root, "worktree", "remove", worktreePath)
				return err
			},
			Finalize: func() error { return cleanupReleasedTaskBranch(root, currentIntent) },
		}, nil
	})
	if err == nil {
		task = next.Tasks[taskID]
	}
	return previous, next, task, err
}

func taskCancel(root, taskID, reason string, principal Principal, expected int64) (State, State, TaskState, error) {
	_, _, state, err := loadProject(root)
	if err != nil {
		return State{}, State{}, TaskState{}, err
	}
	expected, err = effectiveExpected(state, expected)
	if err != nil {
		return State{}, State{}, TaskState{}, err
	}
	task, ok := state.Tasks[taskID]
	if !ok || isClosedTaskStatus(task.Status) || strings.TrimSpace(reason) == "" {
		return State{}, State{}, TaskState{}, &CLIError{Code: "CHS-TASK-CANCEL-STATE", Message: "Task is not cancellable or lacks a reason", ExitCode: 10}
	}
	task.Status = "cancelled"
	task.ClosureReason = reason
	task.BlockReason, task.PreviousStatus = "", ""
	task.UpdatedAt = timeNow()
	previous, next, _, err := updateState(root, principal, "task.cancelled", taskID, expected, func(next *State) error {
		next.Tasks[taskID] = task
		for otherID, other := range next.Tasks {
			if other.Status == "planned" && other.MissionID == next.ActiveMission && containsString(next.Missions[other.MissionID].TaskIDs, otherID) && taskDependenciesSatisfied(*next, other) {
				other.Status = "ready"
				other.UpdatedAt = timeNow()
				next.Tasks[otherID] = other
			}
		}
		return nil
	})
	return previous, next, task, err
}

func taskSupersede(root, taskID, replacementID string, principal Principal, expected int64) (State, State, TaskState, error) {
	_, _, state, err := loadProject(root)
	if err != nil {
		return State{}, State{}, TaskState{}, err
	}
	expected, err = effectiveExpected(state, expected)
	if err != nil {
		return State{}, State{}, TaskState{}, err
	}
	task, ok := state.Tasks[taskID]
	replacement, replacementOK := state.Tasks[replacementID]
	mission := state.Missions[task.MissionID]
	if !ok || !replacementOK || isClosedTaskStatus(task.Status) || replacementID == taskID || task.ReplacementID != "" || replacement.MissionID != task.MissionID || replacement.Status != "planned" || replacement.SupersedesID != "" || containsString(mission.TaskIDs, replacementID) || state.Artifacts[replacementID].Status != "accepted" || containsString(replacement.DependsOn, taskID) {
		return State{}, State{}, TaskState{}, &CLIError{Code: "CHS-TASK-SUPERSEDE-STATE", Message: "replacement must be a detached, accepted, planned Task in the same Mission", ExitCode: 10}
	}
	if err := requireMissionExecutable(state, task.MissionID); err != nil {
		return State{}, State{}, TaskState{}, err
	}
	task.Status = "superseded"
	task.ReplacementID = replacementID
	task.BlockReason, task.PreviousStatus = "", ""
	task.UpdatedAt = timeNow()
	replacement.SupersedesID = taskID
	if taskDependenciesSatisfied(state, replacement) {
		replacement.Status = "ready"
	}
	replacement.UpdatedAt = timeNow()
	previous, next, _, err := updateState(root, principal, "task.superseded", taskID, expected, func(next *State) error {
		next.Tasks[taskID] = task
		next.Tasks[replacementID] = replacement
		mission := next.Missions[task.MissionID]
		mission.TaskIDs = append(mission.TaskIDs, replacementID)
		mission.UpdatedAt = timeNow()
		next.Missions[mission.ID] = mission
		return nil
	})
	return previous, next, task, err
}

func workOpen(root, taskID string, principal Principal, expected int64) (State, State, TaskState, error) {
	_, _, state, err := loadProject(root)
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
	if err := requireMissionExecutable(state, task.MissionID); err != nil {
		return State{}, State{}, TaskState{}, err
	}
	worktreeRelative := taskWorktreeRelativePath(taskID)
	worktreePath := filepath.Join(root, filepath.FromSlash(worktreeRelative))
	intent := workOpenedPayload{TaskID: taskID, WorktreePath: worktreeRelative, Branch: task.Branch}
	previous, next, _, err := executeGitOperation(root, "work.open", "work.opened", taskID, principal, expected, intent, func(current State) (preparedOperation, error) {
		currentTask, ok := current.Tasks[taskID]
		if !ok || currentTask.Owner != principal.Actor || !containsString([]string{"claimed", "changes_requested"}, currentTask.Status) {
			return preparedOperation{}, &CLIError{Code: "CHS-WORK-NOT-ASSIGNED", Message: "task is not assigned to this developer or cannot be opened", ExitCode: 11}
		}
		if err := requireMissionExecutable(current, currentTask.MissionID); err != nil {
			return preparedOperation{}, err
		}
		clean, status, err := gitClean(root)
		if err != nil {
			return preparedOperation{}, err
		}
		if !clean {
			return preparedOperation{}, &CLIError{Code: "CHS-WORK-DIRTY", Message: "worktree must be clean before opening task: " + status, ExitCode: 10}
		}
		targetHead := currentTask.Baseline
		branchExists := false
		if head, err := git(root, "rev-parse", "--verify", "refs/heads/"+currentTask.Branch); err == nil {
			targetHead = head
			branchExists = true
		}
		tree, err := git(root, "rev-parse", targetHead+"^{tree}")
		if err != nil {
			return preparedOperation{}, err
		}
		bindingID := taskWorktreeBindingID(taskID, worktreeRelative, currentTask.Branch)
		gitIdentity := filepath.Base(worktreePath)
		rootState, err := captureGitOperationState(root)
		if err != nil {
			return preparedOperation{}, err
		}
		rootState.WorktreePath = worktreeRelative
		rootState.WorktreePresent = true
		rootState.WorktreeBranch = currentTask.Branch
		rootState.WorktreeHead = targetHead
		rootState.WorktreeIndexTree = tree
		rootState.WorktreeID = gitIdentity
		if _, err := os.Stat(worktreePath); err == nil {
			existingTask := currentTask
			existingTask.WorktreePath = worktreeRelative
			existingTask.WorktreeID = gitIdentity
			existingTask.WorktreeDigest = bindingID
			if _, err := taskWorktreeRoot(root, existingTask); err != nil {
				return preparedOperation{}, err
			}
			actualHead, err := gitHead(worktreePath)
			if err != nil {
				return preparedOperation{}, err
			}
			rootState.WorktreeHead = actualHead
			rootState.WorktreeIndexTree, err = git(worktreePath, "write-tree")
			if err != nil {
				return preparedOperation{}, err
			}
			targetHead = actualHead
		} else if !os.IsNotExist(err) {
			return preparedOperation{}, err
		}
		payload := workOpenedPayload{TaskID: taskID, WorktreePath: worktreeRelative, WorktreeID: gitIdentity, WorktreeDigest: bindingID, Branch: currentTask.Branch, Head: targetHead}
		return preparedOperation{
			Payload:  payload,
			GitAfter: rootState,
			ApplyGit: func() error {
				if _, err := os.Stat(worktreePath); err == nil {
					return nil
				} else if !os.IsNotExist(err) {
					return err
				}
				if !branchExists {
					_, err := git(root, "worktree", "add", "-b", currentTask.Branch, worktreePath, currentTask.Baseline)
					return err
				}
				_, err := git(root, "worktree", "add", worktreePath, currentTask.Branch)
				return err
			},
		}, nil
	})
	if err == nil {
		task = next.Tasks[taskID]
	}
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
	if err := requireMissionExecutable(state, task.MissionID); err != nil {
		return State{}, State{}, nil, err
	}
	worktreeRoot, err := taskWorktreeRoot(root, task)
	if err != nil {
		return State{}, State{}, nil, err
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
		results = append(results, runCheckSpec(worktreeRoot, check))
	}
	snapshotDigest, err := gitWorktreeDigest(worktreeRoot)
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
	if err := requireMissionExecutable(state, task.MissionID); err != nil {
		return State{}, State{}, TaskState{}, err
	}
	if _, err := taskWorktreeRoot(root, task); err != nil {
		return State{}, State{}, TaskState{}, err
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
	if err := requireMissionExecutable(state, task.MissionID); err != nil {
		return State{}, State{}, Submission{}, err
	}
	id, err := newID("SUB")
	if err != nil {
		return State{}, State{}, Submission{}, err
	}
	intent := struct {
		TaskID       string `json:"task_id"`
		SubmissionID string `json:"submission_id"`
		Handoff      string `json:"handoff"`
	}{TaskID: taskID, SubmissionID: id, Handoff: handoff}
	var submission Submission
	previous, next, _, err := executeGitOperation(root, "work.submit", "work.submitted", taskID, principal, expected, intent, func(current State) (preparedOperation, error) {
		currentTask, ok := current.Tasks[taskID]
		if !ok || currentTask.Owner != principal.Actor || currentTask.Status != "in_progress" {
			return preparedOperation{}, &CLIError{Code: "CHS-WORK-NOT-ACTIVE", Message: "task is not active for this developer", ExitCode: 11}
		}
		if err := requireMissionExecutable(current, currentTask.MissionID); err != nil {
			return preparedOperation{}, err
		}
		worktreeRoot, err := taskWorktreeRoot(root, currentTask)
		if err != nil {
			return preparedOperation{}, err
		}
		branch, err := currentBranch(worktreeRoot)
		if err != nil || branch != currentTask.Branch {
			return preparedOperation{}, &CLIError{Code: "CHS-WORK-BRANCH", Message: "current branch is not the task branch", ExitCode: 10}
		}
		files, err := gitWorkingFiles(worktreeRoot)
		if err != nil {
			return preparedOperation{}, err
		}
		if len(files) == 0 {
			return preparedOperation{}, &CLIError{Code: "CHS-WORK-NO-CHANGES", Message: "task has no content changes to submit", ExitCode: 10}
		}
		for _, file := range files {
			if !allowedFile(currentTask.AllowedPaths, file) {
				return preparedOperation{}, &CLIError{Code: "CHS-WORK-SCOPE", Message: "changed file is outside allowed_paths: " + file, ExitCode: 10}
			}
		}
		snapshotDigest, err := gitWorktreeDigest(worktreeRoot)
		if err != nil {
			return preparedOperation{}, err
		}
		if err := validateTaskChecks(currentTask, snapshotDigest); err != nil {
			return preparedOperation{}, err
		}
		before, head, err := gitPrepareCommit(worktreeRoot, "Complete "+taskID, files...)
		if err != nil {
			return preparedOperation{}, err
		}
		changed, err := gitChangedFiles(worktreeRoot, currentTask.Baseline, head)
		if err != nil {
			return preparedOperation{}, err
		}
		submission = Submission{ID: id, TaskID: taskID, BaseCommit: currentTask.Baseline, HeadCommit: head, ChangedFiles: changed, Checks: currentTask.CheckResults, Handoff: handoff}
		tree, err := git(worktreeRoot, "rev-parse", head+"^{tree}")
		if err != nil {
			return preparedOperation{}, err
		}
		expectedGit, err := captureGitOperationState(root)
		if err != nil {
			return preparedOperation{}, err
		}
		expectedGit.WorktreePath = currentTask.WorktreePath
		expectedGit.WorktreePresent = true
		expectedGit.WorktreeBranch = currentTask.Branch
		expectedGit.WorktreeHead = head
		expectedGit.WorktreeIndexTree = tree
		expectedGit.WorktreeID, err = gitWorktreeIdentity(worktreeRoot)
		if err != nil {
			return preparedOperation{}, err
		}
		return preparedOperation{
			Payload:  workSubmittedPayload{Submission: submission},
			GitAfter: expectedGit,
			ApplyGit: func() error { return applyPreparedCommit(worktreeRoot, branch, before, head) },
		}, nil
	})
	if err == nil {
		submission = next.Submissions[id]
	}
	return previous, next, submission, err
}

func validateTaskChecks(task TaskState, snapshotDigest string) error {
	for _, check := range task.Checks {
		result, ok := task.CheckResults[check.ID]
		if !ok || !result.Passed {
			return &CLIError{Code: "CHS-WORK-CHECKS", Message: "required check has not passed: " + check.ID, ExitCode: 10}
		}
		if result.SpecDigest != checkSpecDigest(check) {
			return &CLIError{Code: "CHS-WORK-CHECKS-STALE", Message: "required check no longer matches the Task contract: " + check.ID, ExitCode: 10, Remedy: []string{"rerun chassiss work check using the current Task contract"}}
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
	return reviewCheckState(root, state, submissionID)
}

func reviewCheckState(root string, state State, submissionID string) (Submission, TaskState, []string, error) {
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
		result, ok := submission.Checks[check.ID]
		if !ok || !result.Passed || result.SpecDigest != checkSpecDigest(check) {
			return Submission{}, TaskState{}, nil, &CLIError{Code: "CHS-REVIEW-CHECKS", Message: "submission lacks passed check: " + check.ID, ExitCode: 10}
		}
	}
	snapshotDigest, err := gitCommitSnapshotDigest(root, submission.HeadCommit)
	if err != nil {
		return Submission{}, TaskState{}, nil, err
	}
	for _, result := range submission.Checks {
		if result.SnapshotDigest != snapshotDigest {
			return Submission{}, TaskState{}, nil, &CLIError{Code: "CHS-REVIEW-CHECKS", Message: "submission check evidence does not match its exact head commit", ExitCode: 10}
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
	if state.Tasks[submission.TaskID].Status != "review_pending" {
		return State{}, State{}, Review{}, &CLIError{Code: "CHS-REVIEW-STATE", Message: "submission Task is blocked or no longer pending review", ExitCode: 10}
	}
	if err := requireMissionExecutable(state, state.Tasks[submission.TaskID].MissionID); err != nil {
		return State{}, State{}, Review{}, err
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
	if task.Status != "approved" {
		return State{}, State{}, Integration{}, &CLIError{Code: "CHS-INTEGRATION-NOT-APPROVED", Message: "submission Task is blocked or no longer approved", ExitCode: 10}
	}
	if err := requireMissionExecutable(state, task.MissionID); err != nil {
		return State{}, State{}, Integration{}, err
	}
	review := state.Reviews[submission.ReviewID]
	if review.SubmissionDigest != submission.Digest || review.Verdict != "approve" || review.Reviewer != principal.Actor {
		return State{}, State{}, Integration{}, &CLIError{Code: "CHS-INTEGRATION-REVIEW", Message: "integration must be performed by the approving Reviewer against the same digest", ExitCode: 11}
	}
	id, err := newID("INT")
	if err != nil {
		return State{}, State{}, Integration{}, err
	}
	candidateRelative := filepath.ToSlash(filepath.Join(".chassis", "cache", "integration-"+id))
	candidatePath := filepath.Join(root, filepath.FromSlash(candidateRelative))
	intent := integrationOperationIntent{SubmissionID: submissionID, CandidatePath: candidateRelative, TaskWorktreePath: task.WorktreePath}
	var integration Integration
	previous, next, _, err := executeGitOperation(root, "integrate.apply", "integration.applied", submissionID, principal, expected, intent, func(current State) (preparedOperation, error) {
		currentSubmission, currentTask, _, err := reviewCheckState(root, current, submissionID)
		if err != nil {
			return preparedOperation{}, err
		}
		if currentSubmission.Status != "approved" {
			return preparedOperation{}, &CLIError{Code: "CHS-INTEGRATION-NOT-APPROVED", Message: "submission is not approved", ExitCode: 10}
		}
		if currentTask.Status != "approved" {
			return preparedOperation{}, &CLIError{Code: "CHS-INTEGRATION-NOT-APPROVED", Message: "submission Task is blocked or no longer approved", ExitCode: 10}
		}
		if err := requireMissionExecutable(current, currentTask.MissionID); err != nil {
			return preparedOperation{}, err
		}
		currentReview, ok := current.Reviews[currentSubmission.ReviewID]
		if !ok || currentReview.SubmissionDigest != currentSubmission.Digest || currentReview.Verdict != "approve" || currentReview.Reviewer != principal.Actor {
			return preparedOperation{}, &CLIError{Code: "CHS-INTEGRATION-REVIEW", Message: "integration must use the approving Reviewer and exact submission digest", ExitCode: 11}
		}
		branchHead, err := git(root, "rev-parse", "--verify", "refs/heads/"+currentTask.Branch)
		if err != nil {
			return preparedOperation{}, err
		}
		if branchHead != currentSubmission.HeadCommit {
			return preparedOperation{}, &CLIError{Code: "CHS-INTEGRATION-HEAD-MOVED", Message: "task branch moved after review approval", ExitCode: 10, Remedy: []string{"create a new submission for the new branch head"}}
		}
		previousHead, err := git(root, "rev-parse", "--verify", "refs/heads/"+config.DefaultBranch)
		if err != nil {
			return preparedOperation{}, err
		}
		if previousHead != current.Baseline {
			return preparedOperation{}, &CLIError{Code: "CHS-INTEGRATION-BASELINE-MOVED", Message: "formal branch does not match the recorded baseline", ExitCode: 40, Remedy: []string{"run chassiss verify", "do not force the formal branch"}}
		}
		clean, status, err := gitClean(root)
		if err != nil {
			return preparedOperation{}, err
		}
		if !clean {
			return preparedOperation{}, &CLIError{Code: "CHS-INTEGRATION-DIRTY", Message: "worktree must be clean before integration: " + status, ExitCode: 10}
		}
		if _, err := os.Stat(candidatePath); err == nil {
			return preparedOperation{}, &CLIError{Code: "CHS-INTEGRATION-CANDIDATE", Message: "integration candidate path already exists", ExitCode: 40}
		} else if !os.IsNotExist(err) {
			return preparedOperation{}, err
		}
		if _, err := git(root, "worktree", "add", "--detach", candidatePath, previousHead); err != nil {
			return preparedOperation{}, err
		}
		keepCandidate := false
		defer func() {
			if !keepCandidate {
				_, _ = git(root, "worktree", "remove", "--force", candidatePath)
			}
		}()
		mergeArgs := []string{"-c", "user.name=CHASSISS Reviewer", "-c", "user.email=reviewer@chassiss.local", "merge", "--no-ff", "--no-commit", currentSubmission.HeadCommit}
		if _, err := git(candidatePath, mergeArgs...); err != nil {
			_, _ = git(candidatePath, "merge", "--abort")
			return preparedOperation{}, &CLIError{Code: "CHS-INTEGRATION-CONFLICT", Message: "local integration failed: " + err.Error(), ExitCode: 12}
		}
		mergedTree, err := git(candidatePath, "write-tree")
		if err != nil {
			return preparedOperation{}, err
		}
		checks, err := runIntegrationChecks(candidatePath, currentTask, mergedTree)
		if err != nil {
			_, _ = git(candidatePath, "merge", "--abort")
			return preparedOperation{}, err
		}
		commitArgs := []string{"-c", "user.name=CHASSISS Reviewer", "-c", "user.email=reviewer@chassiss.local", "commit", "-m", "Integrate " + currentTask.ID}
		if _, err := git(candidatePath, commitArgs...); err != nil {
			return preparedOperation{}, err
		}
		integratedHead, err := gitHead(candidatePath)
		if err != nil {
			return preparedOperation{}, err
		}
		actualTree, err := git(candidatePath, "rev-parse", integratedHead+"^{tree}")
		if err != nil || actualTree != mergedTree {
			return preparedOperation{}, &CLIError{Code: "CHS-INTEGRATION-TREE", Message: "integration commit tree differs from checked merge tree", ExitCode: 40}
		}
		integration = Integration{ID: id, SubmissionID: submissionID, SubmissionHead: currentSubmission.HeadCommit, PreviousHead: previousHead, IntegratedHead: integratedHead, IntegratedTree: mergedTree, Checks: checks}
		payload := integrationAppliedPayload{IntegrationID: id, SubmissionID: submissionID, SubmissionHead: currentSubmission.HeadCommit, PreviousHead: previousHead, IntegratedHead: integratedHead, IntegratedTree: mergedTree, Checks: checks}
		keepCandidate = true
		return preparedOperation{
			Payload:  payload,
			GitAfter: GitOperationState{Branch: config.DefaultBranch, Head: integratedHead, IndexTree: mergedTree},
			ApplyGit: func() error {
				formalHead, err := git(root, "rev-parse", "--verify", "refs/heads/"+config.DefaultBranch)
				if err != nil || formalHead != previousHead {
					return &CLIError{Code: "CHS-INTEGRATION-BASELINE-MOVED", Message: "formal branch moved while integration was prepared", ExitCode: 12, Retryable: true}
				}
				if _, err := git(root, "checkout", config.DefaultBranch); err != nil {
					return err
				}
				if err := injectOperationFault("integration_default_checked_out"); err != nil {
					return err
				}
				_, err = git(root, "merge", "--ff-only", integratedHead)
				return err
			},
			Finalize: func() error { return cleanupIntegrationWorktrees(root, intent, payload) },
		}, nil
	})
	if err == nil {
		integration = next.Integrations[id]
	}
	return previous, next, integration, err
}

func runIntegrationChecks(root string, task TaskState, tree string) (map[string]CheckResult, error) {
	results := make(map[string]CheckResult, len(task.Checks))
	for _, check := range task.Checks {
		result := runCheckSpec(root, check)
		result.SnapshotDigest = tree
		if !result.Passed {
			return nil, &CLIError{Code: "CHS-INTEGRATION-CHECKS", Message: "merged result failed acceptance check " + check.ID + ": " + result.Output, ExitCode: 10}
		}
		results[check.ID] = result
	}
	return results, nil
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
		if !containsString([]string{"integrated", "cancelled", "superseded"}, state.Tasks[taskID].Status) {
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
	if state.ActiveMission != "" && state.Missions[state.ActiveMission].Status == "blocked" && containsString([]string{"developer", "reviewer"}, role) {
		return actions
	}
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
				allClosed := true
				for _, id := range mission.TaskIDs {
					task := state.Tasks[id]
					if !containsString([]string{"integrated", "cancelled", "superseded"}, task.Status) {
						allClosed = false
					}
					if task.Status == "ready" {
						actions = append(actions, "task.claim "+id, "task.assign "+id)
					}
					if containsString([]string{"claimed", "in_progress"}, task.Status) && task.SubmissionID == "" {
						actions = append(actions, "task.release "+id)
					}
				}
				if allClosed && mission.Status == "active" {
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
			taskStatus := state.Tasks[submission.TaskID].Status
			if submission.Status == "review_pending" && taskStatus == "review_pending" {
				actions = append(actions, "review.check "+id, "review.approve "+id, "review.request-changes "+id)
			} else if submission.Status == "approved" && taskStatus == "approved" {
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
