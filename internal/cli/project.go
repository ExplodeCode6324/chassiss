package cli

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ExplodeCode6324/chassiss/internal/cryptoutil"
	"github.com/ExplodeCode6324/chassiss/internal/gitstore"
	"github.com/ExplodeCode6324/chassiss/internal/localstate"
	"github.com/ExplodeCode6324/chassiss/internal/protocol"
	"github.com/ExplodeCode6324/chassiss/internal/state"
	"github.com/ExplodeCode6324/chassiss/internal/verifier"
)

type projectContext struct {
	InvocationRoot string
	RepoRoot       string
	Runner         gitstore.Runner
	Store          localstate.Store
	Local          *localstate.State
	LocalProject   localstate.Project
	Verified       *verifier.Result
	Identity       *IdentityBody
}

type projectLocation struct {
	owner        string
	projectID    string
	localProject localstate.Project
	repoRoot     string
	managed      bool
}

func loadProject(ctx context.Context, target string, full bool) (*projectContext, error) {
	invocationRoot, err := repositoryRoot(ctx)
	if err != nil {
		return nil, err
	}
	store, err := localstate.OpenDefault()
	if err != nil {
		return nil, err
	}
	local, err := store.Load()
	if err != nil {
		return nil, err
	}
	projectID, localProject, repoRoot, err := resolveProjectLocation(ctx, local, invocationRoot)
	if err != nil {
		return nil, err
	}
	if projectID == "" {
		return nil, protocol.NewError(protocol.ErrProjectNotFound, protocol.CategoryLocal, "Current repository is not a registered CHASSISS Project.")
	}
	runner := gitstore.New(repoRoot)
	if target == "" {
		target = "refs/heads/main"
	}
	verified, err := verifier.Verify(ctx, runner, target, verifier.Options{
		ExpectedProject: projectID, ExpectedRootFingerprint: localProject.RootFingerprint,
		MinimumCheckpoint: localProject.MinimumCheckpoint.Commit, Full: full,
	})
	if err != nil {
		return nil, err
	}
	context := &projectContext{
		InvocationRoot: invocationRoot,
		RepoRoot:       repoRoot, Runner: runner, Store: store, Local: local,
		LocalProject: localProject, Verified: verified,
	}
	context.Identity = discoverIdentity(verified.State, localProject)
	return context, nil
}

func resolveProjectLocation(
	ctx context.Context,
	local *localstate.State,
	invocationRoot string,
) (string, localstate.Project, string, error) {
	matches := make([]projectLocation, 0)
	for id, candidate := range local.Projects {
		for _, instance := range candidate.RepoInstances {
			if samePath(instance.RepoPath, invocationRoot) {
				matches = append(matches, projectLocation{
					owner:     id + ":repo:" + filepath.Clean(instance.RepoPath),
					projectID: id, localProject: candidate, repoRoot: instance.RepoPath,
				})
			}
		}
		for taskID, worktree := range candidate.Worktrees {
			if samePath(worktree.Path, invocationRoot) {
				matches = append(matches, projectLocation{
					owner:     id + ":worktree:" + taskID + ":" + filepath.Clean(worktree.Path),
					projectID: id, localProject: candidate, managed: true,
				})
			}
		}
	}
	if len(matches) == 0 {
		return "", localstate.Project{}, "", nil
	}
	sort.Slice(matches, func(i, j int) bool { return matches[i].owner < matches[j].owner })
	if len(matches) != 1 {
		owners := make([]string, len(matches))
		for index := range matches {
			owners[index] = matches[index].owner
		}
		failure := protocol.NewError(
			protocol.ErrLocalStateCorrupt, protocol.CategoryLocal,
			"Current repository path has multiple registered Project owners.",
		)
		failure.Details["owners"] = owners
		return "", localstate.Project{}, "", failure
	}
	selected := matches[0]
	if !selected.managed {
		return selected.projectID, selected.localProject, selected.repoRoot, nil
	}
	commonDir, err := repositoryCommonDir(ctx, invocationRoot)
	if err != nil {
		return "", localstate.Project{}, "", protocol.WrapError(
			protocol.ErrLocalStateCorrupt, protocol.CategoryLocal,
			"Managed worktree Git ownership cannot be resolved.", err,
		)
	}
	owners := make([]localstate.RepoInstance, 0)
	for _, instance := range selected.localProject.RepoInstances {
		if samePath(instance.GitDir, commonDir) {
			owners = append(owners, instance)
		}
	}
	if len(owners) != 1 {
		paths := make([]string, len(owners))
		for index := range owners {
			paths[index] = filepath.Clean(owners[index].RepoPath)
		}
		sort.Strings(paths)
		failure := protocol.NewError(
			protocol.ErrLocalStateCorrupt, protocol.CategoryLocal,
			"Managed worktree does not have exactly one registered repository owner.",
		)
		failure.Details["git_common_dir"] = commonDir
		failure.Details["owners"] = paths
		return "", localstate.Project{}, "", failure
	}
	return selected.projectID, selected.localProject, owners[0].RepoPath, nil
}

func repositoryRoot(ctx context.Context) (string, error) {
	runner := gitstore.New("")
	result, err := runner.Run(ctx, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", protocol.WrapError(protocol.ErrProjectNotFound, protocol.CategoryLocal, "Current directory is not inside a Git repository.", err)
	}
	root := filepath.Clean(stringTrimSpace(result.Stdout))
	return root, nil
}

func repositoryCommonDir(ctx context.Context, repoRoot string) (string, error) {
	result, err := gitstore.New(repoRoot).Run(ctx, "rev-parse", "--git-common-dir")
	if err != nil {
		return "", err
	}
	commonDir := filepath.Clean(stringTrimSpace(result.Stdout))
	if !filepath.IsAbs(commonDir) {
		commonDir = filepath.Join(repoRoot, commonDir)
	}
	return filepath.Abs(commonDir)
}

func requireOutsideProject(project *projectContext, outputs ...string) error {
	roots := projectBoundaryRoots(project.LocalProject)
	if project.InvocationRoot != "" {
		roots = append(roots, project.InvocationRoot)
	}
	return requireOutsideRoots(roots, outputs...)
}

func requireOutsideRegisteredProject(project localstate.Project, outputs ...string) error {
	return requireOutsideRoots(projectBoundaryRoots(project), outputs...)
}

func projectBoundaryRoots(project localstate.Project) []string {
	roots := make([]string, 0, len(project.RepoInstances)+len(project.Worktrees))
	for _, instance := range project.RepoInstances {
		roots = append(roots, instance.RepoPath)
	}
	for _, worktree := range project.Worktrees {
		roots = append(roots, worktree.Path)
	}
	return roots
}

func requireOutsideRoots(roots []string, outputs ...string) error {
	canonicalRoots := make([]string, 0, len(roots))
	for _, root := range roots {
		canonical, err := canonicalPath(root)
		if err != nil {
			return protocol.WrapError(
				protocol.ErrLocalStateCorrupt, protocol.CategoryLocal,
				"Registered Project boundary cannot be safely resolved.", err,
			)
		}
		canonicalRoots = append(canonicalRoots, canonical)
	}
	for _, output := range outputs {
		if output == "" {
			continue
		}
		canonicalOutput, err := canonicalPath(output)
		if err != nil {
			return protocol.WrapError(
				protocol.ErrScopeViolation, protocol.CategoryLocal,
				"Output path cannot be safely resolved outside the Project.", err,
			)
		}
		for _, root := range canonicalRoots {
			contained, err := pathContainedBy(root, canonicalOutput)
			if err != nil {
				return protocol.WrapError(
					protocol.ErrScopeViolation, protocol.CategoryLocal,
					"Output path cannot be safely compared with the Project boundary.", err,
				)
			}
			if contained {
				failure := protocol.NewError(
					protocol.ErrScopeViolation, protocol.CategoryLocal,
					"Output path must be outside every registered Project and managed Task worktree.",
				)
				failure.Details["output"] = canonicalOutput
				failure.Details["project_boundary"] = root
				return failure
			}
		}
	}
	return nil
}

func canonicalPath(path string) (string, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	current := filepath.Clean(absolute)
	suffix := make([]string, 0)
	for {
		if _, err := os.Lstat(current); err == nil {
			resolved, err := filepath.EvalSymlinks(current)
			if err != nil {
				return "", err
			}
			if !filepath.IsAbs(resolved) {
				resolved, err = filepath.Abs(resolved)
				if err != nil {
					return "", err
				}
			}
			for index := len(suffix) - 1; index >= 0; index-- {
				resolved = filepath.Join(resolved, suffix[index])
			}
			return filepath.Clean(resolved), nil
		} else if !os.IsNotExist(err) {
			return "", err
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", os.ErrNotExist
		}
		suffix = append(suffix, filepath.Base(current))
		current = parent
	}
}

func pathContainedBy(root, target string) (bool, error) {
	if !strings.EqualFold(filepath.VolumeName(root), filepath.VolumeName(target)) {
		return false, nil
	}
	relative, err := filepath.Rel(root, target)
	if err != nil {
		return false, err
	}
	return relative == "." ||
		(relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))), nil
}

func discoverIdentity(shared *state.State, local localstate.Project) *IdentityBody {
	type match struct {
		grantID string
		grant   state.Grant
		fp      string
	}
	matches := make([]match, 0)
	for keyID, key := range local.Identity.Keys {
		public, fingerprint, err := cryptoutil.PublicFromHandle(key.PrivateKeyHandle)
		if err != nil {
			continue
		}
		for grantID, grant := range shared.Authority.Grants {
			if grant.KeyID == keyID && grant.PublicKey == public {
				matches = append(matches, match{grantID: grantID, grant: grant, fp: fingerprint})
			}
		}
	}
	if len(matches) == 0 {
		return nil
	}
	sort.Slice(matches, func(i, j int) bool { return matches[i].grantID < matches[j].grantID })
	selected := matches[0]
	if local.Identity.SelectedKeyID != "" {
		for _, candidate := range matches {
			if candidate.grant.KeyID == local.Identity.SelectedKeyID {
				selected = candidate
				break
			}
		}
	}
	limits := map[string]any{"mode": selected.grant.Limits.Mode}
	if selected.grant.Limits.MaxActiveTasks != nil {
		limits["max_active_tasks"] = *selected.grant.Limits.MaxActiveTasks
	}
	if selected.grant.Limits.MaxChangedPaths != nil {
		limits["max_changed_paths"] = *selected.grant.Limits.MaxChangedPaths
	}
	return &IdentityBody{
		Actor: selected.grant.Actor, Capabilities: selected.grant.Capabilities,
		GrantID: selected.grantID, KeyFingerprint: selected.fp, KeyID: selected.grant.KeyID,
		Limits: limits, Scope: map[string]any{
			"resources": selected.grant.Scope.Resources, "tasks": selected.grant.Scope.Tasks,
		},
	}
}

func identityForAuthority(authority selectedAuthority) *IdentityBody {
	if authority.Grant == nil {
		return nil
	}
	limits := map[string]any{"mode": authority.Grant.Limits.Mode}
	if authority.Grant.Limits.MaxActiveTasks != nil {
		limits["max_active_tasks"] = *authority.Grant.Limits.MaxActiveTasks
	}
	if authority.Grant.Limits.MaxChangedPaths != nil {
		limits["max_changed_paths"] = *authority.Grant.Limits.MaxChangedPaths
	}
	return &IdentityBody{
		Actor: authority.Grant.Actor, Capabilities: authority.Grant.Capabilities,
		GrantID: authority.GrantID, KeyFingerprint: authority.Fingerprint,
		KeyID: authority.Grant.KeyID, Limits: limits,
		Scope: map[string]any{
			"resources": authority.Grant.Scope.Resources,
			"tasks":     authority.Grant.Scope.Tasks,
		},
	}
}

func signerForAuthority(authority selectedAuthority) SignerBody {
	signer := SignerBody{
		Authority: authority.Reference, GrantID: authority.GrantID,
		KeyFingerprint: authority.Fingerprint,
		Root:           authority.Grant == nil,
	}
	if authority.Grant != nil {
		signer.Actor = authority.Grant.Actor
		signer.KeyID = authority.Grant.KeyID
	} else {
		signer.Actor = "root"
		signer.KeyID = strings.TrimPrefix(authority.Reference, "root:")
	}
	return signer
}

func projectEnvelope(command string, context *projectContext) Envelope {
	envelope := baseEnvelope(command)
	verified := context.Verified
	envelope.Project = &ProjectBody{
		ID: verified.State.Project.ID, Protocol: protocol.ProtocolID,
		RootFingerprint: verified.RootFingerprint,
	}
	var architectureBlob *string
	if verified.State.Project.Architecture != nil {
		value := verified.State.Project.Architecture.BlobOID
		architectureBlob = &value
	}
	var taskbookBlob *string
	if verified.State.Project.Taskbook != nil {
		value := verified.State.Project.Taskbook.BlobOID
		taskbookBlob = &value
	}
	envelope.Snapshot = &SnapshotBody{
		ArchitectureBlob: architectureBlob,
		MainCommit:       verified.Head, Offline: false, StateDigest: verified.StateDigest,
		TaskbookBlob: taskbookBlob, Trust: "verified",
	}
	envelope.Identity = context.Identity
	return envelope
}

func samePath(left, right string) bool {
	leftAbs, errLeft := filepath.Abs(left)
	rightAbs, errRight := filepath.Abs(right)
	if errLeft != nil || errRight != nil {
		return false
	}
	leftEval, errLeft := filepath.EvalSymlinks(leftAbs)
	rightEval, errRight := filepath.EvalSymlinks(rightAbs)
	if errLeft == nil {
		leftAbs = leftEval
	}
	if errRight == nil {
		rightAbs = rightEval
	}
	return leftAbs == rightAbs
}

func advanceCheckpoint(
	ctx context.Context,
	runner gitstore.Runner,
	project *localstate.Project,
	repoRoot string,
	verified *verifier.Result,
) error {
	current := project.MinimumCheckpoint
	if current.Commit == verified.Head {
		if current.StateDigest != verified.StateDigest {
			return protocol.NewError(
				protocol.ErrLocalStateCorrupt, protocol.CategoryLocal,
				"Local checkpoint digest does not match the verified commit.",
			)
		}
	} else {
		ancestor, err := runner.IsAncestor(ctx, current.Commit, verified.Head)
		if err != nil {
			return protocol.WrapError(
				protocol.ErrMainlineRollback, protocol.CategoryTrust,
				"Cannot prove that the verified checkpoint advances local trust.", err,
			)
		}
		if !ancestor {
			failure := protocol.NewError(
				protocol.ErrMainlineRollback, protocol.CategoryTrust,
				"Refusing to move the local minimum checkpoint backward or sideways.",
			)
			failure.CurrentHead = verified.Head
			failure.Details["minimum_checkpoint"] = current.Commit
			return failure
		}
		project.MinimumCheckpoint = localstate.Checkpoint{
			Commit: verified.Head, StateDigest: verified.StateDigest,
		}
	}
	for index := range project.RepoInstances {
		if samePath(project.RepoInstances[index].RepoPath, repoRoot) {
			project.RepoInstances[index].LastVerifiedHead = verified.Head
		}
	}
	return nil
}

func stringTrimSpace(value []byte) string {
	start, end := 0, len(value)
	for start < end && (value[start] == ' ' || value[start] == '\n' || value[start] == '\r' || value[start] == '\t') {
		start++
	}
	for end > start && (value[end-1] == ' ' || value[end-1] == '\n' || value[end-1] == '\r' || value[end-1] == '\t') {
		end--
	}
	return string(value[start:end])
}
