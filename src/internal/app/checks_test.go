package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunCheckSpecPreservesArgvAndPathSpaces(t *testing.T) {
	root := filepath.Join(t.TempDir(), "project with spaces")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := git(root, "init", "-b", "main"); err != nil {
		t.Fatal(err)
	}
	result := runCheckSpec(root, CheckSpec{
		ID: "CHECK-ARGV", Argv: []string{"/usr/bin/printf", "%s|%s", "argument with spaces", "$(not-expanded)"},
		Cwd: ".", Env: map[string]string{}, TimeoutSeconds: 10,
	})
	if !result.Passed || result.Output != "argument with spaces|$(not-expanded)" {
		t.Fatalf("structured argv result = %#v", result)
	}
}

func TestRunCheckSpecUsesOnlySanitizedAndDeclaredEnvironment(t *testing.T) {
	root := t.TempDir()
	if _, err := git(root, "init", "-b", "main"); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CHASSISS_UNDECLARED_SECRET", "must-not-leak")
	result := runCheckSpec(root, CheckSpec{
		ID: "CHECK-ENV", Argv: []string{"/usr/bin/env"}, Cwd: ".",
		Env: map[string]string{"CHASSISS_DECLARED": "visible"}, TimeoutSeconds: 10,
	})
	if !result.Passed || !strings.Contains(result.Output, "CHASSISS_DECLARED=visible") {
		t.Fatalf("declared environment missing: %#v", result)
	}
	if strings.Contains(result.Output, "CHASSISS_UNDECLARED_SECRET") || strings.Contains(result.Output, "must-not-leak") {
		t.Fatalf("undeclared environment leaked: %s", result.Output)
	}
}

func TestRunCheckSpecTimesOut(t *testing.T) {
	root := t.TempDir()
	if _, err := git(root, "init", "-b", "main"); err != nil {
		t.Fatal(err)
	}
	result := runCheckSpec(root, CheckSpec{
		ID: "CHECK-TIMEOUT", Argv: []string{"/bin/sh", "-c", "sleep 5"}, Cwd: ".", Env: map[string]string{}, TimeoutSeconds: 1,
	})
	if result.Passed || result.ExitCode != 124 {
		t.Fatalf("timeout result = %#v", result)
	}
}

func TestRunCheckSpecRejectsSymlinkCwdEscape(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "project")
	external := filepath.Join(parent, "external")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(external, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := git(root, "init", "-b", "main"); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(external, filepath.Join(root, "escaped")); err != nil {
		t.Fatal(err)
	}
	result := runCheckSpec(root, CheckSpec{
		ID: "CHECK-CWD", Argv: []string{"true"}, Cwd: "escaped", Env: map[string]string{}, TimeoutSeconds: 10,
	})
	if result.Passed || result.ExitCode != 2 || !strings.Contains(result.Output, "escapes the task worktree") {
		t.Fatalf("escaped cwd result = %#v", result)
	}
}
