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
	command := exec.Command(name, args...)
	command.Dir = root
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
