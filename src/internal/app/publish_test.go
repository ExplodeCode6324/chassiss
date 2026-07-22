package app

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPublishRemoteGitRecoversAfterRemotePush(t *testing.T) {
	fixture := setupApprovedSubmission(t, []string{"true"})
	state := mustProjectState(t, fixture.Project)
	if _, _, _, err := integrateSubmission(fixture.Project, fixture.Submission.ID, fixture.Reviewer, state.Revision); err != nil {
		t.Fatal(err)
	}
	integrated := mustProjectState(t, fixture.Project)
	remotePath := filepath.Join(t.TempDir(), "sync.git")
	if err := os.MkdirAll(remotePath, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := git(remotePath, "init", "--bare"); err != nil {
		t.Fatal(err)
	}
	if _, err := git(fixture.Project, "remote", "add", "sync", remotePath); err != nil {
		t.Fatal(err)
	}
	check, err := publishCheck(fixture.Project, "remote-git", "sync", "main")
	if err != nil {
		t.Fatal(err)
	}
	if check.Status != "ready" || check.LocalHead != integrated.Baseline || check.RemoteHead != "" {
		t.Fatalf("initial publish check = %#v", check)
	}
	publishOperationFaultHook = func(point string) error {
		if point == "remote_applied_before_phase" {
			return errors.New("injected crash after remote push")
		}
		return nil
	}
	_, _, _, publishErr := publishApply(fixture.Project, "remote-git", "sync", "main", fixture.Orchestrator, integrated.Revision)
	publishOperationFaultHook = nil
	if publishErr == nil {
		t.Fatal("injected publish unexpectedly completed")
	}
	beforeRecovery := mustProjectState(t, fixture.Project)
	if beforeRecovery.Revision != integrated.Revision || len(beforeRecovery.Publications) != 0 || beforeRecovery.Baseline != integrated.Baseline {
		t.Fatalf("remote push incorrectly changed local workflow state: %#v", beforeRecovery)
	}
	remoteHead, err := gitRemoteHead(fixture.Project, "sync", "main")
	if err != nil || remoteHead != integrated.Baseline {
		t.Fatalf("remote head = %q, %v; want %q", remoteHead, err, integrated.Baseline)
	}
	recovered, err := recoverProject(fixture.Project)
	if err != nil {
		t.Fatal(err)
	}
	if recovered.Revision != integrated.Revision+1 || recovered.Baseline != integrated.Baseline || len(recovered.Publications) != 1 {
		t.Fatalf("recovered publication state = %#v", recovered)
	}
	for _, publication := range recovered.Publications {
		if publication.Head != integrated.Baseline || publication.Target != "remote-git" || publication.Remote != "sync" || publication.PreviousRemoteHead != "" {
			t.Fatalf("publication evidence = %#v", publication)
		}
	}
	if _, err := recoverProject(fixture.Project); err != nil {
		t.Fatalf("repeated publish recovery is not idempotent: %v", err)
	}
	check, err = publishCheck(fixture.Project, "remote-git", "sync", "main")
	if err != nil || check.Status != "up_to_date" {
		t.Fatalf("post-publish check = %#v, %v", check, err)
	}
	if journals, err := listPublishOperationJournals(fixture.Project); err != nil || len(journals) != 0 {
		t.Fatalf("publish journals after recovery = %d, %v", len(journals), err)
	}
}

func TestPublishFailureDoesNotUndoLocalIntegration(t *testing.T) {
	fixture := setupApprovedSubmission(t, []string{"true"})
	state := mustProjectState(t, fixture.Project)
	if _, _, _, err := integrateSubmission(fixture.Project, fixture.Submission.ID, fixture.Reviewer, state.Revision); err != nil {
		t.Fatal(err)
	}
	integrated := mustProjectState(t, fixture.Project)
	missingRemote := filepath.Join(t.TempDir(), "missing", "remote.git")
	if _, err := git(fixture.Project, "remote", "add", "broken", missingRemote); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := publishApply(fixture.Project, "remote-git", "broken", "main", fixture.Orchestrator, integrated.Revision); err == nil {
		t.Fatal("publish to missing remote unexpectedly succeeded")
	}
	after := mustProjectState(t, fixture.Project)
	if after.Revision != integrated.Revision || after.Baseline != integrated.Baseline || after.Tasks[fixture.Submission.TaskID].Status != "integrated" || len(after.Publications) != 0 {
		t.Fatalf("failed publish altered local integration: %#v", after)
	}
}

func TestPublishRecoveryFinalizesStoredEventAfterRemoteAdvances(t *testing.T) {
	fixture := setupApprovedSubmission(t, []string{"true"})
	state := mustProjectState(t, fixture.Project)
	if _, _, _, err := integrateSubmission(fixture.Project, fixture.Submission.ID, fixture.Reviewer, state.Revision); err != nil {
		t.Fatal(err)
	}
	integrated := mustProjectState(t, fixture.Project)
	remotePath := filepath.Join(t.TempDir(), "sync.git")
	if err := os.MkdirAll(remotePath, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := git(remotePath, "init", "--bare"); err != nil {
		t.Fatal(err)
	}
	if _, err := git(fixture.Project, "remote", "add", "sync", remotePath); err != nil {
		t.Fatal(err)
	}
	publishOperationFaultHook = func(point string) error {
		if point == "state_committed" {
			return errors.New("injected crash after publication event")
		}
		return nil
	}
	t.Cleanup(func() { publishOperationFaultHook = nil })
	if _, _, _, err := publishApply(fixture.Project, "remote-git", "sync", "main", fixture.Orchestrator, integrated.Revision); err == nil {
		t.Fatal("injected publish unexpectedly completed")
	}
	publishOperationFaultHook = nil
	committed := mustProjectState(t, fixture.Project)
	if committed.Revision != integrated.Revision+1 || len(committed.Publications) != 1 {
		t.Fatalf("publication event was not committed before crash: %#v", committed)
	}
	tree, err := git(fixture.Project, "rev-parse", integrated.Baseline+"^{tree}")
	if err != nil {
		t.Fatal(err)
	}
	descendant, err := git(fixture.Project, "-c", "user.name=CHASSISS", "-c", "user.email=chassiss@local.invalid", "commit-tree", tree, "-p", integrated.Baseline, "-m", "remote follow-up")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := git(fixture.Project, "push", "sync", descendant+":refs/heads/main"); err != nil {
		t.Fatal(err)
	}
	if _, err := recoverProject(fixture.Project); err != nil {
		t.Fatalf("stored publication event could not finalize after remote advanced: %v", err)
	}
	if journals, err := listPublishOperationJournals(fixture.Project); err != nil || len(journals) != 0 {
		t.Fatalf("publish journals after finalization = %d, %v", len(journals), err)
	}
	after := mustProjectState(t, fixture.Project)
	if after.Revision != committed.Revision || after.Baseline != integrated.Baseline || len(after.Publications) != 1 {
		t.Fatalf("finalization rewrote local publication state: %#v", after)
	}
}

func TestPublishRecoveryRejectsRemoteEndpointChange(t *testing.T) {
	project, rootPath := setupAuthProject(t)
	credentialPath := filepath.Join(t.TempDir(), "orchestrator.yaml")
	if _, err := issueCredential(project, rootPath, "agent:orchestrator", "orchestrator", credentialPath, nil); err != nil {
		t.Fatal(err)
	}
	principal, err := loadPrincipal(project, credentialPath, "publish.apply")
	if err != nil {
		t.Fatal(err)
	}
	firstRemote := filepath.Join(t.TempDir(), "first.git")
	secondRemote := filepath.Join(t.TempDir(), "second.git")
	for _, remote := range []string{firstRemote, secondRemote} {
		if err := os.MkdirAll(remote, 0o755); err != nil {
			t.Fatal(err)
		}
		if _, err := git(remote, "init", "--bare"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := git(project, "remote", "add", "sync", firstRemote); err != nil {
		t.Fatal(err)
	}
	state := mustProjectState(t, project)
	publishOperationFaultHook = func(point string) error {
		if point == "remote_applied_before_phase" {
			return errors.New("injected crash after remote push")
		}
		return nil
	}
	t.Cleanup(func() { publishOperationFaultHook = nil })
	if _, _, _, err := publishApply(project, "remote-git", "sync", "main", principal, state.Revision); err == nil {
		t.Fatal("injected publish unexpectedly completed")
	}
	publishOperationFaultHook = nil
	if _, err := git(project, "remote", "set-url", "sync", secondRemote); err != nil {
		t.Fatal(err)
	}
	if _, err := recoverProject(project); err == nil || !strings.Contains(err.Error(), "endpoint changed") {
		t.Fatalf("recovery accepted a changed remote endpoint: %v", err)
	}
	if after := mustProjectState(t, project); after.Revision != state.Revision || len(after.Publications) != 0 {
		t.Fatalf("blocked endpoint recovery altered local state: %#v", after)
	}
}

func TestPublishCheckRejectsExternalHelperURL(t *testing.T) {
	project, _ := setupAuthProject(t)
	if _, err := git(project, "remote", "add", "unsafe", "ext::sh -c echo-danger"); err != nil {
		t.Fatal(err)
	}
	if _, err := publishCheck(project, "remote-git", "unsafe", "main"); err == nil {
		t.Fatal("external-helper Git remote was accepted")
	} else if typed, ok := err.(*CLIError); !ok || typed.Code != "CHS-PUBLISH-REMOTE-UNSAFE" {
		t.Fatalf("unsafe remote error = %#v", err)
	}
}
