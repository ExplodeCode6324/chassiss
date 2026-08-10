package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ExplodeCode6324/chassiss/internal/contracts"
	"github.com/ExplodeCode6324/chassiss/internal/cryptoutil"
	"github.com/ExplodeCode6324/chassiss/internal/gitstore"
	"github.com/ExplodeCode6324/chassiss/internal/localstate"
	"github.com/ExplodeCode6324/chassiss/internal/protocol"
	"github.com/ExplodeCode6324/chassiss/internal/state"
	"github.com/ExplodeCode6324/chassiss/internal/verifier"
	"github.com/ExplodeCode6324/chassiss/internal/workflow"
)

type selectedAuthority struct {
	Reference   string
	GrantID     string
	Grant       *state.Grant
	Handle      string
	KeyPath     string
	Fingerprint string
}

type transitionPlan struct {
	Operation    protocol.Operation
	Evidence     protocol.ExecutionEvidence
	NextState    *state.State
	Tree         gitstore.TreeMap
	ReduceFacts  state.ReduceFacts
	SecondParent string
	Authority    selectedAuthority
	ArchiveRef   string
	ArchiveHead  string
	Result       map[string]any
	Warnings     []Warning
}

type publishedTransition struct {
	Commit          string
	StateDigest     string
	OperationDigest string
	EvidenceDigest  string
}

func loadMutableProject(ctx context.Context) (*projectContext, error) {
	project, err := loadProject(ctx, "", false)
	if err != nil {
		return nil, err
	}
	if project.LocalProject.Remote.URL == "" {
		return project, nil
	}
	if _, err := project.Runner.Run(ctx, "fetch", "--no-tags", "origin",
		"+refs/heads/main:refs/remotes/origin/main",
		"+refs/heads/chassiss/work/*:refs/remotes/origin/chassiss/work/*",
		"+refs/chassiss/archive/*:refs/chassiss/archive/*",
	); err != nil {
		return nil, protocol.WrapError(protocol.ErrRemoteUnreachable, protocol.CategoryNetwork, "Cannot fetch authoritative upstream.", err)
	}
	remote, err := verifier.Verify(ctx, project.Runner, "refs/remotes/origin/main", verifier.Options{
		ExpectedProject:         project.Verified.State.Project.ID,
		ExpectedRootFingerprint: project.LocalProject.RootFingerprint,
		MinimumCheckpoint:       project.LocalProject.MinimumCheckpoint.Commit,
	})
	if err != nil {
		return nil, err
	}
	if remote.Head != project.Verified.Head {
		descendant, err := project.Runner.IsAncestor(ctx, project.Verified.Head, remote.Head)
		if err != nil || !descendant {
			return nil, protocol.NewError(protocol.ErrRemoteIdentityMismatch, protocol.CategoryTrust, "Local and remote main histories diverge.")
		}
		status, err := project.Runner.Run(ctx, "status", "--porcelain=v1", "-z")
		if err != nil {
			return nil, err
		}
		if len(status.Stdout) != 0 {
			return nil, protocol.NewError(protocol.ErrWorktreeDirty, protocol.CategoryLocal, "Cannot advance verified main while its worktree is dirty.")
		}
		if err := project.Runner.UpdateRefCAS(ctx, "refs/heads/main", remote.Head, project.Verified.Head, "CHASSISS sync"); err != nil {
			return nil, err
		}
		if _, err := project.Runner.Run(ctx, "read-tree", "--reset", "-u", remote.Head); err != nil {
			return nil, err
		}
		project, err = loadProject(ctx, "", false)
		if err != nil {
			return nil, err
		}
	}
	if err := checkpointVerifiedProject(ctx, project, remote); err != nil {
		return nil, err
	}
	return project, nil
}

func checkpointVerifiedProject(
	ctx context.Context,
	project *projectContext,
	verified *verifier.Result,
) error {
	if err := project.Store.Update(func(local *localstate.State) error {
		value := local.Projects[verified.State.Project.ID]
		if err := advanceCheckpoint(
			ctx, project.Runner, &value, project.RepoRoot, verified,
		); err != nil {
			return err
		}
		local.Projects[verified.State.Project.ID] = value
		return nil
	}); err != nil {
		return err
	}
	project.LocalProject.MinimumCheckpoint = localstate.Checkpoint{
		Commit: verified.Head, StateDigest: verified.StateDigest,
	}
	project.Verified = verified
	return nil
}

func selectGrant(project *projectContext, invocation invocation, capability string, taskID string, resources []string, requireGlobal bool) (selectedAuthority, error) {
	candidates := make([]selectedAuthority, 0)
	for grantID, grant := range project.Verified.State.Authority.Grants {
		if explicit := invocation.Value("grant"); explicit != "" && explicit != grantID {
			continue
		}
		if !containsString(grant.Capabilities, capability) ||
			(taskID != "" && !matchesTask(taskID, grant.Scope.Tasks)) {
			continue
		}
		if requireGlobal && (!containsString(grant.Scope.Tasks, "*") || !containsString(grant.Scope.Resources, "*")) {
			continue
		}
		allowed := true
		for _, resource := range resources {
			if !matchesResource(resource, grant.Scope.Resources) {
				allowed = false
				break
			}
		}
		if !allowed {
			continue
		}
		key, exists := project.LocalProject.Identity.Keys[grant.KeyID]
		if !exists {
			continue
		}
		if explicitKey := invocation.Value("key"); explicitKey != "" &&
			explicitKey != grant.KeyID && explicitKey != key.PrivateKeyHandle {
			continue
		}
		public, fingerprint, err := cryptoutil.PublicFromHandle(key.PrivateKeyHandle)
		if err != nil || public != grant.PublicKey {
			continue
		}
		keyPath, err := cryptoutil.ResolveFileHandle(key.PrivateKeyHandle)
		if err != nil {
			continue
		}
		grantCopy := grant
		candidates = append(candidates, selectedAuthority{
			Reference: "grant:" + grantID, GrantID: grantID, Grant: &grantCopy,
			Handle: key.PrivateKeyHandle, KeyPath: keyPath, Fingerprint: fingerprint,
		})
	}
	if len(candidates) == 0 {
		return selectedAuthority{}, protocol.NewError(protocol.ErrGrantNotFound, protocol.CategoryAuthorization, "No current local Grant can authorize this Action and scope.")
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].GrantID < candidates[j].GrantID })
	if len(candidates) > 1 && invocation.Value("grant") == "" {
		return selectedAuthority{}, protocol.NewError(protocol.ErrIdentityAmbiguous, protocol.CategoryLocal, "Multiple current Grants can authorize this Action; select --grant explicitly.")
	}
	return candidates[0], nil
}

func selectRoot(project *projectContext, handleValue string) (selectedAuthority, error) {
	handle, err := resolveKeyHandle(handleValue, project.Store.Paths)
	if err != nil {
		return selectedAuthority{}, protocol.WrapError(protocol.ErrSecretKeyNotFound, protocol.CategoryLocal, "Root private-key handle cannot be resolved.", err)
	}
	public, fingerprint, err := cryptoutil.PublicFromHandle(handle)
	if err != nil {
		return selectedAuthority{}, err
	}
	if public != project.Verified.State.Authority.Root.PublicKey {
		return selectedAuthority{}, protocol.NewError(protocol.ErrKeyMismatch, protocol.CategoryAuthorization, "Selected private key does not match the current Root.")
	}
	path, err := cryptoutil.ResolveFileHandle(handle)
	if err != nil {
		return selectedAuthority{}, err
	}
	return selectedAuthority{
		Reference: "root:" + project.Verified.State.Authority.Root.KeyID,
		Handle:    handle, KeyPath: path, Fingerprint: fingerprint,
	}, nil
}

func publishTransition(ctx context.Context, project *projectContext, plan transitionPlan) (Envelope, error) {
	status, err := project.Runner.Run(ctx, "status", "--porcelain=v1", "-z", "--untracked-files=no")
	if err != nil {
		return Envelope{}, err
	}
	if len(status.Stdout) != 0 {
		return Envelope{}, protocol.NewError(protocol.ErrWorktreeDirty, protocol.CategoryLocal, "The main worktree must be clean before publishing a Transition.")
	}
	format := project.Verified.ObjectFormat
	stateData, err := state.Encode(plan.NextState, format)
	if err != nil {
		return Envelope{}, err
	}
	stateDigest := protocol.DigestBytes(stateData)
	stateBlob, err := project.Runner.HashBlob(ctx, stateData)
	if err != nil {
		return Envelope{}, err
	}
	plan.Tree[".chassiss/state.json"] = gitstore.Entry{Mode: "100644", OID: stateBlob}
	treeOID, err := project.Runner.WriteTree(ctx, plan.Tree)
	if err != nil {
		return Envelope{}, err
	}
	parents := []string{project.Verified.Head}
	if plan.SecondParent != "" {
		parents = append(parents, plan.SecondParent)
	}
	message, err := protocol.BuildTransitionMessage(plan.Operation, plan.Evidence, stateDigest, format)
	if err != nil {
		return Envelope{}, err
	}
	operationDigest, _ := protocol.ObjectDigest("operation", plan.Operation)
	evidenceDigest, _ := protocol.ObjectDigest("execution-evidence", plan.Evidence)
	semanticBytes, _ := protocol.CanonicalJSON(plan.Operation)
	evidenceBytes, _ := protocol.CanonicalJSON(plan.Evidence)
	pending := localstate.PendingOperation{
		AuthorityKeyHandle: plan.Authority.Handle, CandidateCommit: nil,
		CandidateEvidence: evidenceBytes, EvidenceDigest: evidenceDigest,
		ExpectedMain: project.Verified.Head, OperationDigest: operationDigest,
		OperationID: plan.Operation.OperationID, SemanticOperation: semanticBytes,
		Status: "prepared", TargetRefs: map[string]string{"refs/heads/main": ""},
	}
	if err := savePending(project, pending); err != nil {
		return Envelope{}, err
	}
	commit, err := project.Runner.CommitTree(ctx, treeOID, parents, message, plan.Authority.KeyPath, gitstore.CommitIdentity{})
	if err != nil {
		return Envelope{}, err
	}
	if err := updatePendingCandidate(project, plan.Operation.OperationID, commit); err != nil {
		return Envelope{}, err
	}
	pending.CandidateCommit = stringPointer(commit)
	pending.Status = "signed"
	pending.TargetRefs["refs/heads/main"] = commit
	if plan.ArchiveRef == "" {
		if _, err := verifier.Verify(ctx, project.Runner, commit, verifier.Options{
			ExpectedProject:         project.Verified.State.Project.ID,
			ExpectedRootFingerprint: project.LocalProject.RootFingerprint,
			MinimumCheckpoint:       project.LocalProject.MinimumCheckpoint.Commit,
		}); err != nil {
			return Envelope{}, protocol.WrapError(protocol.ErrReducerMismatch, protocol.CategoryProtocol, "Locally built Transition failed post-verification.", err)
		}
	}
	if project.LocalProject.Remote.URL == "" {
		if plan.ArchiveRef == "" {
			err = project.Runner.UpdateRefCAS(ctx, "refs/heads/main", commit, project.Verified.Head, "CHASSISS "+plan.Operation.Action)
		} else {
			err = project.Runner.UpdateRefsAtomic(ctx, []gitstore.RefUpdate{
				{Ref: plan.ArchiveRef, NewOID: plan.ArchiveHead, CreateOnly: true},
				{Ref: "refs/heads/main", NewOID: commit, ExpectedOID: project.Verified.Head},
			}, "CHASSISS "+plan.Operation.Action)
		}
		if err != nil {
			reconciliation, reconcileErr := reconcileLocalCASFailure(
				ctx, project, pending,
			)
			if reconciliation.Disposition == pendingPublished {
				commit = reconciliation.Commit
				plan.Evidence = reconciliation.Message.Evidence
				operationDigest, _ = protocol.ObjectDigest(
					"operation", reconciliation.Message.Operation,
				)
				evidenceDigest, _ = protocol.ObjectDigest(
					"execution-evidence", reconciliation.Message.Evidence,
				)
				err = nil
			} else {
				if reconcileErr != nil {
					var safetyFailure *protocol.Error
					if errors.As(reconcileErr, &safetyFailure) {
						if safetyFailure.Details == nil {
							safetyFailure.Details = map[string]any{}
						}
						safetyFailure.OperationID = plan.Operation.OperationID
						safetyFailure.Details["candidate_commit"] = commit
						safetyFailure.Details["cas_error"] = err.Error()
						safetyFailure.Details["pending_status"] = string(pendingUnresolved)
						if current, resolveErr := project.Runner.Resolve(ctx, "refs/heads/main"); resolveErr == nil {
							safetyFailure.CurrentHead = current
						}
						return Envelope{}, safetyFailure
					}
				}
				failure := protocol.WrapError(
					protocol.ErrCASRetryExhausted, protocol.CategoryConflict,
					"Main changed before the Transition could be published.", err,
				)
				failure.OperationID = plan.Operation.OperationID
				failure.Details = map[string]any{
					"candidate_commit": commit,
					"pending_status":   string(reconciliation.Disposition),
				}
				if current, resolveErr := project.Runner.Resolve(ctx, "refs/heads/main"); resolveErr == nil {
					failure.CurrentHead = current
				}
				if reconcileErr != nil {
					failure.Details["reconciliation_error"] = reconcileErr.Error()
				}
				return Envelope{}, failure
			}
		}
	} else {
		publishedHead := commit
		args := []string{"push", "--porcelain", "--atomic",
			"--force-with-lease=refs/heads/main:" + project.Verified.Head,
		}
		if plan.ArchiveRef != "" {
			args = append(args,
				"--force-with-lease="+plan.ArchiveRef+":",
			)
		}
		args = append(args, "origin", commit+":refs/heads/main")
		if plan.ArchiveRef != "" {
			args = append(args, plan.ArchiveHead+":"+plan.ArchiveRef)
		}
		if _, pushErr := project.Runner.Run(ctx, args...); pushErr != nil {
			pendingErr := markPushUnknown(project, plan.Operation.OperationID)
			reconciliation, reconcileErr := reconcileRemotePush(
				ctx, project, plan.Operation.OperationID, operationDigest,
			)
			if reconcileErr != nil {
				failure := protocol.WrapError(
					protocol.ErrPushResultUnknown, protocol.CategoryNetwork,
					"Transition push result requires reconciliation.", pushErr,
				)
				failure.OperationID = plan.Operation.OperationID
				failure.CurrentHead = project.Verified.Head
				failure.Retryable = true
				failure.Details["reconciliation_error"] = reconcileErr.Error()
				if pendingErr != nil {
					failure.Details["pending_store_error"] = pendingErr.Error()
				}
				return Envelope{}, failure
			}
			if !reconciliation.Found {
				if pendingErr == nil {
					pendingErr = markPendingFailed(project, plan.Operation.OperationID)
				}
				if reconciliation.Verified.Head == project.Verified.Head {
					if plan.ArchiveRef != "" {
						if archiveOID, archiveErr := project.Runner.Resolve(ctx, plan.ArchiveRef); archiveErr == nil {
							failure := protocol.NewError(
								protocol.ErrArchiveRefInvalid, protocol.CategoryConflict,
								"Remote rejected create-only Archive Ref publication.",
							)
							failure.OperationID = plan.Operation.OperationID
							failure.CurrentHead = reconciliation.Verified.Head
							failure.Details = map[string]any{
								"actual_oid": archiveOID, "archive_ref": plan.ArchiveRef,
							}
							return Envelope{}, failure
						}
					}
					failure := protocol.WrapError(
						protocol.ErrRemoteUnreachable, protocol.CategoryNetwork,
						"Remote rejected the Transition without changing main.", pushErr,
					)
					failure.OperationID = plan.Operation.OperationID
					failure.CurrentHead = reconciliation.Verified.Head
					failure.Retryable = true
					if pendingErr != nil {
						failure.Details["pending_store_error"] = pendingErr.Error()
					}
					return Envelope{}, failure
				}
				if pendingErr == nil && plan.Evidence.Attempt < 3 {
					retryProject, retryErr := adoptVerifiedRemote(
						ctx, project, reconciliation.Verified,
					)
					if retryErr != nil {
						return Envelope{}, retryErr
					}
					retryPlan, retryErr := rebaseTransitionPlan(
						ctx, project, retryProject, plan,
					)
					if retryErr != nil {
						return Envelope{}, retryErr
					}
					return publishTransition(ctx, retryProject, retryPlan)
				}
				failure := protocol.NewError(
					protocol.ErrCASRetryExhausted, protocol.CategoryConflict,
					"Remote verification confirmed that this candidate was not published.",
				)
				failure.OperationID = plan.Operation.OperationID
				failure.CurrentHead = reconciliation.Verified.Head
				failure.Retryable = true
				failure.Details = map[string]any{
					"attempts": plan.Evidence.Attempt, "candidate_commit": commit,
				}
				if pendingErr != nil {
					failure.Details["pending_store_error"] = pendingErr.Error()
				}
				return Envelope{}, failure
			}
			commit = reconciliation.Commit
			publishedHead = reconciliation.Verified.Head
			plan.Evidence = reconciliation.Message.Evidence
			operationDigest, _ = protocol.ObjectDigest("operation", reconciliation.Message.Operation)
			evidenceDigest, _ = protocol.ObjectDigest("execution-evidence", reconciliation.Message.Evidence)
		}
		if plan.ArchiveRef != "" {
			_ = project.Runner.UpdateRefCAS(ctx, plan.ArchiveRef, plan.ArchiveHead, zeroOID(format), "CHASSISS archive")
		}
		current, resolveErr := project.Runner.Resolve(ctx, "refs/heads/main")
		if resolveErr != nil {
			err = resolveErr
		} else if current != publishedHead {
			err = project.Runner.UpdateRefCAS(ctx, "refs/heads/main", publishedHead, project.Verified.Head, "CHASSISS "+plan.Operation.Action)
		}
	}
	if err != nil {
		return Envelope{}, protocol.WrapError(protocol.ErrCASRetryExhausted, protocol.CategoryConflict, "Main changed before the Transition could be published.", err)
	}
	if _, err := project.Runner.Run(ctx, "read-tree", "--reset", "-u", "refs/heads/main"); err != nil {
		return Envelope{}, err
	}
	verified, err := verifier.Verify(ctx, project.Runner, "refs/heads/main", verifier.Options{
		ExpectedProject:         project.Verified.State.Project.ID,
		ExpectedRootFingerprint: project.LocalProject.RootFingerprint,
		MinimumCheckpoint:       project.LocalProject.MinimumCheckpoint.Commit,
	})
	if err != nil {
		return Envelope{}, err
	}
	if err := finalizePending(ctx, project, pending, verified); err != nil {
		return Envelope{}, err
	}
	envelope := projectEnvelope(plan.Operation.Action, &projectContext{
		InvocationRoot: project.InvocationRoot,
		RepoRoot:       project.RepoRoot, Runner: project.Runner, Store: project.Store,
		Local: project.Local, LocalProject: project.LocalProject, Verified: verified,
		Identity: discoverIdentity(verified.State, project.LocalProject),
	})
	envelope.Command = commandForAction(plan.Operation.Action)
	envelope.Identity = identityForAuthority(plan.Authority)
	envelope.Operation = &OperationBody{
		Commit: commit, EvidenceAttempt: plan.Evidence.Attempt,
		EvidenceDigest: evidenceDigest, OperationDigest: operationDigest,
		OperationID: plan.Operation.OperationID, Signer: signerForAuthority(plan.Authority),
		Status: "published",
	}
	envelope.Result = plan.Result
	if envelope.Result == nil {
		envelope.Result = map[string]any{}
	}
	envelope.Warnings = append(envelope.Warnings, plan.Warnings...)
	return envelope, nil
}

func adoptVerifiedRemote(
	ctx context.Context,
	project *projectContext,
	remote *verifier.Result,
) (*projectContext, error) {
	status, err := project.Runner.Run(ctx, "status", "--porcelain=v1", "-z", "--untracked-files=no")
	if err != nil {
		return nil, err
	}
	if len(status.Stdout) != 0 {
		return nil, protocol.NewError(protocol.ErrWorktreeDirty, protocol.CategoryLocal, "Cannot adopt a new verified main while its worktree is dirty.")
	}
	current, err := project.Runner.Resolve(ctx, "refs/heads/main")
	if err != nil {
		return nil, err
	}
	if current != remote.Head {
		if current != project.Verified.Head {
			return nil, protocol.NewError(protocol.ErrCASRetryExhausted, protocol.CategoryConflict, "Local main changed during remote reconciliation.")
		}
		if err := project.Runner.UpdateRefCAS(ctx, "refs/heads/main", remote.Head, current, "CHASSISS CAS retry"); err != nil {
			return nil, err
		}
	}
	if _, err := project.Runner.Run(ctx, "read-tree", "--reset", "-u", remote.Head); err != nil {
		return nil, err
	}
	if err := checkpointVerifiedProject(ctx, project, remote); err != nil {
		return nil, err
	}
	return loadProject(ctx, "", false)
}

func rebaseTransitionPlan(
	ctx context.Context,
	previous *projectContext,
	current *projectContext,
	plan transitionPlan,
) (transitionPlan, error) {
	if current.Verified.ObjectFormat != previous.Verified.ObjectFormat {
		return transitionPlan{}, protocol.NewError(protocol.ErrRemoteIdentityMismatch, protocol.CategoryTrust, "Git object format changed during CAS retry.")
	}
	previousTree, err := currentTree(ctx, previous)
	if err != nil {
		return transitionPlan{}, err
	}
	currentTreeMap, err := currentTree(ctx, current)
	if err != nil {
		return transitionPlan{}, err
	}
	candidate := cloneTreeMap(currentTreeMap)
	for _, path := range gitstore.ChangedPaths(previousTree, plan.Tree) {
		if path == ".chassiss/state.json" {
			continue
		}
		if currentTreeMap[path] != previousTree[path] {
			return transitionPlan{}, &protocol.Error{
				Code: protocol.ErrCandidateConflict, Category: protocol.CategoryConflict,
				Message: "A CAS retry would overwrite a path changed on current main.",
				Details: map[string]any{"path": path},
			}
		}
		if entry, exists := plan.Tree[path]; exists {
			candidate[path] = entry
		} else {
			delete(candidate, path)
		}
	}
	plan.Tree = candidate
	plan.Evidence.Parent = stringPointer(current.Verified.Head)
	plan.Evidence.Attempt++
	plan.Evidence.Facts = cloneAnyMap(plan.Evidence.Facts)
	plan.ReduceFacts.ObjectFormat = current.Verified.ObjectFormat
	plan.ReduceFacts.ParentCommit = current.Verified.Head

	switch plan.Operation.Action {
	case "task.started":
		plan.Evidence.Facts["base"] = current.Verified.Head
		plan.ReduceFacts.ActiveTasksForActor = 0
		actor, _ := plan.Evidence.Facts["actor"].(string)
		for _, task := range current.Verified.State.Tasks {
			if task.Actor == actor && (task.Phase == "active" || task.Phase == "submitted" || task.Phase == "approved") {
				plan.ReduceFacts.ActiveTasksForActor++
			}
		}
	case "task.reviewed", "task.reviewed-indexed":
		if err := rebuildReviewRetry(ctx, current, &plan); err != nil {
			return transitionPlan{}, err
		}
	case "integration.applied":
		if err := rebuildIntegrationRetry(ctx, current, &plan); err != nil {
			return transitionPlan{}, err
		}
	case "taskbook.archived":
		if !ordinaryTreeEqual(previousTree, currentTreeMap) {
			return transitionPlan{}, protocol.NewError(
				protocol.ErrTaskbookClosureStale, protocol.CategoryReview,
				"Ordinary project content changed during Taskbook closure CAS.",
			)
		}
		if err := rebuildClosureRetry(ctx, current, &plan); err != nil {
			return transitionPlan{}, err
		}
	case "architecture.updated-compatible":
		architectureBlob, _ := plan.Operation.Preconditions["architecture_blob"].(string)
		taskbookBlob, _ := plan.Operation.Preconditions["taskbook_blob"].(string)
		if current.Verified.State.Project.Architecture == nil ||
			architectureBlob != current.Verified.State.Project.Architecture.BlobOID {
			return transitionPlan{}, protocol.NewError(protocol.ErrArchitectureStale, protocol.CategoryConflict, "Architecture changed during CAS retry.")
		}
		if current.Verified.State.Project.Taskbook == nil ||
			taskbookBlob != current.Verified.State.Project.Taskbook.BlobOID {
			return transitionPlan{}, protocol.NewError(protocol.ErrTaskbookStale, protocol.CategoryConflict, "Taskbook changed during Architecture update CAS retry.")
		}
		if !ordinaryTreeEqual(previousTree, currentTreeMap) {
			paths := make([]string, 0)
			for _, path := range gitstore.ChangedPaths(previousTree, currentTreeMap) {
				if path != ".chassiss/state.json" {
					paths = append(paths, path)
				}
			}
			return transitionPlan{}, &protocol.Error{
				Code: protocol.ErrCandidateConflict, Category: protocol.CategoryConflict,
				Message: "Ordinary project content changed during compatible Architecture update CAS retry.",
				Details: map[string]any{"paths": paths},
			}
		}
		if err := requireArchitectureQuiescence(current.Verified.State); err != nil {
			return transitionPlan{}, err
		}
		candidateBlob, _ := plan.Evidence.Facts["new_blob"].(string)
		candidateData, err := current.Runner.ReadBlob(ctx, candidateBlob)
		if err != nil {
			return transitionPlan{}, err
		}
		candidateArchitecture, err := contracts.ParseArchitecture(candidateData)
		if err != nil {
			return transitionPlan{}, protocol.WrapError(protocol.ErrArchitectureInvalid, protocol.CategoryValidation, "Architecture candidate is invalid during CAS retry.", err)
		}
		taskbookData, err := current.Runner.ReadBlob(ctx, current.Verified.State.Project.Taskbook.BlobOID)
		if err != nil {
			return transitionPlan{}, err
		}
		if _, err := contracts.ParseTaskbook(taskbookData, candidateArchitecture); err != nil {
			return transitionPlan{}, protocol.WrapError(protocol.ErrTaskbookInvalid, protocol.CategoryValidation, "Active Taskbook is incompatible with the candidate Architecture during CAS retry.", err)
		}
	case "owner.applied":
		candidateTree, err := current.Runner.WriteTree(ctx, plan.Tree)
		if err != nil {
			return transitionPlan{}, err
		}
		changed := gitstore.ChangedPaths(currentTreeMap, plan.Tree)
		changedDigest, _ := protocol.ObjectDigest("changed-paths", changed)
		plan.Evidence.Facts["candidate_tree"] = candidateTree
		plan.Evidence.Facts["changed_paths"] = stringSliceAny(changed)
		plan.Evidence.Facts["changed_paths_digest"] = changedDigest
	}
	next, err := state.Reduce(
		current.Verified.State, plan.Operation, plan.Evidence, plan.ReduceFacts,
	)
	if err != nil {
		return transitionPlan{}, err
	}
	plan.NextState = next
	return plan, nil
}

func rebuildReviewRetry(
	ctx context.Context,
	project *projectContext,
	plan *transitionPlan,
) error {
	taskID := plan.Operation.Target
	task, exists := project.Verified.State.Tasks[taskID]
	if !exists || task.Attempt == nil || task.Contract == nil {
		return protocol.NewError(protocol.ErrAttemptStale, protocol.CategoryReview, "Review Attempt changed during CAS retry.")
	}
	resources, contract, err := taskResources(project, taskID)
	if err != nil {
		return err
	}
	reviewContext, _, err := buildReviewContext(ctx, project, taskID, task, contract)
	if err != nil {
		return err
	}
	contextObject, err := objectMap(reviewContext)
	if err != nil {
		return err
	}
	checks, err := jsonValue(reviewContext.CheckResults)
	if err != nil {
		return err
	}
	attemptDigest, err := state.AttemptDigest(taskID, task)
	if err != nil {
		return err
	}
	plan.Evidence.Facts = map[string]any{
		"attempt_digest": attemptDigest,
		"check_results":  checks,
		"review_context": contextObject,
	}
	plan.ReduceFacts.TaskResources = resources
	return nil
}

func rebuildIntegrationRetry(
	ctx context.Context,
	project *projectContext,
	plan *transitionPlan,
) error {
	taskID := plan.Operation.Target
	task, exists := project.Verified.State.Tasks[taskID]
	if !exists || task.Phase != "approved" || task.Attempt == nil ||
		task.Review == nil || task.Contract == nil {
		return protocol.NewError(protocol.ErrReviewContextStale, protocol.CategoryReview, "Approved Review changed during CAS retry.")
	}
	resources, contract, err := taskResources(project, taskID)
	if err != nil {
		return err
	}
	drift, err := verifier.ClassifyDrift(
		ctx, project.Runner, project.Verified.ObjectFormat, project.Verified.State,
		taskID, contract, project.Verified.Architecture, task.Review.ReviewMain,
		project.Verified.Head,
	)
	if err != nil {
		return err
	}
	if drift.Classification != "unrelated" {
		return &protocol.Error{
			Code: protocol.ErrReviewContextStale, Category: protocol.CategoryReview,
			Message: "Relevant drift during CAS retry requires a new Review.",
			Details: map[string]any{"drift_classification": drift},
		}
	}
	mainCommit, err := project.Runner.ReadCommit(ctx, project.Verified.Head)
	if err != nil {
		return err
	}
	mainTree, err := project.Runner.ReadTree(ctx, mainCommit.Tree)
	if err != nil {
		return err
	}
	candidate, err := workflow.BuildCandidate(
		ctx, project.Runner, project.Verified.ObjectFormat,
		mainTree, project.Verified.State, taskID,
	)
	if err != nil {
		return protocol.WrapError(protocol.ErrCandidateConflict, protocol.CategoryConflict, "CAS retry candidate overlay failed.", err)
	}
	if violations := scopeViolations(candidate.ChangedPaths, contract.Writes); len(violations) > 0 {
		return &protocol.Error{
			Code: protocol.ErrScopeViolation, Category: protocol.CategoryValidation,
			Message: "CAS retry candidate exceeds the frozen Task writes.",
			Details: map[string]any{"paths": violations},
		}
	}
	if err := persistCandidate(ctx, project.Runner, candidate); err != nil {
		return err
	}
	results, err := runCandidateChecks(
		ctx, project, taskID, task, contract.Checks, candidate.TreeOID, "integration",
	)
	if err != nil {
		return err
	}
	if !allChecksPassed(results) {
		return protocol.NewError(protocol.ErrCheckFailed, protocol.CategoryCheck, "Integration Checks failed during CAS retry.")
	}
	checkValues, err := jsonValue(results)
	if err != nil {
		return err
	}
	driftValue, err := objectMap(drift)
	if err != nil {
		return err
	}
	plan.Evidence.Facts = map[string]any{
		"attempt_head": task.Attempt.Head, "candidate_tree": candidate.TreeOID,
		"check_results": checkValues, "drift_classification": driftValue,
		"review_context_digest": task.Review.ContextDigest,
		"review_report_digest":  task.Review.ReportDigest,
	}
	plan.Tree = candidate.Tree
	plan.SecondParent = task.Attempt.Head
	plan.ReduceFacts.TaskResources = resources
	return nil
}

func rebuildClosureRetry(
	ctx context.Context,
	project *projectContext,
	plan *transitionPlan,
) error {
	if project.Verified.Taskbook == nil || project.Verified.State.Project.Taskbook == nil ||
		project.Verified.Taskbook.ID != plan.Operation.Target {
		return protocol.NewError(protocol.ErrTaskbookClosureStale, protocol.CategoryReview, "Taskbook changed during closure CAS retry.")
	}
	terminal := make(map[string]any, len(project.Verified.State.Tasks))
	closing := make(map[string]any)
	for id, task := range project.Verified.State.Tasks {
		switch task.Phase {
		case "closed", "cancelled", "superseded":
		default:
			return protocol.NewError(protocol.ErrTaskbookClosureStale, protocol.CategoryReview, "Task projection changed during closure CAS retry.")
		}
		terminal[id] = task.Phase
		if task.Phase == "closed" {
			commit := closingIntegration(project, id)
			if commit == "" {
				return protocol.NewError(protocol.ErrTaskbookClosureStale, protocol.CategoryReview, "Closing Integration changed during closure CAS retry.")
			}
			closing[id] = commit
		}
	}
	mainCommit, err := project.Runner.ReadCommit(ctx, project.Verified.Head)
	if err != nil {
		return err
	}
	taskbookBlob := project.Verified.State.Project.Taskbook.BlobOID
	results, err := cleanCheckoutChecks(
		ctx, project, project.Verified.Taskbook.Workflow.Checks, workflow.CheckBinding{
			ArchitectureBlob: project.Verified.State.Project.Architecture.BlobOID,
			Head:             project.Verified.Head, Phase: "workflow-closure", Task: nil,
			TaskbookBlob: taskbookBlob, Tree: mainCommit.Tree,
		},
	)
	if err != nil {
		return err
	}
	if !allChecksPassed(results) {
		return protocol.NewError(protocol.ErrCheckFailed, protocol.CategoryCheck, "Workflow Closure Checks failed during CAS retry.")
	}
	checkValues, err := jsonValue(results)
	if err != nil {
		return err
	}
	plan.Evidence.Facts = map[string]any{
		"active_blob":       taskbookBlob,
		"architecture_blob": project.Verified.State.Project.Architecture.BlobOID,
		"archive_blob":      taskbookBlob,
		"archive_path":      "docs/taskbooks/archive/" + project.Verified.Taskbook.ID + ".yaml",
		"check_results":     checkValues, "closing_integrations": closing,
		"terminal_tasks": terminal,
	}
	return nil
}

func cloneTreeMap(value gitstore.TreeMap) gitstore.TreeMap {
	result := make(gitstore.TreeMap, len(value))
	for path, entry := range value {
		result[path] = entry
	}
	return result
}

func cloneAnyMap(value map[string]any) map[string]any {
	result := make(map[string]any, len(value))
	for key, item := range value {
		result[key] = item
	}
	return result
}

func ordinaryTreeEqual(left, right gitstore.TreeMap) bool {
	for _, path := range gitstore.ChangedPaths(left, right) {
		if path != ".chassiss/state.json" {
			return false
		}
	}
	return true
}

func stringPointer(value string) *string { return &value }

type pushReconciliation struct {
	Commit   string
	Found    bool
	Message  protocol.TransitionMessage
	Verified *verifier.Result
}

func reconcileRemotePush(
	ctx context.Context,
	project *projectContext,
	operationID string,
	operationDigest string,
) (pushReconciliation, error) {
	if _, err := project.Runner.Run(ctx, "fetch", "--no-tags", "origin",
		"+refs/heads/main:refs/remotes/origin/main",
		"+refs/heads/chassiss/work/*:refs/remotes/origin/chassiss/work/*",
		"+refs/chassiss/archive/*:refs/chassiss/archive/*",
	); err != nil {
		return pushReconciliation{}, err
	}
	verified, err := verifier.Verify(ctx, project.Runner, "refs/remotes/origin/main", verifier.Options{
		ExpectedProject:         project.Verified.State.Project.ID,
		ExpectedRootFingerprint: project.LocalProject.RootFingerprint,
		MinimumCheckpoint:       project.LocalProject.MinimumCheckpoint.Commit,
	})
	if err != nil {
		return pushReconciliation{}, err
	}
	for index := len(verified.Transitions) - 1; index >= 0; index-- {
		transition := verified.Transitions[index]
		if transition.OperationID != operationID {
			continue
		}
		if transition.OperationDigest != operationDigest {
			return pushReconciliation{}, &protocol.Error{
				Code: protocol.ErrOperationIDCollision, Category: protocol.CategoryProtocol,
				Message: "Remote history contains the Operation ID with different semantics.",
				Details: map[string]any{
					"actual_digest":   transition.OperationDigest,
					"expected_digest": operationDigest,
					"operation_id":    operationID,
				},
			}
		}
		commit, err := project.Runner.ReadCommit(ctx, transition.Commit)
		if err != nil {
			return pushReconciliation{}, err
		}
		message, err := protocol.ParseTransitionMessage(commit.Message, verified.ObjectFormat)
		if err != nil {
			return pushReconciliation{}, err
		}
		return pushReconciliation{
			Commit: transition.Commit, Found: true, Message: message, Verified: verified,
		}, nil
	}
	return pushReconciliation{Found: false, Verified: verified}, nil
}

func currentTree(ctx context.Context, project *projectContext) (gitstore.TreeMap, error) {
	commit, err := project.Runner.ReadCommit(ctx, project.Verified.Head)
	if err != nil {
		return nil, err
	}
	return project.Runner.ReadTree(ctx, commit.Tree)
}

func taskResources(project *projectContext, taskID string) ([]string, contracts.Task, error) {
	if project.Verified.Taskbook == nil {
		return nil, contracts.Task{}, protocol.NewError(protocol.ErrTaskbookNotActive, protocol.CategoryValidation, "No Taskbook is active.")
	}
	task, exists := project.Verified.Taskbook.Tasks[taskID]
	if !exists {
		return nil, contracts.Task{}, protocol.NewError(protocol.ErrReferenceNotFound, protocol.CategoryValidation, "Task does not exist.")
	}
	resources := append(append([]string(nil), task.Modules...), task.Affects...)
	sort.Strings(resources)
	return resources, task, nil
}

func savePending(project *projectContext, pending localstate.PendingOperation) error {
	return project.Store.Update(func(local *localstate.State) error {
		value := local.Projects[project.Verified.State.Project.ID]
		if value.PendingOperations == nil {
			value.PendingOperations = map[string]localstate.PendingOperation{}
		}
		value.PendingOperations[pending.OperationID] = pending
		local.Projects[project.Verified.State.Project.ID] = value
		return nil
	})
}

func updatePendingCandidate(project *projectContext, operationID, commit string) error {
	return project.Store.Update(func(local *localstate.State) error {
		value := local.Projects[project.Verified.State.Project.ID]
		pending := value.PendingOperations[operationID]
		pending.CandidateCommit = &commit
		pending.Status = "signed"
		pending.TargetRefs["refs/heads/main"] = commit
		value.PendingOperations[operationID] = pending
		local.Projects[project.Verified.State.Project.ID] = value
		return nil
	})
}

func markPushUnknown(project *projectContext, operationID string) error {
	return project.Store.Update(func(local *localstate.State) error {
		value := local.Projects[project.Verified.State.Project.ID]
		pending, exists := value.PendingOperations[operationID]
		if !exists {
			return fmt.Errorf("pending Operation %s does not exist", operationID)
		}
		pending.Status = "push-unknown"
		value.PendingOperations[operationID] = pending
		local.Projects[project.Verified.State.Project.ID] = value
		return nil
	})
}

func markPendingFailed(project *projectContext, operationID string) error {
	return project.Store.Update(func(local *localstate.State) error {
		value := local.Projects[project.Verified.State.Project.ID]
		pending, exists := value.PendingOperations[operationID]
		if !exists {
			return fmt.Errorf("pending Operation %s does not exist", operationID)
		}
		pending.Status = "failed"
		value.PendingOperations[operationID] = pending
		local.Projects[project.Verified.State.Project.ID] = value
		return nil
	})
}

func finalizePending(
	ctx context.Context,
	project *projectContext,
	expected localstate.PendingOperation,
	verified *verifier.Result,
) error {
	return project.Store.Update(func(local *localstate.State) error {
		value := local.Projects[verified.State.Project.ID]
		if pending, exists := value.PendingOperations[expected.OperationID]; exists &&
			samePendingAttempt(pending, expected) {
			delete(value.PendingOperations, expected.OperationID)
		}
		if err := advanceCheckpoint(
			ctx, project.Runner, &value, project.RepoRoot, verified,
		); err != nil {
			return err
		}
		local.Projects[verified.State.Project.ID] = value
		return nil
	})
}

func cloneSharedState(value *state.State) (*state.State, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var result state.State
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func zeroOID(format string) string {
	if format == "sha256" {
		return strings.Repeat("0", 64)
	}
	return strings.Repeat("0", 40)
}

func commandForAction(action string) string {
	switch action {
	case "task.started":
		return "task start"
	case "task.released":
		return "task release"
	case "attempt.abandoned":
		return "attempt abandon"
	case "task.blocked":
		return "task block"
	case "task.resumed":
		return "task resume"
	case "task.cancelled":
		return "task cancel"
	case "task.superseded":
		return "task supersede"
	case "task.submitted":
		return "submit"
	case "task.reviewed", "task.reviewed-indexed":
		return "review"
	case "integration.applied":
		return "integrate"
	case "taskbook.opened":
		return "taskbook open"
	case "taskbook.updated":
		return "taskbook update"
	case "taskbook.archived":
		return "taskbook archive"
	case "architecture.updated", "architecture.updated-compatible":
		return "architecture update"
	case "authority.grant-added":
		return "grant add"
	case "authority.grant-revoked":
		return "grant revoke"
	case "owner.applied":
		return "owner apply"
	default:
		return action
	}
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func matchesTask(task string, patterns []string) bool {
	for _, pattern := range patterns {
		if pattern == "*" || pattern == task ||
			strings.HasSuffix(pattern, "*") && strings.HasPrefix(task, strings.TrimSuffix(pattern, "*")) {
			return true
		}
	}
	return false
}

func matchesResource(resource string, patterns []string) bool {
	kind, _, _ := strings.Cut(resource, ":")
	for _, pattern := range patterns {
		if pattern == "*" || pattern == resource || pattern == kind+":*" {
			return true
		}
	}
	return false
}

func taskWorkRef(taskID, actor, base string) string {
	return "refs/heads/chassiss/work/" + taskID + "/" + base[:12] + "/" + actor
}

func taskWorkRefs(taskID, actor, base string) []string {
	return []string{
		taskWorkRef(taskID, actor, base),
		"refs/heads/chassiss/work/" + taskID + "/" + actor,
	}
}

func worktreePath(project *projectContext, taskID, actor, base string) string {
	return filepath.Join(
		project.Store.Paths.Data, "worktrees", project.Verified.State.Project.ID,
		taskID, base[:12], actor,
	)
}
