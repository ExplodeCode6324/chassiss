package cli

import (
	"bytes"
	"context"
	"encoding/json"
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
	"github.com/ExplodeCode6324/chassiss/internal/protocol"
	"github.com/ExplodeCode6324/chassiss/internal/state"
)

func TestVersionJSONEnvelope(t *testing.T) {
	var stdout, stderr bytes.Buffer
	exit := Run([]string{"version", "--json"}, bytes.NewReader(nil), &stdout, &stderr)
	if exit != 0 {
		t.Fatalf("exit %d stderr %s", exit, stderr.String())
	}
	var envelope Envelope
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if !envelope.OK || envelope.Schema != "chassiss.cli/v1" ||
		envelope.AvailableActions == nil || envelope.Extensions == nil || envelope.Warnings == nil {
		t.Fatalf("incomplete envelope %#v", envelope)
	}
}

func TestUnknownOptionReturnsStableUsageError(t *testing.T) {
	var stdout, stderr bytes.Buffer
	exit := Run([]string{"version", "--unknown", "--json"}, bytes.NewReader(nil), &stdout, &stderr)
	if exit != 2 {
		t.Fatalf("exit %d stderr %s", exit, stderr.String())
	}
	var envelope Envelope
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.OK || envelope.Error == nil || envelope.Error.Code != "CHS_USAGE_INVALID" {
		t.Fatalf("unexpected error envelope %#v", envelope)
	}
}

func TestRemoteURLValidation(t *testing.T) {
	valid := []string{
		"https://github.com/example/project.git",
		"ssh://git@example.com/example/project.git",
		"git://127.0.0.1:9418/project.git",
		"git@example.com:example/project.git",
	}
	for _, value := range valid {
		if err := validateRemoteURL(value); err != nil {
			t.Errorf("valid remote %q rejected: %v", value, err)
		}
	}
	invalid := []string{
		"", "../project.git", "/tmp/project.git", "file:///tmp/project.git",
		"https://user:secret@example.com/project.git",
		"https://example.com/project.git?token=secret",
		"git@example.com:",
	}
	for _, value := range invalid {
		if err := validateRemoteURL(value); err == nil {
			t.Errorf("invalid remote %q unexpectedly accepted", value)
		}
	}
}

func TestHelpSchemaUsesConcreteArraysAndErrors(t *testing.T) {
	var stdout, stderr bytes.Buffer
	exit := Run([]string{"help", "task", "start", "--json"}, bytes.NewReader(nil), &stdout, &stderr)
	if exit != 0 {
		t.Fatalf("exit %d stderr %s", exit, stderr.String())
	}
	var envelope struct {
		Result struct {
			Commands []CommandDefinition `json:"commands"`
		} `json:"result"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if len(envelope.Result.Commands) != 1 {
		t.Fatalf("unexpected help commands %#v", envelope.Result.Commands)
	}
	definition := envelope.Result.Commands[0]
	if definition.Arguments == nil || definition.Options == nil || definition.PossibleErrors == nil {
		t.Fatalf("help schema contains a null array: %#v", definition)
	}
	if !containsString(definition.PossibleErrors, "CHS_CAPABILITY_DENIED") ||
		!containsString(definition.PossibleErrors, "CHS_USAGE_INVALID") {
		t.Fatalf("help schema omitted stable possible errors: %#v", definition.PossibleErrors)
	}
}

func TestRemoteCASRetryRecomputesAuthorityTransition(t *testing.T) {
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
	defer func() {
		_ = daemon.Process.Kill()
		_ = daemon.Wait()
	}()
	remoteURL := "git://127.0.0.1:" + strconv.Itoa(port) + "/" + filepath.Base(remote)
	deadline := time.Now().Add(3 * time.Second)
	for {
		connection, dialErr := net.DialTimeout("tcp", "127.0.0.1:"+strconv.Itoa(port), 100*time.Millisecond)
		if dialErr == nil {
			_ = connection.Close()
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("git daemon did not start: %v", dialErr)
		}
		time.Sleep(20 * time.Millisecond)
	}
	if _, err := cryptoutil.GenerateFileKey(filepath.Join(dataDirectory, "keys"), "KEY-ROOT-01"); err != nil {
		t.Fatal(err)
	}
	architecture, _ := filepath.Abs(filepath.Join("..", "..", "docs", "templates", "architecture.yaml"))
	taskbook, _ := filepath.Abs(filepath.Join("..", "..", "docs", "templates", "taskbook.yaml"))
	previousDirectory, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(workspace); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(previousDirectory)
	var stdout, stderr bytes.Buffer
	if exit := Run([]string{
		"init", "--project", "PRJ-RETRY", "--architecture", architecture,
		"--taskbook", taskbook, "--root-key", "KEY-ROOT-01",
		"--remote", remoteURL, "--json",
	}, bytes.NewReader(nil), &stdout, &stderr); exit != 0 {
		t.Fatalf("init failed: %s %s", stdout.String(), stderr.String())
	}
	ctx := context.Background()
	original, err := loadProject(ctx, "", false)
	if err != nil {
		t.Fatal(err)
	}
	root, err := selectRoot(original, "KEY-ROOT-01")
	if err != nil {
		t.Fatal(err)
	}
	makePlan := func(grantID, keyID, operationID string) transitionPlan {
		t.Helper()
		generated, err := cryptoutil.GenerateFileKey(filepath.Join(dataDirectory, "keys"), keyID)
		if err != nil {
			t.Fatal(err)
		}
		maxActive := int64(1)
		grant := state.Grant{
			Actor: "retry-agent", Capabilities: []string{"task.start"},
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
			Action: "authority.grant-added", Project: original.Verified.State.Project.ID,
			Authority: root.Reference, Target: grantID,
			Preconditions: map[string]any{
				"grant_absent": true,
				"root_key_id":  original.Verified.State.Authority.Root.KeyID,
			},
			Payload: map[string]any{
				"grant": grantObject, "grant_id": grantID, "request_digest": nil,
			},
		}
		digest, _ := protocol.ObjectDigest("operation", operation)
		parent := original.Verified.Head
		evidence := protocol.ExecutionEvidence{
			Schema: protocol.EvidenceSchema, OperationDigest: digest,
			Action: operation.Action, Attempt: 1, Parent: &parent, Facts: map[string]any{},
		}
		facts := state.ReduceFacts{
			ObjectFormat: original.Verified.ObjectFormat, ParentCommit: parent,
			SignerFingerprint: root.Fingerprint,
		}
		next, err := state.Reduce(original.Verified.State, operation, evidence, facts)
		if err != nil {
			t.Fatal(err)
		}
		tree, err := currentTree(ctx, original)
		if err != nil {
			t.Fatal(err)
		}
		return transitionPlan{
			Operation: operation, Evidence: evidence, NextState: next,
			Tree: tree, ReduceFacts: facts, Authority: root,
		}
	}
	first := makePlan("GRT-RETRY-A", "KEY-RETRY-A", "OPR-11ARZ3NDEKTSV4RRFFQ69G5FAV")
	concurrent := makePlan("GRT-RETRY-B", "KEY-RETRY-B", "OPR-21ARZ3NDEKTSV4RRFFQ69G5FAV")
	proposal, err := createProposal(ctx, original, concurrent, "")
	if err != nil {
		t.Fatal(err)
	}
	if proposal.Operation == nil {
		t.Fatal("concurrent proposal has no signed commit")
	}
	if _, err := original.Runner.Run(
		ctx, "push", "--porcelain", "--atomic",
		"--force-with-lease=refs/heads/main:"+original.Verified.Head,
		"origin", proposal.Operation.Commit+":refs/heads/main",
	); err != nil {
		t.Fatal(err)
	}
	published, err := publishTransition(ctx, original, first)
	if err != nil {
		t.Fatal(err)
	}
	if published.Operation == nil || published.Operation.EvidenceAttempt != 2 {
		t.Fatalf("expected a second Evidence attempt, got %#v", published.Operation)
	}
	current, err := loadProject(ctx, "", false)
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := current.Verified.State.Authority.Grants["GRT-RETRY-A"]; !exists {
		t.Fatal("retried next State lost the requested Grant")
	}
	if _, exists := current.Verified.State.Authority.Grants["GRT-RETRY-B"]; !exists {
		t.Fatal("retried next State overwrote the concurrent Grant")
	}
	cloneDirectory := filepath.Join(t.TempDir(), "clone")
	stdout.Reset()
	stderr.Reset()
	if exit := Run([]string{
		"clone", remoteURL, cloneDirectory,
		"--project", "PRJ-RETRY",
		"--root-fingerprint", current.Verified.RootFingerprint,
		"--checkpoint", current.Verified.Head,
		"--json",
	}, bytes.NewReader(nil), &stdout, &stderr); exit != 0 {
		t.Fatalf("trusted clone failed: %s %s", stdout.String(), stderr.String())
	}
	if err := os.Chdir(cloneDirectory); err != nil {
		t.Fatal(err)
	}
	stdout.Reset()
	stderr.Reset()
	if exit := Run([]string{"verify", "--full", "--json"}, bytes.NewReader(nil), &stdout, &stderr); exit != 0 {
		t.Fatalf("cloned history failed verification: %s %s", stdout.String(), stderr.String())
	}
	if err := os.Chdir(workspace); err != nil {
		t.Fatal(err)
	}
}

func TestInitAndVerifyGenesis(t *testing.T) {
	workspace := t.TempDir()
	dataDirectory := filepath.Join(t.TempDir(), "local-data")
	t.Setenv("CHASSISS_DATA_DIR", dataDirectory)
	key, err := cryptoutil.GenerateFileKey(filepath.Join(dataDirectory, "keys"), "KEY-ROOT-01")
	if err != nil {
		t.Fatal(err)
	}
	if key.PublicKey == "" {
		t.Fatal("root key was not generated")
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
	if err := os.WriteFile(filepath.Join(workspace, "go.mod"), []byte("module example.test/project\n\ngo 1.24\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	previous, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(workspace); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(previous)

	var stdout, stderr bytes.Buffer
	exit := Run([]string{
		"init", "--project", "PRJ-TEST", "--architecture", architecture,
		"--taskbook", taskbook, "--root-key", "KEY-ROOT-01", "--json",
	}, bytes.NewReader(nil), &stdout, &stderr)
	if exit != 0 {
		t.Fatalf("init exit %d\nstdout %s\nstderr %s", exit, stdout.String(), stderr.String())
	}
	var initEnvelope Envelope
	if err := json.Unmarshal(stdout.Bytes(), &initEnvelope); err != nil {
		t.Fatal(err)
	}
	if initEnvelope.Operation == nil || initEnvelope.Operation.Status != "published" {
		t.Fatalf("unexpected init operation %#v", initEnvelope.Operation)
	}
	stdout.Reset()
	stderr.Reset()
	exit = Run([]string{"verify", "--full", "--json"}, bytes.NewReader(nil), &stdout, &stderr)
	if exit != 0 {
		t.Fatalf("verify exit %d\nstdout %s\nstderr %s", exit, stdout.String(), stderr.String())
	}
	var verifyEnvelope Envelope
	if err := json.Unmarshal(stdout.Bytes(), &verifyEnvelope); err != nil {
		t.Fatal(err)
	}
	if verifyEnvelope.Snapshot == nil || verifyEnvelope.Snapshot.Trust != "verified" ||
		verifyEnvelope.Project == nil || verifyEnvelope.Project.ID != "PRJ-TEST" {
		t.Fatalf("unexpected verify envelope %#v", verifyEnvelope)
	}

	keyEnvelope := runJSON(t, []string{
		"key", "generate", "--id", "KEY-AGENT-01", "--actor", "agent-one",
	}, &stdout, &stderr)
	if !keyEnvelope.OK {
		t.Fatal("agent key generation failed")
	}
	requestPath := filepath.Join(t.TempDir(), "grant-request.json")
	requestEnvelope := runJSON(t, []string{
		"grant", "request", "--project", "PRJ-TEST", "--key", "KEY-AGENT-01",
		"--profile", "developer", "--task-scope", "TASK-*",
		"--capability", "review.attest", "--capability", "integration.apply",
		"--capability", "taskbook.archive", "--capability", "taskbook.open",
		"--capability", "taskbook.update", "--capability", "architecture.update",
		"--capability", "owner.apply", "--task-scope", "*",
		"--resource-scope", "module:*", "--resource-scope", "schema:*", "--resource-scope", "*",
		"--limits", "bounded", "--output", requestPath,
	}, &stdout, &stderr)
	if !requestEnvelope.OK {
		t.Fatal("Grant Request failed")
	}
	grantEnvelope := runJSON(t, []string{
		"grant", "add", "--request", requestPath, "--grant-id", "GRT-AGENT-01",
		"--root-key", "KEY-ROOT-01", "--profile", "developer",
		"--capability", "review.attest", "--capability", "integration.apply",
		"--capability", "taskbook.archive", "--capability", "taskbook.open",
		"--capability", "taskbook.update", "--capability", "architecture.update",
		"--capability", "owner.apply", "--task-scope", "TASK-*", "--task-scope", "*",
		"--resource-scope", "module:*", "--resource-scope", "schema:*",
		"--resource-scope", "*", "--limits", "bounded",
	}, &stdout, &stderr)
	if grantEnvelope.Operation == nil {
		t.Fatalf("Grant transition missing: %#v", grantEnvelope)
	}
	runJSON(t, []string{
		"key", "generate", "--id", "KEY-EXTRA-01", "--actor", "extra-agent",
	}, &stdout, &stderr)
	extraRequestPath := filepath.Join(t.TempDir(), "extra-grant-request.json")
	runJSON(t, []string{
		"grant", "request", "--project", "PRJ-TEST", "--key", "KEY-EXTRA-01",
		"--profile", "developer", "--task-scope", "TASK-*",
		"--resource-scope", "module:*", "--resource-scope", "schema:*",
		"--limits", "bounded", "--output", extraRequestPath,
	}, &stdout, &stderr)
	proposalPath := filepath.Join(t.TempDir(), "grant-proposal.bundle")
	proposalEnvelope := runJSON(t, []string{
		"grant", "add", "--request", extraRequestPath, "--grant-id", "GRT-EXTRA-01",
		"--root-key", "KEY-ROOT-01", "--profile", "developer",
		"--task-scope", "TASK-*", "--resource-scope", "module:*",
		"--resource-scope", "schema:*", "--limits", "bounded",
		"--proposal", proposalPath,
	}, &stdout, &stderr)
	if proposalEnvelope.Operation == nil || proposalEnvelope.Operation.Status != "signed" {
		t.Fatalf("Grant proposal was not signed: %#v", proposalEnvelope)
	}
	inspectEnvelope := runJSON(t, []string{
		"transition", "inspect", proposalPath,
	}, &stdout, &stderr)
	if inspectEnvelope.Result.(map[string]any)["commit"] == "" {
		t.Fatalf("Proposal inspect failed: %#v", inspectEnvelope)
	}
	publishEnvelope := runJSON(t, []string{
		"transition", "publish", proposalPath,
	}, &stdout, &stderr)
	if publishEnvelope.Result.(map[string]any)["commit"] == "" {
		t.Fatalf("Proposal publish failed: %#v", publishEnvelope)
	}
	startEnvelope := runJSON(t, []string{"task", "start", "TASK-001", "--grant", "GRT-AGENT-01"}, &stdout, &stderr)
	if startEnvelope.Operation == nil {
		t.Fatalf("Task start transition missing: %#v", startEnvelope)
	}
	result := startEnvelope.Result.(map[string]any)
	if result["worktree"] == "" {
		t.Fatalf("Task start did not return a worktree: %#v", result)
	}
	worktree := result["worktree"].(string)
	if err := os.MkdirAll(filepath.Join(worktree, "src", "core", "reducer"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(worktree, "tests", "core", "reducer"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(worktree, "src", "core", "reducer", "reducer.go"),
		[]byte("package reducer\n\nfunc Apply(value int) int { return value + 1 }\n"), 0o644,
	); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(worktree, "tests", "core", "reducer", "reducer_test.go"),
		[]byte("package reducer_test\n\nimport \"testing\"\n\nfunc TestPlaceholder(t *testing.T) {}\n"), 0o644,
	); err != nil {
		t.Fatal(err)
	}
	commitEnvelope := runJSON(t, []string{
		"work", "commit", "TASK-001", "--message", "implement reducer",
	}, &stdout, &stderr)
	if commitEnvelope.Result.(map[string]any)["commit"] == "" {
		t.Fatalf("Work Commit missing: %#v", commitEnvelope)
	}
	submitEnvelope := runJSON(t, []string{"submit", "TASK-001", "--grant", "GRT-AGENT-01"}, &stdout, &stderr)
	if submitEnvelope.Operation == nil {
		t.Fatalf("Submit transition missing: %#v", submitEnvelope)
	}
	reportPath := filepath.Join(t.TempDir(), "review-report.json")
	report := map[string]any{
		"schema":  "chassiss.review-report/v1",
		"verdict": "approve",
		"summary": "The exact Attempt satisfies the frozen contract.",
		"results": map[string]any{
			"requirements": "pass", "contract": "pass",
			"architecture": "conformant", "integration": "pass",
		},
		"findings": []any{},
		"reviewer_attention_responses": []any{
			map[string]any{
				"attention": "Reject extra fields in every sparse Task phase.",
				"response":  "Reducer tests and verifier checks cover sparse projections.",
			},
			map[string]any{
				"attention": "Authorize from the parent State only.",
				"response":  "Authority was verified against the exact parent State.",
			},
		},
	}
	reportData, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(reportPath, reportData, 0o600); err != nil {
		t.Fatal(err)
	}
	reviewEnvelope := runJSON(t, []string{
		"review", "TASK-001", "--verdict", "approve", "--report", reportPath,
		"--grant", "GRT-AGENT-01",
	}, &stdout, &stderr)
	if reviewEnvelope.Operation == nil || len(reviewEnvelope.Warnings) != 2 {
		t.Fatalf("Review transition or independence warnings missing: %#v", reviewEnvelope)
	}
	integrateEnvelope := runJSON(t, []string{"integrate", "TASK-001", "--grant", "GRT-AGENT-01"}, &stdout, &stderr)
	if integrateEnvelope.Operation == nil {
		t.Fatalf("Integration transition missing: %#v", integrateEnvelope)
	}
	verifyEnvelope = runJSON(t, []string{"verify", "--full"}, &stdout, &stderr)
	if verifyEnvelope.Snapshot == nil || verifyEnvelope.Snapshot.Trust != "verified" {
		t.Fatalf("Integrated history did not fully verify: %#v", verifyEnvelope)
	}
	if _, err := os.Stat(filepath.Join(workspace, "src", "core", "reducer", "reducer.go")); err != nil {
		t.Fatalf("Integration did not materialize candidate content: %v", err)
	}
	binDirectory := t.TempDir()
	binaryName := "chassiss"
	if goruntime.GOOS == "windows" {
		binaryName += ".exe"
	}
	binary := filepath.Join(binDirectory, binaryName)
	build := exec.Command("go", "build", "-o", binary, filepath.Join(previous, "..", "..", "cmd", "chassiss"))
	build.Dir = previous
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build check binary: %v\n%s", err, output)
	}
	t.Setenv("PATH", binDirectory+string(os.PathListSeparator)+os.Getenv("PATH"))
	closurePath := filepath.Join(t.TempDir(), "closure-report.json")
	closureReport := map[string]any{
		"schema":   "chassiss.taskbook-closure-report/v1",
		"taskbook": "TASKBOOK-001",
		"summary":  "The workflow outcome and all terminal dispositions are accepted.",
		"task_responses": []any{
			map[string]any{
				"task": "TASK-001", "phase": "closed",
				"response": "The verified Integration closes the frozen Task Contract.",
			},
		},
		"completion_criteria_responses": []any{
			map[string]any{
				"criterion": "Every task has a Reviewer-accepted terminal disposition.",
				"response":  "TASK-001 is closed by its reviewed Integration.", "status": "pass",
			},
			map[string]any{
				"criterion": "Independent verification passes from Genesis to current main.",
				"response":  "The isolated Workflow Check performs full verification.", "status": "pass",
			},
		},
		"findings": []any{},
	}
	closureData, err := json.Marshal(closureReport)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(closurePath, closureData, 0o600); err != nil {
		t.Fatal(err)
	}
	archiveEnvelope := runJSON(t, []string{
		"taskbook", "archive", "--report", closurePath, "--grant", "GRT-AGENT-01",
	}, &stdout, &stderr)
	if archiveEnvelope.Operation == nil {
		t.Fatalf("Taskbook archive transition missing: %#v", archiveEnvelope)
	}
	if _, err := os.Stat(filepath.Join(workspace, "docs", "taskbooks", "archive", "TASKBOOK-001.yaml")); err != nil {
		t.Fatalf("Taskbook archive was not materialized: %v", err)
	}
	architectureDraft := filepath.Join(t.TempDir(), "architecture.yaml")
	runJSON(t, []string{
		"architecture", "draft", "--output", architectureDraft,
	}, &stdout, &stderr)
	architectureData, err := os.ReadFile(architectureDraft)
	if err != nil {
		t.Fatal(err)
	}
	architectureData = []byte(strings.Replace(
		string(architectureData),
		"Git stores shared facts, State stores the current projection, Architecture",
		"Git stores signed shared facts, State stores the current projection, Architecture",
		1,
	))
	if err := os.WriteFile(architectureDraft, architectureData, 0o600); err != nil {
		t.Fatal(err)
	}
	architectureEnvelope := runJSON(t, []string{
		"architecture", "update", "--file", architectureDraft,
		"--reason", "Clarify the signed shared-fact boundary.", "--grant", "GRT-AGENT-01",
	}, &stdout, &stderr)
	if architectureEnvelope.Operation == nil {
		t.Fatalf("Architecture update transition missing: %#v", architectureEnvelope)
	}
	if err := os.WriteFile(filepath.Join(workspace, "OWNER.txt"), []byte("owner snapshot\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	previewEnvelope := runJSON(t, []string{
		"owner", "apply", "--reason", "Capture owner material.",
		"--summary", "Add owner snapshot.",
	}, &stdout, &stderr)
	if previewEnvelope.Result.(map[string]any)["requires_yes"] != true {
		t.Fatalf("Owner preview did not require confirmation: %#v", previewEnvelope)
	}
	ownerEnvelope := runJSON(t, []string{
		"owner", "apply", "--reason", "Capture owner material.",
		"--summary", "Add owner snapshot.", "--yes", "--grant", "GRT-AGENT-01",
	}, &stdout, &stderr)
	if ownerEnvelope.Operation == nil {
		t.Fatalf("Owner Apply transition missing: %#v", ownerEnvelope)
	}
	nextTaskbook := filepath.Join(t.TempDir(), "next-taskbook.yaml")
	runJSON(t, []string{
		"taskbook", "draft", "--new", "--output", nextTaskbook,
	}, &stdout, &stderr)
	openEnvelope := runJSON(t, []string{
		"taskbook", "open", "--file", nextTaskbook,
		"--reason", "Open the next workflow.", "--grant", "GRT-AGENT-01",
	}, &stdout, &stderr)
	if openEnvelope.Operation == nil {
		t.Fatalf("Next Taskbook open transition missing: %#v", openEnvelope)
	}
	currentTaskbook := filepath.Join(t.TempDir(), "current-taskbook.yaml")
	runJSON(t, []string{
		"taskbook", "draft", "--output", currentTaskbook,
	}, &stdout, &stderr)
	currentData, err := os.ReadFile(currentTaskbook)
	if err != nil {
		t.Fatal(err)
	}
	currentData = []byte(strings.Replace(
		string(currentData), "Edit this workflow title", "Next verified workflow", 1,
	))
	if err := os.WriteFile(currentTaskbook, currentData, 0o600); err != nil {
		t.Fatal(err)
	}
	updateEnvelope := runJSON(t, []string{
		"taskbook", "update", "--file", currentTaskbook,
		"--reason", "Name the next workflow.", "--grant", "GRT-AGENT-01",
	}, &stdout, &stderr)
	if updateEnvelope.Operation == nil {
		t.Fatalf("Taskbook update transition missing: %#v", updateEnvelope)
	}
	verifyEnvelope = runJSON(t, []string{"verify", "--full"}, &stdout, &stderr)
	if verifyEnvelope.Snapshot == nil || verifyEnvelope.Snapshot.Trust != "verified" {
		t.Fatalf("Full lifecycle history did not verify: %#v", verifyEnvelope)
	}
}

func runJSON(t *testing.T, args []string, stdout, stderr *bytes.Buffer) Envelope {
	t.Helper()
	stdout.Reset()
	stderr.Reset()
	args = append(args, "--json")
	exit := Run(args, bytes.NewReader(nil), stdout, stderr)
	if exit != 0 {
		t.Fatalf("%v exit %d\nstdout %s\nstderr %s", args, exit, stdout.String(), stderr.String())
	}
	var envelope Envelope
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	return envelope
}
