package verifier

import (
	"bytes"
	"context"
	"fmt"
	"reflect"
	"sort"

	"github.com/ExplodeCode6324/chassiss/internal/contracts"
	"github.com/ExplodeCode6324/chassiss/internal/gitstore"
	"github.com/ExplodeCode6324/chassiss/internal/protocol"
	"github.com/ExplodeCode6324/chassiss/internal/state"
	"github.com/ExplodeCode6324/chassiss/internal/workflow"
)

// ClassifyDrift deterministically classifies the first-parent range that
// appeared after a Review Context was prepared.
func ClassifyDrift(
	ctx context.Context,
	runner gitstore.Runner,
	objectFormat string,
	parent *state.State,
	taskID string,
	contract contracts.Task,
	architecture *contracts.Architecture,
	reviewMain string,
	integrationParent string,
) (workflow.DriftClassification, error) {
	commits, changedPaths, transitionReasons, err := driftRange(
		ctx, runner, reviewMain, integrationParent, taskID,
	)
	if err != nil {
		return workflow.DriftClassification{}, err
	}
	reasons := append([]workflow.DriftReason(nil), transitionReasons...)
	affected := make([]string, 0)
	resources := architecture.Resources()
	closure, err := architecture.RequiresClosure(
		append(append([]string(nil), contract.Modules...), contract.Affects...),
	)
	if err != nil {
		return workflow.DriftClassification{}, err
	}
	closureSet := stringSet(closure)
	taskScopes, err := pathScopes(contract.Writes)
	if err != nil {
		return workflow.DriftClassification{}, err
	}
	for _, path := range changedPaths {
		if path == ".chassiss/state.json" {
			continue
		}
		if path == "docs/architecture.yaml" || path == "docs/taskbook.yaml" {
			reasons = append(reasons, workflow.DriftReason{Kind: "contract", Value: path})
			continue
		}
		if contracts.PathWithinAny(path, taskScopes) {
			reasons = append(reasons, workflow.DriftReason{Kind: "path", Value: path})
		}
		for resourceID, resource := range resources {
			scopes, scopeErr := pathScopes(resource.Paths)
			if scopeErr != nil {
				return workflow.DriftClassification{}, scopeErr
			}
			if contracts.PathWithinAny(path, scopes) {
				affected = append(affected, resourceID)
				if _, relevant := closureSet[resourceID]; relevant {
					reasons = append(reasons, workflow.DriftReason{Kind: "resource", Value: resourceID})
				}
			}
		}
	}
	affected = sortedUnique(affected)
	mainCommit, err := runner.ReadCommit(ctx, integrationParent)
	if err != nil {
		return workflow.DriftClassification{}, err
	}
	mainTree, err := runner.ReadTree(ctx, mainCommit.Tree)
	if err != nil {
		return workflow.DriftClassification{}, err
	}
	if _, err := workflow.BuildCandidate(ctx, runner, objectFormat, mainTree, parent, taskID); err != nil {
		reasons = append(reasons, workflow.DriftReason{Kind: "candidate-conflict", Value: "deterministic overlay failed"})
	}
	commitDigest, err := protocol.ObjectDigest("commit-range", commits)
	if err != nil {
		return workflow.DriftClassification{}, err
	}
	pathsDigest, err := protocol.ObjectDigest("changed-paths", changedPaths)
	if err != nil {
		return workflow.DriftClassification{}, err
	}
	classification := "unrelated"
	if len(reasons) > 0 {
		classification = "relevant"
	}
	result := workflow.DriftClassification{
		AffectedResources: affected, ChangedPathsDigest: pathsDigest,
		Classification: classification, CommitRangeDigest: commitDigest,
		IntegrationParent: integrationParent, Reasons: reasons, ReviewMain: reviewMain,
		Schema: workflow.DriftSchema, Task: taskID,
	}
	return result, result.Validate()
}

func driftRange(
	ctx context.Context,
	runner gitstore.Runner,
	reviewMain string,
	integrationParent string,
	taskID string,
) ([]string, []string, []workflow.DriftReason, error) {
	current := integrationParent
	reversed := make([]gitstore.Commit, 0)
	for len(reversed) < 100_000 && current != reviewMain {
		commit, err := runner.ReadCommit(ctx, current)
		if err != nil {
			return nil, nil, nil, err
		}
		if len(commit.Parents) == 0 {
			return nil, nil, nil, protocol.NewError(protocol.ErrReviewContextStale, protocol.CategoryReview, "Review main is not an ancestor of the Integration parent.")
		}
		reversed = append(reversed, commit)
		current = commit.Parents[0]
	}
	if current != reviewMain {
		return nil, nil, nil, protocol.NewError(protocol.ErrReviewContextStale, protocol.CategoryReview, "Drift range exceeds the verifier limit.")
	}
	commits := make([]string, len(reversed))
	changedSet := map[string]struct{}{}
	reasons := make([]workflow.DriftReason, 0)
	targetReviews := 0
	for index := len(reversed) - 1; index >= 0; index-- {
		commit := reversed[index]
		commits[len(reversed)-1-index] = commit.OID
		parentTree, err := runner.ReadTree(ctx, commit.Parents[0])
		if err != nil {
			return nil, nil, nil, err
		}
		tree, err := runner.ReadTree(ctx, commit.Tree)
		if err != nil {
			return nil, nil, nil, err
		}
		for _, path := range gitstore.ChangedPaths(parentTree, tree) {
			changedSet[path] = struct{}{}
		}
		message, err := protocol.ParseTransitionMessage(commit.Message, mustObjectFormat(ctx, runner))
		if err != nil {
			return nil, nil, nil, err
		}
		if message.Operation.Target != taskID {
			continue
		}
		switch message.Operation.Action {
		case "task.reviewed":
			targetReviews++
		case "task.blocked", "task.resumed":
			// A completed control pair is checked below.
		default:
			reasons = append(reasons, workflow.DriftReason{Kind: "unknown", Value: message.Operation.Action})
		}
	}
	if targetReviews != 1 {
		reasons = append(reasons, workflow.DriftReason{Kind: "unknown", Value: fmt.Sprintf("target review count %d", targetReviews)})
	}
	changed := make([]string, 0, len(changedSet))
	for path := range changedSet {
		changed = append(changed, path)
	}
	sort.Slice(changed, func(i, j int) bool {
		return bytes.Compare([]byte(changed[i]), []byte(changed[j])) < 0
	})
	return commits, changed, reasons, nil
}

func pathScopes(values []string) ([]contracts.PathScope, error) {
	result := make([]contracts.PathScope, 0, len(values))
	for _, value := range values {
		scope, err := contracts.ParsePathScope(value)
		if err != nil {
			return nil, err
		}
		result = append(result, scope)
	}
	return result, nil
}

func stringSet(values []string) map[string]struct{} {
	result := make(map[string]struct{}, len(values))
	for _, value := range values {
		result[value] = struct{}{}
	}
	return result
}

func sortedUnique(values []string) []string {
	sort.Strings(values)
	result := values[:0]
	for _, value := range values {
		if len(result) == 0 || result[len(result)-1] != value {
			result = append(result, value)
		}
	}
	return result
}

func mustObjectFormat(ctx context.Context, runner gitstore.Runner) string {
	format, err := runner.ObjectFormat(ctx)
	if err != nil {
		return "sha1"
	}
	return format
}

func equalDrift(left, right workflow.DriftClassification) bool {
	return reflect.DeepEqual(left, right)
}
