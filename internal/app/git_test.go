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
	spaced := " leading and trailing .txt "
	if err := os.WriteFile(filepath.Join(root, spaced), []byte("spaces\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	files, err := gitWorkingFiles(root)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{spaced, "calc/calc.go", "new.go"}
	if !reflect.DeepEqual(files, want) {
		t.Fatalf("files = %#v, want %#v", files, want)
	}
	diff, err := gitWorkingDiff(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, marker := range []string{"calc/calc.go", "new.go", "leading and trailing", "Changed = true"} {
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

func TestGitWorktreeDigestBindsModesSymlinksAndDoesNotStage(t *testing.T) {
	root := t.TempDir()
	if _, err := git(root, "init", "-b", "main"); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"target-a", "target-b"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("same content\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	executable := filepath.Join(root, "tool.sh")
	if err := os.WriteFile(executable, []byte("#!/bin/sh\nexit 0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "current")
	if err := os.Symlink("target-a", link); err != nil {
		t.Fatal(err)
	}
	if _, err := gitCommit(root, "baseline", "target-a", "target-b", "tool.sh", "current"); err != nil {
		t.Fatal(err)
	}
	indexBefore, err := git(root, "write-tree")
	if err != nil {
		t.Fatal(err)
	}
	baselineDigest, err := gitWorktreeDigest(root)
	if err != nil {
		t.Fatal(err)
	}
	indexAfter, _ := git(root, "write-tree")
	if indexAfter != indexBefore {
		t.Fatalf("digest changed real index from %s to %s", indexBefore, indexAfter)
	}
	if err := os.Chmod(executable, 0o755); err != nil {
		t.Fatal(err)
	}
	modeDigest, err := gitWorktreeDigest(root)
	if err != nil {
		t.Fatal(err)
	}
	if modeDigest == baselineDigest {
		t.Fatal("executable-bit change did not change Git tree/index digest")
	}
	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("target-b", link); err != nil {
		t.Fatal(err)
	}
	linkDigest, err := gitWorktreeDigest(root)
	if err != nil {
		t.Fatal(err)
	}
	if linkDigest == modeDigest {
		t.Fatal("symlink target change did not change Git tree/index digest")
	}
}

func TestGitWorkingFilesReportsBothSidesOfRename(t *testing.T) {
	root := t.TempDir()
	if _, err := git(root, "init", "-b", "main"); err != nil {
		t.Fatal(err)
	}
	oldPath := filepath.Join(root, "old.txt")
	if err := os.WriteFile(oldPath, []byte("content\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := gitCommit(root, "baseline", "old.txt"); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(oldPath, filepath.Join(root, "new.txt")); err != nil {
		t.Fatal(err)
	}
	files, err := gitWorkingFiles(root)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"new.txt", "old.txt"}
	if !reflect.DeepEqual(files, want) {
		t.Fatalf("rename files = %#v, want %#v", files, want)
	}
}

func TestGitChangeMetricsBindFilesLinesAndCommits(t *testing.T) {
	root := t.TempDir()
	if _, err := git(root, "init", "-b", "main"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	base, err := gitCommit(root, "base", "a.txt")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("one\ntwo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "b.txt"), []byte("three\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := gitCommit(root, "first", "a.txt", "b.txt"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "b.txt"), []byte("three\nfour\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	head, err := gitCommit(root, "second", "b.txt")
	if err != nil {
		t.Fatal(err)
	}
	metrics, err := gitChangeMetrics(root, base, head)
	if err != nil {
		t.Fatal(err)
	}
	want := ChangeMetrics{ChangedFiles: 2, AddedLines: 3, DeletedLines: 0, DiffLines: 3, Commits: 2, BinaryFiles: 0}
	if metrics != want {
		t.Fatalf("change metrics = %#v, want %#v", metrics, want)
	}
}
