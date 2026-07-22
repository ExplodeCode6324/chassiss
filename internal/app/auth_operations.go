package app

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const authOperationVersion = 1

const (
	authOperationPrepared            = "prepared"
	authOperationCredentialPrepared  = "credential_prepared"
	authOperationTrustCommitted      = "trust_committed"
	authOperationCredentialPublished = "credential_published"
)

type AuthOperationJournal struct {
	Version               int       `json:"version"`
	ID                    string    `json:"id"`
	Action                string    `json:"action"`
	CredentialID          string    `json:"credential_id"`
	ExpectedTrustRevision int64     `json:"expected_trust_revision"`
	Phase                 string    `json:"phase"`
	BeforeTrust           Trust     `json:"before_trust"`
	AfterTrust            Trust     `json:"after_trust"`
	OutputPath            string    `json:"output_path,omitempty"`
	TempPath              string    `json:"temp_path,omitempty"`
	CredentialDigest      string    `json:"credential_digest,omitempty"`
	CreatedAt             time.Time `json:"created_at"`
	UpdatedAt             time.Time `json:"updated_at"`
}

// authOperationFaultHook is used only by crash-injection tests.
var authOperationFaultHook func(point string) error

type CredentialPolicy struct {
	NotBefore *time.Time
	ExpiresAt *time.Time
	Resources ResourceScope
}

func issueCredentialExpected(rootDir, rootPath, actor, role, output string, requested []string, expectedRevision int64) (*Credential, error) {
	return issueCredentialWithPolicy(rootDir, rootPath, actor, role, output, requested, expectedRevision, CredentialPolicy{})
}

func issueCredentialWithPolicy(rootDir, rootPath, actor, role, output string, requested []string, expectedRevision int64, policy CredentialPolicy) (*Credential, error) {
	if !validActor(actor) {
		return nil, &CLIError{Code: "CHS-AUTH-ACTOR", Message: "actor must be a non-empty stable identifier without whitespace or control characters", ExitCode: 20}
	}
	_, publicRoot, privateRoot, err := loadRoot(rootPath)
	if err != nil {
		return nil, err
	}
	lock, err := acquireLock(rootDir)
	if err != nil {
		return nil, err
	}
	defer lock.release()
	if err := requireNoPendingOperations(rootDir); err != nil {
		return nil, err
	}
	config, trust, _, err := loadProject(rootDir)
	if err != nil {
		return nil, err
	}
	if _, err := verifyProject(rootDir); err != nil {
		return nil, err
	}
	if keyFingerprint(publicRoot) != config.RootFingerprint {
		return nil, &CLIError{Code: "CHS-AUTH-ROOT-MISMATCH", Message: "Master Root does not own this project", ExitCode: 11}
	}
	if expectedRevision >= 0 && trust.Revision != expectedRevision {
		return nil, trustRevisionConflict(expectedRevision, trust.Revision)
	}
	policy, err = normalizeCredentialPolicy(policy, config.ProjectID, timeNow())
	if err != nil {
		return nil, err
	}
	actions, err := grantedActions(role, requested)
	if err != nil {
		return nil, err
	}
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	id, err := newID("CRED")
	if err != nil {
		return nil, err
	}
	now := timeNow()
	grant := Grant{ID: id, Actor: actor, Role: role, Actions: actions, PublicKey: base64.RawStdEncoding.EncodeToString(public), IssuedAt: now, NotBefore: policy.NotBefore, ExpiresAt: policy.ExpiresAt, Resources: policy.Resources}
	credential := &Credential{
		Kind: "chassiss-role-credential", Version: CredentialVersion, ID: id, ProjectID: config.ProjectID,
		RootFingerprint: keyFingerprint(publicRoot), Actor: actor, Role: role, Actions: actions,
		PrivateKey: base64.RawStdEncoding.EncodeToString(private), IssuedAt: now,
		NotBefore: policy.NotBefore, ExpiresAt: policy.ExpiresAt, Resources: policy.Resources,
	}
	credentialData, err := yaml.Marshal(credential)
	if err != nil {
		return nil, err
	}
	output, err = filepath.Abs(output)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(output), 0o700); err != nil {
		return nil, err
	}
	if _, err := os.Stat(output); err == nil {
		return nil, &CLIError{Code: "CHS-AUTH-CREDENTIAL-EXISTS", Message: "refusing to overwrite an existing credential file", ExitCode: 10}
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	operationID, err := newID("AUTHOP")
	if err != nil {
		return nil, err
	}
	tempPath := filepath.Join(filepath.Dir(output), "."+filepath.Base(output)+"."+operationID+".tmp")
	if _, err := os.Stat(tempPath); err == nil {
		return nil, &CLIError{Code: "CHS-AUTH-TEMP-EXISTS", Message: "credential temporary path already exists", ExitCode: 40}
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	before, err := cloneTrust(trust)
	if err != nil {
		return nil, err
	}
	after, err := cloneTrust(trust)
	if err != nil {
		return nil, err
	}
	after.Grants = append(after.Grants, grant)
	after.Revision++
	after.UpdatedAt = now
	if err := signTrust(&after, privateRoot); err != nil {
		return nil, err
	}
	journal := AuthOperationJournal{
		Version: authOperationVersion, ID: operationID, Action: "auth.issue", CredentialID: id,
		ExpectedTrustRevision: trust.Revision, Phase: authOperationPrepared, BeforeTrust: before, AfterTrust: after,
		OutputPath: output, TempPath: tempPath, CredentialDigest: digestBytes(credentialData), CreatedAt: now, UpdatedAt: now,
	}
	if err := writeAuthOperationJournal(rootDir, journal); err != nil {
		return nil, err
	}
	if err := injectAuthOperationFault("prepared"); err != nil {
		return nil, err
	}
	if err := writeAtomic(tempPath, credentialData, 0o600); err != nil {
		return nil, err
	}
	journal.Phase = authOperationCredentialPrepared
	journal.UpdatedAt = timeNow()
	if err := writeAuthOperationJournal(rootDir, journal); err != nil {
		return nil, err
	}
	if err := injectAuthOperationFault("credential_prepared"); err != nil {
		return nil, err
	}
	_, trustPath, _, _ := projectPaths(rootDir)
	if err := writeYAMLAtomic(trustPath, &after, 0o644); err != nil {
		return nil, err
	}
	if err := injectAuthOperationFault("trust_committed_before_phase"); err != nil {
		return nil, err
	}
	journal.Phase = authOperationTrustCommitted
	journal.UpdatedAt = timeNow()
	if err := writeAuthOperationJournal(rootDir, journal); err != nil {
		return nil, err
	}
	if err := injectAuthOperationFault("trust_committed"); err != nil {
		return nil, err
	}
	if err := publishCredentialFile(journal); err != nil {
		return nil, err
	}
	if err := injectAuthOperationFault("credential_published_before_phase"); err != nil {
		return nil, err
	}
	journal.Phase = authOperationCredentialPublished
	journal.UpdatedAt = timeNow()
	if err := writeAuthOperationJournal(rootDir, journal); err != nil {
		return nil, err
	}
	if err := injectAuthOperationFault("credential_published"); err != nil {
		return nil, err
	}
	if err := removeAuthOperationJournal(rootDir, operationID); err != nil {
		return nil, err
	}
	return credential, nil
}

func normalizeCredentialPolicy(policy CredentialPolicy, projectID string, now time.Time) (CredentialPolicy, error) {
	if policy.NotBefore != nil {
		value := policy.NotBefore.UTC()
		policy.NotBefore = &value
	}
	if policy.ExpiresAt != nil {
		value := policy.ExpiresAt.UTC()
		policy.ExpiresAt = &value
		if !value.After(now) || (policy.NotBefore != nil && !value.After(*policy.NotBefore)) {
			return CredentialPolicy{}, &CLIError{Code: "CHS-AUTH-VALIDITY", Message: "expires_at must be later than now and not_before", ExitCode: 20}
		}
	}
	policy.Resources = normalizeResourceScope(policy.Resources)
	if len(policy.Resources.Projects) != 0 && !containsString(policy.Resources.Projects, projectID) {
		return CredentialPolicy{}, &CLIError{Code: "CHS-AUTH-RESOURCE", Message: "project scope must include the current project ID", ExitCode: 20}
	}
	return policy, nil
}

func normalizeResourceScope(scope ResourceScope) ResourceScope {
	scope.Projects = normalizeScopeList(scope.Projects)
	scope.Missions = normalizeScopeList(scope.Missions)
	scope.Tasks = normalizeScopeList(scope.Tasks)
	scope.Submissions = normalizeScopeList(scope.Submissions)
	scope.SubmissionDigests = normalizeScopeList(scope.SubmissionDigests)
	scope.Heads = normalizeScopeList(scope.Heads)
	scope.Baselines = normalizeScopeList(scope.Baselines)
	return scope
}

func normalizeScopeList(values []string) []string {
	set := map[string]struct{}{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			set[value] = struct{}{}
		}
	}
	result := make([]string, 0, len(set))
	for value := range set {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func revokeCredentialExpected(rootDir, rootPath, credentialID, reason string, expectedRevision int64) error {
	_, publicRoot, privateRoot, err := loadRoot(rootPath)
	if err != nil {
		return err
	}
	lock, err := acquireLock(rootDir)
	if err != nil {
		return err
	}
	defer lock.release()
	if err := requireNoPendingOperations(rootDir); err != nil {
		return err
	}
	config, trust, _, err := loadProject(rootDir)
	if err != nil {
		return err
	}
	if _, err := verifyProject(rootDir); err != nil {
		return err
	}
	if keyFingerprint(publicRoot) != config.RootFingerprint {
		return &CLIError{Code: "CHS-AUTH-ROOT-MISMATCH", Message: "Master Root does not own this project", ExitCode: 11}
	}
	if expectedRevision >= 0 && trust.Revision != expectedRevision {
		return trustRevisionConflict(expectedRevision, trust.Revision)
	}
	found := false
	for _, grant := range trust.Grants {
		if grant.ID == credentialID {
			found = true
			break
		}
	}
	if !found {
		return &CLIError{Code: "CHS-AUTH-NOT-FOUND", Message: "credential grant not found", ExitCode: 10}
	}
	for _, revoked := range trust.Revocations {
		if revoked.CredentialID == credentialID {
			return nil
		}
	}
	now := timeNow()
	before, err := cloneTrust(trust)
	if err != nil {
		return err
	}
	after, err := cloneTrust(trust)
	if err != nil {
		return err
	}
	after.Revocations = append(after.Revocations, Revocation{CredentialID: credentialID, RevokedAt: now, Reason: reason})
	after.Revision++
	after.UpdatedAt = now
	if err := signTrust(&after, privateRoot); err != nil {
		return err
	}
	operationID, err := newID("AUTHOP")
	if err != nil {
		return err
	}
	journal := AuthOperationJournal{
		Version: authOperationVersion, ID: operationID, Action: "auth.revoke", CredentialID: credentialID,
		ExpectedTrustRevision: trust.Revision, Phase: authOperationPrepared, BeforeTrust: before, AfterTrust: after,
		CreatedAt: now, UpdatedAt: now,
	}
	if err := writeAuthOperationJournal(rootDir, journal); err != nil {
		return err
	}
	if err := injectAuthOperationFault("prepared"); err != nil {
		return err
	}
	_, trustPath, _, _ := projectPaths(rootDir)
	if err := writeYAMLAtomic(trustPath, &after, 0o644); err != nil {
		return err
	}
	if err := injectAuthOperationFault("trust_committed_before_phase"); err != nil {
		return err
	}
	journal.Phase = authOperationTrustCommitted
	journal.UpdatedAt = timeNow()
	if err := writeAuthOperationJournal(rootDir, journal); err != nil {
		return err
	}
	if err := injectAuthOperationFault("trust_committed"); err != nil {
		return err
	}
	return removeAuthOperationJournal(rootDir, operationID)
}

func grantedActions(role string, requested []string) ([]string, error) {
	allowed, ok := roleActions[role]
	if !ok {
		return nil, &CLIError{Code: "CHS-AUTH-ROLE", Message: "unknown role: " + role, ExitCode: 20}
	}
	actions := append([]string{}, allowed...)
	if len(requested) > 0 {
		allowedSet := stringSet(allowed)
		actions = nil
		for _, action := range requested {
			if _, ok := allowedSet[action]; !ok {
				return nil, &CLIError{Code: "CHS-AUTH-ACTION", Message: fmt.Sprintf("role %s cannot be granted action %s", role, action), ExitCode: 11}
			}
			actions = append(actions, action)
		}
	}
	sort.Strings(actions)
	return actions, nil
}

func recoverAuthOperationsLocked(root string, config Config) error {
	journals, err := listAuthOperationJournals(root)
	if err != nil {
		return err
	}
	for _, journal := range journals {
		if journal.Version != authOperationVersion || journal.ID == "" || !containsString([]string{"auth.issue", "auth.revoke"}, journal.Action) || journal.BeforeTrust.Revision != journal.ExpectedTrustRevision || journal.AfterTrust.Revision != journal.ExpectedTrustRevision+1 {
			return &CLIError{Code: "CHS-AUTH-OPERATION-INVALID", Message: "authorization operation journal is invalid", ExitCode: 40}
		}
		_, trustPath, _, _ := projectPaths(root)
		var current Trust
		if err := loadYAML(trustPath, &current); err != nil {
			return err
		}
		if err := verifyTrust(config, current); err != nil {
			return err
		}
		atBefore := equalCanonicalJSON(current, journal.BeforeTrust)
		atAfter := equalCanonicalJSON(current, journal.AfterTrust)
		switch journal.Action {
		case "auth.issue":
			if err := validateAuthCredentialPaths(journal); err != nil {
				return err
			}
			if atBefore {
				if _, err := os.Stat(journal.OutputPath); err == nil {
					return authOperationIntegrityBlocked(journal)
				} else if !os.IsNotExist(err) {
					return err
				}
				if err := removeCredentialTemp(journal); err != nil {
					return err
				}
			} else if atAfter {
				if err := publishCredentialFile(journal); err != nil {
					return err
				}
			} else {
				return authOperationIntegrityBlocked(journal)
			}
		case "auth.revoke":
			if !atBefore && !atAfter {
				return authOperationIntegrityBlocked(journal)
			}
		}
		if err := removeAuthOperationJournal(root, journal.ID); err != nil {
			return err
		}
	}
	return nil
}

func publishCredentialFile(journal AuthOperationJournal) error {
	if err := validateAuthCredentialPaths(journal); err != nil {
		return err
	}
	if data, err := os.ReadFile(journal.OutputPath); err == nil {
		if digestBytes(data) != journal.CredentialDigest {
			return authOperationIntegrityBlocked(journal)
		}
		return removeCredentialTemp(journal)
	} else if !os.IsNotExist(err) {
		return err
	}
	data, err := os.ReadFile(journal.TempPath)
	if err != nil || digestBytes(data) != journal.CredentialDigest {
		return authOperationIntegrityBlocked(journal)
	}
	if err := os.Rename(journal.TempPath, journal.OutputPath); err != nil {
		return err
	}
	directory, err := os.Open(filepath.Dir(journal.OutputPath))
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}

func validateAuthCredentialPaths(journal AuthOperationJournal) error {
	if journal.OutputPath == "" || journal.TempPath == "" || journal.CredentialDigest == "" || filepath.Dir(journal.OutputPath) != filepath.Dir(journal.TempPath) || !strings.Contains(filepath.Base(journal.TempPath), journal.ID) {
		return &CLIError{Code: "CHS-AUTH-OPERATION-INVALID", Message: "authorization journal credential paths are invalid", ExitCode: 40}
	}
	return nil
}

func removeCredentialTemp(journal AuthOperationJournal) error {
	if journal.TempPath == "" {
		return nil
	}
	if err := os.Remove(journal.TempPath); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func requireNoPendingOperations(root string) error {
	workflow, err := listOperationJournals(root)
	if err != nil {
		return err
	}
	auth, err := listAuthOperationJournals(root)
	if err != nil {
		return err
	}
	if err := requireNoPendingPublishOperations(root); err != nil {
		return err
	}
	if len(workflow) != 0 || len(auth) != 0 {
		return &CLIError{Code: "CHS-OPERATION-RECOVERY-REQUIRED", Message: "an unfinished workflow or authorization operation must be recovered before another write", ExitCode: 40, Remedy: []string{"run chassiss recover"}}
	}
	return nil
}

func trustRevisionConflict(expected, current int64) error {
	return &CLIError{Code: "CHS-CONFLICT-TRUST-REVISION", Message: fmt.Sprintf("expected trust revision %d, current revision is %d", expected, current), ExitCode: 12, Retryable: true, Remedy: []string{"run chassiss status", "reload trust metadata and retry"}}
}

func cloneTrust(trust Trust) (Trust, error) {
	data, err := json.Marshal(trust)
	if err != nil {
		return Trust{}, err
	}
	var cloned Trust
	if err := json.Unmarshal(data, &cloned); err != nil {
		return Trust{}, err
	}
	return cloned, nil
}

func authOperationJournalPath(root, id string) string {
	return filepath.Join(root, ".chassis", "auth-operations", id+".json")
}

func writeAuthOperationJournal(root string, journal AuthOperationJournal) error {
	return writeJSONAtomic(authOperationJournalPath(root, journal.ID), journal, 0o600)
}

func removeAuthOperationJournal(root, id string) error {
	path := authOperationJournalPath(root, id)
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

func listAuthOperationJournals(root string) ([]AuthOperationJournal, error) {
	directory := filepath.Join(root, ".chassis", "auth-operations")
	entries, err := os.ReadDir(directory)
	if os.IsNotExist(err) {
		return []AuthOperationJournal{}, nil
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
	result := make([]AuthOperationJournal, 0, len(names))
	for _, name := range names {
		data, err := os.ReadFile(filepath.Join(directory, name))
		if err != nil {
			return nil, err
		}
		var journal AuthOperationJournal
		if err := strictJSON(data, &journal); err != nil {
			return nil, err
		}
		result = append(result, journal)
	}
	return result, nil
}

func authOperationIntegrityBlocked(journal AuthOperationJournal) error {
	return &CLIError{Code: "CHS-INTEGRITY-BLOCKED", Message: "authorization state does not match unfinished operation " + journal.ID, ExitCode: 40, Remedy: []string{"do not edit trust.yaml or credential files", "inspect .chassis/auth-operations/" + journal.ID + ".json"}}
}

func injectAuthOperationFault(point string) error {
	if authOperationFaultHook == nil {
		return nil
	}
	return authOperationFaultHook(point)
}
