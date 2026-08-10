package cli

import (
	"context"
	"reflect"
	"sort"

	"github.com/ExplodeCode6324/chassiss/internal/localstate"
	"github.com/ExplodeCode6324/chassiss/internal/protocol"
	"github.com/ExplodeCode6324/chassiss/internal/verifier"
)

type pendingDisposition string

const (
	pendingFailed     pendingDisposition = "failed"
	pendingPublished  pendingDisposition = "published"
	pendingUnresolved pendingDisposition = "unresolved"
)

type pendingReconciliation struct {
	Failed     []string `json:"failed"`
	Published  []string `json:"published"`
	Unresolved []string `json:"unresolved"`
}

type localCASReconciliation struct {
	Commit      string
	Disposition pendingDisposition
	Message     protocol.TransitionMessage
	Verified    *verifier.Result
}

func reconcileLocalCASFailure(
	ctx context.Context,
	project *projectContext,
	expected localstate.PendingOperation,
) (localCASReconciliation, error) {
	local, err := project.Store.Load()
	if err != nil {
		return localCASReconciliation{Disposition: pendingUnresolved}, err
	}
	value, exists := local.Projects[project.Verified.State.Project.ID]
	if !exists {
		return localCASReconciliation{Disposition: pendingUnresolved}, nil
	}
	current, err := verifier.Verify(ctx, project.Runner, "refs/heads/main", verifier.Options{
		ExpectedProject:         project.Verified.State.Project.ID,
		ExpectedRootFingerprint: value.RootFingerprint,
		MinimumCheckpoint:       value.MinimumCheckpoint.Commit,
	})
	if err != nil {
		return localCASReconciliation{Disposition: pendingUnresolved}, err
	}
	pending := expected
	if current, exists := value.PendingOperations[expected.OperationID]; exists &&
		samePendingAttempt(current, expected) {
		pending = current
	}
	disposition, publishedCommit := classifyPending(ctx, project, current, pending)
	switch disposition {
	case pendingPublished:
		commit, err := project.Runner.ReadCommit(ctx, publishedCommit)
		if err != nil {
			return localCASReconciliation{Disposition: pendingUnresolved}, err
		}
		message, err := protocol.ParseTransitionMessage(commit.Message, current.ObjectFormat)
		if err != nil {
			return localCASReconciliation{Disposition: pendingUnresolved}, err
		}
		return localCASReconciliation{
			Commit: publishedCommit, Disposition: disposition,
			Message: message, Verified: current,
		}, nil
	case pendingFailed:
		marked, err := markPendingFailedIfExact(project, expected.OperationID, pending)
		if err != nil {
			return localCASReconciliation{Disposition: pendingUnresolved}, err
		}
		if !marked {
			return localCASReconciliation{Disposition: pendingUnresolved}, nil
		}
	}
	return localCASReconciliation{Disposition: disposition}, nil
}

func reconcileAndCheckpointPending(
	ctx context.Context,
	project *projectContext,
	verified *verifier.Result,
) (pendingReconciliation, error) {
	result := pendingReconciliation{
		Failed: []string{}, Published: []string{}, Unresolved: []string{},
	}
	err := project.Store.Update(func(local *localstate.State) error {
		value := local.Projects[verified.State.Project.ID]
		for operationID, pending := range value.PendingOperations {
			if pending.Status == string(pendingFailed) {
				continue
			}
			disposition, _ := classifyPending(ctx, project, verified, pending)
			switch disposition {
			case pendingPublished:
				delete(value.PendingOperations, operationID)
				result.Published = append(result.Published, operationID)
			case pendingFailed:
				pending.Status = string(pendingFailed)
				value.PendingOperations[operationID] = pending
				result.Failed = append(result.Failed, operationID)
			default:
				result.Unresolved = append(result.Unresolved, operationID)
			}
		}
		if err := advanceCheckpoint(
			ctx, project.Runner, &value, project.RepoRoot, verified,
		); err != nil {
			return err
		}
		local.Projects[verified.State.Project.ID] = value
		return nil
	})
	sort.Strings(result.Failed)
	sort.Strings(result.Published)
	sort.Strings(result.Unresolved)
	return result, err
}

func markPendingFailedIfExact(
	project *projectContext,
	operationID string,
	expected localstate.PendingOperation,
) (bool, error) {
	marked := false
	err := project.Store.Update(func(local *localstate.State) error {
		value := local.Projects[project.Verified.State.Project.ID]
		current, exists := value.PendingOperations[operationID]
		if !exists || !reflect.DeepEqual(current, expected) {
			return nil
		}
		current.Status = string(pendingFailed)
		value.PendingOperations[operationID] = current
		local.Projects[project.Verified.State.Project.ID] = value
		marked = true
		return nil
	})
	return marked, err
}

func samePendingAttempt(left, right localstate.PendingOperation) bool {
	left.Status = ""
	right.Status = ""
	return reflect.DeepEqual(left, right)
}

func classifyPending(
	ctx context.Context,
	project *projectContext,
	verified *verifier.Result,
	pending localstate.PendingOperation,
) (pendingDisposition, string) {
	// Publication is proved only by the verified first-parent Transition index.
	// Failure is proved only when verified main has advanced past ExpectedMain,
	// excludes the exact candidate, and the candidate is directly bound to that
	// now-stale parent. Every other shape remains unresolved.
	if pending.CandidateCommit == nil {
		return pendingUnresolved, ""
	}
	candidate := *pending.CandidateCommit
	firstParent := map[string]struct{}{verified.Genesis: {}}
	for _, transition := range verified.Transitions {
		firstParent[transition.Commit] = struct{}{}
		if transition.OperationID != pending.OperationID {
			continue
		}
		if transition.OperationDigest == pending.OperationDigest {
			return pendingPublished, transition.Commit
		}
		return pendingUnresolved, ""
	}

	if pending.ExpectedMain == verified.Head {
		return pendingUnresolved, ""
	}
	if _, exists := firstParent[pending.ExpectedMain]; !exists {
		return pendingUnresolved, ""
	}
	ancestor, err := project.Runner.IsAncestor(ctx, candidate, verified.Head)
	if err != nil || ancestor {
		return pendingUnresolved, ""
	}
	commit, err := project.Runner.ReadCommit(ctx, candidate)
	if err != nil || len(commit.Parents) == 0 || commit.Parents[0] != pending.ExpectedMain {
		return pendingUnresolved, ""
	}
	message, err := protocol.ParseTransitionMessage(commit.Message, verified.ObjectFormat)
	if err != nil || message.Operation.OperationID != pending.OperationID {
		return pendingUnresolved, ""
	}
	operationDigest, err := protocol.ObjectDigest("operation", message.Operation)
	if err != nil || operationDigest != pending.OperationDigest {
		return pendingUnresolved, ""
	}
	evidenceDigest, err := protocol.ObjectDigest("execution-evidence", message.Evidence)
	if err != nil || evidenceDigest != pending.EvidenceDigest {
		return pendingUnresolved, ""
	}
	return pendingFailed, ""
}
