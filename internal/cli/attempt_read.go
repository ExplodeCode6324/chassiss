package cli

import (
	"context"

	"github.com/ExplodeCode6324/chassiss/internal/protocol"
	"github.com/ExplodeCode6324/chassiss/internal/state"
)

type attemptFailureRecord struct {
	Commit    string                    `json:"commit"`
	Failure   map[string]any            `json:"failure,omitempty"`
	Index     state.AttemptFailureIndex `json:"index"`
	Operation string                    `json:"operation"`
}

func attemptFailuresCommand(ctx context.Context, invocation invocation) (Envelope, error) {
	project, err := loadProject(ctx, "", true)
	if err != nil {
		return Envelope{}, err
	}
	taskID := invocation.Positionals[0]
	operationFilter := invocation.Value("operation")
	if operationFilter != "" {
		if err := protocol.ValidateOperationID(operationFilter); err != nil {
			return Envelope{}, usageError(err.Error())
		}
	}
	indices := make(map[string]state.AttemptFailureIndex)
	if project.Verified.State.Audit != nil {
		for _, index := range project.Verified.State.Audit.Failures {
			if index.Task == taskID {
				indices[index.OperationID] = index
			}
		}
	}
	records := make([]attemptFailureRecord, 0)
	for _, transition := range project.Verified.Transitions {
		if transition.Action != "attempt.abandoned" || transition.Target != taskID {
			continue
		}
		if operationFilter != "" && transition.OperationID != operationFilter {
			continue
		}
		index, exists := indices[transition.OperationID]
		if !exists {
			continue
		}
		record := attemptFailureRecord{
			Commit: transition.Commit, Index: index, Operation: transition.OperationID,
		}
		if operationFilter != "" {
			commit, err := project.Runner.ReadCommit(ctx, transition.Commit)
			if err != nil {
				return Envelope{}, err
			}
			message, err := protocol.ParseTransitionMessage(commit.Message, project.Verified.ObjectFormat)
			if err != nil {
				return Envelope{}, err
			}
			failure, ok := message.Operation.Payload["failure"].(map[string]any)
			if !ok {
				return Envelope{}, protocol.NewError(protocol.ErrOperationInvalid, protocol.CategoryProtocol, "Verified abandoned Attempt has no failure record.")
			}
			record.Failure = failure
		}
		records = append(records, record)
	}
	if operationFilter != "" && len(records) == 0 {
		return Envelope{}, protocol.NewError(protocol.ErrReferenceNotFound, protocol.CategoryValidation, "Requested failed Attempt does not exist in verified history.")
	}
	envelope := projectEnvelope("attempt failures", project)
	envelope.Result = map[string]any{"failures": records, "task": taskID}
	return envelope, nil
}
