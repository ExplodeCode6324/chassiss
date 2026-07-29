package verifier

import (
	"context"

	"github.com/ExplodeCode6324/chassiss/internal/contracts"
	"github.com/ExplodeCode6324/chassiss/internal/gitstore"
	"github.com/ExplodeCode6324/chassiss/internal/protocol"
	"github.com/ExplodeCode6324/chassiss/internal/state"
)

func (engine verifier) verifyWhitelist(
	ctx context.Context,
	parentTree, tree gitstore.TreeMap,
	parent *state.State,
	commit gitstore.Commit,
	message protocol.TransitionMessage,
	architecture *contracts.Architecture,
	taskbook *contracts.Taskbook,
) error {
	changed := gitstore.ChangedPaths(parentTree, tree)
	action := message.Operation.Action
	exact := func(paths ...string) bool {
		if len(changed) != len(paths) {
			return false
		}
		expected := map[string]struct{}{}
		for _, path := range paths {
			expected[path] = struct{}{}
		}
		for _, path := range changed {
			if _, exists := expected[path]; !exists {
				return false
			}
		}
		return true
	}
	switch action {
	case "architecture.established", "architecture.updated":
		if !exact(".chassiss/state.json", "docs/architecture.yaml") {
			return whitelistError(action, changed)
		}
	case "taskbook.opened", "taskbook.updated":
		if !exact(".chassiss/state.json", "docs/taskbook.yaml") {
			return whitelistError(action, changed)
		}
	case "taskbook.archived":
		if parent.Project.Taskbook == nil {
			return whitelistError(action, changed)
		}
		archive := "docs/taskbooks/archive/" + parent.Project.Taskbook.ID + ".yaml"
		if !exact(".chassiss/state.json", "docs/taskbook.yaml", archive) ||
			parentTree["docs/taskbook.yaml"].OID != tree[archive].OID {
			return whitelistError(action, changed)
		}
	case "integration.applied":
		task := parent.Tasks[message.Operation.Target]
		if task.Contract == nil {
			return whitelistError(action, changed)
		}
		contract, _, _, err := engine.effectiveTaskContract(ctx, parent, message.Operation.Target)
		if err != nil {
			return err
		}
		ordinary := make([]string, 0)
		for _, path := range changed {
			if path != ".chassiss/state.json" {
				ordinary = append(ordinary, path)
			}
		}
		if err := validateChangedPaths(ordinary, contract.Writes); err != nil {
			return err
		}
	case "owner.applied":
		if parentTree[".chassiss/state.json"] != tree[".chassiss/state.json"] {
			return whitelistError(action, changed)
		}
		for _, path := range changed {
			if contracts.IsProtectedPath(path) {
				return whitelistError(action, changed)
			}
		}
	default:
		if !exact(".chassiss/state.json") {
			return whitelistError(action, changed)
		}
	}
	return nil
}

func whitelistError(action string, changed []string) error {
	return &protocol.Error{
		Code: protocol.ErrProtectedPathChanged, Category: protocol.CategoryProtocol,
		Message: "Transition changed paths outside the Action whitelist.",
		Details: map[string]any{"action": action, "paths": changed},
	}
}
