package cli

import (
	"context"

	"github.com/ExplodeCode6324/chassiss/internal/gitstore"
	"github.com/ExplodeCode6324/chassiss/internal/protocol"
	"github.com/ExplodeCode6324/chassiss/internal/state"
	"github.com/ExplodeCode6324/chassiss/internal/workflow"
)

func taskbookArchiveCommand(ctx context.Context, invocation invocation) (Envelope, error) {
	project, err := loadMutableProject(ctx)
	if err != nil {
		return Envelope{}, err
	}
	if project.Verified.Taskbook == nil || project.Verified.State.Project.Taskbook == nil {
		return Envelope{}, protocol.NewError(protocol.ErrTaskbookNotActive, protocol.CategoryValidation, "No Taskbook is active.")
	}
	terminal := make(map[string]any, len(project.Verified.State.Tasks))
	phases := make(map[string]string, len(project.Verified.State.Tasks))
	closing := make(map[string]any)
	for id, task := range project.Verified.State.Tasks {
		switch task.Phase {
		case "closed", "cancelled", "superseded":
		default:
			return Envelope{}, protocol.NewError(protocol.ErrTaskbookNotComplete, protocol.CategoryValidation, "Every Task must be terminal before Taskbook archive.")
		}
		terminal[id], phases[id] = task.Phase, task.Phase
		if task.Phase == "closed" {
			commit := closingIntegration(project, id)
			if commit == "" {
				return Envelope{}, protocol.NewError(protocol.ErrTaskbookClosureStale, protocol.CategoryReview, "A closed Task lacks its verified closing Integration.")
			}
			closing[id] = commit
		}
	}
	if invocation.Flags["prepare"] {
		if invocation.Value("report") != "" {
			return Envelope{}, usageError("--prepare cannot be combined with --report")
		}
		output := invocation.Value("output")
		if output == "" {
			return Envelope{}, usageError("taskbook archive --prepare requires --output")
		}
		if err := requireOutsideProject(project, output); err != nil {
			return Envelope{}, err
		}
		template := workflow.NewClosureReportTemplate(project.Verified.Taskbook, phases)
		data, err := protocol.CanonicalJSON(template)
		if err != nil {
			return Envelope{}, err
		}
		if err := writeExternalFile(output, append(data, '\n')); err != nil {
			return Envelope{}, err
		}
		envelope := projectEnvelope("taskbook archive", project)
		envelope.Result = map[string]any{
			"output": output, "prepared": true,
			"report_schema":   workflow.ClosureReportSchema,
			"report_template": template, "taskbook": project.Verified.Taskbook.ID,
		}
		return envelope, nil
	}
	if invocation.Value("output") != "" {
		return Envelope{}, usageError("--output requires --prepare")
	}
	if invocation.Value("report") == "" {
		return Envelope{}, usageError("taskbook archive requires --report")
	}
	var report workflow.ClosureReport
	reportObject, err := readClosedJSON(invocation.Value("report"), &report)
	if err != nil {
		return Envelope{}, protocol.WrapError(protocol.ErrSchemaInvalid, protocol.CategoryReview, "Taskbook Closure Report JSON is invalid.", err)
	}
	if err := report.Validate(project.Verified.Taskbook, phases, project.Verified.Architecture.Resources()); err != nil {
		failure := protocol.WrapError(protocol.ErrTaskbookClosureStale, protocol.CategoryReview, "Taskbook Closure Report is invalid or stale.", err)
		failure.Details["schema"] = workflow.ClosureReportSchema
		failure.Remediation = []protocol.Remediation{{
			Argv: []string{
				"chassiss", "taskbook", "archive", "--prepare",
				"--output", "<closure-report.json>", "--json",
			},
			Description: "Generate a hydrated Closure Report template for the exact terminal Task set.",
		}}
		return Envelope{}, failure
	}
	authority, err := selectGrant(project, invocation, "taskbook.archive", "", nil, true)
	if err != nil {
		return Envelope{}, err
	}
	mainCommit, err := project.Runner.ReadCommit(ctx, project.Verified.Head)
	if err != nil {
		return Envelope{}, err
	}
	taskbookBlob := project.Verified.State.Project.Taskbook.BlobOID
	results, err := cleanCheckoutChecks(ctx, project, project.Verified.Taskbook.Workflow.Checks, workflow.CheckBinding{
		ArchitectureBlob: project.Verified.State.Project.Architecture.BlobOID,
		Head:             project.Verified.Head, Phase: "workflow-closure", Task: nil,
		TaskbookBlob: taskbookBlob, Tree: mainCommit.Tree,
	})
	if err != nil {
		return Envelope{}, err
	}
	if !allChecksPassed(results) {
		return Envelope{}, protocol.NewError(protocol.ErrCheckFailed, protocol.CategoryCheck, "One or more Workflow Closure Checks failed.")
	}
	checksValue, err := jsonValue(results)
	if err != nil {
		return Envelope{}, err
	}
	archivePath := "docs/taskbooks/archive/" + project.Verified.Taskbook.ID + ".yaml"
	tree, err := project.Runner.ReadTree(ctx, mainCommit.Tree)
	if err != nil {
		return Envelope{}, err
	}
	if _, exists := tree[archivePath]; exists {
		return Envelope{}, protocol.NewError(protocol.ErrTaskbookInvalid, protocol.CategoryValidation, "Taskbook archive path already exists.")
	}
	delete(tree, "docs/taskbook.yaml")
	tree[archivePath] = gitstore.Entry{Mode: "100644", OID: taskbookBlob}
	operationID, err := operationID(invocation)
	if err != nil {
		return Envelope{}, err
	}
	operation := protocol.Operation{
		Schema: protocol.OperationSchema, OperationID: operationID,
		Action: "taskbook.archived", Project: project.Verified.State.Project.ID,
		Authority: authority.Reference, Target: project.Verified.Taskbook.ID,
		Preconditions: map[string]any{
			"all_tasks_terminal": true, "taskbook_blob": taskbookBlob,
		},
		Payload: map[string]any{"closure_report": reportObject},
	}
	parent := project.Verified.Head
	operationDigest, _ := protocol.ObjectDigest("operation", operation)
	evidence := protocol.ExecutionEvidence{
		Schema: protocol.EvidenceSchema, OperationDigest: operationDigest,
		Action: operation.Action, Attempt: 1, Parent: &parent,
		Facts: map[string]any{
			"active_blob":       taskbookBlob,
			"architecture_blob": project.Verified.State.Project.Architecture.BlobOID,
			"archive_blob":      taskbookBlob, "archive_path": archivePath,
			"check_results": checksValue, "closing_integrations": closing,
			"terminal_tasks": terminal,
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
	return publishTransition(ctx, project, transitionPlan{
		Operation: operation, Evidence: evidence, NextState: next, Tree: tree,
		ReduceFacts: reduceFacts, Authority: authority, Result: map[string]any{
			"archive_blob": taskbookBlob, "archive_path": archivePath,
			"taskbook": project.Verified.Taskbook.ID,
		},
	})
}

func closingIntegration(project *projectContext, taskID string) string {
	for index := len(project.Verified.Transitions) - 1; index >= 0; index-- {
		transition := project.Verified.Transitions[index]
		if transition.Target == taskID && transition.Action == "integration.applied" {
			return transition.Commit
		}
	}
	return ""
}
