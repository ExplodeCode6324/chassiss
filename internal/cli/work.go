package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/ExplodeCode6324/chassiss/internal/contracts"
	"github.com/ExplodeCode6324/chassiss/internal/cryptoutil"
	"github.com/ExplodeCode6324/chassiss/internal/gitstore"
	"github.com/ExplodeCode6324/chassiss/internal/localstate"
	"github.com/ExplodeCode6324/chassiss/internal/protocol"
	"github.com/ExplodeCode6324/chassiss/internal/state"
	"github.com/ExplodeCode6324/chassiss/internal/workflow"
)

func workCommand(ctx context.Context, invocation invocation) (Envelope, error) {
	switch invocation.Definition.Path {
	case "work open":
		return workOpenCommand(ctx, invocation)
	case "work status":
		return workStatusCommand(ctx, invocation)
	case "work diff":
		return workDiffCommand(ctx, invocation)
	case "work log":
		return workLogCommand(ctx, invocation)
	case "work commit":
		return workCommitCommand(ctx, invocation)
	case "work restore":
		return workRestoreCommand(ctx, invocation)
	case "work remove":
		return workRemoveCommand(ctx, invocation)
	case "check":
		return checkCommand(ctx, invocation)
	case "submit":
		return submitCommand(ctx, invocation)
	default:
		panic("unreachable")
	}
}

func workOpenCommand(ctx context.Context, invocation invocation) (Envelope, error) {
	project, err := loadProject(ctx, "", false)
	if err != nil {
		return Envelope{}, err
	}
	taskID := invocation.Positionals[0]
	task, exists := project.Verified.State.Tasks[taskID]
	if !exists || task.Actor == "" || task.Base == "" {
		return Envelope{}, protocol.NewError(protocol.ErrTaskPhaseInvalid, protocol.CategoryValidation, "Task has no active managed work.")
	}
	worktree, exists := project.LocalProject.Worktrees[taskID]
	if exists {
		if info, statErr := os.Stat(worktree.Path); statErr == nil && info.IsDir() {
			envelope := projectEnvelope("work open", project)
			envelope.Result = map[string]any{"task": taskID, "worktree": worktree.Path}
			return envelope, nil
		}
	}
	path, err := createManagedWorktree(ctx, project, taskID, task.Actor, task.Base)
	if err != nil {
		return Envelope{}, err
	}
	envelope := projectEnvelope("work open", project)
	envelope.Result = map[string]any{"task": taskID, "worktree": path}
	return envelope, nil
}

func workStatusCommand(ctx context.Context, invocation invocation) (Envelope, error) {
	project, worktree, task, contract, runner, err := loadWork(ctx, invocation.Positionals[0])
	if err != nil {
		return Envelope{}, err
	}
	changed, err := workingChangedPaths(ctx, runner)
	if err != nil {
		return Envelope{}, err
	}
	violations := scopeViolations(changed, contract.Writes)
	head, err := runner.Resolve(ctx, "HEAD")
	if err != nil {
		return Envelope{}, err
	}
	envelope := projectEnvelope("work status", project)
	envelope.Result = map[string]any{
		"base": task.Base, "changed_paths": changed, "dirty": len(changed) > 0,
		"head": head, "published_head": worktree.PublishedHead,
		"scope_violations": violations, "task": invocation.Positionals[0],
		"worktree": worktree.Path,
	}
	return envelope, nil
}

func workDiffCommand(ctx context.Context, invocation invocation) (Envelope, error) {
	project, _, task, _, runner, err := loadWork(ctx, invocation.Positionals[0])
	if err != nil {
		return Envelope{}, err
	}
	against := invocation.Value("against")
	if against == "" {
		against = "base"
	}
	var revision string
	switch against {
	case "base":
		revision = task.Base
	case "main":
		revision = project.Verified.Head
	case "head":
		revision = "HEAD"
	default:
		return Envelope{}, usageError("--against must be base, main, or head")
	}
	paths := invocation.Values["path"]
	if len(paths) > 0 {
		for _, path := range paths {
			if err := contracts.ValidateRepoPath(path); err != nil {
				return Envelope{}, usageError(err.Error())
			}
		}
	}
	diff, err := runner.WorkingDiff(ctx, revision, invocation.Flags["stat"], paths)
	if err != nil {
		return Envelope{}, err
	}
	envelope := projectEnvelope("work diff", project)
	envelope.Result = map[string]any{
		"against": against, "diff": string(diff), "task": invocation.Positionals[0],
	}
	return envelope, nil
}

func workLogCommand(ctx context.Context, invocation invocation) (Envelope, error) {
	project, _, task, _, runner, err := loadWork(ctx, invocation.Positionals[0])
	if err != nil {
		return Envelope{}, err
	}
	limit := 100
	if value := invocation.Value("limit"); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil || parsed < 1 || parsed > 10_000 {
			return Envelope{}, usageError("--limit must be in [1,10000]")
		}
		limit = parsed
	}
	result, err := runner.Run(ctx, "log", "--first-parent", "--format=%H%x00%P%x00%s%x00",
		"-n", strconv.Itoa(limit), task.Base+"..HEAD")
	if err != nil {
		return Envelope{}, err
	}
	fields := bytes.Split(result.Stdout, []byte{0})
	commits := make([]map[string]any, 0)
	for index := 0; index+2 < len(fields); index += 3 {
		if len(fields[index]) == 0 {
			continue
		}
		commits = append(commits, map[string]any{
			"commit":  string(bytes.TrimSpace(fields[index])),
			"parents": strings.Fields(string(fields[index+1])),
			"subject": string(fields[index+2]),
		})
	}
	envelope := projectEnvelope("work log", project)
	envelope.Result = map[string]any{"commits": commits, "task": invocation.Positionals[0]}
	return envelope, nil
}

func workCommitCommand(ctx context.Context, invocation invocation) (Envelope, error) {
	project, worktree, task, contract, runner, err := loadWork(ctx, invocation.Positionals[0])
	if err != nil {
		return Envelope{}, err
	}
	if task.Phase != "active" || task.Blocked != nil {
		return Envelope{}, protocol.NewError(protocol.ErrTaskPhaseInvalid, protocol.CategoryValidation, "Work Commit requires an unblocked active Task.")
	}
	changed, err := workingChangedPaths(ctx, runner)
	if err != nil {
		return Envelope{}, err
	}
	selected := invocation.Values["path"]
	if len(selected) == 0 {
		selected = changed
	} else {
		selected = sortedUniquePaths(selected)
	}
	if len(selected) == 0 {
		return Envelope{}, protocol.NewError(protocol.ErrUsageInvalid, protocol.CategoryUsage, "Work Commit cannot be empty.")
	}
	if violations := scopeViolations(selected, contract.Writes); len(violations) > 0 {
		return Envelope{}, &protocol.Error{
			Code: protocol.ErrScopeViolation, Category: protocol.CategoryValidation,
			Message: "Work contains paths outside the frozen Task Contract.",
			Details: map[string]any{"paths": violations},
		}
	}
	for _, path := range selected {
		if !containsString(changed, path) {
			return Envelope{}, usageError("selected --path has no uncommitted change: " + path)
		}
		if err := validateWorkSymlink(worktree.Path, path); err != nil {
			return Envelope{}, err
		}
	}
	public, keyPath, fingerprint, err := taskActorKey(project, task.Actor)
	if err != nil {
		return Envelope{}, err
	}
	_ = public
	if _, err := runner.Run(ctx, "read-tree", "HEAD"); err != nil {
		return Envelope{}, err
	}
	args := []string{"add", "-A", "--"}
	args = append(args, selected...)
	if _, err := runner.Run(ctx, args...); err != nil {
		return Envelope{}, err
	}
	staged, err := runner.Run(ctx, "diff", "--cached", "--name-only", "-z")
	if err != nil {
		return Envelope{}, err
	}
	stagedPaths := splitNUL(staged.Stdout)
	if len(stagedPaths) == 0 || len(scopeViolations(stagedPaths, contract.Writes)) > 0 {
		return Envelope{}, protocol.NewError(protocol.ErrScopeViolation, protocol.CategoryValidation, "Staged Work diff is empty or outside Task scope.")
	}
	treeResult, err := runner.Run(ctx, "write-tree")
	if err != nil {
		return Envelope{}, err
	}
	tree := stringTrimSpace(treeResult.Stdout)
	head, err := runner.Resolve(ctx, "HEAD")
	if err != nil {
		return Envelope{}, err
	}
	message := strings.TrimSpace(invocation.Value("message"))
	if message == "" {
		return Envelope{}, usageError("--message must be non-empty")
	}
	commit, err := runner.CommitTree(ctx, tree, []string{head}, message+"\n", keyPath, gitstore.CommitIdentity{})
	if err != nil {
		return Envelope{}, err
	}
	if err := runner.UpdateRefCAS(ctx, worktree.Branch, commit, head, "CHASSISS Work Commit"); err != nil {
		return Envelope{}, err
	}
	if _, err := runner.Run(ctx, "read-tree", commit); err != nil {
		return Envelope{}, err
	}
	if err := project.Store.Update(func(local *localstate.State) error {
		value := local.Projects[project.Verified.State.Project.ID]
		record := value.Worktrees[invocation.Positionals[0]]
		record.Head = commit
		record.Dirty = false
		value.Worktrees[invocation.Positionals[0]] = record
		local.Projects[project.Verified.State.Project.ID] = value
		return nil
	}); err != nil {
		return Envelope{}, err
	}
	envelope := projectEnvelope("work commit", project)
	envelope.Result = map[string]any{
		"commit": commit, "key_fingerprint": fingerprint,
		"paths": stagedPaths, "task": invocation.Positionals[0],
	}
	return envelope, nil
}

func workRestoreCommand(ctx context.Context, invocation invocation) (Envelope, error) {
	if !invocation.Flags["yes"] {
		return Envelope{}, usageError("work restore requires --yes after exact path review")
	}
	project, worktree, _, _, runner, err := loadWork(ctx, invocation.Positionals[0])
	if err != nil {
		return Envelope{}, err
	}
	changed, err := workingChangedPaths(ctx, runner)
	if err != nil {
		return Envelope{}, err
	}
	paths := sortedUniquePaths(invocation.Values["path"])
	for _, path := range paths {
		if err := contracts.ValidateRepoPath(path); err != nil || contracts.IsProtectedPath(path) ||
			strings.Contains(path, "*") || !containsString(changed, path) {
			return Envelope{}, usageError("work restore requires exact changed non-protected paths")
		}
		absolute := filepath.Join(worktree.Path, filepath.FromSlash(path))
		if info, statErr := os.Lstat(absolute); statErr == nil && info.IsDir() {
			return Envelope{}, usageError("work restore does not accept directory targets")
		}
		tracked, _ := runner.Run(ctx, "ls-files", "--error-unmatch", "--", path)
		if len(tracked.Stdout) > 0 {
			if _, err := runner.Run(ctx, "restore", "--source=HEAD", "--staged", "--worktree", "--", path); err != nil {
				return Envelope{}, err
			}
		} else if err := os.Remove(absolute); err != nil && !os.IsNotExist(err) {
			return Envelope{}, err
		}
	}
	envelope := projectEnvelope("work restore", project)
	envelope.Result = map[string]any{"restored": paths, "task": invocation.Positionals[0]}
	return envelope, nil
}

func workRemoveCommand(ctx context.Context, invocation invocation) (Envelope, error) {
	project, worktree, task, _, runner, err := loadWork(ctx, invocation.Positionals[0])
	if err != nil {
		return Envelope{}, err
	}
	changed, err := workingChangedPaths(ctx, runner)
	if err != nil {
		return Envelope{}, err
	}
	if len(changed) > 0 {
		return Envelope{}, protocol.NewError(protocol.ErrWorktreeDirty, protocol.CategoryLocal, "Dirty managed worktree cannot be removed.")
	}
	head, err := runner.Resolve(ctx, "HEAD")
	if err != nil {
		return Envelope{}, err
	}
	if !workRemovalSafe(task, worktree, head) {
		return Envelope{}, protocol.NewError(protocol.ErrAttemptUnreachable, protocol.CategoryLocal, "Work Head is not retained by main/archive and cannot be removed.")
	}
	if invocation.Flags["discard-unreachable"] && !invocation.Flags["yes"] {
		return Envelope{}, usageError("--discard-unreachable requires --yes")
	}
	cleanup := cleanupManagedWork(ctx, project, invocation.Positionals[0], worktree, head)
	if cleanup.Err != nil {
		err := protocol.WrapError(
			protocol.ErrWorktreeNotFound, protocol.CategoryLocal,
			"Managed Work cleanup failed.", cleanup.Err,
		)
		err.Details = map[string]any{
			"artifact": cleanup.Artifact, "task": invocation.Positionals[0],
			"worktree_removed":          cleanup.WorktreeRemoved,
			"local_ref_removed":         cleanup.LocalRefRemoved,
			"worktree_registry_removed": cleanup.RegistryRemoved,
		}
		return Envelope{}, err
	}
	envelope := projectEnvelope("work remove", project)
	envelope.Result = map[string]any{"removed": true, "task": invocation.Positionals[0]}
	addManagedWorkCleanupResult(envelope.Result.(map[string]any), cleanup)
	return envelope, nil
}

func workRemovalSafe(task state.TaskState, worktree localstate.Worktree, head string) bool {
	return head == task.Base || task.Phase == "closed" ||
		task.Phase == "ready" && head == worktree.Base ||
		task.Phase == "cancelled" || task.Phase == "superseded"
}

func checkCommand(ctx context.Context, invocation invocation) (Envelope, error) {
	project, _, task, contract, runner, err := loadWork(ctx, invocation.Positionals[0])
	if err != nil {
		return Envelope{}, err
	}
	results, changed, err := runTaskChecks(ctx, project, runner, invocation.Positionals[0], task, contract)
	if err != nil {
		return Envelope{}, err
	}
	envelope := projectEnvelope("check", project)
	envelope.Result = map[string]any{
		"changed_paths": changed, "check_results": results,
		"passed": allChecksPassed(results), "task": invocation.Positionals[0],
	}
	return envelope, nil
}

func submitCommand(ctx context.Context, invocation invocation) (Envelope, error) {
	project, err := loadMutableProject(ctx)
	if err != nil {
		return Envelope{}, err
	}
	taskID := invocation.Positionals[0]
	worktree, exists := project.LocalProject.Worktrees[taskID]
	if !exists {
		return Envelope{}, protocol.NewError(protocol.ErrWorktreeNotFound, protocol.CategoryLocal, "Managed Task worktree is not registered.")
	}
	task := project.Verified.State.Tasks[taskID]
	if task.Phase != "active" || task.Blocked != nil {
		return Envelope{}, protocol.NewError(protocol.ErrTaskPhaseInvalid, protocol.CategoryValidation, "Submit requires an unblocked active Task.")
	}
	resources, contract, err := taskResources(project, taskID)
	if err != nil {
		return Envelope{}, err
	}
	authoritySelection, err := selectGrant(project, invocation, "task.submit", taskID, resources, false)
	if err != nil {
		return Envelope{}, err
	}
	if authoritySelection.Grant.Actor != task.Actor {
		return Envelope{}, protocol.NewError(protocol.ErrTaskActorMismatch, protocol.CategoryAuthorization, "Submit Grant actor is not the Task actor.")
	}
	workRunner := gitstore.New(worktree.Path)
	results, changed, err := runTaskChecks(ctx, project, workRunner, taskID, task, contract)
	if err != nil {
		return Envelope{}, err
	}
	if !allChecksPassed(results) {
		return Envelope{}, protocol.NewError(protocol.ErrCheckFailed, protocol.CategoryCheck, "One or more frozen Task Checks failed.")
	}
	head, err := workRunner.Resolve(ctx, "HEAD")
	if err != nil {
		return Envelope{}, err
	}
	headCommit, err := project.Runner.ReadCommit(ctx, head)
	if err != nil {
		return Envelope{}, err
	}
	if project.LocalProject.Remote.URL != "" {
		ref := taskWorkRef(taskID, task.Actor, task.Base)
		expected := worktree.PublishedHead
		args := []string{"push", "--porcelain"}
		if expected != "" {
			args = append(args, "--force-with-lease="+ref+":"+expected)
		} else {
			args = append(args, "--force-with-lease="+ref+":")
		}
		args = append(args, "origin", head+":"+ref)
		if _, pushErr := project.Runner.Run(ctx, args...); pushErr != nil {
			actual, exists, reconcileErr := readRemoteRef(ctx, project.Runner, "origin", ref)
			if reconcileErr != nil {
				failure := protocol.WrapError(
					protocol.ErrPushResultUnknown, protocol.CategoryNetwork,
					"Work Ref push result requires reconciliation.", pushErr,
				)
				failure.Retryable = true
				failure.Details["reconciliation_error"] = reconcileErr.Error()
				return Envelope{}, failure
			}
			if !exists {
				return Envelope{}, protocol.WrapError(
					protocol.ErrRemoteUnreachable, protocol.CategoryNetwork,
					"Remote verification confirmed that the exact Work Head was not published.", pushErr,
				)
			}
			if actual != head {
				failure := protocol.NewError(
					protocol.ErrAttemptStale, protocol.CategoryConflict,
					"Remote Work Ref does not contain the exact submitted Work Head.",
				)
				failure.Details = map[string]any{
					"actual_head": actual, "expected_head": head, "ref": ref,
				}
				return Envelope{}, failure
			}
		}
	}
	changedDigest, _ := protocol.ObjectDigest("changed-paths", changed)
	submission := workflow.SubmissionEvidence{
		ArchitectureBlob: task.Contract.ArchitectureBlob, Base: task.Base,
		ChangedPathsCount: int64(len(changed)), ChangedPathsDigest: changedDigest,
		CheckResults: results, Head: head, Schema: workflow.SubmissionEvidenceSchema,
		Submitter: workflow.Submitter{
			Actor: task.Actor, KeyFingerprint: authoritySelection.Fingerprint,
		},
		Task: taskID, TaskbookBlob: task.Contract.TaskbookBlob, Tree: headCommit.Tree,
	}
	submissionObject, err := objectMap(submission)
	if err != nil {
		return Envelope{}, err
	}
	operationID, err := operationID(invocation)
	if err != nil {
		return Envelope{}, err
	}
	operation := protocol.Operation{
		Schema: protocol.OperationSchema, OperationID: operationID,
		Action: "task.submitted", Project: project.Verified.State.Project.ID,
		Authority: authoritySelection.Reference, Target: taskID,
		Preconditions: map[string]any{
			"actor": task.Actor, "architecture_blob": task.Contract.ArchitectureBlob,
			"base": task.Base, "phase": "active", "taskbook_blob": task.Contract.TaskbookBlob,
		},
		Payload: map[string]any{"head": head},
	}
	operationDigest, _ := protocol.ObjectDigest("operation", operation)
	parent := project.Verified.Head
	evidence := protocol.ExecutionEvidence{
		Schema: protocol.EvidenceSchema, OperationDigest: operationDigest,
		Action: operation.Action, Attempt: 1, Parent: &parent,
		Facts: map[string]any{"submission_evidence": submissionObject},
	}
	reduceFacts := state.ReduceFacts{
		ObjectFormat: project.Verified.ObjectFormat, ParentCommit: parent,
		SignerFingerprint: authoritySelection.Fingerprint,
		TaskResources:     resources, ChangedPaths: int64(len(changed)),
	}
	if contract.ChangeLimits != nil {
		limit := contract.ChangeLimits.MaxChangedPaths
		reduceFacts.EffectiveMaxChangedPaths = &limit
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
		ReduceFacts: reduceFacts, Authority: authoritySelection, Result: map[string]any{
			"head": head, "task": taskID,
		},
	})
	if err != nil {
		return Envelope{}, err
	}
	_ = project.Store.Update(func(local *localstate.State) error {
		value := local.Projects[project.Verified.State.Project.ID]
		record := value.Worktrees[taskID]
		record.Head = head
		record.PublishedHead = head
		value.Worktrees[taskID] = record
		local.Projects[project.Verified.State.Project.ID] = value
		return nil
	})
	return envelope, nil
}

func loadWork(ctx context.Context, taskID string) (*projectContext, localstate.Worktree, state.TaskState, contracts.Task, gitstore.Runner, error) {
	project, err := loadProject(ctx, "", false)
	if err != nil {
		return nil, localstate.Worktree{}, state.TaskState{}, contracts.Task{}, gitstore.Runner{}, err
	}
	task, exists := project.Verified.State.Tasks[taskID]
	if !exists {
		return nil, localstate.Worktree{}, state.TaskState{}, contracts.Task{}, gitstore.Runner{}, protocol.NewError(protocol.ErrReferenceNotFound, protocol.CategoryValidation, "Task does not exist.")
	}
	worktree, exists := project.LocalProject.Worktrees[taskID]
	if !exists {
		return nil, localstate.Worktree{}, state.TaskState{}, contracts.Task{}, gitstore.Runner{}, protocol.NewError(protocol.ErrWorktreeNotFound, protocol.CategoryLocal, "Managed Task worktree is not registered.")
	}
	_, contract, err := taskResources(project, taskID)
	if err != nil {
		return nil, localstate.Worktree{}, state.TaskState{}, contracts.Task{}, gitstore.Runner{}, err
	}
	return project, worktree, task, contract, gitstore.New(worktree.Path), nil
}

func workingChangedPaths(ctx context.Context, runner gitstore.Runner) ([]string, error) {
	tracked, err := runner.Run(ctx, "diff", "--name-only", "-z", "HEAD")
	if err != nil {
		return nil, err
	}
	untracked, err := runner.Run(ctx, "ls-files", "--others", "--exclude-standard", "-z")
	if err != nil {
		return nil, err
	}
	return sortedUniquePaths(append(splitNUL(tracked.Stdout), splitNUL(untracked.Stdout)...)), nil
}

func splitNUL(data []byte) []string {
	result := make([]string, 0)
	for _, value := range bytes.Split(data, []byte{0}) {
		if len(value) > 0 {
			result = append(result, string(value))
		}
	}
	return result
}

func sortedUniquePaths(values []string) []string {
	result := append([]string(nil), values...)
	sort.Slice(result, func(i, j int) bool { return bytes.Compare([]byte(result[i]), []byte(result[j])) < 0 })
	write := 0
	for _, value := range result {
		if write == 0 || result[write-1] != value {
			result[write] = value
			write++
		}
	}
	return result[:write]
}

func scopeViolations(paths, writes []string) []string {
	scopes := make([]contracts.PathScope, 0, len(writes))
	for _, value := range writes {
		scope, err := contracts.ParsePathScope(value)
		if err == nil {
			scopes = append(scopes, scope)
		}
	}
	violations := make([]string, 0)
	for _, path := range paths {
		if contracts.ValidateRepoPath(path) != nil || contracts.IsProtectedPath(path) ||
			!contracts.PathWithinAny(path, scopes) {
			violations = append(violations, path)
		}
	}
	return violations
}

func validateWorkSymlink(root, path string) error {
	absolute := filepath.Join(root, filepath.FromSlash(path))
	info, err := os.Lstat(absolute)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink == 0 {
		return nil
	}
	target, err := os.Readlink(absolute)
	if err != nil {
		return err
	}
	if symlinkTargetEscapesRepository(target) {
		return protocol.NewError(protocol.ErrScopeViolation, protocol.CategoryValidation, "Work symlink escapes the managed worktree.")
	}
	return nil
}

func taskActorKey(project *projectContext, actor string) (string, string, string, error) {
	type candidate struct {
		public, path, fingerprint, keyID string
	}
	candidates := make([]candidate, 0)
	for _, grant := range project.Verified.State.Authority.Grants {
		if grant.Actor != actor {
			continue
		}
		key, exists := project.LocalProject.Identity.Keys[grant.KeyID]
		if !exists {
			continue
		}
		public, fingerprint, err := cryptoutil.PublicFromHandle(key.PrivateKeyHandle)
		if err != nil || public != grant.PublicKey {
			continue
		}
		path, err := cryptoutil.ResolveFileHandle(key.PrivateKeyHandle)
		if err != nil {
			continue
		}
		candidates = append(candidates, candidate{public: public, path: path, fingerprint: fingerprint, keyID: grant.KeyID})
	}
	if len(candidates) == 0 {
		return "", "", "", protocol.NewError(protocol.ErrSecretKeyNotFound, protocol.CategoryLocal, "No local current key matches the Task actor.")
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].keyID < candidates[j].keyID })
	selected := candidates[0]
	for _, candidate := range candidates {
		if candidate.keyID == project.LocalProject.Identity.SelectedKeyID {
			selected = candidate
			break
		}
	}
	return selected.public, selected.path, selected.fingerprint, nil
}

func runTaskChecks(ctx context.Context, project *projectContext, runner gitstore.Runner, taskID string, task state.TaskState, contract contracts.Task) ([]workflow.CheckResult, []string, error) {
	working, err := workingChangedPaths(ctx, runner)
	if err != nil {
		return nil, nil, err
	}
	if len(working) != 0 {
		return nil, nil, protocol.NewError(protocol.ErrWorktreeDirty, protocol.CategoryLocal, "Checks require a clean managed worktree.")
	}
	head, err := runner.Resolve(ctx, "HEAD")
	if err != nil {
		return nil, nil, err
	}
	headCommit, err := project.Runner.ReadCommit(ctx, head)
	if err != nil {
		return nil, nil, err
	}
	baseTree, err := project.Runner.ReadTree(ctx, task.Base)
	if err != nil {
		return nil, nil, err
	}
	workTree, err := project.Runner.ReadTree(ctx, headCommit.Tree)
	if err != nil {
		return nil, nil, err
	}
	changed := gitstore.ChangedPaths(baseTree, workTree)
	if violations := scopeViolations(changed, contract.Writes); len(violations) > 0 {
		return nil, nil, &protocol.Error{
			Code: protocol.ErrScopeViolation, Category: protocol.CategoryValidation,
			Message: "Work Head contains paths outside the frozen Task Contract.",
			Details: map[string]any{"paths": violations},
		}
	}
	if contract.ChangeLimits != nil && int64(len(changed)) > contract.ChangeLimits.MaxChangedPaths {
		return nil, nil, protocol.NewError(protocol.ErrLimitExceeded, protocol.CategoryAuthorization, "Task max_changed_paths is exceeded.")
	}
	taskValue := taskID
	results, _, err := workflow.RunChecks(ctx, runner.Repo, contract.Checks, workflow.CheckBinding{
		ArchitectureBlob: task.Contract.ArchitectureBlob, Head: head,
		Phase: "submission", Task: &taskValue,
		TaskbookBlob: task.Contract.TaskbookBlob, Tree: headCommit.Tree,
	})
	return results, changed, err
}

func allChecksPassed(results []workflow.CheckResult) bool {
	for _, result := range results {
		if result.Status != "pass" {
			return false
		}
	}
	return true
}
