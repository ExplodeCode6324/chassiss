package app

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestProjectLockCannotBeStolenBecauseOfAge(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".chassis"), 0o755); err != nil {
		t.Fatal(err)
	}
	first, err := acquireLock(root)
	if err != nil {
		t.Fatal(err)
	}
	defer first.release()
	lockPath := filepath.Join(root, ".chassis", "lock")
	old := time.Now().Add(-24 * time.Hour)
	if err := os.Chtimes(lockPath, old, old); err != nil {
		t.Fatal(err)
	}
	if _, err := acquireLock(root); err == nil {
		t.Fatal("an old mtime allowed a live project lock to be stolen")
	} else {
		var typed *CLIError
		if !errors.As(err, &typed) || typed.Code != "CHS-CONFLICT-LOCKED" {
			t.Fatalf("second lock error = %#v", err)
		}
	}
	first.release()
	second, err := acquireLock(root)
	if err != nil {
		t.Fatalf("released advisory lock could not be reacquired: %v", err)
	}
	second.release()
	data, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "pid=") || !strings.Contains(string(data), "acquired_at=") {
		t.Fatalf("lock diagnostics are incomplete: %q", data)
	}
}

func TestProjectLockIsReleasedWhenOwnerProcessExits(t *testing.T) {
	if os.Getenv("CHASSISS_LOCK_EXIT_HELPER") == "1" {
		if _, err := acquireLock(os.Getenv("CHASSISS_LOCK_TEST_ROOT")); err != nil {
			os.Exit(2)
		}
		os.Exit(0)
	}
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".chassis"), 0o755); err != nil {
		t.Fatal(err)
	}
	command := exec.Command(os.Args[0], "-test.run=^TestProjectLockIsReleasedWhenOwnerProcessExits$")
	command.Env = append(os.Environ(), "CHASSISS_LOCK_EXIT_HELPER=1", "CHASSISS_LOCK_TEST_ROOT="+root)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("lock helper failed: %v: %s", err, output)
	}
	lock, err := acquireLock(root)
	if err != nil {
		t.Fatalf("process exit left a stale advisory lock: %v", err)
	}
	lock.release()
}
