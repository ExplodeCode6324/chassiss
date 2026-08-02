package protocol

import (
	"fmt"
	"sort"
)

type ActionSpec struct {
	Name               string
	Capability         string
	TaskAction         bool
	RootOnly           bool
	AllowedPathClass   string
	PayloadFields      []string
	PreconditionFields []string
	EvidenceFields     []string
}

var actionSpecs = map[string]ActionSpec{
	"project.bootstrap": {
		Name: "project.bootstrap", RootOnly: true, AllowedPathClass: "genesis",
		PayloadFields: []string{
			"project_id", "root_key_id", "root_public_key", "source_commit",
			"source_history_blob", "source_object_format", "source_tree",
		},
		EvidenceFields: []string{"initial_tree", "source_commit", "source_history_blob", "source_tree"},
	},
	"project.genesis": {
		Name: "project.genesis", RootOnly: true, AllowedPathClass: "genesis",
		PayloadFields:  []string{"architecture_blob", "project_id", "root_key_id", "root_public_key", "taskbook_blob"},
		EvidenceFields: []string{"architecture_blob", "initial_tree", "taskbook_blob"},
	},
	"architecture.established": {
		Name: "architecture.established", Capability: "architecture.establish", AllowedPathClass: "architecture",
		PayloadFields:      []string{"candidate_blob", "reason"},
		PreconditionFields: []string{"architecture", "taskbook"},
		EvidenceFields:     []string{"new_blob"},
	},
	"architecture.updated": {
		Name: "architecture.updated", Capability: "architecture.update", AllowedPathClass: "architecture",
		PayloadFields:      []string{"candidate_blob", "reason"},
		PreconditionFields: []string{"architecture_blob", "taskbook"},
		EvidenceFields:     []string{"new_blob", "old_blob", "semantic_diff"},
	},
	"architecture.updated-compatible": {
		Name: "architecture.updated-compatible", Capability: "architecture.update", AllowedPathClass: "architecture",
		PayloadFields:      []string{"candidate_blob", "reason"},
		PreconditionFields: []string{"all_tasks_quiescent", "architecture_blob", "taskbook_blob"},
		EvidenceFields:     []string{"new_blob", "old_blob", "semantic_diff", "taskbook_blob"},
	},
	"taskbook.opened": {
		Name: "taskbook.opened", Capability: "taskbook.open", AllowedPathClass: "taskbook",
		PayloadFields:      []string{"candidate_blob", "reason", "taskbook_id"},
		PreconditionFields: []string{"architecture_blob", "taskbook"},
		EvidenceFields:     []string{"architecture_blob", "ready_tasks", "taskbook_blob"},
	},
	"taskbook.updated": {
		Name: "taskbook.updated", Capability: "taskbook.update", AllowedPathClass: "taskbook",
		PayloadFields:      []string{"candidate_blob", "reason"},
		PreconditionFields: []string{"taskbook_blob"},
		EvidenceFields:     []string{"new_blob", "old_blob", "semantic_diff"},
	},
	"taskbook.archived": {
		Name: "taskbook.archived", Capability: "taskbook.archive", AllowedPathClass: "taskbook-archive",
		PayloadFields:      []string{"closure_report"},
		PreconditionFields: []string{"all_tasks_terminal", "taskbook_blob"},
		EvidenceFields:     []string{"active_blob", "architecture_blob", "archive_blob", "archive_path", "check_results", "closing_integrations", "terminal_tasks"},
	},
	"task.started": {
		Name: "task.started", Capability: "task.start", TaskAction: true, AllowedPathClass: "state",
		PreconditionFields: []string{"architecture_blob", "phase", "taskbook_blob"},
		EvidenceFields:     []string{"actor", "architecture_blob", "base", "taskbook_blob"},
	},
	"task.released": {
		Name: "task.released", Capability: "task.release", TaskAction: true, AllowedPathClass: "state",
		PayloadFields:      []string{"reason"},
		PreconditionFields: []string{"actor", "base", "phase"},
		EvidenceFields:     []string{"base", "observed_work_head", "observed_work_tree"},
	},
	"attempt.abandoned": {
		Name: "attempt.abandoned", RootOnly: true, TaskAction: true, AllowedPathClass: "state",
		PayloadFields:      []string{"failure"},
		PreconditionFields: []string{"actor", "agent_grant_id", "agent_key_id", "base", "phase"},
		EvidenceFields:     []string{"changed_paths_digest", "observed_work_head", "observed_work_tree"},
	},
	"task.blocked": {
		Name: "task.blocked", Capability: "task.block", TaskAction: true, AllowedPathClass: "state",
		PayloadFields:      []string{"reason"},
		PreconditionFields: []string{"blocked", "phase"},
	},
	"task.resumed": {
		Name: "task.resumed", Capability: "task.resume", TaskAction: true, AllowedPathClass: "state",
		PayloadFields:      []string{"reason"},
		PreconditionFields: []string{"blocked", "phase"},
	},
	"task.submitted": {
		Name: "task.submitted", Capability: "task.submit", TaskAction: true, AllowedPathClass: "state",
		PayloadFields:      []string{"head"},
		PreconditionFields: []string{"actor", "architecture_blob", "base", "phase", "taskbook_blob"},
		EvidenceFields:     []string{"submission_evidence"},
	},
	"task.reviewed": {
		Name: "task.reviewed", Capability: "review.attest", TaskAction: true, AllowedPathClass: "state",
		PayloadFields:      []string{"report", "verdict"},
		PreconditionFields: []string{"attempt_digest", "phase"},
		EvidenceFields:     []string{"attempt_digest", "check_results", "review_context"},
	},
	"task.reviewed-indexed": {
		Name: "task.reviewed-indexed", Capability: "review.attest", TaskAction: true, AllowedPathClass: "state",
		PayloadFields:      []string{"report", "verdict"},
		PreconditionFields: []string{"attempt_digest", "phase"},
		EvidenceFields:     []string{"attempt_digest", "check_results", "review_context"},
	},
	"task.cancelled": {
		Name: "task.cancelled", Capability: "task.cancel", TaskAction: true, AllowedPathClass: "state",
		PayloadFields:      []string{"reason"},
		PreconditionFields: []string{"attempt_digest", "phase"},
		EvidenceFields:     []string{"archive_head", "archive_ref", "attempt_digest"},
	},
	"task.superseded": {
		Name: "task.superseded", Capability: "task.supersede", TaskAction: true, AllowedPathClass: "state",
		PayloadFields:      []string{"reason", "replacement_task"},
		PreconditionFields: []string{"attempt_digest", "phase"},
		EvidenceFields:     []string{"archive_head", "archive_ref", "attempt_digest"},
	},
	"integration.applied": {
		Name: "integration.applied", Capability: "integration.apply", TaskAction: true, AllowedPathClass: "integration",
		PreconditionFields: []string{"attempt_digest", "phase", "review_context_digest", "review_report_digest"},
		EvidenceFields:     []string{"attempt_head", "candidate_tree", "check_results", "drift_classification", "review_context_digest", "review_report_digest"},
	},
	"authority.grant-added": {
		Name: "authority.grant-added", RootOnly: true, AllowedPathClass: "state",
		PayloadFields:      []string{"grant", "grant_id", "request_digest"},
		PreconditionFields: []string{"grant_absent", "root_key_id"},
	},
	"authority.grant-revoked": {
		Name: "authority.grant-revoked", RootOnly: true, AllowedPathClass: "state",
		PayloadFields:      []string{"grant_id", "reason"},
		PreconditionFields: []string{"grant_id", "root_key_id"},
	},
	"owner.applied": {
		Name: "owner.applied", Capability: "owner.apply", AllowedPathClass: "owner",
		PayloadFields:      []string{"reason", "summary"},
		PreconditionFields: []string{"no_active_agent_workflow", "source_base", "source_tree"},
		EvidenceFields:     []string{"candidate_tree", "changed_paths", "changed_paths_digest", "source_base", "source_tree"},
	},
}

var validCapabilities = map[string]struct{}{
	"taskbook.update": {}, "taskbook.open": {}, "taskbook.archive": {},
	"architecture.establish": {}, "architecture.update": {}, "task.start": {}, "task.release": {},
	"task.block": {}, "task.resume": {}, "task.submit": {}, "task.cancel": {},
	"task.supersede": {}, "review.attest": {}, "integration.apply": {},
	"owner.apply": {},
}

func Action(name string) (ActionSpec, bool) {
	spec, ok := actionSpecs[name]
	return spec, ok
}

func Actions() []ActionSpec {
	result := make([]ActionSpec, 0, len(actionSpecs))
	for _, spec := range actionSpecs {
		result = append(result, spec)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result
}

func ValidateCapability(value string) error {
	if _, ok := validCapabilities[value]; !ok {
		return fmt.Errorf("unknown v1 capability %q", value)
	}
	return nil
}
