package app

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEventV4CompleteFourRoleLifecycle(t *testing.T) {
	testRoot := t.TempDir()
	rootPath := filepath.Join(testRoot, "master-root.yaml")
	if _, err := createRoot(rootPath); err != nil {
		t.Fatal(err)
	}
	project := filepath.Join(testRoot, "project")
	if _, _, err := initializeProject(project, rootPath, false); err != nil {
		t.Fatal(err)
	}

	masterKey, masterPublic, masterPrivate, err := loadRoot(rootPath)
	if err != nil {
		t.Fatal(err)
	}
	master := rootPrincipal(masterKey, masterPublic, masterPrivate)
	designer := issueTestPrincipal(t, project, rootPath, testRoot, "agent:designer", "designer")
	orchestrator := issueTestPrincipal(t, project, rootPath, testRoot, "agent:orchestrator", "orchestrator")
	developer := issueTestPrincipal(t, project, rootPath, testRoot, "agent:developer", "developer")
	reviewer := issueTestPrincipal(t, project, rootPath, testRoot, "agent:reviewer", "reviewer")

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
	requirementsState := submitAndAcceptTestArtifact(t, project, "docs/requirements.md", designer, master)

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
`, requirementsState.Digest)
	writeTestArtifact(t, project, "docs/architecture.md", architecture)
	architectureState := submitAndAcceptTestArtifact(t, project, "docs/architecture.md", designer, master)

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
`, requirementsState.Digest, architectureState.Digest)
	writeTestArtifact(t, project, "docs/missions/M001.md", mission)
	submitAndAcceptTestArtifact(t, project, "docs/missions/M001.md", designer, master)

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
    verification_paths:
      - docs/tasks/M001-T001.md
---
# Task M001-T001
## Objective
Create code.txt.
## Inputs and Assumptions
- Empty repository.
## Forbidden and Out of Scope
- Other files.
## Deliverables
- code.txt
## Stop Conditions
- Scope change.
## Reviewer Attention
- Exact submitted commit.
`, requirementsState.Digest, architectureState.Digest)
	writeTestArtifact(t, project, "docs/tasks/M001-T001.md", taskDocument)
	submitAndAcceptTestArtifact(t, project, "docs/tasks/M001-T001.md", designer, master)

	state := mustProjectState(t, project)
	if _, _, err := activateMission(project, "M001", orchestrator, state.Revision); err != nil {
		t.Fatal(err)
	}
	state = mustProjectState(t, project)
	if _, _, _, err := taskClaimOrAssign(project, "M001-T001", developer.Actor, orchestrator, state.Revision, true); err != nil {
		t.Fatal(err)
	}
	state = mustProjectState(t, project)
	if _, _, _, err := workOpen(project, "M001-T001", developer, state.Revision); err != nil {
		t.Fatal(err)
	}
	state = mustProjectState(t, project)
	worktreeRoot, err := taskWorktreeRoot(project, state.Tasks["M001-T001"])
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(worktreeRoot, "code.txt"), []byte("complete\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(worktreeRoot, "package.json"), []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	beforeRejectedCheck := state.Revision
	if _, _, _, err := runTaskCheck(project, "M001-T001", "", true, developer, state.Revision); err == nil {
		t.Fatal("work check accepted an out-of-scope snapshot")
	} else if typed, ok := err.(*CLIError); !ok || typed.Code != "CHS-WORK-SCOPE" {
		t.Fatalf("out-of-scope check error = %#v, want CHS-WORK-SCOPE", err)
	}
	if after := mustProjectState(t, project).Revision; after != beforeRejectedCheck {
		t.Fatalf("out-of-scope check advanced revision from %d to %d", beforeRejectedCheck, after)
	}
	if err := os.Remove(filepath.Join(worktreeRoot, "package.json")); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := runTaskCheck(project, "M001-T001", "", true, developer, state.Revision); err != nil {
		t.Fatal(err)
	}
	state = mustProjectState(t, project)
	_, _, submitted, err := workSubmit(project, "M001-T001", "ready", "", developer, state.Revision)
	if err != nil {
		t.Fatal(err)
	}
	state = mustProjectState(t, project)
	submitted = state.Submissions[submitted.ID]
	if submitted.CommitMessage != "M001-T001: ready" || submitted.Metrics == nil || submitted.Metrics.ChangedFiles != 1 || submitted.Metrics.DiffLines != 1 || submitted.Metrics.Commits != 1 {
		t.Fatalf("submission budget/message evidence = %#v", submitted)
	}
	if _, _, _, err := recordReview(project, submitted.ID, "request_changes", "add the final detail", reviewer, state.Revision); err != nil {
		t.Fatal(err)
	}
	state = mustProjectState(t, project)
	current, history := taskChangeRequests(state, "M001-T001")
	if current == nil || current.SubmissionID != submitted.ID || current.Report != "add the final detail" || len(history) != 1 {
		t.Fatalf("change request projection = current %#v history %#v", current, history)
	}
	contextCredential := filepath.Join(testRoot, "developer-context.yaml")
	if _, err := issueCredential(project, rootPath, developer.Actor, "developer", contextCredential, nil); err != nil {
		t.Fatal(err)
	}
	contextResponse, err := dispatch(globalOptions{root: project, credential: contextCredential}, []string{"work", "context", "M001-T001"})
	if err != nil {
		t.Fatal(err)
	}
	contextJSON, _ := json.Marshal(contextResponse.Result)
	if !strings.Contains(string(contextJSON), `"change_request"`) || !strings.Contains(string(contextJSON), `"change_request_history"`) || !strings.Contains(string(contextJSON), "add the final detail") {
		t.Fatalf("Developer work context omitted review feedback: %s", contextJSON)
	}
	if _, _, _, err := workOpen(project, "M001-T001", developer, state.Revision); err != nil {
		t.Fatal(err)
	}
	state = mustProjectState(t, project)
	if current, history := taskChangeRequests(state, "M001-T001"); current == nil || current.Report != "add the final detail" || len(history) != 1 {
		t.Fatalf("opening returned work lost its current change request: current %#v history %#v", current, history)
	}
	if err := os.WriteFile(filepath.Join(worktreeRoot, "code.txt"), []byte("complete\nfinal detail\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := runTaskCheck(project, "M001-T001", "", true, developer, state.Revision); err != nil {
		t.Fatal(err)
	}
	state = mustProjectState(t, project)
	_, _, revised, err := workSubmit(project, "M001-T001", "revised", "", developer, state.Revision)
	if err != nil {
		t.Fatal(err)
	}
	state = mustProjectState(t, project)
	current, history = taskChangeRequests(state, "M001-T001")
	if current != nil || len(history) != 1 || history[0].SubmissionID != submitted.ID {
		t.Fatalf("revised change request projection = current %#v history %#v", current, history)
	}
	if _, _, _, err := recordReview(project, revised.ID, "approve", "approved", reviewer, state.Revision); err != nil {
		t.Fatal(err)
	}
	state = mustProjectState(t, project)
	if _, _, _, err := integrateSubmission(project, revised.ID, reviewer, state.Revision); err != nil {
		t.Fatal(err)
	}
	state = mustProjectState(t, project)
	if _, _, _, err := submitMissionAcceptance(project, "M001", "integration verified", orchestrator, state.Revision); err != nil {
		t.Fatal(err)
	}
	state = mustProjectState(t, project)
	if _, _, _, err := acceptMission(project, "M001", master, state.Revision); err != nil {
		t.Fatal(err)
	}
	state = mustProjectState(t, project)
	if state.Phase != "idle" || state.Missions["M001"].Status != "completed" || state.Tasks["M001-T001"].Status != "integrated" {
		t.Fatalf("final state = %#v", state)
	}
	if _, err := verifyProject(project); err != nil {
		t.Fatalf("completed V4-state project did not verify: %v", err)
	}
}

func issueTestPrincipal(t *testing.T, project, rootPath, outputDir, actor, role string) Principal {
	t.Helper()
	suffix, err := newID("test")
	if err != nil {
		t.Fatal(err)
	}
	fileName := strings.NewReplacer(":", "-", "/", "-", "\\", "-").Replace(actor) + "-" + role + "-" + suffix + ".yaml"
	path := filepath.Join(outputDir, fileName)
	if _, err := issueCredential(project, rootPath, actor, role, path, nil); err != nil {
		t.Fatal(err)
	}
	principal, err := loadPrincipal(project, path, "")
	if err != nil {
		t.Fatal(err)
	}
	return principal
}

func writeTestArtifact(t *testing.T, project, relative, content string) {
	t.Helper()
	path := filepath.Join(project, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func submitAndAcceptTestArtifact(t *testing.T, project, path string, designer, master Principal) ArtifactState {
	t.Helper()
	state := mustProjectState(t, project)
	_, _, artifact, err := submitArtifact(project, path, designer, state.Revision)
	if err != nil {
		t.Fatal(err)
	}
	state = mustProjectState(t, project)
	_, _, _, err = acceptArtifact(project, artifact.SubmissionID, master, state.Revision)
	if err != nil {
		t.Fatal(err)
	}
	return mustProjectState(t, project).Artifacts[artifact.ID]
}

func mustProjectState(t *testing.T, project string) State {
	t.Helper()
	_, _, state, err := loadProject(project)
	if err != nil {
		t.Fatal(err)
	}
	return state
}
