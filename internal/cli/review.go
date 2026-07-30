package cli

import (
	"context"
	"fmt"
	"os"

	"github.com/ExplodeCode6324/chassiss/internal/contracts"
	"github.com/ExplodeCode6324/chassiss/internal/gitstore"
	"github.com/ExplodeCode6324/chassiss/internal/localstate"
	"github.com/ExplodeCode6324/chassiss/internal/protocol"
	"github.com/ExplodeCode6324/chassiss/internal/state"
	"github.com/ExplodeCode6324/chassiss/internal/verifier"
	"github.com/ExplodeCode6324/chassiss/internal/workflow"
)

func reviewCommand(ctx context.Context, invocation invocation) (Envelope, error) {
	project, err := loadMutableProject(ctx)
	if err != nil {
		return Envelope{}, err
	}
	taskID := invocation.Positionals[0]
	task, exists := project.Verified.State.Tasks[taskID]
	if !exists || task.Attempt == nil || task.Contract == nil || task.Blocked != nil ||
		(task.Phase != "submitted" && task.Phase != "approved") {
		return Envelope{}, protocol.NewError(protocol.ErrTaskPhaseInvalid, protocol.CategoryReview, "Review requires an unblocked submitted or approved Task with an exact Attempt.")
	}
	resources, contract, err := taskResources(project, taskID)
	if err != nil {
		return Envelope{}, err
	}
	context, candidate, err := buildReviewContext(ctx, project, taskID, task, contract)
	if err != nil {
		return Envelope{}, err
	}
	if invocation.Flags["prepare"] {
		if invocation.Value("verdict") != "" || invocation.Value("report") != "" {
			return Envelope{}, usageError("--prepare cannot be combined with --verdict or --report")
		}
		output := invocation.Value("output")
		if output == "" {
			return Envelope{}, usageError("review --prepare requires --output")
		}
		data, err := protocol.CanonicalJSON(context)
		if err != nil {
			return Envelope{}, err
		}
		if err := writeExternalFile(output, append(data, '\n')); err != nil {
			return Envelope{}, err
		}
		reportTemplate := workflow.NewReviewReportTemplate(contract.ReviewerAttention)
		reportOutput := invocation.Value("report-output")
		if reportOutput != "" {
			templateData, err := protocol.CanonicalJSON(reportTemplate)
			if err != nil {
				return Envelope{}, err
			}
			if err := writeExternalFile(reportOutput, append(templateData, '\n')); err != nil {
				return Envelope{}, err
			}
		}
		envelope := projectEnvelope("review", project)
		envelope.Result = map[string]any{
			"candidate_tree": candidate.TreeOID, "context": context,
			"context_output": output, "prepared": true,
			"report_output": reportOutput, "report_schema": workflow.ReviewReportSchema,
			"report_template": reportTemplate, "task": taskID,
		}
		return envelope, nil
	}
	if invocation.Value("report-output") != "" {
		return Envelope{}, usageError("--report-output requires --prepare")
	}
	verdict := invocation.Value("verdict")
	if verdict != "approve" && verdict != "request_changes" {
		return Envelope{}, usageError("--verdict must be approve or request_changes")
	}
	reportPath := invocation.Value("report")
	if reportPath == "" {
		return Envelope{}, usageError("review requires --report")
	}
	var report workflow.ReviewReport
	reportObject, err := readClosedJSON(reportPath, &report)
	if err != nil {
		return Envelope{}, protocol.WrapError(protocol.ErrReviewReportInvalid, protocol.CategoryReview, "Review Report JSON is invalid.", err)
	}
	if report.Verdict != verdict {
		return Envelope{}, protocol.NewError(protocol.ErrReviewReportInvalid, protocol.CategoryReview, "Review Report verdict does not match --verdict.")
	}
	if err := report.Validate(contract.ReviewerAttention, project.Verified.Architecture.Resources()); err != nil {
		failure := protocol.WrapError(protocol.ErrReviewReportInvalid, protocol.CategoryReview, "Review Report is invalid.", err)
		failure.Details["schema"] = workflow.ReviewReportSchema
		failure.Remediation = []protocol.Remediation{{
			Argv: []string{
				"chassiss", "review", taskID, "--prepare",
				"--output", "<review-context.json>", "--report-output", "<review-report.json>", "--json",
			},
			Description: "Generate a hydrated Review Report template for this exact Attempt.",
		}}
		return Envelope{}, failure
	}
	if err := context.Validate(contract.Checks, project.Verified.ObjectFormat, verdict == "approve"); err != nil {
		return Envelope{}, protocol.WrapError(protocol.ErrReviewContextStale, protocol.CategoryReview, "Review Context or Checks are invalid.", err)
	}
	authority, err := selectGrant(project, invocation, "review.attest", taskID, resources, false)
	if err != nil {
		return Envelope{}, err
	}
	attemptDigest, err := state.AttemptDigest(taskID, task)
	if err != nil {
		return Envelope{}, err
	}
	contextObject, err := objectMap(context)
	if err != nil {
		return Envelope{}, err
	}
	checksValue, err := jsonValue(context.CheckResults)
	if err != nil {
		return Envelope{}, err
	}
	operationID, err := operationID(invocation)
	if err != nil {
		return Envelope{}, err
	}
	operation := protocol.Operation{
		Schema: protocol.OperationSchema, OperationID: operationID,
		Action: "task.reviewed-indexed", Project: project.Verified.State.Project.ID,
		Authority: authority.Reference, Target: taskID,
		Preconditions: map[string]any{"attempt_digest": attemptDigest, "phase": task.Phase},
		Payload:       map[string]any{"report": reportObject, "verdict": verdict},
	}
	operationDigest, _ := protocol.ObjectDigest("operation", operation)
	parent := project.Verified.Head
	evidence := protocol.ExecutionEvidence{
		Schema: protocol.EvidenceSchema, OperationDigest: operationDigest,
		Action: operation.Action, Attempt: 1, Parent: &parent,
		Facts: map[string]any{
			"attempt_digest": attemptDigest, "check_results": checksValue,
			"review_context": contextObject,
		},
	}
	reduceFacts := state.ReduceFacts{
		ObjectFormat: project.Verified.ObjectFormat, ParentCommit: parent,
		SignerFingerprint: authority.Fingerprint, TaskResources: resources,
	}
	next, err := state.Reduce(project.Verified.State, operation, evidence, reduceFacts)
	if err != nil {
		return Envelope{}, err
	}
	tree, err := currentTree(ctx, project)
	if err != nil {
		return Envelope{}, err
	}
	warnings := reviewWarnings(task, authority)
	return publishTransition(ctx, project, transitionPlan{
		Operation: operation, Evidence: evidence, NextState: next, Tree: tree,
		ReduceFacts: reduceFacts, Authority: authority, Warnings: warnings,
		Result: map[string]any{
			"candidate_tree": candidate.TreeOID, "review_context": context,
			"task": taskID, "verdict": verdict,
		},
	})
}

func integrateCommand(ctx context.Context, invocation invocation) (Envelope, error) {
	project, err := loadMutableProject(ctx)
	if err != nil {
		return Envelope{}, err
	}
	if err := requireNoUnresolvedPending(project); err != nil {
		return Envelope{}, err
	}
	taskID := invocation.Positionals[0]
	task, exists := project.Verified.State.Tasks[taskID]
	if !exists || task.Phase != "approved" || task.Blocked != nil ||
		task.Attempt == nil || task.Review == nil || task.Contract == nil {
		return Envelope{}, protocol.NewError(protocol.ErrTaskPhaseInvalid, protocol.CategoryReview, "Integrate requires an unblocked approved Task with an exact Attempt and Review.")
	}
	resources, contract, err := taskResources(project, taskID)
	if err != nil {
		return Envelope{}, err
	}
	authority, err := selectGrant(project, invocation, "integration.apply", taskID, resources, false)
	if err != nil {
		return Envelope{}, err
	}
	drift, candidate, err := integrationCandidate(ctx, project, taskID, task, contract)
	if err != nil {
		return Envelope{}, err
	}
	if err := persistCandidate(ctx, project.Runner, candidate); err != nil {
		return Envelope{}, err
	}
	checkResults, err := runCandidateChecks(
		ctx, project, taskID, task, contract.Checks, candidate.TreeOID, "integration",
	)
	if err != nil {
		return Envelope{}, err
	}
	if !allChecksPassed(checkResults) {
		return Envelope{}, protocol.NewError(protocol.ErrCheckFailed, protocol.CategoryCheck, "One or more Integration Checks failed.")
	}
	attemptDigest, err := state.AttemptDigest(taskID, task)
	if err != nil {
		return Envelope{}, err
	}
	checksValue, err := jsonValue(checkResults)
	if err != nil {
		return Envelope{}, err
	}
	driftValue, err := objectMap(drift)
	if err != nil {
		return Envelope{}, err
	}
	operationID, err := operationID(invocation)
	if err != nil {
		return Envelope{}, err
	}
	operation := protocol.Operation{
		Schema: protocol.OperationSchema, OperationID: operationID,
		Action: "integration.applied", Project: project.Verified.State.Project.ID,
		Authority: authority.Reference, Target: taskID,
		Preconditions: map[string]any{
			"attempt_digest": attemptDigest, "phase": "approved",
			"review_context_digest": task.Review.ContextDigest,
			"review_report_digest":  task.Review.ReportDigest,
		},
		Payload: map[string]any{},
	}
	operationDigest, _ := protocol.ObjectDigest("operation", operation)
	parent := project.Verified.Head
	evidence := protocol.ExecutionEvidence{
		Schema: protocol.EvidenceSchema, OperationDigest: operationDigest,
		Action: operation.Action, Attempt: 1, Parent: &parent,
		Facts: map[string]any{
			"attempt_head": task.Attempt.Head, "candidate_tree": candidate.TreeOID,
			"check_results": checksValue, "drift_classification": driftValue,
			"review_context_digest": task.Review.ContextDigest,
			"review_report_digest":  task.Review.ReportDigest,
		},
	}
	reduceFacts := state.ReduceFacts{
		ObjectFormat: project.Verified.ObjectFormat, ParentCommit: parent,
		SignerFingerprint: authority.Fingerprint, TaskResources: resources,
	}
	next, err := state.Reduce(project.Verified.State, operation, evidence, reduceFacts)
	if err != nil {
		return Envelope{}, err
	}
	envelope, err := publishTransition(ctx, project, transitionPlan{
		Operation: operation, Evidence: evidence, NextState: next, Tree: candidate.Tree,
		ReduceFacts: reduceFacts, SecondParent: task.Attempt.Head, Authority: authority,
		Result: map[string]any{
			"candidate_tree": candidate.TreeOID, "drift_classification": drift,
			"task": taskID,
		},
	})
	if err != nil {
		return Envelope{}, err
	}
	cleanupWarnings := cleanupIntegratedWork(ctx, project, taskID)
	envelope.Warnings = append(envelope.Warnings, cleanupWarnings...)
	return envelope, nil
}

func integrationCandidate(
	ctx context.Context,
	project *projectContext,
	taskID string,
	task state.TaskState,
	contract contracts.Task,
) (workflow.DriftClassification, workflow.Candidate, error) {
	drift, err := verifier.ClassifyDrift(
		ctx, project.Runner, project.Verified.ObjectFormat, project.Verified.State,
		taskID, contract, project.Verified.Architecture, task.Review.ReviewMain,
		project.Verified.Head,
	)
	if err != nil {
		return workflow.DriftClassification{}, workflow.Candidate{}, err
	}
	if drift.Classification != "unrelated" {
		return workflow.DriftClassification{}, workflow.Candidate{}, &protocol.Error{
			Code: protocol.ErrReviewContextStale, Category: protocol.CategoryReview,
			Message: "Relevant mainline drift requires a new Review Context.",
			Details: map[string]any{"drift_classification": drift},
		}
	}
	mainCommit, err := project.Runner.ReadCommit(ctx, project.Verified.Head)
	if err != nil {
		return workflow.DriftClassification{}, workflow.Candidate{}, err
	}
	mainTree, err := project.Runner.ReadTree(ctx, mainCommit.Tree)
	if err != nil {
		return workflow.DriftClassification{}, workflow.Candidate{}, err
	}
	candidate, err := workflow.BuildCandidate(
		ctx, project.Runner, project.Verified.ObjectFormat, mainTree,
		project.Verified.State, taskID,
	)
	if err != nil {
		return workflow.DriftClassification{}, workflow.Candidate{}, protocol.WrapError(
			protocol.ErrCandidateConflict, protocol.CategoryConflict,
			"Candidate overlay failed.", err,
		)
	}
	if violations := scopeViolations(candidate.ChangedPaths, contract.Writes); len(violations) > 0 {
		return workflow.DriftClassification{}, workflow.Candidate{}, &protocol.Error{
			Code: protocol.ErrScopeViolation, Category: protocol.CategoryValidation,
			Message: "Integration candidate exceeds the frozen Task writes.",
			Details: map[string]any{"paths": violations},
		}
	}
	return drift, candidate, nil
}

func buildReviewContext(
	ctx context.Context,
	project *projectContext,
	taskID string,
	task state.TaskState,
	contract contracts.Task,
) (workflow.ReviewContext, workflow.Candidate, error) {
	mainCommit, err := project.Runner.ReadCommit(ctx, project.Verified.Head)
	if err != nil {
		return workflow.ReviewContext{}, workflow.Candidate{}, err
	}
	mainTree, err := project.Runner.ReadTree(ctx, mainCommit.Tree)
	if err != nil {
		return workflow.ReviewContext{}, workflow.Candidate{}, err
	}
	candidate, err := workflow.BuildCandidate(
		ctx, project.Runner, project.Verified.ObjectFormat, mainTree,
		project.Verified.State, taskID,
	)
	if err != nil {
		return workflow.ReviewContext{}, workflow.Candidate{}, protocol.WrapError(
			protocol.ErrCandidateConflict, protocol.CategoryConflict,
			"Review candidate overlay failed.", err,
		)
	}
	if violations := scopeViolations(candidate.ChangedPaths, contract.Writes); len(violations) > 0 {
		return workflow.ReviewContext{}, workflow.Candidate{}, &protocol.Error{
			Code: protocol.ErrScopeViolation, Category: protocol.CategoryValidation,
			Message: "Review candidate exceeds the frozen Task writes.",
			Details: map[string]any{"paths": violations},
		}
	}
	if err := persistCandidate(ctx, project.Runner, candidate); err != nil {
		return workflow.ReviewContext{}, workflow.Candidate{}, err
	}
	results, err := runCandidateChecks(
		ctx, project, taskID, task, contract.Checks, candidate.TreeOID, "review",
	)
	if err != nil {
		return workflow.ReviewContext{}, workflow.Candidate{}, err
	}
	attemptCommit, err := project.Runner.ReadCommit(ctx, task.Attempt.Head)
	if err != nil {
		return workflow.ReviewContext{}, workflow.Candidate{}, err
	}
	closure, err := project.Verified.Architecture.RequiresClosure(
		append(append([]string(nil), contract.Modules...), contract.Affects...),
	)
	if err != nil {
		return workflow.ReviewContext{}, workflow.Candidate{}, err
	}
	reviewContext := workflow.ReviewContext{
		ArtifactTree: attemptCommit.Tree, ArchitectureBlob: task.Contract.ArchitectureBlob,
		AttemptHead: task.Attempt.Head, CandidateTree: candidate.TreeOID,
		CheckResults: results, RequiresClosure: closure, ReviewMain: project.Verified.Head,
		Schema: workflow.ReviewContextSchema, Task: taskID, TaskBase: task.Base,
		TaskbookBlob: task.Contract.TaskbookBlob,
	}
	if err := reviewContext.Validate(contract.Checks, project.Verified.ObjectFormat, false); err != nil {
		return workflow.ReviewContext{}, workflow.Candidate{}, err
	}
	return reviewContext, candidate, nil
}

func persistCandidate(ctx context.Context, runner gitstore.Runner, candidate workflow.Candidate) error {
	stateOID, err := runner.HashBlob(ctx, candidate.StateBytes)
	if err != nil {
		return err
	}
	if candidate.Tree[".chassiss/state.json"].OID != stateOID {
		return fmt.Errorf("persisted candidate State OID mismatch")
	}
	tree, err := runner.WriteTree(ctx, candidate.Tree)
	if err != nil {
		return err
	}
	if tree != candidate.TreeOID {
		return fmt.Errorf("persisted candidate tree OID mismatch")
	}
	return nil
}

func runCandidateChecks(
	ctx context.Context,
	project *projectContext,
	taskID string,
	task state.TaskState,
	specs []contracts.CheckSpec,
	tree string,
	phase string,
) ([]workflow.CheckResult, error) {
	taskValue := taskID
	var results []workflow.CheckResult
	err := withIsolatedTree(ctx, project, tree, func(checkout string) error {
		var runErr error
		results, _, runErr = workflow.RunChecks(ctx, checkout, specs, workflow.CheckBinding{
			ArchitectureBlob: task.Contract.ArchitectureBlob,
			Head:             project.Verified.Head,
			Phase:            phase,
			Task:             &taskValue,
			TaskbookBlob:     task.Contract.TaskbookBlob,
			Tree:             tree,
		})
		return runErr
	})
	return results, err
}

func readClosedJSON(path string, target any) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	value, err := protocol.ParseCanonicalInput(data)
	if err != nil {
		return nil, err
	}
	object, ok := value.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("JSON root must be an object")
	}
	if err := workflow.DecodeClosed(object, target); err != nil {
		return nil, err
	}
	return object, nil
}

func jsonValue(value any) (any, error) {
	data, err := protocol.CanonicalJSON(value)
	if err != nil {
		return nil, err
	}
	return protocol.ParseCanonicalInput(data)
}

func reviewWarnings(task state.TaskState, authority selectedAuthority) []Warning {
	warnings := make([]Warning, 0, 2)
	if authority.Grant != nil && authority.Grant.Actor == task.Actor {
		warnings = append(warnings, Warning{
			Code:    "CHS_WARN_REVIEW_SAME_ACTOR",
			Message: "Reviewer actor is the same as the Attempt submitter actor.",
			Details: map[string]any{"actor": task.Actor},
		})
	}
	if task.Attempt != nil && authority.Fingerprint == task.Attempt.SubmitterKeyFingerprint {
		warnings = append(warnings, Warning{
			Code:    "CHS_WARN_REVIEW_SAME_KEY",
			Message: "Reviewer key is the same as the Attempt submitter key.",
			Details: map[string]any{"key_fingerprint": authority.Fingerprint},
		})
	}
	return warnings
}

func cleanupIntegratedWork(ctx context.Context, project *projectContext, taskID string) []Warning {
	worktree, exists := project.LocalProject.Worktrees[taskID]
	if !exists {
		return nil
	}
	var cleanupErr error
	if _, err := project.Runner.Run(ctx, "worktree", "remove", worktree.Path); err != nil {
		cleanupErr = err
	} else if _, err := project.Runner.Run(ctx, "update-ref", "-d", worktree.Branch); err != nil {
		cleanupErr = err
	} else if project.LocalProject.Remote.URL != "" {
		_, cleanupErr = project.Runner.Run(ctx, "push", "--porcelain", "origin", ":"+worktree.Branch)
	}
	if cleanupErr == nil {
		_ = project.Store.Update(func(local *localstate.State) error {
			value := local.Projects[project.Verified.State.Project.ID]
			delete(value.Worktrees, taskID)
			local.Projects[project.Verified.State.Project.ID] = value
			return nil
		})
		return nil
	}
	return []Warning{{
		Code:    "CHS_WARN_LOCAL_CLEANUP",
		Message: "Integration was published, but managed Work cleanup requires local reconciliation.",
		Details: map[string]any{"task": taskID},
	}}
}
