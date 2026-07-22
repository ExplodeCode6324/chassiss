package app

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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
	state.Phase = "idle"
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
	config, _, state, err := loadProject(target)
	if err != nil {
		t.Fatal(err)
	}
	_, _, _, eventsPath := projectPaths(target)
	events, err := readEvents(eventsPath)
	if err != nil {
		t.Fatal(err)
	}
	forged, err := makeEvent(config.ProjectID, state.Revision+1, "artifact.accepted", "requirements", principal, events[len(events)-1].Digest, timeNow(), artifactAcceptedPayload{ArtifactID: "requirements", AcceptedCommit: "forged"})
	if err != nil {
		t.Fatal(err)
	}
	if err := writeEventAtomic(eventsPath, forged); err != nil {
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

func TestCLIRecoverBypassesInvalidStateProjection(t *testing.T) {
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
	state.Publications = nil
	_, _, statePath, _ := projectPaths(target)
	if err := writeYAMLAtomic(statePath, &state, 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := dispatch(globalOptions{root: target}, []string{"recover"})
	if err != nil {
		t.Fatalf("CLI recover was blocked by an invalid projection: %v", err)
	}
	if result.RevisionAfter != 1 {
		t.Fatalf("recovered revision = %d, want 1", result.RevisionAfter)
	}
	recovered := mustProjectState(t, target)
	if recovered.Publications == nil {
		t.Fatal("CLI recover did not restore deterministic state collections")
	}
}

func TestEventV2StoresMinimalPayloadAndReplaysDeterministically(t *testing.T) {
	root := t.TempDir()
	rootPath := filepath.Join(root, "master-root.yaml")
	if _, err := createRoot(rootPath); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(root, "project")
	if _, _, err := initializeProject(target, rootPath, false); err != nil {
		t.Fatal(err)
	}
	config, trust, _, err := loadProject(target)
	if err != nil {
		t.Fatal(err)
	}
	_, _, _, eventsPath := projectPaths(target)
	entries, err := os.ReadDir(eventsPath)
	if err != nil || len(entries) != 1 {
		t.Fatalf("event files = %d, %v; want one", len(entries), err)
	}
	raw, err := os.ReadFile(filepath.Join(eventsPath, entries[0].Name()))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte(`"state"`)) || !bytes.Contains(raw, []byte(`"payload"`)) {
		t.Fatalf("event is not a minimal-payload V2 event: %s", raw)
	}
	events, err := readEvents(eventsPath)
	if err != nil {
		t.Fatal(err)
	}
	first, err := verifyEventChain(config, trust, events)
	if err != nil {
		t.Fatal(err)
	}
	second, err := verifyEventChain(config, trust, events)
	if err != nil {
		t.Fatal(err)
	}
	firstJSON, _ := canonicalJSON(first)
	secondJSON, _ := canonicalJSON(second)
	if !bytes.Equal(firstJSON, secondJSON) {
		t.Fatalf("identical event sequence produced different state\n%s\n%s", firstJSON, secondJSON)
	}
}

func TestReduceEventRejectsDeveloperPayloadWithMissionDelta(t *testing.T) {
	now := time.Unix(100, 0).UTC()
	config := Config{Version: ConfigVersion, ProjectID: "PRJ-1", WIPLimit: 2}
	missionArtifact := ArtifactState{ID: "M001", Kind: "mission", Path: "docs/missions/M001.md", Digest: "sha256:mission", Status: "accepted", SubmissionID: "ART-M", SubmittedBy: "agent:designer", AcceptedBy: "master", AcceptedCommit: "base", UpdatedAt: now}
	taskArtifact := ArtifactState{ID: "M001-T001", Kind: "task", Path: "docs/tasks/M001-T001.md", Digest: "sha256:task", Status: "accepted", SubmissionID: "ART-T", SubmittedBy: "agent:designer", AcceptedBy: "master", AcceptedCommit: "base", UpdatedAt: now}
	previous := State{
		Version: StateVersion, ProjectID: config.ProjectID, Revision: 5, Phase: "execution", Baseline: "base", ActiveMission: "M001",
		Artifacts: map[string]ArtifactState{"M001": missionArtifact, "M001-T001": taskArtifact},
		Missions:  map[string]MissionState{"M001": {ID: "M001", ArtifactID: "M001", Status: "active", TaskIDs: []string{"M001-T001"}, UpdatedAt: now}},
		Tasks: map[string]TaskState{"M001-T001": {
			ID: "M001-T001", MissionID: "M001", ArtifactID: "M001-T001", Status: "in_progress", Owner: "agent:developer", Branch: "chassiss/m001-t001", Baseline: "base",
			DependsOn: []string{}, AllowedPaths: []string{"code.go"}, Checks: []CheckSpec{{ID: "CHECK-001", Argv: []string{"go", "test", "./..."}, Cwd: ".", Env: map[string]string{}, TimeoutSeconds: 120}}, CheckResults: map[string]CheckResult{}, UpdatedAt: now,
		}},
		Submissions: map[string]Submission{}, Reviews: map[string]Review{}, Integrations: map[string]Integration{}, UpdatedAt: now, UpdatedBy: "agent:developer",
	}
	payload := json.RawMessage(`{"task_id":"M001-T001","results":[{"id":"CHECK-001","spec_digest":"sha256:forged","exit_code":0,"passed":true,"output":"ok","snapshot_digest":"sha256:snapshot","checked_at":"1970-01-01T00:00:00Z"}],"mission":{"status":"completed"}}`)
	event := Event{Version: EventVersion, ProjectID: config.ProjectID, Sequence: 6, ID: "EVT-6", Type: "work.checked", Actor: "agent:developer", Role: "developer", CredentialID: "CRED-D", Resource: "M001-T001", OccurredAt: now.Add(time.Second), Payload: payload}
	if _, err := reduceEvent(config, previous, event); err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("developer mission delta error = %v, want strict unknown-field rejection", err)
	}
}

func TestEventPayloadMutationInvalidatesSignature(t *testing.T) {
	root := t.TempDir()
	rootPath := filepath.Join(root, "master-root.yaml")
	if _, err := createRoot(rootPath); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(root, "project")
	if _, _, err := initializeProject(target, rootPath, false); err != nil {
		t.Fatal(err)
	}
	config, trust, _, err := loadProject(target)
	if err != nil {
		t.Fatal(err)
	}
	_, _, _, eventsPath := projectPaths(target)
	events, err := readEvents(eventsPath)
	if err != nil {
		t.Fatal(err)
	}
	var payload projectInitializedPayload
	if err := json.Unmarshal(events[0].Payload, &payload); err != nil {
		t.Fatal(err)
	}
	payload.Baseline = "tampered"
	events[0].Payload, _ = marshalPayload(payload)
	if _, err := verifyEventChain(config, trust, events); err == nil || !strings.Contains(err.Error(), "digest is invalid") {
		t.Fatalf("mutated signed payload error = %v, want digest rejection", err)
	}
}

func TestLoadProjectExplicitlyRejectsV1Config(t *testing.T) {
	root := t.TempDir()
	rootPath := filepath.Join(root, "master-root.yaml")
	if _, err := createRoot(rootPath); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(root, "project")
	if _, _, err := initializeProject(target, rootPath, false); err != nil {
		t.Fatal(err)
	}
	configPath, _, _, _ := projectPaths(target)
	var config Config
	if err := loadYAML(configPath, &config); err != nil {
		t.Fatal(err)
	}
	config.Version = 1
	if err := writeYAMLAtomic(configPath, config, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := loadProject(target); err == nil {
		t.Fatal("V1 config was silently accepted")
	} else if typed, ok := err.(*CLIError); !ok || typed.Code != "CHS-SCHEMA-V1-UNSUPPORTED" {
		t.Fatalf("error = %#v, want CHS-SCHEMA-V1-UNSUPPORTED", err)
	}
}
