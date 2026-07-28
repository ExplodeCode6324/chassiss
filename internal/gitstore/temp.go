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
