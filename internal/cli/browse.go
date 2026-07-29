package cli

import (
	"bytes"
	"context"
	"encoding/base64"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/ExplodeCode6324/chassiss/internal/contracts"
	"github.com/ExplodeCode6324/chassiss/internal/localstate"
	"github.com/ExplodeCode6324/chassiss/internal/protocol"
	"github.com/ExplodeCode6324/chassiss/internal/state"
	"github.com/ExplodeCode6324/chassiss/internal/verifier"
)

func browseCommand(ctx context.Context, invocation invocation) (Envelope, error) {
	switch invocation.Definition.Path {
	case "context":
		return contextCommand(ctx, invocation)
	case "log":
		return logCommand(ctx, invocation)
	case "file show":
		return fileShowCommand(ctx, invocation)
	case "remote show":
		return remoteShowCommand(ctx)
	case "remote set":
		return remoteSetCommand(ctx, invocation)
	case "task list":
		return taskListCommand(ctx, invocation)
	case "task show":
		return taskShowCommand(ctx, invocation)
	default:
		panic("unreachable")
	}
}

func contextCommand(ctx context.Context, invocation invocation) (Envelope, error) {
	project, err := loadProject(ctx, "", false)
	if err != nil {
		return Envelope{}, err
	}
	result := map[string]any{
		"architecture": project.Verified.Architecture,
		"identity":     project.Identity,
		"project":      project.Verified.State.Project,
	}
	if project.Verified.Taskbook != nil {
		result["taskbook"] = project.Verified.Taskbook
	}
	if len(invocation.Positionals) == 1 {
		taskID := invocation.Positionals[0]
		runtimeTask, exists := project.Verified.State.Tasks[taskID]
		if !exists || project.Verified.Taskbook == nil {
			return Envelope{}, protocol.NewError(protocol.ErrReferenceNotFound, protocol.CategoryValidation, "Task does not exist.")
		}
		contract := project.Verified.Taskbook.Tasks[taskID]
		closure, err := project.Verified.Architecture.RequiresClosure(
			append(append([]string(nil), contract.Modules...), contract.Affects...),
		)
		if err != nil {
			return Envelope{}, err
		}
		resourceSlice := make(map[string]contracts.Resource, len(closure))
		for _, id := range closure {
			resourceSlice[id] = project.Verified.Architecture.Resources()[id]
		}
		result = map[string]any{
			"contract": contract, "dependencies": contract.DependsOn,
			"resources": resourceSlice, "runtime": runtimeTask, "task": taskID,
			"worktree": project.LocalProject.Worktrees[taskID],
		}
	}
	if resourceID := invocation.Value("resource"); resourceID != "" {
		resource, exists := project.Verified.Architecture.Resources()[resourceID]
		if !exists {
			return Envelope{}, protocol.NewError(protocol.ErrReferenceNotFound, protocol.CategoryValidation, "Architecture Resource does not exist.")
		}
		result["resource"] = map[string]any{"id": resourceID, "value": resource}
	}
	if section := invocation.Value("section"); section != "" {
		value, exists := result[section]
		if !exists {
			return Envelope{}, protocol.NewError(protocol.ErrReferenceNotFound, protocol.CategoryValidation, "Requested context section does not exist.")
		}
		result = map[string]any{"section": section, "value": value}
	}
	envelope := projectEnvelope("context", project)
	envelope.Snapshot.Offline = invocation.Flags["offline"]
	envelope.Result = result
	return envelope, nil
}

func logCommand(ctx context.Context, invocation invocation) (Envelope, error) {
	project, err := loadProject(ctx, "", true)
	if err != nil {
		return Envelope{}, err
	}
	limit := 100
	if raw := invocation.Value("limit"); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value < 1 || value > 100_000 {
			return Envelope{}, usageError("--limit must be in [1,100000]")
		}
		limit = value
	}
	records := make([]verifier.Transition, 0)
	for index := len(project.Verified.Transitions) - 1; index >= 0 && len(records) < limit; index-- {
		transition := project.Verified.Transitions[index]
		if value := invocation.Value("task"); value != "" && transition.Target != value {
			continue
		}
		if value := invocation.Value("action"); value != "" && transition.Action != value {
			continue
		}
		if value := invocation.Value("operation"); value != "" && transition.OperationID != value {
			continue
		}
		records = append(records, transition)
	}
	envelope := projectEnvelope("log", project)
	envelope.Result = map[string]any{"transitions": records}
	return envelope, nil
}

func fileShowCommand(ctx context.Context, invocation invocation) (Envelope, error) {
	project, err := loadProject(ctx, "", false)
	if err != nil {
		return Envelope{}, err
	}
	path := invocation.Positionals[0]
	if err := contracts.ValidateRepoPath(path); err != nil {
		return Envelope{}, usageError(err.Error())
	}
	at := invocation.Value("at")
	if at == "" {
		at = "main"
	}
	revision := project.Verified.Head
	taskID := invocation.Value("task")
	switch at {
	case "main":
	case "task-base", "work-head":
		task, exists := project.Verified.State.Tasks[taskID]
		if taskID == "" || !exists {
			return Envelope{}, usageError("--at task-base/work-head requires --task")
		}
		if at == "task-base" {
			revision = task.Base
		} else if task.Attempt != nil {
			revision = task.Attempt.Head
		} else if worktree, exists := project.LocalProject.Worktrees[taskID]; exists {
			revision = worktree.Head
		} else {
			return Envelope{}, protocol.NewError(protocol.ErrWorktreeNotFound, protocol.CategoryLocal, "Task has no current Work Head.")
		}
	default:
		return Envelope{}, usageError("--at must be main, task-base, or work-head")
	}
	commit, err := project.Runner.ReadCommit(ctx, revision)
	if err != nil {
		return Envelope{}, err
	}
	tree, err := project.Runner.ReadTree(ctx, commit.Tree)
	if err != nil {
		return Envelope{}, err
	}
	entry, exists := tree[path]
	if !exists || entry.Mode == "160000" {
		return Envelope{}, protocol.NewError(protocol.ErrPathNotFound, protocol.CategoryValidation, "Path is not a readable blob in the selected verified tree.")
	}
	data, err := project.Runner.ReadBlob(ctx, entry.OID)
	if err != nil {
		return Envelope{}, err
	}
	encoding, content := "utf-8", string(data)
	if !utf8.Valid(data) || bytes.IndexByte(data, 0) >= 0 {
		encoding, content = "base64", base64.StdEncoding.EncodeToString(data)
	}
	if output := invocation.Value("output"); output != "" {
		if err := requireOutsideProject(project.RepoRoot, output); err != nil {
			return Envelope{}, err
		}
		if err := writeExternalFile(output, data); err != nil {
			return Envelope{}, err
		}
	}
	envelope := projectEnvelope("file show", project)
	envelope.Result = map[string]any{
		"blob": entry.OID, "content": content, "encoding": encoding,
		"mode": entry.Mode, "path": path, "revision": revision, "size": len(data),
	}
	return envelope, nil
}

func remoteShowCommand(ctx context.Context) (Envelope, error) {
	project, err := loadProject(ctx, "", false)
	if err != nil {
		return Envelope{}, err
	}
	reachable := false
	remoteHead := ""
	if project.LocalProject.Remote.URL != "" {
		result, runErr := project.Runner.Run(ctx, "ls-remote", "--heads", "origin", "refs/heads/main")
		reachable = runErr == nil
		if reachable {
			fields := strings.Fields(string(result.Stdout))
			if len(fields) > 0 {
				remoteHead = fields[0]
			}
		}
	}
	envelope := projectEnvelope("remote show", project)
	envelope.Result = map[string]any{
		"fetch_reachable": reachable, "main": remoteHead,
		"project":        project.Verified.State.Project.ID,
		"push_reachable": reachable, "root_fingerprint": project.Verified.RootFingerprint,
		"url":             project.LocalProject.Remote.URL,
		"url_fingerprint": project.LocalProject.Remote.URLFingerprint,
	}
	return envelope, nil
}

func remoteSetCommand(ctx context.Context, invocation invocation) (Envelope, error) {
	project, err := loadProject(ctx, "", false)
	if err != nil {
		return Envelope{}, err
	}
	url := invocation.Positionals[0]
	if err := validateRemoteURL(url); err != nil {
		return Envelope{}, usageError(err.Error())
	}
	tempID, err := protocol.NewOperationID()
	if err != nil {
		return Envelope{}, err
	}
	tempRef := "refs/chassiss/tmp/remote-set/" + strings.TrimPrefix(tempID, "OPR-")
	defer project.Runner.Run(ctx, "update-ref", "-d", tempRef)
	if _, err := project.Runner.Run(ctx, "fetch", "--no-tags", url, "+refs/heads/main:"+tempRef); err != nil {
		return Envelope{}, protocol.WrapError(protocol.ErrRemoteUnreachable, protocol.CategoryNetwork, "Candidate upstream main cannot be fetched.", err)
	}
	remote, err := verifier.Verify(ctx, project.Runner, tempRef, verifier.Options{
		ExpectedProject:         project.Verified.State.Project.ID,
		ExpectedRootFingerprint: project.LocalProject.RootFingerprint,
		MinimumCheckpoint:       project.LocalProject.MinimumCheckpoint.Commit,
	})
	if err != nil {
		return Envelope{}, err
	}
	if err := verifyRemoteRetainedRefs(ctx, project.Runner, url, remote); err != nil {
		return Envelope{}, err
	}
	if _, err := project.Runner.Run(ctx, "remote", "get-url", "origin"); err == nil {
		_, err = project.Runner.Run(ctx, "remote", "set-url", "origin", url)
	} else {
		_, err = project.Runner.Run(ctx, "remote", "add", "origin", url)
	}
	if err != nil {
		return Envelope{}, err
	}
	fingerprint := protocol.DigestBytes([]byte(url))
	if err := project.Store.Update(func(local *localstate.State) error {
		value := local.Projects[project.Verified.State.Project.ID]
		value.Remote = localstate.Remote{URL: url, URLFingerprint: fingerprint}
		local.Projects[project.Verified.State.Project.ID] = value
		return nil
	}); err != nil {
		return Envelope{}, err
	}
	envelope := projectEnvelope("remote set", project)
	envelope.Result = map[string]any{
		"main": remote.Head, "url": url, "url_fingerprint": fingerprint,
	}
	return envelope, nil
}

func taskListCommand(ctx context.Context, invocation invocation) (Envelope, error) {
	project, err := loadProject(ctx, "", false)
	if err != nil {
		return Envelope{}, err
	}
	ids := make([]string, 0, len(project.Verified.State.Tasks))
	for id := range project.Verified.State.Tasks {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	tasks := make([]map[string]any, 0)
	for _, id := range ids {
		runtimeTask := project.Verified.State.Tasks[id]
		if phase := invocation.Value("phase"); phase != "" && runtimeTask.Phase != phase {
			continue
		}
		if actor := invocation.Value("actor"); actor != "" && runtimeTask.Actor != actor {
			continue
		}
		available := taskAvailable(project, id)
		if invocation.Flags["available"] && !available {
			continue
		}
		tasks = append(tasks, map[string]any{
			"actor": runtimeTask.Actor, "available": available,
			"blocked": runtimeTask.Blocked != nil, "phase": runtimeTask.Phase, "task": id,
		})
	}
	envelope := projectEnvelope("task list", project)
	envelope.Result = map[string]any{"tasks": tasks}
	return envelope, nil
}

func taskShowCommand(ctx context.Context, invocation invocation) (Envelope, error) {
	project, err := loadProject(ctx, "", invocation.Flags["history"])
	if err != nil {
		return Envelope{}, err
	}
	taskID := invocation.Positionals[0]
	runtimeTask, exists := project.Verified.State.Tasks[taskID]
	if !exists || project.Verified.Taskbook == nil {
		return Envelope{}, protocol.NewError(protocol.ErrReferenceNotFound, protocol.CategoryValidation, "Task does not exist.")
	}
	result := map[string]any{
		"available": taskAvailable(project, taskID),
		"contract":  project.Verified.Taskbook.Tasks[taskID],
		"runtime":   runtimeTask, "task": taskID,
	}
	if invocation.Flags["history"] {
		history := make([]verifier.Transition, 0)
		for _, transition := range project.Verified.Transitions {
			if transition.Target == taskID {
				history = append(history, transition)
			}
		}
		result["history"] = history
	}
	envelope := projectEnvelope("task show", project)
	envelope.Result = result
	return envelope, nil
}

func taskAvailable(project *projectContext, taskID string) bool {
	runtimeTask := project.Verified.State.Tasks[taskID]
	if runtimeTask.Phase != "ready" || runtimeTask.Blocked != nil || project.Verified.Taskbook == nil {
		return false
	}
	contract := project.Verified.Taskbook.Tasks[taskID]
	for _, dependency := range contract.DependsOn {
		if project.Verified.State.Tasks[dependency].Phase != "closed" {
			return false
		}
	}
	for otherID, other := range project.Verified.State.Tasks {
		if otherID == taskID || (other.Phase != "active" && other.Phase != "submitted" && other.Phase != "approved") {
			continue
		}
		conflict, err := contracts.Conflict(
			project.Verified.Architecture, taskID, contract,
			otherID, project.Verified.Taskbook.Tasks[otherID],
		)
		if err != nil || conflict.Conflict {
			return false
		}
	}
	return true
}

func requireOutsideProject(root, output string) error {
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	outputAbs, err := filepath.Abs(output)
	if err != nil {
		return err
	}
	relative, err := filepath.Rel(rootAbs, outputAbs)
	if err != nil || relative == "." || (relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))) {
		return protocol.NewError(protocol.ErrScopeViolation, protocol.CategoryLocal, "Output path must be outside the managed Project worktree.")
	}
	return nil
}

func textualDiff(oldPath string, oldData []byte, newPath string, newData []byte) (string, error) {
	temp, err := os.MkdirTemp("", "chassiss-diff-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(temp)
	left := filepath.Join(temp, "old")
	right := filepath.Join(temp, "new")
	if err := os.WriteFile(left, oldData, 0o600); err != nil {
		return "", err
	}
	if err := os.WriteFile(right, newData, 0o600); err != nil {
		return "", err
	}
	command := exec.Command("git", "diff", "--no-index", "--no-ext-diff", "--no-color", "--", left, right)
	output, runErr := command.CombinedOutput()
	if exit, ok := runErr.(*exec.ExitError); ok && exit.ExitCode() == 1 {
		runErr = nil
	}
	diff := strings.ReplaceAll(string(output), left, "a/"+oldPath)
	diff = strings.ReplaceAll(diff, right, "b/"+newPath)
	return diff, runErr
}

var _ = state.TaskState{}
