package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ExplodeCode6324/chassiss/internal/cryptoutil"
)

func TestBootstrapExistingProjectAndEstablishArchitecture(t *testing.T) {
	source := t.TempDir()
	runTestGit(t, source, "init", "-b", "main")
	if err := os.MkdirAll(filepath.Join(source, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "README.md"), []byte("# legacy project\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "src", "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runTestGit(t, source, "add", "README.md", "src/main.go")
	runTestGit(t, source, "-c", "user.name=Legacy", "-c", "user.email=legacy@example.invalid", "commit", "-m", "legacy snapshot")
	sourceCommit := strings.TrimSpace(runTestGit(t, source, "rev-parse", "HEAD"))

	target := t.TempDir()
	dataDirectory := filepath.Join(t.TempDir(), "local-data")
	t.Setenv("CHASSISS_DATA_DIR", dataDirectory)
	if _, err := cryptoutil.GenerateFileKey(filepath.Join(dataDirectory, "keys"), "KEY-ROOT-BOOTSTRAP"); err != nil {
		t.Fatal(err)
	}
	notes := filepath.Join(t.TempDir(), "history.md")
	if err := os.WriteFile(notes, []byte("The legacy main branch was frozen after release 1.0.\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	previous, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(target); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(previous)

	var stdout, stderr bytes.Buffer
	bootstrap := runJSON(t, []string{
		"bootstrap", "--project", "PRJ-BOOTSTRAP", "--source", source,
		"--ref", sourceCommit, "--root-key", "KEY-ROOT-BOOTSTRAP", "--history", notes,
	}, &stdout, &stderr)
	if bootstrap.Operation == nil || bootstrap.Operation.Status != "published" {
		t.Fatalf("bootstrap operation missing: %#v", bootstrap)
	}
	if bootstrap.Snapshot == nil || bootstrap.Snapshot.ArchitectureBlob != nil ||
		bootstrap.Snapshot.TaskbookBlob != nil {
		t.Fatalf("bootstrap unexpectedly established contracts: %#v", bootstrap.Snapshot)
	}
	if _, err := os.Stat(filepath.Join(target, "src", "main.go")); err != nil {
		t.Fatalf("source snapshot was not materialized: %v", err)
	}
	if parents := strings.Fields(runTestGit(t, target, "rev-list", "--parents", "-n", "1", "HEAD")); len(parents) != 1 {
		t.Fatalf("bootstrap commit is not zero-parent: %v", parents)
	}
	if output, err := exec.Command("git", "-C", target, "cat-file", "-e", sourceCommit+"^{commit}").CombinedOutput(); err == nil {
		t.Fatalf("source commit object was imported into the authoritative repository: %s", output)
	}
	historyData, err := os.ReadFile(filepath.Join(target, filepath.FromSlash(sourceHistoryPath)))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(historyData, []byte(sourceCommit)) ||
		!bytes.Contains(historyData, []byte("non-authoritative")) ||
		!bytes.Contains(historyData, []byte("release 1.0")) {
		t.Fatalf("source history reference is incomplete:\n%s", historyData)
	}
	verify := runJSON(t, []string{"verify", "--full"}, &stdout, &stderr)
	if verify.Snapshot == nil || verify.Snapshot.ArchitectureBlob != nil {
		t.Fatalf("verified bootstrap has an Architecture: %#v", verify.Snapshot)
	}

	external := t.TempDir()
	if err := os.Chdir(external); err != nil {
		t.Fatal(err)
	}
	runJSON(t, []string{
		"key", "generate", "--id", "KEY-ARCHITECT-01", "--actor", "architect",
	}, &stdout, &stderr)
	request := filepath.Join(t.TempDir(), "architect-request.json")
	runJSON(t, []string{
		"grant", "request", "--project", "PRJ-BOOTSTRAP", "--key", "KEY-ARCHITECT-01",
		"--capability", "architecture.establish", "--task-scope", "*",
		"--resource-scope", "*", "--limits", "unbounded", "--output", request,
	}, &stdout, &stderr)
	if err := os.Chdir(target); err != nil {
		t.Fatal(err)
	}
	runJSON(t, []string{
		"grant", "add", "--request", request, "--grant-id", "GRT-ARCHITECT-01",
		"--root-key", "KEY-ROOT-BOOTSTRAP", "--capability", "architecture.establish",
		"--task-scope", "*", "--resource-scope", "*", "--limits", "unbounded",
	}, &stdout, &stderr)
	runJSON(t, []string{"key", "attach", "KEY-ARCHITECT-01", "--select"}, &stdout, &stderr)
	architecture := filepath.Join(t.TempDir(), "architecture.yaml")
	runJSON(t, []string{"architecture", "draft", "--new", "--output", architecture}, &stdout, &stderr)
	established := runJSON(t, []string{
		"architecture", "establish", "--file", architecture,
		"--reason", "audited the adopted source", "--grant", "GRT-ARCHITECT-01",
	}, &stdout, &stderr)
	if established.Operation == nil || established.Operation.Status != "published" {
		t.Fatalf("Architecture establish operation missing: %#v", established)
	}
	verify = runJSON(t, []string{"verify", "--full"}, &stdout, &stderr)
	if verify.Snapshot == nil || verify.Snapshot.ArchitectureBlob == nil ||
		verify.Snapshot.TaskbookBlob != nil {
		t.Fatalf("Architecture establish produced an invalid snapshot: %#v", verify.Snapshot)
	}
	taskbook := filepath.Join(t.TempDir(), "taskbook.yaml")
	runJSON(t, []string{"taskbook", "draft", "--new", "--output", taskbook}, &stdout, &stderr)
}

func TestBootstrapRejectsProtectedSourceBeforeInitializingTarget(t *testing.T) {
	source := t.TempDir()
	runTestGit(t, source, "init", "-b", "main")
	if err := os.MkdirAll(filepath.Join(source, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "docs", "architecture.yaml"), []byte("legacy\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runTestGit(t, source, "add", "docs/architecture.yaml")
	runTestGit(t, source, "-c", "user.name=Legacy", "-c", "user.email=legacy@example.invalid", "commit", "-m", "protected collision")
	sourceCommit := strings.TrimSpace(runTestGit(t, source, "rev-parse", "HEAD"))
	target := t.TempDir()
	t.Setenv("CHASSISS_DATA_DIR", filepath.Join(t.TempDir(), "local-data"))
	previous, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(target); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(previous)
	var stdout, stderr bytes.Buffer
	exit := Run([]string{
		"bootstrap", "--project", "PRJ-COLLISION", "--source", source,
		"--ref", sourceCommit, "--root-key", "KEY-MISSING", "--json",
	}, bytes.NewReader(nil), &stdout, &stderr)
	if exit != 6 {
		t.Fatalf("bootstrap collision exit %d\nstdout %s\nstderr %s", exit, stdout.String(), stderr.String())
	}
	var envelope Envelope
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Error == nil || envelope.Error.Code != "CHS_PROTECTED_PATH_CHANGED" {
		t.Fatalf("unexpected collision error: %#v", envelope.Error)
	}
	if entries, err := os.ReadDir(target); err != nil || len(entries) != 0 {
		t.Fatalf("failed bootstrap initialized the target: entries=%v err=%v", entries, err)
	}
}

func runTestGit(t *testing.T, directory string, args ...string) string {
	t.Helper()
	commandArgs := append([]string{"-C", directory}, args...)
	output, err := exec.Command("git", commandArgs...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, output)
	}
	return string(output)
}
