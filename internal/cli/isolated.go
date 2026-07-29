package cli

import (
	"context"
	"os"
	"path/filepath"

	"github.com/ExplodeCode6324/chassiss/internal/gitstore"
	"github.com/ExplodeCode6324/chassiss/internal/localstate"
)

// withIsolatedTree creates a real linked Git worktree so frozen Checks that
// inspect Git can run, while replacing its index/worktree with the exact tree
// under test. The temporary repo instance exists only for the Check window.
func withIsolatedTree(
	ctx context.Context,
	project *projectContext,
	tree string,
	run func(string) error,
) error {
	tempRoot, err := os.MkdirTemp("", "chassiss-isolated-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tempRoot)
	checkout := filepath.Join(tempRoot, "checkout")
	if _, err := project.Runner.Run(
		ctx, "worktree", "add", "--detach", "--no-checkout",
		checkout, project.Verified.Head,
	); err != nil {
		return err
	}
	defer project.Runner.Run(ctx, "worktree", "remove", "--force", checkout)
	runner := gitstore.New(checkout)
	if _, err := runner.Run(ctx, "read-tree", "--reset", "-u", tree); err != nil {
		return err
	}
	gitDir, err := runner.Run(ctx, "rev-parse", "--absolute-git-dir")
	if err != nil {
		return err
	}
	instance := localstate.RepoInstance{
		CreatedByCLI: false, GitDir: stringTrimSpace(gitDir.Stdout),
		LastVerifiedHead: project.Verified.Head, RepoPath: checkout,
	}
	if err := project.Store.Update(func(local *localstate.State) error {
		value := local.Projects[project.Verified.State.Project.ID]
		value.RepoInstances = append(value.RepoInstances, instance)
		local.Projects[project.Verified.State.Project.ID] = value
		return nil
	}); err != nil {
		return err
	}
	defer project.Store.Update(func(local *localstate.State) error {
		value := local.Projects[project.Verified.State.Project.ID]
		instances := value.RepoInstances[:0]
		for _, current := range value.RepoInstances {
			if !samePath(current.RepoPath, checkout) {
				instances = append(instances, current)
			}
		}
		value.RepoInstances = instances
		local.Projects[project.Verified.State.Project.ID] = value
		return nil
	})
	return run(checkout)
}
