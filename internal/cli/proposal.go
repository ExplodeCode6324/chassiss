package cli

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/ExplodeCode6324/chassiss/internal/gitstore"
	"github.com/ExplodeCode6324/chassiss/internal/localstate"
	"github.com/ExplodeCode6324/chassiss/internal/protocol"
	"github.com/ExplodeCode6324/chassiss/internal/state"
	"github.com/ExplodeCode6324/chassiss/internal/verifier"
)

func transitionCommand(ctx context.Context, invocation invocation) (Envelope, error) {
	project, err := loadProject(ctx, "", false)
	if err != nil {
		return Envelope{}, err
	}
	commit, source, err := resolveProposal(ctx, project, invocation.Positionals[0])
	if err != nil {
		return Envelope{}, err
	}
	verified, err := verifier.Verify(ctx, project.Runner, commit, verifier.Options{
		ExpectedProject:         project.Verified.State.Project.ID,
		ExpectedRootFingerprint: project.LocalProject.RootFingerprint,
		MinimumCheckpoint:       project.LocalProject.MinimumCheckpoint.Commit,
	})
	if err != nil {
		return Envelope{}, err
	}
	candidate, err := project.Runner.ReadCommit(ctx, commit)
	if err != nil {
		return Envelope{}, err
	}
	message, err := protocol.ParseTransitionMessage(candidate.Message, project.Verified.ObjectFormat)
	if err != nil {
		return Envelope{}, err
	}
	if invocation.Definition.Path == "transition inspect" {
		envelope := projectEnvelope("transition inspect", project)
		envelope.Result = map[string]any{
			"commit": commit, "evidence": message.Evidence,
			"operation": message.Operation, "source": source,
			"state_digest": verified.StateDigest,
		}
		return envelope, nil
	}
	if len(candidate.Parents) == 0 || candidate.Parents[0] != project.Verified.Head {
		return Envelope{}, &protocol.Error{
			Code: protocol.ErrProposalStale, Category: protocol.CategoryConflict,
			Message:     "Proposal parent is not the exact current verified main.",
			CurrentHead: project.Verified.Head, OperationID: message.Operation.OperationID,
		}
	}
	status, err := project.Runner.Run(ctx, "status", "--porcelain=v1", "-z")
	if err != nil {
		return Envelope{}, err
	}
	if len(status.Stdout) != 0 {
		return Envelope{}, protocol.NewError(protocol.ErrWorktreeDirty, protocol.CategoryLocal, "Proposal publish requires a clean main worktree.")
	}
	expected, err := proposalPending(ctx, project, commit, candidate, message)
	if err != nil {
		return Envelope{}, err
	}
	publishedHead := commit
	if project.LocalProject.Remote.URL == "" {
		err = project.Runner.UpdateRefCAS(
			ctx, "refs/heads/main", commit, project.Verified.Head,
			"CHASSISS proposal publish",
		)
		if err != nil {
			verified, publishedHead, err = reconcileLocalProposalCAS(
				ctx, project, expected, commit, err,
			)
			if err != nil {
				return Envelope{}, err
			}
		}
	} else {
		_, pushErr := project.Runner.Run(ctx, "push", "--porcelain", "--atomic",
			"--force-with-lease=refs/heads/main:"+project.Verified.Head,
			"origin", commit+":refs/heads/main",
		)
		if pushErr != nil {
			operationDigest, _ := protocol.ObjectDigest("operation", message.Operation)
			reconciliation, reconcileErr := reconcileRemotePush(
				ctx, project, message.Operation.OperationID, operationDigest,
			)
			if reconcileErr != nil {
				failure := protocol.WrapError(
					protocol.ErrPushResultUnknown, protocol.CategoryNetwork,
					"Proposal push result requires reconciliation.", pushErr,
				)
				failure.OperationID = message.Operation.OperationID
				failure.CurrentHead = project.Verified.Head
				failure.Retryable = true
				failure.Details["reconciliation_error"] = reconcileErr.Error()
				return Envelope{}, failure
			}
			if !reconciliation.Found || reconciliation.Commit != commit {
				failure := protocol.NewError(
					protocol.ErrProposalStale, protocol.CategoryConflict,
					"Remote verification confirmed that the exact proposal was not published.",
				)
				failure.OperationID = message.Operation.OperationID
				failure.CurrentHead = reconciliation.Verified.Head
				failure.Details = map[string]any{"candidate_commit": commit}
				return Envelope{}, failure
			}
			verified = reconciliation.Verified
			publishedHead = reconciliation.Verified.Head
		}
		current, resolveErr := project.Runner.Resolve(ctx, "refs/heads/main")
		if resolveErr != nil {
			err = resolveErr
		} else if current != publishedHead {
			err = project.Runner.UpdateRefCAS(
				ctx, "refs/heads/main", publishedHead, project.Verified.Head,
				"CHASSISS proposal publish",
			)
		}
	}
	if err != nil {
		return Envelope{}, protocol.WrapError(protocol.ErrCASRetryExhausted, protocol.CategoryConflict, "Proposal publish CAS failed.", err)
	}
	if _, err := project.Runner.Run(ctx, "read-tree", "--reset", "-u", publishedHead); err != nil {
		return Envelope{}, err
	}
	if err := finalizePending(ctx, project, expected, verified); err != nil {
		return Envelope{}, err
	}
	envelope := projectEnvelope("transition publish", &projectContext{
		InvocationRoot: project.InvocationRoot,
		RepoRoot:       project.RepoRoot, Runner: project.Runner, Store: project.Store,
		Local: project.Local, LocalProject: project.LocalProject, Verified: verified,
		Identity: discoverIdentity(verified.State, project.LocalProject),
	})
	envelope.Result = map[string]any{"commit": commit, "operation_id": message.Operation.OperationID}
	return envelope, nil
}

func reconcileLocalProposalCAS(
	ctx context.Context,
	project *projectContext,
	expected localstate.PendingOperation,
	commit string,
	casErr error,
) (*verifier.Result, string, error) {
	reconciliation, reconcileErr := reconcileLocalCASFailure(ctx, project, expected)
	if reconcileErr != nil {
		var safetyFailure *protocol.Error
		if errors.As(reconcileErr, &safetyFailure) {
			if safetyFailure.Details == nil {
				safetyFailure.Details = map[string]any{}
			}
			safetyFailure.OperationID = expected.OperationID
			safetyFailure.Details["candidate_commit"] = commit
			safetyFailure.Details["cas_error"] = casErr.Error()
			safetyFailure.Details["pending_status"] = string(pendingUnresolved)
			if current, resolveErr := project.Runner.Resolve(ctx, "refs/heads/main"); resolveErr == nil {
				safetyFailure.CurrentHead = current
			}
			return nil, "", safetyFailure
		}
	}
	if reconciliation.Disposition == pendingPublished &&
		reconciliation.Commit == commit {
		return reconciliation.Verified, reconciliation.Verified.Head, nil
	}
	failure := protocol.WrapError(
		protocol.ErrCASRetryExhausted, protocol.CategoryConflict,
		"Proposal publish CAS failed.", casErr,
	)
	failure.OperationID = expected.OperationID
	failure.Details["candidate_commit"] = commit
	failure.Details["pending_status"] = string(reconciliation.Disposition)
	if reconcileErr != nil {
		failure.Details["reconciliation_error"] = reconcileErr.Error()
	}
	if current, resolveErr := project.Runner.Resolve(ctx, "refs/heads/main"); resolveErr == nil {
		failure.CurrentHead = current
	}
	return nil, "", failure
}

func proposalPending(
	ctx context.Context,
	project *projectContext,
	commit string,
	candidate gitstore.Commit,
	message protocol.TransitionMessage,
) (localstate.PendingOperation, error) {
	if len(candidate.Parents) == 0 {
		return localstate.PendingOperation{}, protocol.NewError(
			protocol.ErrProposalStale, protocol.CategoryConflict,
			"Proposal has no expected main parent.",
		)
	}
	operationDigest, err := protocol.ObjectDigest("operation", message.Operation)
	if err != nil {
		return localstate.PendingOperation{}, err
	}
	evidenceDigest, err := protocol.ObjectDigest("execution-evidence", message.Evidence)
	if err != nil {
		return localstate.PendingOperation{}, err
	}
	local, err := project.Store.Load()
	if err != nil {
		return localstate.PendingOperation{}, err
	}
	if value, exists := local.Projects[project.Verified.State.Project.ID]; exists {
		if pending, exists := value.PendingOperations[message.Operation.OperationID]; exists &&
			pending.CandidateCommit != nil && *pending.CandidateCommit == commit &&
			pending.ExpectedMain == candidate.Parents[0] &&
			pending.OperationDigest == operationDigest &&
			pending.EvidenceDigest == evidenceDigest {
			return pending, nil
		}
	}
	semanticBytes, err := protocol.CanonicalJSON(message.Operation)
	if err != nil {
		return localstate.PendingOperation{}, err
	}
	evidenceBytes, err := protocol.CanonicalJSON(message.Evidence)
	if err != nil {
		return localstate.PendingOperation{}, err
	}
	ref := "refs/heads/chassiss/transition/" +
		message.Operation.Action + "/" + message.Operation.OperationID
	return localstate.PendingOperation{
		AuthorityKeyHandle: "", CandidateCommit: stringPointer(commit),
		CandidateEvidence: evidenceBytes, EvidenceDigest: evidenceDigest,
		ExpectedMain: candidate.Parents[0], OperationDigest: operationDigest,
		OperationID: message.Operation.OperationID, SemanticOperation: semanticBytes,
		Status: "signed", TargetRefs: map[string]string{ref: commit},
	}, nil
}

func createProposal(
	ctx context.Context,
	project *projectContext,
	plan transitionPlan,
	output string,
) (Envelope, error) {
	bundleOutput := ""
	if output != "" && !strings.HasPrefix(output, "refs/") {
		if err := requireOutsideProject(project, output); err != nil {
			return Envelope{}, err
		}
		absolute, err := filepath.Abs(output)
		if err != nil {
			return Envelope{}, err
		}
		if _, err := os.Stat(absolute); err == nil {
			return Envelope{}, protocol.NewError(protocol.ErrUsageInvalid, protocol.CategoryLocal, "Proposal bundle output already exists.")
		}
		bundleOutput = absolute
	} else if output != "" {
		expectedRef := "refs/heads/chassiss/transition/" + plan.Operation.Action + "/" + plan.Operation.OperationID
		if output != expectedRef {
			return Envelope{}, usageError("proposal ref output must use the canonical Transition ref")
		}
	}
	stateData, err := state.Encode(plan.NextState, project.Verified.ObjectFormat)
	if err != nil {
		return Envelope{}, err
	}
	stateBlob, err := project.Runner.HashBlob(ctx, stateData)
	if err != nil {
		return Envelope{}, err
	}
	plan.Tree[".chassiss/state.json"] = gitstore.Entry{Mode: "100644", OID: stateBlob}
	tree, err := project.Runner.WriteTree(ctx, plan.Tree)
	if err != nil {
		return Envelope{}, err
	}
	message, err := protocol.BuildTransitionMessage(
		plan.Operation, plan.Evidence, protocol.DigestBytes(stateData),
		project.Verified.ObjectFormat,
	)
	if err != nil {
		return Envelope{}, err
	}
	parents := []string{project.Verified.Head}
	if plan.SecondParent != "" {
		parents = append(parents, plan.SecondParent)
	}
	commit, err := project.Runner.CommitTree(
		ctx, tree, parents, message, plan.Authority.KeyPath, gitstore.CommitIdentity{},
	)
	if err != nil {
		return Envelope{}, err
	}
	if _, err := verifier.Verify(ctx, project.Runner, commit, verifier.Options{
		ExpectedProject:         project.Verified.State.Project.ID,
		ExpectedRootFingerprint: project.LocalProject.RootFingerprint,
		MinimumCheckpoint:       project.LocalProject.MinimumCheckpoint.Commit,
	}); err != nil {
		return Envelope{}, err
	}
	ref := "refs/heads/chassiss/transition/" + plan.Operation.Action + "/" + plan.Operation.OperationID
	if err := project.Runner.UpdateRefCAS(
		ctx, ref, commit, zeroOID(project.Verified.ObjectFormat), "CHASSISS proposal",
	); err != nil {
		return Envelope{}, err
	}
	semanticBytes, _ := protocol.CanonicalJSON(plan.Operation)
	evidenceBytes, _ := protocol.CanonicalJSON(plan.Evidence)
	operationDigest, _ := protocol.ObjectDigest("operation", plan.Operation)
	evidenceDigest, _ := protocol.ObjectDigest("execution-evidence", plan.Evidence)
	candidate := commit
	if err := savePending(project, localstate.PendingOperation{
		AuthorityKeyHandle: plan.Authority.Handle, CandidateCommit: &candidate,
		CandidateEvidence: evidenceBytes, EvidenceDigest: evidenceDigest,
		ExpectedMain: project.Verified.Head, OperationDigest: operationDigest,
		OperationID: plan.Operation.OperationID, SemanticOperation: semanticBytes,
		Status: "signed", TargetRefs: map[string]string{ref: commit},
	}); err != nil {
		return Envelope{}, err
	}
	artifact := ref
	if bundleOutput != "" {
		if err := os.MkdirAll(filepath.Dir(bundleOutput), 0o700); err != nil {
			return Envelope{}, err
		}
		if _, err := project.Runner.Run(ctx, "bundle", "create", bundleOutput, ref); err != nil {
			return Envelope{}, err
		}
		artifact = bundleOutput
	}
	envelope := projectEnvelope(commandForAction(plan.Operation.Action), project)
	envelope.Identity = identityForAuthority(plan.Authority)
	envelope.Operation = &OperationBody{
		Commit: commit, EvidenceAttempt: plan.Evidence.Attempt,
		EvidenceDigest: evidenceDigest, OperationDigest: operationDigest,
		OperationID: plan.Operation.OperationID, Signer: signerForAuthority(plan.Authority),
		Status: "signed",
	}
	envelope.Result = map[string]any{"commit": commit, "proposal": artifact, "ref": ref}
	return envelope, nil
}

func resolveProposal(
	ctx context.Context,
	project *projectContext,
	source string,
) (string, string, error) {
	if strings.HasPrefix(source, "refs/heads/chassiss/transition/") {
		commit, err := project.Runner.Resolve(ctx, source)
		return commit, source, err
	}
	absolute, err := filepath.Abs(source)
	if err != nil {
		return "", "", err
	}
	if _, err := os.Stat(absolute); err != nil {
		return "", "", protocol.WrapError(protocol.ErrPathNotFound, protocol.CategoryLocal, "Proposal bundle does not exist.", err)
	}
	heads, err := project.Runner.Run(ctx, "bundle", "list-heads", absolute)
	if err != nil {
		return "", "", err
	}
	fields := strings.Fields(string(heads.Stdout))
	if len(fields) != 2 || !strings.HasPrefix(fields[1], "refs/heads/chassiss/transition/") {
		return "", "", protocol.NewError(protocol.ErrSchemaInvalid, protocol.CategoryValidation, "Proposal bundle must contain exactly one canonical Transition ref.")
	}
	if _, err := project.Runner.Run(ctx, "bundle", "unbundle", absolute); err != nil {
		return "", "", err
	}
	if err := protocol.ValidateOID(fields[0], project.Verified.ObjectFormat); err != nil {
		return "", "", err
	}
	return fields[0], absolute, nil
}
