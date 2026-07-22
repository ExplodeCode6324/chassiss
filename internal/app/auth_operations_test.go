package app

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestAuthIssueJournalRecoversEveryDurablePhase(t *testing.T) {
	points := []struct {
		Name      string
		Published bool
	}{
		{"prepared", false},
		{"credential_prepared", false},
		{"trust_committed_before_phase", true},
		{"trust_committed", true},
		{"credential_published_before_phase", true},
		{"credential_published", true},
	}
	for _, point := range points {
		t.Run(point.Name, func(t *testing.T) {
			project, rootPath := setupAuthProject(t)
			output := filepath.Join(t.TempDir(), "developer.yaml")
			authOperationFaultHook = func(current string) error {
				if current == point.Name {
					return errors.New("injected auth crash at " + current)
				}
				return nil
			}
			_, err := issueCredential(project, rootPath, "agent:developer", "developer", output, nil)
			authOperationFaultHook = nil
			if err == nil {
				t.Fatal("injected authorization operation unexpectedly completed")
			}
			journals, err := listAuthOperationJournals(project)
			if err != nil || len(journals) != 1 {
				t.Fatalf("auth journals = %d, %v; want one", len(journals), err)
			}
			credentialID := journals[0].CredentialID
			if _, err := recoverProject(project); err != nil {
				t.Fatal(err)
			}
			if _, err := recoverProject(project); err != nil {
				t.Fatalf("repeated auth recovery is not idempotent: %v", err)
			}
			_, trust, _, err := loadProject(project)
			if err != nil {
				t.Fatal(err)
			}
			_, outputErr := os.Stat(output)
			grantExists := false
			for _, grant := range trust.Grants {
				grantExists = grantExists || grant.ID == credentialID
			}
			if point.Published {
				if outputErr != nil || !grantExists {
					t.Fatalf("committed issue was not completed: output=%v grant=%v", outputErr, grantExists)
				}
				if _, err := loadPrincipal(project, output, "work.open"); err != nil {
					t.Fatalf("recovered credential is unusable: %v", err)
				}
			} else if !os.IsNotExist(outputErr) || grantExists {
				t.Fatalf("uncommitted issue was not cancelled: output=%v grant=%v", outputErr, grantExists)
			}
			if journals, err := listAuthOperationJournals(project); err != nil || len(journals) != 0 {
				t.Fatalf("auth journals after recovery = %d, %v", len(journals), err)
			}
		})
	}
}

func TestAuthRevokeJournalAndIdempotency(t *testing.T) {
	for _, point := range []struct {
		Name    string
		Revoked bool
	}{{"prepared", false}, {"trust_committed_before_phase", true}, {"trust_committed", true}} {
		t.Run(point.Name, func(t *testing.T) {
			project, rootPath := setupAuthProject(t)
			output := filepath.Join(t.TempDir(), "developer.yaml")
			credential, err := issueCredential(project, rootPath, "agent:developer", "developer", output, nil)
			if err != nil {
				t.Fatal(err)
			}
			authOperationFaultHook = func(current string) error {
				if current == point.Name {
					return errors.New("injected revoke crash at " + current)
				}
				return nil
			}
			err = revokeCredential(project, rootPath, credential.ID, "test")
			authOperationFaultHook = nil
			if err == nil {
				t.Fatal("injected revoke unexpectedly completed")
			}
			if _, err := recoverProject(project); err != nil {
				t.Fatal(err)
			}
			_, trust, _, err := loadProject(project)
			if err != nil {
				t.Fatal(err)
			}
			revoked := trustHasRevocation(trust, credential.ID)
			if revoked != point.Revoked {
				t.Fatalf("recovered revoked=%v, want %v", revoked, point.Revoked)
			}
			if point.Revoked {
				revision := trust.Revision
				if err := revokeCredential(project, rootPath, credential.ID, "duplicate"); err != nil {
					t.Fatal(err)
				}
				_, repeated, _, _ := loadProject(project)
				if repeated.Revision != revision {
					t.Fatalf("idempotent revoke advanced trust revision from %d to %d", revision, repeated.Revision)
				}
			}
		})
	}
}

func TestAuthTrustRevisionCASRejectsStaleUpdate(t *testing.T) {
	project, rootPath := setupAuthProject(t)
	_, trust, _, err := loadProject(project)
	if err != nil {
		t.Fatal(err)
	}
	firstOutput := filepath.Join(t.TempDir(), "first.yaml")
	if _, err := issueCredentialExpected(project, rootPath, "agent:first", "developer", firstOutput, nil, trust.Revision); err != nil {
		t.Fatal(err)
	}
	secondOutput := filepath.Join(t.TempDir(), "second.yaml")
	if _, err := issueCredentialExpected(project, rootPath, "agent:second", "developer", secondOutput, nil, trust.Revision); err == nil {
		t.Fatal("stale trust revision was accepted")
	} else if typed, ok := err.(*CLIError); !ok || typed.Code != "CHS-CONFLICT-TRUST-REVISION" {
		t.Fatalf("stale trust error = %#v", err)
	}
	if _, err := os.Stat(secondOutput); !os.IsNotExist(err) {
		t.Fatalf("stale issue left a credential file: %v", err)
	}
}

func TestConcurrentAuthIssueDoesNotLoseGrant(t *testing.T) {
	project, rootPath := setupAuthProject(t)
	type request struct {
		Actor  string
		Output string
	}
	requests := []request{
		{Actor: "agent:one", Output: filepath.Join(t.TempDir(), "one.yaml")},
		{Actor: "agent:two", Output: filepath.Join(t.TempDir(), "two.yaml")},
	}
	errorsByIndex := make([]error, len(requests))
	start := make(chan struct{})
	var wait sync.WaitGroup
	for index := range requests {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			<-start
			_, errorsByIndex[index] = issueCredential(project, rootPath, requests[index].Actor, "developer", requests[index].Output, nil)
		}(index)
	}
	close(start)
	wait.Wait()
	for index, err := range errorsByIndex {
		if err == nil {
			continue
		}
		typed, ok := err.(*CLIError)
		if !ok || typed.Code != "CHS-CONFLICT-LOCKED" {
			t.Fatalf("concurrent issue error = %#v", err)
		}
		if _, retryErr := issueCredential(project, rootPath, requests[index].Actor, "developer", requests[index].Output, nil); retryErr != nil {
			t.Fatalf("retry after serialized conflict: %v", retryErr)
		}
	}
	_, trust, _, err := loadProject(project)
	if err != nil {
		t.Fatal(err)
	}
	for _, request := range requests {
		if _, ok := activeDeveloperGrant(trust, request.Actor, timeNow()); !ok {
			t.Fatalf("concurrent issue lost grant for %s", request.Actor)
		}
		if _, err := os.Stat(request.Output); err != nil {
			t.Fatalf("credential missing for %s: %v", request.Actor, err)
		}
	}
}

func TestConcurrentAuthIssueAndRevokeConvergeDeterministically(t *testing.T) {
	project, rootPath := setupAuthProject(t)
	oldOutput := filepath.Join(t.TempDir(), "old.yaml")
	oldCredential, err := issueCredential(project, rootPath, "agent:old", "developer", oldOutput, nil)
	if err != nil {
		t.Fatal(err)
	}
	newOutput := filepath.Join(t.TempDir(), "new.yaml")
	start := make(chan struct{})
	results := make(chan error, 2)
	go func() {
		<-start
		_, issueErr := issueCredential(project, rootPath, "agent:new", "developer", newOutput, nil)
		results <- issueErr
	}()
	go func() {
		<-start
		results <- revokeCredential(project, rootPath, oldCredential.ID, "replace")
	}()
	close(start)
	issueDone, revokeDone := false, false
	for index := 0; index < 2; index++ {
		err := <-results
		if err == nil {
			continue
		}
		typed, ok := err.(*CLIError)
		if !ok || typed.Code != "CHS-CONFLICT-LOCKED" {
			t.Fatalf("concurrent issue/revoke error = %#v", err)
		}
	}
	_, trust, _, err := loadProject(project)
	if err != nil {
		t.Fatal(err)
	}
	_, issueDone = activeDeveloperGrant(trust, "agent:new", timeNow())
	revokeDone = trustHasRevocation(trust, oldCredential.ID)
	if !issueDone {
		if _, err := issueCredential(project, rootPath, "agent:new", "developer", newOutput, nil); err != nil {
			t.Fatal(err)
		}
	}
	if !revokeDone {
		if err := revokeCredential(project, rootPath, oldCredential.ID, "replace"); err != nil {
			t.Fatal(err)
		}
	}
	_, trust, _, err = loadProject(project)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := activeDeveloperGrant(trust, "agent:new", timeNow()); !ok || !trustHasRevocation(trust, oldCredential.ID) {
		t.Fatalf("issue/revoke did not converge: trust=%#v", trust)
	}
}

func setupAuthProject(t *testing.T) (project, rootPath string) {
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
	return project, rootPath
}

func trustHasRevocation(trust Trust, credentialID string) bool {
	for _, revocation := range trust.Revocations {
		if revocation.CredentialID == credentialID {
			return true
		}
	}
	return false
}
