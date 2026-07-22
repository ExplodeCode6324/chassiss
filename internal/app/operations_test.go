package app

import (
	"errors"
	"path/filepath"
	"testing"
)

func TestOperationJournalRecoversArtifactCommitAtEveryDurablePhase(t *testing.T) {
	points := []string{"git_ref_applied", "git_applied_before_phase", "git_applied", "event_stored", "state_committed"}
	for _, point := range points {
		t.Run(point, func(t *testing.T) {
			project, rootPath, submissionID := setupSubmittedRequirements(t)
			masterKey, public, private, err := loadRoot(rootPath)
			if err != nil {
				t.Fatal(err)
			}
			master := rootPrincipal(masterKey, public, private)
			state := mustProjectState(t, project)
			operationFaultHook = func(current string) error {
				if current == point {
					return errors.New("injected crash at " + point)
				}
				return nil
			}
			_, _, _, err = acceptArtifact(project, submissionID, master, state.Revision)
			operationFaultHook = nil
			if err == nil {
				t.Fatal("injected operation unexpectedly completed")
			}
			journals, listErr := listOperationJournals(project)
			if listErr != nil || len(journals) != 1 {
				t.Fatalf("journals = %d, %v; want one recoverable operation", len(journals), listErr)
			}
			recovered, err := recoverProject(project)
			if err != nil {
				t.Fatalf("recover after %s: %v", point, err)
			}
			if recovered.Artifacts["requirements"].Status != "accepted" {
				t.Fatalf("recovered artifact = %#v", recovered.Artifacts["requirements"])
			}
			if _, err := recoverProject(project); err != nil {
				t.Fatalf("repeated recover is not idempotent: %v", err)
			}
			journals, err = listOperationJournals(project)
			if err != nil || len(journals) != 0 {
				t.Fatalf("final journals = %d, %v; want none", len(journals), err)
			}
			if _, err := verifyProject(project); err != nil {
				t.Fatalf("recovered project does not verify: %v", err)
			}
		})
	}
}

func TestPreparedOperationWithoutGitSideEffectIsCancelled(t *testing.T) {
	project, rootPath, submissionID := setupSubmittedRequirements(t)
	masterKey, public, private, err := loadRoot(rootPath)
	if err != nil {
		t.Fatal(err)
	}
	master := rootPrincipal(masterKey, public, private)
	state := mustProjectState(t, project)
	before, err := captureGitOperationState(project)
	if err != nil {
		t.Fatal(err)
	}
	operationFaultHook = func(point string) error {
		if point == "prepared" {
			return errors.New("injected crash before Git preparation")
		}
		return nil
	}
	_, _, _, err = acceptArtifact(project, submissionID, master, state.Revision)
	operationFaultHook = nil
	if err == nil {
		t.Fatal("injected prepared operation unexpectedly completed")
	}
	recovered, err := recoverProject(project)
	if err != nil {
		t.Fatal(err)
	}
	after, err := captureGitOperationState(project)
	if err != nil {
		t.Fatal(err)
	}
	if before != after || recovered.Artifacts["requirements"].Status != "submitted" {
		t.Fatalf("untouched prepared operation was not cancelled safely: before=%#v after=%#v artifact=%#v", before, after, recovered.Artifacts["requirements"])
	}
}

func TestRecoverIntegrityBlocksUnexpectedGitResult(t *testing.T) {
	project, rootPath, submissionID := setupSubmittedRequirements(t)
	masterKey, public, private, err := loadRoot(rootPath)
	if err != nil {
		t.Fatal(err)
	}
	master := rootPrincipal(masterKey, public, private)
	state := mustProjectState(t, project)
	operationFaultHook = func(point string) error {
		if point == "git_applied" {
			return errors.New("injected crash after Git")
		}
		return nil
	}
	_, _, _, err = acceptArtifact(project, submissionID, master, state.Revision)
	operationFaultHook = nil
	if err == nil {
		t.Fatal("injected operation unexpectedly completed")
	}
	if _, err := git(project, "-c", "user.name=intruder", "-c", "user.email=intruder@invalid", "commit", "--allow-empty", "-m", "unexpected"); err != nil {
		t.Fatal(err)
	}
	unexpectedHead, err := gitHead(project)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := recoverProject(project); err == nil {
		t.Fatal("recover accepted Git state that differed from the journal")
	} else if typed, ok := err.(*CLIError); !ok || typed.Code != "CHS-INTEGRITY-BLOCKED" {
		t.Fatalf("recover error = %#v, want CHS-INTEGRITY-BLOCKED", err)
	}
	currentHead, _ := gitHead(project)
	if currentHead != unexpectedHead {
		t.Fatalf("recover implicitly moved formal branch from %s to %s", unexpectedHead, currentHead)
	}
}

func setupSubmittedRequirements(t *testing.T) (project, rootPath, submissionID string) {
	t.Helper()
	testRoot := t.TempDir()
	rootPath = filepath.Join(testRoot, "master-root.yaml")
	if _, err := createRoot(rootPath); err != nil {
		t.Fatal(err)
	}
	project = filepath.Join(testRoot, "project")
	if _, _, err := initializeProject(project, rootPath, false); err != nil {
		t.Fatal(err)
	}
	designer := issueTestPrincipal(t, project, rootPath, testRoot, "agent:designer", "designer")
	content := `---
kind: requirements
id: requirements
---
# Requirements
## Problem
Need a test.
## Required Behavior
- REQ-001: behave.
## Success Criteria
- SC-001: pass.
## Scope
- local
## Constraints
- none
## Decisions Required from Master
- None
`
	writeTestArtifact(t, project, "docs/requirements.md", content)
	state := mustProjectState(t, project)
	_, _, artifact, err := submitArtifact(project, "docs/requirements.md", designer, state.Revision)
	if err != nil {
		t.Fatal(err)
	}
	return project, rootPath, artifact.SubmissionID
}
