package app

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const publishOperationVersion = 1

const (
	publishOperationPrepared       = "prepared"
	publishOperationRemoteApplied  = "remote_applied"
	publishOperationStateCommitted = "state_committed"
)

type PublishCheck struct {
	Target          string `json:"target"`
	Remote          string `json:"remote"`
	RemoteURLDigest string `json:"remote_url_digest"`
	Branch          string `json:"branch"`
	LocalHead       string `json:"local_head"`
	RemoteHead      string `json:"remote_head,omitempty"`
	Status          string `json:"status"`
}

type PublishOperationJournal struct {
	Version          int       `json:"version"`
	ID               string    `json:"id"`
	ExpectedRevision int64     `json:"expected_revision"`
	Phase            string    `json:"phase"`
	Target           string    `json:"target"`
	Remote           string    `json:"remote"`
	RemoteURLDigest  string    `json:"remote_url_digest"`
	Branch           string    `json:"branch"`
	BeforeRemoteHead string    `json:"before_remote_head,omitempty"`
	AfterRemoteHead  string    `json:"after_remote_head"`
	Event            Event     `json:"event"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

var publishOperationFaultHook func(point string) error

func publishCheck(root, target, remote, branch string) (PublishCheck, error) {
	config, _, state, err := loadProject(root)
	if err != nil {
		return PublishCheck{}, err
	}
	if _, err := verifyProject(root); err != nil {
		return PublishCheck{}, err
	}
	clean, status, err := gitClean(root)
	if err != nil {
		return PublishCheck{}, err
	}
	if !clean {
		return PublishCheck{}, &CLIError{Code: "CHS-PUBLISH-DIRTY", Message: "formal worktree must be clean before publish: " + status, ExitCode: 10}
	}
	return inspectPublishState(root, config, state, target, remote, branch, false)
}

func inspectPublishState(root string, config Config, state State, target, remote, branch string, fetchRemote bool) (PublishCheck, error) {
	if !containsString([]string{"github", "gitlab", "remote-git"}, target) {
		return PublishCheck{}, &CLIError{Code: "CHS-PUBLISH-TARGET", Message: "publish target must be github, gitlab, or remote-git", ExitCode: 20}
	}
	if remote == "" {
		remote = "origin"
	}
	if strings.HasPrefix(remote, "-") || strings.ContainsAny(remote, " \t\r\n\x00") {
		return PublishCheck{}, &CLIError{Code: "CHS-PUBLISH-REMOTE", Message: "publish remote name is invalid", ExitCode: 20}
	}
	if branch == "" {
		branch = config.DefaultBranch
	}
	if _, err := git(root, "check-ref-format", "--branch", branch); err != nil {
		return PublishCheck{}, &CLIError{Code: "CHS-PUBLISH-BRANCH", Message: "publish branch is invalid", ExitCode: 20}
	}
	remoteURL, err := git(root, "remote", "get-url", remote)
	if err != nil || remoteURL == "" {
		return PublishCheck{}, &CLIError{Code: "CHS-PUBLISH-REMOTE", Message: "configured Git remote is unavailable: " + remote, ExitCode: 10}
	}
	if strings.Contains(remoteURL, "::") || strings.ContainsAny(remoteURL, "\r\n\x00") {
		return PublishCheck{}, &CLIError{Code: "CHS-PUBLISH-REMOTE-UNSAFE", Message: "Git remote uses an unsafe external-helper or control-character URL", ExitCode: 40}
	}
	remoteURLDigest := digestBytes([]byte(remoteURL))
	localHead, err := git(root, "rev-parse", "--verify", "refs/heads/"+config.DefaultBranch)
	if err != nil || localHead != state.Baseline {
		return PublishCheck{}, &CLIError{Code: "CHS-PUBLISH-BASELINE", Message: "formal local branch does not match the CLI baseline", ExitCode: 40}
	}
	remoteHead, err := gitRemoteHead(root, remote, branch)
	if err != nil {
		return PublishCheck{}, err
	}
	result := PublishCheck{Target: target, Remote: remote, RemoteURLDigest: remoteURLDigest, Branch: branch, LocalHead: localHead, RemoteHead: remoteHead, Status: "ready"}
	if remoteHead == localHead {
		result.Status = "up_to_date"
		return result, nil
	}
	if remoteHead == "" {
		return result, nil
	}
	if fetchRemote {
		if _, err := git(root, "fetch", "--no-tags", remote, remoteHead); err != nil {
			return PublishCheck{}, &CLIError{Code: "CHS-PUBLISH-FETCH", Message: "cannot fetch the exact remote head: " + err.Error(), ExitCode: 30, Retryable: true}
		}
	}
	if _, err := git(root, "cat-file", "-e", remoteHead+"^{commit}"); err != nil {
		result.Status = "remote_unknown"
		return result, nil
	}
	if _, err := git(root, "merge-base", "--is-ancestor", remoteHead, localHead); err != nil {
		result.Status = "diverged"
	}
	return result, nil
}

func publishApply(root, target, remote, branch string, principal Principal, expected int64) (State, State, Publication, error) {
	lock, err := acquireLock(root)
	if err != nil {
		return State{}, State{}, Publication{}, err
	}
	defer lock.release()
	if err := requireNoPendingOperations(root); err != nil {
		return State{}, State{}, Publication{}, err
	}
	config, _, _, err := loadProject(root)
	if err != nil {
		return State{}, State{}, Publication{}, err
	}
	previous, err := verifyProject(root)
	if err != nil {
		return State{}, State{}, Publication{}, err
	}
	if expected >= 0 && previous.Revision != expected {
		return State{}, State{}, Publication{}, &CLIError{Code: "CHS-CONFLICT-REVISION", Message: fmt.Sprintf("expected revision %d, current revision is %d", expected, previous.Revision), ExitCode: 12, Retryable: true}
	}
	clean, status, err := gitClean(root)
	if err != nil {
		return State{}, State{}, Publication{}, err
	}
	if !clean {
		return State{}, State{}, Publication{}, &CLIError{Code: "CHS-PUBLISH-DIRTY", Message: "formal worktree must be clean before publish: " + status, ExitCode: 10}
	}
	check, err := inspectPublishState(root, config, previous, target, remote, branch, true)
	if err != nil {
		return State{}, State{}, Publication{}, err
	}
	if check.Status == "up_to_date" {
		return State{}, State{}, Publication{}, &CLIError{Code: "CHS-PUBLISH-UP-TO-DATE", Message: "remote branch already equals the local baseline", ExitCode: 10}
	}
	if check.Status != "ready" {
		return State{}, State{}, Publication{}, &CLIError{Code: "CHS-PUBLISH-NON-FAST-FORWARD", Message: "remote branch is not a known ancestor of the local baseline", ExitCode: 10, Remedy: []string{"inspect remote changes", "integrate remote history locally through an explicit Task; do not force push"}}
	}
	id, err := newID("PUB")
	if err != nil {
		return State{}, State{}, Publication{}, err
	}
	payload := publicationAppliedPayload{PublicationID: id, Target: check.Target, Remote: check.Remote, RemoteURLDigest: check.RemoteURLDigest, Branch: check.Branch, PreviousRemoteHead: check.RemoteHead, Head: previous.Baseline}
	eventsPath := filepath.Join(root, ".chassis", "events")
	events, err := readEvents(eventsPath)
	if err != nil {
		return State{}, State{}, Publication{}, err
	}
	previousDigest := events[len(events)-1].Digest
	event, err := makeEvent(previous.ProjectID, previous.Revision+1, "publication.applied", id, principal, previousDigest, timeNow(), payload)
	if err != nil {
		return State{}, State{}, Publication{}, err
	}
	if _, err := reduceEvent(config, previous, event); err != nil {
		return State{}, State{}, Publication{}, err
	}
	journal := PublishOperationJournal{Version: publishOperationVersion, ID: id, ExpectedRevision: previous.Revision, Phase: publishOperationPrepared, Target: check.Target, Remote: check.Remote, RemoteURLDigest: check.RemoteURLDigest, Branch: check.Branch, BeforeRemoteHead: check.RemoteHead, AfterRemoteHead: previous.Baseline, Event: event, CreatedAt: timeNow(), UpdatedAt: timeNow()}
	if err := writePublishOperationJournal(root, journal); err != nil {
		return State{}, State{}, Publication{}, err
	}
	if err := injectPublishOperationFault("prepared"); err != nil {
		return State{}, State{}, Publication{}, err
	}
	refspec := previous.Baseline + ":refs/heads/" + check.Branch
	if _, err := git(root, "push", "--porcelain", check.Remote, refspec); err != nil {
		return State{}, State{}, Publication{}, &CLIError{Code: "CHS-PUBLISH-PUSH", Message: "remote push failed: " + err.Error(), ExitCode: 30, Retryable: true, Remedy: []string{"run chassiss recover before retrying", "do not force push"}}
	}
	if err := injectPublishOperationFault("remote_applied_before_phase"); err != nil {
		return State{}, State{}, Publication{}, err
	}
	remoteHead, err := gitRemoteHead(root, check.Remote, check.Branch)
	if err != nil || remoteHead != previous.Baseline {
		return State{}, State{}, Publication{}, &CLIError{Code: "CHS-PUBLISH-REMOTE-MISMATCH", Message: "remote branch does not match the journaled baseline after push", ExitCode: 40}
	}
	journal.Phase = publishOperationRemoteApplied
	journal.UpdatedAt = timeNow()
	if err := writePublishOperationJournal(root, journal); err != nil {
		return State{}, State{}, Publication{}, err
	}
	if err := injectPublishOperationFault("remote_applied"); err != nil {
		return State{}, State{}, Publication{}, err
	}
	committed, err := commitOperationEventLocked(root, config, OperationJournal{Event: &journal.Event})
	if err != nil {
		return State{}, State{}, Publication{}, err
	}
	journal.Phase = publishOperationStateCommitted
	journal.UpdatedAt = timeNow()
	if err := writePublishOperationJournal(root, journal); err != nil {
		return State{}, State{}, Publication{}, err
	}
	if err := injectPublishOperationFault("state_committed"); err != nil {
		return State{}, State{}, Publication{}, err
	}
	if err := removePublishOperationJournal(root, journal.ID); err != nil {
		return State{}, State{}, Publication{}, err
	}
	return previous, committed, committed.Publications[id], nil
}

func recoverPublishOperationsLocked(root string, config Config) error {
	journals, err := listPublishOperationJournals(root)
	if err != nil {
		return err
	}
	for _, journal := range journals {
		if err := validatePublishOperationJournal(journal); err != nil {
			return err
		}
		stored, err := publishOperationEventStored(root, journal)
		if err != nil {
			return err
		}
		if stored {
			if _, err := commitOperationEventLocked(root, config, OperationJournal{Event: &journal.Event}); err != nil {
				return err
			}
			if err := removePublishOperationJournal(root, journal.ID); err != nil {
				return err
			}
			continue
		}
		remoteURL, err := git(root, "remote", "get-url", journal.Remote)
		if err != nil || digestBytes([]byte(remoteURL)) != journal.RemoteURLDigest {
			return &CLIError{Code: "CHS-INTEGRITY-BLOCKED", Message: "Git remote endpoint changed during unfinished publish operation " + journal.ID, ExitCode: 40, Remedy: []string{"restore the exact journaled remote endpoint or ask Master to inspect the operation"}}
		}
		remoteHead, err := gitRemoteHead(root, journal.Remote, journal.Branch)
		if err != nil {
			return err
		}
		switch {
		case remoteHead == journal.AfterRemoteHead:
			if _, err := commitOperationEventLocked(root, config, OperationJournal{Event: &journal.Event}); err != nil {
				return err
			}
		case remoteHead == journal.BeforeRemoteHead && journal.Phase == publishOperationPrepared:
			// Push was not durably observed. Cancel the journal; local workflow state is unchanged.
		default:
			return &CLIError{Code: "CHS-INTEGRITY-BLOCKED", Message: "remote branch does not match unfinished publish operation " + journal.ID, ExitCode: 40, Remedy: []string{"do not force push", "inspect .chassis/publish-operations/" + journal.ID + ".json"}}
		}
		if err := removePublishOperationJournal(root, journal.ID); err != nil {
			return err
		}
	}
	return nil
}

func validatePublishOperationJournal(journal PublishOperationJournal) error {
	if journal.Version != publishOperationVersion || journal.ID == "" || journal.Event.ID == "" || journal.Event.Type != "publication.applied" || journal.Event.Sequence != journal.ExpectedRevision+1 || journal.AfterRemoteHead == "" || journal.Remote == "" || journal.RemoteURLDigest == "" || journal.Branch == "" || !containsString([]string{publishOperationPrepared, publishOperationRemoteApplied, publishOperationStateCommitted}, journal.Phase) {
		return &CLIError{Code: "CHS-PUBLISH-OPERATION-INVALID", Message: "publish operation journal is invalid", ExitCode: 40}
	}
	var payload publicationAppliedPayload
	if err := decodePayload(journal.Event.Payload, &payload); err != nil {
		return err
	}
	if journal.Event.Resource != journal.ID || payload.PublicationID != journal.ID || payload.Target != journal.Target || payload.Remote != journal.Remote || payload.RemoteURLDigest != journal.RemoteURLDigest || payload.Branch != journal.Branch || payload.PreviousRemoteHead != journal.BeforeRemoteHead || payload.Head != journal.AfterRemoteHead {
		return &CLIError{Code: "CHS-PUBLISH-OPERATION-INVALID", Message: "publish operation journal does not match its signed event", ExitCode: 40}
	}
	return nil
}

func publishOperationEventStored(root string, journal PublishOperationJournal) (bool, error) {
	_, _, _, eventsPath := projectPaths(root)
	data, err := os.ReadFile(eventFilePath(eventsPath, journal.Event))
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	var stored Event
	if err := strictJSON(data, &stored); err != nil {
		return false, err
	}
	if !equalCanonicalJSON(stored, journal.Event) {
		return false, &CLIError{Code: "CHS-PUBLISH-OPERATION-INVALID", Message: "stored publication event differs from its journal", ExitCode: 40}
	}
	return true, nil
}

func gitRemoteHead(root, remote, branch string) (string, error) {
	output, err := git(root, "ls-remote", "--heads", remote, "refs/heads/"+branch)
	if err != nil {
		return "", &CLIError{Code: "CHS-PUBLISH-REMOTE", Message: "cannot read remote branch: " + err.Error(), ExitCode: 30, Retryable: true}
	}
	if output == "" {
		return "", nil
	}
	fields := strings.Fields(output)
	if len(fields) != 2 || fields[1] != "refs/heads/"+branch {
		return "", &CLIError{Code: "CHS-PUBLISH-REMOTE", Message: "remote branch response is invalid", ExitCode: 40}
	}
	return fields[0], nil
}

func requireNoPendingPublishOperations(root string) error {
	journals, err := listPublishOperationJournals(root)
	if err != nil {
		return err
	}
	if len(journals) != 0 {
		return &CLIError{Code: "CHS-OPERATION-RECOVERY-REQUIRED", Message: "an unfinished publish operation must be recovered before another write", ExitCode: 40, Remedy: []string{"run chassiss recover"}}
	}
	return nil
}

func publishOperationJournalPath(root, id string) string {
	return filepath.Join(root, ".chassis", "publish-operations", id+".json")
}

func writePublishOperationJournal(root string, journal PublishOperationJournal) error {
	return writeJSONAtomic(publishOperationJournalPath(root, journal.ID), journal, 0o600)
}

func removePublishOperationJournal(root, id string) error {
	path := publishOperationJournalPath(root, id)
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

func listPublishOperationJournals(root string) ([]PublishOperationJournal, error) {
	directory := filepath.Join(root, ".chassis", "publish-operations")
	entries, err := os.ReadDir(directory)
	if os.IsNotExist(err) {
		return []PublishOperationJournal{}, nil
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
	result := make([]PublishOperationJournal, 0, len(names))
	for _, name := range names {
		data, err := os.ReadFile(filepath.Join(directory, name))
		if err != nil {
			return nil, err
		}
		var journal PublishOperationJournal
		if err := strictJSON(data, &journal); err != nil {
			return nil, err
		}
		result = append(result, journal)
	}
	return result, nil
}

func injectPublishOperationFault(point string) error {
	if publishOperationFaultHook == nil {
		return nil
	}
	return publishOperationFaultHook(point)
}
