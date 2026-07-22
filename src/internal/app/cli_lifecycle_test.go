package app

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestCLIGreenfieldEndToEnd(t *testing.T) {
	runCLIFourRoleLifecycle(t, false)
}

func TestCLIBrownfieldEndToEnd(t *testing.T) {
	runCLIFourRoleLifecycle(t, true)
}

func runCLIFourRoleLifecycle(t *testing.T, existing bool) {
	t.Helper()
	testRoot := t.TempDir()
	rootKey := filepath.Join(testRoot, "master-root.yaml")
	runCLIJSON(t, "--json", "auth", "master-init", "--output", rootKey)

	project := filepath.Join(testRoot, "project")
	initArguments := []string{
		"--json", "--credential", rootKey,
		"project", "init", project,
		"--max-changed-files", "10", "--max-diff-lines", "100", "--max-commits", "5",
	}
	if existing {
		if err := os.MkdirAll(project, 0o755); err != nil {
			t.Fatal(err)
		}
		if _, err := git(project, "init", "-b", "trunk"); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(project, "legacy.txt"), []byte("legacy\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := gitCommit(project, "Legacy baseline", "legacy.txt"); err != nil {
			t.Fatal(err)
		}
		initArguments = append(initArguments, "--existing")
	}
	runCLIJSON(t, initArguments...)

	credentials := map[string]string{"master": rootKey}
	for _, role := range []string{"designer", "orchestrator", "developer", "reviewer"} {
		path := filepath.Join(testRoot, role+".yaml")
		runCLIJSON(t,
			"--json", "--root", project, "--credential", rootKey,
			"auth", "issue", "--actor", "agent:"+role, "--role", role, "--output", path,
		)
		credentials[role] = path
	}
	designerBootstrap := runBootstrapCLI(t, project, credentials["designer"])
	if designerBootstrap.Principal.Role != "designer" || !hasBootstrapAction(designerBootstrap.AvailableActions, "template.get") {
		t.Fatalf("initial Designer bootstrap = %#v", designerBootstrap)
	}

	requirements := `---
kind: requirements
id: requirements
---
# Requirements
## Problem
Need one file.
## Required Behavior
- REQ-001: create code.txt.
## Success Criteria
- SC-001: check passes.
## Scope
- code.txt
## Constraints
- local only
## Decisions Required from Master
- None
`
	writeTestArtifact(t, project, "docs/requirements.md", requirements)
	cliSubmitAndAcceptArtifact(t, project, "docs/requirements.md", credentials)
	state := mustProjectState(t, project)
	requirementsDigest := state.Artifacts["requirements"].Digest

	architecture := fmt.Sprintf(`---
kind: architecture
id: architecture
requirements_digest: %s
---
# Architecture
## System Context
Local file.
## Components and Boundaries
- code.txt
## Interfaces
- filesystem
## Data and State
- Git
## Security
- signed events
## Validation Strategy
- true
## Parallelization Boundaries
- one file
## Decisions Required from Master
- None
`, requirementsDigest)
	writeTestArtifact(t, project, "docs/architecture.md", architecture)
	cliSubmitAndAcceptArtifact(t, project, "docs/architecture.md", credentials)
	state = mustProjectState(t, project)
	architectureDigest := state.Artifacts["architecture"].Digest

	mission := fmt.Sprintf(`---
kind: mission
id: M001
requirements_digest: %s
architecture_digest: %s
task_ids:
  - M001-T001
---
# Mission M001
## Outcome
Create code.txt.
## Requirements Covered
- REQ-001
## Acceptance Criteria
- Task integrated.
## Constraints and Risks
- None
## Completion Evidence
- Pending
`, requirementsDigest, architectureDigest)
	writeTestArtifact(t, project, "docs/missions/M001.md", mission)
	cliSubmitAndAcceptArtifact(t, project, "docs/missions/M001.md", credentials)

	taskDocument := fmt.Sprintf(`---
kind: task
id: M001-T001
mission_id: M001
requirements_digest: %s
architecture_digest: %s
depends_on: []
allowed_paths:
  - code.txt
acceptance_checks:
  - id: CHECK-001
    argv: ["true"]
    cwd: "."
    env: {}
    timeout_seconds: 10
---
# Task M001-T001
## Objective
Create code.txt.
## Inputs and Assumptions
- Repository baseline exists.
## Forbidden and Out of Scope
- Other files.
## Deliverables
- code.txt
## Stop Conditions
- Scope change.
## Reviewer Attention
- Exact submitted commit.
`, requirementsDigest, architectureDigest)
	writeTestArtifact(t, project, "docs/tasks/M001-T001.md", taskDocument)
	cliSubmitAndAcceptArtifact(t, project, "docs/tasks/M001-T001.md", credentials)

	state = mustProjectState(t, project)
	wantBudget := TaskBudget{MaxChangedFiles: 10, MaxDiffLines: 100, MaxCommits: 5}
	if state.Tasks["M001-T001"].Budget != wantBudget {
		t.Fatalf("frozen task budget = %#v, want %#v", state.Tasks["M001-T001"].Budget, wantBudget)
	}
	orchestratorBootstrap := runBootstrapCLI(t, project, credentials["orchestrator"])
	if orchestratorBootstrap.Principal.Role != "orchestrator" || !hasBootstrapAction(orchestratorBootstrap.AvailableActions, "mission.activate") {
		t.Fatalf("planned Orchestrator bootstrap = %#v", orchestratorBootstrap)
	}
	runProjectCLI(t, project, credentials["orchestrator"], "mission", "activate", "M001")
	runProjectCLI(t, project, credentials["orchestrator"], "task", "assign", "M001-T001", "--owner", "agent:developer")
	developerBootstrap := runBootstrapCLI(t, project, credentials["developer"])
	if developerBootstrap.Principal.Role != "developer" || !hasBootstrapAction(developerBootstrap.AvailableActions, "work.open") {
		t.Fatalf("claimed Developer bootstrap = %#v", developerBootstrap)
	}
	runProjectCLI(t, project, credentials["developer"], "work", "open", "M001-T001")

	state = mustProjectState(t, project)
	worktreeRoot, err := taskWorktreeRoot(project, state.Tasks["M001-T001"])
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(worktreeRoot, "code.txt"), []byte("complete\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runProjectCLI(t, project, credentials["developer"], "work", "check", "M001-T001", "--all")
	runProjectCLI(t, project, credentials["developer"], "work", "submit", "M001-T001", "--file", "ready", "--message", "CLI completion")

	state = mustProjectState(t, project)
	submission := state.Submissions[state.Tasks["M001-T001"].SubmissionID]
	if submission.CommitMessage != "M001-T001: CLI completion" || submission.Metrics == nil {
		t.Fatalf("CLI submission evidence = %#v", submission)
	}
	reviewerBootstrap := runBootstrapCLI(t, project, credentials["reviewer"])
	if reviewerBootstrap.Principal.Role != "reviewer" || !hasBootstrapAction(reviewerBootstrap.AvailableActions, "review.check") || !hasBootstrapAction(reviewerBootstrap.AvailableActions, "review.approve") {
		t.Fatalf("pending Reviewer bootstrap = %#v", reviewerBootstrap)
	}
	runProjectCLI(t, project, credentials["reviewer"], "review", "approve", submission.ID, "--report", "approved")
	runProjectCLI(t, project, credentials["reviewer"], "integrate", "apply", submission.ID)
	runProjectCLI(t, project, credentials["orchestrator"], "mission", "submit-acceptance", "M001", "--evidence", "integration verified")
	masterBootstrap := runBootstrapCLI(t, project, credentials["master"])
	if masterBootstrap.Principal.Role != "master" || !hasBootstrapAction(masterBootstrap.AvailableActions, "mission.accept") {
		t.Fatalf("acceptance-pending Master bootstrap = %#v", masterBootstrap)
	}
	runProjectCLI(t, project, credentials["master"], "mission", "accept", "M001")
	runProjectCLI(t, project, credentials["master"], "verify")

	config, _, state, err := loadProject(project)
	if err != nil {
		t.Fatal(err)
	}
	wantMode := "greenfield"
	if existing {
		wantMode = "brownfield"
	}
	if config.Mode != wantMode || state.Phase != "idle" || state.Missions["M001"].Status != "completed" || state.Tasks["M001-T001"].Status != "integrated" {
		t.Fatalf("final CLI lifecycle: config=%#v state=%#v", config, state)
	}
}

func cliSubmitAndAcceptArtifact(t *testing.T, project, path string, credentials map[string]string) {
	t.Helper()
	runProjectCLI(t, project, credentials["designer"], "artifact", "submit", path)
	state := mustProjectState(t, project)
	var submissionID string
	for _, artifact := range state.Artifacts {
		if artifact.Path == path {
			submissionID = artifact.SubmissionID
			break
		}
	}
	if submissionID == "" {
		t.Fatalf("artifact %s was not submitted", path)
	}
	masterBootstrap := runBootstrapCLI(t, project, credentials["master"])
	if !hasBootstrapAction(masterBootstrap.AvailableActions, "artifact.accept") || len(masterBootstrap.ContextRequests) == 0 {
		t.Fatalf("pending artifact Master bootstrap = %#v", masterBootstrap)
	}
	runProjectCLI(t, project, credentials["master"], "artifact", "accept", submissionID)
}

func runProjectCLI(t *testing.T, project, credential string, arguments ...string) Response {
	t.Helper()
	global := []string{"--json", "--root", project, "--credential", credential}
	return runCLIJSON(t, append(global, arguments...)...)
}

func runBootstrapCLI(t *testing.T, project, credential string) BootstrapResult {
	t.Helper()
	response := runProjectCLI(t, project, credential, "bootstrap")
	data, err := json.Marshal(response.Result)
	if err != nil {
		t.Fatal(err)
	}
	var result BootstrapResult
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}
	return result
}

func hasBootstrapAction(actions []BootstrapAction, action string) bool {
	for _, candidate := range actions {
		if candidate.Action == action {
			return true
		}
	}
	return false
}

func runCLIJSON(t *testing.T, arguments ...string) Response {
	t.Helper()
	var stdout, stderr bytes.Buffer
	exitCode := Run(arguments, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("chassiss %v exited %d: stdout=%s stderr=%s", arguments, exitCode, stdout.String(), stderr.String())
	}
	var response Response
	if err := json.Unmarshal(stdout.Bytes(), &response); err != nil {
		t.Fatalf("decode chassiss %v response: %v; stdout=%s", arguments, err, stdout.String())
	}
	if !response.OK || response.Command == "" {
		t.Fatalf("invalid chassiss %v response: %#v", arguments, response)
	}
	return response
}
