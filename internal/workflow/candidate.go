package workflow

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/ExplodeCode6324/chassiss/internal/gitstore"
	"github.com/ExplodeCode6324/chassiss/internal/state"
)

type Candidate struct {
	ChangedPaths []string
	StateBytes   []byte
	Tree         gitstore.TreeMap
	TreeOID      string
}

// BuildCandidate applies the exact base..Attempt delta to main and replaces
// State with the hypothetical integration.applied projection.
func BuildCandidate(
	ctx context.Context,
	runner gitstore.Runner,
	objectFormat string,
	mainTree gitstore.TreeMap,
	parent *state.State,
	taskID string,
) (Candidate, error) {
	task, exists := parent.Tasks[taskID]
	if !exists || task.Attempt == nil {
		return Candidate{}, fmt.Errorf("Task %s has no current Attempt", taskID)
	}
	base, err := runner.ReadTree(ctx, task.Base)
	if err != nil {
		return Candidate{}, err
	}
	workCommit, err := runner.ReadCommit(ctx, task.Attempt.Head)
	if err != nil {
		return Candidate{}, err
	}
	work, err := runner.ReadTree(ctx, workCommit.Tree)
	if err != nil {
		return Candidate{}, err
	}
	overlay, changed, err := gitstore.Overlay(base, work, mainTree)
	if err != nil {
		return Candidate{}, err
	}
	hypothetical, err := cloneState(parent)
	if err != nil {
		return Candidate{}, err
	}
	hypothetical.Tasks[taskID] = state.TaskState{Phase: "closed"}
	stateBytes, err := state.Encode(hypothetical, objectFormat)
	if err != nil {
		return Candidate{}, err
	}
	stateOID, err := gitstore.HashObject("blob", stateBytes, objectFormat)
	if err != nil {
		return Candidate{}, err
	}
	overlay[".chassiss/state.json"] = gitstore.Entry{Mode: "100644", OID: stateOID}
	treeOID, err := gitstore.TreeOID(overlay, objectFormat)
	if err != nil {
		return Candidate{}, err
	}
	return Candidate{ChangedPaths: changed, StateBytes: stateBytes, Tree: overlay, TreeOID: treeOID}, nil
}

func cloneState(value *state.State) (*state.State, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var result state.State
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	return &result, nil
}
