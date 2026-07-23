package app

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"time"
)

func projectPaths(root string) (configPath, trustPath, statePath, eventsPath string) {
	control := filepath.Join(root, ".chassis")
	return filepath.Join(control, "config.yaml"), filepath.Join(control, "trust.yaml"), filepath.Join(control, "state.yaml"), filepath.Join(control, "events")
}

func loadProject(root string) (Config, Trust, State, error) {
	var config Config
	var trust Trust
	var state State
	configPath, trustPath, statePath, _ := projectPaths(root)
	if err := loadYAML(configPath, &config); err != nil {
		return config, trust, state, err
	}
	if config.Version != ConfigVersion {
		return config, trust, state, &CLIError{Code: "CHS-SCHEMA-UNSUPPORTED", Message: "project schema is not supported; initialize a new API V2 project", ExitCode: 40}
	}
	if err := loadYAML(trustPath, &trust); err != nil {
		return config, trust, state, err
	}
	if err := verifyTrust(config, trust); err != nil {
		return config, trust, state, err
	}
	if err := loadYAML(statePath, &state); err != nil {
		return config, trust, state, err
	}
	if err := validateState(config, state); err != nil {
		return config, trust, state, err
	}
	return config, trust, state, nil
}

func initializeProject(target, rootKeyPath string, existing bool) (Config, State, error) {
	return initializeProjectWithBudget(target, rootKeyPath, existing, newProjectDefaultTaskBudget)
}

func initializeProjectWithBudget(target, rootKeyPath string, existing bool, budget TaskBudget) (Config, State, error) {
	var emptyConfig Config
	var emptyState State
	if err := validateTaskBudgetDefinition(budget); err != nil {
		return emptyConfig, emptyState, err
	}
	absolute, err := filepath.Abs(target)
	if err != nil {
		return emptyConfig, emptyState, err
	}
	target = absolute
	if _, err := os.Stat(filepath.Join(target, ".chassis")); err == nil {
		return emptyConfig, emptyState, &CLIError{Code: "CHS-PROJECT-EXISTS", Message: "project already contains .chassis", ExitCode: 10}
	} else if !os.IsNotExist(err) {
		return emptyConfig, emptyState, err
	}
	root, public, private, err := loadRoot(rootKeyPath)
	if err != nil {
		return emptyConfig, emptyState, err
	}
	if existing {
		top, gitErr := git(target, "rev-parse", "--show-toplevel")
		if gitErr != nil || !samePath(top, target) {
			return emptyConfig, emptyState, &CLIError{Code: "CHS-PROJECT-NOT-GIT", Message: "--existing requires the target itself to be an existing Git repository root", ExitCode: 10}
		}
		clean, status, err := gitClean(target)
		if err != nil {
			return emptyConfig, emptyState, err
		}
		if !clean {
			return emptyConfig, emptyState, &CLIError{Code: "CHS-PROJECT-DIRTY", Message: "--existing requires a clean worktree: " + status, ExitCode: 10}
		}
		if _, err := gitHead(target); err != nil {
			return emptyConfig, emptyState, &CLIError{Code: "CHS-PROJECT-NO-BASELINE", Message: "--existing requires at least one commit", ExitCode: 10}
		}
	} else {
		if err := os.MkdirAll(target, 0o755); err != nil {
			return emptyConfig, emptyState, err
		}
		top, _ := git(target, "rev-parse", "--show-toplevel")
		if samePath(top, target) {
			if _, err := gitHead(target); err == nil {
				return emptyConfig, emptyState, &CLIError{Code: "CHS-PROJECT-HAS-HISTORY", Message: "repository has commits; use --existing", ExitCode: 10}
			}
		} else if _, err := git(target, "init", "-b", "main"); err != nil {
			return emptyConfig, emptyState, err
		}
	}
	for _, path := range []string{
		filepath.Join(target, ".chassis", "submissions"), filepath.Join(target, ".chassis", "cache"), filepath.Join(target, ".chassis", "events"), filepath.Join(target, ".chassis", "operations"), filepath.Join(target, ".chassis", "auth-operations"), filepath.Join(target, ".chassis", "publish-operations"),
		filepath.Join(target, "docs", "missions"), filepath.Join(target, "docs", "tasks"),
	} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			return emptyConfig, emptyState, err
		}
	}
	if existing {
		excludePath := filepath.Join(target, ".git", "info", "exclude")
		file, err := os.OpenFile(excludePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if err != nil {
			return emptyConfig, emptyState, err
		}
		_, _ = fmt.Fprintln(file, "\n# CHASSISS local control state\n.chassis/")
		_ = file.Close()
	} else {
		ignorePath := filepath.Join(target, ".gitignore")
		if err := writeAtomic(ignorePath, []byte(".chassis/\n"), 0o644); err != nil {
			return emptyConfig, emptyState, err
		}
		if _, err := gitCommit(target, "Initialize CHASSISS project", ".gitignore"); err != nil {
			return emptyConfig, emptyState, err
		}
	}
	branch, err := gitDefaultBranch(target)
	if err != nil {
		return emptyConfig, emptyState, err
	}
	baseline, err := gitHead(target)
	if err != nil {
		return emptyConfig, emptyState, err
	}
	projectID, err := newID("PRJ")
	if err != nil {
		return emptyConfig, emptyState, err
	}
	now := time.Now().UTC()
	mode := "greenfield"
	if existing {
		mode = "brownfield"
	}
	config := Config{
		Version: ConfigVersion, ProjectID: projectID, Mode: mode, DefaultBranch: branch, ContentBackend: "local-git",
		WIPLimit: 2, DefaultTaskBudget: budget, RootFingerprint: keyFingerprint(public), CreatedAt: now,
	}
	trust := Trust{Version: TrustVersion, Revision: 1, ProjectID: projectID, RootPublicKey: root.PublicKey, Grants: []Grant{}, Revocations: []Revocation{}, UpdatedAt: now}
	if err := signTrust(&trust, private); err != nil {
		return emptyConfig, emptyState, err
	}
	configPath, trustPath, statePath, eventsPath := projectPaths(target)
	if err := writeYAMLAtomic(configPath, &config, 0o644); err != nil {
		return emptyConfig, emptyState, err
	}
	if err := writeYAMLAtomic(trustPath, &trust, 0o644); err != nil {
		return emptyConfig, emptyState, err
	}
	principal := rootPrincipal(root, public, private)
	payload := projectInitializedPayload{Config: config, Baseline: baseline}
	event, err := makeEvent(projectID, 1, "project.initialized", projectID, principal, "", now, payload)
	if err != nil {
		return emptyConfig, emptyState, err
	}
	state, err := reduceEvent(config, State{}, event)
	if err != nil {
		return emptyConfig, emptyState, err
	}
	if err := writeEventAtomic(eventsPath, event); err != nil {
		return emptyConfig, emptyState, err
	}
	if err := writeYAMLAtomic(statePath, &state, 0o644); err != nil {
		return emptyConfig, emptyState, err
	}
	return config, state, nil
}

func samePath(left, right string) bool {
	canonical := func(path string) string {
		absolute, err := filepath.Abs(path)
		if err == nil {
			path = absolute
		}
		resolved, err := filepath.EvalSymlinks(path)
		if err == nil {
			path = resolved
		}
		return filepath.Clean(path)
	}
	return canonical(left) == canonical(right)
}

func cloneState(state State) (State, error) {
	data, err := json.Marshal(state)
	if err != nil {
		return State{}, err
	}
	var copy State
	if err := json.Unmarshal(data, &copy); err != nil {
		return State{}, err
	}
	return copy, nil
}

func makeEvent(projectID string, sequence int64, eventType, resource string, principal Principal, previousDigest string, occurredAt time.Time, payload any) (Event, error) {
	id, err := newID("EVT")
	if err != nil {
		return Event{}, err
	}
	payloadData, err := marshalPayload(payload)
	if err != nil {
		return Event{}, err
	}
	event := Event{
		Version: EventVersion, ProjectID: projectID, Sequence: sequence, ID: id, Type: eventType,
		Actor: principal.Actor, Role: principal.Role, CredentialID: principal.ID, Resource: resource,
		OccurredAt: occurredAt, PreviousDigest: previousDigest, Payload: payloadData,
	}
	if err := authorizeEventScope(principal.Resources, event); err != nil {
		return Event{}, err
	}
	data, err := eventSigningBytes(event)
	if err != nil {
		return Event{}, err
	}
	event.Digest = digestBytes(data)
	event.Signature = base64.RawStdEncoding.EncodeToString(ed25519.Sign(ed25519.PrivateKey(principal.PrivateKey), []byte(event.Digest)))
	return event, nil
}

func eventSigningBytes(event Event) ([]byte, error) {
	event.Digest = ""
	event.Signature = ""
	return canonicalJSON(event)
}

func updateState(root string, principal Principal, eventType, resource string, expected int64, mutate func(*State) error) (State, State, Event, error) {
	lock, err := acquireLock(root)
	if err != nil {
		return State{}, State{}, Event{}, err
	}
	defer lock.release()
	if err := requireNoPendingOperations(root); err != nil {
		return State{}, State{}, Event{}, err
	}
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
	next, err := cloneState(previous)
	if err != nil {
		return State{}, State{}, Event{}, err
	}
	if err := mutate(&next); err != nil {
		return State{}, State{}, Event{}, err
	}
	_, _, statePath, eventsPath := projectPaths(root)
	events, err := readEvents(eventsPath)
	if err != nil {
		return State{}, State{}, Event{}, err
	}
	previousDigest := ""
	if len(events) > 0 {
		previousDigest = events[len(events)-1].Digest
	}
	payload, err := eventPayloadFromCandidate(previous, next, eventType, resource)
	if err != nil {
		return State{}, State{}, Event{}, err
	}
	event, err := makeEvent(previous.ProjectID, previous.Revision+1, eventType, resource, principal, previousDigest, time.Now().UTC(), payload)
	if err != nil {
		return State{}, State{}, Event{}, err
	}
	next, err = reduceEvent(config, previous, event)
	if err != nil {
		return State{}, State{}, Event{}, err
	}
	if err := writeEventAtomic(eventsPath, event); err != nil {
		return State{}, State{}, Event{}, err
	}
	if err := writeYAMLAtomic(statePath, &next, 0o644); err != nil {
		return State{}, State{}, Event{}, err
	}
	return previous, next, event, nil
}

func effectiveExpected(state State, requested int64) (int64, error) {
	if requested >= 0 && requested != state.Revision {
		return 0, &CLIError{Code: "CHS-CONFLICT-REVISION", Message: fmt.Sprintf("expected revision %d, current revision is %d", requested, state.Revision), ExitCode: 12, Retryable: true, Remedy: []string{"run chassiss status", "re-evaluate the action"}}
	}
	return state.Revision, nil
}

func verifyEventChain(config Config, trust Trust, events []Event) (State, error) {
	if len(events) == 0 {
		return State{}, &CLIError{Code: "CHS-INTEGRITY-EVENTS", Message: "event log is empty", ExitCode: 40}
	}
	rootPublic, _ := base64.RawStdEncoding.DecodeString(trust.RootPublicKey)
	grants := map[string]Grant{}
	for _, grant := range trust.Grants {
		grants[grant.ID] = grant
	}
	revokedAt := map[string]time.Time{}
	for _, revocation := range trust.Revocations {
		if current, ok := revokedAt[revocation.CredentialID]; !ok || revocation.RevokedAt.Before(current) {
			revokedAt[revocation.CredentialID] = revocation.RevokedAt
		}
	}
	previousDigest := ""
	var previousOccurredAt time.Time
	var rebuilt State
	for index, event := range events {
		sequence := int64(index + 1)
		if event.Version != EventVersion {
			return State{}, &CLIError{Code: "CHS-SCHEMA-UNSUPPORTED", Message: "event schema is not supported; initialize a new API V2 project", ExitCode: 40}
		}
		if event.ProjectID != config.ProjectID || event.Sequence != sequence {
			return State{}, &CLIError{Code: "CHS-INTEGRITY-EVENTS", Message: fmt.Sprintf("event sequence %d has invalid envelope", sequence), ExitCode: 40}
		}
		if event.PreviousDigest != previousDigest {
			return State{}, &CLIError{Code: "CHS-INTEGRITY-EVENTS", Message: fmt.Sprintf("event sequence %d breaks the digest chain", sequence), ExitCode: 40}
		}
		if event.OccurredAt.IsZero() || (!previousOccurredAt.IsZero() && event.OccurredAt.Before(previousOccurredAt)) {
			return State{}, &CLIError{Code: "CHS-INTEGRITY-EVENTS", Message: fmt.Sprintf("event sequence %d has a non-monotonic timestamp", sequence), ExitCode: 40}
		}
		data, err := eventSigningBytes(event)
		if err != nil {
			return State{}, err
		}
		if digestBytes(data) != event.Digest {
			return State{}, &CLIError{Code: "CHS-INTEGRITY-EVENTS", Message: fmt.Sprintf("event sequence %d digest is invalid", sequence), ExitCode: 40}
		}
		signature, err := base64.RawStdEncoding.DecodeString(event.Signature)
		if err != nil {
			return State{}, &CLIError{Code: "CHS-INTEGRITY-EVENTS", Message: fmt.Sprintf("event sequence %d signature is malformed", sequence), ExitCode: 40}
		}
		requiredAction, knownEvent := eventRequiredAction(event.Type)
		if !knownEvent {
			return State{}, &CLIError{Code: "CHS-INTEGRITY-EVENTS", Message: fmt.Sprintf("event sequence %d has an unknown event type", sequence), ExitCode: 40}
		}
		var public []byte
		if event.Role == "master" {
			public = rootPublic
			if event.Type != "project.initialized" {
				if _, ok := rootPrincipal(&RootKey{}, rootPublic, nil).Actions[requiredAction]; !ok {
					return State{}, &CLIError{Code: "CHS-INTEGRITY-EVENTS", Message: fmt.Sprintf("event sequence %d is not authorized for Master", sequence), ExitCode: 40}
				}
			}
		} else {
			grant, ok := grants[event.CredentialID]
			if !ok || grant.Actor != event.Actor || grant.Role != event.Role {
				return State{}, &CLIError{Code: "CHS-INTEGRITY-EVENTS", Message: fmt.Sprintf("event sequence %d uses an unknown credential", sequence), ExitCode: 40}
			}
			if !containsString(grant.Actions, requiredAction) {
				return State{}, &CLIError{Code: "CHS-INTEGRITY-EVENTS", Message: fmt.Sprintf("event sequence %d credential is not authorized for %s", sequence, event.Type), ExitCode: 40}
			}
			if event.OccurredAt.Before(grant.IssuedAt) {
				return State{}, &CLIError{Code: "CHS-INTEGRITY-EVENTS", Message: fmt.Sprintf("event sequence %d predates its credential grant", sequence), ExitCode: 40}
			}
			if !credentialTimeValid(grant.NotBefore, grant.ExpiresAt, event.OccurredAt) {
				return State{}, &CLIError{Code: "CHS-INTEGRITY-EVENTS", Message: fmt.Sprintf("event sequence %d falls outside its credential validity window", sequence), ExitCode: 40}
			}
			if revoked, ok := revokedAt[event.CredentialID]; ok && !event.OccurredAt.Before(revoked) {
				return State{}, &CLIError{Code: "CHS-INTEGRITY-EVENTS", Message: fmt.Sprintf("event sequence %d was signed after credential revocation", sequence), ExitCode: 40}
			}
			public, _ = base64.RawStdEncoding.DecodeString(grant.PublicKey)
			if err := authorizeEventScope(grant.Resources, event); err != nil {
				return State{}, &CLIError{Code: "CHS-INTEGRITY-EVENTS", Message: fmt.Sprintf("event sequence %d exceeds its credential resource scope", sequence), ExitCode: 40}
			}
		}
		if len(public) != ed25519.PublicKeySize || !ed25519.Verify(ed25519.PublicKey(public), []byte(event.Digest), signature) {
			return State{}, &CLIError{Code: "CHS-INTEGRITY-EVENTS", Message: fmt.Sprintf("event sequence %d signature is invalid", sequence), ExitCode: 40}
		}
		if event.Type == "task.claimed" || event.Type == "task.assigned" {
			var payload taskClaimedPayload
			if err := decodePayload(event.Payload, &payload); err != nil {
				return State{}, fmt.Errorf("event sequence %d task owner payload is invalid: %w", sequence, err)
			}
			ownerGrant, ok := grants[payload.OwnerGrantID]
			if !ok || ownerGrant.Actor != payload.Owner || ownerGrant.Role != "developer" || !containsString(ownerGrant.Actions, "work.open") || event.OccurredAt.Before(ownerGrant.IssuedAt) || !credentialTimeValid(ownerGrant.NotBefore, ownerGrant.ExpiresAt, event.OccurredAt) || !scopeAllows(ownerGrant.Resources.Tasks, payload.TaskID) {
				return State{}, &CLIError{Code: "CHS-INTEGRITY-EVENTS", Message: fmt.Sprintf("event sequence %d task owner grant is invalid", sequence), ExitCode: 40}
			}
			if revoked, ok := revokedAt[ownerGrant.ID]; ok && !event.OccurredAt.Before(revoked) {
				return State{}, &CLIError{Code: "CHS-INTEGRITY-EVENTS", Message: fmt.Sprintf("event sequence %d assigned a revoked Developer grant", sequence), ExitCode: 40}
			}
		}
		rebuilt, err = reduceEvent(config, rebuilt, event)
		if err != nil {
			return State{}, fmt.Errorf("event sequence %d cannot be reduced: %w", sequence, err)
		}
		previousDigest = event.Digest
		previousOccurredAt = event.OccurredAt
	}
	return rebuilt, nil
}

func eventRequiredAction(eventType string) (string, bool) {
	actions := map[string]string{
		"project.initialized":          "project.init",
		"artifact.submitted":           "artifact.submit",
		"artifact.accepted":            "artifact.accept",
		"artifact.rejected":            "artifact.reject",
		"mission.activated":            "mission.activate",
		"mission.blocked":              "mission.block",
		"mission.resumed":              "mission.resume",
		"mission.acceptance_submitted": "mission.submit-acceptance",
		"mission.completed":            "mission.accept",
		"task.claimed":                 "task.claim",
		"task.assigned":                "task.assign",
		"task.blocked":                 "task.block",
		"task.resumed":                 "task.resume",
		"task.released":                "task.release",
		"task.cancelled":               "task.cancel",
		"task.superseded":              "task.supersede",
		"work.opened":                  "work.open",
		"work.checked":                 "work.check",
		"work.checkpointed":            "work.checkpoint",
		"work.submitted":               "work.submit",
		"work.blocked":                 "work.block",
		"review.approved":              "review.approve",
		"review.changes_requested":     "review.request-changes",
		"integration.applied":          "integrate.apply",
		"owner.baseline_applied":       "owner.apply",
		"publication.applied":          "publish.apply",
	}
	action, ok := actions[eventType]
	return action, ok
}

func verifyProject(root string) (State, error) {
	config, trust, state, err := loadProject(root)
	if err != nil {
		return State{}, err
	}
	_, _, _, eventsPath := projectPaths(root)
	events, err := readEvents(eventsPath)
	if err != nil {
		return State{}, err
	}
	rebuilt, err := verifyEventChain(config, trust, events)
	if err != nil {
		return State{}, err
	}
	if !reflect.DeepEqual(rebuilt, state) {
		return State{}, &CLIError{Code: "CHS-INTEGRITY-PROJECTION", Message: "state.yaml differs from the signed event projection; run chassiss recover", ExitCode: 40}
	}
	return state, nil
}

func recoverProject(root string) (State, error) {
	lock, err := acquireLock(root)
	if err != nil {
		return State{}, err
	}
	defer lock.release()
	var config Config
	var trust Trust
	configPath, trustPath, statePath, eventsPath := projectPaths(root)
	if err := loadYAML(configPath, &config); err != nil {
		return State{}, err
	}
	if config.Version != ConfigVersion {
		return State{}, &CLIError{Code: "CHS-SCHEMA-UNSUPPORTED", Message: "project schema is not supported by this CHASSISS version; initialize a new V2 project", ExitCode: 40}
	}
	if err := loadYAML(trustPath, &trust); err != nil {
		return State{}, err
	}
	if err := verifyTrust(config, trust); err != nil {
		return State{}, err
	}
	if err := recoverAuthOperationsLocked(root, config); err != nil {
		return State{}, err
	}
	if err := loadYAML(trustPath, &trust); err != nil {
		return State{}, err
	}
	if err := verifyTrust(config, trust); err != nil {
		return State{}, err
	}
	if err := recoverPublishOperationsLocked(root, config); err != nil {
		return State{}, err
	}
	if err := recoverOperationsLocked(root, config); err != nil {
		return State{}, err
	}
	events, err := readEvents(eventsPath)
	if err != nil {
		return State{}, err
	}
	rebuilt, err := verifyEventChain(config, trust, events)
	if err != nil {
		return State{}, err
	}
	if err := writeYAMLAtomic(statePath, &rebuilt, 0o644); err != nil {
		return State{}, err
	}
	return rebuilt, nil
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func sortedTaskIDs(tasks map[string]TaskState) []string {
	ids := make([]string, 0, len(tasks))
	for id := range tasks {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

func pathWithin(root, path string) (string, error) {
	if filepath.IsAbs(path) {
		clean := filepath.Clean(path)
		relative, err := filepath.Rel(root, clean)
		if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(os.PathSeparator)) {
			return "", &CLIError{Code: "CHS-PATH-OUTSIDE", Message: "path is outside project root", ExitCode: 20}
		}
		return clean, nil
	}
	clean := filepath.Join(root, filepath.Clean(path))
	relative, err := filepath.Rel(root, clean)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(os.PathSeparator)) {
		return "", &CLIError{Code: "CHS-PATH-OUTSIDE", Message: "path is outside project root", ExitCode: 20}
	}
	return clean, nil
}
