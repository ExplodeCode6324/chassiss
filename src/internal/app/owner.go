package app

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

const maxOwnerReasonBytes = 64 * 1024

type ownerOperationIntent struct {
	Reason string `json:"reason"`
}

func validateOwnerReason(reason string) error {
	if strings.TrimSpace(reason) == "" {
		return &CLIError{Code: "CHS-OWNER-REASON", Message: "Owner change reason must not be empty", ExitCode: 20}
	}
	if !utf8.ValidString(reason) || len(reason) > maxOwnerReasonBytes {
		return &CLIError{Code: "CHS-OWNER-REASON", Message: "Owner change reason must be valid UTF-8 and no larger than 64 KiB", ExitCode: 20}
	}
	return nil
}

func ownerCommitMessage(reason string) string {
	summary := ""
	for _, line := range strings.Split(reason, "\n") {
		if value := strings.TrimSpace(line); value != "" {
			summary = value
			break
		}
	}
	summary = strings.Map(func(value rune) rune {
		if unicode.IsControl(value) {
			return ' '
		}
		return value
	}, summary)
	summary = strings.Join(strings.Fields(summary), " ")
	runes := []rune(summary)
	const maxSummaryRunes = 100
	if len(runes) > maxSummaryRunes {
		summary = string(runes[:maxSummaryRunes])
	}
	return "Owner baseline: " + summary
}

func ownerApplyStateAllowed(state State) error {
	if state.ActiveMission != "" || state.Phase == "execution" {
		return &CLIError{
			Code: "CHS-OWNER-PROJECT-ACTIVE", Message: "Owner baseline changes require a quiescent project with no active Mission",
			ExitCode: 10, Remedy: []string{"finish or explicitly close the active Mission before applying an Owner change"},
		}
	}
	for _, task := range state.Tasks {
		if isActiveTaskStatus(task.Status) {
			return &CLIError{
				Code: "CHS-OWNER-PROJECT-ACTIVE", Message: "Owner baseline changes are forbidden while a Task is active",
				ExitCode: 10, Remedy: []string{"finish or explicitly close all active Tasks before applying an Owner change"},
			}
		}
	}
	for _, artifact := range state.Artifacts {
		if artifact.Status == "submitted" {
			return &CLIError{
				Code: "CHS-OWNER-PROJECT-ACTIVE", Message: "Owner baseline changes are forbidden while an artifact awaits review",
				ExitCode: 10, Remedy: []string{"accept or reject every pending artifact before applying an Owner change"},
			}
		}
	}
	return nil
}

func validateOwnerChangedFiles(state State, files []string) error {
	if !validChangedFiles(files) {
		return &CLIError{Code: "CHS-OWNER-FILES", Message: "Owner change files are empty or not canonical", ExitCode: 40}
	}
	protected := map[string]string{}
	for _, artifact := range state.Artifacts {
		if artifact.Path != "" {
			protected[artifact.Path] = artifact.ID
		}
	}
	for _, file := range files {
		if ownerControlPath(file) {
			return &CLIError{Code: "CHS-OWNER-PROTECTED", Message: "Owner changes cannot modify CHASSISS or Git control data: " + file, ExitCode: 10}
		}
		if artifactID := protected[file]; artifactID != "" {
			return &CLIError{
				Code: "CHS-OWNER-PROTECTED", Message: fmt.Sprintf("Owner changes cannot modify managed artifact %s at %s", artifactID, file),
				ExitCode: 10, Remedy: []string{"revise managed artifacts through the normal Designer and Master acceptance flow"},
			}
		}
	}
	return nil
}

func ownerControlPath(file string) bool {
	return file == ".chassis" || strings.HasPrefix(file, ".chassis/") || file == ".git" || strings.HasPrefix(file, ".git/")
}

func ownerApply(root, reason string, principal Principal, expected int64) (State, State, OwnerChange, error) {
	if err := validateOwnerReason(reason); err != nil {
		return State{}, State{}, OwnerChange{}, err
	}
	changeID, err := newID("OWN")
	if err != nil {
		return State{}, State{}, OwnerChange{}, err
	}
	commitMessage := ownerCommitMessage(reason)
	intent := ownerOperationIntent{Reason: reason}
	previous, next, _, err := executeGitOperation(root, "owner.apply", "owner.baseline_applied", changeID, principal, expected, intent, func(current State) (preparedOperation, error) {
		if err := ownerApplyStateAllowed(current); err != nil {
			return preparedOperation{}, err
		}
		if !scopeAllows(principal.Resources.Baselines, current.Baseline) {
			return preparedOperation{}, &CLIError{Code: "CHS-AUTH-RESOURCE", Message: "Owner credential is not scoped to the current formal baseline", ExitCode: 11}
		}
		config, _, _, err := loadProject(root)
		if err != nil {
			return preparedOperation{}, err
		}
		branch, err := currentBranch(root)
		if err != nil {
			return preparedOperation{}, err
		}
		if branch != config.DefaultBranch {
			return preparedOperation{}, &CLIError{
				Code: "CHS-OWNER-BRANCH", Message: "Owner changes must be applied from the configured default branch",
				ExitCode: 10, Remedy: []string{"switch to " + config.DefaultBranch + " without moving its head"},
			}
		}
		head, err := gitHead(root)
		if err != nil {
			return preparedOperation{}, err
		}
		if head != current.Baseline {
			return preparedOperation{}, &CLIError{
				Code: "CHS-OWNER-BASELINE-MOVED", Message: "current Git head does not match the formal baseline",
				ExitCode: 10, Remedy: []string{"restore the default branch to the signed formal baseline; Owner apply does not adopt pre-existing commits"},
			}
		}
		files, err := gitWorkingFiles(root)
		if err != nil {
			return preparedOperation{}, err
		}
		if len(files) == 0 {
			return preparedOperation{}, &CLIError{Code: "CHS-OWNER-NO-CHANGES", Message: "there are no local files to apply as an Owner change", ExitCode: 10}
		}
		if err := validateOwnerChangedFiles(current, files); err != nil {
			return preparedOperation{}, err
		}
		before, commit, err := gitPrepareCommit(root, commitMessage, files...)
		if err != nil {
			return preparedOperation{}, err
		}
		if before != current.Baseline {
			return preparedOperation{}, &CLIError{Code: "CHS-OWNER-BASELINE-MOVED", Message: "formal baseline moved while preparing the Owner change", ExitCode: 12, Retryable: true}
		}
		changedFiles, err := gitChangedFiles(root, before, commit)
		if err != nil {
			return preparedOperation{}, err
		}
		if err := validateOwnerChangedFiles(current, changedFiles); err != nil {
			return preparedOperation{}, err
		}
		metrics, err := gitChangeMetrics(root, before, commit)
		if err != nil {
			return preparedOperation{}, err
		}
		if metrics.Commits != 1 || metrics.ChangedFiles != len(changedFiles) {
			return preparedOperation{}, &CLIError{Code: "CHS-OWNER-EVIDENCE", Message: "Owner change must produce exactly one internally prepared commit", ExitCode: 40}
		}
		if err := validateTaskBudget(TaskBudget{}, metrics); err != nil {
			return preparedOperation{}, err
		}
		treeDigest, err := gitCommitSnapshotDigest(root, commit)
		if err != nil {
			return preparedOperation{}, err
		}
		indexTree, err := git(root, "rev-parse", commit+"^{tree}")
		if err != nil {
			return preparedOperation{}, err
		}
		payload := ownerBaselineAppliedPayload{
			OwnerChangeID: changeID, Reason: reason, PreviousHead: before, NewHead: commit,
			TreeDigest: treeDigest, ChangedFiles: changedFiles, CommitMessage: commitMessage, Metrics: metrics,
		}
		return preparedOperation{
			Payload: payload,
			GitAfter: GitOperationState{
				Branch: branch, Head: commit, IndexTree: indexTree,
			},
			ApplyGit: func() error { return applyPreparedCommit(root, branch, before, commit) },
		}, nil
	})
	if err != nil {
		return State{}, State{}, OwnerChange{}, err
	}
	return previous, next, next.OwnerChanges[changeID], nil
}
