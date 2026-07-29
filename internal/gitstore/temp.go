package gitstore

import (
	"context"
	"os"
	"path/filepath"
)

func createOwnerOnlyTemp(directory, pattern string) (*os.File, error) {
	file, err := os.CreateTemp(directory, pattern)
	if err != nil {
		return nil, err
	}
	if err := file.Chmod(0o600); err != nil {
		file.Close()
		os.Remove(file.Name())
		return nil, err
	}
	return file, nil
}

func removeFile(path string) error {
	err := os.Remove(path)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

// CheckoutTree materializes a tree into an existing empty directory without
// touching the repository's worktree or index.
func (runner Runner) CheckoutTree(ctx context.Context, tree, directory string) error {
	absolute, err := filepath.Abs(directory)
	if err != nil {
		return err
	}
	info, err := os.Stat(absolute)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return os.ErrInvalid
	}
	entries, err := os.ReadDir(absolute)
	if err != nil {
		return err
	}
	if len(entries) != 0 {
		return os.ErrExist
	}
	gitDir, err := repositoryGitDir(ctx, runner)
	if err != nil {
		return err
	}
	index, err := newTemporaryIndex(gitDir)
	if err != nil {
		return err
	}
	defer index.Close()
	extra := map[string]string{"GIT_INDEX_FILE": index.Path}
	if _, err := runner.RunWithInput(ctx, nil, extra, "read-tree", tree); err != nil {
		return err
	}
	_, err = runner.RunWithInput(ctx, nil, extra, "checkout-index", "--all", "--prefix="+absolute+string(os.PathSeparator))
	return err
}

// WorkingDiff compares the exact worktree contents with revision through a
// disposable index. Unlike a normal one-revision git diff, it includes
// untracked files without changing the repository's real index.
func (runner Runner) WorkingDiff(ctx context.Context, revision string, stat bool, paths []string) ([]byte, error) {
	gitDir, err := repositoryGitDir(ctx, runner)
	if err != nil {
		return nil, err
	}
	index, err := newTemporaryIndex(gitDir)
	if err != nil {
		return nil, err
	}
	defer index.Close()
	extra := map[string]string{"GIT_INDEX_FILE": index.Path}
	if _, err := runner.RunWithInput(ctx, nil, extra, "read-tree", revision); err != nil {
		return nil, err
	}
	otherArgs := []string{"ls-files", "--others", "--exclude-standard", "-z"}
	if len(paths) > 0 {
		otherArgs = append(otherArgs, "--")
		otherArgs = append(otherArgs, paths...)
	}
	others, err := runner.RunWithInput(ctx, nil, extra, otherArgs...)
	if err != nil {
		return nil, err
	}
	untracked := splitNULPaths(others.Stdout)
	if len(untracked) > 0 {
		addArgs := []string{"add", "--intent-to-add", "--"}
		addArgs = append(addArgs, untracked...)
		if _, err := runner.RunWithInput(ctx, nil, extra, addArgs...); err != nil {
			return nil, err
		}
	}
	diffArgs := []string{"diff", "--no-ext-diff", "--no-textconv", "--no-renames"}
	if stat {
		diffArgs = append(diffArgs, "--stat")
	}
	if len(paths) > 0 {
		diffArgs = append(diffArgs, "--")
		diffArgs = append(diffArgs, paths...)
	}
	result, err := runner.RunWithInput(ctx, nil, extra, diffArgs...)
	if err != nil {
		return nil, err
	}
	return result.Stdout, nil
}

func splitNULPaths(data []byte) []string {
	result := make([]string, 0)
	start := 0
	for index, value := range data {
		if value != 0 {
			continue
		}
		if index > start {
			result = append(result, string(data[start:index]))
		}
		start = index + 1
	}
	if start < len(data) {
		result = append(result, string(data[start:]))
	}
	return result
}
