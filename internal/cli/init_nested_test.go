package cli

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/ExplodeCode6324/chassiss/internal/cryptoutil"
)

func TestInitNestedTargetDoesNotInheritParentHistory(t *testing.T) {
	parent := t.TempDir()
	child := filepath.Join(parent, "nested-project")
	if err := os.Mkdir(child, 0o755); err != nil {
		t.Fatal(err)
	}
	if output, err := exec.Command("git", "-C", parent, "init", "-b", "main").CombinedOutput(); err != nil {
		t.Fatalf("parent init: %v\n%s", err, output)
	}
	if err := os.WriteFile(filepath.Join(parent, "parent.txt"), []byte("parent\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if output, err := exec.Command("git", "-C", parent, "add", "parent.txt").CombinedOutput(); err != nil {
		t.Fatalf("parent add: %v\n%s", err, output)
	}
	if output, err := exec.Command(
		"git", "-C", parent, "-c", "user.name=Test", "-c", "user.email=test@example.invalid",
		"commit", "-m", "parent",
	).CombinedOutput(); err != nil {
		t.Fatalf("parent commit: %v\n%s", err, output)
	}
	dataDirectory := filepath.Join(t.TempDir(), "local-data")
	t.Setenv("CHASSISS_DATA_DIR", dataDirectory)
	if _, err := cryptoutil.GenerateFileKey(filepath.Join(dataDirectory, "keys"), "KEY-ROOT-NESTED"); err != nil {
		t.Fatal(err)
	}
	architecture, _ := filepath.Abs(filepath.Join("..", "..", "docs", "templates", "architecture.yaml"))
	taskbook, _ := filepath.Abs(filepath.Join("..", "..", "docs", "templates", "taskbook.yaml"))
	previous, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(child); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(previous)
	var stdout, stderr bytes.Buffer
	if exit := Run([]string{
		"init", "--project", "PRJ-NESTED", "--architecture", architecture,
		"--taskbook", taskbook, "--root-key", "KEY-ROOT-NESTED", "--json",
	}, bytes.NewReader(nil), &stdout, &stderr); exit != 0 {
		t.Fatalf("nested init exit %d\nstdout %s\nstderr %s", exit, stdout.String(), stderr.String())
	}
	childRoot, err := exec.Command("git", "-C", child, "rev-parse", "--show-toplevel").Output()
	if err != nil {
		t.Fatal(err)
	}
	if !samePath(filepath.Clean(string(bytes.TrimSpace(childRoot))), child) {
		t.Fatalf("nested init used the parent repository: %q", childRoot)
	}
}
