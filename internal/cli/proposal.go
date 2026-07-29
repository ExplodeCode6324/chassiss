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
	if project.LocalProject.Remote.URL == "" {
		err = project.Runner.UpdateRefCAS(ctx, "refs/heads/main", commit, project.Verified.Head, "CHASSISS proposal publish")
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
		}
		err = project.Runner.UpdateRefCAS(ctx, "refs/heads/main", commit, project.Verified.Head, "CHASSISS proposal publish")
	}
	if err != nil {
		return Envelope{}, protocol.WrapError(protocol.ErrCASRetryExhausted, protocol.CategoryConflict, "Proposal publish CAS failed.", err)
	}
	if _, err := project.Runner.Run(ctx, "read-tree", "--reset", "-u", commit); err != nil {
		return Envelope{}, err
	}
	if err := finalizePending(project, message.Operation.OperationID, verified); err != nil {
		return Envelope{}, err
	}
	envelope := projectEnvelope("transition publish", &projectContext{
		RepoRoot: project.RepoRoot, Runner: project.Runner, Store: project.Store,
		Local: project.Local, LocalProject: project.LocalProject, Verified: verified,
		Identity: discoverIdentity(verified.State, project.LocalProject),
	})
	envelope.Result = map[string]any{"commit": commit, "operation_id": message.Operation.OperationID}
	return envelope, nil
}

func createProposal(
	ctx context.Context,
	project *projectContext,
	plan transitionPlan,
	output string,
) (Envelope, error) {
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
	if output != "" && !strings.HasPrefix(output, "refs/") {
		if err := requireOutsideProject(project.RepoRoot, output); err != nil {
			return Envelope{}, err
		}
		absolute, err := filepath.Abs(output)
		if err != nil {
			return Envelope{}, err
		}
		if _, err := os.Stat(absolute); err == nil {
			return Envelope{}, protocol.NewError(protocol.ErrUsageInvalid, protocol.CategoryLocal, "Proposal bundle output already exists.")
		}
		if err := os.MkdirAll(filepath.Dir(absolute), 0o700); err != nil {
			return Envelope{}, err
		}
		if _, err := project.Runner.Run(ctx, "bundle", "create", absolute, ref); err != nil {
			return Envelope{}, err
		}
		artifact = absolute
	} else if output != "" && output != ref {
		return Envelope{}, usageError("proposal ref output must use the canonical Transition ref")
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
