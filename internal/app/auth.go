package app

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

var roleActions = map[string][]string{
	"designer": {
		"artifact.submit",
	},
	"orchestrator": {
		"mission.activate", "mission.block", "mission.resume", "mission.submit-acceptance",
		"task.claim", "task.assign", "task.block", "task.resume",
	},
	"developer": {
		"work.open", "work.check", "work.checkpoint", "work.submit", "work.block",
	},
	"reviewer": {
		"review.approve", "review.request-changes", "integrate.apply",
	},
}

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
	if trust.ProjectID != config.ProjectID || trust.Version < 1 {
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
	actions := map[string]struct{}{
		"auth.issue": {}, "auth.revoke": {}, "artifact.accept": {}, "artifact.reject": {}, "mission.accept": {},
	}
	return Principal{ID: root.ID, Actor: "master", Role: "master", Actions: actions, PrivateKey: private, PublicKey: public}
}

func issueCredential(rootDir string, rootPath, actor, role, output string, requested []string) (*Credential, error) {
	rootKey, publicRoot, privateRoot, err := loadRoot(rootPath)
	if err != nil {
		return nil, err
	}
	config, trust, _, err := loadProject(rootDir)
	if err != nil {
		return nil, err
	}
	if keyFingerprint(publicRoot) != config.RootFingerprint {
		return nil, &CLIError{Code: "CHS-AUTH-ROOT-MISMATCH", Message: "Master Root does not own this project", ExitCode: 11}
	}
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
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	id, err := newID("CRED")
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	grant := Grant{ID: id, Actor: actor, Role: role, Actions: actions, PublicKey: base64.RawStdEncoding.EncodeToString(public), IssuedAt: now}
	trust.Grants = append(trust.Grants, grant)
	trust.Version++
	trust.UpdatedAt = now
	if err := signTrust(&trust, privateRoot); err != nil {
		return nil, err
	}
	credential := &Credential{
		Kind: "chassiss-role-credential", Version: CredentialVersion, ID: id, ProjectID: config.ProjectID,
		RootFingerprint: keyFingerprint(publicRoot), Actor: actor, Role: role, Actions: actions,
		PrivateKey: base64.RawStdEncoding.EncodeToString(private), IssuedAt: now,
	}
	if err := writeYAMLAtomic(output, credential, 0o600); err != nil {
		return nil, err
	}
	if err := writeYAMLAtomic(filepath.Join(rootDir, ".chassis", "trust.yaml"), &trust, 0o644); err != nil {
		return nil, err
	}
	_ = rootKey
	return credential, nil
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
	actions := stringSet(credential.Actions)
	if _, ok := actions[action]; action != "" && !ok {
		return Principal{}, &CLIError{Code: "CHS-AUTH-DENIED", Message: fmt.Sprintf("role %s cannot perform %s", credential.Role, action), ExitCode: 11}
	}
	return Principal{ID: credential.ID, Actor: credential.Actor, Role: credential.Role, Actions: actions, PrivateKey: private, PublicKey: public}, nil
}

func revokeCredential(rootDir, rootPath, credentialID, reason string) error {
	_, publicRoot, privateRoot, err := loadRoot(rootPath)
	if err != nil {
		return err
	}
	config, trust, _, err := loadProject(rootDir)
	if err != nil {
		return err
	}
	if keyFingerprint(publicRoot) != config.RootFingerprint {
		return &CLIError{Code: "CHS-AUTH-ROOT-MISMATCH", Message: "Master Root does not own this project", ExitCode: 11}
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
	trust.Revocations = append(trust.Revocations, Revocation{CredentialID: credentialID, RevokedAt: time.Now().UTC(), Reason: reason})
	trust.Version++
	trust.UpdatedAt = time.Now().UTC()
	if err := signTrust(&trust, privateRoot); err != nil {
		return err
	}
	return writeYAMLAtomic(filepath.Join(rootDir, ".chassis", "trust.yaml"), &trust, 0o644)
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
