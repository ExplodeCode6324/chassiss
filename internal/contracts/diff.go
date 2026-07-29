package contracts

import (
	"reflect"
	"sort"

	"github.com/ExplodeCode6324/chassiss/internal/protocol"
)

type ArchitectureDiff struct {
	AddedResources    []string `json:"added_resources"`
	ExtensionsChanged bool     `json:"extensions_changed"`
	NewBlob           string   `json:"new_blob"`
	OldBlob           string   `json:"old_blob"`
	OverviewChanged   bool     `json:"overview_changed"`
	PrinciplesChanged bool     `json:"principles_changed"`
	RemovedResources  []string `json:"removed_resources"`
	Schema            string   `json:"schema"`
	UpdatedResources  []string `json:"updated_resources"`
}

type TaskbookDiff struct {
	AddedConstraints  []string `json:"added_constraints"`
	AddedRequirements []string `json:"added_requirements"`
	AddedTasks        []string `json:"added_tasks"`
	ExtensionsChanged bool     `json:"extensions_changed"`
	NewBlob           string   `json:"new_blob"`
	OldBlob           string   `json:"old_blob"`
	Schema            string   `json:"schema"`
	UpdatedReadyTasks []string `json:"updated_ready_tasks"`
	WorkflowChanged   bool     `json:"workflow_changed"`
}

func DiffArchitecture(oldValue, newValue *Architecture, oldBlob, newBlob string) ArchitectureDiff {
	oldResources := oldValue.Resources()
	newResources := newValue.Resources()
	diff := ArchitectureDiff{
		Schema: "chassiss.architecture-diff/v1", OldBlob: oldBlob, NewBlob: newBlob,
		OverviewChanged:   oldValue.Overview != newValue.Overview,
		PrinciplesChanged: !reflect.DeepEqual(oldValue.Principles, newValue.Principles),
		ExtensionsChanged: !reflect.DeepEqual(oldValue.Extensions, newValue.Extensions),
		AddedResources:    []string{},
		RemovedResources:  []string{},
		UpdatedResources:  []string{},
	}
	for id, oldResource := range oldResources {
		newResource, exists := newResources[id]
		switch {
		case !exists:
			diff.RemovedResources = append(diff.RemovedResources, id)
		case !reflect.DeepEqual(oldResource, newResource):
			diff.UpdatedResources = append(diff.UpdatedResources, id)
		}
	}
	for id := range newResources {
		if _, exists := oldResources[id]; !exists {
			diff.AddedResources = append(diff.AddedResources, id)
		}
	}
	sort.Strings(diff.AddedResources)
	sort.Strings(diff.RemovedResources)
	sort.Strings(diff.UpdatedResources)
	return diff
}

func DiffTaskbook(oldValue, newValue *Taskbook, oldBlob, newBlob string, phases map[string]string) (TaskbookDiff, error) {
	diff := TaskbookDiff{
		Schema: "chassiss.taskbook-diff/v1", OldBlob: oldBlob, NewBlob: newBlob,
		ExtensionsChanged: !reflect.DeepEqual(oldValue.Extensions, newValue.Extensions),
		WorkflowChanged:   !reflect.DeepEqual(oldValue.Workflow, newValue.Workflow),
		AddedConstraints:  []string{},
		AddedRequirements: []string{},
		AddedTasks:        []string{},
		UpdatedReadyTasks: []string{},
	}
	for id, oldRequirement := range oldValue.Requirements {
		value, exists := newValue.Requirements[id]
		if !exists || !reflect.DeepEqual(value, oldRequirement) {
			return TaskbookDiff{}, protocol.NewError(protocol.ErrTaskbookInvalid, protocol.CategoryValidation, "Existing Requirement IDs cannot be removed or changed.")
		}
	}
	for id := range newValue.Requirements {
		if _, exists := oldValue.Requirements[id]; !exists {
			diff.AddedRequirements = append(diff.AddedRequirements, id)
		}
	}
	for id, oldConstraint := range oldValue.Constraints {
		value, exists := newValue.Constraints[id]
		if !exists || !reflect.DeepEqual(value, oldConstraint) {
			return TaskbookDiff{}, protocol.NewError(protocol.ErrTaskbookInvalid, protocol.CategoryValidation, "Existing Constraint IDs cannot be removed or changed.")
		}
	}
	for id := range newValue.Constraints {
		if _, exists := oldValue.Constraints[id]; !exists {
			diff.AddedConstraints = append(diff.AddedConstraints, id)
		}
	}
	for id, oldTask := range oldValue.Tasks {
		value, exists := newValue.Tasks[id]
		if !exists {
			return TaskbookDiff{}, protocol.NewError(protocol.ErrTaskbookInvalid, protocol.CategoryValidation, "Task IDs cannot be removed from an active Taskbook.")
		}
		if !reflect.DeepEqual(value, oldTask) {
			if phases[id] != "ready" {
				return TaskbookDiff{}, protocol.NewError(protocol.ErrTaskbookInvalid, protocol.CategoryValidation, "Only ready Task Contracts may be updated.")
			}
			diff.UpdatedReadyTasks = append(diff.UpdatedReadyTasks, id)
		}
	}
	for id := range newValue.Tasks {
		if _, exists := oldValue.Tasks[id]; !exists {
			diff.AddedTasks = append(diff.AddedTasks, id)
		}
	}
	if diff.WorkflowChanged {
		for _, phase := range phases {
			if phase != "ready" {
				return TaskbookDiff{}, protocol.NewError(protocol.ErrTaskbookInvalid, protocol.CategoryValidation, "Workflow is frozen after the first Task starts.")
			}
		}
	}
	sort.Strings(diff.AddedConstraints)
	sort.Strings(diff.AddedRequirements)
	sort.Strings(diff.AddedTasks)
	sort.Strings(diff.UpdatedReadyTasks)
	return diff, nil
}

func (diff ArchitectureDiff) Digest() (string, error) {
	return protocol.ObjectDigest("architecture-semantic-diff", diff)
}

func (diff TaskbookDiff) Digest() (string, error) {
	return protocol.ObjectDigest("taskbook-semantic-diff", diff)
}
