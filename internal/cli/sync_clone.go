package cli

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/ExplodeCode6324/chassiss/internal/cryptoutil"
	"github.com/ExplodeCode6324/chassiss/internal/gitstore"
	"github.com/ExplodeCode6324/chassiss/internal/localstate"
	"github.com/ExplodeCode6324/chassiss/internal/protocol"
	"github.com/ExplodeCode6324/chassiss/internal/verifier"
)

func cloneCommand(ctx context.Context, invocation invocation) (Envelope, error) {
	remoteURL := invocation.Positionals[0]
	if err := validateRemoteURL(remoteURL); err != nil {
		return Envelope{}, usageError(err.Error())
	}
	directory, err := filepath.Abs(invocation.Positionals[1])
	if err != nil {
		return Envelope{}, err
	}
	if info, statErr := os.Stat(directory); statErr == nil {
		if !info.IsDir() {
			return Envelope{}, protocol.NewError(protocol.ErrUsageInvalid, protocol.CategoryLocal, "Clone destination exists and is not a directory.")
		}
		entries, readErr := os.ReadDir(directory)
		if readErr != nil || len(entries) != 0 {
			return Envelope{}, protocol.NewError(protocol.ErrUsageInvalid, protocol.CategoryLocal, "Clone destination must not exist or must be empty.")
		}
	} else if !os.IsNotExist(statErr) {
		return Envelope{}, statErr
	}
	untrusted := invocation.Flags["untrusted-read-only"]
	rootFingerprint := invocation.Value("root-fingerprint")
	checkpoint := invocation.Value("checkpoint")
	if untrusted {
		if rootFingerprint != "" || checkpoint != "" || invocation.Value("key") != "" {
			return Envelope{}, usageError("--untrusted-read-only cannot register trust or a private key")
		}
	} else if rootFingerprint == "" || checkpoint == "" {
		return Envelope{}, usageError("trusted clone requires --root-fingerprint and --checkpoint")
	}
	parent := filepath.Dir(directory)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return Envelope{}, err
	}
	if _, err := gitstore.New("").Run(
		ctx, "clone", "--origin", "origin", "--branch", "main", "--single-branch",
		"--", remoteURL, directory,
	); err != nil {
		return Envelope{}, protocol.WrapError(protocol.ErrRemoteUnreachable, protocol.CategoryNetwork, "Remote clone failed.", err)
	}
	runner := gitstore.New(directory)
	if _, err := runner.Run(ctx, "fetch", "--no-tags", "origin",
		"+refs/heads/chassiss/work/*:refs/remotes/origin/chassiss/work/*",
		"+refs/chassiss/archive/*:refs/chassiss/archive/*",
	); err != nil {
		return Envelope{}, protocol.WrapError(protocol.ErrRemoteUnreachable, protocol.CategoryNetwork, "Required CHASSISS refs could not be fetched.", err)
	}
	options := verifier.Options{ExpectedProject: invocation.Value("project")}
	if !untrusted {
		options.ExpectedRootFingerprint = rootFingerprint
		options.MinimumCheckpoint = checkpoint
	}
	verified, err := verifier.Verify(ctx, runner, "refs/heads/main", options)
	if err != nil {
		return Envelope{}, err
	}
	if untrusted {
		envelope := baseEnvelope("clone")
		envelope.Result = map[string]any{
			"directory": directory, "head": verified.Head,
			"registered": false, "trust": "untrusted-read-only",
		}
		return envelope, nil
	}
	store, err := localstate.OpenDefault()
	if err != nil {
		return Envelope{}, err
	}
	keys := map[string]localstate.Key{}
	selected := ""
	if keyValue := invocation.Value("key"); keyValue != "" {
		handle, err := resolveKeyHandle(keyValue, store.Paths)
		if err != nil {
			return Envelope{}, err
		}
		public, _, err := cryptoutil.PublicFromHandle(handle)
		if err != nil {
			return Envelope{}, err
		}
		selected = matchingKeyID(verified, public)
		if selected == "" {
			return Envelope{}, protocol.NewError(protocol.ErrKeyMismatch, protocol.CategoryAuthorization, "Clone key does not match current Root or Grant public material.")
		}
		keys[selected] = localstate.Key{PrivateKeyHandle: handle}
	}
	gitDir, err := runner.Run(ctx, "rev-parse", "--absolute-git-dir")
	if err != nil {
		return Envelope{}, err
	}
	if err := store.Update(func(local *localstate.State) error {
		current, exists := local.Projects[verified.State.Project.ID]
		if exists {
			if current.RootFingerprint != rootFingerprint || current.GenesisCommit != verified.Genesis {
				return protocol.NewError(protocol.ErrRemoteIdentityMismatch, protocol.CategoryTrust, "Registered Project trust anchor differs from the cloned history.")
			}
			for keyID, key := range keys {
				current.Identity.Keys[keyID] = key
			}
			if selected != "" {
				current.Identity.SelectedKeyID = selected
			}
			if err := advanceCheckpoint(ctx, runner, &current, directory, verified); err != nil {
				return err
			}
			current.RepoInstances = append(current.RepoInstances, localstate.RepoInstance{
				CreatedByCLI: true, GitDir: stringTrimSpace(gitDir.Stdout),
				LastVerifiedHead: verified.Head, RepoPath: directory,
			})
			current.Remote = localstate.Remote{URL: remoteURL, URLFingerprint: protocol.DigestBytes([]byte(remoteURL))}
			local.Projects[verified.State.Project.ID] = current
			return nil
		}
		local.Projects[verified.State.Project.ID] = localstate.Project{
			GenesisCommit:     verified.Genesis,
			Identity:          localstate.Identity{Keys: keys, SelectedKeyID: selected},
			MinimumCheckpoint: localstate.Checkpoint{Commit: verified.Head, StateDigest: verified.StateDigest},
			PendingOperations: map[string]localstate.PendingOperation{},
			Remote:            localstate.Remote{URL: remoteURL, URLFingerprint: protocol.DigestBytes([]byte(remoteURL))},
			RepoInstances: []localstate.RepoInstance{{
				CreatedByCLI: true, GitDir: stringTrimSpace(gitDir.Stdout),
				LastVerifiedHead: verified.Head, RepoPath: directory,
			}},
			RootFingerprint: rootFingerprint, Worktrees: map[string]localstate.Worktree{},
		}
		return nil
	}); err != nil {
		return Envelope{}, err
	}
	envelope := baseEnvelope("clone")
	envelope.Project = &ProjectBody{
		ID: verified.State.Project.ID, Protocol: protocol.ProtocolID,
		RootFingerprint: verified.RootFingerprint,
	}
	envelope.Result = map[string]any{
		"directory": directory, "head": verified.Head, "registered": true, "trust": "verified",
	}
	return envelope, nil
}

func syncCommand(ctx context.Context, invocation invocation) (Envelope, error) {
	project, err := loadProject(ctx, "", false)
	if err != nil {
		return Envelope{}, err
	}
	if project.LocalProject.Remote.URL == "" {
		if invocation.Flags["all-work"] {
			project.Verified, err = verifier.Verify(ctx, project.Runner, "refs/heads/main", verifier.Options{
				ExpectedProject:         project.Verified.State.Project.ID,
				ExpectedRootFingerprint: project.LocalProject.RootFingerprint,
				MinimumCheckpoint:       project.LocalProject.MinimumCheckpoint.Commit,
				Full:                    true,
			})
			if err != nil {
				return Envelope{}, err
			}
		}
		pruned := []string{}
		if invocation.Flags["prune"] {
			pruned, err = pruneTransitionRefs(ctx, project.Runner, project.Verified.Head)
			if err != nil {
				return Envelope{}, err
			}
		}
		reconciliation, err := reconcileAndCheckpointPending(ctx, project, project.Verified)
		if err != nil {
			return Envelope{}, err
		}
		envelope := projectEnvelope("sync", project)
		envelope.Result = map[string]any{
			"advanced": false, "head": project.Verified.Head,
			"pending_reconciliation": reconciliation, "pruned": pruned,
			"remote": false,
		}
		return envelope, nil
	}
	if _, err := project.Runner.Run(ctx, "fetch", "--no-tags", "origin",
		"+refs/heads/main:refs/remotes/origin/main",
		"+refs/heads/chassiss/work/*:refs/remotes/origin/chassiss/work/*",
		"+refs/chassiss/archive/*:refs/chassiss/archive/*",
		"+refs/heads/chassiss/transition/*:refs/remotes/origin/chassiss/transition/*",
	); err != nil {
		return Envelope{}, protocol.WrapError(protocol.ErrRemoteUnreachable, protocol.CategoryNetwork, "Authoritative upstream fetch failed.", err)
	}
	remote, err := verifier.Verify(ctx, project.Runner, "refs/remotes/origin/main", verifier.Options{
		ExpectedProject:         project.Verified.State.Project.ID,
		ExpectedRootFingerprint: project.LocalProject.RootFingerprint,
		MinimumCheckpoint:       project.LocalProject.MinimumCheckpoint.Commit,
		Full:                    invocation.Flags["all-work"],
	})
	if err != nil {
		return Envelope{}, err
	}
	descendant, err := project.Runner.IsAncestor(ctx, project.Verified.Head, remote.Head)
	if err != nil || !descendant {
		return Envelope{}, protocol.NewError(protocol.ErrRemoteIdentityMismatch, protocol.CategoryTrust, "Remote main is not a descendant of local verified main.")
	}
	if invocation.Flags["all-work"] {
		if err := verifyCurrentRemoteWork(ctx, project, remote); err != nil {
			return Envelope{}, err
		}
	}
	if remote.Head != project.Verified.Head {
		status, err := project.Runner.Run(ctx, "status", "--porcelain=v1", "-z")
		if err != nil {
			return Envelope{}, err
		}
		if len(status.Stdout) != 0 {
			return Envelope{}, protocol.NewError(protocol.ErrWorktreeDirty, protocol.CategoryLocal, "Cannot advance main while its worktree is dirty.")
		}
		if err := project.Runner.UpdateRefCAS(ctx, "refs/heads/main", remote.Head, project.Verified.Head, "CHASSISS sync"); err != nil {
			return Envelope{}, err
		}
		if _, err := project.Runner.Run(ctx, "read-tree", "--reset", "-u", remote.Head); err != nil {
			return Envelope{}, err
		}
	}
	pruned := []string{}
	if invocation.Flags["prune"] {
		pruned, err = pruneTransitionRefs(ctx, project.Runner, remote.Head)
		if err != nil {
			return Envelope{}, err
		}
	}
	reconciliation, err := reconcileAndCheckpointPending(ctx, project, remote)
	if err != nil {
		return Envelope{}, err
	}
	envelope := projectEnvelope("sync", &projectContext{
		InvocationRoot: project.InvocationRoot,
		RepoRoot:       project.RepoRoot, Runner: project.Runner, Store: project.Store,
		Local: project.Local, LocalProject: project.LocalProject, Verified: remote,
		Identity: discoverIdentity(remote.State, project.LocalProject),
	})
	envelope.Result = map[string]any{
		"advanced": remote.Head != project.Verified.Head, "head": remote.Head,
		"pending_reconciliation": reconciliation, "pruned": pruned, "remote": true,
	}
	return envelope, nil
}

func matchingKeyID(verified *verifier.Result, public string) string {
	if verified.State.Authority.Root.PublicKey == public {
		return verified.State.Authority.Root.KeyID
	}
	for _, grant := range verified.State.Authority.Grants {
		if grant.PublicKey == public {
			return grant.KeyID
		}
	}
	return ""
}

func verifyCurrentRemoteWork(ctx context.Context, project *projectContext, current *verifier.Result) error {
	for taskID, task := range current.State.Tasks {
		if (task.Phase != "submitted" && task.Phase != "approved") || task.Attempt == nil {
			continue
		}
		retained := false
		for _, ref := range taskWorkRefs(taskID, task.Actor, task.Base) {
			remoteRef := "refs/remotes/origin/" + strings.TrimPrefix(ref, "refs/heads/")
			head, err := project.Runner.Resolve(ctx, remoteRef)
			if err == nil && head == task.Attempt.Head {
				retained = true
				break
			}
		}
		if !retained {
			return protocol.NewError(protocol.ErrAttemptUnreachable, protocol.CategoryProtocol, "Current submitted/approved Attempt is not held by its exact remote Work Ref.")
		}
	}
	return nil
}

func verifyRemoteRetainedRefs(
	ctx context.Context,
	runner gitstore.Runner,
	remoteURL string,
	verified *verifier.Result,
) error {
	result, err := runner.Run(
		ctx, "ls-remote", remoteURL,
		"refs/heads/chassiss/work/*", "refs/chassiss/archive/*",
	)
	if err != nil {
		return protocol.WrapError(protocol.ErrRemoteUnreachable, protocol.CategoryNetwork, "Candidate upstream retained refs cannot be read.", err)
	}
	refs := make(map[string]string)
	for _, line := range strings.Split(string(result.Stdout), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 {
			refs[fields[1]] = fields[0]
		}
	}
	for taskID, task := range verified.State.Tasks {
		if (task.Phase != "submitted" && task.Phase != "approved") || task.Attempt == nil {
			continue
		}
		retained := false
		candidates := taskWorkRefs(taskID, task.Actor, task.Base)
		for _, ref := range candidates {
			if refs[ref] == task.Attempt.Head {
				retained = true
				break
			}
		}
		if !retained {
			return &protocol.Error{
				Code: protocol.ErrAttemptUnreachable, Category: protocol.CategoryTrust,
				Message: "Candidate upstream does not retain the exact current Work Ref.",
				Details: map[string]any{
					"expected_head": task.Attempt.Head, "refs": candidates, "task": taskID,
				},
			}
		}
	}
	for _, transition := range verified.Transitions {
		if transition.Action != "task.cancelled" && transition.Action != "task.superseded" {
			continue
		}
		commit, err := runner.ReadCommit(ctx, transition.Commit)
		if err != nil {
			return err
		}
		message, err := protocol.ParseTransitionMessage(commit.Message, verified.ObjectFormat)
		if err != nil {
			return err
		}
		ref, _ := message.Evidence.Facts["archive_ref"].(string)
		head, _ := message.Evidence.Facts["archive_head"].(string)
		if ref != "" && refs[ref] != head {
			return &protocol.Error{
				Code: protocol.ErrArchiveRefInvalid, Category: protocol.CategoryTrust,
				Message: "Candidate upstream does not retain an exact required Archive Ref.",
				Details: map[string]any{
					"expected_head": head, "ref": ref, "task": transition.Target,
				},
			}
		}
	}
	return nil
}

func pruneTransitionRefs(ctx context.Context, runner gitstore.Runner, main string) ([]string, error) {
	result, err := runner.Run(ctx, "for-each-ref", "--format=%(refname)%00%(objectname)%00", "refs/heads/chassiss/transition/")
	if err != nil {
		return nil, err
	}
	fields := strings.Split(string(result.Stdout), "\x00")
	pruned := make([]string, 0)
	for index := 0; index+1 < len(fields); index += 2 {
		ref := strings.TrimSpace(fields[index])
		oid := strings.TrimSpace(fields[index+1])
		if ref == "" || oid == "" {
			continue
		}
		ancestor, err := runner.IsAncestor(ctx, oid, main)
		if err != nil {
			return nil, err
		}
		if ancestor {
			if _, err := runner.Run(ctx, "update-ref", "-d", ref, oid); err != nil {
				return nil, err
			}
			pruned = append(pruned, ref)
		}
	}
	return pruned, nil
}
