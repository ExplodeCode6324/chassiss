package app

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestGitWorkingFilesAndDiff(t *testing.T) {
	root := t.TempDir()
	if _, err := git(root, "init", "-b", "main"); err != nil {
		t.Fatal(err)
	}
	tracked := filepath.Join(root, "calc", "calc.go")
	if err := os.MkdirAll(filepath.Dir(tracked), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tracked, []byte("package calc\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := gitCommit(root, "baseline", "calc/calc.go"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tracked, []byte("package calc\n\nconst Changed = true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "new.go"), []byte("package calc\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	files, err := gitWorkingFiles(root)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"calc/calc.go", "new.go"}
	if !reflect.DeepEqual(files, want) {
		t.Fatalf("files = %#v, want %#v", files, want)
	}
	diff, err := gitWorkingDiff(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, marker := range []string{"calc/calc.go", "new.go", "Changed = true"} {
		if !strings.Contains(diff, marker) {
			t.Fatalf("diff does not contain %q:\n%s", marker, diff)
		}
	}
}

func TestGitWorktreeDigestChangesWithContent(t *testing.T) {
	root := t.TempDir()
	if _, err := git(root, "init", "-b", "main"); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "file.txt")
	if err := os.WriteFile(path, []byte("baseline"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := gitCommit(root, "baseline", "file.txt"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("first"), 0o644); err != nil {
		t.Fatal(err)
	}
	first, err := gitWorktreeDigest(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("second"), 0o644); err != nil {
		t.Fatal(err)
	}
	second, err := gitWorktreeDigest(root)
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("worktree digest did not change with file content")
	}
}
