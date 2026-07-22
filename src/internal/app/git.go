package app

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

func runCommand(root, name string, args ...string) (string, error) {
	return runCommandEnvironment(root, nil, name, args...)
}

func runCommandEnvironment(root string, extraEnvironment []string, name string, args ...string) (string, error) {
	output, err := runCommandEnvironmentRaw(root, extraEnvironment, name, args...)
	return strings.TrimSpace(string(output)), err
}

func runCommandEnvironmentRaw(root string, extraEnvironment []string, name string, args ...string) ([]byte, error) {
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
		return stdout.Bytes(), fmt.Errorf("%s", message)
	}
	return stdout.Bytes(), nil
}

func gitEnvironment(root string, environment []string, args ...string) (string, error) {
	return runCommandEnvironment(root, environment, "git", args...)
}

func gitEnvironmentRaw(root string, environment []string, args ...string) ([]byte, error) {
	return runCommandEnvironmentRaw(root, environment, "git", args...)
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
	output, err := gitEnvironmentRaw(root, nil, "diff", "--name-only", "-z", "--no-renames", base, head, "--")
	if err != nil {
		return nil, err
	}
	files := splitGitPaths(output)
	sort.Strings(files)
	return files, nil
}

func gitChangeMetrics(root, base, head string) (ChangeMetrics, error) {
	files, err := gitChangedFiles(root, base, head)
	if err != nil {
		return ChangeMetrics{}, err
	}
	metrics := ChangeMetrics{ChangedFiles: len(files)}
	numstat, err := gitEnvironmentRaw(root, nil, "diff", "--numstat", "-z", "--no-renames", base, head, "--")
	if err != nil {
		return ChangeMetrics{}, err
	}
	for _, record := range bytes.Split(numstat, []byte{0}) {
		if len(record) == 0 {
			continue
		}
		fields := bytes.SplitN(record, []byte{'\t'}, 3)
		if len(fields) != 3 {
			return ChangeMetrics{}, fmt.Errorf("invalid git numstat record")
		}
		if string(fields[0]) == "-" || string(fields[1]) == "-" {
			metrics.BinaryFiles++
			continue
		}
		added, addErr := strconv.Atoi(string(fields[0]))
		deleted, deleteErr := strconv.Atoi(string(fields[1]))
		if addErr != nil || deleteErr != nil || added < 0 || deleted < 0 {
			return ChangeMetrics{}, fmt.Errorf("invalid git numstat counts")
		}
		metrics.AddedLines += added
		metrics.DeletedLines += deleted
	}
	metrics.DiffLines = metrics.AddedLines + metrics.DeletedLines
	commitCount, err := git(root, "rev-list", "--count", base+".."+head)
	if err != nil {
		return ChangeMetrics{}, err
	}
	metrics.Commits, err = strconv.Atoi(commitCount)
	if err != nil || metrics.Commits < 0 {
		return ChangeMetrics{}, fmt.Errorf("invalid git commit count")
	}
	return metrics, nil
}

func gitWorkingFiles(root string) ([]string, error) {
	tracked, err := gitEnvironmentRaw(root, nil, "diff", "--name-only", "-z", "--no-renames", "HEAD", "--")
	if err != nil {
		return nil, err
	}
	untracked, err := gitEnvironmentRaw(root, nil, "ls-files", "--others", "--exclude-standard", "-z")
	if err != nil {
		return nil, err
	}
	set := map[string]struct{}{}
	for _, output := range [][]byte{tracked, untracked} {
		for _, line := range splitGitPaths(output) {
			path := filepath.ToSlash(line)
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
	untracked, err := gitEnvironmentRaw(root, nil, "ls-files", "--others", "--exclude-standard", "-z")
	if err != nil {
		return "", err
	}
	parts := []string{}
	if tracked != "" {
		parts = append(parts, tracked)
	}
	for _, path := range splitGitPaths(untracked) {
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
	return gitSnapshotDigest(root, "HEAD", true)
}

func gitCommitSnapshotDigest(root, commit string) (string, error) {
	return gitSnapshotDigest(root, commit, false)
}

func gitSnapshotDigest(root, treeish string, includeWorktree bool) (string, error) {
	cacheDir := filepath.Join(root, ".git")
	commonDir, err := git(root, "rev-parse", "--git-common-dir")
	if err == nil {
		if !filepath.IsAbs(commonDir) {
			commonDir = filepath.Join(root, commonDir)
		}
		cacheDir = commonDir
	}
	temporary, err := os.CreateTemp(cacheDir, "chassiss-digest-index-*")
	if err != nil {
		return "", err
	}
	indexPath := temporary.Name()
	if err := temporary.Close(); err != nil {
		return "", err
	}
	if err := os.Remove(indexPath); err != nil {
		return "", err
	}
	defer os.Remove(indexPath)
	environment := []string{"GIT_INDEX_FILE=" + indexPath}
	if _, err := gitEnvironment(root, environment, "read-tree", treeish); err != nil {
		return "", err
	}
	if includeWorktree {
		if _, err := gitEnvironment(root, environment, "add", "-A", "--", "."); err != nil {
			return "", err
		}
	}
	tree, err := gitEnvironment(root, environment, "write-tree")
	if err != nil {
		return "", err
	}
	stages, err := gitEnvironmentRaw(root, environment, "ls-files", "--stage", "-z")
	if err != nil {
		return "", err
	}
	manifest := struct {
		Tree        string `json:"tree"`
		StageDigest string `json:"stage_digest"`
	}{Tree: tree, StageDigest: digestBytes(stages)}
	data, err := canonicalJSON(manifest)
	if err != nil {
		return "", err
	}
	return digestBytes(data), nil
}

func splitGitPaths(output []byte) []string {
	if len(output) == 0 {
		return []string{}
	}
	parts := bytes.Split(output, []byte{0})
	files := make([]string, 0, len(parts))
	for _, path := range parts {
		if len(path) != 0 {
			files = append(files, filepath.ToSlash(string(path)))
		}
	}
	return files
}

func currentBranch(root string) (string, error) {
	return git(root, "branch", "--show-current")
}

func taskWorktreeRelativePath(taskID string) string {
	return filepath.ToSlash(filepath.Join(".chassis", "worktrees", strings.ToLower(taskID)))
}

func taskWorktreeBindingID(taskID, worktreePath, branch string) string {
	data, _ := canonicalJSON(struct {
		TaskID       string `json:"task_id"`
		WorktreePath string `json:"worktree_path"`
		Branch       string `json:"branch"`
	}{TaskID: taskID, WorktreePath: worktreePath, Branch: branch})
	return digestBytes(data)
}

func gitWorktreeIdentity(root string) (string, error) {
	gitDir, err := git(root, "rev-parse", "--absolute-git-dir")
	if err != nil {
		return "", err
	}
	identity := filepath.Base(filepath.Clean(gitDir))
	if identity == "" || identity == ".git" {
		return "", &CLIError{Code: "CHS-WORKTREE-IDENTITY", Message: "path is not a linked task worktree", ExitCode: 40}
	}
	return identity, nil
}

func taskWorktreeRoot(projectRoot string, task TaskState) (string, error) {
	if task.WorktreePath == "" || task.WorktreeID == "" || task.WorktreeDigest == "" {
		return "", &CLIError{Code: "CHS-WORKTREE-MISSING", Message: "task has no bound worktree", ExitCode: 10}
	}
	worktreeRoot, err := pathWithin(projectRoot, task.WorktreePath)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(worktreeRoot)
	if os.IsNotExist(err) {
		return "", &CLIError{Code: "CHS-WORKTREE-MISSING", Message: "task worktree was deleted or moved", ExitCode: 40}
	}
	if err != nil || !info.IsDir() {
		return "", &CLIError{Code: "CHS-WORKTREE-MISSING", Message: "task worktree path is invalid", ExitCode: 40}
	}
	top, err := git(worktreeRoot, "rev-parse", "--show-toplevel")
	if err != nil || !samePath(top, worktreeRoot) {
		return "", &CLIError{Code: "CHS-WORKTREE-IDENTITY", Message: "task path is not the expected Git worktree", ExitCode: 40}
	}
	if task.WorktreeDigest != taskWorktreeBindingID(task.ID, task.WorktreePath, task.Branch) {
		return "", &CLIError{Code: "CHS-WORKTREE-IDENTITY", Message: "task worktree binding digest changed", ExitCode: 40}
	}
	actualIdentity, err := gitWorktreeIdentity(worktreeRoot)
	if err != nil || actualIdentity != task.WorktreeID {
		return "", &CLIError{Code: "CHS-WORKTREE-IDENTITY", Message: "task worktree identity changed", ExitCode: 40}
	}
	branch, err := currentBranch(worktreeRoot)
	if err != nil || branch != task.Branch {
		return "", &CLIError{Code: "CHS-WORKTREE-BRANCH", Message: "task worktree is on the wrong branch", ExitCode: 40}
	}
	return worktreeRoot, nil
}
