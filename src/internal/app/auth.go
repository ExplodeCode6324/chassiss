package app

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"
)

func createRoot(path string) (*RootKey, error) {
	if _, err := os.Stat(path); err == nil {
		return nil, &CLIError{Code: "CHS-AUTH-ROOT-EXISTS", Message: "refusing to overwrite existing Master Root", ExitCode: 10}
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	id, err := newID("ROOT")
	if err != nil {
		return nil, err
	}
	root := &RootKey{
		Kind: "chassiss-master-root", Version: CredentialVersion, ID: id,
		PublicKey: base64.RawStdEncoding.EncodeToString(public), PrivateKey: base64.RawStdEncoding.EncodeToString(private),
		CreatedAt: time.Now().UTC(),
	}
	if err := writeYAMLAtomic(path, root, 0o600); err != nil {
		return nil, err
	}
	return root, nil
}

func loadRoot(path string) (*RootKey, ed25519.PublicKey, ed25519.PrivateKey, error) {
	var root RootKey
	if err := loadYAML(path, &root); err != nil {
		return nil, nil, nil, err
	}
	if root.Kind != "chassiss-master-root" || root.Version != CredentialVersion {
		return nil, nil, nil, &CLIError{Code: "CHS-AUTH-ROOT-INVALID", Message: "credential is not a supported Master Root", ExitCode: 11}
	}
	public, err := base64.RawStdEncoding.DecodeString(root.PublicKey)
	if err != nil || len(public) != ed25519.PublicKeySize {
		return nil, nil, nil, &CLIError{Code: "CHS-AUTH-ROOT-INVALID", Message: "Master Root public key is invalid", ExitCode: 11}
	}
	private, err := base64.RawStdEncoding.DecodeString(root.PrivateKey)
	if err != nil || len(private) != ed25519.PrivateKeySize {
		return nil, nil, nil, &CLIError{Code: "CHS-AUTH-ROOT-INVALID", Message: "Master Root private key is invalid", ExitCode: 11}
	}
	if !ed25519.PublicKey(public).Equal(ed25519.PrivateKey(private).Public()) {
		return nil, nil, nil, &CLIError{Code: "CHS-AUTH-ROOT-INVALID", Message: "Master Root keypair does not match", ExitCode: 11}
	}
	return &root, ed25519.PublicKey(public), ed25519.PrivateKey(private), nil
}

func keyFingerprint(public []byte) string {
	sum := sha256.Sum256(public)
	return "ed25519:" + hex.EncodeToString(sum[:])
}

func trustSigningBytes(trust Trust) ([]byte, error) {
	trust.Signature = ""
	trust.Grants = append([]Grant(nil), trust.Grants...)
	trust.Revocations = append([]Revocation(nil), trust.Revocations...)
	sort.Slice(trust.Grants, func(i, j int) bool { return trust.Grants[i].ID < trust.Grants[j].ID })
	sort.Slice(trust.Revocations, func(i, j int) bool { return trust.Revocations[i].CredentialID < trust.Revocations[j].CredentialID })
	return canonicalJSON(trust)
}

func signTrust(trust *Trust, private ed25519.PrivateKey) error {
	data, err := trustSigningBytes(*trust)
	if err != nil {
		return err
	}
	trust.Signature = base64.RawStdEncoding.EncodeToString(ed25519.Sign(private, data))
	return nil
}

func verifyTrust(config Config, trust Trust) error {
	if trust.ProjectID != config.ProjectID || trust.Version != TrustVersion || trust.Revision < 1 {
		return &CLIError{Code: "CHS-INTEGRITY-TRUST", Message: "trust metadata does not match project", ExitCode: 40}
	}
	public, err := base64.RawStdEncoding.DecodeString(trust.RootPublicKey)
	if err != nil || len(public) != ed25519.PublicKeySize || keyFingerprint(public) != config.RootFingerprint {
		return &CLIError{Code: "CHS-INTEGRITY-TRUST", Message: "trust root does not match the project root fingerprint", ExitCode: 40}
	}
	signature, err := base64.RawStdEncoding.DecodeString(trust.Signature)
	if err != nil {
		return &CLIError{Code: "CHS-INTEGRITY-TRUST", Message: "trust signature is malformed", ExitCode: 40}
	}
	data, err := trustSigningBytes(trust)
	if err != nil {
		return err
	}
	if !ed25519.Verify(ed25519.PublicKey(public), data, signature) {
		return &CLIError{Code: "CHS-INTEGRITY-TRUST", Message: "trust metadata signature is invalid", ExitCode: 40}
	}
	return nil
}

func rootPrincipal(root *RootKey, public ed25519.PublicKey, private ed25519.PrivateKey) Principal {
	actions := stringSet(actionsForRole("master"))
	return Principal{ID: root.ID, Actor: "master", Role: "master", Actions: actions, PrivateKey: private, PublicKey: public}
}

func issueCredential(rootDir string, rootPath, actor, role, output string, requested []string) (*Credential, error) {
	return issueCredentialExpected(rootDir, rootPath, actor, role, output, requested, -1)
}

func validActor(actor string) bool {
	if len(actor) < 1 || len(actor) > 128 {
		return false
	}
	for _, value := range actor {
		if value <= ' ' || value == 0x7f {
			return false
		}
	}
	return true
}

func activeDeveloperGrant(trust Trust, actor string, at time.Time) (Grant, bool) {
	return activeDeveloperGrantForTask(trust, actor, "", at)
}

func activeDeveloperGrantForTask(trust Trust, actor, taskID string, at time.Time) (Grant, bool) {
	if !validActor(actor) {
		return Grant{}, false
	}
	revokedAt := map[string]time.Time{}
	for _, revocation := range trust.Revocations {
		if current, ok := revokedAt[revocation.CredentialID]; !ok || revocation.RevokedAt.Before(current) {
			revokedAt[revocation.CredentialID] = revocation.RevokedAt
		}
	}
	var selected Grant
	for _, grant := range trust.Grants {
		if grant.Actor != actor || grant.Role != "developer" || grant.IssuedAt.After(at) || !containsString(grant.Actions, "work.open") || !credentialTimeValid(grant.NotBefore, grant.ExpiresAt, at) || (taskID != "" && !scopeAllows(grant.Resources.Tasks, taskID)) {
			continue
		}
		if revoked, ok := revokedAt[grant.ID]; ok && !at.Before(revoked) {
			continue
		}
		if selected.ID == "" || grant.IssuedAt.After(selected.IssuedAt) || (grant.IssuedAt.Equal(selected.IssuedAt) && grant.ID < selected.ID) {
			selected = grant
		}
	}
	return selected, selected.ID != ""
}

func loadPrincipal(rootDir, credentialPath string, action string) (Principal, error) {
	if credentialPath == "" {
		return Principal{}, &CLIError{Code: "CHS-AUTH-MISSING", Message: "write command requires --credential", ExitCode: 11}
	}
	config, trust, _, err := loadProject(rootDir)
	if err != nil {
		return Principal{}, err
	}
	var header struct {
		Kind string `yaml:"kind"`
	}
	if err := loadYAML(credentialPath, &header); err != nil {
		return Principal{}, err
	}
	if header.Kind == "chassiss-master-root" {
		root, public, private, err := loadRoot(credentialPath)
		if err != nil {
			return Principal{}, err
		}
		if keyFingerprint(public) != config.RootFingerprint {
			return Principal{}, &CLIError{Code: "CHS-AUTH-ROOT-MISMATCH", Message: "Master Root does not own this project", ExitCode: 11}
		}
		principal := rootPrincipal(root, public, private)
		if _, ok := principal.Actions[action]; action != "" && !ok {
			return Principal{}, &CLIError{Code: "CHS-AUTH-DENIED", Message: "Master credential does not allow action " + action, ExitCode: 11}
		}
		return principal, nil
	}
	var credential Credential
	if err := loadYAML(credentialPath, &credential); err != nil {
		return Principal{}, err
	}
	if credential.Kind != "chassiss-role-credential" || credential.ProjectID != config.ProjectID || credential.RootFingerprint != config.RootFingerprint {
		return Principal{}, &CLIError{Code: "CHS-AUTH-CREDENTIAL", Message: "credential does not belong to this project", ExitCode: 11}
	}
	var grant *Grant
	for index := range trust.Grants {
		if trust.Grants[index].ID == credential.ID {
			grant = &trust.Grants[index]
			break
		}
	}
	if grant == nil || grant.Actor != credential.Actor || grant.Role != credential.Role || strings.Join(grant.Actions, "\x00") != strings.Join(credential.Actions, "\x00") {
		return Principal{}, &CLIError{Code: "CHS-AUTH-CREDENTIAL", Message: "credential is not present in current trust grants", ExitCode: 11}
	}
	if !equalCanonicalJSON(grant.NotBefore, credential.NotBefore) || !equalCanonicalJSON(grant.ExpiresAt, credential.ExpiresAt) || !equalCanonicalJSON(grant.Resources, credential.Resources) {
		return Principal{}, &CLIError{Code: "CHS-AUTH-CREDENTIAL", Message: "credential policy does not match its trust grant", ExitCode: 11}
	}
	for _, revoked := range trust.Revocations {
		if revoked.CredentialID == credential.ID {
			return Principal{}, &CLIError{Code: "CHS-AUTH-REVOKED", Message: "credential has been revoked", ExitCode: 11}
		}
	}
	private, err := base64.RawStdEncoding.DecodeString(credential.PrivateKey)
	if err != nil || len(private) != ed25519.PrivateKeySize {
		return Principal{}, &CLIError{Code: "CHS-AUTH-CREDENTIAL", Message: "credential private key is invalid", ExitCode: 11}
	}
	public := ed25519.PrivateKey(private).Public().(ed25519.PublicKey)
	if base64.RawStdEncoding.EncodeToString(public) != grant.PublicKey {
		return Principal{}, &CLIError{Code: "CHS-AUTH-CREDENTIAL", Message: "credential private key does not match trust grant", ExitCode: 11}
	}
	now := timeNow()
	if credential.NotBefore != nil && now.Before(*credential.NotBefore) {
		return Principal{}, &CLIError{Code: "CHS-AUTH-NOT-YET-VALID", Message: "credential is not valid yet", ExitCode: 11}
	}
	if credential.ExpiresAt != nil && !now.Before(*credential.ExpiresAt) {
		return Principal{}, &CLIError{Code: "CHS-AUTH-EXPIRED", Message: "credential has expired", ExitCode: 11}
	}
	if !scopeAllows(credential.Resources.Projects, config.ProjectID) {
		return Principal{}, &CLIError{Code: "CHS-AUTH-RESOURCE", Message: "credential is not scoped to this project", ExitCode: 11}
	}
	actions := stringSet(credential.Actions)
	if _, ok := actions[action]; action != "" && !ok {
		return Principal{}, &CLIError{Code: "CHS-AUTH-DENIED", Message: fmt.Sprintf("role %s cannot perform %s", credential.Role, action), ExitCode: 11}
	}
	return Principal{ID: credential.ID, Actor: credential.Actor, Role: credential.Role, Actions: actions, PrivateKey: private, PublicKey: public, Resources: credential.Resources}, nil
}

func credentialTimeValid(notBefore, expiresAt *time.Time, at time.Time) bool {
	return (notBefore == nil || !at.Before(*notBefore)) && (expiresAt == nil || at.Before(*expiresAt))
}

func scopeAllows(values []string, resource string) bool {
	return len(values) == 0 || containsString(values, resource)
}

func authorizeEventScope(scope ResourceScope, event Event) error {
	if !scopeAllows(scope.Projects, event.ProjectID) {
		return &CLIError{Code: "CHS-AUTH-RESOURCE", Message: "credential is not scoped to this project", ExitCode: 11}
	}
	switch {
	case strings.HasPrefix(event.Type, "mission."):
		if !scopeAllows(scope.Missions, event.Resource) {
			return &CLIError{Code: "CHS-AUTH-RESOURCE", Message: "credential is not scoped to mission " + event.Resource, ExitCode: 11}
		}
	case strings.HasPrefix(event.Type, "task."), strings.HasPrefix(event.Type, "work."):
		if !scopeAllows(scope.Tasks, event.Resource) {
			return &CLIError{Code: "CHS-AUTH-RESOURCE", Message: "credential is not scoped to task " + event.Resource, ExitCode: 11}
		}
	case strings.HasPrefix(event.Type, "review."):
		if !scopeAllows(scope.Submissions, event.Resource) {
			return &CLIError{Code: "CHS-AUTH-RESOURCE", Message: "credential is not scoped to submission " + event.Resource, ExitCode: 11}
		}
		var payload reviewRecordedPayload
		if err := decodePayload(event.Payload, &payload); err != nil {
			return err
		}
		if !scopeAllows(scope.SubmissionDigests, payload.SubmissionDigest) {
			return &CLIError{Code: "CHS-AUTH-RESOURCE", Message: "credential is not scoped to the submission digest", ExitCode: 11}
		}
	case event.Type == "integration.applied":
		if !scopeAllows(scope.Submissions, event.Resource) {
			return &CLIError{Code: "CHS-AUTH-RESOURCE", Message: "credential is not scoped to submission " + event.Resource, ExitCode: 11}
		}
		var payload integrationAppliedPayload
		if err := decodePayload(event.Payload, &payload); err != nil {
			return err
		}
		if !scopeAllows(scope.SubmissionDigests, payload.SubmissionDigest) || !scopeAllows(scope.Heads, payload.SubmissionHead) || !scopeAllows(scope.Baselines, payload.PreviousHead) {
			return &CLIError{Code: "CHS-AUTH-RESOURCE", Message: "credential does not match integration digest, head, or baseline scope", ExitCode: 11}
		}
	case event.Type == "publication.applied":
		var payload publicationAppliedPayload
		if err := decodePayload(event.Payload, &payload); err != nil {
			return err
		}
		if !scopeAllows(scope.Baselines, payload.Head) {
			return &CLIError{Code: "CHS-AUTH-RESOURCE", Message: "credential is not scoped to the published baseline", ExitCode: 11}
		}
	}
	return nil
}

func revokeCredential(rootDir, rootPath, credentialID, reason string) error {
	return revokeCredentialExpected(rootDir, rootPath, credentialID, reason, -1)
}

func stringSet(values []string) map[string]struct{} {
	result := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			result[value] = struct{}{}
		}
	}
	return result
}
