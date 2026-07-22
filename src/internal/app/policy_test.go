package app

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
	"time"
)

func TestCommandPolicyRegistryIsTheRoleActionAuthority(t *testing.T) {
	want := map[string][]string{
		"designer":     {"artifact.submit"},
		"orchestrator": {"mission.activate", "mission.block", "mission.resume", "mission.submit-acceptance", "publish.apply", "task.assign", "task.block", "task.claim", "task.release", "task.resume", "task.supersede"},
		"developer":    {"work.block", "work.check", "work.checkpoint", "work.open", "work.submit"},
		"reviewer":     {"integrate.apply", "review.approve", "review.request-changes"},
		"master":       {"artifact.accept", "artifact.reject", "auth.issue", "auth.revoke", "mission.accept", "publish.apply", "task.cancel"},
	}
	seenCommands := map[string]bool{}
	for _, policy := range commandPolicies {
		if policy.Command == "" || seenCommands[policy.Command] {
			t.Fatalf("empty or duplicate command policy: %#v", policy)
		}
		seenCommands[policy.Command] = true
		if policy.Action != "" && (!policy.Mutating || !policy.Expose) {
			t.Fatalf("grantable action is not an exposed mutation: %#v", policy)
		}
		for _, role := range policy.Roles {
			if !roleKnown(role) {
				t.Fatalf("command %s references unknown role %s", policy.Command, role)
			}
		}
	}
	for role, expected := range want {
		actual := actionsForRole(role)
		sort.Strings(expected)
		if !reflect.DeepEqual(actual, expected) {
			t.Fatalf("%s actions = %#v, want %#v", role, actual, expected)
		}
	}
	statusPolicy, _ := commandPolicyFor("status")
	reviewPolicy, _ := commandPolicyFor("review list")
	if commandRequiresRoleCredential(statusPolicy) || !commandRequiresRoleCredential(reviewPolicy) {
		t.Fatal("role-scoped read credential policy is inconsistent")
	}
}

func TestRolePolicyDigestGolden(t *testing.T) {
	digest, err := rolePolicyDigest()
	if err != nil {
		t.Fatal(err)
	}
	const want = "sha256:4ed4b6c24933193f9fe2c91db9f6cdd54053f011562c243bbbee7c4e911af28b"
	if digest != want {
		t.Fatalf("Role Policy V%d changed: got %s, want %s; bump RolePolicyVersion for intentional semantic changes", RolePolicyVersion, digest, want)
	}
}

func TestBootstrapUsesCredentialCapabilitiesAndResourceScope(t *testing.T) {
	now := time.Unix(100, 0).UTC()
	state := State{
		Revision: 9, ActiveMission: "M001",
		Missions: map[string]MissionState{"M001": {ID: "M001", Status: "active", TaskIDs: []string{"M001-T001"}}},
		Tasks: map[string]TaskState{"M001-T001": {
			ID: "M001-T001", MissionID: "M001", Status: "claimed", Owner: "agent:developer",
		}},
		Artifacts: map[string]ArtifactState{}, Submissions: map[string]Submission{},
	}
	principal := Principal{
		ID: "CRED-1", Actor: "agent:developer", Role: "developer",
		Actions: stringSet([]string{"work.open"}), Resources: ResourceScope{Tasks: []string{"M001-T001"}},
	}
	trust := Trust{Revision: 4, Grants: []Grant{{ID: "CRED-1", Actor: principal.Actor, Role: principal.Role, ExpiresAt: nil, IssuedAt: now}}}
	result, err := buildBootstrapResult("/project", trust, state, principal)
	if err != nil {
		t.Fatal(err)
	}
	if result.SchemaVersion != BootstrapSchemaVersion || result.StateRevision != 9 || result.TrustRevision != 4 || result.Principal.Role != "developer" || result.Principal.Actor != principal.Actor {
		t.Fatalf("bootstrap identity/envelope = %#v", result)
	}
	if !hasPolicyCommand(result.Capabilities, "work open") || hasPolicyCommand(result.Capabilities, "work submit") {
		t.Fatalf("credential-filtered capabilities = %#v", result.Capabilities)
	}
	if len(result.AvailableActions) != 1 || result.AvailableActions[0].Action != "work.open" || !reflect.DeepEqual(result.AvailableActions[0].Argv, []string{"work", "open", "M001-T001"}) {
		t.Fatalf("available actions = %#v", result.AvailableActions)
	}
	if len(result.ContextRequests) != 1 || result.ContextRequests[0].Resource != "M001-T001" {
		t.Fatalf("context requests = %#v", result.ContextRequests)
	}

	principal.Resources.Tasks = []string{"M001-T999"}
	result, err = buildBootstrapResult("/project", trust, state, principal)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.AvailableActions) != 0 || len(result.ContextRequests) != 0 {
		t.Fatalf("out-of-scope bootstrap leaked Task actions: %#v", result)
	}
}

func TestBootstrapReviewerActionsRespectSubmissionScopes(t *testing.T) {
	state := State{
		Revision: 5,
		Tasks:    map[string]TaskState{"M001-T001": {ID: "M001-T001", Status: "review_pending"}},
		Submissions: map[string]Submission{"SUB-1": {
			ID: "SUB-1", TaskID: "M001-T001", Actor: "agent:developer", Status: "review_pending", Digest: "sha256:digest", HeadCommit: "head", BaseCommit: "base",
		}},
		Artifacts: map[string]ArtifactState{}, Missions: map[string]MissionState{},
	}
	principal := Principal{
		ID: "CRED-R", Actor: "agent:reviewer", Role: "reviewer", Actions: stringSet([]string{"review.approve"}),
		Resources: ResourceScope{Submissions: []string{"SUB-1"}, SubmissionDigests: []string{"sha256:digest"}},
	}
	actions := bootstrapActions(state, Trust{}, principal)
	if len(actions) != 2 || actions[0].Action != "review.approve" || actions[1].Action != "review.check" || len(actions[0].RequiredInputs) != 1 || actions[0].RequiredInputs[0].Name != "report" {
		t.Fatalf("reviewer scoped actions = %#v", actions)
	}
	principal.Resources.SubmissionDigests = []string{"sha256:other"}
	if actions := bootstrapActions(state, Trust{}, principal); len(actions) != 0 {
		t.Fatalf("digest-scoped reviewer received actions: %#v", actions)
	}
}

func TestBootstrapOffersTaskClaimOnlyWithDeveloperGrant(t *testing.T) {
	state := State{
		ActiveMission: "M001",
		Missions:      map[string]MissionState{"M001": {ID: "M001", Status: "active", TaskIDs: []string{"M001-T001"}}},
		Tasks:         map[string]TaskState{"M001-T001": {ID: "M001-T001", MissionID: "M001", Status: "ready"}},
		Artifacts:     map[string]ArtifactState{}, Submissions: map[string]Submission{},
	}
	principal := Principal{
		ID: "CRED-O", Actor: "agent:dual-role", Role: "orchestrator",
		Actions: stringSet([]string{"task.claim", "task.assign"}),
	}
	actions := bootstrapActions(state, Trust{}, principal)
	if hasBootstrapAction(actions, "task.claim") || !hasBootstrapAction(actions, "task.assign") {
		t.Fatalf("claim without Developer grant = %#v", actions)
	}
	trust := Trust{Grants: []Grant{{
		ID: "CRED-D", Actor: principal.Actor, Role: "developer", Actions: []string{"work.open"}, IssuedAt: timeNow().Add(-time.Minute),
	}}}
	actions = bootstrapActions(state, trust, principal)
	if !hasBootstrapAction(actions, "task.claim") || !hasBootstrapAction(actions, "task.assign") {
		t.Fatalf("claim with Developer grant = %#v", actions)
	}
}

func TestRoleReadCommandsEnforceResourceScopes(t *testing.T) {
	state := State{
		Baseline: "formal-head",
		Submissions: map[string]Submission{
			"SUB-1": {ID: "SUB-1", Digest: "sha256:one", HeadCommit: "head-1"},
			"SUB-2": {ID: "SUB-2", Digest: "sha256:two", HeadCommit: "head-2"},
		},
	}
	developer := Principal{Role: "developer", Resources: ResourceScope{Tasks: []string{"M001-T001"}}}
	if err := authorizeRoleReadScope(state, developer, "work context", commandArgs{positionals: []string{"M001-T001"}}); err != nil {
		t.Fatalf("in-scope Task context was rejected: %v", err)
	}
	if err := authorizeRoleReadScope(state, developer, "work context", commandArgs{positionals: []string{"M001-T002"}}); err == nil {
		t.Fatal("out-of-scope Task context was allowed")
	}
	reviewer := Principal{
		Role: "reviewer", Resources: ResourceScope{
			Submissions: []string{"SUB-1"}, SubmissionDigests: []string{"sha256:one"}, Heads: []string{"head-1"}, Baselines: []string{"formal-head"},
		},
	}
	if err := authorizeRoleReadScope(state, reviewer, "review context", commandArgs{positionals: []string{"SUB-1"}}); err != nil {
		t.Fatalf("in-scope submission context was rejected: %v", err)
	}
	if err := authorizeRoleReadScope(state, reviewer, "review context", commandArgs{positionals: []string{"SUB-2"}}); err == nil {
		t.Fatal("out-of-scope submission context was allowed")
	}
	if err := authorizeRoleReadScope(state, reviewer, "integrate check", commandArgs{positionals: []string{"SUB-1"}}); err != nil {
		t.Fatalf("in-scope integration check was rejected: %v", err)
	}
	reviewer.Resources.Baselines = []string{"different"}
	if err := authorizeRoleReadScope(state, reviewer, "integrate check", commandArgs{positionals: []string{"SUB-1"}}); err == nil {
		t.Fatal("integration check ignored baseline scope")
	}
}

func TestBootstrapCLIRequiresAndDerivesCredential(t *testing.T) {
	project, rootPath := setupAuthProject(t)
	credentialPath := filepath.Join(t.TempDir(), "developer.yaml")
	if _, err := issueCredential(project, rootPath, "agent:developer", "developer", credentialPath, []string{"work.open"}); err != nil {
		t.Fatal(err)
	}
	response := runProjectCLI(t, project, credentialPath, "bootstrap")
	data, err := json.Marshal(response.Result)
	if err != nil {
		t.Fatal(err)
	}
	var result BootstrapResult
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}
	if result.Principal.Role != "developer" || result.Principal.Actor != "agent:developer" || len(result.Principal.Actions) != 1 || result.Principal.Actions[0] != "work.open" {
		t.Fatalf("bootstrap principal = %#v", result.Principal)
	}
	var stdout, stderr bytes.Buffer
	exitCode := Run([]string{"--json", "--root", project, "--credential", credentialPath, "review", "list"}, &stdout, &stderr)
	if exitCode != 11 {
		t.Fatalf("Developer credential accessed Reviewer read command: exit=%d stdout=%s stderr=%s", exitCode, stdout.String(), stderr.String())
	}
	var failure Response
	if err := json.Unmarshal(stderr.Bytes(), &failure); err != nil || failure.Error == nil || failure.Error.Code != "CHS-AUTH-DENIED" {
		t.Fatalf("role-scoped read denial = %#v, %v", failure, err)
	}

	stdout.Reset()
	stderr.Reset()
	exitCode = Run([]string{"--json", "--root", project, "bootstrap"}, &stdout, &stderr)
	if exitCode != 11 {
		t.Fatalf("bootstrap without credential exited %d: %s %s", exitCode, stdout.String(), stderr.String())
	}
	failure = Response{}
	if err := json.Unmarshal(stderr.Bytes(), &failure); err != nil || failure.Error == nil || failure.Error.Code != "CHS-AUTH-MISSING" {
		t.Fatalf("bootstrap missing-credential response = %#v, %v", failure, err)
	}

	stdout.Reset()
	stderr.Reset()
	exitCode = Run([]string{"--json", "--root", project, "--credential", credentialPath, "bootstrap", "--role", "reviewer"}, &stdout, &stderr)
	if exitCode != 20 {
		t.Fatalf("bootstrap accepted a self-declared role: exit=%d stdout=%s stderr=%s", exitCode, stdout.String(), stderr.String())
	}

	var credential Credential
	if err := loadYAML(credentialPath, &credential); err != nil {
		t.Fatal(err)
	}
	if err := revokeCredential(project, rootPath, credential.ID, "bootstrap revocation test"); err != nil {
		t.Fatal(err)
	}
	stdout.Reset()
	stderr.Reset()
	exitCode = Run([]string{"--json", "--root", project, "--credential", credentialPath, "bootstrap"}, &stdout, &stderr)
	if exitCode != 11 {
		t.Fatalf("revoked credential bootstrap exited %d: %s %s", exitCode, stdout.String(), stderr.String())
	}
	failure = Response{}
	if err := json.Unmarshal(stderr.Bytes(), &failure); err != nil || failure.Error == nil || failure.Error.Code != "CHS-AUTH-REVOKED" {
		t.Fatalf("revoked bootstrap response = %#v, %v", failure, err)
	}
}

func hasPolicyCommand(commands []PolicyCommand, command string) bool {
	for _, item := range commands {
		if item.Command == command {
			return true
		}
	}
	return false
}
