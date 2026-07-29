package gitstore

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWorkingDiffIncludesUntrackedWithoutMutatingIndex(t *testing.T) {
	repository := t.TempDir()
	runner := New(repository)
	ctx := context.Background()
	if _, err := runner.Run(ctx, "init", "-b", "main"); err != nil {
		t.Fatal(err)
	}
	tracked := filepath.Join(repository, "tracked.txt")
	if err := os.WriteFile(tracked, []byte("before\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := runner.Run(ctx, "add", "tracked.txt"); err != nil {
		t.Fatal(err)
	}
	if _, err := runner.Run(
		ctx, "-c", "user.name=Test", "-c", "user.email=test@example.invalid",
		"commit", "-m", "base",
	); err != nil {
		t.Fatal(err)
	}
	indexBefore, err := runner.Run(ctx, "ls-files", "--stage", "-z")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tracked, []byte("after\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repository, "new.txt"), []byte("new content\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	diff, err := runner.WorkingDiff(ctx, "HEAD", false, nil)
	if err != nil {
		t.Fatal(err)
	}
	text := string(diff)
	if !strings.Contains(text, "-before") || !strings.Contains(text, "+after") ||
		!strings.Contains(text, "new.txt") || !strings.Contains(text, "+new content") {
		t.Fatalf("working diff omitted tracked or untracked content:\n%s", text)
	}
	indexAfter, err := runner.Run(ctx, "ls-files", "--stage", "-z")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(indexBefore.Stdout, indexAfter.Stdout) {
		t.Fatal("WorkingDiff mutated the real Git index")
	}
}
