package verifier

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ExplodeCode6324/chassiss/internal/contracts"
	"github.com/ExplodeCode6324/chassiss/internal/cryptoutil"
	"github.com/ExplodeCode6324/chassiss/internal/gitstore"
	"github.com/ExplodeCode6324/chassiss/internal/protocol"
	"github.com/ExplodeCode6324/chassiss/internal/state"
)

func genesisRepository(t *testing.T) (gitstore.Runner, string, string) {
	t.Helper()
	ctx := context.Background()
	repository := t.TempDir()
	bootstrap := gitstore.New("")
	if _, err := bootstrap.Run(ctx, "init", "-b", "main", repository); err != nil {
		t.Fatal(err)
	}
	runner := gitstore.New(repository)
	key, err := cryptoutil.GenerateFileKey(t.TempDir(), "KEY-ROOT-01")
	if err != nil {
		t.Fatal(err)
	}
	keyPath, err := cryptoutil.ResolveFileHandle(key.Handle)
	if err != nil {
		t.Fatal(err)
	}
	architectureData, err := os.ReadFile(filepath.Join("..", "..", "docs", "templates", "architecture.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	taskbookData, err := os.ReadFile(filepath.Join("..", "..", "docs", "templates", "taskbook.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	architecture, err := contracts.ParseArchitecture(architectureData)
	if err != nil {
		t.Fatal(err)
	}
	taskbook, err := contracts.ParseTaskbook(taskbookData, architecture)
	if err != nil {
		t.Fatal(err)
	}
	architectureBlob, err := runner.HashBlob(ctx, architectureData)
	if err != nil {
		t.Fatal(err)
	}
	taskbookBlob, err := runner.HashBlob(ctx, taskbookData)
	if err != nil {
		t.Fatal(err)
	}
	operation := protocol.Operation{
		Schema: protocol.OperationSchema, OperationID: "OPR-01ARZ3NDEKTSV4RRFFQ69G5FAV",
		Action: "project.genesis", Project: "PRJ-TEST",
		Authority: "root:KEY-ROOT-01", Target: "PRJ-TEST",
		Preconditions: map[string]any{},
		Payload: map[string]any{
			"architecture_blob": architectureBlob, "project_id": "PRJ-TEST",
			"root_key_id": "KEY-ROOT-01", "root_public_key": key.PublicKey,
			"taskbook_blob": taskbookBlob,
		},
	}
	operationDigest, err := protocol.ObjectDigest("operation", operation)
	if err != nil {
		t.Fatal(err)
	}
	placeholderTree := strings.Repeat("0", 40)
	evidence := protocol.ExecutionEvidence{
		Schema: protocol.EvidenceSchema, OperationDigest: operationDigest,
		Action: operation.Action, Attempt: 1, Parent: nil,
		Facts: map[string]any{
			"architecture_blob": architectureBlob, "initial_tree": placeholderTree,
			"taskbook_blob": taskbookBlob,
		},
	}
	ready := make([]string, 0, len(taskbook.Tasks))
	for id := range taskbook.Tasks {
		ready = append(ready, id)
	}
	initial, err := state.Reduce(nil, operation, evidence, state.ReduceFacts{
		ObjectFormat: "sha1", GenesisTaskbookID: taskbook.ID, GenesisReadyTasks: ready,
	})
	if err != nil {
		t.Fatal(err)
	}
	stateData, err := state.Encode(initial, "sha1")
	if err != nil {
		t.Fatal(err)
	}
	stateBlob, err := runner.HashBlob(ctx, stateData)
	if err != nil {
		t.Fatal(err)
	}
	tree, err := runner.WriteTree(ctx, gitstore.TreeMap{
		".chassiss/state.json":   {Mode: "100644", OID: stateBlob},
		"docs/architecture.yaml": {Mode: "100644", OID: architectureBlob},
		"docs/taskbook.yaml":     {Mode: "100644", OID: taskbookBlob},
	})
	if err != nil {
		t.Fatal(err)
	}
	evidence.Facts["initial_tree"] = tree
	stateDigest := protocol.DigestBytes(stateData)
	message, err := protocol.BuildTransitionMessage(operation, evidence, stateDigest, "sha1")
	if err != nil {
		t.Fatal(err)
	}
	commit, err := runner.CommitTree(ctx, tree, nil, message, keyPath, gitstore.CommitIdentity{})
	if err != nil {
		t.Fatal(err)
	}
	if err := runner.UpdateRefCAS(ctx, "refs/heads/main", commit, strings.Repeat("0", 40), "Genesis"); err != nil {
		t.Fatal(err)
	}
	return runner, commit, key.Fingerprint
}

func TestVerifyGenesis(t *testing.T) {
	runner, commit, rootFingerprint := genesisRepository(t)
	result, err := Verify(context.Background(), runner, "refs/heads/main", Options{
		ExpectedProject: "PRJ-TEST", ExpectedRootFingerprint: rootFingerprint,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Genesis != commit || result.Head != commit || result.State.Project.ID != "PRJ-TEST" {
		t.Fatalf("unexpected verification result %#v", result)
	}
}

func TestVerifyRejectsOrdinaryMainCommit(t *testing.T) {
	runner, genesis, _ := genesisRepository(t)
	ctx := context.Background()
	genesisCommit, err := runner.ReadCommit(ctx, genesis)
	if err != nil {
		t.Fatal(err)
	}
	result, err := runner.RunWithInput(ctx, []byte("ordinary\n"), map[string]string{
		"GIT_AUTHOR_NAME": "Test", "GIT_AUTHOR_EMAIL": "test@example.invalid",
		"GIT_COMMITTER_NAME": "Test", "GIT_COMMITTER_EMAIL": "test@example.invalid",
	}, "commit-tree", genesisCommit.Tree, "-p", genesis)
	if err != nil {
		t.Fatal(err)
	}
	ordinary := strings.TrimSpace(string(result.Stdout))
	if err := runner.UpdateRefCAS(ctx, "refs/heads/main", ordinary, genesis, "tamper"); err != nil {
		t.Fatal(err)
	}
	if _, err := Verify(ctx, runner, "refs/heads/main", Options{}); err == nil {
		t.Fatal("ordinary main commit unexpectedly verified")
	}
}

func TestCurrentAttemptRequiresExactWorkRef(t *testing.T) {
	runner, head, _ := genesisRepository(t)
	current := &state.State{Tasks: map[string]state.TaskState{
		"TASK-001": {
			Actor: "agent-one", Phase: "submitted",
			Attempt: &state.Attempt{Head: head},
		},
	}}
	ctx := context.Background()
	if err := verifyCurrentWorkRefs(ctx, runner, current); err == nil {
		t.Fatal("missing Work Ref unexpectedly passed")
	}
	ref := "refs/heads/chassiss/work/TASK-001/agent-one"
	if err := runner.UpdateRefCAS(ctx, ref, head, strings.Repeat("0", 40), "fixture"); err != nil {
		t.Fatal(err)
	}
	if err := verifyCurrentWorkRefs(ctx, runner, current); err != nil {
		t.Fatal(err)
	}
}
