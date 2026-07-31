package cli

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/ExplodeCode6324/chassiss/internal/gitstore"
	"github.com/ExplodeCode6324/chassiss/internal/localstate"
	"github.com/ExplodeCode6324/chassiss/internal/protocol"
	"github.com/ExplodeCode6324/chassiss/internal/state"
)

func taskMutationCommand(ctx context.Context, invocation invocation) (Envelope, error) {
	project, err := loadMutableProject(ctx)
	if err != nil {
		return Envelope{}, err
	}
	taskID := invocation.Positionals[0]
	runtimeTask, exists := project.Verified.State.Tasks[taskID]
	if !exists {
		return Envelope{}, protocol.NewError(protocol.ErrReferenceNotFound, protocol.CategoryValidation, "Task does not exist.")
	}
	resources, _, err := taskResources(project, taskID)
	if err != nil {
		return Envelope{}, err
	}
	action := map[string]string{
		"task start": "task.started", "task release": "task.released",
		"attempt abandon": "attempt.abandoned",
		"task block":      "task.blocked", "task resume": "task.resumed",
		"task cancel": "task.cancelled", "task supersede": "task.superseded",
	}[invocation.Definition.Path]
	spec, _ := protocol.Action(action)
	var authority selectedAuthority
	if action == "attempt.abandoned" {
		authority, err = selectRoot(project, invocation.Value("root-key"))
	} else if action == "task.superseded" && invocation.Value("grant") == "" && invocation.Value("key") != "" {
		authority, err = selectRoot(project, invocation.Value("key"))
	} else {
		authority, err = selectGrant(project, invocation, spec.Capability, taskID, resources, false)
	}
	if err != nil {
		return Envelope{}, err
	}
	operationID, err := operationID(invocation)
	if err != nil {
		return Envelope{}, err
	}
	operation := protocol.Operation{
		Schema: protocol.OperationSchema, OperationID: operationID,
		Action: action, Project: project.Verified.State.Project.ID,
		Authority: authority.Reference, Target: taskID,
		Preconditions: map[string]any{}, Payload: map[string]any{},
	}
	evidenceFacts := map[string]any{}
	reduceFacts := state.ReduceFacts{
		ObjectFormat: project.Verified.ObjectFormat, ParentCommit: project.Verified.Head,
		TaskResources: resources, SignerFingerprint: authority.Fingerprint,
	}
	var archiveRef, archiveHead string
	switch action {
	case "task.started":
		if runtimeTask.Phase != "ready" || runtimeTask.Blocked != nil {
			return Envelope{}, protocol.NewError(protocol.ErrTaskPhaseInvalid, protocol.CategoryValidation, "Task start requires unblocked ready.")
		}
		if project.Verified.State.Project.Taskbook == nil {
			return Envelope{}, protocol.NewError(protocol.ErrTaskbookNotActive, protocol.CategoryValidation, "No Taskbook is active.")
		}
		operation.Preconditions = map[string]any{
			"architecture_blob": project.Verified.State.Project.Architecture.BlobOID,
			"phase":             "ready", "taskbook_blob": project.Verified.State.Project.Taskbook.BlobOID,
		}
		evidenceFacts = map[string]any{
			"actor":             authority.Grant.Actor,
			"architecture_blob": project.Verified.State.Project.Architecture.BlobOID,
			"base":              project.Verified.Head,
			"taskbook_blob":     project.Verified.State.Project.Taskbook.BlobOID,
		}
		for _, task := range project.Verified.State.Tasks {
			if task.Actor == authority.Grant.Actor &&
				(task.Phase == "active" || task.Phase == "submitted" || task.Phase == "approved") {
				reduceFacts.ActiveTasksForActor++
			}
		}
	case "task.released":
		if runtimeTask.Phase != "active" || runtimeTask.Blocked != nil {
			return Envelope{}, protocol.NewError(protocol.ErrTaskPhaseInvalid, protocol.CategoryValidation, "Task release requires an unblocked active Task.")
		}
		if err := verifyReleaseWorktree(ctx, project, taskID, runtimeTask); err != nil {
			return Envelope{}, err
		}
		baseCommit, err := project.Runner.ReadCommit(ctx, runtimeTask.Base)
		if err != nil {
			return Envelope{}, err
		}
		operation.Preconditions = map[string]any{
			"actor": runtimeTask.Actor, "base": runtimeTask.Base, "phase": "active",
		}
		operation.Payload = map[string]any{"reason": invocation.Value("reason")}
		evidenceFacts = map[string]any{
			"base": runtimeTask.Base, "observed_work_head": runtimeTask.Base,
			"observed_work_tree": baseCommit.Tree,
		}
	case "attempt.abandoned":
		if runtimeTask.Phase != "active" && runtimeTask.Phase != "submitted" &&
			runtimeTask.Phase != "approved" {
			return Envelope{}, protocol.NewError(protocol.ErrTaskPhaseInvalid, protocol.CategoryValidation, "Attempt abandon requires an active Agent phase.")
		}
		agentGrantID := invocation.Value("agent-grant")
		agentKeyID := invocation.Value("agent-key")
		agentGrant, exists := project.Verified.State.Authority.Grants[agentGrantID]
		if !exists || agentGrant.Actor != runtimeTask.Actor || agentGrant.KeyID != agentKeyID {
			return Envelope{}, protocol.NewError(protocol.ErrGrantNotFound, protocol.CategoryAuthorization, "Agent Grant/key does not match the active Task actor.")
		}
		worktree, exists := project.LocalProject.Worktrees[taskID]
		if !exists {
			return Envelope{}, protocol.NewError(protocol.ErrWorktreeNotFound, protocol.CategoryLocal, "Managed Task worktree is not registered.")
		}
		workRunner := gitstore.New(worktree.Path)
		changed, err := workingChangedPaths(ctx, workRunner)
		if err != nil {
			return Envelope{}, err
		}
		changedDigest, _ := protocol.ObjectDigest("changed-paths", changed)
		workHead, err := workRunner.Resolve(ctx, "HEAD")
		if err != nil {
			return Envelope{}, err
		}
		workCommit, err := workRunner.ReadCommit(ctx, workHead)
		if err != nil {
			return Envelope{}, err
		}
		failure := map[string]any{
			"actor": runtimeTask.Actor, "agent_grant_id": agentGrantID,
			"agent_key_id": agentKeyID, "changed_paths_digest": changedDigest,
			"code": invocation.Value("code"), "phase": runtimeTask.Phase,
			"reason": invocation.Value("reason"), "schema": "chassiss.attempt-failure/v1",
			"summary": invocation.Value("summary"), "task": taskID,
			"taskbook":  project.Verified.State.Project.Taskbook.ID,
			"work_head": workHead, "work_tree": workCommit.Tree,
		}
		operation.Preconditions = map[string]any{
			"actor": runtimeTask.Actor, "agent_grant_id": agentGrantID,
			"agent_key_id": agentKeyID, "base": runtimeTask.Base,
			"phase": runtimeTask.Phase,
		}
		operation.Payload = map[string]any{"failure": failure}
		evidenceFacts = map[string]any{
			"changed_paths_digest": changedDigest,
			"observed_work_head":   workHead,
			"observed_work_tree":   workCommit.Tree,
		}
	case "task.blocked":
		operation.Preconditions = map[string]any{"blocked": false, "phase": runtimeTask.Phase}
		operation.Payload = map[string]any{"reason": invocation.Value("reason")}
	case "task.resumed":
		operation.Preconditions = map[string]any{"blocked": true, "phase": runtimeTask.Phase}
		var reason any
		if invocation.Value("reason") != "" {
			reason = invocation.Value("reason")
		}
		operation.Payload = map[string]any{"reason": reason}
	case "task.cancelled", "task.superseded":
		operation.Preconditions = map[string]any{"attempt_digest": nil, "phase": runtimeTask.Phase}
		operation.Payload = map[string]any{"reason": invocation.Value("reason")}
		if action == "task.superseded" {
			var replacement any
			if invocation.Value("replacement") != "" {
				replacement = invocation.Value("replacement")
			}
			operation.Payload["replacement_task"] = replacement
		}
		evidenceFacts = map[string]any{
			"archive_head": nil, "archive_ref": nil, "attempt_digest": nil,
		}
		if runtimeTask.Attempt != nil {
			attemptDigest, err := state.AttemptDigest(taskID, runtimeTask)
			if err != nil {
				return Envelope{}, err
			}
			archiveRef = "refs/chassiss/archive/" + taskID + "/" + strings.TrimPrefix(attemptDigest, "sha256:")
			archiveHead = runtimeTask.Attempt.Head
			operation.Preconditions["attempt_digest"] = attemptDigest
			evidenceFacts = map[string]any{
				"archive_head": archiveHead, "archive_ref": archiveRef,
				"attempt_digest": attemptDigest,
			}
		}
	}
	operationDigest, _ := protocol.ObjectDigest("operation", operation)
	parent := project.Verified.Head
	evidence := protocol.ExecutionEvidence{
		Schema: protocol.EvidenceSchema, OperationDigest: operationDigest,
		Action: action, Attempt: 1, Parent: &parent, Facts: evidenceFacts,
	}
	next, err := state.Reduce(project.Verified.State, operation, evidence, reduceFacts)
	if err != nil {
		return Envelope{}, err
	}
	tree, err := currentTree(ctx, project)
	if err != nil {
		return Envelope{}, err
	}
	envelope, err := publishTransition(ctx, project, transitionPlan{
		Operation: operation, Evidence: evidence, NextState: next, Tree: tree,
		ReduceFacts: reduceFacts, Authority: authority,
		ArchiveRef: archiveRef, ArchiveHead: archiveHead,
		Result: map[string]any{"task": taskID},
	})
	if err != nil {
		return Envelope{}, err
	}
	if action == "attempt.abandoned" {
		worktreeRemoved, warnings := cleanupAbandonedWork(ctx, project, taskID)
		envelope.Warnings = append(envelope.Warnings, warnings...)
		envelope.Result.(map[string]any)["worktree_removed"] = worktreeRemoved
		envelope.Result.(map[string]any)["agent_grant"] = invocation.Value("agent-grant")
		envelope.Result.(map[string]any)["agent_key"] = invocation.Value("agent-key")
	}
	if action == "task.started" {
		published, readErr := project.Runner.ReadCommit(ctx, envelope.Operation.Commit)
		base := project.Verified.Head
		if readErr == nil && len(published.Parents) > 0 {
			base = published.Parents[0]
		}
		path, workErr := createManagedWorktree(ctx, project, taskID, authority.Grant.Actor, base)
		if workErr != nil {
			envelope.Warnings = append(envelope.Warnings, Warning{
				Code:    "CHS_WARN_WORKTREE_RECOVERY_REQUIRED",
				Message: "Task started but the local managed worktree must be restored with work open.",
				Details: map[string]any{"error": workErr.Error()},
			})
		} else {
			envelope.Result.(map[string]any)["worktree"] = path
		}
	}
	return envelope, nil
}

func cleanupAbandonedWork(ctx context.Context, project *projectContext, taskID string) (bool, []Warning) {
	worktree, exists := project.LocalProject.Worktrees[taskID]
	if !exists {
		return true, nil
	}
	if _, err := project.Runner.Run(ctx, "worktree", "remove", "--force", worktree.Path); err != nil {
		return false, []Warning{cleanupWarning(taskID, "worktree")}
	}
	warnings := make([]Warning, 0)
	if _, err := project.Runner.Run(ctx, "update-ref", "-d", worktree.Branch); err != nil {
		warnings = append(warnings, cleanupWarning(taskID, "local_ref"))
	}
	if project.LocalProject.Remote.URL != "" {
		if _, err := project.Runner.Run(ctx, "push", "--porcelain", "origin", ":"+worktree.Branch); err != nil {
			warnings = append(warnings, cleanupWarning(taskID, "remote_ref"))
		}
	}
	if err := project.Store.Update(func(local *localstate.State) error {
		value := local.Projects[project.Verified.State.Project.ID]
		delete(value.Worktrees, taskID)
		local.Projects[project.Verified.State.Project.ID] = value
		return nil
	}); err != nil {
		warnings = append(warnings, cleanupWarning(taskID, "registry"))
	}
	return true, warnings
}

func cleanupWarning(taskID, artifact string) Warning {
	return Warning{
		Code:    "CHS_WARN_LOCAL_CLEANUP",
		Message: "Attempt failure was recorded, but managed Work cleanup requires reconciliation.",
		Details: map[string]any{"artifact": artifact, "task": taskID},
	}
}

func createManagedWorktree(ctx context.Context, project *projectContext, taskID, actor, base string) (string, error) {
	path := worktreePath(project, taskID, actor, base)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", err
	}
	ref := taskWorkRef(taskID, actor, base)
	zero := zeroOID(project.Verified.ObjectFormat)
	if err := project.Runner.UpdateRefCAS(ctx, ref, base, zero, "CHASSISS Work start"); err != nil {
		if existing, resolveErr := project.Runner.Resolve(ctx, ref); resolveErr != nil || existing != base {
			return "", err
		}
	}
	if _, err := project.Runner.Run(ctx, "worktree", "add", "--detach", path, base); err != nil {
		return "", err
	}
	workRunner := gitstore.New(path)
	if _, err := workRunner.Run(ctx, "symbolic-ref", "HEAD", ref); err != nil {
		return "", err
	}
	if err := project.Store.Update(func(local *localstate.State) error {
		value := local.Projects[project.Verified.State.Project.ID]
		value.Worktrees[taskID] = localstate.Worktree{
			TaskID: taskID, Actor: actor, Path: path, Branch: ref,
			Base: base, Head: base, Dirty: false,
		}
		local.Projects[project.Verified.State.Project.ID] = value
		return nil
	}); err != nil {
		return "", err
	}
	return path, nil
}

func verifyReleaseWorktree(ctx context.Context, project *projectContext, taskID string, runtimeTask state.TaskState) error {
	worktree, exists := project.LocalProject.Worktrees[taskID]
	if !exists {
		return protocol.NewError(protocol.ErrWorktreeNotFound, protocol.CategoryLocal, "Managed Task worktree is not registered.")
	}
	runner := gitstore.New(worktree.Path)
	result, err := runner.Run(ctx, "status", "--porcelain=v1", "-z")
	if err != nil {
		return err
	}
	if len(result.Stdout) != 0 {
		return protocol.NewError(protocol.ErrWorktreeDirty, protocol.CategoryLocal, "Task release requires a clean worktree.")
	}
	head, err := runner.Resolve(ctx, "HEAD")
	if err != nil {
		return err
	}
	if head != runtimeTask.Base {
		return protocol.NewError(protocol.ErrReleaseHasChanges, protocol.CategoryValidation, "Task release requires Work Head to equal the frozen base.")
	}
	return nil
}
