package cli

import (
	"sort"
	"strings"

	"github.com/ExplodeCode6324/chassiss/internal/protocol"
)

type CommandDefinition struct {
	Path               string     `json:"path"`
	Summary            string     `json:"summary"`
	Mutating           bool       `json:"mutating"`
	Arguments          []Argument `json:"arguments"`
	Options            []Option   `json:"options"`
	RequiredCapability *string    `json:"required_capability"`
	PossibleErrors     []string   `json:"possible_errors"`
	ResultSchema       string     `json:"result_schema"`
}

type Argument struct {
	Name     string `json:"name"`
	Required bool   `json:"required"`
}

type Option struct {
	Name       string `json:"name"`
	Required   bool   `json:"required"`
	Repeatable bool   `json:"repeatable"`
	Boolean    bool   `json:"boolean"`
}

func capability(value string) *string { return &value }

var commandDefinitions = []CommandDefinition{
	{Path: "version", Summary: "Show CLI and protocol versions.", ResultSchema: "chassiss.version/v1"},
	{Path: "help", Summary: "Show the machine-readable command schema.", Arguments: []Argument{{Name: "command-path"}}, ResultSchema: "chassiss.command-schema/v1"},
	{Path: "init", Summary: "Create a signed v1 Genesis.", Mutating: true, Options: []Option{
		{Name: "project", Required: true}, {Name: "architecture", Required: true},
		{Name: "taskbook", Required: true}, {Name: "root-key", Required: true}, {Name: "remote"}, {Name: "operation-id"},
	}, ResultSchema: "chassiss.init/v1"},
	{Path: "clone", Summary: "Clone and bootstrap a trusted Project.", Mutating: true, Arguments: []Argument{{Name: "remote", Required: true}, {Name: "directory", Required: true}}, Options: []Option{
		{Name: "project", Required: true}, {Name: "root-fingerprint"},
		{Name: "checkpoint"}, {Name: "key"}, {Name: "untrusted-read-only", Boolean: true},
	}},
	{Path: "sync", Summary: "Fetch, verify, reconcile, and checkpoint.", Mutating: true, Options: []Option{{Name: "all-work", Boolean: true}, {Name: "prune", Boolean: true}}},
	{Path: "verify", Summary: "Verify signed first-parent history.", Options: []Option{{Name: "full", Boolean: true}, {Name: "commit"}, {Name: "ref"}}},
	{Path: "status", Summary: "Show verified Project and local runtime status.", Options: []Option{{Name: "task"}, {Name: "offline", Boolean: true}}},
	{Path: "context", Summary: "Return deterministic Project or Task context.", Arguments: []Argument{{Name: "task-id"}}, Options: []Option{{Name: "section"}, {Name: "resource"}, {Name: "offline", Boolean: true}}, ResultSchema: "chassiss.context/v1"},
	{Path: "log", Summary: "Read verified Transition history.", Options: []Option{{Name: "task"}, {Name: "action"}, {Name: "operation"}, {Name: "limit"}}},
	{Path: "file show", Summary: "Read a file from a verified tree.", Arguments: []Argument{{Name: "path", Required: true}}, Options: []Option{{Name: "at"}, {Name: "task"}, {Name: "output"}}},
	{Path: "remote show", Summary: "Show authoritative upstream identity."},
	{Path: "remote set", Summary: "Set a verified authoritative upstream.", Mutating: true, Arguments: []Argument{{Name: "url", Required: true}}},
	{Path: "task list", Summary: "List current Task projections.", Options: []Option{{Name: "phase"}, {Name: "available", Boolean: true}, {Name: "actor"}}},
	{Path: "task show", Summary: "Show a Task and effective Contract.", Arguments: []Argument{{Name: "task-id", Required: true}}, Options: []Option{{Name: "history", Boolean: true}}},
	{Path: "task start", Summary: "Start a ready Task and create its managed worktree.", Mutating: true, Arguments: []Argument{{Name: "task-id", Required: true}}, Options: mutationOptions(), RequiredCapability: capability("task.start")},
	{Path: "task release", Summary: "Release unchanged active work.", Mutating: true, Arguments: []Argument{{Name: "task-id", Required: true}}, Options: append(mutationOptions(), Option{Name: "reason", Required: true}), RequiredCapability: capability("task.release")},
	{Path: "task block", Summary: "Block a non-terminal Task.", Mutating: true, Arguments: []Argument{{Name: "task-id", Required: true}}, Options: append(mutationOptions(), Option{Name: "reason", Required: true}), RequiredCapability: capability("task.block")},
	{Path: "task resume", Summary: "Resume a blocked Task.", Mutating: true, Arguments: []Argument{{Name: "task-id", Required: true}}, Options: append(mutationOptions(), Option{Name: "reason"}), RequiredCapability: capability("task.resume")},
	{Path: "task cancel", Summary: "Cancel a non-terminal Task.", Mutating: true, Arguments: []Argument{{Name: "task-id", Required: true}}, Options: append(mutationOptions(), Option{Name: "reason", Required: true}), RequiredCapability: capability("task.cancel")},
	{Path: "task supersede", Summary: "Supersede a non-terminal Task.", Mutating: true, Arguments: []Argument{{Name: "task-id", Required: true}}, Options: append(mutationOptions(), Option{Name: "reason", Required: true}, Option{Name: "replacement"}), RequiredCapability: capability("task.supersede")},
	{Path: "work open", Summary: "Return or restore a managed Task worktree.", Arguments: []Argument{{Name: "task-id", Required: true}}},
	{Path: "work status", Summary: "Show managed worktree status.", Arguments: []Argument{{Name: "task-id", Required: true}}},
	{Path: "work diff", Summary: "Show managed Task changes.", Arguments: []Argument{{Name: "task-id", Required: true}}, Options: []Option{{Name: "path", Repeatable: true}, {Name: "against"}, {Name: "stat", Boolean: true}}},
	{Path: "work log", Summary: "Show the linear managed Work chain.", Arguments: []Argument{{Name: "task-id", Required: true}}, Options: []Option{{Name: "limit"}}},
	{Path: "work commit", Summary: "Create an SSH-signed Work Commit.", Mutating: true, Arguments: []Argument{{Name: "task-id", Required: true}}, Options: []Option{{Name: "message", Required: true}, {Name: "path", Repeatable: true}}},
	{Path: "work restore", Summary: "Restore exact uncommitted paths.", Mutating: true, Arguments: []Argument{{Name: "task-id", Required: true}}, Options: []Option{{Name: "path", Required: true, Repeatable: true}, {Name: "yes", Boolean: true}}},
	{Path: "work remove", Summary: "Remove an eligible managed worktree.", Mutating: true, Arguments: []Argument{{Name: "task-id", Required: true}}, Options: []Option{{Name: "discard-unreachable", Boolean: true}, {Name: "yes", Boolean: true}}},
	{Path: "check", Summary: "Run frozen Task Checks locally.", Arguments: []Argument{{Name: "task-id", Required: true}}},
	{Path: "submit", Summary: "Publish and submit an exact Attempt.", Mutating: true, Arguments: []Argument{{Name: "task-id", Required: true}}, Options: mutationOptions(), RequiredCapability: capability("task.submit")},
	{Path: "review", Summary: "Prepare or attest a semantic Review.", Mutating: true, Arguments: []Argument{{Name: "task-id", Required: true}}, Options: append(mutationOptions(), Option{Name: "prepare", Boolean: true}, Option{Name: "output"}, Option{Name: "verdict"}, Option{Name: "report"}), RequiredCapability: capability("review.attest")},
	{Path: "integrate", Summary: "Apply the exact approved Integration.", Mutating: true, Arguments: []Argument{{Name: "task-id", Required: true}}, Options: mutationOptions(), RequiredCapability: capability("integration.apply")},
	{Path: "taskbook show", Summary: "Show active Taskbook content.", Options: []Option{{Name: "task"}, {Name: "requirement"}, {Name: "constraint"}}},
	{Path: "taskbook draft", Summary: "Copy or create an external Taskbook candidate.", Options: []Option{{Name: "output", Required: true}, {Name: "new", Boolean: true}}},
	{Path: "taskbook validate", Summary: "Validate a Taskbook candidate.", Options: []Option{{Name: "file", Required: true}}},
	{Path: "taskbook diff", Summary: "Diff a Taskbook candidate.", Options: []Option{{Name: "file", Required: true}}},
	{Path: "taskbook open", Summary: "Open a new Taskbook workflow.", Mutating: true, Options: append(mutationOptions(), Option{Name: "file", Required: true}, Option{Name: "reason", Required: true}), RequiredCapability: capability("taskbook.open")},
	{Path: "taskbook update", Summary: "Update ready portions of the Taskbook.", Mutating: true, Options: append(mutationOptions(), Option{Name: "file", Required: true}, Option{Name: "reason", Required: true}), RequiredCapability: capability("taskbook.update")},
	{Path: "taskbook archive", Summary: "Attest closure, run Workflow Checks, and archive.", Mutating: true, Options: append(mutationOptions(), Option{Name: "report", Required: true}), RequiredCapability: capability("taskbook.archive")},
	{Path: "architecture show", Summary: "Show an Architecture Resource.", Arguments: []Argument{{Name: "resource-id", Required: true}}, Options: []Option{{Name: "file"}}},
	{Path: "architecture draft", Summary: "Copy current Architecture outside the repository.", Options: []Option{{Name: "output", Required: true}}},
	{Path: "architecture diff", Summary: "Diff an Architecture candidate.", Options: []Option{{Name: "file", Required: true}}},
	{Path: "architecture requires", Summary: "Query upstream Resource closure.", Arguments: []Argument{{Name: "resource-id", Required: true}}, Options: []Option{{Name: "transitive", Boolean: true}}},
	{Path: "architecture required-by", Summary: "Query downstream Resource closure.", Arguments: []Argument{{Name: "resource-id", Required: true}}, Options: []Option{{Name: "transitive", Boolean: true}}},
	{Path: "architecture impact", Summary: "Query Resource impact.", Arguments: []Argument{{Name: "resource-id", Required: true}}},
	{Path: "architecture validate", Summary: "Validate current or candidate Architecture.", Options: []Option{{Name: "file"}}},
	{Path: "architecture update", Summary: "Update Architecture with no active Taskbook.", Mutating: true, Options: append(mutationOptions(), Option{Name: "file", Required: true}, Option{Name: "reason", Required: true}), RequiredCapability: capability("architecture.update")},
	{Path: "key generate", Summary: "Generate a local Ed25519 key.", Mutating: true, Options: []Option{{Name: "id", Required: true}, {Name: "actor", Required: true}, {Name: "store"}}},
	{Path: "key list", Summary: "List local key handles."},
	{Path: "key show", Summary: "Show a local public key.", Arguments: []Argument{{Name: "key-id", Required: true}}},
	{Path: "key remove", Summary: "Remove eligible local private material.", Mutating: true, Arguments: []Argument{{Name: "key-id", Required: true}}, Options: []Option{{Name: "orphan-grant", Boolean: true}, {Name: "yes", Boolean: true}}},
	{Path: "grant request", Summary: "Create a proof-of-possession Grant Request.", Options: grantRequestOptions()},
	{Path: "grant list", Summary: "List current verified Grants.", Options: []Option{{Name: "actor"}}},
	{Path: "grant show", Summary: "Show a current verified Grant.", Arguments: []Argument{{Name: "grant-id", Required: true}}},
	{Path: "grant add", Summary: "Create a Root-signed Grant Transition.", Mutating: true, Options: grantAddOptions()},
	{Path: "grant revoke", Summary: "Create a Root-signed revoke Transition.", Mutating: true, Arguments: []Argument{{Name: "grant-id", Required: true}}, Options: []Option{{Name: "reason", Required: true}, {Name: "root-key", Required: true}, {Name: "proposal"}, {Name: "operation-id"}}},
	{Path: "transition inspect", Summary: "Inspect an offline signed proposal.", Arguments: []Argument{{Name: "ref-or-bundle", Required: true}}},
	{Path: "transition publish", Summary: "Publish an exact non-stale proposal.", Mutating: true, Arguments: []Argument{{Name: "ref-or-bundle", Required: true}}},
	{Path: "owner apply", Summary: "Apply a restricted human-owned snapshot.", Mutating: true, Options: append(mutationOptions(), Option{Name: "reason", Required: true}, Option{Name: "summary", Required: true}, Option{Name: "yes", Boolean: true}), RequiredCapability: capability("owner.apply")},
	{Path: "export", Summary: "Export a strictly read-only Project view.", Options: []Option{{Name: "ref"}, {Name: "output"}, {Name: "format"}}},
	{Path: "cache clean", Summary: "Delete rebuildable local cache.", Mutating: true, Options: []Option{{Name: "project"}, {Name: "kind"}, {Name: "yes", Boolean: true}}},
}

func mutationOptions() []Option {
	return []Option{{Name: "operation-id"}, {Name: "key"}, {Name: "grant"}}
}

func grantRequestOptions() []Option {
	return []Option{
		{Name: "project", Required: true}, {Name: "key", Required: true},
		{Name: "profile"}, {Name: "capability", Repeatable: true},
		{Name: "task-scope", Required: true, Repeatable: true},
		{Name: "resource-scope", Required: true, Repeatable: true},
		{Name: "limits", Required: true}, {Name: "max-active-tasks"},
		{Name: "max-changed-paths"}, {Name: "output", Required: true},
	}
}

func grantAddOptions() []Option {
	return []Option{
		{Name: "request", Required: true}, {Name: "grant-id", Required: true},
		{Name: "root-key", Required: true}, {Name: "profile"},
		{Name: "capability", Repeatable: true}, {Name: "task-scope", Repeatable: true},
		{Name: "resource-scope", Repeatable: true}, {Name: "limits"},
		{Name: "max-active-tasks"}, {Name: "max-changed-paths"}, {Name: "proposal"}, {Name: "operation-id"},
	}
}

func sortedDefinitions() []CommandDefinition {
	result := append([]CommandDefinition(nil), commandDefinitions...)
	for index := range result {
		if result[index].Arguments == nil {
			result[index].Arguments = []Argument{}
		}
		if result[index].Options == nil {
			result[index].Options = []Option{}
		}
		result[index].PossibleErrors = possibleErrorsFor(result[index])
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Path < result[j].Path })
	return result
}

func possibleErrorsFor(definition CommandDefinition) []string {
	errors := []string{protocol.ErrUsageInvalid}
	localOnly := definition.Path == "version" || definition.Path == "help" ||
		strings.HasPrefix(definition.Path, "key ") || definition.Path == "grant request" ||
		definition.Path == "clone" || definition.Path == "init" ||
		definition.Path == "cache clean"
	if !localOnly {
		errors = append(errors,
			protocol.ErrProjectNotFound,
			protocol.ErrLocalStateCorrupt,
			protocol.ErrProtocolUnsupported,
			protocol.ErrMainlineRollback,
		)
	}
	if definition.RequiredCapability != nil {
		errors = append(errors,
			protocol.ErrGrantNotFound,
			protocol.ErrKeyMismatch,
			protocol.ErrCapabilityDenied,
			protocol.ErrTaskScopeDenied,
			protocol.ErrResourceScopeDenied,
			protocol.ErrLimitExceeded,
		)
	}
	if definition.Mutating && definition.Path != "key generate" &&
		definition.Path != "key remove" && definition.Path != "cache clean" {
		errors = append(errors,
			protocol.ErrPendingUnresolved,
			protocol.ErrRemoteUnreachable,
			protocol.ErrCASRetryExhausted,
		)
	}
	sort.Strings(errors)
	write := 0
	for _, code := range errors {
		if write == 0 || errors[write-1] != code {
			errors[write] = code
			write++
		}
	}
	return errors[:write]
}
