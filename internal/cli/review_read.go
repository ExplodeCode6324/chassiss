package cli

import (
	"context"
	"strconv"

	"github.com/ExplodeCode6324/chassiss/internal/protocol"
)

type reviewHistoryRecord struct {
	Attempt       int            `json:"attempt"`
	Commit        string         `json:"commit"`
	ContextDigest string         `json:"context_digest"`
	OperationID   string         `json:"operation_id"`
	Report        map[string]any `json:"report,omitempty"`
	ReportDigest  string         `json:"report_digest"`
	Task          string         `json:"task"`
	Verdict       string         `json:"verdict"`
}

func reviewReadCommand(ctx context.Context, invocation invocation) (Envelope, error) {
	project, err := loadProject(ctx, "", true)
	if err != nil {
		return Envelope{}, err
	}
	taskID := invocation.Positionals[0]
	records := make([]reviewHistoryRecord, 0)
	for _, transition := range project.Verified.Transitions {
		if (transition.Action != "task.reviewed" && transition.Action != "task.reviewed-indexed") ||
			transition.Target != taskID {
			continue
		}
		commit, err := project.Runner.ReadCommit(ctx, transition.Commit)
		if err != nil {
			return Envelope{}, err
		}
		message, err := protocol.ParseTransitionMessage(commit.Message, project.Verified.ObjectFormat)
		if err != nil {
			return Envelope{}, err
		}
		report, ok := message.Operation.Payload["report"].(map[string]any)
		if !ok {
			return Envelope{}, protocol.NewError(protocol.ErrReviewReportInvalid, protocol.CategoryProtocol, "Verified Review transition has no Report object.")
		}
		contextObject, ok := message.Evidence.Facts["review_context"].(map[string]any)
		if !ok {
			return Envelope{}, protocol.NewError(protocol.ErrEvidenceInvalid, protocol.CategoryProtocol, "Verified Review transition has no Review Context object.")
		}
		reportDigest, _ := protocol.ObjectDigest("review-report", report)
		contextDigest, _ := protocol.ObjectDigest("review-context", contextObject)
		verdict, _ := report["verdict"].(string)
		records = append(records, reviewHistoryRecord{
			Attempt: len(records) + 1, Commit: transition.Commit,
			ContextDigest: contextDigest, OperationID: transition.OperationID,
			Report: report, ReportDigest: reportDigest, Task: taskID,
			Verdict: verdict,
		})
	}
	envelope := projectEnvelope(invocation.Definition.Path, project)
	if invocation.Definition.Path == "review list" {
		indices := make([]reviewHistoryRecord, len(records))
		copy(indices, records)
		for index := range indices {
			indices[index].Report = nil
		}
		envelope.Result = map[string]any{"reviews": indices, "task": taskID}
		return envelope, nil
	}
	if invocation.Value("attempt") != "" && invocation.Value("operation") != "" {
		return Envelope{}, usageError("review show accepts only one of --attempt or --operation")
	}
	selected := -1
	if raw := invocation.Value("attempt"); raw != "" {
		attempt, err := strconv.Atoi(raw)
		if err != nil || attempt < 1 {
			return Envelope{}, usageError("--attempt must be a positive integer")
		}
		if attempt <= len(records) {
			selected = attempt - 1
		}
	} else if operationID := invocation.Value("operation"); operationID != "" {
		if err := protocol.ValidateOperationID(operationID); err != nil {
			return Envelope{}, usageError(err.Error())
		}
		for index := range records {
			if records[index].OperationID == operationID {
				selected = index
				break
			}
		}
	} else if len(records) > 0 {
		selected = len(records) - 1
	}
	if selected < 0 {
		return Envelope{}, protocol.NewError(protocol.ErrReferenceNotFound, protocol.CategoryValidation, "Requested Review Report does not exist in verified history.")
	}
	envelope.Result = map[string]any{"review": records[selected]}
	return envelope, nil
}
