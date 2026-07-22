package app

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestTwoDevelopersUseIndependentTaskWorktrees(t *testing.T) {
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
	developerA := issueTestPrincipal(t, project, rootPath, testRoot, "agent:developer-a", "developer")
	developerB := issueTestPrincipal(t, project, rootPath, testRoot, "agent:developer-b", "developer")
	reviewerOnly := issueTestPrincipal(t, project, rootPath, testRoot, "agent:reviewer-only", "reviewer")
	revokedDeveloper := issueTestPrincipal(t, project, rootPath, testRoot, "agent:revoked-developer", "developer")
	if err := revokeCredential(project, rootPath, revokedDeveloper.ID, "test revoked owner"); err != nil {
		t.Fatal(err)
	}

	requirements := `---
kind: requirements
id: requirements
---
# Requirements
## Problem
Need two independent files.
## Required Behavior
- REQ-001: create a.txt and b.txt independently.
## Success Criteria
- SC-001: both checks pass.
## Scope
- a.txt
- b.txt
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
Local files.
## Components and Boundaries
- a.txt
- b.txt
## Interfaces
- filesystem
## Data and State
- Git worktrees
## Security
- signed events
## Validation Strategy
- true
## Parallelization Boundaries
- one file per Task
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
  - M001-T002
---
# Mission M001
## Outcome
Create two files in parallel.
## Requirements Covered
- REQ-001
## Acceptance Criteria
- Both Tasks integrated.
## Constraints and Risks
- None
## Completion Evidence
- Pending
`, requirementsState.Digest, architectureState.Digest)
	writeTestArtifact(t, project, "docs/missions/M001.md", mission)
	submitAndAcceptTestArtifact(t, project, "docs/missions/M001.md", designer, master)

	for index, taskID := range []string{"M001-T001", "M001-T002"} {
		file := []string{"a.txt", "b.txt"}[index]
		taskDocument := fmt.Sprintf(`---
kind: task
id: %s
mission_id: M001
requirements_digest: %s
architecture_digest: %s
depends_on: []
allowed_paths:
  - %s
acceptance_checks:
  - id: CHECK-001
    argv: ["true"]
    cwd: "."
    env: {}
    timeout_seconds: 10
---
# Task %s
## Objective
Create %s.
## Inputs and Assumptions
- Empty repository.
## Forbidden and Out of Scope
- The other Task file.
## Deliverables
- %s
## Stop Conditions
- Scope change.
## Reviewer Attention
- Worktree isolation.
`, taskID, requirementsState.Digest, architectureState.Digest, file, taskID, file, file)
		path := "docs/tasks/" + taskID + ".md"
		writeTestArtifact(t, project, path, taskDocument)
		submitAndAcceptTestArtifact(t, project, path, designer, master)
	}

	state := mustProjectState(t, project)
	if _, _, err := activateMission(project, "M001", orchestrator, state.Revision); err != nil {
		t.Fatal(err)
	}
	for _, owner := range []string{"agent:missing", reviewerOnly.Actor, revokedDeveloper.Actor} {
		state = mustProjectState(t, project)
		if _, _, _, err := taskClaimOrAssign(project, "M001-T001", owner, orchestrator, state.Revision, true); err == nil {
			t.Fatalf("assignment accepted invalid Developer owner %q", owner)
		} else if typed, ok := err.(*CLIError); !ok || typed.Code != "CHS-TASK-OWNER-AUTH" {
			t.Fatalf("invalid owner error = %#v, want CHS-TASK-OWNER-AUTH", err)
		}
	}
	state = mustProjectState(t, project)
	if _, _, _, err := taskClaimOrAssign(project, "M001-T001", developerA.Actor, orchestrator, state.Revision, true); err != nil {
		t.Fatal(err)
	}
	assigned := mustProjectState(t, project).Tasks["M001-T001"]
	if assigned.OwnerGrantID != developerA.ID {
		t.Fatalf("owner grant = %q, want %q", assigned.OwnerGrantID, developerA.ID)
	}
	if err := revokeCredential(project, rootPath, developerA.ID, "rotate Developer credential"); err != nil {
		t.Fatal(err)
	}
	rotatedA := issueTestPrincipal(t, project, rootPath, testRoot, developerA.Actor, "developer")
	state = mustProjectState(t, project)
	if _, _, _, err := workOpen(project, "M001-T001", rotatedA, state.Revision); err != nil {
		t.Fatalf("same-actor rotated credential could not continue assigned Task: %v", err)
	}
	for _, assignment := range []struct {
		ID        string
		Developer Principal
	}{{"M001-T002", developerB}} {
		state = mustProjectState(t, project)
		if _, _, _, err := taskClaimOrAssign(project, assignment.ID, assignment.Developer.Actor, orchestrator, state.Revision, true); err != nil {
			t.Fatal(err)
		}
		state = mustProjectState(t, project)
		if _, _, _, err := workOpen(project, assignment.ID, assignment.Developer, state.Revision); err != nil {
			t.Fatal(err)
		}
	}

	state = mustProjectState(t, project)
	taskA, taskB := state.Tasks["M001-T001"], state.Tasks["M001-T002"]
	rootA, err := taskWorktreeRoot(project, taskA)
	if err != nil {
		t.Fatal(err)
	}
	rootB, err := taskWorktreeRoot(project, taskB)
	if err != nil {
		t.Fatal(err)
	}
	if samePath(rootA, rootB) || taskA.WorktreeID == taskB.WorktreeID || taskA.WorktreeDigest == taskB.WorktreeDigest {
		t.Fatalf("tasks share worktree binding: %#v %#v", taskA, taskB)
	}
	if branch, _ := currentBranch(project); branch != "main" {
		t.Fatalf("opening Tasks switched primary worktree to %q", branch)
	}
	releasedPath, releasedBranch := rootB, taskB.Branch
	state = mustProjectState(t, project)
	operationFaultHook = func(point string) error {
		if point == "state_committed" {
			return errors.New("injected crash before release cleanup")
		}
		return nil
	}
	_, _, _, releaseErr := taskRelease(project, taskB.ID, orchestrator, state.Revision)
	operationFaultHook = nil
	if releaseErr == nil {
		t.Fatal("injected release unexpectedly completed")
	}
	recovered, err := recoverProject(project)
	if err != nil {
		t.Fatal(err)
	}
	if released := recovered.Tasks[taskB.ID]; released.Status != "ready" || released.Owner != "" {
		t.Fatalf("recovered released Task = %#v", released)
	}
	if _, err := os.Stat(releasedPath); !os.IsNotExist(err) {
		t.Fatalf("released worktree still exists: %v", err)
	}
	if _, err := git(project, "rev-parse", "--verify", "refs/heads/"+releasedBranch); err == nil {
		t.Fatal("released baseline-only Task branch was retained")
	}
	state = mustProjectState(t, project)
	if _, _, _, err := taskClaimOrAssign(project, taskB.ID, developerB.Actor, orchestrator, state.Revision, true); err != nil {
		t.Fatal(err)
	}
	state = mustProjectState(t, project)
	if _, _, _, err := workOpen(project, taskB.ID, developerB, state.Revision); err != nil {
		t.Fatal(err)
	}
	state = mustProjectState(t, project)
	taskA, taskB = state.Tasks["M001-T001"], state.Tasks["M001-T002"]
	rootB, err = taskWorktreeRoot(project, taskB)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(rootA, "a.txt"), []byte("a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(rootB, "b.txt"), []byte("b\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	filesA, err := gitWorkingFiles(rootA)
	if err != nil {
		t.Fatal(err)
	}
	filesB, err := gitWorkingFiles(rootB)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(filesA, []string{"a.txt"}) || !reflect.DeepEqual(filesB, []string{"b.txt"}) {
		t.Fatalf("worktree changes leaked: A=%#v B=%#v", filesA, filesB)
	}
	state = mustProjectState(t, project)
	if _, _, _, err := taskRelease(project, taskA.ID, orchestrator, state.Revision); err == nil {
		t.Fatal("release discarded dirty Task work")
	} else if typed, ok := err.(*CLIError); !ok || typed.Code != "CHS-TASK-RELEASE-CHANGES" {
		t.Fatalf("dirty release error = %#v", err)
	}

	config, _, _, err := loadProject(project)
	if err != nil {
		t.Fatal(err)
	}
	forged := state
	forged.Tasks = make(map[string]TaskState, len(state.Tasks))
	for id, task := range state.Tasks {
		forged.Tasks[id] = task
	}
	duplicate := forged.Tasks["M001-T002"]
	duplicate.WorktreePath = taskA.WorktreePath
	duplicate.WorktreeID = taskA.WorktreeID
	duplicate.WorktreeDigest = taskA.WorktreeDigest
	forged.Tasks[duplicate.ID] = duplicate
	if err := validateState(config, forged); err == nil {
		t.Fatal("state validator accepted one worktree bound to two Tasks")
	}

	moved := rootA + "-moved"
	if err := os.Rename(rootA, moved); err != nil {
		t.Fatal(err)
	}
	state = mustProjectState(t, project)
	if _, _, _, err := runTaskCheck(project, taskA.ID, "", true, rotatedA, state.Revision); err == nil {
		t.Fatal("check accepted a moved task worktree")
	} else if typed, ok := err.(*CLIError); !ok || typed.Code != "CHS-WORKTREE-MISSING" {
		t.Fatalf("moved worktree error = %#v, want CHS-WORKTREE-MISSING", err)
	}

	replacementDocument := fmt.Sprintf(`---
kind: task
id: M001-T003
mission_id: M001
requirements_digest: %s
architecture_digest: %s
depends_on: []
allowed_paths:
  - a-v2.txt
acceptance_checks:
  - id: CHECK-001
    argv: ["true"]
    cwd: "."
    env: {}
    timeout_seconds: 10
---
# Task M001-T003
## Objective
Replace the frozen contract of M001-T001.
## Inputs and Assumptions
- The old Task remains immutable.
## Forbidden and Out of Scope
- Editing the old Task contract.
## Deliverables
- a-v2.txt
## Stop Conditions
- Scope change.
## Reviewer Attention
- Supersede linkage.
`, requirementsState.Digest, architectureState.Digest)
	writeTestArtifact(t, project, "docs/tasks/M001-T003.md", replacementDocument)
	submitAndAcceptTestArtifact(t, project, "docs/tasks/M001-T003.md", designer, master)
	state = mustProjectState(t, project)
	if _, next, oldTask, err := taskSupersede(project, taskA.ID, "M001-T003", orchestrator, state.Revision); err != nil {
		t.Fatal(err)
	} else if oldTask.Status != "superseded" || oldTask.ReplacementID != "M001-T003" || next.Tasks["M001-T003"].Status != "ready" || next.Tasks["M001-T003"].SupersedesID != taskA.ID {
		t.Fatalf("supersede state: old=%#v new=%#v", oldTask, next.Tasks["M001-T003"])
	}
	state = mustProjectState(t, project)
	if _, next, cancelled, err := taskCancel(project, "M001-T003", "Master waived replacement", master, state.Revision); err != nil {
		t.Fatal(err)
	} else if cancelled.Status != "cancelled" || cancelled.ClosureReason == "" || !dependencySatisfied(next, taskA.ID) {
		t.Fatalf("cancelled replacement state = %#v", cancelled)
	}
}
