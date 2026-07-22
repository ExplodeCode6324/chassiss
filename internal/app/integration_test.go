package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type approvedSubmissionFixture struct {
	Project      string
	Master       Principal
	Orchestrator Principal
	Reviewer     Principal
	Submission   Submission
}

func TestIntegrationRejectsApprovedBranchThatMoved(t *testing.T) {
	fixture := setupApprovedSubmission(t, []string{"true"})
	formalBefore, err := git(fixture.Project, "rev-parse", "refs/heads/main")
	if err != nil {
		t.Fatal(err)
	}
	state := mustProjectState(t, fixture.Project)
	worktreeRoot, err := taskWorktreeRoot(fixture.Project, state.Tasks[fixture.Submission.TaskID])
	if err != nil {
		t.Fatal(err)
	}
	if _, err := git(worktreeRoot, "-c", "user.name=developer", "-c", "user.email=developer@invalid", "commit", "--allow-empty", "-m", "unreviewed"); err != nil {
		t.Fatal(err)
	}
	state = mustProjectState(t, fixture.Project)
	_, _, _, err = integrateSubmission(fixture.Project, fixture.Submission.ID, fixture.Reviewer, state.Revision)
	if err == nil {
		t.Fatal("integration accepted a task branch that moved after approval")
	}
	typed, ok := err.(*CLIError)
	if !ok || typed.Code != "CHS-INTEGRATION-HEAD-MOVED" {
		t.Fatalf("error = %#v, want CHS-INTEGRATION-HEAD-MOVED", err)
	}
	formalAfter, _ := git(fixture.Project, "rev-parse", "refs/heads/main")
	if formalAfter != formalBefore {
		t.Fatalf("formal branch moved from %s to %s after rejected integration", formalBefore, formalAfter)
	}
	state = mustProjectState(t, fixture.Project)
	if state.Submissions[fixture.Submission.ID].Status != "approved" {
		t.Fatalf("rejected integration changed submission state: %#v", state.Submissions[fixture.Submission.ID])
	}
}

func TestIntegrationRunsChecksOnMergedCandidate(t *testing.T) {
	fixture := setupApprovedSubmission(t, []string{"git", "symbolic-ref", "--quiet", "HEAD"})
	formalBefore, _ := git(fixture.Project, "rev-parse", "refs/heads/main")
	state := mustProjectState(t, fixture.Project)
	_, _, _, err := integrateSubmission(fixture.Project, fixture.Submission.ID, fixture.Reviewer, state.Revision)
	if err == nil {
		t.Fatal("integration recorded success despite a merged-candidate check failure")
	}
	typed, ok := err.(*CLIError)
	if !ok || typed.Code != "CHS-INTEGRATION-CHECKS" {
		t.Fatalf("error = %#v, want CHS-INTEGRATION-CHECKS", err)
	}
	formalAfter, _ := git(fixture.Project, "rev-parse", "refs/heads/main")
	if formalAfter != formalBefore {
		t.Fatalf("failed candidate check moved formal branch from %s to %s", formalBefore, formalAfter)
	}
	state = mustProjectState(t, fixture.Project)
	if state.Submissions[fixture.Submission.ID].Status != "approved" || len(state.Integrations) != 0 {
		t.Fatalf("failed candidate check recorded integration: %#v", state)
	}
}

func TestIntegrationMergeParentAndEvidenceUseApprovedHead(t *testing.T) {
	fixture := setupApprovedSubmission(t, []string{"true"})
	state := mustProjectState(t, fixture.Project)
	_, next, integration, err := integrateSubmission(fixture.Project, fixture.Submission.ID, fixture.Reviewer, state.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if integration.SubmissionHead != fixture.Submission.HeadCommit {
		t.Fatalf("integrated submission head = %s, want %s", integration.SubmissionHead, fixture.Submission.HeadCommit)
	}
	parents, err := git(fixture.Project, "rev-list", "--parents", "-n", "1", integration.IntegratedHead)
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Fields(parents)
	if len(parts) != 3 || parts[1] != integration.PreviousHead || parts[2] != fixture.Submission.HeadCommit {
		t.Fatalf("merge parents = %#v, want previous and approved submission head", parts)
	}
	check, ok := integration.Checks["CHECK-001"]
	if !ok || !check.Passed || check.SnapshotDigest != integration.IntegratedTree {
		t.Fatalf("integration check evidence = %#v", integration.Checks)
	}
	if next.Baseline != integration.IntegratedHead || next.Submissions[fixture.Submission.ID].Status != "integrated" {
		t.Fatalf("integration state = %#v", next)
	}
}

func TestIntegrationJournalRecoversAfterFormalBranchAdvance(t *testing.T) {
	fixture := setupApprovedSubmission(t, []string{"true"})
	state := mustProjectState(t, fixture.Project)
	operationFaultHook = func(point string) error {
		if point == "git_applied" {
			return errors.New("injected crash after formal branch advance")
		}
		return nil
	}
	_, _, _, err := integrateSubmission(fixture.Project, fixture.Submission.ID, fixture.Reviewer, state.Revision)
	operationFaultHook = nil
	if err == nil {
		t.Fatal("injected integration unexpectedly completed")
	}
	beforeRecover := mustProjectState(t, fixture.Project)
	if beforeRecover.Submissions[fixture.Submission.ID].Status != "approved" {
		t.Fatalf("state advanced before recovery: %#v", beforeRecover.Submissions[fixture.Submission.ID])
	}
	recovered, err := recoverProject(fixture.Project)
	if err != nil {
		t.Fatal(err)
	}
	if recovered.Submissions[fixture.Submission.ID].Status != "integrated" || recovered.Baseline == beforeRecover.Baseline {
		t.Fatalf("recovered integration state = %#v", recovered)
	}
	formalHead, _ := git(fixture.Project, "rev-parse", "refs/heads/main")
	if formalHead != recovered.Baseline {
		t.Fatalf("formal head = %s, recovered baseline = %s", formalHead, recovered.Baseline)
	}
	if journals, err := listOperationJournals(fixture.Project); err != nil || len(journals) != 0 {
		t.Fatalf("journals after recovery = %d, %v", len(journals), err)
	}
}

func setupApprovedSubmission(t *testing.T, checkArgv []string) approvedSubmissionFixture {
	t.Helper()
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
- task check
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
	encodedArgv, err := json.Marshal(checkArgv)
	if err != nil {
		t.Fatal(err)
	}
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
    argv: %s
    cwd: "."
    env: {}
    timeout_seconds: 10
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
`, requirementsState.Digest, architectureState.Digest, encodedArgv)
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
	if _, _, _, err := runTaskCheck(project, "M001-T001", "", true, developer, state.Revision); err != nil {
		t.Fatal(err)
	}
	state = mustProjectState(t, project)
	_, _, submission, err := workSubmit(project, "M001-T001", "ready", developer, state.Revision)
	if err != nil {
		t.Fatal(err)
	}
	state = mustProjectState(t, project)
	submission = state.Submissions[submission.ID]
	if _, _, _, err := recordReview(project, submission.ID, "approve", "approved", reviewer, state.Revision); err != nil {
		t.Fatal(err)
	}
	return approvedSubmissionFixture{Project: project, Master: master, Orchestrator: orchestrator, Reviewer: reviewer, Submission: mustProjectState(t, project).Submissions[submission.ID]}
}
