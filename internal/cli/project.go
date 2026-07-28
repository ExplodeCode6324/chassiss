package cli

import (
	"context"
	"path/filepath"
	"sort"

	"github.com/ExplodeCode6324/chassiss/internal/cryptoutil"
	"github.com/ExplodeCode6324/chassiss/internal/gitstore"
	"github.com/ExplodeCode6324/chassiss/internal/localstate"
	"github.com/ExplodeCode6324/chassiss/internal/protocol"
	"github.com/ExplodeCode6324/chassiss/internal/state"
	"github.com/ExplodeCode6324/chassiss/internal/verifier"
)

type projectContext struct {
	RepoRoot     string
	Runner       gitstore.Runner
	Store        localstate.Store
	Local        *localstate.State
	LocalProject localstate.Project
	Verified     *verifier.Result
	Identity     *IdentityBody
}

func loadProject(ctx context.Context, target string, full bool) (*projectContext, error) {
	repoRoot, err := repositoryRoot(ctx)
	if err != nil {
		return nil, err
	}
	runner := gitstore.New(repoRoot)
	store, err := localstate.OpenDefault()
	if err != nil {
		return nil, err
	}
	local, err := store.Load()
	if err != nil {
		return nil, err
	}
	var localProject localstate.Project
	var projectID string
	for id, candidate := range local.Projects {
		for _, instance := range candidate.RepoInstances {
			if samePath(instance.RepoPath, repoRoot) {
				projectID, localProject = id, candidate
				break
			}
		}
	}
	if projectID == "" {
		return nil, protocol.NewError(protocol.ErrProjectNotFound, protocol.CategoryLocal, "Current repository is not a registered CHASSISS Project.")
	}
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
		RepoRoot: repoRoot, Runner: runner, Store: store, Local: local,
		LocalProject: localProject, Verified: verified,
	}
	context.Identity = discoverIdentity(verified.State, localProject)
	return context, nil
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

func projectEnvelope(command string, context *projectContext) Envelope {
	envelope := baseEnvelope(command)
	verified := context.Verified
	envelope.Project = &ProjectBody{
		ID: verified.State.Project.ID, Protocol: protocol.ProtocolID,
		RootFingerprint: verified.RootFingerprint,
	}
	var taskbookBlob *string
	if verified.State.Project.Taskbook != nil {
		value := verified.State.Project.Taskbook.BlobOID
		taskbookBlob = &value
	}
	envelope.Snapshot = &SnapshotBody{
		ArchitectureBlob: verified.State.Project.Architecture.BlobOID,
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
