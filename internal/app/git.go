package app

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

func runCommand(root, name string, args ...string) (string, error) {
	return runCommandEnvironment(root, nil, name, args...)
}

func runCommandEnvironment(root string, extraEnvironment []string, name string, args ...string) (string, error) {
	command := exec.Command(name, args...)
	command.Dir = root
	if len(extraEnvironment) != 0 {
		command.Env = append(os.Environ(), extraEnvironment...)
	}
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	err := command.Run()
	if err != nil {
		message := strings.TrimSpace(stderr.String())
		if message == "" {
			message = err.Error()
		}
		return strings.TrimSpace(stdout.String()), fmt.Errorf("%s", message)
	}
	return strings.TrimSpace(stdout.String()), nil
}

func gitEnvironment(root string, environment []string, args ...string) (string, error) {
	return runCommandEnvironment(root, environment, "git", args...)
}

func git(root string, args ...string) (string, error) {
	return runCommand(root, "git", args...)
}

func gitCommit(root, message string, paths ...string) (string, error) {
	addArgs := append([]string{"add", "--"}, paths...)
	if _, err := git(root, addArgs...); err != nil {
		return "", err
	}
	commitArgs := []string{"-c", "user.name=CHASSISS", "-c", "user.email=chassiss@local.invalid", "commit", "-m", message}
	if _, err := git(root, commitArgs...); err != nil {
		return "", err
	}
	return git(root, "rev-parse", "HEAD")
}

func gitPrepareCommit(root, message string, paths ...string) (string, string, error) {
	before, err := gitHead(root)
	if err != nil {
		return "", "", err
	}
	cacheDir := filepath.Join(root, ".chassis", "cache")
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return "", "", err
	}
	temporary, err := os.CreateTemp(cacheDir, "prepare-index-*")
	if err != nil {
		return "", "", err
	}
	indexPath := temporary.Name()
	if err := temporary.Close(); err != nil {
		return "", "", err
	}
	if err := os.Remove(indexPath); err != nil {
		return "", "", err
	}
	defer os.Remove(indexPath)
	environment := []string{"GIT_INDEX_FILE=" + indexPath}
	if _, err := gitEnvironment(root, environment, "read-tree", before); err != nil {
		return "", "", err
	}
	addArgs := append([]string{"add", "--"}, paths...)
	if _, err := gitEnvironment(root, environment, addArgs...); err != nil {
		return "", "", err
	}
	tree, err := gitEnvironment(root, environment, "write-tree")
	if err != nil {
		return "", "", err
	}
	after, err := gitEnvironment(root, environment,
		"-c", "user.name=CHASSISS", "-c", "user.email=chassiss@local.invalid",
		"commit-tree", tree, "-p", before, "-m", message,
	)
	if err != nil {
		return "", "", err
	}
	return before, after, nil
}

func applyPreparedCommit(root, branch, before, after string) error {
	currentBranchName, err := currentBranch(root)
	if err != nil || currentBranchName != branch {
		return &CLIError{Code: "CHS-OPERATION-BRANCH", Message: "current branch changed while preparing commit", ExitCode: 40}
	}
	currentHead, err := gitHead(root)
	if err != nil || currentHead != before {
		return &CLIError{Code: "CHS-OPERATION-HEAD", Message: "branch head changed while preparing commit", ExitCode: 40}
	}
	if _, err := git(root, "update-ref", "refs/heads/"+branch, after, before); err != nil {
		return err
	}
	if err := injectOperationFault("git_ref_applied"); err != nil {
		return err
	}
	if _, err := git(root, "read-tree", "--reset", after); err != nil {
		return err
	}
	return nil
}

func gitDefaultBranch(root string) (string, error) {
	branch, err := git(root, "symbolic-ref", "--quiet", "--short", "HEAD")
	if err == nil && branch != "" {
		return branch, nil
	}
	return "main", nil
}

func gitHead(root string) (string, error) {
	return git(root, "rev-parse", "HEAD")
}

func gitClean(root string) (bool, string, error) {
	status, err := git(root, "status", "--porcelain")
	if err != nil {
		return false, "", err
	}
	return status == "", status, nil
}

func gitChangedFiles(root, base, head string) ([]string, error) {
	output, err := git(root, "diff", "--name-only", base, head, "--")
	if err != nil {
		return nil, err
	}
	if output == "" {
		return []string{}, nil
	}
	files := strings.Split(output, "\n")
	for index := range files {
		files[index] = filepath.ToSlash(strings.TrimSpace(files[index]))
	}
	sort.Strings(files)
	return files, nil
}

func gitWorkingFiles(root string) ([]string, error) {
	tracked, err := git(root, "diff", "--name-only", "HEAD", "--")
	if err != nil {
		return nil, err
	}
	untracked, err := git(root, "ls-files", "--others", "--exclude-standard")
	if err != nil {
		return nil, err
	}
	set := map[string]struct{}{}
	for _, output := range []string{tracked, untracked} {
		for _, line := range strings.Split(output, "\n") {
			path := filepath.ToSlash(strings.TrimSpace(line))
			if path != "" && !strings.HasPrefix(path, ".chassis/") {
				set[path] = struct{}{}
			}
		}
	}
	files := make([]string, 0, len(set))
	for path := range set {
		files = append(files, path)
	}
	sort.Strings(files)
	return files, nil
}

func gitWorkingDiff(root string) (string, error) {
	tracked, err := git(root, "diff", "HEAD", "--")
	if err != nil {
		return "", err
	}
	untracked, err := git(root, "ls-files", "--others", "--exclude-standard")
	if err != nil {
		return "", err
	}
	parts := []string{}
	if tracked != "" {
		parts = append(parts, tracked)
	}
	for _, path := range strings.Split(untracked, "\n") {
		path = strings.TrimSpace(path)
		if path == "" || strings.HasPrefix(filepath.ToSlash(path), ".chassis/") {
			continue
		}
		command := exec.Command("git", "diff", "--no-index", "--no-ext-diff", "--", "/dev/null", path)
		command.Dir = root
		output, runErr := command.CombinedOutput()
		var exitError *exec.ExitError
		if runErr != nil && (!errors.As(runErr, &exitError) || exitError.ExitCode() != 1) {
			return "", fmt.Errorf("cannot render diff for %s: %w", path, runErr)
		}
		if len(output) > 0 {
			parts = append(parts, strings.TrimSpace(string(output)))
		}
	}
	return strings.Join(parts, "\n"), nil
}

func gitWorktreeDigest(root string) (string, error) {
	files, err := gitWorkingFiles(root)
	if err != nil {
		return "", err
	}
	type entry struct {
		Path    string `json:"path"`
		Digest  string `json:"digest"`
		Deleted bool   `json:"deleted,omitempty"`
	}
	entries := make([]entry, 0, len(files))
	for _, path := range files {
		data, readErr := os.ReadFile(filepath.Join(root, filepath.FromSlash(path)))
		if os.IsNotExist(readErr) {
			entries = append(entries, entry{Path: path, Deleted: true})
			continue
		}
		if readErr != nil {
			return "", readErr
		}
		entries = append(entries, entry{Path: path, Digest: digestBytes(data)})
	}
	data, err := canonicalJSON(entries)
	if err != nil {
		return "", err
	}
	return digestBytes(data), nil
}

func currentBranch(root string) (string, error) {
	return git(root, "branch", "--show-current")
}
