package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ExplodeCode6324/chassiss/internal/cryptoutil"
	"github.com/ExplodeCode6324/chassiss/internal/localstate"
	"github.com/ExplodeCode6324/chassiss/internal/protocol"
	"github.com/ExplodeCode6324/chassiss/internal/state"
)

func TestLocalPublishCASFailureMarksExcludedCandidateFailed(t *testing.T) {
	ctx, original, root := newLocalPublishProject(t, "PRJ-LOCAL-CAS")
	winner := newRootGrantPlan(
		t, ctx, original, root,
		"GRT-LOCAL-CAS-WINNER", "KEY-LOCAL-CAS-WINNER",
		"OPR-31ARZ3NDEKTSV4RRFFQ69G5FAV",
	)
	loser := newRootGrantPlan(
		t, ctx, original, root,
		"GRT-LOCAL-CAS-LOSER", "KEY-LOCAL-CAS-LOSER",
		"OPR-41ARZ3NDEKTSV4RRFFQ69G5FAV",
	)
	if _, err := publishTransition(ctx, original, winner); err != nil {
		t.Fatal(err)
	}
	if _, err := publishTransition(ctx, original, loser); err == nil {
		t.Fatal("stale local publication unexpectedly succeeded")
	} else {
		var failure *protocol.Error
		if !errors.As(err, &failure) || failure.Code != protocol.ErrCASRetryExhausted {
			t.Fatalf("unexpected CAS failure: %v", err)
		}
		if failure.Details["pending_status"] != string(pendingFailed) {
			t.Fatalf("candidate was not deterministically failed: %#v", failure.Details)
		}
	}

	current, err := loadProject(ctx, "", false)
	if err != nil {
		t.Fatal(err)
	}
	pending := current.LocalProject.PendingOperations[loser.Operation.OperationID]
	if pending.Status != string(pendingFailed) {
		t.Fatalf("losing pending Operation remains unresolved: %#v", pending)
	}
	if err := requireNoUnresolvedPending(current); err != nil {
		t.Fatalf("failed pending Operation blocked a subsequent legal action: %v", err)
	}

	root, err = selectRoot(current, "KEY-ROOT-01")
	if err != nil {
		t.Fatal(err)
	}
	next := newRootGrantPlan(
		t, ctx, current, root,
		"GRT-LOCAL-CAS-NEXT", "KEY-LOCAL-CAS-NEXT",
		"OPR-51ARZ3NDEKTSV4RRFFQ69G5FAV",
	)
	if _, err := publishTransition(ctx, current, next); err != nil {
		t.Fatalf("subsequent legal publication failed: %v", err)
	}
}

func TestLocalPublishCASReconciliationUsesPublishedEvidence(t *testing.T) {
	ctx, original, root := newLocalPublishProject(t, "PRJ-LOCAL-CAS-EVIDENCE")
	loser := newRootGrantPlan(
		t, ctx, original, root,
		"GRT-LOCAL-CAS-EVIDENCE", "KEY-LOCAL-CAS-EVIDENCE",
		"OPR-A1ARZ3NDEKTSV4RRFFQ69G5FAV",
	)
	winner := loser
	winner.Tree = cloneTreeMap(loser.Tree)
	winner.Evidence.Attempt = 2
	published, err := publishTransition(ctx, original, winner)
	if err != nil {
		t.Fatal(err)
	}
	if published.Operation == nil {
		t.Fatal("published Transition has no Operation metadata")
	}

	reconciled, err := publishTransition(ctx, original, loser)
	if err != nil {
		t.Fatalf("same-Operation CAS reconciliation failed: %v", err)
	}
	if reconciled.Operation == nil {
		t.Fatal("reconciled Transition has no Operation metadata")
	}
	if reconciled.Operation.Commit != published.Operation.Commit {
		t.Fatalf(
			"reconciled commit mismatch: got %s want %s",
			reconciled.Operation.Commit, published.Operation.Commit,
		)
	}
	commit, err := original.Runner.ReadCommit(ctx, published.Operation.Commit)
	if err != nil {
		t.Fatal(err)
	}
	message, err := protocol.ParseTransitionMessage(commit.Message, original.Verified.ObjectFormat)
	if err != nil {
		t.Fatal(err)
	}
	operationDigest, err := protocol.ObjectDigest("operation", message.Operation)
	if err != nil {
		t.Fatal(err)
	}
	evidenceDigest, err := protocol.ObjectDigest("execution-evidence", message.Evidence)
	if err != nil {
		t.Fatal(err)
	}
	if reconciled.Operation.EvidenceAttempt != message.Evidence.Attempt ||
		reconciled.Operation.EvidenceDigest != evidenceDigest ||
		reconciled.Operation.OperationDigest != operationDigest {
		t.Fatalf(
			"reconciled metadata does not match published commit:\nmetadata %#v\nmessage %#v",
			reconciled.Operation, message,
		)
	}
	if reconciled.Operation.EvidenceAttempt != 2 {
		t.Fatalf(
			"reconciled envelope reused loser Evidence attempt: %#v",
			reconciled.Operation,
		)
	}
}

func TestFinalizePendingKeepsReplacementAndMissingRecordStillReconciles(t *testing.T) {
	ctx, original, root := newLocalPublishProject(t, "PRJ-LOCAL-CAS-SAME-ID")
	loser := newRootGrantPlan(
		t, ctx, original, root,
		"GRT-LOCAL-CAS-SAME-ID", "KEY-LOCAL-CAS-SAME-ID",
		"OPR-D1ARZ3NDEKTSV4RRFFQ69G5FAV",
	)
	winner := loser
	winner.Tree = cloneTreeMap(loser.Tree)
	winner.Evidence.Attempt = 2
	published, err := publishTransition(ctx, original, winner)
	if err != nil {
		t.Fatal(err)
	}
	if published.Operation == nil {
		t.Fatal("published Transition has no Operation metadata")
	}
	if _, err := createProposal(ctx, original, loser, ""); err != nil {
		t.Fatal(err)
	}
	local, err := original.Store.Load()
	if err != nil {
		t.Fatal(err)
	}
	replacement := local.Projects[original.Verified.State.Project.ID].
		PendingOperations[loser.Operation.OperationID]
	winnerPending := replacement
	winnerPending.CandidateCommit = stringPointer(published.Operation.Commit)
	if err := finalizePending(ctx, original, winnerPending, mustLoadCurrentProject(t, ctx).Verified); err != nil {
		t.Fatal(err)
	}
	local, err = original.Store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if current, exists := local.Projects[original.Verified.State.Project.ID].
		PendingOperations[loser.Operation.OperationID]; !exists ||
		!samePendingAttempt(current, replacement) {
		t.Fatalf("winner finalize deleted a replacement same-ID pending: %#v", current)
	}

	if err := original.Store.Update(func(local *localstate.State) error {
		project := local.Projects[original.Verified.State.Project.ID]
		delete(project.PendingOperations, loser.Operation.OperationID)
		local.Projects[original.Verified.State.Project.ID] = project
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	reconciliation, err := reconcileLocalCASFailure(ctx, original, replacement)
	if err != nil {
		t.Fatal(err)
	}
	if reconciliation.Disposition != pendingPublished ||
		reconciliation.Commit != published.Operation.Commit ||
		reconciliation.Message.Evidence.Attempt != 2 {
		t.Fatalf("missing registry entry hid the verified same-ID publication: %#v", reconciliation)
	}
}

func TestStaleCheckpointWriterCannotMoveMinimumBackward(t *testing.T) {
	ctx, original, root := newLocalPublishProject(t, "PRJ-CHECKPOINT-MONOTONIC")
	winner := newRootGrantPlan(
		t, ctx, original, root,
		"GRT-CHECKPOINT-WINNER", "KEY-CHECKPOINT-WINNER",
		"OPR-E1ARZ3NDEKTSV4RRFFQ69G5FAV",
	)
	if _, err := publishTransition(ctx, original, winner); err != nil {
		t.Fatal(err)
	}
	current := mustLoadCurrentProject(t, ctx)
	checkpoint := current.LocalProject.MinimumCheckpoint

	if _, err := reconcileAndCheckpointPending(ctx, original, original.Verified); err == nil {
		t.Fatal("stale sync unexpectedly moved the minimum checkpoint backward")
	} else {
		var failure *protocol.Error
		if !errors.As(err, &failure) || failure.Code != protocol.ErrMainlineRollback {
			t.Fatalf("unexpected stale checkpoint error: %v", err)
		}
	}
	reloaded := mustLoadCurrentProject(t, ctx)
	if reloaded.LocalProject.MinimumCheckpoint != checkpoint {
		t.Fatalf(
			"minimum checkpoint regressed: got %#v want %#v",
			reloaded.LocalProject.MinimumCheckpoint, checkpoint,
		)
	}
}

func TestLocalCASReconciliationPreservesProtocolFailure(t *testing.T) {
	ctx, original, root := newLocalPublishProject(t, "PRJ-LOCAL-CAS-TRUST")
	plan := newRootGrantPlan(
		t, ctx, original, root,
		"GRT-LOCAL-CAS-TRUST", "KEY-LOCAL-CAS-TRUST",
		"OPR-F1ARZ3NDEKTSV4RRFFQ69G5FAV",
	)
	parent, err := original.Runner.ReadCommit(ctx, original.Verified.Head)
	if err != nil {
		t.Fatal(err)
	}
	result, err := original.Runner.RunWithInput(
		ctx,
		[]byte("ordinary unsigned commit\n"),
		map[string]string{
			"GIT_AUTHOR_NAME":     "test",
			"GIT_AUTHOR_EMAIL":    "test@local.invalid",
			"GIT_COMMITTER_NAME":  "test",
			"GIT_COMMITTER_EMAIL": "test@local.invalid",
		},
		"commit-tree", parent.Tree, "-p", original.Verified.Head, "-F", "-",
	)
	if err != nil {
		t.Fatal(err)
	}
	invalid := strings.TrimSpace(string(result.Stdout))
	if err := original.Runner.UpdateRefCAS(
		ctx, "refs/heads/main", invalid, original.Verified.Head, "test invalid main",
	); err != nil {
		t.Fatal(err)
	}
	if _, err := original.Runner.Run(ctx, "read-tree", "--reset", "-u", invalid); err != nil {
		t.Fatal(err)
	}

	if _, err := publishTransition(ctx, original, plan); err == nil {
		t.Fatal("publication over an invalid current main unexpectedly succeeded")
	} else {
		var failure *protocol.Error
		if !errors.As(err, &failure) {
			t.Fatalf("protocol failure was replaced by a generic error: %v", err)
		}
		if failure.Code == protocol.ErrCASRetryExhausted ||
			(failure.Category != protocol.CategoryProtocol &&
				failure.Category != protocol.CategoryTrust) {
			t.Fatalf("safety failure was masked as CAS conflict: %#v", failure)
		}
		if failure.OperationID != plan.Operation.OperationID ||
			failure.Details["pending_status"] != string(pendingUnresolved) {
			t.Fatalf("safety failure lacks reconciliation context: %#v", failure)
		}
	}
	local, err := original.Store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if pending := local.Projects[original.Verified.State.Project.ID].
		PendingOperations[plan.Operation.OperationID]; pending.Status != "signed" {
		t.Fatalf("safety failure destructively changed pending: %#v", pending)
	}
}

func TestLocalProposalCASRecognizesExactConcurrentPublication(t *testing.T) {
	ctx, original, root := newLocalPublishProject(t, "PRJ-LOCAL-PROPOSAL-CAS")
	plan := newRootGrantPlan(
		t, ctx, original, root,
		"GRT-LOCAL-PROPOSAL-CAS", "KEY-LOCAL-PROPOSAL-CAS",
		"OPR-G1ARZ3NDEKTSV4RRFFQ69G5FAV",
	)
	proposal, err := createProposal(ctx, original, plan, "")
	if err != nil {
		t.Fatal(err)
	}
	if proposal.Operation == nil {
		t.Fatal("proposal has no candidate commit")
	}
	commit := proposal.Operation.Commit
	candidate, err := original.Runner.ReadCommit(ctx, commit)
	if err != nil {
		t.Fatal(err)
	}
	message, err := protocol.ParseTransitionMessage(candidate.Message, original.Verified.ObjectFormat)
	if err != nil {
		t.Fatal(err)
	}
	expected, err := proposalPending(ctx, original, commit, candidate, message)
	if err != nil {
		t.Fatal(err)
	}
	if err := original.Runner.UpdateRefCAS(
		ctx, "refs/heads/main", commit, original.Verified.Head,
		"test concurrent exact proposal publication",
	); err != nil {
		t.Fatal(err)
	}
	if _, err := original.Runner.Run(ctx, "read-tree", "--reset", "-u", commit); err != nil {
		t.Fatal(err)
	}

	verified, publishedHead, err := reconcileLocalProposalCAS(
		ctx, original, expected, commit, errors.New("simulated stale CAS"),
	)
	if err != nil {
		t.Fatal(err)
	}
	if verified.Head != commit || publishedHead != commit {
		t.Fatalf(
			"exact local proposal was not reconciled: verified=%s target=%s proposal=%s",
			verified.Head, publishedHead, commit,
		)
	}
}

func TestRemoteProposalReconciliationAdvancesLocalMainToDescendant(t *testing.T) {
	if goruntime.GOOS == "windows" {
		t.Skip("Git for Windows daemon receive-pack is not a reliable loopback test transport")
	}
	ctx, original, root := newRemotePublishProject(t, "PRJ-REMOTE-PROPOSAL-DESC")
	proposalPlan := newRootGrantPlan(
		t, ctx, original, root,
		"GRT-REMOTE-PROPOSAL", "KEY-REMOTE-PROPOSAL",
		"OPR-H1ARZ3NDEKTSV4RRFFQ69G5FAV",
	)
	proposal, err := createProposal(ctx, original, proposalPlan, "")
	if err != nil {
		t.Fatal(err)
	}
	if proposal.Operation == nil {
		t.Fatal("proposal has no candidate commit")
	}
	proposalCommit := proposal.Operation.Commit
	if _, err := original.Runner.Run(
		ctx, "push", "--porcelain", "--atomic",
		"--force-with-lease=refs/heads/main:"+original.Verified.Head,
		"origin", proposalCommit+":refs/heads/main",
	); err != nil {
		t.Fatal(err)
	}

	atProposal, err := loadProject(ctx, proposalCommit, false)
	if err != nil {
		t.Fatal(err)
	}
	root, err = selectRoot(atProposal, "KEY-ROOT-01")
	if err != nil {
		t.Fatal(err)
	}
	descendantPlan := newRootGrantPlan(
		t, ctx, atProposal, root,
		"GRT-REMOTE-DESCENDANT", "KEY-REMOTE-DESCENDANT",
		"OPR-J1ARZ3NDEKTSV4RRFFQ69G5FAV",
	)
	descendant, err := createProposal(ctx, atProposal, descendantPlan, "")
	if err != nil {
		t.Fatal(err)
	}
	if descendant.Operation == nil {
		t.Fatal("descendant proposal has no candidate commit")
	}
	descendantCommit := descendant.Operation.Commit
	if _, err := original.Runner.Run(
		ctx, "push", "--porcelain", "--atomic",
		"--force-with-lease=refs/heads/main:"+proposalCommit,
		"origin", descendantCommit+":refs/heads/main",
	); err != nil {
		t.Fatal(err)
	}

	ref := "refs/heads/chassiss/transition/" +
		proposalPlan.Operation.Action + "/" + proposalPlan.Operation.OperationID
	var stdout, stderr bytes.Buffer
	exit := Run(
		[]string{"transition", "publish", ref, "--json"},
		bytes.NewReader(nil), &stdout, &stderr,
	)
	if exit != 0 {
		t.Fatalf("proposal reconciliation failed: %s %s", stdout.String(), stderr.String())
	}
	var envelope Envelope
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	result := envelope.Result.(map[string]any)
	if result["commit"] != proposalCommit {
		t.Fatalf("result lost exact proposal commit: %#v", envelope.Result)
	}
	current := mustLoadCurrentProject(t, ctx)
	if current.Verified.Head != descendantCommit ||
		current.LocalProject.MinimumCheckpoint.Commit != descendantCommit {
		t.Fatalf(
			"local main/checkpoint stopped below verified remote descendant: head=%s checkpoint=%s want=%s",
			current.Verified.Head,
			current.LocalProject.MinimumCheckpoint.Commit,
			descendantCommit,
		)
	}
}

func TestLocalSyncReconcilesLegacyStaleSignedPending(t *testing.T) {
	ctx, original, root := newLocalPublishProject(t, "PRJ-LOCAL-SYNC-STALE")
	stale := newRootGrantPlan(
		t, ctx, original, root,
		"GRT-LOCAL-SYNC-STALE", "KEY-LOCAL-SYNC-STALE",
		"OPR-61ARZ3NDEKTSV4RRFFQ69G5FAV",
	)
	if _, err := createProposal(ctx, original, stale, ""); err != nil {
		t.Fatal(err)
	}
	winner := newRootGrantPlan(
		t, ctx, original, root,
		"GRT-LOCAL-SYNC-WINNER", "KEY-LOCAL-SYNC-WINNER",
		"OPR-71ARZ3NDEKTSV4RRFFQ69G5FAV",
	)
	if _, err := publishTransition(ctx, original, winner); err != nil {
		t.Fatal(err)
	}

	envelope := runSyncJSON(t)
	reconciliation := resultObject(t, envelope.Result, "pending_reconciliation")
	if !resultListContains(reconciliation["failed"], stale.Operation.OperationID) {
		t.Fatalf("sync did not report the stale pending Operation as failed: %#v", reconciliation)
	}
	current, err := loadProject(ctx, "", false)
	if err != nil {
		t.Fatal(err)
	}
	if pending := current.LocalProject.PendingOperations[stale.Operation.OperationID]; pending.Status != string(pendingFailed) {
		t.Fatalf("sync left stale pending unresolved: %#v", pending)
	}
	if err := requireNoUnresolvedPending(current); err != nil {
		t.Fatalf("sync recovery did not unblock later legal actions: %v", err)
	}
}

func TestLocalSyncReconcilesPublishedCandidateWithoutFailingIt(t *testing.T) {
	ctx, original, root := newLocalPublishProject(t, "PRJ-LOCAL-SYNC-PUBLISHED")
	plan := newRootGrantPlan(
		t, ctx, original, root,
		"GRT-LOCAL-SYNC-PUBLISHED", "KEY-LOCAL-SYNC-PUBLISHED",
		"OPR-81ARZ3NDEKTSV4RRFFQ69G5FAV",
	)
	proposal, err := createProposal(ctx, original, plan, "")
	if err != nil {
		t.Fatal(err)
	}
	if proposal.Operation == nil {
		t.Fatal("proposal has no candidate commit")
	}
	candidate := proposal.Operation.Commit
	if err := original.Runner.UpdateRefCAS(
		ctx, "refs/heads/main", candidate, original.Verified.Head,
		"test interrupted successful publication",
	); err != nil {
		t.Fatal(err)
	}
	if _, err := original.Runner.Run(ctx, "read-tree", "--reset", "-u", candidate); err != nil {
		t.Fatal(err)
	}

	envelope := runSyncJSON(t)
	reconciliation := resultObject(t, envelope.Result, "pending_reconciliation")
	if !resultListContains(reconciliation["published"], plan.Operation.OperationID) ||
		resultListContains(reconciliation["failed"], plan.Operation.OperationID) {
		t.Fatalf("published candidate was misclassified: %#v", reconciliation)
	}
	current, err := loadProject(ctx, "", false)
	if err != nil {
		t.Fatal(err)
	}
	if current.Verified.Head != candidate {
		t.Fatalf("sync changed the successfully published head: got %s want %s", current.Verified.Head, candidate)
	}
	if _, exists := current.Verified.State.Authority.Grants[plan.Operation.Target]; !exists {
		t.Fatal("successfully published semantic operation was lost")
	}
	if _, exists := current.LocalProject.PendingOperations[plan.Operation.OperationID]; exists {
		t.Fatal("published pending Operation was not reconciled")
	}
}

func TestLocalSyncLeavesStillPublishableCandidateUnresolved(t *testing.T) {
	ctx, original, root := newLocalPublishProject(t, "PRJ-LOCAL-SYNC-AMBIGUOUS")
	plan := newRootGrantPlan(
		t, ctx, original, root,
		"GRT-LOCAL-SYNC-AMBIGUOUS", "KEY-LOCAL-SYNC-AMBIGUOUS",
		"OPR-91ARZ3NDEKTSV4RRFFQ69G5FAV",
	)
	if _, err := createProposal(ctx, original, plan, ""); err != nil {
		t.Fatal(err)
	}

	envelope := runSyncJSON(t)
	reconciliation := resultObject(t, envelope.Result, "pending_reconciliation")
	if !resultListContains(reconciliation["unresolved"], plan.Operation.OperationID) {
		t.Fatalf("still-publishable candidate was not kept unresolved: %#v", reconciliation)
	}
	current, err := loadProject(ctx, "", false)
	if err != nil {
		t.Fatal(err)
	}
	if pending := current.LocalProject.PendingOperations[plan.Operation.OperationID]; pending.Status != "signed" {
		t.Fatalf("ambiguous candidate was destructively reclassified: %#v", pending)
	}
}

func newLocalPublishProject(
	t *testing.T,
	projectID string,
) (context.Context, *projectContext, selectedAuthority) {
	t.Helper()
	workspace := t.TempDir()
	dataDirectory := filepath.Join(t.TempDir(), "local-data")
	t.Setenv("CHASSISS_DATA_DIR", dataDirectory)
	if _, err := cryptoutil.GenerateFileKey(filepath.Join(dataDirectory, "keys"), "KEY-ROOT-01"); err != nil {
		t.Fatal(err)
	}
	architecture, err := filepath.Abs(filepath.Join("..", "..", "docs", "templates", "architecture.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	taskbook, err := filepath.Abs(filepath.Join("..", "..", "docs", "templates", "taskbook.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "README.md"), []byte("# test\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(workspace, "go.mod"),
		[]byte("module example.test/project\n\ngo 1.24\n"),
		0o644,
	); err != nil {
		t.Fatal(err)
	}
	previous, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(workspace); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(previous); err != nil {
			t.Errorf("restore working directory: %v", err)
		}
	})

	var stdout, stderr bytes.Buffer
	if exit := Run([]string{
		"init", "--project", projectID,
		"--architecture", architecture, "--taskbook", taskbook,
		"--root-key", "KEY-ROOT-01", "--json",
	}, bytes.NewReader(nil), &stdout, &stderr); exit != 0 {
		t.Fatalf("init failed: %s %s", stdout.String(), stderr.String())
	}
	ctx := context.Background()
	project, err := loadProject(ctx, "", false)
	if err != nil {
		t.Fatal(err)
	}
	root, err := selectRoot(project, "KEY-ROOT-01")
	if err != nil {
		t.Fatal(err)
	}
	return ctx, project, root
}

func newRemotePublishProject(
	t *testing.T,
	projectID string,
) (context.Context, *projectContext, selectedAuthority) {
	t.Helper()
	workspace := t.TempDir()
	dataDirectory := filepath.Join(t.TempDir(), "local-data")
	remote := filepath.Join(t.TempDir(), "remote.git")
	t.Setenv("CHASSISS_DATA_DIR", dataDirectory)
	if output, err := exec.Command("git", "init", "--bare", remote).CombinedOutput(); err != nil {
		t.Fatalf("bare remote init: %v\n%s", err, output)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	daemon := exec.Command(
		"git", "daemon", "--reuseaddr", "--export-all", "--enable=receive-pack",
		"--base-path="+filepath.Dir(remote), "--listen=127.0.0.1",
		"--port="+strconv.Itoa(port),
	)
	if err := daemon.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = daemon.Process.Kill()
		_ = daemon.Wait()
	})
	deadline := time.Now().Add(3 * time.Second)
	for {
		connection, dialErr := net.DialTimeout(
			"tcp", "127.0.0.1:"+strconv.Itoa(port), 100*time.Millisecond,
		)
		if dialErr == nil {
			_ = connection.Close()
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("git daemon did not start: %v", dialErr)
		}
		time.Sleep(20 * time.Millisecond)
	}
	remoteURL := "git://127.0.0.1:" + strconv.Itoa(port) + "/" + filepath.Base(remote)
	if _, err := cryptoutil.GenerateFileKey(
		filepath.Join(dataDirectory, "keys"), "KEY-ROOT-01",
	); err != nil {
		t.Fatal(err)
	}
	architecture, err := filepath.Abs(filepath.Join("..", "..", "docs", "templates", "architecture.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	taskbook, err := filepath.Abs(filepath.Join("..", "..", "docs", "templates", "taskbook.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "README.md"), []byte("# test\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(workspace, "go.mod"),
		[]byte("module example.test/project\n\ngo 1.24\n"),
		0o644,
	); err != nil {
		t.Fatal(err)
	}
	previous, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(workspace); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(previous); err != nil {
			t.Errorf("restore working directory: %v", err)
		}
	})
	var stdout, stderr bytes.Buffer
	if exit := Run([]string{
		"init", "--project", projectID,
		"--architecture", architecture, "--taskbook", taskbook,
		"--root-key", "KEY-ROOT-01", "--remote", remoteURL, "--json",
	}, bytes.NewReader(nil), &stdout, &stderr); exit != 0 {
		t.Fatalf("init failed: %s %s", stdout.String(), stderr.String())
	}
	ctx := context.Background()
	project, err := loadProject(ctx, "", false)
	if err != nil {
		t.Fatal(err)
	}
	root, err := selectRoot(project, "KEY-ROOT-01")
	if err != nil {
		t.Fatal(err)
	}
	return ctx, project, root
}

func newRootGrantPlan(
	t *testing.T,
	ctx context.Context,
	project *projectContext,
	root selectedAuthority,
	grantID string,
	keyID string,
	operationID string,
) transitionPlan {
	t.Helper()
	generated, err := cryptoutil.GenerateFileKey(filepath.Join(project.Store.Paths.Data, "keys"), keyID)
	if err != nil {
		t.Fatal(err)
	}
	maxActive := int64(1)
	grant := state.Grant{
		Actor: "local-cas-agent", Capabilities: []string{"task.start"},
		KeyID: keyID, PublicKey: generated.PublicKey,
		Limits: state.Limits{Mode: "bounded", MaxActiveTasks: &maxActive},
		Scope:  state.Scope{Tasks: []string{"*"}, Resources: []string{"*"}},
	}
	grantObject, err := objectMap(grant)
	if err != nil {
		t.Fatal(err)
	}
	operation := protocol.Operation{
		Schema: protocol.OperationSchema, OperationID: operationID,
		Action: "authority.grant-added", Project: project.Verified.State.Project.ID,
		Authority: root.Reference, Target: grantID,
		Preconditions: map[string]any{
			"grant_absent": true,
			"root_key_id":  project.Verified.State.Authority.Root.KeyID,
		},
		Payload: map[string]any{
			"grant": grantObject, "grant_id": grantID, "request_digest": nil,
		},
	}
	digest, err := protocol.ObjectDigest("operation", operation)
	if err != nil {
		t.Fatal(err)
	}
	parent := project.Verified.Head
	evidence := protocol.ExecutionEvidence{
		Schema: protocol.EvidenceSchema, OperationDigest: digest,
		Action: operation.Action, Attempt: 1, Parent: &parent, Facts: map[string]any{},
	}
	facts := state.ReduceFacts{
		ObjectFormat: project.Verified.ObjectFormat, ParentCommit: parent,
		SignerFingerprint: root.Fingerprint,
	}
	next, err := state.Reduce(project.Verified.State, operation, evidence, facts)
	if err != nil {
		t.Fatal(err)
	}
	tree, err := currentTree(ctx, project)
	if err != nil {
		t.Fatal(err)
	}
	return transitionPlan{
		Operation: operation, Evidence: evidence, NextState: next,
		Tree: tree, ReduceFacts: facts, Authority: root,
	}
}

func runSyncJSON(t *testing.T) Envelope {
	t.Helper()
	var stdout, stderr bytes.Buffer
	exit := Run([]string{"sync", "--json"}, bytes.NewReader(nil), &stdout, &stderr)
	if exit != 0 {
		t.Fatalf("local sync failed: %s %s", stdout.String(), stderr.String())
	}
	var envelope Envelope
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	return envelope
}

func resultObject(t *testing.T, result any, key string) map[string]any {
	t.Helper()
	value, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("result is not an object: %#v", result)
	}
	object, ok := value[key].(map[string]any)
	if !ok {
		t.Fatalf("result %q is not an object: %#v", key, value[key])
	}
	return object
}

func resultListContains(value any, expected string) bool {
	values, ok := value.([]any)
	if !ok {
		return false
	}
	for _, candidate := range values {
		if candidate == expected {
			return true
		}
	}
	return false
}

func mustLoadCurrentProject(t *testing.T, ctx context.Context) *projectContext {
	t.Helper()
	project, err := loadProject(ctx, "", false)
	if err != nil {
		t.Fatal(err)
	}
	return project
}
