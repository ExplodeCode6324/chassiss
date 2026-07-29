package cli

import (
	"context"
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/ExplodeCode6324/chassiss/internal/contracts"
	"github.com/ExplodeCode6324/chassiss/internal/gitstore"
	"github.com/ExplodeCode6324/chassiss/internal/protocol"
	"github.com/ExplodeCode6324/chassiss/internal/state"
	"github.com/ExplodeCode6324/chassiss/internal/workflow"
)

//go:embed templates/taskbook-new.yaml
var newTaskbookTemplate []byte

//go:embed templates/architecture-new.yaml
var newArchitectureTemplate []byte

const draftMetadataSchema = "chassiss.draft-metadata/v1"

type draftMetadata struct {
	ArchitectureBlob string  `json:"architecture_blob"`
	Kind             string  `json:"kind"`
	Project          string  `json:"project"`
	Schema           string  `json:"schema"`
	TaskbookBlob     *string `json:"taskbook_blob"`
}

func contractCommand(ctx context.Context, invocation invocation) (Envelope, error) {
	switch invocation.Definition.Path {
	case "taskbook show":
		return taskbookShowCommand(ctx, invocation)
	case "taskbook draft":
		return taskbookDraftCommand(ctx, invocation)
	case "taskbook diff":
		return taskbookDiffCommand(ctx, invocation)
	case "taskbook open", "taskbook update":
		return taskbookMutationCommand(ctx, invocation)
	case "taskbook archive":
		return taskbookArchiveCommand(ctx, invocation)
	case "architecture show":
		return architectureShowCommand(ctx, invocation)
	case "architecture draft":
		return architectureDraftCommand(ctx, invocation)
	case "architecture diff":
		return architectureDiffCommand(ctx, invocation)
	case "architecture requires", "architecture required-by", "architecture impact":
		return architectureGraphCommand(ctx, invocation)
	case "architecture establish", "architecture update":
		return architectureUpdateCommand(ctx, invocation)
	default:
		panic("unreachable")
	}
}

func taskbookShowCommand(ctx context.Context, invocation invocation) (Envelope, error) {
	project, err := loadProject(ctx, "", false)
	if err != nil {
		return Envelope{}, err
	}
	if project.Verified.Taskbook == nil {
		return Envelope{}, protocol.NewError(protocol.ErrTaskbookNotActive, protocol.CategoryValidation, "No Taskbook is active.")
	}
	selected := 0
	result := map[string]any{"taskbook": project.Verified.Taskbook}
	for option, collection := range map[string]any{
		"task":        project.Verified.Taskbook.Tasks,
		"requirement": project.Verified.Taskbook.Requirements,
		"constraint":  project.Verified.Taskbook.Constraints,
	} {
		id := invocation.Value(option)
		if id == "" {
			continue
		}
		selected++
		var value any
		var exists bool
		switch typed := collection.(type) {
		case map[string]contracts.Task:
			value, exists = typed[id]
		case map[string]contracts.Requirement:
			value, exists = typed[id]
		case map[string]contracts.Constraint:
			value, exists = typed[id]
		}
		if !exists {
			return Envelope{}, protocol.NewError(protocol.ErrReferenceNotFound, protocol.CategoryValidation, "Selected Taskbook entry does not exist.")
		}
		result = map[string]any{"id": id, "kind": option, "value": value}
	}
	if selected > 1 {
		return Envelope{}, usageError("taskbook show accepts only one selector")
	}
	envelope := projectEnvelope("taskbook show", project)
	envelope.Result = result
	return envelope, nil
}

func taskbookDraftCommand(ctx context.Context, invocation invocation) (Envelope, error) {
	project, err := loadProject(ctx, "", false)
	if err != nil {
		return Envelope{}, err
	}
	if project.Verified.State.Project.Architecture == nil || project.Verified.Architecture == nil {
		return Envelope{}, protocol.NewError(protocol.ErrArchitectureNotEstablished, protocol.CategoryValidation, "Establish Architecture before creating a Taskbook.")
	}
	output := invocation.Value("output")
	if err := requireOutsideProject(project.RepoRoot, output); err != nil {
		return Envelope{}, err
	}
	var data []byte
	var base *string
	if invocation.Flags["new"] {
		if project.Verified.State.Project.Taskbook != nil {
			return Envelope{}, protocol.NewError(protocol.ErrTaskbookAlreadyActive, protocol.CategoryValidation, "A new Taskbook draft requires no active Taskbook.")
		}
		data = append([]byte(nil), newTaskbookTemplate...)
	} else {
		if project.Verified.State.Project.Taskbook == nil {
			return Envelope{}, protocol.NewError(protocol.ErrTaskbookNotActive, protocol.CategoryValidation, "No current Taskbook exists to draft.")
		}
		value := project.Verified.State.Project.Taskbook.BlobOID
		base = &value
		data, err = project.Runner.ReadBlob(ctx, value)
		if err != nil {
			return Envelope{}, err
		}
	}
	if err := writeExternalFile(output, data); err != nil {
		return Envelope{}, err
	}
	metadata := draftMetadata{
		ArchitectureBlob: project.Verified.State.Project.Architecture.BlobOID,
		Kind:             "taskbook", Project: project.Verified.State.Project.ID,
		Schema: draftMetadataSchema, TaskbookBlob: base,
	}
	metadataData, err := protocol.CanonicalJSON(metadata)
	if err != nil {
		return Envelope{}, err
	}
	sidecar := output + ".chassiss.json"
	if err := writeExternalFile(sidecar, append(metadataData, '\n')); err != nil {
		return Envelope{}, err
	}
	envelope := projectEnvelope("taskbook draft", project)
	envelope.Result = map[string]any{"output": output, "sidecar": sidecar}
	return envelope, nil
}

func taskbookDiffCommand(ctx context.Context, invocation invocation) (Envelope, error) {
	project, err := loadProject(ctx, "", false)
	if err != nil {
		return Envelope{}, err
	}
	if project.Verified.Taskbook == nil {
		return Envelope{}, protocol.NewError(protocol.ErrTaskbookNotActive, protocol.CategoryValidation, "No Taskbook is active.")
	}
	candidateData, err := os.ReadFile(invocation.Value("file"))
	if err != nil {
		return Envelope{}, err
	}
	candidate, err := contracts.ParseTaskbook(candidateData, project.Verified.Architecture)
	if err != nil {
		return Envelope{}, protocol.WrapError(protocol.ErrTaskbookInvalid, protocol.CategoryValidation, "Taskbook candidate is invalid.", err)
	}
	if candidate.ID != project.Verified.Taskbook.ID {
		return Envelope{}, protocol.NewError(protocol.ErrTaskbookInvalid, protocol.CategoryValidation, "Active Taskbook ID cannot change.")
	}
	oldData, err := project.Runner.ReadBlob(ctx, project.Verified.State.Project.Taskbook.BlobOID)
	if err != nil {
		return Envelope{}, err
	}
	newBlob, err := gitstore.HashObject("blob", candidateData, project.Verified.ObjectFormat)
	if err != nil {
		return Envelope{}, err
	}
	phases := taskPhases(project.Verified.State)
	semantic, err := contracts.DiffTaskbook(
		project.Verified.Taskbook, candidate,
		project.Verified.State.Project.Taskbook.BlobOID, newBlob, phases,
	)
	if err != nil {
		return Envelope{}, err
	}
	text, err := textualDiff("docs/taskbook.yaml", oldData, "docs/taskbook.yaml", candidateData)
	if err != nil {
		return Envelope{}, err
	}
	envelope := projectEnvelope("taskbook diff", project)
	envelope.Result = map[string]any{"semantic_diff": semantic, "text_diff": text}
	return envelope, nil
}

func taskbookMutationCommand(ctx context.Context, invocation invocation) (Envelope, error) {
	project, err := loadMutableProject(ctx)
	if err != nil {
		return Envelope{}, err
	}
	if project.Verified.State.Project.Architecture == nil || project.Verified.Architecture == nil {
		return Envelope{}, protocol.NewError(protocol.ErrArchitectureNotEstablished, protocol.CategoryValidation, "Establish Architecture before opening or updating a Taskbook.")
	}
	action := "taskbook.opened"
	capability := "taskbook.open"
	if invocation.Definition.Path == "taskbook update" {
		action, capability = "taskbook.updated", "taskbook.update"
	}
	metadata, err := readDraftMetadata(invocation.Value("file"), "taskbook")
	if err != nil {
		return Envelope{}, err
	}
	if metadata.Project != project.Verified.State.Project.ID ||
		metadata.ArchitectureBlob != project.Verified.State.Project.Architecture.BlobOID {
		return Envelope{}, protocol.NewError(protocol.ErrTaskbookStale, protocol.CategoryConflict, "Taskbook draft Architecture base is stale.")
	}
	candidateData, err := os.ReadFile(invocation.Value("file"))
	if err != nil {
		return Envelope{}, err
	}
	candidate, err := contracts.ParseTaskbook(candidateData, project.Verified.Architecture)
	if err != nil {
		return Envelope{}, protocol.WrapError(protocol.ErrTaskbookInvalid, protocol.CategoryValidation, "Taskbook candidate is invalid.", err)
	}
	candidateBlob, err := project.Runner.HashBlob(ctx, candidateData)
	if err != nil {
		return Envelope{}, err
	}
	var targets, resources []string
	var preconditions, evidenceFacts map[string]any
	payload := map[string]any{"candidate_blob": candidateBlob, "reason": invocation.Value("reason")}
	if action == "taskbook.opened" {
		if project.Verified.State.Project.Taskbook != nil || metadata.TaskbookBlob != nil {
			return Envelope{}, protocol.NewError(protocol.ErrTaskbookAlreadyActive, protocol.CategoryValidation, "Taskbook open requires a null Taskbook base.")
		}
		archivePath := "docs/taskbooks/archive/" + candidate.ID + ".yaml"
		tree, err := currentTree(ctx, project)
		if err != nil {
			return Envelope{}, err
		}
		if _, reused := tree[archivePath]; reused {
			return Envelope{}, protocol.NewError(protocol.ErrTaskbookInvalid, protocol.CategoryValidation, "Taskbook ID was already archived and cannot be reused.")
		}
		targets = sortedTaskIDs(candidate)
		targets, resources = contractTargets(candidate, targets)
		preconditions = map[string]any{
			"architecture_blob": project.Verified.State.Project.Architecture.BlobOID,
			"taskbook":          nil,
		}
		evidenceFacts = map[string]any{
			"architecture_blob": project.Verified.State.Project.Architecture.BlobOID,
			"ready_tasks":       stringSliceAny(targets), "taskbook_blob": candidateBlob,
		}
		payload["taskbook_id"] = candidate.ID
	} else {
		if project.Verified.State.Project.Taskbook == nil || metadata.TaskbookBlob == nil ||
			*metadata.TaskbookBlob != project.Verified.State.Project.Taskbook.BlobOID {
			return Envelope{}, protocol.NewError(protocol.ErrTaskbookStale, protocol.CategoryConflict, "Taskbook draft base is stale.")
		}
		if candidate.ID != project.Verified.Taskbook.ID {
			return Envelope{}, protocol.NewError(protocol.ErrTaskbookInvalid, protocol.CategoryValidation, "Active Taskbook ID cannot change.")
		}
		if candidateBlob == project.Verified.State.Project.Taskbook.BlobOID {
			return Envelope{}, protocol.NewError(protocol.ErrUsageInvalid, protocol.CategoryValidation, "Taskbook update cannot be a no-op.")
		}
		semantic, err := contracts.DiffTaskbook(
			project.Verified.Taskbook, candidate,
			project.Verified.State.Project.Taskbook.BlobOID, candidateBlob,
			taskPhases(project.Verified.State),
		)
		if err != nil {
			return Envelope{}, err
		}
		changedTasks := append(append([]string(nil), semantic.AddedTasks...), semantic.UpdatedReadyTasks...)
		targets, resources = contractTargets(candidate, changedTasks)
		preconditions = map[string]any{"taskbook_blob": project.Verified.State.Project.Taskbook.BlobOID}
		semanticObject, _ := objectMap(semantic)
		evidenceFacts = map[string]any{
			"new_blob": candidateBlob, "old_blob": project.Verified.State.Project.Taskbook.BlobOID,
			"semantic_diff": semanticObject,
		}
	}
	authority, err := selectGrant(project, invocation, capability, "", resources, requiresGlobalTaskbook(action, evidenceFacts))
	if err != nil {
		return Envelope{}, err
	}
	operationID, err := operationID(invocation)
	if err != nil {
		return Envelope{}, err
	}
	operation := protocol.Operation{
		Schema: protocol.OperationSchema, OperationID: operationID, Action: action,
		Project: project.Verified.State.Project.ID, Authority: authority.Reference,
		Target: candidate.ID, Preconditions: preconditions, Payload: payload,
	}
	parent := project.Verified.Head
	operationDigest, _ := protocol.ObjectDigest("operation", operation)
	evidence := protocol.ExecutionEvidence{
		Schema: protocol.EvidenceSchema, OperationDigest: operationDigest,
		Action: action, Attempt: 1, Parent: &parent, Facts: evidenceFacts,
	}
	reduceFacts := state.ReduceFacts{
		ObjectFormat: project.Verified.ObjectFormat, ParentCommit: parent,
		SignerFingerprint: authority.Fingerprint, TargetTasks: targets,
		TargetResources: resources, RequireGlobalScope: requiresGlobalTaskbook(action, evidenceFacts),
	}
	next, err := state.Reduce(project.Verified.State, operation, evidence, reduceFacts)
	if err != nil {
		return Envelope{}, err
	}
	tree, err := currentTree(ctx, project)
	if err != nil {
		return Envelope{}, err
	}
	tree["docs/taskbook.yaml"] = gitstore.Entry{Mode: "100644", OID: candidateBlob}
	return publishTransition(ctx, project, transitionPlan{
		Operation: operation, Evidence: evidence, NextState: next, Tree: tree,
		ReduceFacts: reduceFacts, Authority: authority,
		Result: map[string]any{"taskbook": candidate.ID, "blob": candidateBlob},
	})
}

func architectureShowCommand(ctx context.Context, invocation invocation) (Envelope, error) {
	project, err := loadProject(ctx, "", false)
	if err != nil {
		return Envelope{}, err
	}
	architecture := project.Verified.Architecture
	if file := invocation.Value("file"); file != "" {
		data, err := os.ReadFile(file)
		if err != nil {
			return Envelope{}, err
		}
		architecture, err = contracts.ParseArchitecture(data)
		if err != nil {
			return Envelope{}, err
		}
	}
	if architecture == nil {
		return Envelope{}, protocol.NewError(protocol.ErrArchitectureNotEstablished, protocol.CategoryValidation, "Architecture is not established.")
	}
	id := invocation.Positionals[0]
	resource, exists := architecture.Resources()[id]
	if !exists {
		return Envelope{}, protocol.NewError(protocol.ErrReferenceNotFound, protocol.CategoryValidation, "Architecture Resource does not exist.")
	}
	envelope := projectEnvelope("architecture show", project)
	envelope.Result = map[string]any{"id": id, "resource": resource}
	return envelope, nil
}

func architectureDraftCommand(ctx context.Context, invocation invocation) (Envelope, error) {
	project, err := loadProject(ctx, "", false)
	if err != nil {
		return Envelope{}, err
	}
	output := invocation.Value("output")
	if err := requireOutsideProject(project.RepoRoot, output); err != nil {
		return Envelope{}, err
	}
	base := ""
	var data []byte
	if project.Verified.State.Project.Architecture == nil {
		if !invocation.Flags["new"] {
			return Envelope{}, protocol.NewError(protocol.ErrArchitectureNotEstablished, protocol.CategoryValidation, "Source bootstrap requires architecture draft --new.")
		}
		data = append([]byte(nil), newArchitectureTemplate...)
	} else {
		if invocation.Flags["new"] {
			return Envelope{}, protocol.NewError(protocol.ErrArchitectureInvalid, protocol.CategoryValidation, "Architecture is already established.")
		}
		base = project.Verified.State.Project.Architecture.BlobOID
		data, err = project.Runner.ReadBlob(ctx, base)
		if err != nil {
			return Envelope{}, err
		}
	}
	if err := writeExternalFile(output, data); err != nil {
		return Envelope{}, err
	}
	metadata := draftMetadata{
		ArchitectureBlob: base,
		Kind:             "architecture", Project: project.Verified.State.Project.ID,
		Schema: draftMetadataSchema, TaskbookBlob: nil,
	}
	metadataData, _ := protocol.CanonicalJSON(metadata)
	sidecar := output + ".chassiss.json"
	if err := writeExternalFile(sidecar, append(metadataData, '\n')); err != nil {
		return Envelope{}, err
	}
	envelope := projectEnvelope("architecture draft", project)
	envelope.Result = map[string]any{"output": output, "sidecar": sidecar}
	return envelope, nil
}

func architectureDiffCommand(ctx context.Context, invocation invocation) (Envelope, error) {
	project, err := loadProject(ctx, "", false)
	if err != nil {
		return Envelope{}, err
	}
	if project.Verified.State.Project.Architecture == nil || project.Verified.Architecture == nil {
		return Envelope{}, protocol.NewError(protocol.ErrArchitectureNotEstablished, protocol.CategoryValidation, "Architecture diff requires an established Architecture.")
	}
	candidateData, err := os.ReadFile(invocation.Value("file"))
	if err != nil {
		return Envelope{}, err
	}
	candidate, err := contracts.ParseArchitecture(candidateData)
	if err != nil {
		return Envelope{}, protocol.WrapError(protocol.ErrArchitectureInvalid, protocol.CategoryValidation, "Architecture candidate is invalid.", err)
	}
	if candidate.ID != project.Verified.Architecture.ID {
		return Envelope{}, protocol.NewError(protocol.ErrArchitectureInvalid, protocol.CategoryValidation, "Architecture ID cannot change.")
	}
	oldData, err := project.Runner.ReadBlob(ctx, project.Verified.State.Project.Architecture.BlobOID)
	if err != nil {
		return Envelope{}, err
	}
	newBlob, _ := gitstore.HashObject("blob", candidateData, project.Verified.ObjectFormat)
	semantic := contracts.DiffArchitecture(
		project.Verified.Architecture, candidate,
		project.Verified.State.Project.Architecture.BlobOID, newBlob,
	)
	text, err := textualDiff("docs/architecture.yaml", oldData, "docs/architecture.yaml", candidateData)
	if err != nil {
		return Envelope{}, err
	}
	envelope := projectEnvelope("architecture diff", project)
	envelope.Result = map[string]any{"semantic_diff": semantic, "text_diff": text}
	return envelope, nil
}

func architectureGraphCommand(ctx context.Context, invocation invocation) (Envelope, error) {
	project, err := loadProject(ctx, "", false)
	if err != nil {
		return Envelope{}, err
	}
	if project.Verified.State.Project.Architecture == nil || project.Verified.Architecture == nil {
		return Envelope{}, protocol.NewError(protocol.ErrArchitectureNotEstablished, protocol.CategoryValidation, "Architecture graph queries require an established Architecture.")
	}
	id := invocation.Positionals[0]
	resources := project.Verified.Architecture.Resources()
	if _, exists := resources[id]; !exists {
		return Envelope{}, protocol.NewError(protocol.ErrReferenceNotFound, protocol.CategoryValidation, "Architecture Resource does not exist.")
	}
	result := map[string]any{"resource": id}
	switch invocation.Definition.Path {
	case "architecture requires":
		values := directOrTransitiveRequires(project.Verified.Architecture, id, invocation.Flags["transitive"])
		result["requires"] = values
	case "architecture required-by":
		values := requiredBy(project.Verified.Architecture, id, invocation.Flags["transitive"])
		result["required_by"] = values
	case "architecture impact":
		affected := requiredBy(project.Verified.Architecture, id, true)
		affected = append(affected, id)
		sort.Strings(affected)
		affectedSet := setOf(affected)
		modules := make([]string, 0)
		for _, resourceID := range affected {
			if len(resourceID) > 7 && resourceID[:7] == "module:" {
				modules = append(modules, resourceID)
			}
		}
		tasks := make([]string, 0)
		if project.Verified.Taskbook != nil {
			for taskID, task := range project.Verified.Taskbook.Tasks {
				closure, _ := project.Verified.Architecture.RequiresClosure(
					append(append([]string(nil), task.Modules...), task.Affects...),
				)
				for _, resourceID := range closure {
					if _, hit := affectedSet[resourceID]; hit {
						tasks = append(tasks, taskID)
						break
					}
				}
			}
		}
		sort.Strings(tasks)
		result["affected_resources"], result["modules"], result["tasks"] = affected, modules, tasks
	}
	envelope := projectEnvelope(invocation.Definition.Path, project)
	envelope.Result = result
	return envelope, nil
}

func architectureUpdateCommand(ctx context.Context, invocation invocation) (Envelope, error) {
	project, err := loadMutableProject(ctx)
	if err != nil {
		return Envelope{}, err
	}
	establish := invocation.Definition.Path == "architecture establish"
	if establish {
		if project.Verified.State.Project.Architecture != nil || project.Verified.Architecture != nil {
			return Envelope{}, protocol.NewError(protocol.ErrArchitectureInvalid, protocol.CategoryValidation, "Architecture is already established.")
		}
	} else if project.Verified.State.Project.Architecture == nil || project.Verified.Architecture == nil {
		return Envelope{}, protocol.NewError(protocol.ErrArchitectureNotEstablished, protocol.CategoryValidation, "Use architecture establish for a source bootstrap.")
	}
	if project.Verified.State.Project.Taskbook != nil {
		return Envelope{}, protocol.NewError(protocol.ErrTaskbookAlreadyActive, protocol.CategoryValidation, "Architecture update requires no active Taskbook.")
	}
	metadata, err := readDraftMetadata(invocation.Value("file"), "architecture")
	if err != nil {
		return Envelope{}, err
	}
	expectedBase := ""
	if project.Verified.State.Project.Architecture != nil {
		expectedBase = project.Verified.State.Project.Architecture.BlobOID
	}
	if metadata.Project != project.Verified.State.Project.ID ||
		metadata.ArchitectureBlob != expectedBase {
		return Envelope{}, protocol.NewError(protocol.ErrArchitectureStale, protocol.CategoryConflict, "Architecture draft base is stale.")
	}
	candidateData, err := os.ReadFile(invocation.Value("file"))
	if err != nil {
		return Envelope{}, err
	}
	candidate, err := contracts.ParseArchitecture(candidateData)
	if err != nil {
		return Envelope{}, protocol.WrapError(protocol.ErrArchitectureInvalid, protocol.CategoryValidation, "Architecture candidate is invalid.", err)
	}
	if !establish && candidate.ID != project.Verified.Architecture.ID {
		return Envelope{}, protocol.NewError(protocol.ErrArchitectureInvalid, protocol.CategoryValidation, "Architecture ID cannot change.")
	}
	newBlob, err := project.Runner.HashBlob(ctx, candidateData)
	if err != nil {
		return Envelope{}, err
	}
	if !establish && newBlob == expectedBase {
		return Envelope{}, protocol.NewError(protocol.ErrUsageInvalid, protocol.CategoryValidation, "Architecture update cannot be a no-op.")
	}
	var semanticObject map[string]any
	targets := make([]string, 0)
	requireGlobal := true
	if establish {
		for id := range candidate.Resources() {
			targets = append(targets, id)
		}
	} else {
		semantic := contracts.DiffArchitecture(project.Verified.Architecture, candidate, expectedBase, newBlob)
		targets = append(append(append([]string(nil), semantic.AddedResources...), semantic.UpdatedResources...), semantic.RemovedResources...)
		requireGlobal = semantic.OverviewChanged || semantic.PrinciplesChanged
		semanticObject, _ = objectMap(semantic)
	}
	sort.Strings(targets)
	capability := "architecture.update"
	action := "architecture.updated"
	if establish {
		capability = "architecture.establish"
		action = "architecture.established"
	}
	authority, err := selectGrant(project, invocation, capability, "", targets, requireGlobal)
	if err != nil {
		return Envelope{}, err
	}
	operationID, err := operationID(invocation)
	if err != nil {
		return Envelope{}, err
	}
	operation := protocol.Operation{
		Schema: protocol.OperationSchema, OperationID: operationID,
		Action: action, Project: project.Verified.State.Project.ID,
		Authority: authority.Reference, Target: candidate.ID,
		Preconditions: map[string]any{},
		Payload:       map[string]any{"candidate_blob": newBlob, "reason": invocation.Value("reason")},
	}
	if establish {
		operation.Preconditions = map[string]any{"architecture": nil, "taskbook": nil}
	} else {
		operation.Preconditions = map[string]any{"architecture_blob": expectedBase, "taskbook": nil}
	}
	parent := project.Verified.Head
	operationDigest, _ := protocol.ObjectDigest("operation", operation)
	evidence := protocol.ExecutionEvidence{
		Schema: protocol.EvidenceSchema, OperationDigest: operationDigest,
		Action: operation.Action, Attempt: 1, Parent: &parent,
		Facts: map[string]any{"new_blob": newBlob},
	}
	if !establish {
		evidence.Facts["old_blob"] = expectedBase
		evidence.Facts["semantic_diff"] = semanticObject
	}
	reduceFacts := state.ReduceFacts{
		ObjectFormat: project.Verified.ObjectFormat, ParentCommit: parent,
		SignerFingerprint: authority.Fingerprint, TargetResources: targets,
		RequireGlobalScope: requireGlobal,
	}
	next, err := state.Reduce(project.Verified.State, operation, evidence, reduceFacts)
	if err != nil {
		return Envelope{}, err
	}
	tree, err := currentTree(ctx, project)
	if err != nil {
		return Envelope{}, err
	}
	tree["docs/architecture.yaml"] = gitstore.Entry{Mode: "100644", OID: newBlob}
	return publishTransition(ctx, project, transitionPlan{
		Operation: operation, Evidence: evidence, NextState: next, Tree: tree,
		ReduceFacts: reduceFacts, Authority: authority,
		Result: map[string]any{"architecture": candidate.ID, "blob": newBlob},
	})
}

func readDraftMetadata(candidate, kind string) (draftMetadata, error) {
	var metadata draftMetadata
	_, err := readClosedJSON(candidate+".chassiss.json", &metadata)
	if err != nil {
		return metadata, protocol.WrapError(protocol.ErrPathNotFound, protocol.CategoryLocal, "Candidate sidecar metadata is missing or invalid.", err)
	}
	if metadata.Schema != draftMetadataSchema || metadata.Kind != kind {
		return metadata, protocol.NewError(protocol.ErrSchemaInvalid, protocol.CategoryValidation, "Candidate sidecar kind/schema is invalid.")
	}
	return metadata, nil
}

func taskPhases(shared *state.State) map[string]string {
	result := make(map[string]string, len(shared.Tasks))
	for id, task := range shared.Tasks {
		result[id] = task.Phase
	}
	return result
}

func sortedTaskIDs(taskbook *contracts.Taskbook) []string {
	result := make([]string, 0, len(taskbook.Tasks))
	for id := range taskbook.Tasks {
		result = append(result, id)
	}
	sort.Strings(result)
	return result
}

func contractTargets(taskbook *contracts.Taskbook, tasks []string) ([]string, []string) {
	resources := make([]string, 0)
	for _, id := range tasks {
		task := taskbook.Tasks[id]
		resources = append(resources, task.Modules...)
		resources = append(resources, task.Affects...)
	}
	sort.Strings(tasks)
	return sortedUniquePaths(tasks), sortedUniquePaths(resources)
}

func requiresGlobalTaskbook(action string, evidence map[string]any) bool {
	if action == "taskbook.opened" {
		return false
	}
	diff, _ := evidence["semantic_diff"].(map[string]any)
	workflowChanged, _ := diff["workflow_changed"].(bool)
	extensionsChanged, _ := diff["extensions_changed"].(bool)
	addedRequirements, _ := diff["added_requirements"].([]any)
	addedConstraints, _ := diff["added_constraints"].([]any)
	return workflowChanged || extensionsChanged || len(addedRequirements) > 0 || len(addedConstraints) > 0
}

func stringSliceAny(values []string) any {
	result := make([]any, len(values))
	for index, value := range values {
		result[index] = value
	}
	return result
}

func directOrTransitiveRequires(architecture *contracts.Architecture, id string, transitive bool) []string {
	if transitive {
		values, _ := architecture.RequiresClosure([]string{id})
		result := make([]string, 0, len(values)-1)
		for _, value := range values {
			if value != id {
				result = append(result, value)
			}
		}
		return result
	}
	return append([]string(nil), architecture.Resources()[id].Requires...)
}

func requiredBy(architecture *contracts.Architecture, id string, transitive bool) []string {
	resources := architecture.Resources()
	resultSet := map[string]struct{}{}
	frontier := []string{id}
	for len(frontier) > 0 {
		current := frontier[0]
		frontier = frontier[1:]
		for candidate, resource := range resources {
			for _, required := range resource.Requires {
				if required == current {
					if _, exists := resultSet[candidate]; !exists {
						resultSet[candidate] = struct{}{}
						if transitive {
							frontier = append(frontier, candidate)
						}
					}
				}
			}
		}
		if !transitive {
			break
		}
	}
	result := make([]string, 0, len(resultSet))
	for value := range resultSet {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func setOf(values []string) map[string]struct{} {
	result := make(map[string]struct{}, len(values))
	for _, value := range values {
		result[value] = struct{}{}
	}
	return result
}

func cleanCheckoutChecks(
	ctx context.Context,
	project *projectContext,
	specs []contracts.CheckSpec,
	binding workflow.CheckBinding,
) ([]workflow.CheckResult, error) {
	var results []workflow.CheckResult
	err := withIsolatedTree(ctx, project, binding.Tree, func(checkout string) error {
		var runErr error
		results, _, runErr = workflow.RunChecks(ctx, checkout, specs, binding)
		return runErr
	})
	return results, err
}

func candidatePath(path string) string {
	absolute, _ := filepath.Abs(path)
	return absolute
}

var _ = fmt.Sprintf
