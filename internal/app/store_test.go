package app

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInitializeGreenfieldCreatesIndependentNestedRepository(t *testing.T) {
	parent := t.TempDir()
	if _, err := git(parent, "init", "-b", "main"); err != nil {
		t.Fatal(err)
	}
	rootPath := filepath.Join(parent, "master-root.yaml")
	if _, err := createRoot(rootPath); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(parent, "nested-project")
	config, _, err := initializeProject(target, rootPath, false)
	if err != nil {
		t.Fatal(err)
	}
	if config.Mode != "greenfield" {
		t.Fatalf("mode = %q, want greenfield", config.Mode)
	}
	top, err := git(target, "rev-parse", "--show-toplevel")
	if err != nil {
		t.Fatal(err)
	}
	if !samePath(top, target) {
		t.Fatalf("repository root = %q, want %q", top, target)
	}
}

func TestInitializeBrownfieldPreservesHistoryAndBranch(t *testing.T) {
	root := t.TempDir()
	rootPath := filepath.Join(root, "master-root.yaml")
	if _, err := createRoot(rootPath); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(root, "existing")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := git(target, "init", "-b", "trunk"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "legacy.txt"), []byte("legacy\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	before, err := gitCommit(target, "legacy baseline", "legacy.txt")
	if err != nil {
		t.Fatal(err)
	}
	config, state, err := initializeProject(target, rootPath, true)
	if err != nil {
		t.Fatal(err)
	}
	if config.Mode != "brownfield" || config.DefaultBranch != "trunk" {
		t.Fatalf("config = %#v", config)
	}
	if state.Baseline != before {
		t.Fatalf("baseline = %q, want %q", state.Baseline, before)
	}
}

func TestUpdateStateRejectsUnsignedProjectionChange(t *testing.T) {
	root := t.TempDir()
	rootPath := filepath.Join(root, "master-root.yaml")
	if _, err := createRoot(rootPath); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(root, "project")
	if _, _, err := initializeProject(target, rootPath, false); err != nil {
		t.Fatal(err)
	}
	config, _, state, err := loadProject(target)
	if err != nil {
		t.Fatal(err)
	}
	state.Phase = "tampered-but-schema-valid"
	_, _, statePath, _ := projectPaths(target)
	if err := writeYAMLAtomic(statePath, &state, 0o644); err != nil {
		t.Fatal(err)
	}
	rootKey, public, private, err := loadRoot(rootPath)
	if err != nil {
		t.Fatal(err)
	}
	principal := rootPrincipal(rootKey, public, private)
	_, _, _, err = updateState(target, principal, "test.noop", config.ProjectID, state.Revision, func(*State) error { return nil })
	if err == nil {
		t.Fatal("unsigned state projection change was accepted")
	}
	if typed, ok := err.(*CLIError); !ok || typed.Code != "CHS-INTEGRITY-PROJECTION" {
		t.Fatalf("error = %#v, want CHS-INTEGRITY-PROJECTION", err)
	}
}

func TestVerifyEventChainRejectsRoleActionForgery(t *testing.T) {
	root := t.TempDir()
	rootPath := filepath.Join(root, "master-root.yaml")
	if _, err := createRoot(rootPath); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(root, "project")
	if _, _, err := initializeProject(target, rootPath, false); err != nil {
		t.Fatal(err)
	}
	credentialPath := filepath.Join(root, "developer.yaml")
	if _, err := issueCredential(target, rootPath, "agent:developer", "developer", credentialPath, nil); err != nil {
		t.Fatal(err)
	}
	principal, err := loadPrincipal(target, credentialPath, "")
	if err != nil {
		t.Fatal(err)
	}
	_, _, state, err := loadProject(target)
	if err != nil {
		t.Fatal(err)
	}
	_, _, statePath, eventsPath := projectPaths(target)
	events, err := readEvents(eventsPath)
	if err != nil {
		t.Fatal(err)
	}
	state.Revision++
	state.UpdatedBy = principal.Actor
	state.UpdatedAt = timeNow()
	forged, err := makeEvent(state, "artifact.accepted", "requirements", principal, events[len(events)-1].Digest)
	if err != nil {
		t.Fatal(err)
	}
	if err := appendJSONLine(eventsPath, &forged); err != nil {
		t.Fatal(err)
	}
	if err := writeYAMLAtomic(statePath, &state, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := verifyProject(target); err == nil {
		t.Fatal("developer-signed artifact acceptance forgery was accepted")
	}
}

func TestRecoverProjectRebuildsSignedProjection(t *testing.T) {
	root := t.TempDir()
	rootPath := filepath.Join(root, "master-root.yaml")
	if _, err := createRoot(rootPath); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(root, "project")
	if _, _, err := initializeProject(target, rootPath, false); err != nil {
		t.Fatal(err)
	}
	_, _, state, err := loadProject(target)
	if err != nil {
		t.Fatal(err)
	}
	state.Phase = "tampered"
	_, _, statePath, _ := projectPaths(target)
	if err := writeYAMLAtomic(statePath, &state, 0o644); err != nil {
		t.Fatal(err)
	}
	recovered, err := recoverProject(target)
	if err != nil {
		t.Fatal(err)
	}
	if recovered.Phase != "design" || recovered.Revision != 1 {
		t.Fatalf("recovered state = %#v", recovered)
	}
	if _, err := verifyProject(target); err != nil {
		t.Fatalf("recovered project did not verify: %v", err)
	}
}
