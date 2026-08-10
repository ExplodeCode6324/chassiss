package verifier

import (
	"context"
	"fmt"
	"reflect"
	"sort"
	"strings"

	"github.com/ExplodeCode6324/chassiss/internal/contracts"
	"github.com/ExplodeCode6324/chassiss/internal/gitstore"
	"github.com/ExplodeCode6324/chassiss/internal/protocol"
	"github.com/ExplodeCode6324/chassiss/internal/state"
	"github.com/ExplodeCode6324/chassiss/internal/workflow"
)

func (engine verifier) verifyFacts(
	ctx context.Context,
	parentCommit gitstore.Commit,
	parentTree gitstore.TreeMap,
	parent *state.State,
	commit gitstore.Commit,
	tree gitstore.TreeMap,
	message protocol.TransitionMessage,
) (state.ReduceFacts, *contracts.Architecture, *contracts.Taskbook, error) {
	facts := state.ReduceFacts{
		ObjectFormat: engine.format, ParentCommit: parentCommit.OID,
	}
	architecture, taskbook, err := engine.contractsForState(ctx, parent)
	if err != nil {
		return facts, nil, nil, err
	}
	action := message.Operation.Action
	spec, _ := protocol.Action(action)
	var taskContract contracts.Task
	var frozenArchitecture *contracts.Architecture
	var frozenTaskbook *contracts.Taskbook
	if spec.TaskAction {
		taskContract, frozenArchitecture, frozenTaskbook, err = engine.effectiveTaskContract(ctx, parent, message.Operation.Target)
		if err != nil {
			return facts, nil, nil, err
		}
		facts.TaskResources = uniqueStrings(append(append([]string(nil), taskContract.Modules...), taskContract.Affects...))
	}
	switch action {
	case "architecture.established":
		if architecture != nil || parent.Project.Architecture != nil {
			return facts, nil, nil, protocol.NewError(protocol.ErrArchitectureInvalid, protocol.CategoryValidation, "Architecture is already established.")
		}
		newBlob := stringFact(message.Evidence.Facts, "new_blob")
		newData, err := engine.runner.ReadBlob(ctx, newBlob)
		if err != nil {
			return facts, nil, nil, err
		}
		newArchitecture, err := contracts.ParseArchitecture(newData)
		if err != nil {
			return facts, nil, nil, err
		}
		if message.Operation.Target != newArchitecture.ID {
			return facts, nil, nil, protocol.NewError(protocol.ErrArchitectureInvalid, protocol.CategoryValidation, "Architecture target does not match the candidate ID.")
		}
		if engine.architectureID != "" {
			return facts, nil, nil, protocol.NewError(protocol.ErrArchitectureInvalid, protocol.CategoryValidation, "Architecture was already established in history.")
		}
		for id := range newArchitecture.Resources() {
			facts.TargetResources = append(facts.TargetResources, id)
		}
		sort.Strings(facts.TargetResources)
		facts.RequireGlobalScope = true
	case "architecture.updated", "architecture.updated-compatible":
		if architecture == nil || parent.Project.Architecture == nil {
			return facts, nil, nil, protocol.NewError(protocol.ErrArchitectureInvalid, protocol.CategoryValidation, "Architecture is not established.")
		}
		newBlob := stringFact(message.Evidence.Facts, "new_blob")
		newData, err := engine.runner.ReadBlob(ctx, newBlob)
		if err != nil {
			return facts, nil, nil, err
		}
		newArchitecture, err := contracts.ParseArchitecture(newData)
		if err != nil {
			return facts, nil, nil, err
		}
		if message.Operation.Target != newArchitecture.ID ||
			newArchitecture.ID != architecture.ID ||
			(engine.architectureID != "" && newArchitecture.ID != engine.architectureID) {
			return facts, nil, nil, protocol.NewError(protocol.ErrArchitectureInvalid, protocol.CategoryValidation, "Architecture ID cannot change.")
		}
		if action == "architecture.updated-compatible" {
			if taskbook == nil || parent.Project.Taskbook == nil {
				return facts, nil, nil, protocol.NewError(protocol.ErrTaskbookNotActive, protocol.CategoryValidation, "Compatible Architecture update requires an active Taskbook.")
			}
			taskbookBlob := parent.Project.Taskbook.BlobOID
			if stringFact(message.Operation.Preconditions, "architecture_blob") != parent.Project.Architecture.BlobOID ||
				stringFact(message.Operation.Preconditions, "taskbook_blob") != taskbookBlob ||
				stringFact(message.Evidence.Facts, "old_blob") != parent.Project.Architecture.BlobOID ||
				stringFact(message.Evidence.Facts, "taskbook_blob") != taskbookBlob ||
				!boolFact(message.Operation.Preconditions, "all_tasks_quiescent") {
				return facts, nil, nil, protocol.NewError(protocol.ErrEvidenceInvalid, protocol.CategoryProtocol, "Compatible Architecture update does not bind the exact parent contracts.")
			}
			if inFlight := verifierInFlightTasks(parent); len(inFlight) != 0 {
				return facts, nil, nil, &protocol.Error{
					Code: protocol.ErrTaskbookNotQuiescent, Category: protocol.CategoryConflict,
					Message: "Active Taskbook contains an in-flight Task.",
					Details: map[string]any{"in_flight_tasks": inFlight},
				}
			}
			taskbookData, err := engine.runner.ReadBlob(ctx, taskbookBlob)
			if err != nil {
				return facts, nil, nil, err
			}
			if _, err := contracts.ParseTaskbook(taskbookData, newArchitecture); err != nil {
				return facts, nil, nil, protocol.WrapError(protocol.ErrTaskbookInvalid, protocol.CategoryValidation, "Active Taskbook is incompatible with the candidate Architecture.", err)
			}
		}
		diff := contracts.DiffArchitecture(architecture, newArchitecture, parent.Project.Architecture.BlobOID, newBlob)
		if !canonicalEqual(diff, message.Evidence.Facts["semantic_diff"]) {
			return facts, nil, nil, protocol.NewError(protocol.ErrArchitectureInvalid, protocol.CategoryValidation, "Architecture semantic diff does not match the candidate.")
		}
		for _, id := range diff.AddedResources {
			if _, reused := engine.seenResources[id]; reused {
				return facts, nil, nil, protocol.NewError(protocol.ErrArchitectureInvalid, protocol.CategoryValidation, "A removed Architecture Resource ID cannot be reused.")
			}
		}
		facts.TargetResources = uniqueStrings(append(append(diff.AddedResources, diff.UpdatedResources...), diff.RemovedResources...))
		facts.RequireGlobalScope = diff.OverviewChanged || diff.PrinciplesChanged
	case "taskbook.opened":
		newBlob := stringFact(message.Evidence.Facts, "taskbook_blob")
		newData, err := engine.runner.ReadBlob(ctx, newBlob)
		if err != nil {
			return facts, nil, nil, err
		}
		newTaskbook, err := contracts.ParseTaskbook(newData, architecture)
		if err != nil {
			return facts, nil, nil, err
		}
		if _, reused := engine.seenTaskbookIDs[newTaskbook.ID]; reused {
			return facts, nil, nil, protocol.NewError(protocol.ErrTaskbookInvalid, protocol.CategoryValidation, "Taskbook ID cannot be reused.")
		}
		facts.TargetTasks, facts.TargetResources = taskbookTargets(newTaskbook, allTaskIDs(newTaskbook))
	case "taskbook.updated":
		newBlob := stringFact(message.Evidence.Facts, "new_blob")
		newData, err := engine.runner.ReadBlob(ctx, newBlob)
		if err != nil {
			return facts, nil, nil, err
		}
		newTaskbook, err := contracts.ParseTaskbook(newData, architecture)
		if err != nil {
			return facts, nil, nil, err
		}
		phases := make(map[string]string, len(parent.Tasks))
		for id, task := range parent.Tasks {
			phases[id] = task.Phase
		}
		diff, err := contracts.DiffTaskbook(taskbook, newTaskbook, parent.Project.Taskbook.BlobOID, newBlob, phases)
		if err != nil {
			return facts, nil, nil, err
		}
		if !canonicalEqual(diff, message.Evidence.Facts["semantic_diff"]) {
			return facts, nil, nil, protocol.NewError(protocol.ErrTaskbookInvalid, protocol.CategoryValidation, "Taskbook semantic diff does not match the candidate.")
		}
		changedTasks := append(append([]string(nil), diff.AddedTasks...), diff.UpdatedReadyTasks...)
		facts.TargetTasks, facts.TargetResources = taskbookTargets(newTaskbook, changedTasks)
		facts.RequireGlobalScope = diff.WorkflowChanged || diff.ExtensionsChanged ||
			len(diff.AddedRequirements) > 0 || len(diff.AddedConstraints) > 0
	case "taskbook.archived":
		facts.RequireGlobalScope = true
		if err := engine.verifyArchiveFacts(ctx, parentCommit, parent, architecture, taskbook, message); err != nil {
			return facts, nil, nil, err
		}
	case "task.started":
		grantID := strings.TrimPrefix(message.Operation.Authority, "grant:")
		grant := parent.Authority.Grants[grantID]
		for _, task := range parent.Tasks {
			if task.Actor == grant.Actor && (task.Phase == "active" || task.Phase == "submitted" || task.Phase == "approved") {
				facts.ActiveTasksForActor++
			}
		}
		if err := verifyStartPreconditions(parent, taskbook, architecture, message.Operation.Target); err != nil {
			return facts, nil, nil, err
		}
	case "task.released":
		baseCommit, err := engine.runner.ReadCommit(ctx, stringFact(message.Evidence.Facts, "base"))
		if err != nil {
			return facts, nil, nil, err
		}
		if stringFact(message.Evidence.Facts, "observed_work_tree") != baseCommit.Tree {
			return facts, nil, nil, protocol.NewError(protocol.ErrReleaseHasChanges, protocol.CategoryValidation, "Release Work tree is not the frozen base tree.")
		}
	case "attempt.abandoned":
		if err := engine.verifyAbandonedAttempt(ctx, parent, message); err != nil {
			return facts, nil, nil, err
		}
	case "task.submitted":
		changed, err := engine.verifySubmission(ctx, parent, taskContract, message)
		if err != nil {
			return facts, nil, nil, err
		}
		facts.ChangedPaths = int64(len(changed))
		if taskContract.ChangeLimits != nil {
			limit := taskContract.ChangeLimits.MaxChangedPaths
			facts.EffectiveMaxChangedPaths = &limit
		}
	case "task.reviewed", "task.reviewed-indexed":
		if err := engine.verifyReview(ctx, parentCommit, parentTree, parent, taskContract, frozenArchitecture, frozenTaskbook, message); err != nil {
			return facts, nil, nil, err
		}
	case "task.cancelled", "task.superseded":
		if err := engine.verifyArchiveRef(ctx, parent, message); err != nil {
			return facts, nil, nil, err
		}
	case "integration.applied":
		if err := engine.verifyIntegration(ctx, parentCommit, parentTree, parent, commit, taskContract, frozenArchitecture, frozenTaskbook, message); err != nil {
			return facts, nil, nil, err
		}
	case "owner.applied":
		facts.RequireGlobalScope = true
		if err := engine.verifyOwner(ctx, parentTree, parent, tree, message); err != nil {
			return facts, nil, nil, err
		}
	}
	return facts, architecture, taskbook, nil
}

func (engine verifier) verifyAbandonedAttempt(
	ctx context.Context,
	parent *state.State,
	message protocol.TransitionMessage,
) error {
	task, exists := parent.Tasks[message.Operation.Target]
	if !exists {
		return protocol.NewError(protocol.ErrReferenceNotFound, protocol.CategoryValidation, "Abandoned Task does not exist.")
	}
	workHead := stringFact(message.Evidence.Facts, "observed_work_head")
	workTree := stringFact(message.Evidence.Facts, "observed_work_tree")
	commit, err := engine.runner.ReadCommit(ctx, workHead)
	if err != nil {
		return protocol.WrapError(protocol.ErrAttemptUnreachable, protocol.CategoryProtocol, "Abandoned Work Head is unreachable.", err)
	}
	if commit.Tree != workTree {
		return protocol.NewError(protocol.ErrEvidenceInvalid, protocol.CategoryProtocol, "Abandoned Work tree does not match its Work Head.")
	}
	descendant, err := engine.runner.IsAncestor(ctx, task.Base, workHead)
	if err != nil || !descendant {
		return protocol.NewError(protocol.ErrAttemptUnreachable, protocol.CategoryProtocol, "Abandoned Work Head is not descended from the frozen Task base.")
	}
	return nil
}

func (engine verifier) effectiveTaskContract(ctx context.Context, parent *state.State, taskID string) (contracts.Task, *contracts.Architecture, *contracts.Taskbook, error) {
	runtimeTask, exists := parent.Tasks[taskID]
	if !exists {
		return contracts.Task{}, nil, nil, protocol.NewError(protocol.ErrReferenceNotFound, protocol.CategoryValidation, "Task does not exist in current State.")
	}
	architectureBlob := parent.Project.Architecture.BlobOID
	taskbookBlob := ""
	if parent.Project.Taskbook != nil {
		taskbookBlob = parent.Project.Taskbook.BlobOID
	}
	if runtimeTask.Contract != nil {
		architectureBlob = runtimeTask.Contract.ArchitectureBlob
		taskbookBlob = runtimeTask.Contract.TaskbookBlob
	}
	architectureData, err := engine.runner.ReadBlob(ctx, architectureBlob)
	if err != nil {
		return contracts.Task{}, nil, nil, err
	}
	architecture, err := contracts.ParseArchitecture(architectureData)
	if err != nil {
		return contracts.Task{}, nil, nil, err
	}
	taskbookData, err := engine.runner.ReadBlob(ctx, taskbookBlob)
	if err != nil {
		return contracts.Task{}, nil, nil, err
	}
	taskbook, err := contracts.ParseTaskbook(taskbookData, architecture)
	if err != nil {
		return contracts.Task{}, nil, nil, err
	}
	contract, exists := taskbook.Tasks[taskID]
	if !exists {
		return contracts.Task{}, nil, nil, protocol.NewError(protocol.ErrReferenceNotFound, protocol.CategoryValidation, "Frozen Taskbook lacks the Task Contract.")
	}
	return contract, architecture, taskbook, nil
}

func verifyStartPreconditions(parent *state.State, taskbook *contracts.Taskbook, architecture *contracts.Architecture, taskID string) error {
	target := taskbook.Tasks[taskID]
	for _, dependency := range target.DependsOn {
		if parent.Tasks[dependency].Phase != "closed" {
			return protocol.NewError(protocol.ErrDependencyUnsatisfied, protocol.CategoryConflict, "Task dependency is not closed.")
		}
	}
	for otherID, runtimeTask := range parent.Tasks {
		if otherID == taskID || (runtimeTask.Phase != "active" && runtimeTask.Phase != "submitted" && runtimeTask.Phase != "approved") {
			continue
		}
		conflict, err := contracts.Conflict(architecture, taskID, target, otherID, taskbook.Tasks[otherID])
		if err != nil {
			return err
		}
		if conflict.Conflict {
			return protocol.NewError(protocol.ErrTaskConflict, protocol.CategoryConflict, "Task conflicts with an occupied Task.")
		}
	}
	return nil
}

func (engine verifier) verifySubmission(ctx context.Context, parent *state.State, contract contracts.Task, message protocol.TransitionMessage) ([]string, error) {
	task := parent.Tasks[message.Operation.Target]
	head := stringFact(message.Operation.Payload, "head")
	headCommit, err := engine.runner.ReadCommit(ctx, head)
	if err != nil {
		return nil, protocol.WrapError(protocol.ErrAttemptUnreachable, protocol.CategoryProtocol, "Attempt Head is unreachable.", err)
	}
	if err := engine.verifyWorkChain(ctx, task.Base, head, task.Actor, contract.Writes); err != nil {
		return nil, err
	}
	baseTree, err := engine.runner.ReadTree(ctx, task.Base)
	if err != nil {
		return nil, err
	}
	workTree, err := engine.runner.ReadTree(ctx, headCommit.Tree)
	if err != nil {
		return nil, err
	}
	changed := gitstore.ChangedPaths(baseTree, workTree)
	if err := validateChangedPaths(changed, contract.Writes); err != nil {
		return nil, err
	}
	var evidence workflow.SubmissionEvidence
	if err := workflow.DecodeClosed(message.Evidence.Facts["submission_evidence"], &evidence); err != nil {
		return nil, protocol.WrapError(protocol.ErrEvidenceInvalid, protocol.CategoryProtocol, "Submission Evidence schema is invalid.", err)
	}
	if evidence.Tree != headCommit.Tree || evidence.Head != head || evidence.Base != task.Base ||
		evidence.Task != message.Operation.Target || task.Contract == nil ||
		evidence.ArchitectureBlob != task.Contract.ArchitectureBlob ||
		evidence.TaskbookBlob != task.Contract.TaskbookBlob {
		return nil, protocol.NewError(protocol.ErrAttemptStale, protocol.CategoryProtocol, "Submission Evidence does not bind the exact frozen Attempt.")
	}
	if err := evidence.Validate(contract.Checks, changed, engine.format); err != nil {
		return nil, err
	}
	return changed, nil
}

func (engine verifier) verifyWorkChain(ctx context.Context, base, head, actor string, writes []string) error {
	current := head
	visited := map[string]struct{}{}
	for current != base {
		if _, exists := visited[current]; exists {
			return protocol.NewError(protocol.ErrMainlineNonLinear, protocol.CategoryProtocol, "Work chain contains a cycle.")
		}
		visited[current] = struct{}{}
		commit, err := engine.runner.ReadCommit(ctx, current)
		if err != nil {
			return err
		}
		if len(commit.Parents) != 1 {
			return protocol.NewError(protocol.ErrMainlineNonLinear, protocol.CategoryProtocol, "Work Commit chain must be single-parent linear.")
		}
		validSignature := false
		for publicKey := range engine.knownKeys[actor] {
			if engine.runner.VerifyCommitSSH(ctx, current, publicKey) == nil {
				validSignature = true
				break
			}
		}
		if !validSignature {
			return protocol.NewError(protocol.ErrCommitSignatureInvalid, protocol.CategoryTrust, "Work Commit signature does not map to the Task Actor.")
		}
		parentTree, err := engine.runner.ReadTree(ctx, commit.Parents[0])
		if err != nil {
			return err
		}
		tree, err := engine.runner.ReadTree(ctx, commit.Tree)
		if err != nil {
			return err
		}
		if err := validateChangedPaths(gitstore.ChangedPaths(parentTree, tree), writes); err != nil {
			return err
		}
		current = commit.Parents[0]
	}
	return nil
}

func validateChangedPaths(paths, writes []string) error {
	scopes := make([]contracts.PathScope, 0, len(writes))
	for _, value := range writes {
		scope, err := contracts.ParsePathScope(value)
		if err != nil {
			return err
		}
		scopes = append(scopes, scope)
	}
	for _, path := range paths {
		if err := contracts.ValidateRepoPath(path); err != nil {
			return protocol.WrapError(protocol.ErrPathEncodingInvalid, protocol.CategoryValidation, "Changed path encoding is invalid.", err)
		}
		if contracts.IsProtectedPath(path) {
			return protocol.NewError(protocol.ErrProtectedPathChanged, protocol.CategoryValidation, "Work changed a protected protocol path.")
		}
		if !contracts.PathWithinAny(path, scopes) {
			return protocol.NewError(protocol.ErrScopeViolation, protocol.CategoryValidation, "Work contains a path outside the frozen Task writes.")
		}
	}
	return nil
}

func (engine verifier) verifyReview(
	ctx context.Context,
	parentCommit gitstore.Commit,
	parentTree gitstore.TreeMap,
	parent *state.State,
	contract contracts.Task,
	architecture *contracts.Architecture,
	taskbook *contracts.Taskbook,
	message protocol.TransitionMessage,
) error {
	task := parent.Tasks[message.Operation.Target]
	var report workflow.ReviewReport
	if err := workflow.DecodeClosed(message.Operation.Payload["report"], &report); err != nil {
		return err
	}
	if report.Verdict != stringFact(message.Operation.Payload, "verdict") {
		return protocol.NewError(protocol.ErrReviewReportInvalid, protocol.CategoryReview, "Review Report verdict does not match the Operation.")
	}
	if err := report.Validate(contract.ReviewerAttention, architecture.Resources()); err != nil {
		return protocol.WrapError(protocol.ErrReviewReportInvalid, protocol.CategoryReview, "Review Report is invalid.", err)
	}
	var reviewContext workflow.ReviewContext
	if err := workflow.DecodeClosed(message.Evidence.Facts["review_context"], &reviewContext); err != nil {
		return err
	}
	if reviewContext.ReviewMain != parentCommit.OID || reviewContext.Task != message.Operation.Target ||
		task.Attempt == nil || reviewContext.AttemptHead != task.Attempt.Head ||
		reviewContext.TaskBase != task.Base || task.Contract == nil ||
		reviewContext.ArchitectureBlob != task.Contract.ArchitectureBlob ||
		reviewContext.TaskbookBlob != task.Contract.TaskbookBlob {
		return protocol.NewError(protocol.ErrReviewContextStale, protocol.CategoryReview, "Review Context is stale.")
	}
	headCommit, err := engine.runner.ReadCommit(ctx, task.Attempt.Head)
	if err != nil {
		return err
	}
	if reviewContext.ArtifactTree != headCommit.Tree {
		return protocol.NewError(protocol.ErrReviewContextStale, protocol.CategoryReview, "Review artifact tree does not match the Attempt.")
	}
	candidate, err := engine.candidateTree(ctx, parentTree, parent, message.Operation.Target)
	if err != nil {
		return err
	}
	if reviewContext.CandidateTree != candidate {
		return protocol.NewError(protocol.ErrReviewContextStale, protocol.CategoryReview, "Review candidate tree does not match deterministic overlay.")
	}
	closure, err := architecture.RequiresClosure(append(append([]string(nil), contract.Modules...), contract.Affects...))
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(reviewContext.RequiresClosure, closure) {
		return protocol.NewError(protocol.ErrReviewContextStale, protocol.CategoryReview, "Review Resource closure is stale.")
	}
	if err := reviewContext.Validate(contract.Checks, engine.format, report.Verdict == "approve"); err != nil {
		return err
	}
	var evidenceResults []workflow.CheckResult
	if err := workflow.DecodeClosed(message.Evidence.Facts["check_results"], &evidenceResults); err != nil ||
		!reflect.DeepEqual(evidenceResults, reviewContext.CheckResults) {
		return protocol.NewError(protocol.ErrEvidenceInvalid, protocol.CategoryProtocol, "Review Check Results are not bound consistently.")
	}
	return nil
}

func (engine verifier) verifyIntegration(
	ctx context.Context,
	parentCommit gitstore.Commit,
	parentTree gitstore.TreeMap,
	parent *state.State,
	commit gitstore.Commit,
	contract contracts.Task,
	architecture *contracts.Architecture,
	taskbook *contracts.Taskbook,
	message protocol.TransitionMessage,
) error {
	task := parent.Tasks[message.Operation.Target]
	if task.Attempt == nil || task.Review == nil || len(commit.Parents) != 2 ||
		commit.Parents[1] != task.Attempt.Head {
		return protocol.NewError(protocol.ErrMainlineNonLinear, protocol.CategoryProtocol, "Integration second parent is not the exact Attempt Head.")
	}
	candidate, err := engine.candidateTree(ctx, parentTree, parent, message.Operation.Target)
	if err != nil {
		return err
	}
	if commit.Tree != candidate || stringFact(message.Evidence.Facts, "candidate_tree") != candidate ||
		stringFact(message.Evidence.Facts, "attempt_head") != task.Attempt.Head ||
		stringFact(message.Evidence.Facts, "review_context_digest") != task.Review.ContextDigest ||
		stringFact(message.Evidence.Facts, "review_report_digest") != task.Review.ReportDigest {
		return protocol.NewError(protocol.ErrIntegrationTreeMismatch, protocol.CategoryReview, "Integration does not bind the exact deterministic candidate.")
	}
	var recordedDrift workflow.DriftClassification
	if err := workflow.DecodeClosed(message.Evidence.Facts["drift_classification"], &recordedDrift); err != nil {
		return protocol.WrapError(protocol.ErrEvidenceInvalid, protocol.CategoryProtocol, "Integration Drift Classification schema is invalid.", err)
	}
	expectedDrift, err := ClassifyDrift(
		ctx, engine.runner, engine.format, parent, message.Operation.Target,
		contract, architecture, task.Review.ReviewMain, parentCommit.OID,
	)
	if err != nil {
		return err
	}
	if !equalDrift(recordedDrift, expectedDrift) {
		return protocol.NewError(protocol.ErrEvidenceInvalid, protocol.CategoryProtocol, "Integration Drift Classification does not match verified Git history.")
	}
	if expectedDrift.Classification != "unrelated" {
		return protocol.NewError(protocol.ErrReviewContextStale, protocol.CategoryReview, "Relevant drift requires a new Review Context.")
	}
	var results []workflow.CheckResult
	if err := workflow.DecodeClosed(message.Evidence.Facts["check_results"], &results); err != nil {
		return err
	}
	taskID := message.Operation.Target
	if err := workflow.ValidateCheckResults(contract.Checks, results, workflow.CheckBinding{
		ArchitectureBlob: task.Contract.ArchitectureBlob, Head: parentCommit.OID,
		Phase: "integration", Task: &taskID, TaskbookBlob: task.Contract.TaskbookBlob,
		Tree: candidate,
	}, true); err != nil {
		return err
	}
	return nil
}

func (engine verifier) candidateTree(ctx context.Context, mainTree gitstore.TreeMap, parent *state.State, taskID string) (string, error) {
	candidate, err := workflow.BuildCandidate(ctx, engine.runner, engine.format, mainTree, parent, taskID)
	if err != nil {
		return "", protocol.WrapError(protocol.ErrCandidateConflict, protocol.CategoryConflict, "Candidate overlay failed.", err)
	}
	return candidate.TreeOID, nil
}

func (engine verifier) verifyArchiveRef(ctx context.Context, parent *state.State, message protocol.TransitionMessage) error {
	task := parent.Tasks[message.Operation.Target]
	if task.Attempt == nil {
		return nil
	}
	ref, ok := message.Evidence.Facts["archive_ref"].(string)
	if !ok {
		return protocol.NewError(protocol.ErrArchiveRefInvalid, protocol.CategoryProtocol, "Attempt Archive Ref is missing.")
	}
	oid, err := engine.runner.Resolve(ctx, ref)
	if err != nil || oid != task.Attempt.Head {
		return protocol.NewError(protocol.ErrArchiveRefInvalid, protocol.CategoryProtocol, "Attempt Archive Ref is missing or changed.")
	}
	return nil
}

func (engine verifier) verifyArchiveFacts(
	ctx context.Context,
	parentCommit gitstore.Commit,
	parent *state.State,
	architecture *contracts.Architecture,
	taskbook *contracts.Taskbook,
	message protocol.TransitionMessage,
) error {
	if taskbook == nil {
		return protocol.NewError(protocol.ErrTaskbookNotActive, protocol.CategoryValidation, "No Taskbook is active.")
	}
	if message.Operation.Target != taskbook.ID ||
		stringFact(message.Evidence.Facts, "active_blob") != parent.Project.Taskbook.BlobOID ||
		stringFact(message.Evidence.Facts, "archive_blob") != parent.Project.Taskbook.BlobOID ||
		stringFact(message.Evidence.Facts, "architecture_blob") != parent.Project.Architecture.BlobOID ||
		stringFact(message.Evidence.Facts, "archive_path") != "docs/taskbooks/archive/"+taskbook.ID+".yaml" {
		return protocol.NewError(protocol.ErrTaskbookClosureStale, protocol.CategoryReview, "Taskbook archive facts do not bind the exact active contracts.")
	}
	var report workflow.ClosureReport
	if err := workflow.DecodeClosed(message.Operation.Payload["closure_report"], &report); err != nil {
		return err
	}
	phases := make(map[string]string, len(parent.Tasks))
	expectedTerminal := make(map[string]any, len(parent.Tasks))
	for id, task := range parent.Tasks {
		phases[id] = task.Phase
		expectedTerminal[id] = task.Phase
	}
	if !canonicalEqual(expectedTerminal, message.Evidence.Facts["terminal_tasks"]) {
		return protocol.NewError(protocol.ErrTaskbookClosureStale, protocol.CategoryReview, "Taskbook terminal projection is stale.")
	}
	expectedIntegrations, err := engine.closingIntegrations(ctx, parentCommit.OID, parent)
	if err != nil {
		return err
	}
	if !canonicalEqual(expectedIntegrations, message.Evidence.Facts["closing_integrations"]) {
		return protocol.NewError(protocol.ErrTaskbookClosureStale, protocol.CategoryReview, "Taskbook closing Integration map is stale.")
	}
	if err := report.Validate(taskbook, phases, architecture.Resources()); err != nil {
		return err
	}
	var results []workflow.CheckResult
	if err := workflow.DecodeClosed(message.Evidence.Facts["check_results"], &results); err != nil {
		return err
	}
	if err := workflow.ValidateCheckResults(taskbook.Workflow.Checks, results, workflow.CheckBinding{
		ArchitectureBlob: parent.Project.Architecture.BlobOID,
		Head:             parentCommit.OID, Phase: "workflow-closure", Task: nil,
		TaskbookBlob: parent.Project.Taskbook.BlobOID, Tree: parentCommit.Tree,
	}, true); err != nil {
		return err
	}
	return nil
}

func (engine verifier) closingIntegrations(ctx context.Context, head string, parent *state.State) (map[string]any, error) {
	needed := make(map[string]struct{})
	for id, task := range parent.Tasks {
		if task.Phase == "closed" {
			needed[id] = struct{}{}
		}
	}
	result := make(map[string]any, len(needed))
	current := head
	for len(needed) > 0 {
		commit, err := engine.runner.ReadCommit(ctx, current)
		if err != nil {
			return nil, err
		}
		message, err := protocol.ParseTransitionMessage(commit.Message, engine.format)
		if err != nil {
			return nil, err
		}
		if message.Operation.Action == "integration.applied" {
			if _, exists := needed[message.Operation.Target]; exists {
				result[message.Operation.Target] = commit.OID
				delete(needed, message.Operation.Target)
			}
		}
		if len(commit.Parents) == 0 {
			break
		}
		current = commit.Parents[0]
	}
	if len(needed) != 0 {
		return nil, protocol.NewError(protocol.ErrTaskbookClosureStale, protocol.CategoryReview, "A closed Task lacks a verified closing Integration.")
	}
	return result, nil
}

func (engine verifier) verifyOwner(ctx context.Context, parentTree gitstore.TreeMap, parent *state.State, resultTree gitstore.TreeMap, message protocol.TransitionMessage) error {
	sourceBase := stringFact(message.Evidence.Facts, "source_base")
	if sourceBase != stringFact(message.Operation.Preconditions, "source_base") ||
		stringFact(message.Evidence.Facts, "source_tree") != stringFact(message.Operation.Preconditions, "source_tree") ||
		!boolFact(message.Operation.Preconditions, "no_active_agent_workflow") {
		return protocol.NewError(protocol.ErrEvidenceInvalid, protocol.CategoryProtocol, "Owner Apply preconditions and Evidence disagree.")
	}
	sourceCommit, err := engine.runner.ReadCommit(ctx, sourceBase)
	if err != nil {
		return err
	}
	base, err := engine.runner.ReadTree(ctx, sourceCommit.Tree)
	if err != nil {
		return err
	}
	changed, err := stringSlice(message.Evidence.Facts["changed_paths"])
	if err != nil {
		return err
	}
	digest, _ := protocol.ObjectDigest("changed-paths", changed)
	if digest != stringFact(message.Evidence.Facts, "changed_paths_digest") {
		return protocol.NewError(protocol.ErrEvidenceInvalid, protocol.CategoryProtocol, "Owner changed-path digest mismatch.")
	}
	for _, path := range changed {
		if contracts.IsProtectedPath(path) {
			return protocol.NewError(protocol.ErrProtectedPathChanged, protocol.CategoryValidation, "Owner Apply changed a protected path.")
		}
	}
	source := cloneTree(base)
	for _, path := range changed {
		if entry, exists := resultTree[path]; exists {
			source[path] = entry
		} else {
			delete(source, path)
		}
	}
	sourceTree, err := gitstore.TreeOID(source, engine.format)
	if err != nil || sourceTree != stringFact(message.Evidence.Facts, "source_tree") {
		return protocol.NewError(protocol.ErrEvidenceInvalid, protocol.CategoryProtocol, "Owner source tree cannot be reconstructed.")
	}
	overlay, _, err := gitstore.Overlay(base, source, parentTree)
	if err != nil {
		return err
	}
	candidate, _ := gitstore.TreeOID(overlay, engine.format)
	if candidate != stringFact(message.Evidence.Facts, "candidate_tree") {
		return protocol.NewError(protocol.ErrCandidateConflict, protocol.CategoryConflict, "Owner candidate tree mismatch.")
	}
	return nil
}

func boolFact(object map[string]any, key string) bool {
	value, _ := object[key].(bool)
	return value
}

func verifierInFlightTasks(shared *state.State) []map[string]any {
	ids := make([]string, 0)
	for id, task := range shared.Tasks {
		if task.Phase == "active" || task.Phase == "submitted" || task.Phase == "approved" {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	result := make([]map[string]any, 0, len(ids))
	for _, id := range ids {
		task := shared.Tasks[id]
		result = append(result, map[string]any{"actor": task.Actor, "phase": task.Phase, "task": id})
	}
	return result
}

func taskbookTargets(taskbook *contracts.Taskbook, taskIDs []string) ([]string, []string) {
	resources := make([]string, 0)
	for _, id := range taskIDs {
		task := taskbook.Tasks[id]
		resources = append(resources, task.Modules...)
		resources = append(resources, task.Affects...)
	}
	return uniqueStrings(taskIDs), uniqueStrings(resources)
}

func allTaskIDs(taskbook *contracts.Taskbook) []string {
	result := make([]string, 0, len(taskbook.Tasks))
	for id := range taskbook.Tasks {
		result = append(result, id)
	}
	sort.Strings(result)
	return result
}

func uniqueStrings(values []string) []string {
	sort.Strings(values)
	result := values[:0]
	for _, value := range values {
		if len(result) == 0 || result[len(result)-1] != value {
			result = append(result, value)
		}
	}
	return result
}

func canonicalEqual(left, right any) bool {
	a, errA := protocol.CanonicalJSON(left)
	b, errB := protocol.CanonicalJSON(right)
	return errA == nil && errB == nil && string(a) == string(b)
}

func stringSlice(value any) ([]string, error) {
	var result []string
	if err := workflow.DecodeClosed(value, &result); err != nil {
		return nil, err
	}
	if !sort.StringsAreSorted(result) {
		return nil, fmt.Errorf("string set is not sorted")
	}
	return result, nil
}

func cloneTree(value gitstore.TreeMap) gitstore.TreeMap {
	result := make(gitstore.TreeMap, len(value))
	for path, entry := range value {
		result[path] = entry
	}
	return result
}
