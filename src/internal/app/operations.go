package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"time"
)

const operationVersion = 1

const (
	operationPrepared       = "prepared"
	operationGitApplied     = "git_applied"
	operationStateCommitted = "state_committed"
)

type GitOperationState struct {
	Branch            string `json:"branch"`
	Head              string `json:"head"`
	IndexTree         string `json:"index_tree"`
	WorktreePath      string `json:"worktree_path,omitempty"`
	WorktreePresent   bool   `json:"worktree_present,omitempty"`
	WorktreeBranch    string `json:"worktree_branch,omitempty"`
	WorktreeHead      string `json:"worktree_head,omitempty"`
	WorktreeIndexTree string `json:"worktree_index_tree,omitempty"`
	WorktreeID        string `json:"worktree_id,omitempty"`
}

type OperationJournal struct {
	Version          int               `json:"version"`
	ID               string            `json:"id"`
	Action           string            `json:"action"`
	Actor            string            `json:"actor"`
	Role             string            `json:"role"`
	CredentialID     string            `json:"credential_id"`
	Resource         string            `json:"resource"`
	ExpectedRevision int64             `json:"expected_revision"`
	Phase            string            `json:"phase"`
	GitBefore        GitOperationState `json:"git_before"`
	GitAfter         GitOperationState `json:"git_after,omitempty"`
	Intent           json.RawMessage   `json:"intent"`
	Event            *Event            `json:"event,omitempty"`
	RecoveryPolicy   string            `json:"recovery_policy"`
	CreatedAt        time.Time         `json:"created_at"`
	UpdatedAt        time.Time         `json:"updated_at"`
}

type preparedOperation struct {
	Payload  any
	GitAfter GitOperationState
	ApplyGit func() error
	Finalize func() error
}

type integrationOperationIntent struct {
	SubmissionID     string `json:"submission_id"`
	CandidatePath    string `json:"candidate_path"`
	TaskWorktreePath string `json:"task_worktree_path"`
}

type taskReleaseOperationIntent struct {
	TaskID       string `json:"task_id"`
	Branch       string `json:"branch"`
	Baseline     string `json:"baseline"`
	WorktreePath string `json:"worktree_path"`
}

// operationFaultHook is used only by crash-injection tests. A non-nil error
// simulates process loss: the journal is intentionally left for recover.
var operationFaultHook func(point string) error

func executeGitOperation(root, action, eventType, resource string, principal Principal, expected int64, intent any, prepare func(State) (preparedOperation, error)) (State, State, Event, error) {
	lock, err := acquireLock(root)
	if err != nil {
		return State{}, State{}, Event{}, err
	}
	defer lock.release()

	config, _, _, err := loadProject(root)
	if err != nil {
		return State{}, State{}, Event{}, err
	}
	previous, err := verifyProject(root)
	if err != nil {
		return State{}, State{}, Event{}, err
	}
	if expected >= 0 && previous.Revision != expected {
		return State{}, State{}, Event{}, &CLIError{Code: "CHS-CONFLICT-REVISION", Message: fmt.Sprintf("expected revision %d, current revision is %d", expected, previous.Revision), ExitCode: 12, Retryable: true, Remedy: []string{"run chassiss status", "re-evaluate the action"}}
	}
	if err := requireNoPendingOperations(root); err != nil {
		return State{}, State{}, Event{}, err
	}

	worktreePath := operationTaskWorktreePath(action, resource, previous)
	before, err := captureGitOperationStateFor(root, worktreePath)
	if err != nil {
		return State{}, State{}, Event{}, err
	}
	intentData, err := marshalPayload(intent)
	if err != nil {
		return State{}, State{}, Event{}, err
	}
	operationID, err := newID("OP")
	if err != nil {
		return State{}, State{}, Event{}, err
	}
	now := timeNow()
	journal := OperationJournal{
		Version: operationVersion, ID: operationID, Action: action, Actor: principal.Actor, Role: principal.Role, CredentialID: principal.ID,
		Resource: resource, ExpectedRevision: previous.Revision, Phase: operationPrepared, GitBefore: before, Intent: intentData,
		RecoveryPolicy: "complete only when Git matches the signed after-state; cancel untouched prepared operations; otherwise integrity-block",
		CreatedAt:      now, UpdatedAt: now,
	}
	if err := writeOperationJournal(root, journal); err != nil {
		return State{}, State{}, Event{}, err
	}
	if err := injectOperationFault("prepared"); err != nil {
		return State{}, State{}, Event{}, err
	}

	prepared, err := prepare(previous)
	if err != nil {
		_ = removeOperationJournal(root, journal.ID)
		return State{}, State{}, Event{}, err
	}
	if prepared.GitAfter.Branch == "" || prepared.GitAfter.Head == "" || prepared.ApplyGit == nil {
		_ = removeOperationJournal(root, journal.ID)
		return State{}, State{}, Event{}, &CLIError{Code: "CHS-OPERATION-PLAN", Message: "Git operation plan is incomplete", ExitCode: 40}
	}
	eventsPath := filepath.Join(root, ".chassis", "events")
	events, err := readEvents(eventsPath)
	if err != nil {
		return State{}, State{}, Event{}, err
	}
	previousDigest := ""
	if len(events) > 0 {
		previousDigest = events[len(events)-1].Digest
	}
	event, err := makeEvent(previous.ProjectID, previous.Revision+1, eventType, resource, principal, previousDigest, timeNow(), prepared.Payload)
	if err != nil {
		return State{}, State{}, Event{}, err
	}
	_, err = reduceEvent(config, previous, event)
	if err != nil {
		_ = removeOperationJournal(root, journal.ID)
		return State{}, State{}, Event{}, err
	}
	journal.GitAfter = prepared.GitAfter
	journal.Event = &event
	journal.UpdatedAt = timeNow()
	if err := writeOperationJournal(root, journal); err != nil {
		return State{}, State{}, Event{}, err
	}
	if err := injectOperationFault("objects_prepared"); err != nil {
		return State{}, State{}, Event{}, err
	}

	if err := prepared.ApplyGit(); err != nil {
		current, captureErr := captureGitOperationStateFor(root, journal.GitAfter.WorktreePath)
		if captureErr == nil && reflect.DeepEqual(current, journal.GitBefore) {
			_ = removeOperationJournal(root, journal.ID)
		}
		return State{}, State{}, Event{}, err
	}
	if err := injectOperationFault("git_applied_before_phase"); err != nil {
		return State{}, State{}, Event{}, err
	}
	current, err := captureGitOperationStateFor(root, journal.GitAfter.WorktreePath)
	if err != nil {
		return State{}, State{}, Event{}, err
	}
	if !reflect.DeepEqual(current, journal.GitAfter) {
		return State{}, State{}, Event{}, &CLIError{Code: "CHS-OPERATION-GIT-MISMATCH", Message: "Git result does not match operation journal", ExitCode: 40, Remedy: []string{"do not reset or force the branch", "inspect the operation journal"}}
	}
	journal.Phase = operationGitApplied
	journal.UpdatedAt = timeNow()
	if err := writeOperationJournal(root, journal); err != nil {
		return State{}, State{}, Event{}, err
	}
	if err := injectOperationFault("git_applied"); err != nil {
		return State{}, State{}, Event{}, err
	}

	committed, err := commitOperationEventLocked(root, config, journal)
	if err != nil {
		return State{}, State{}, Event{}, err
	}
	journal.Phase = operationStateCommitted
	journal.UpdatedAt = timeNow()
	if err := writeOperationJournal(root, journal); err != nil {
		return State{}, State{}, Event{}, err
	}
	if err := injectOperationFault("state_committed"); err != nil {
		return State{}, State{}, Event{}, err
	}
	if prepared.Finalize != nil {
		if err := prepared.Finalize(); err != nil {
			return State{}, State{}, Event{}, err
		}
	}
	if err := removeOperationJournal(root, journal.ID); err != nil {
		return State{}, State{}, Event{}, err
	}
	return previous, committed, event, nil
}

func commitOperationEventLocked(root string, config Config, journal OperationJournal) (State, error) {
	if journal.Event == nil {
		return State{}, &CLIError{Code: "CHS-OPERATION-EVENT-MISSING", Message: "operation journal has no signed event", ExitCode: 40}
	}
	_, trustPath, statePath, eventsPath := projectPaths(root)
	var trust Trust
	if err := loadYAML(trustPath, &trust); err != nil {
		return State{}, err
	}
	if err := verifyTrust(config, trust); err != nil {
		return State{}, err
	}
	events, err := readEvents(eventsPath)
	if err != nil {
		return State{}, err
	}
	eventPath := eventFilePath(eventsPath, *journal.Event)
	if data, readErr := os.ReadFile(eventPath); readErr == nil {
		var stored Event
		if err := strictJSON(data, &stored); err != nil {
			return State{}, err
		}
		if !equalCanonicalJSON(stored, *journal.Event) {
			return State{}, &CLIError{Code: "CHS-OPERATION-EVENT-MISMATCH", Message: "stored event differs from operation journal", ExitCode: 40}
		}
	} else if os.IsNotExist(readErr) {
		if int64(len(events)+1) != journal.Event.Sequence {
			return State{}, &CLIError{Code: "CHS-OPERATION-EVENT-SEQUENCE", Message: "operation event is not the next event", ExitCode: 40}
		}
		candidate := append(append([]Event{}, events...), *journal.Event)
		if _, err := verifyEventChain(config, trust, candidate); err != nil {
			return State{}, err
		}
		if err := writeEventAtomic(eventsPath, *journal.Event); err != nil {
			return State{}, err
		}
		if err := injectOperationFault("event_stored"); err != nil {
			return State{}, err
		}
	} else {
		return State{}, readErr
	}
	events, err = readEvents(eventsPath)
	if err != nil {
		return State{}, err
	}
	rebuilt, err := verifyEventChain(config, trust, events)
	if err != nil {
		return State{}, err
	}
	if rebuilt.Revision != journal.Event.Sequence {
		return State{}, &CLIError{Code: "CHS-OPERATION-EVENT-SEQUENCE", Message: "operation event is not the event-chain tip", ExitCode: 40}
	}
	if err := writeYAMLAtomic(statePath, rebuilt, 0o644); err != nil {
		return State{}, err
	}
	return rebuilt, nil
}

func recoverOperationsLocked(root string, config Config) error {
	journals, err := listOperationJournals(root)
	if err != nil {
		return err
	}
	for _, journal := range journals {
		if journal.Version != operationVersion || journal.ID == "" || journal.Phase == "" {
			return &CLIError{Code: "CHS-OPERATION-INVALID", Message: "operation journal is invalid", ExitCode: 40}
		}
		current, err := captureGitOperationStateFor(root, journal.GitAfter.WorktreePath)
		if err != nil {
			return err
		}
		switch {
		case journal.Phase == operationStateCommitted:
			if journal.Event == nil || !reflect.DeepEqual(current, journal.GitAfter) {
				return operationIntegrityBlocked(journal)
			}
			if _, err := commitOperationEventLocked(root, config, journal); err != nil {
				return err
			}
			if err := finalizeRecoveredOperation(root, journal); err != nil {
				return err
			}
		case journal.Event != nil && reflect.DeepEqual(current, journal.GitAfter):
			if _, err := commitOperationEventLocked(root, config, journal); err != nil {
				return err
			}
			if err := finalizeRecoveredOperation(root, journal); err != nil {
				return err
			}
		case reflect.DeepEqual(current, journal.GitBefore):
			if journal.Action == "task.release" {
				if err := removeOperationJournal(root, journal.ID); err != nil {
					return err
				}
				continue
			}
			if journal.Action == "work.open" && journal.Event != nil {
				applied, err := recoverWorkOpenGit(root, journal)
				if err != nil {
					return err
				}
				if applied {
					if _, err := commitOperationEventLocked(root, config, journal); err != nil {
						return err
					}
				}
			}
			if journal.Action == "integrate.apply" && journal.Event != nil {
				applied, err := recoverIntegrationGit(root, journal)
				if err != nil {
					return err
				}
				if applied {
					if _, err := commitOperationEventLocked(root, config, journal); err != nil {
						return err
					}
				}
			}
			if err := finalizeRecoveredOperation(root, journal); err != nil {
				return err
			}
		case journal.Event != nil && containsString([]string{"artifact.accept", "work.submit", "integrate.apply"}, journal.Action) && operationRefsAtAfter(journal, current):
			if err := syncOperationIndex(root, journal); err != nil {
				return err
			}
			current, err = captureGitOperationStateFor(root, journal.GitAfter.WorktreePath)
			if err != nil || !reflect.DeepEqual(current, journal.GitAfter) {
				return operationIntegrityBlocked(journal)
			}
			if _, err := commitOperationEventLocked(root, config, journal); err != nil {
				return err
			}
			if err := finalizeRecoveredOperation(root, journal); err != nil {
				return err
			}
		case integrationAtPreviousHead(journal, current):
			applied, err := recoverIntegrationGit(root, journal)
			if err != nil || !applied {
				if err != nil {
					return err
				}
				return operationIntegrityBlocked(journal)
			}
			if _, err := commitOperationEventLocked(root, config, journal); err != nil {
				return err
			}
			if err := finalizeRecoveredOperation(root, journal); err != nil {
				return err
			}
		default:
			return operationIntegrityBlocked(journal)
		}
	}
	return nil
}

func operationRefsAtAfter(journal OperationJournal, current GitOperationState) bool {
	if current.Branch != journal.GitAfter.Branch || current.Head != journal.GitAfter.Head {
		return false
	}
	if journal.GitAfter.WorktreePath == "" {
		return true
	}
	return current.WorktreePath == journal.GitAfter.WorktreePath && current.WorktreePresent && current.WorktreeBranch == journal.GitAfter.WorktreeBranch && current.WorktreeHead == journal.GitAfter.WorktreeHead && current.WorktreeID == journal.GitAfter.WorktreeID
}

func syncOperationIndex(root string, journal OperationJournal) error {
	if journal.GitAfter.WorktreePath == "" {
		_, err := git(root, "read-tree", "--reset", journal.GitAfter.Head)
		return err
	}
	worktreeRoot, err := pathWithin(root, journal.GitAfter.WorktreePath)
	if err != nil {
		return err
	}
	_, err = git(worktreeRoot, "read-tree", "--reset", journal.GitAfter.WorktreeHead)
	return err
}

func integrationAtPreviousHead(journal OperationJournal, current GitOperationState) bool {
	if journal.Event == nil || journal.Action != "integrate.apply" || current.Branch != journal.GitAfter.Branch {
		return false
	}
	var payload integrationAppliedPayload
	return decodePayload(journal.Event.Payload, &payload) == nil && current.Head == payload.PreviousHead
}

func recoverIntegrationGit(root string, journal OperationJournal) (bool, error) {
	if journal.Event == nil || journal.GitAfter.Branch == "" || journal.GitAfter.Head == "" {
		return false, operationIntegrityBlocked(journal)
	}
	targetRef := "refs/heads/" + journal.GitAfter.Branch
	targetHead, err := git(root, "rev-parse", "--verify", targetRef)
	if err != nil {
		return false, err
	}
	if targetHead == journal.GitAfter.Head {
		if currentBranchName, _ := currentBranch(root); currentBranchName != journal.GitAfter.Branch {
			if _, err := git(root, "checkout", journal.GitAfter.Branch); err != nil {
				return false, err
			}
		}
		if _, err := git(root, "read-tree", "--reset", journal.GitAfter.Head); err != nil {
			return false, err
		}
	} else {
		var payload integrationAppliedPayload
		if err := decodePayload(journal.Event.Payload, &payload); err != nil {
			return false, err
		}
		if targetHead != payload.PreviousHead {
			return false, operationIntegrityBlocked(journal)
		}
		if _, err := git(root, "cat-file", "-e", payload.IntegratedHead+"^{commit}"); err != nil {
			return false, operationIntegrityBlocked(journal)
		}
		if currentBranchName, _ := currentBranch(root); currentBranchName != journal.GitAfter.Branch {
			if _, err := git(root, "checkout", journal.GitAfter.Branch); err != nil {
				return false, err
			}
		}
		if _, err := git(root, "merge", "--ff-only", payload.IntegratedHead); err != nil {
			return false, err
		}
	}
	current, err := captureGitOperationStateFor(root, journal.GitAfter.WorktreePath)
	if err != nil {
		return false, err
	}
	if !reflect.DeepEqual(current, journal.GitAfter) {
		return false, operationIntegrityBlocked(journal)
	}
	return true, nil
}

func finalizeRecoveredOperation(root string, journal OperationJournal) error {
	if journal.Action == "integrate.apply" {
		var intent integrationOperationIntent
		if err := decodePayload(journal.Intent, &intent); err != nil {
			return err
		}
		if journal.Event == nil {
			return cleanupIntegrationWorktrees(root, intent, integrationAppliedPayload{})
		}
		var payload integrationAppliedPayload
		if err := decodePayload(journal.Event.Payload, &payload); err != nil {
			return err
		}
		if err := cleanupIntegrationWorktrees(root, intent, payload); err != nil {
			return err
		}
	}
	if journal.Action == "task.release" {
		var intent taskReleaseOperationIntent
		if err := decodePayload(journal.Intent, &intent); err != nil {
			return err
		}
		if err := cleanupReleasedTaskBranch(root, intent); err != nil {
			return err
		}
	}
	return removeOperationJournal(root, journal.ID)
}

func cleanupReleasedTaskBranch(root string, intent taskReleaseOperationIntent) error {
	if intent.Branch == "" || intent.Baseline == "" {
		return &CLIError{Code: "CHS-OPERATION-INVALID", Message: "task release cleanup intent is incomplete", ExitCode: 40}
	}
	ref := "refs/heads/" + intent.Branch
	head, err := git(root, "rev-parse", "--verify", ref)
	if err != nil {
		return nil
	}
	if head != intent.Baseline {
		return &CLIError{Code: "CHS-INTEGRITY-BLOCKED", Message: "released task branch moved before cleanup", ExitCode: 40, Remedy: []string{"do not reset or force the task branch", "inspect the task release operation journal"}}
	}
	_, err = git(root, "update-ref", "-d", ref, intent.Baseline)
	return err
}

func cleanupIntegrationWorktrees(root string, intent integrationOperationIntent, payload integrationAppliedPayload) error {
	if intent.CandidatePath != "" {
		candidate := filepath.Join(root, filepath.FromSlash(intent.CandidatePath))
		if _, err := os.Stat(candidate); err == nil {
			if _, err := git(root, "worktree", "remove", "--force", candidate); err != nil {
				return err
			}
		} else if !os.IsNotExist(err) {
			return err
		}
	}
	if intent.TaskWorktreePath != "" && payload.SubmissionHead != "" {
		taskWorktree := filepath.Join(root, filepath.FromSlash(intent.TaskWorktreePath))
		if _, err := os.Stat(taskWorktree); err == nil {
			clean, status, err := gitClean(taskWorktree)
			if err != nil {
				return err
			}
			head, err := gitHead(taskWorktree)
			if err != nil || !clean || head != payload.SubmissionHead {
				return &CLIError{Code: "CHS-WORKTREE-CLEANUP", Message: "integrated task worktree is not clean at the submitted head: " + status, ExitCode: 40}
			}
			if _, err := git(root, "worktree", "remove", taskWorktree); err != nil {
				return err
			}
		} else if !os.IsNotExist(err) {
			return err
		}
	}
	_, _ = git(root, "worktree", "prune")
	return nil
}

func recoverWorkOpenGit(root string, journal OperationJournal) (bool, error) {
	if journal.Event == nil || journal.GitAfter.WorktreePath == "" {
		return false, operationIntegrityBlocked(journal)
	}
	var payload workOpenedPayload
	if err := decodePayload(journal.Event.Payload, &payload); err != nil {
		return false, err
	}
	worktreePath, err := pathWithin(root, payload.WorktreePath)
	if err != nil {
		return false, err
	}
	if _, err := os.Stat(worktreePath); os.IsNotExist(err) {
		ref := "refs/heads/" + payload.Branch
		head, refErr := git(root, "rev-parse", "--verify", ref)
		if refErr == nil {
			if head != payload.Head {
				return false, operationIntegrityBlocked(journal)
			}
			if _, err := git(root, "worktree", "add", worktreePath, payload.Branch); err != nil {
				return false, err
			}
		} else {
			if _, err := git(root, "worktree", "add", "-b", payload.Branch, worktreePath, payload.Head); err != nil {
				return false, err
			}
		}
	} else if err != nil {
		return false, err
	}
	current, err := captureGitOperationStateFor(root, journal.GitAfter.WorktreePath)
	if err != nil {
		return false, err
	}
	if !reflect.DeepEqual(current, journal.GitAfter) {
		return false, operationIntegrityBlocked(journal)
	}
	return true, nil
}

func operationIntegrityBlocked(journal OperationJournal) error {
	return &CLIError{Code: "CHS-INTEGRITY-BLOCKED", Message: "Git state does not match unfinished operation " + journal.ID, ExitCode: 40, Remedy: []string{"do not reset or force any branch", "inspect .chassis/operations/" + journal.ID + ".json"}}
}

func captureGitOperationState(root string) (GitOperationState, error) {
	return captureGitOperationStateFor(root, "")
}

func captureGitOperationStateFor(root, worktreeRelative string) (GitOperationState, error) {
	branch, err := currentBranch(root)
	if err != nil {
		return GitOperationState{}, err
	}
	head, err := gitHead(root)
	if err != nil {
		return GitOperationState{}, err
	}
	indexTree, err := git(root, "write-tree")
	if err != nil {
		return GitOperationState{}, err
	}
	result := GitOperationState{Branch: branch, Head: head, IndexTree: indexTree, WorktreePath: worktreeRelative}
	if worktreeRelative == "" {
		return result, nil
	}
	worktreePath, err := pathWithin(root, worktreeRelative)
	if err != nil {
		return GitOperationState{}, err
	}
	if _, err := os.Stat(worktreePath); os.IsNotExist(err) {
		return result, nil
	} else if err != nil {
		return GitOperationState{}, err
	}
	result.WorktreePresent = true
	result.WorktreeBranch, err = currentBranch(worktreePath)
	if err != nil {
		return GitOperationState{}, err
	}
	result.WorktreeHead, err = gitHead(worktreePath)
	if err != nil {
		return GitOperationState{}, err
	}
	result.WorktreeIndexTree, err = git(worktreePath, "write-tree")
	if err != nil {
		return GitOperationState{}, err
	}
	result.WorktreeID, err = gitWorktreeIdentity(worktreePath)
	if err != nil {
		return GitOperationState{}, err
	}
	return result, nil
}

func operationTaskWorktreePath(action, resource string, state State) string {
	switch action {
	case "work.open":
		return taskWorktreeRelativePath(resource)
	case "work.submit":
		return state.Tasks[resource].WorktreePath
	case "task.release":
		return state.Tasks[resource].WorktreePath
	default:
		return ""
	}
}

func operationJournalPath(root, id string) string {
	return filepath.Join(root, ".chassis", "operations", id+".json")
}

func writeOperationJournal(root string, journal OperationJournal) error {
	return writeJSONAtomic(operationJournalPath(root, journal.ID), journal, 0o600)
}

func removeOperationJournal(root, id string) error {
	path := operationJournalPath(root, id)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	directory, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}

func listOperationJournals(root string) ([]OperationJournal, error) {
	directory := filepath.Join(root, ".chassis", "operations")
	entries, err := os.ReadDir(directory)
	if os.IsNotExist(err) {
		return []OperationJournal{}, nil
	}
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() && filepath.Ext(entry.Name()) == ".json" {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names)
	result := make([]OperationJournal, 0, len(names))
	for _, name := range names {
		data, err := os.ReadFile(filepath.Join(directory, name))
		if err != nil {
			return nil, err
		}
		var journal OperationJournal
		if err := strictJSON(data, &journal); err != nil {
			return nil, fmt.Errorf("parse operation %s: %w", name, err)
		}
		result = append(result, journal)
	}
	return result, nil
}

func eventFilePath(eventsDir string, event Event) string {
	return filepath.Join(eventsDir, fmt.Sprintf("%020d-%s.json", event.Sequence, event.ID))
}

func injectOperationFault(point string) error {
	if operationFaultHook == nil {
		return nil
	}
	return operationFaultHook(point)
}

func strictJSON(data []byte, output any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(output); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return fmt.Errorf("trailing JSON data")
	}
	return nil
}

func equalCanonicalJSON(left, right any) bool {
	leftData, leftErr := canonicalJSON(left)
	rightData, rightErr := canonicalJSON(right)
	return leftErr == nil && rightErr == nil && bytes.Equal(leftData, rightData)
}
