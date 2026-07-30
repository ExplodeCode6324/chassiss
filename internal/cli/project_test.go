package cli

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	goruntime "runtime"
	"testing"

	"github.com/ExplodeCode6324/chassiss/internal/localstate"
	"github.com/ExplodeCode6324/chassiss/internal/protocol"
)

func TestResolveProjectLocationRejectsAmbiguousOwners(t *testing.T) {
	repository := t.TempDir()
	for name, projects := range map[string]map[string]localstate.Project{
		"multiple_projects": {
			"PRJ-AMBIGUOUS-A": {
				RepoInstances: []localstate.RepoInstance{{RepoPath: repository}},
			},
			"PRJ-AMBIGUOUS-B": {
				RepoInstances: []localstate.RepoInstance{{RepoPath: repository}},
			},
		},
		"multiple_owner_records": {
			"PRJ-AMBIGUOUS-A": {
				RepoInstances: []localstate.RepoInstance{{RepoPath: repository}},
				Worktrees: map[string]localstate.Worktree{
					"TASK-001": {Path: repository},
				},
			},
		},
	} {
		t.Run(name, func(t *testing.T) {
			local := &localstate.State{Projects: projects}
			_, _, _, err := resolveProjectLocation(context.Background(), local, repository)
			var failure *protocol.Error
			if !errors.As(err, &failure) || failure.Code != protocol.ErrLocalStateCorrupt {
				t.Fatalf("ambiguous owner records were not rejected deterministically: %v", err)
			}
			owners, ok := failure.Details["owners"].([]string)
			if !ok || len(owners) != 2 || owners[0] >= owners[1] {
				t.Fatalf("ambiguous owners were not returned in stable order: %#v", failure.Details)
			}
		})
	}
}

func TestRequireOutsideRegisteredProjectCoversEveryCanonicalTree(t *testing.T) {
	root := t.TempDir()
	main := filepath.Join(root, "main")
	clone := filepath.Join(root, "clone")
	worktree := filepath.Join(root, "worktree")
	outside := filepath.Join(root, "outside")
	for _, directory := range []string{main, clone, worktree, outside} {
		if err := os.MkdirAll(directory, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	project := localstate.Project{
		RepoInstances: []localstate.RepoInstance{
			{RepoPath: main},
			{RepoPath: clone},
		},
		Worktrees: map[string]localstate.Worktree{
			"TASK-001": {Path: worktree},
		},
	}
	assertScopeViolation := func(output string) {
		t.Helper()
		err := requireOutsideRegisteredProject(project, output)
		var failure *protocol.Error
		if !errors.As(err, &failure) || failure.Code != protocol.ErrScopeViolation {
			t.Fatalf("output %q was not refused with CHS_SCOPE_VIOLATION: %v", output, err)
		}
	}
	for _, boundary := range []string{main, clone, worktree} {
		assertScopeViolation(filepath.Join(boundary, "not-yet-created", "artifact.json"))
	}
	if err := requireOutsideRegisteredProject(
		project, filepath.Join(outside, "not-yet-created", "artifact.json"),
	); err != nil {
		t.Fatalf("legitimate outside output was refused: %v", err)
	}
	for label, boundary := range map[string]string{
		"main": main, "clone": clone, "worktree": worktree,
	} {
		alias := filepath.Join(outside, "alias-"+label)
		if err := os.Symlink(boundary, alias); err != nil {
			if goruntime.GOOS == "windows" {
				continue
			}
			t.Fatal(err)
		}
		assertScopeViolation(filepath.Join(alias, "not-yet-created", "artifact.json"))
	}
	danglingAlias := filepath.Join(outside, "dangling-alias")
	if err := os.Symlink(filepath.Join(main, "missing-target"), danglingAlias); err != nil {
		if goruntime.GOOS != "windows" {
			t.Fatal(err)
		}
	} else {
		assertScopeViolation(filepath.Join(danglingAlias, "artifact.json"))
	}
}

func TestGrantRequestForUnregisteredProjectKeepsOutputAvailable(t *testing.T) {
	workspace := t.TempDir()
	t.Setenv("CHASSISS_DATA_DIR", filepath.Join(t.TempDir(), "local-data"))
	previous, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(workspace); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(previous)
	var stdout, stderr bytes.Buffer
	runJSON(t, []string{
		"key", "generate", "--id", "KEY-UNREGISTERED-01", "--actor", "unregistered-agent",
	}, &stdout, &stderr)
	output := filepath.Join(workspace, "grant-request.json")
	request := runJSON(t, []string{
		"grant", "request", "--project", "PRJ-UNREGISTERED",
		"--key", "KEY-UNREGISTERED-01", "--profile", "developer",
		"--task-scope", "TASK-*", "--resource-scope", "module:*",
		"--limits", "bounded", "--output", output,
	}, &stdout, &stderr)
	if request.Result.(map[string]any)["output"] != output {
		t.Fatalf("unregistered Project Grant Request output failed: %#v", request)
	}
	if _, err := os.Stat(output); err != nil {
		t.Fatalf("unregistered Project Grant Request was not written: %v", err)
	}
}
