package cli

import (
	"context"
	"os"
	"sort"

	"github.com/ExplodeCode6324/chassiss/internal/contracts"
	"github.com/ExplodeCode6324/chassiss/internal/protocol"
)

func verifyCommand(ctx context.Context, invocation invocation) (Envelope, error) {
	if invocation.Value("commit") != "" && invocation.Value("ref") != "" {
		return Envelope{}, usageError("verify accepts only one of --commit and --ref")
	}
	target := invocation.Value("commit")
	if target == "" {
		target = invocation.Value("ref")
	}
	context, err := loadProject(ctx, target, invocation.Flags["full"])
	if err != nil {
		return Envelope{}, err
	}
	envelope := projectEnvelope("verify", context)
	envelope.Result = map[string]any{
		"commits_verified": len(context.Verified.Transitions),
		"genesis":          context.Verified.Genesis, "head": context.Verified.Head,
		"state_digest": context.Verified.StateDigest,
	}
	return envelope, nil
}

func statusCommand(ctx context.Context, invocation invocation) (Envelope, error) {
	context, err := loadProject(ctx, "", false)
	if err != nil {
		return Envelope{}, err
	}
	envelope := projectEnvelope("status", context)
	envelope.Snapshot.Offline = invocation.Flags["offline"]
	tasks := make(map[string]any)
	for id, task := range context.Verified.State.Tasks {
		if filter := invocation.Value("task"); filter != "" && filter != id {
			continue
		}
		tasks[id] = task
	}
	envelope.Result = map[string]any{
		"pending_operations": context.LocalProject.PendingOperations,
		"tasks":              tasks, "worktrees": context.LocalProject.Worktrees,
	}
	return envelope, nil
}

func architectureValidateCommand(ctx context.Context, invocation invocation) (Envelope, error) {
	var data []byte
	var err error
	if file := invocation.Value("file"); file != "" {
		data, err = os.ReadFile(file)
	} else {
		project, loadErr := loadProject(ctx, "", false)
		if loadErr != nil {
			return Envelope{}, loadErr
		}
		if project.Verified.State.Project.Architecture == nil {
			return Envelope{}, protocol.NewError(protocol.ErrArchitectureNotEstablished, protocol.CategoryValidation, "Architecture is not established.")
		}
		data, err = project.Runner.ReadBlob(ctx, project.Verified.State.Project.Architecture.BlobOID)
	}
	if err != nil {
		return Envelope{}, protocol.WrapError(protocol.ErrPathNotFound, protocol.CategoryLocal, "Cannot read Architecture.", err)
	}
	architecture, err := contracts.ParseArchitecture(data)
	if err != nil {
		return Envelope{}, protocol.WrapError(protocol.ErrArchitectureInvalid, protocol.CategoryValidation, "Architecture is invalid.", err)
	}
	resources := make([]string, 0, len(architecture.Resources()))
	for id := range architecture.Resources() {
		resources = append(resources, id)
	}
	sort.Strings(resources)
	envelope := baseEnvelope("architecture validate")
	envelope.Result = map[string]any{
		"id": architecture.ID, "resources": resources, "valid": true,
	}
	return envelope, nil
}

func taskbookValidateCommand(ctx context.Context, invocation invocation) (Envelope, error) {
	project, err := loadProject(ctx, "", false)
	if err != nil {
		return Envelope{}, err
	}
	if project.Verified.State.Project.Architecture == nil || project.Verified.Architecture == nil {
		return Envelope{}, protocol.NewError(protocol.ErrArchitectureNotEstablished, protocol.CategoryValidation, "Establish Architecture before validating a Taskbook.")
	}
	data, err := os.ReadFile(invocation.Value("file"))
	if err != nil {
		return Envelope{}, protocol.WrapError(protocol.ErrPathNotFound, protocol.CategoryLocal, "Cannot read Taskbook candidate.", err)
	}
	taskbook, err := contracts.ParseTaskbook(data, project.Verified.Architecture)
	if err != nil {
		return Envelope{}, protocol.WrapError(protocol.ErrTaskbookInvalid, protocol.CategoryValidation, "Taskbook candidate is invalid.", err)
	}
	tasks := make([]string, 0, len(taskbook.Tasks))
	for id := range taskbook.Tasks {
		tasks = append(tasks, id)
	}
	sort.Strings(tasks)
	envelope := projectEnvelope("taskbook validate", project)
	envelope.Result = map[string]any{"id": taskbook.ID, "tasks": tasks, "valid": true}
	return envelope, nil
}
