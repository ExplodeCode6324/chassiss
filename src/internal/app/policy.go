package app

import (
	"sort"
	"strings"
	"time"
)

const (
	RolePolicyVersion      = 3
	BootstrapSchemaVersion = "chassiss.bootstrap/v3"
)

type commandPolicy struct {
	Command  string
	Action   string
	Usage    string
	Summary  string
	Roles    []string
	Values   []string
	Flags    []string
	Mutating bool
	Expose   bool
}

type PolicyCommand struct {
	Command      string   `json:"command"`
	Action       string   `json:"action,omitempty"`
	Usage        string   `json:"usage"`
	Summary      string   `json:"summary"`
	Mutating     bool     `json:"mutating"`
	ValueOptions []string `json:"value_options,omitempty"`
	FlagOptions  []string `json:"flag_options,omitempty"`
}

type BootstrapPrincipal struct {
	CredentialID string        `json:"credential_id"`
	Actor        string        `json:"actor"`
	Role         string        `json:"role"`
	Actions      []string      `json:"actions"`
	Resources    ResourceScope `json:"resources"`
	NotBefore    *time.Time    `json:"not_before,omitempty"`
	ExpiresAt    *time.Time    `json:"expires_at,omitempty"`
	Persistent   bool          `json:"persistent"`
}

type BootstrapPolicy struct {
	Version    int      `json:"version"`
	Digest     string   `json:"digest"`
	Role       string   `json:"role"`
	Invariants []string `json:"invariants"`
}

type BootstrapAction struct {
	Action         string           `json:"action"`
	Argv           []string         `json:"argv"`
	Resource       string           `json:"resource,omitempty"`
	Reason         string           `json:"reason"`
	Optional       bool             `json:"optional,omitempty"`
	RequiredInputs []BootstrapInput `json:"required_inputs,omitempty"`
	OptionalInputs []BootstrapInput `json:"optional_inputs,omitempty"`
}

type BootstrapInput struct {
	Kind      string `json:"kind"`
	Name      string `json:"name"`
	ValueHint string `json:"value_hint"`
}

type BootstrapContextRequest struct {
	Kind     string   `json:"kind"`
	Argv     []string `json:"argv"`
	Resource string   `json:"resource"`
}

type BootstrapResult struct {
	SchemaVersion    string                    `json:"schema_version"`
	BinaryVersion    string                    `json:"binary_version"`
	ProjectRoot      string                    `json:"project_root"`
	StateRevision    int64                     `json:"state_revision"`
	TrustRevision    int64                     `json:"trust_revision"`
	Principal        BootstrapPrincipal        `json:"principal"`
	Policy           BootstrapPolicy           `json:"policy"`
	Capabilities     []PolicyCommand           `json:"capabilities"`
	AvailableActions []BootstrapAction         `json:"available_actions"`
	ContextRequests  []BootstrapContextRequest `json:"context_requests,omitempty"`
	RefreshOn        []string                  `json:"refresh_on"`
}

var supportedRoles = []string{"designer", "developer", "master", "orchestrator", "owner", "reviewer"}
var issuableRoles = stringSet([]string{"designer", "developer", "orchestrator", "owner", "reviewer"})

var commonPolicyInvariants = []string{
	"Treat the trusted CLI, signed event chain, current trust revision, and projected State as the authority.",
	"Never edit .chassis directly or manufacture workflow facts outside domain commands.",
	"Treat available_actions as a revision-bound projection, not as an authorization token; every command revalidates all rules.",
	"Use structured argv without a shell and refresh bootstrap after a conflict, rejection, trust change, or credential rotation.",
}

var commandPolicies = []commandPolicy{
	{Command: "auth master-init", Usage: "auth master-init [--output <root-file-or-directory>]", Summary: "Create a long-lived Master Root outside any project, defaulting to ~/.chassiss/master-root.yaml.", Values: []string{"output"}, Mutating: true},
	{Command: "project init", Usage: "project init <path> [--existing] [budget options]", Summary: "Initialize a greenfield or brownfield CHASSISS project.", Values: []string{"master-root", "max-changed-files", "max-diff-lines", "max-commits"}, Flags: []string{"existing"}, Mutating: true},
	{Command: "auth inspect", Usage: "auth inspect [credential-path]", Summary: "Inspect non-secret credential metadata.", Roles: supportedRoles},
	{Command: "auth issue", Action: "auth.issue", Usage: "auth issue --actor <actor> --role <role> [--output <path>] [scope options]", Summary: "Issue a role credential and atomically update trust, using the matching local Master Root and a safe default output when omitted.", Roles: []string{"master"}, Values: []string{"master-root", "actor", "role", "output", "actions", "not-before", "expires-at", "ttl-seconds", "projects", "missions", "tasks", "submissions", "submission-digests", "heads", "baselines"}, Flags: []string{"persistent"}, Mutating: true, Expose: true},
	{Command: "auth export", Usage: "auth export <credential-path>", Summary: "Export a role credential as a versioned, checksummed CHASSISS armor block."},
	{Command: "auth import", Usage: "auth import --output <credential-path>", Summary: "Import one CHASSISS credential armor block from standard input.", Values: []string{"output"}, Mutating: true},
	{Command: "auth revoke", Action: "auth.revoke", Usage: "auth revoke <credential-id> [--reason <text>]", Summary: "Revoke a role credential and atomically update trust.", Roles: []string{"master"}, Values: []string{"master-root", "id", "reason"}, Mutating: true, Expose: true},
	{Command: "bootstrap", Usage: "bootstrap", Summary: "Verify the credential and return its current role policy, capabilities, contexts, and revision-bound actions.", Roles: supportedRoles, Expose: true},
	{Command: "status", Usage: "status", Summary: "Read the current project state summary.", Roles: supportedRoles, Expose: true},
	{Command: "next", Usage: "next --role <role> [--actor <actor>]", Summary: "Legacy unauthenticated role projection; bootstrap is authoritative for an actual credential.", Roles: supportedRoles, Values: []string{"role", "actor"}},
	{Command: "doctor", Usage: "doctor", Summary: "Verify project integrity and report Git health.", Roles: supportedRoles, Expose: true},
	{Command: "verify", Usage: "verify", Summary: "Verify project integrity and optional credential anchoring.", Roles: supportedRoles, Expose: true},
	{Command: "recover", Usage: "recover", Summary: "Deterministically finish valid journals or stop on an integrity mismatch.", Roles: supportedRoles, Mutating: true, Expose: true},
	{Command: "explain", Usage: "explain <error-code>", Summary: "Explain a stable CLI error and remediation.", Roles: supportedRoles, Expose: true},

	{Command: "owner apply", Action: "owner.apply", Usage: "owner apply --reason <text>", Summary: "Commit an Owner-authored maintenance change directly to the formal baseline with signed audit evidence.", Roles: []string{"owner"}, Values: []string{"reason"}, Mutating: true, Expose: true},
	{Command: "owner history", Usage: "owner history", Summary: "Read retained Owner baseline-change evidence.", Roles: []string{"owner", "master"}, Expose: true},

	{Command: "template list", Usage: "template list", Summary: "List embedded artifact template kinds.", Roles: []string{"designer"}, Expose: true},
	{Command: "template get", Usage: "template get <kind> [--id <id>] [--output <project-path>]", Summary: "Render the current machine-valid artifact template.", Roles: []string{"designer"}, Values: []string{"id", "output"}, Mutating: true, Expose: true},
	{Command: "artifact check", Usage: "artifact check <path>", Summary: "Validate an artifact without changing state.", Roles: []string{"designer", "master"}, Expose: true},
	{Command: "artifact submit", Action: "artifact.submit", Usage: "artifact submit <path>", Summary: "Submit the exact artifact digest for Master review.", Roles: []string{"designer"}, Mutating: true, Expose: true},
	{Command: "artifact list", Usage: "artifact list [--pending]", Summary: "List artifact state and pending submissions.", Roles: []string{"designer", "master"}, Flags: []string{"pending"}, Expose: true},
	{Command: "artifact context", Usage: "artifact context <submission-id>", Summary: "Read an artifact submission and its exact content.", Roles: []string{"designer", "reviewer", "master"}, Expose: true},
	{Command: "artifact accept", Action: "artifact.accept", Usage: "artifact accept <submission-id>", Summary: "Accept an independently authored artifact digest.", Roles: []string{"master"}, Mutating: true, Expose: true},
	{Command: "artifact reject", Action: "artifact.reject", Usage: "artifact reject <submission-id> --reason <text>", Summary: "Reject an artifact with an actionable reason.", Roles: []string{"master"}, Values: []string{"reason"}, Mutating: true, Expose: true},

	{Command: "mission list", Usage: "mission list", Summary: "List Missions.", Roles: []string{"orchestrator", "master"}, Expose: true},
	{Command: "mission context", Usage: "mission context <mission-id>", Summary: "Read one Mission and its Task states.", Roles: []string{"orchestrator", "master"}, Expose: true},
	{Command: "mission activate", Action: "mission.activate", Usage: "mission activate <mission-id>", Summary: "Activate one accepted Mission after validating its Task graph.", Roles: []string{"orchestrator"}, Mutating: true, Expose: true},
	{Command: "mission block", Action: "mission.block", Usage: "mission block <mission-id> --reason <text>", Summary: "Stop all downstream Mission progress with a reason.", Roles: []string{"orchestrator"}, Values: []string{"reason"}, Mutating: true, Expose: true},
	{Command: "mission resume", Action: "mission.resume", Usage: "mission resume <mission-id>", Summary: "Resume a blocked Mission after revalidation.", Roles: []string{"orchestrator"}, Mutating: true, Expose: true},
	{Command: "mission submit-acceptance", Action: "mission.submit-acceptance", Usage: "mission submit-acceptance <mission-id> --evidence <file-or-text>", Summary: "Submit Mission completion evidence to Master.", Roles: []string{"orchestrator"}, Values: []string{"evidence"}, Mutating: true, Expose: true},
	{Command: "mission accept", Action: "mission.accept", Usage: "mission accept <mission-id>", Summary: "Accept a completed Mission and return the project to idle.", Roles: []string{"master"}, Mutating: true, Expose: true},

	{Command: "task list", Usage: "task list [--ready|--active|--blocked|--review]", Summary: "List Tasks with an optional state filter.", Roles: []string{"orchestrator", "developer", "master"}, Flags: []string{"ready", "active", "blocked", "review"}, Expose: true},
	{Command: "task context", Usage: "task context <task-id>", Summary: "Read a frozen Task contract and state.", Roles: []string{"orchestrator", "developer", "reviewer", "master"}, Expose: true},
	{Command: "task claim", Action: "task.claim", Usage: "task claim <task-id>", Summary: "Claim a ready Task for the same actor when it also has a Developer grant.", Roles: []string{"orchestrator"}, Mutating: true, Expose: true},
	{Command: "task assign", Action: "task.assign", Usage: "task assign <task-id> --owner <developer-actor>", Summary: "Assign a ready Task to an active Developer actor.", Roles: []string{"orchestrator"}, Values: []string{"owner"}, Mutating: true, Expose: true},
	{Command: "task block", Action: "task.block", Usage: "task block <task-id> --reason <text>", Summary: "Block a Task while retaining its frozen contract.", Roles: []string{"orchestrator"}, Values: []string{"reason"}, Mutating: true, Expose: true},
	{Command: "task resume", Action: "task.resume", Usage: "task resume <task-id>", Summary: "Resume a blocked Task only after complete state and Git revalidation.", Roles: []string{"orchestrator"}, Mutating: true, Expose: true},
	{Command: "task release", Action: "task.release", Usage: "task release <task-id>", Summary: "Safely release an unsubmitted clean Task back to ready.", Roles: []string{"orchestrator"}, Mutating: true, Expose: true},
	{Command: "task cancel", Action: "task.cancel", Usage: "task cancel <task-id> --reason <text>", Summary: "Close a Task while retaining its evidence for inspection.", Roles: []string{"master"}, Values: []string{"reason"}, Mutating: true, Expose: true},
	{Command: "task supersede", Action: "task.supersede", Usage: "task supersede <task-id> --replacement <new-task-id>", Summary: "Replace a frozen Task with a separately accepted Task contract.", Roles: []string{"orchestrator"}, Values: []string{"replacement"}, Mutating: true, Expose: true},

	{Command: "work open", Action: "work.open", Usage: "work open <task-id>", Summary: "Create or reopen the Task-bound linked worktree.", Roles: []string{"developer"}, Mutating: true, Expose: true},
	{Command: "work context", Usage: "work context <task-id>", Summary: "Read the complete Developer Task package.", Roles: []string{"developer"}, Expose: true},
	{Command: "work status", Usage: "work status <task-id>", Summary: "Read Task and worktree state.", Roles: []string{"developer"}, Expose: true},
	{Command: "work diff", Usage: "work diff <task-id>", Summary: "Read the current tracked and untracked Task diff.", Roles: []string{"developer"}, Expose: true},
	{Command: "work check", Action: "work.check", Usage: "work check <task-id> (--all|--id <check-id>)", Summary: "Run frozen structured checks plus scope/budget preflight and bind independent evidence to the current snapshot.", Roles: []string{"developer"}, Values: []string{"id"}, Flags: []string{"all"}, Mutating: true, Expose: true},
	{Command: "work checkpoint", Action: "work.checkpoint", Usage: "work checkpoint <task-id> --file <checkpoint-file-or-text>", Summary: "Record a signed progress checkpoint.", Roles: []string{"developer"}, Values: []string{"file"}, Mutating: true, Expose: true},
	{Command: "work submit", Action: "work.submit", Usage: "work submit <task-id> --file <handoff-file-or-text> [--message <summary>]", Summary: "Create an immutable submission after scope, check, snapshot, and budget validation.", Roles: []string{"developer"}, Values: []string{"file", "message"}, Mutating: true, Expose: true},
	{Command: "work block", Action: "work.block", Usage: "work block <task-id> --reason <text>", Summary: "Block owned work when the frozen Task cannot be completed safely.", Roles: []string{"developer"}, Values: []string{"reason"}, Mutating: true, Expose: true},

	{Command: "review list", Usage: "review list", Summary: "List immutable submissions awaiting review.", Roles: []string{"reviewer"}, Expose: true},
	{Command: "review history", Usage: "review history [--task <task-id>] [--submission <submission-id>]", Summary: "Read retained review decisions with role and resource-scope filtering.", Roles: []string{"developer", "orchestrator", "reviewer", "master"}, Values: []string{"task", "submission"}, Expose: true},
	{Command: "review context", Usage: "review context <submission-id>", Summary: "Read the exact submission, Task contract, files, and diff.", Roles: []string{"reviewer"}, Expose: true},
	{Command: "review check", Usage: "review check <submission-id>", Summary: "Mechanically revalidate submission identity, digest, Git range, budget, checks, and scope; this is not a semantic verdict.", Roles: []string{"reviewer"}, Expose: true},
	{Command: "review approve", Action: "review.approve", Usage: "review approve <submission-id> --report <file-or-text>", Summary: "Approve an independently authored exact submission digest.", Roles: []string{"reviewer"}, Values: []string{"report"}, Mutating: true, Expose: true},
	{Command: "review request-changes", Action: "review.request-changes", Usage: "review request-changes <submission-id> --report <file-or-text>", Summary: "Request changes against an exact submission digest.", Roles: []string{"reviewer"}, Values: []string{"report"}, Mutating: true, Expose: true},
	{Command: "integrate check", Usage: "integrate check <submission-id>", Summary: "Verify that an approved submission remains integrable.", Roles: []string{"reviewer"}, Expose: true},
	{Command: "integrate apply", Action: "integrate.apply", Usage: "integrate apply <submission-id>", Summary: "Merge the exact approved head in a candidate worktree, rerun checks, and journal the formal advance.", Roles: []string{"reviewer"}, Mutating: true, Expose: true},

	{Command: "publish check", Usage: "publish check --target <github|gitlab|remote-git> [--remote <name>] [--branch <name>]", Summary: "Preflight publication of the exact local formal baseline.", Roles: []string{"orchestrator", "master"}, Values: []string{"target", "remote", "branch"}, Expose: true},
	{Command: "publish apply", Action: "publish.apply", Usage: "publish apply --target <github|gitlab|remote-git> [--remote <name>] [--branch <name>]", Summary: "Fast-forward publish the exact formal baseline independently from integration.", Roles: []string{"orchestrator", "master"}, Values: []string{"target", "remote", "branch"}, Mutating: true, Expose: true},
}

func commandPolicyFor(command string) (commandPolicy, bool) {
	for _, policy := range commandPolicies {
		if policy.Command == command {
			return policy, true
		}
	}
	return commandPolicy{}, false
}

func roleKnown(role string) bool {
	return containsString(supportedRoles, role)
}

func commandRequiresRoleCredential(policy commandPolicy) bool {
	if !policy.Expose || policy.Action != "" || len(policy.Roles) == 0 {
		return false
	}
	if len(policy.Roles) != len(supportedRoles) {
		return true
	}
	for _, role := range supportedRoles {
		if !containsString(policy.Roles, role) {
			return true
		}
	}
	return false
}

func authorizeRoleReadScope(state State, principal Principal, command string, parsed commandArgs) error {
	resource := ""
	if len(parsed.positionals) == 1 {
		resource = parsed.positionals[0]
	}
	switch command {
	case "mission context":
		if resource != "" && !scopeAllows(principal.Resources.Missions, resource) {
			return &CLIError{Code: "CHS-AUTH-RESOURCE", Message: "credential is not scoped to mission " + resource, ExitCode: 11}
		}
	case "task context":
		if resource != "" && !scopeAllows(principal.Resources.Tasks, resource) {
			return &CLIError{Code: "CHS-AUTH-RESOURCE", Message: "credential is not scoped to task " + resource, ExitCode: 11}
		}
	case "work context", "work status", "work diff":
		if resource != "" && !scopeAllows(principal.Resources.Tasks, resource) {
			return &CLIError{Code: "CHS-AUTH-RESOURCE", Message: "credential is not scoped to task " + resource, ExitCode: 11}
		}
		if task, ok := state.Tasks[resource]; ok && task.Owner != principal.Actor {
			return &CLIError{Code: "CHS-AUTH-RESOURCE", Message: "task is not owned by this Developer", ExitCode: 11}
		}
	case "review context", "review check", "integrate check":
		if resource == "" {
			return nil
		}
		if !bootstrapCandidateAllowed(state, principal, strings.Replace(command, " ", ".", 1), resource) {
			return &CLIError{Code: "CHS-AUTH-RESOURCE", Message: "credential is not scoped to submission " + resource + " and its immutable evidence", ExitCode: 11}
		}
	case "review history":
		if taskID := parsed.values["task"]; taskID != "" && !scopeAllows(principal.Resources.Tasks, taskID) {
			return &CLIError{Code: "CHS-AUTH-RESOURCE", Message: "credential is not scoped to task " + taskID, ExitCode: 11}
		}
		if submissionID := parsed.values["submission"]; submissionID != "" && !scopeAllows(principal.Resources.Submissions, submissionID) {
			return &CLIError{Code: "CHS-AUTH-RESOURCE", Message: "credential is not scoped to submission " + submissionID, ExitCode: 11}
		}
	case "publish check":
		if !scopeAllows(principal.Resources.Baselines, state.Baseline) {
			return &CLIError{Code: "CHS-AUTH-RESOURCE", Message: "credential is not scoped to the current formal baseline", ExitCode: 11}
		}
	}
	return nil
}

func missionVisibleTo(principal Principal, missionID string) bool {
	return scopeAllows(principal.Resources.Missions, missionID)
}

func taskVisibleTo(principal Principal, taskID string) bool {
	return scopeAllows(principal.Resources.Tasks, taskID)
}

func submissionVisibleTo(state State, principal Principal, submissionID string) bool {
	if !scopeAllows(principal.Resources.Submissions, submissionID) {
		return false
	}
	submission, ok := state.Submissions[submissionID]
	return ok && scopeAllows(principal.Resources.SubmissionDigests, submission.Digest)
}

func reviewVisibleTo(state State, principal Principal, review Review) bool {
	submission, ok := state.Submissions[review.SubmissionID]
	if !ok {
		return false
	}
	task, ok := state.Tasks[submission.TaskID]
	if !ok || !taskVisibleTo(principal, task.ID) || !submissionVisibleTo(state, principal, submission.ID) {
		return false
	}
	switch principal.Role {
	case "developer":
		return task.Owner == principal.Actor
	case "reviewer":
		return true
	case "orchestrator":
		return missionVisibleTo(principal, task.MissionID)
	case "master":
		return true
	default:
		return false
	}
}

func actionsForRole(role string) []string {
	actions := []string{}
	for _, policy := range commandPolicies {
		if policy.Action != "" && containsString(policy.Roles, role) {
			actions = append(actions, policy.Action)
		}
	}
	sort.Strings(actions)
	return actions
}

func policyCommand(policy commandPolicy) PolicyCommand {
	return PolicyCommand{
		Command: policy.Command, Action: policy.Action, Usage: policy.Usage, Summary: policy.Summary, Mutating: policy.Mutating,
		ValueOptions: append([]string(nil), policy.Values...), FlagOptions: append([]string(nil), policy.Flags...),
	}
}

func rolePolicyDigest() (string, error) {
	type digestCommand struct {
		PolicyCommand
		Roles  []string `json:"roles"`
		Expose bool     `json:"expose"`
	}
	bundle := struct {
		Version    int             `json:"version"`
		Invariants []string        `json:"invariants"`
		Commands   []digestCommand `json:"commands"`
	}{
		Version: RolePolicyVersion, Invariants: append([]string(nil), commonPolicyInvariants...),
		Commands: make([]digestCommand, 0, len(commandPolicies)),
	}
	for _, policy := range commandPolicies {
		roles := append([]string(nil), policy.Roles...)
		sort.Strings(roles)
		bundle.Commands = append(bundle.Commands, digestCommand{PolicyCommand: policyCommand(policy), Roles: roles, Expose: policy.Expose})
	}
	sort.Slice(bundle.Commands, func(i, j int) bool { return bundle.Commands[i].Command < bundle.Commands[j].Command })
	data, err := canonicalJSON(bundle)
	if err != nil {
		return "", err
	}
	return digestBytes(data), nil
}

func capabilitiesForPrincipal(principal Principal) []PolicyCommand {
	result := []PolicyCommand{}
	for _, policy := range commandPolicies {
		if !policy.Expose || !containsString(policy.Roles, principal.Role) {
			continue
		}
		if policy.Action != "" {
			if _, allowed := principal.Actions[policy.Action]; !allowed {
				continue
			}
		}
		result = append(result, policyCommand(policy))
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Command < result[j].Command })
	return result
}

func bootstrapPrincipalFor(trust Trust, principal Principal) BootstrapPrincipal {
	actions := make([]string, 0, len(principal.Actions))
	for action := range principal.Actions {
		actions = append(actions, action)
	}
	sort.Strings(actions)
	result := BootstrapPrincipal{
		CredentialID: principal.ID, Actor: principal.Actor, Role: principal.Role, Actions: actions,
		Resources: principal.Resources, Persistent: true,
	}
	for _, grant := range trust.Grants {
		if grant.ID == principal.ID {
			result.NotBefore = grant.NotBefore
			result.ExpiresAt = grant.ExpiresAt
			result.Persistent = grant.ExpiresAt == nil
			break
		}
	}
	return result
}

func buildBootstrapResult(root string, trust Trust, state State, principal Principal) (BootstrapResult, error) {
	digest, err := rolePolicyDigest()
	if err != nil {
		return BootstrapResult{}, err
	}
	return BootstrapResult{
		SchemaVersion: BootstrapSchemaVersion, BinaryVersion: Version, ProjectRoot: root,
		StateRevision: state.Revision, TrustRevision: trust.Revision,
		Principal:    bootstrapPrincipalFor(trust, principal),
		Policy:       BootstrapPolicy{Version: RolePolicyVersion, Digest: digest, Role: principal.Role, Invariants: append([]string(nil), commonPolicyInvariants...)},
		Capabilities: capabilitiesForPrincipal(principal), AvailableActions: bootstrapActions(state, trust, principal),
		ContextRequests: bootstrapContextRequests(state, trust, principal),
		RefreshOn:       []string{"state revision conflict", "trust revision change", "credential rotation or revocation", "CLI rejection", "selected resource or task changes"},
	}, nil
}

func nextActionsForPrincipal(state State, principal Principal) []string {
	result := []string{}
	for _, candidate := range nextActions(state, principal.Role, principal.Actor) {
		parts := strings.Fields(candidate)
		if len(parts) == 0 || !bootstrapCandidateAllowed(state, principal, parts[0], candidateResource(parts)) {
			continue
		}
		result = append(result, candidate)
	}
	return result
}

func bootstrapActions(state State, trust Trust, principal Principal) []BootstrapAction {
	result := []BootstrapAction{}
	for _, candidate := range nextActionsForPrincipal(state, principal) {
		parts := strings.Fields(candidate)
		if len(parts) == 0 || (parts[0] == "task.claim" && !bootstrapTaskClaimAllowed(trust, principal, candidateResource(parts))) {
			continue
		}
		action, resource := parts[0], candidateResource(parts)
		command := strings.Replace(action, ".", " ", 1)
		_, ok := commandPolicyFor(command)
		if !ok {
			continue
		}
		argv := strings.Fields(command)
		if len(parts) > 1 && parts[1] != "mission-or-task" {
			argv = append(argv, parts[1:]...)
		}
		item := BootstrapAction{Action: action, Argv: argv, Resource: resource, Reason: bootstrapActionReason(action)}
		switch action {
		case "artifact.submit":
			if resource == "" {
				item.RequiredInputs = []BootstrapInput{{Kind: "positional", Name: "artifact_path", ValueHint: "project-relative path"}}
			}
		case "artifact.reject", "mission.block", "task.block", "task.cancel", "work.block", "owner.apply":
			item.RequiredInputs = []BootstrapInput{{Kind: "option", Name: "reason", ValueHint: "non-empty text"}}
		case "mission.submit-acceptance":
			item.RequiredInputs = []BootstrapInput{{Kind: "option", Name: "evidence", ValueHint: "project file or inline text"}}
		case "task.assign":
			item.RequiredInputs = []BootstrapInput{{Kind: "option", Name: "owner", ValueHint: "authorized Developer actor ID"}}
		case "work.check":
			item.Argv = append(item.Argv, "--all")
		case "work.checkpoint":
			item.Optional = true
			item.RequiredInputs = []BootstrapInput{{Kind: "option", Name: "file", ValueHint: "checkpoint project file or inline text"}}
		case "work.submit":
			item.RequiredInputs = []BootstrapInput{{Kind: "option", Name: "file", ValueHint: "handoff project file or inline text"}}
			item.OptionalInputs = []BootstrapInput{{Kind: "option", Name: "message", ValueHint: "single-line summary"}}
		case "review.approve", "review.request-changes":
			item.RequiredInputs = []BootstrapInput{{Kind: "option", Name: "report", ValueHint: "review report project file or inline text"}}
		}
		result = append(result, item)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Action != result[j].Action {
			return result[i].Action < result[j].Action
		}
		return strings.Join(result[i].Argv, "\x00") < strings.Join(result[j].Argv, "\x00")
	})
	return result
}

func bootstrapTaskClaimAllowed(trust Trust, principal Principal, taskID string) bool {
	if taskID == "" {
		return false
	}
	_, ok := activeDeveloperGrantForTask(trust, principal.Actor, taskID, timeNow())
	return ok
}

func candidateResource(parts []string) string {
	if len(parts) < 2 || parts[1] == "mission-or-task" {
		return ""
	}
	switch {
	case strings.HasPrefix(parts[0], "mission."), strings.HasPrefix(parts[0], "task."), strings.HasPrefix(parts[0], "work."), strings.HasPrefix(parts[0], "review."), strings.HasPrefix(parts[0], "integrate."), strings.HasPrefix(parts[0], "artifact."):
		return parts[1]
	default:
		return ""
	}
}

func bootstrapCandidateAllowed(state State, principal Principal, action, resource string) bool {
	command := strings.Replace(action, ".", " ", 1)
	policy, ok := commandPolicyFor(command)
	if !ok || !containsString(policy.Roles, principal.Role) {
		return false
	}
	if policy.Action != "" {
		if _, allowed := principal.Actions[policy.Action]; !allowed {
			return false
		}
	}
	switch {
	case action == "owner.apply":
		return scopeAllows(principal.Resources.Baselines, state.Baseline)
	case strings.HasPrefix(action, "mission."):
		return resource == "" || scopeAllows(principal.Resources.Missions, resource)
	case strings.HasPrefix(action, "task."), strings.HasPrefix(action, "work."):
		return resource == "" || scopeAllows(principal.Resources.Tasks, resource)
	case strings.HasPrefix(action, "review."), strings.HasPrefix(action, "integrate."):
		if resource == "" || !scopeAllows(principal.Resources.Submissions, resource) {
			return false
		}
		submission, ok := state.Submissions[resource]
		if !ok || !scopeAllows(principal.Resources.SubmissionDigests, submission.Digest) {
			return false
		}
		if strings.HasPrefix(action, "integrate.") && (!scopeAllows(principal.Resources.Heads, submission.HeadCommit) || !scopeAllows(principal.Resources.Baselines, state.Baseline)) {
			return false
		}
	}
	return true
}

func bootstrapActionReason(action string) string {
	reasons := map[string]string{
		"template.get":              "The next design artifact does not exist yet.",
		"artifact.submit":           "An artifact must be submitted or revised before the lifecycle can advance.",
		"artifact.accept":           "An independently authored artifact is awaiting Master acceptance.",
		"artifact.reject":           "An independently authored artifact is awaiting Master review.",
		"mission.activate":          "An accepted planned Mission is ready to execute.",
		"mission.resume":            "The active Mission is blocked and only an explicit resume can reopen progress.",
		"mission.submit-acceptance": "Every Mission Task is closed and completion evidence can be submitted.",
		"mission.accept":            "Mission completion evidence is awaiting Master acceptance.",
		"task.claim":                "A dependency-ready Task can be claimed by an Orchestrator actor that also has a Developer grant.",
		"task.assign":               "A dependency-ready Task can be assigned to an authorized Developer actor.",
		"task.release":              "An unsubmitted active Task can be safely returned to ready if its worktree is clean.",
		"work.open":                 "The owned Task is claimed or has requested changes and needs its bound worktree.",
		"work.check":                "The owned Task is in progress and its frozen checks should be run on current content.",
		"work.checkpoint":           "An optional signed progress checkpoint may be recorded at a meaningful milestone.",
		"work.submit":               "All frozen checks currently pass and the owned Task may be submitted.",
		"review.check":              "An independently authored immutable submission is awaiting machine revalidation.",
		"review.approve":            "An independently authored immutable submission is awaiting a semantic verdict.",
		"review.request-changes":    "An independently authored immutable submission is awaiting a semantic verdict.",
		"integrate.apply":           "The exact approved submission remains pending local integration.",
		"owner.apply":               "The project is quiescent and an Owner may explicitly adopt local maintenance changes into the formal baseline.",
	}
	if reason := reasons[action]; reason != "" {
		return reason
	}
	return "The current signed State makes this a candidate action; execution will revalidate every precondition."
}

func bootstrapContextRequests(state State, trust Trust, principal Principal) []BootstrapContextRequest {
	requests := map[string]BootstrapContextRequest{}
	add := func(kind string, argv []string, resource string) {
		key := kind + "\x00" + resource
		requests[key] = BootstrapContextRequest{Kind: kind, Argv: argv, Resource: resource}
	}
	switch principal.Role {
	case "designer":
		for _, rejection := range designerRejections(state) {
			artifact := state.Artifacts[rejection.ID]
			add("artifact", []string{"artifact", "context", artifact.SubmissionID}, artifact.SubmissionID)
		}
	case "orchestrator":
		if state.ActiveMission != "" && scopeAllows(principal.Resources.Missions, state.ActiveMission) {
			add("mission", []string{"mission", "context", state.ActiveMission}, state.ActiveMission)
		}
	case "developer":
		for _, id := range sortedTaskIDs(state.Tasks) {
			task := state.Tasks[id]
			if task.Owner == principal.Actor && scopeAllows(principal.Resources.Tasks, id) && !isClosedTaskStatus(task.Status) {
				add("task", []string{"work", "context", id}, id)
			}
		}
	case "reviewer":
		for _, action := range bootstrapActions(state, trust, principal) {
			if strings.HasPrefix(action.Action, "review.") || strings.HasPrefix(action.Action, "integrate.") {
				add("submission", []string{"review", "context", action.Resource}, action.Resource)
			}
		}
	case "master":
		for _, id := range sortedArtifactIDs(state.Artifacts) {
			artifact := state.Artifacts[id]
			if artifact.Status == "submitted" {
				add("artifact", []string{"artifact", "context", artifact.SubmissionID}, artifact.SubmissionID)
			}
		}
		if state.ActiveMission != "" {
			add("mission", []string{"mission", "context", state.ActiveMission}, state.ActiveMission)
		}
	}
	keys := make([]string, 0, len(requests))
	for key := range requests {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := make([]BootstrapContextRequest, 0, len(keys))
	for _, key := range keys {
		result = append(result, requests[key])
	}
	return result
}
