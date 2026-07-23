package app

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
)

func TestOwnerCredentialIsUniqueUntilExplicitlyRevoked(t *testing.T) {
	project, rootPath := setupAuthProject(t)
	firstPath := filepath.Join(t.TempDir(), "owner-first.yaml")
	first, err := issueCredential(project, rootPath, "master:owner", "owner", firstPath, nil)
	if err != nil {
		t.Fatal(err)
	}
	secondPath := filepath.Join(t.TempDir(), "owner-second.yaml")
	if _, err := issueCredential(project, rootPath, "master:replacement", "owner", secondPath, nil); err == nil {
		t.Fatal("second unrevoked Owner credential was issued")
	} else if typed, ok := err.(*CLIError); !ok || typed.Code != "CHS-AUTH-OWNER-EXISTS" || typed.Diagnostic != "owner_already_exists" {
		t.Fatalf("duplicate Owner error = %#v", err)
	}
	if _, err := os.Stat(secondPath); !os.IsNotExist(err) {
		t.Fatalf("rejected Owner issue left an output file: %v", err)
	}

	developerPath := filepath.Join(t.TempDir(), "developer.yaml")
	if _, err := issueCredential(project, rootPath, "agent:developer", "developer", developerPath, nil); err != nil {
		t.Fatalf("Owner uniqueness blocked an unrelated role: %v", err)
	}
	if err := revokeCredential(project, rootPath, first.ID, "rotate Owner"); err != nil {
		t.Fatal(err)
	}
	second, err := issueCredential(project, rootPath, "master:replacement", "owner", secondPath, nil)
	if err != nil {
		t.Fatalf("explicitly revoked Owner could not be replaced: %v", err)
	}
	if _, err := loadPrincipal(project, secondPath, "owner.apply"); err != nil {
		t.Fatalf("replacement Owner credential is unusable: %v", err)
	}

	_, trust, _, err := loadProject(project)
	if err != nil {
		t.Fatal(err)
	}
	current, exists, err := unrevokedOwnerGrant(trust)
	if err != nil || !exists || current.ID != second.ID {
		t.Fatalf("current Owner = %#v, %v, want %s", current, err, second.ID)
	}

	_, _, private, err := loadRoot(rootPath)
	if err != nil {
		t.Fatal(err)
	}
	tampered := trust
	tampered.Grants = append(append([]Grant(nil), trust.Grants...), Grant{ID: "CRED-DUPLICATE-OWNER", Actor: "master:duplicate", Role: "owner"})
	if err := signTrust(&tampered, private); err != nil {
		t.Fatal(err)
	}
	if err := verifyTrust(mustProjectConfig(t, project), tampered); err == nil {
		t.Fatal("root-signed trust with two unrevoked Owner grants was accepted")
	} else if typed, ok := err.(*CLIError); !ok || typed.Code != "CHS-INTEGRITY-TRUST" || typed.Diagnostic != "owner_grant_conflict" {
		t.Fatalf("duplicate Owner trust error = %#v", err)
	}
}

func TestConcurrentOwnerIssueConvergesToOneGrant(t *testing.T) {
	project, rootPath := setupAuthProject(t)
	outputs := []string{filepath.Join(t.TempDir(), "one.yaml"), filepath.Join(t.TempDir(), "two.yaml")}
	results := make([]error, len(outputs))
	start := make(chan struct{})
	var wait sync.WaitGroup
	for index := range outputs {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			<-start
			_, results[index] = issueCredential(project, rootPath, "master:owner-"+string(rune('a'+index)), "owner", outputs[index], nil)
		}(index)
	}
	close(start)
	wait.Wait()

	successes := 0
	for index, err := range results {
		if err == nil {
			successes++
			continue
		}
		typed, ok := err.(*CLIError)
		if !ok || (typed.Code != "CHS-CONFLICT-LOCKED" && typed.Code != "CHS-AUTH-OWNER-EXISTS") {
			t.Fatalf("concurrent Owner issue error = %#v", err)
		}
		if typed.Code == "CHS-CONFLICT-LOCKED" {
			if _, retryErr := issueCredential(project, rootPath, "master:owner-retry", "owner", outputs[index], nil); retryErr == nil {
				t.Fatal("serialized retry issued a second Owner")
			} else if retryTyped, ok := retryErr.(*CLIError); !ok || retryTyped.Code != "CHS-AUTH-OWNER-EXISTS" {
				t.Fatalf("serialized retry error = %#v", retryErr)
			}
		}
	}
	if successes != 1 {
		t.Fatalf("concurrent Owner issue successes = %d, want 1", successes)
	}
	_, trust, _, err := loadProject(project)
	if err != nil {
		t.Fatal(err)
	}
	if _, exists, err := unrevokedOwnerGrant(trust); err != nil || !exists {
		t.Fatalf("concurrent Owner trust = %#v, exists=%v, err=%v", trust, exists, err)
	}
}

func TestOwnerApplyAdvancesBaselineWithSignedAuditHistory(t *testing.T) {
	project, rootPath := setupAuthProject(t)
	ownerPath := filepath.Join(t.TempDir(), "owner.yaml")
	if _, err := issueCredential(project, rootPath, "master:owner", "owner", ownerPath, nil); err != nil {
		t.Fatal(err)
	}
	owner, err := loadPrincipal(project, ownerPath, "owner.apply")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := loadPrincipal(project, rootPath, "owner.apply"); err == nil {
		t.Fatal("Master Root was allowed to bypass the dedicated Owner credential")
	}
	_, trust, before, err := loadProject(project)
	if err != nil {
		t.Fatal(err)
	}
	bootstrap, err := buildBootstrapResult(project, trust, before, owner)
	if err != nil {
		t.Fatal(err)
	}
	if len(bootstrap.AvailableActions) != 1 || bootstrap.AvailableActions[0].Action != "owner.apply" ||
		len(bootstrap.AvailableActions[0].RequiredInputs) != 1 || bootstrap.AvailableActions[0].RequiredInputs[0].Name != "reason" {
		t.Fatalf("Owner bootstrap action = %#v", bootstrap.AvailableActions)
	}

	if err := os.WriteFile(filepath.Join(project, "maintenance.txt"), []byte("owner maintenance\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	previous, next, change, err := ownerApply(project, "Update repository maintenance metadata", owner, before.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if change.ID == "" || change.Actor != owner.Actor || change.CredentialID != owner.ID || change.PreviousHead != previous.Baseline ||
		change.NewHead != next.Baseline || len(change.ChangedFiles) != 1 || change.ChangedFiles[0] != "maintenance.txt" ||
		change.Metrics.ChangedFiles != 1 || change.Metrics.Commits != 1 || next.LastOwnerChangeID != change.ID {
		t.Fatalf("Owner change = %#v, next = %#v", change, next)
	}
	if clean, detail, err := gitClean(project); err != nil || !clean {
		t.Fatalf("Owner apply left Git dirty: clean=%v detail=%q err=%v", clean, detail, err)
	}
	verified, err := verifyProject(project)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(verified.OwnerChanges[change.ID], change) {
		t.Fatalf("verified Owner history lost change: %#v", verified.OwnerChanges)
	}

	response, err := dispatch(globalOptions{root: project, credential: ownerPath}, []string{"owner", "history"})
	if err != nil {
		t.Fatal(err)
	}
	result, ok := response.Result.(map[string]any)
	if !ok {
		t.Fatalf("Owner history result = %#v", response.Result)
	}
	items, ok := result["owner_changes"].([]OwnerChange)
	if !ok || len(items) != 1 || items[0].ID != change.ID {
		t.Fatalf("Owner history = %#v", result)
	}
	if _, err := dispatch(globalOptions{root: project, credential: rootPath}, []string{"owner", "history"}); err != nil {
		t.Fatalf("Master Root could not inspect Owner history: %v", err)
	}
}

func TestOwnerApplyRejectsUnsafeStateProtectedFilesAndMovedHead(t *testing.T) {
	active := State{
		Phase: "execution", ActiveMission: "M001",
		Missions:  map[string]MissionState{"M001": {ID: "M001", Status: "active"}},
		Tasks:     map[string]TaskState{},
		Artifacts: map[string]ArtifactState{},
	}
	if err := ownerApplyStateAllowed(active); err == nil {
		t.Fatal("active Mission allowed an Owner baseline change")
	}
	protected := State{Artifacts: map[string]ArtifactState{"requirements": {ID: "requirements", Path: "docs/requirements.md"}}}
	if err := validateOwnerChangedFiles(protected, []string{"docs/requirements.md"}); err == nil {
		t.Fatal("managed artifact path allowed an Owner baseline change")
	}
	if err := validateOwnerChangedFiles(State{Artifacts: map[string]ArtifactState{}}, []string{".chassis/state.yaml"}); err == nil {
		t.Fatal("control path allowed an Owner baseline change")
	}

	project, rootPath := setupAuthProject(t)
	ownerPath := filepath.Join(t.TempDir(), "owner.yaml")
	if _, err := issueCredential(project, rootPath, "master:owner", "owner", ownerPath, nil); err != nil {
		t.Fatal(err)
	}
	owner, err := loadPrincipal(project, ownerPath, "owner.apply")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project, "manual.txt"), []byte("committed outside CHASSISS\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := gitCommit(project, "manual commit", "manual.txt"); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := ownerApply(project, "Adopt manual commit", owner, -1); err == nil {
		t.Fatal("Owner apply adopted a pre-existing commit")
	} else if typed, ok := err.(*CLIError); !ok || typed.Code != "CHS-OWNER-BASELINE-MOVED" {
		t.Fatalf("moved baseline error = %#v", err)
	}
}

func TestOwnerApplyRecoversAfterRefAdvance(t *testing.T) {
	project, rootPath := setupAuthProject(t)
	ownerPath := filepath.Join(t.TempDir(), "owner.yaml")
	if _, err := issueCredential(project, rootPath, "master:owner", "owner", ownerPath, nil); err != nil {
		t.Fatal(err)
	}
	owner, err := loadPrincipal(project, ownerPath, "owner.apply")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project, "recover.txt"), []byte("recoverable\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	operationFaultHook = func(point string) error {
		if point == "git_ref_applied" {
			return errors.New("injected Owner crash after ref advance")
		}
		return nil
	}
	t.Cleanup(func() { operationFaultHook = nil })
	if _, _, _, err := ownerApply(project, "Recover Owner maintenance", owner, -1); err == nil {
		t.Fatal("injected Owner operation unexpectedly completed")
	}
	operationFaultHook = nil
	recovered, err := recoverProject(project)
	if err != nil {
		t.Fatal(err)
	}
	if len(recovered.OwnerChanges) != 1 || recovered.LastOwnerChangeID == "" {
		t.Fatalf("recovered Owner history = %#v", recovered.OwnerChanges)
	}
	if _, err := verifyProject(project); err != nil {
		t.Fatalf("recovered Owner project does not verify: %v", err)
	}
	if journals, err := listOperationJournals(project); err != nil || len(journals) != 0 {
		t.Fatalf("Owner journals after recovery = %d, %v", len(journals), err)
	}
}

func mustProjectConfig(t *testing.T, project string) Config {
	t.Helper()
	config, _, _, err := loadProject(project)
	if err != nil {
		t.Fatal(err)
	}
	return config
}
