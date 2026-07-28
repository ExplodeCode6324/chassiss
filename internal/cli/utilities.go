package cli

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ExplodeCode6324/chassiss/internal/contracts"
	"github.com/ExplodeCode6324/chassiss/internal/gitstore"
	"github.com/ExplodeCode6324/chassiss/internal/localstate"
	"github.com/ExplodeCode6324/chassiss/internal/protocol"
	"github.com/ExplodeCode6324/chassiss/internal/state"
)

func utilityCommand(ctx context.Context, invocation invocation) (Envelope, error) {
	switch invocation.Definition.Path {
	case "owner apply":
		return ownerApplyCommand(ctx, invocation)
	case "export":
		return exportCommand(ctx, invocation)
	case "cache clean":
		return cacheCleanCommand(invocation)
	default:
		panic("unreachable")
	}
}

func ownerApplyCommand(ctx context.Context, invocation invocation) (Envelope, error) {
	project, err := loadMutableProject(ctx)
	if err != nil {
		return Envelope{}, err
	}
	for _, worktree := range project.LocalProject.Worktrees {
		if samePath(worktree.Path, project.RepoRoot) {
			return Envelope{}, protocol.NewError(protocol.ErrOwnerWorkflowActive, protocol.CategoryLocal, "Owner Apply cannot run inside a managed Task worktree.")
		}
	}
	if len(project.LocalProject.Worktrees) != 0 || len(project.LocalProject.PendingOperations) != 0 {
		return Envelope{}, protocol.NewError(protocol.ErrOwnerWorkflowActive, protocol.CategoryLocal, "Owner Apply requires no managed/orphan worktree and no pending Operation.")
	}
	for _, task := range project.Verified.State.Tasks {
		if task.Phase == "active" || task.Phase == "submitted" || task.Phase == "approved" {
			return Envelope{}, protocol.NewError(protocol.ErrOwnerWorkflowActive, protocol.CategoryValidation, "Owner Apply requires no active Agent workflow.")
		}
	}
	cached, err := project.Runner.Run(ctx, "diff", "--cached", "--name-only", "-z", project.Verified.Head)
	if err != nil {
		return Envelope{}, err
	}
	_ = cached // Staged changes are included in the exact working snapshot below.
	changed, err := workingChangedPaths(ctx, project.Runner)
	if err != nil {
		return Envelope{}, err
	}
	if len(changed) == 0 {
		return Envelope{}, protocol.NewError(protocol.ErrUsageInvalid, protocol.CategoryValidation, "Owner Apply has no ordinary file changes.")
	}
	for _, path := range changed {
		if contracts.IsProtectedPath(path) {
			return Envelope{}, protocol.NewError(protocol.ErrProtectedPathChanged, protocol.CategoryValidation, "Owner Apply cannot change protocol-protected paths.")
		}
		if err := contracts.ValidateRepoPath(path); err != nil {
			return Envelope{}, err
		}
		if err := validateWorkSymlink(project.RepoRoot, path); err != nil {
			return Envelope{}, err
		}
	}
	untrackedResult, err := project.Runner.Run(ctx, "ls-files", "--others", "--exclude-standard", "-z")
	if err != nil {
		return Envelope{}, err
	}
	untracked := splitNUL(untrackedResult.Stdout)
	if _, err := project.Runner.Run(ctx, "read-tree", project.Verified.Head); err != nil {
		return Envelope{}, err
	}
	addArgs := append([]string{"add", "-A", "--"}, changed...)
	if _, err := project.Runner.Run(ctx, addArgs...); err != nil {
		return Envelope{}, err
	}
	sourceResult, err := project.Runner.Run(ctx, "write-tree")
	if err != nil {
		return Envelope{}, err
	}
	sourceTreeOID := stringTrimSpace(sourceResult.Stdout)
	sourceTree, err := project.Runner.ReadTree(ctx, sourceTreeOID)
	if err != nil {
		return Envelope{}, err
	}
	parentCommit, err := project.Runner.ReadCommit(ctx, project.Verified.Head)
	if err != nil {
		return Envelope{}, err
	}
	parentTree, err := project.Runner.ReadTree(ctx, parentCommit.Tree)
	if err != nil {
		return Envelope{}, err
	}
	candidate, overlaid, err := gitstore.Overlay(parentTree, sourceTree, parentTree)
	if err != nil {
		return Envelope{}, protocol.WrapError(protocol.ErrCandidateConflict, protocol.CategoryConflict, "Owner candidate overlay failed.", err)
	}
	if !sameStrings(changed, overlaid) {
		return Envelope{}, protocol.NewError(protocol.ErrEvidenceInvalid, protocol.CategoryProtocol, "Owner working snapshot changed-path set is inconsistent.")
	}
	candidateTree, err := project.Runner.WriteTree(ctx, candidate)
	if err != nil {
		return Envelope{}, err
	}
	if _, err := project.Runner.Run(ctx, "read-tree", project.Verified.Head); err != nil {
		return Envelope{}, err
	}
	warning := Warning{
		Code:    "CHS_WARN_OWNER_APPLY",
		Message: "Owner Apply will publish the exact ordinary working-tree snapshot.",
		Details: map[string]any{"candidate_tree": candidateTree, "changed_paths": changed},
	}
	if !invocation.Flags["yes"] {
		envelope := projectEnvelope("owner apply", project)
		envelope.Warnings = append(envelope.Warnings, warning)
		envelope.Result = map[string]any{
			"candidate_tree": candidateTree, "changed_paths": changed,
			"published": false, "requires_yes": true,
		}
		return envelope, nil
	}
	authority, err := selectGrant(project, invocation, "owner.apply", "", nil, true)
	if err != nil {
		return Envelope{}, err
	}
	changedDigest, _ := protocol.ObjectDigest("changed-paths", changed)
	operationID, err := operationID(invocation)
	if err != nil {
		return Envelope{}, err
	}
	operation := protocol.Operation{
		Schema: protocol.OperationSchema, OperationID: operationID,
		Action: "owner.applied", Project: project.Verified.State.Project.ID,
		Authority: authority.Reference, Target: project.Verified.State.Project.ID,
		Preconditions: map[string]any{
			"no_active_agent_workflow": true, "source_base": project.Verified.Head,
			"source_tree": sourceTreeOID,
		},
		Payload: map[string]any{
			"reason": invocation.Value("reason"), "summary": invocation.Value("summary"),
		},
	}
	parent := project.Verified.Head
	operationDigest, _ := protocol.ObjectDigest("operation", operation)
	evidence := protocol.ExecutionEvidence{
		Schema: protocol.EvidenceSchema, OperationDigest: operationDigest,
		Action: operation.Action, Attempt: 1, Parent: &parent,
		Facts: map[string]any{
			"candidate_tree": candidateTree, "changed_paths": stringSliceAny(changed),
			"changed_paths_digest": changedDigest, "source_base": parent,
			"source_tree": sourceTreeOID,
		},
	}
	reduceFacts := state.ReduceFacts{
		ObjectFormat: project.Verified.ObjectFormat, ParentCommit: parent,
		SignerFingerprint: authority.Fingerprint, RequireGlobalScope: true,
	}
	next, err := state.Reduce(project.Verified.State, operation, evidence, reduceFacts)
	if err != nil {
		return Envelope{}, err
	}
	for _, path := range untracked {
		absolute := filepath.Join(project.RepoRoot, filepath.FromSlash(path))
		if info, err := os.Lstat(absolute); err == nil && !info.IsDir() {
			if err := os.Remove(absolute); err != nil {
				return Envelope{}, err
			}
		}
	}
	if _, err := project.Runner.Run(ctx, "read-tree", "--reset", "-u", project.Verified.Head); err != nil {
		return Envelope{}, err
	}
	envelope, err := publishTransition(ctx, project, transitionPlan{
		Operation: operation, Evidence: evidence, NextState: next, Tree: candidate,
		ReduceFacts: reduceFacts, Authority: authority, Warnings: []Warning{warning},
		Result: map[string]any{
			"candidate_tree": candidateTree, "changed_paths": changed, "published": true,
		},
	})
	return envelope, err
}

func exportCommand(ctx context.Context, invocation invocation) (Envelope, error) {
	target := invocation.Value("ref")
	project, err := loadProject(ctx, target, true)
	if err != nil {
		return Envelope{}, err
	}
	format := invocation.Value("format")
	if format == "" {
		format = "json"
	}
	exported := map[string]any{
		"architecture": project.Verified.Architecture,
		"head":         project.Verified.Head, "project": project.Verified.State.Project.ID,
		"protocol": protocol.ProtocolID, "root_fingerprint": project.Verified.RootFingerprint,
		"state": project.Verified.State, "state_digest": project.Verified.StateDigest,
		"taskbook": project.Verified.Taskbook, "transitions": project.Verified.Transitions,
		"warning": "Non-authoritative read-only export; do not import as protocol State.",
	}
	jsonData, err := protocol.CanonicalJSON(exported)
	if err != nil {
		return Envelope{}, err
	}
	var data []byte
	switch format {
	case "json":
		data = append(jsonData, '\n')
	case "markdown":
		data = []byte(fmt.Sprintf(
			"# CHASSISS read-only export\n\n- Project: `%s`\n- Head: `%s`\n- State digest: `%s`\n- Trust: verified\n\n```json\n%s\n```\n",
			project.Verified.State.Project.ID, project.Verified.Head,
			project.Verified.StateDigest, jsonData,
		))
	default:
		return Envelope{}, usageError("--format must be json or markdown")
	}
	output := invocation.Value("output")
	if output != "" {
		if err := requireOutsideProject(project.RepoRoot, output); err != nil {
			return Envelope{}, err
		}
		if err := writeExternalFile(output, data); err != nil {
			return Envelope{}, err
		}
	}
	envelope := projectEnvelope("export", project)
	envelope.Result = map[string]any{
		"content": string(data), "format": format, "output": output,
		"read_only": true,
	}
	return envelope, nil
}

func cacheCleanCommand(invocation invocation) (Envelope, error) {
	store, err := localstate.OpenDefault()
	if err != nil {
		return Envelope{}, err
	}
	kind := invocation.Value("kind")
	if kind == "" {
		kind = "all"
	}
	validKinds := map[string]bool{
		"history": true, "architecture": true, "taskbook": true,
		"candidate": true, "checks": true, "all": true,
	}
	if !validKinds[kind] {
		return Envelope{}, usageError("--kind is not a recognized rebuildable cache")
	}
	kinds := []string{kind}
	if kind == "all" {
		kinds = []string{"history", "architecture", "taskbook", "candidate", "checks"}
	}
	projectID := invocation.Value("project")
	if projectID != "" {
		if err := protocol.ValidateID(protocol.IDProject, projectID); err != nil {
			return Envelope{}, usageError(err.Error())
		}
	}
	targets := make([]string, 0, len(kinds))
	for _, entry := range kinds {
		target := filepath.Join(store.Paths.Cache, entry)
		if projectID != "" {
			target = filepath.Join(target, projectID)
		}
		if !pathWithin(store.Paths.Cache, target) || target == store.Paths.Cache {
			return Envelope{}, protocol.NewError(protocol.ErrScopeViolation, protocol.CategoryLocal, "Cache target escaped the cache root.")
		}
		targets = append(targets, target)
	}
	envelope := baseEnvelope("cache clean")
	if !invocation.Flags["yes"] {
		envelope.Result = map[string]any{"removed": false, "requires_yes": true, "targets": targets}
		return envelope, nil
	}
	removed := make([]string, 0)
	for _, target := range targets {
		if _, err := os.Lstat(target); os.IsNotExist(err) {
			continue
		} else if err != nil {
			return Envelope{}, err
		}
		if err := os.RemoveAll(target); err != nil {
			return Envelope{}, err
		}
		removed = append(removed, target)
	}
	envelope.Result = map[string]any{"removed": true, "targets": removed}
	return envelope, nil
}

func sameStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	a, b := append([]string(nil), left...), append([]string(nil), right...)
	sort.Slice(a, func(i, j int) bool { return bytes.Compare([]byte(a[i]), []byte(a[j])) < 0 })
	sort.Slice(b, func(i, j int) bool { return bytes.Compare([]byte(b[i]), []byte(b[j])) < 0 })
	return strings.Join(a, "\x00") == strings.Join(b, "\x00")
}
