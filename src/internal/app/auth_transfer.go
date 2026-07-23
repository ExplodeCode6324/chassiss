package app

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"

	"gopkg.in/yaml.v3"
)

const (
	credentialArmorBegin   = "-----BEGIN CHASSISS CREDENTIAL-----"
	credentialArmorEnd     = "-----END CHASSISS CREDENTIAL-----"
	credentialEnvelopeKind = "chassiss-credential-envelope"
	credentialEnvelopeV1   = 1
	maxCredentialArmorSize = 256 * 1024
)

type credentialEnvelope struct {
	Kind    string `json:"kind"`
	Version int    `json:"version"`
	Digest  string `json:"digest"`
	Payload string `json:"payload"`
}

type credentialTransferResult struct {
	ID       string `json:"id"`
	Actor    string `json:"actor"`
	Role     string `json:"role"`
	Digest   string `json:"digest"`
	Path     string `json:"path,omitempty"`
	Armor    string `json:"armor,omitempty"`
	Imported bool   `json:"imported,omitempty"`
}

func exportCredentialArmor(path string) (credentialTransferResult, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return credentialTransferResult{}, err
	}
	credential, err := parseRoleCredential(data)
	if err != nil {
		return credentialTransferResult{}, err
	}
	envelope := credentialEnvelope{
		Kind: credentialEnvelopeKind, Version: credentialEnvelopeV1,
		Digest: digestBytes(data), Payload: string(data),
	}
	encoded, err := json.Marshal(envelope)
	if err != nil {
		return credentialTransferResult{}, err
	}
	armor := credentialArmorBegin + "\n" + base64.StdEncoding.EncodeToString(encoded) + "\n" + credentialArmorEnd + "\n"
	return credentialTransferResult{ID: credential.ID, Actor: credential.Actor, Role: credential.Role, Digest: envelope.Digest, Path: absolutePath(path), Armor: armor}, nil
}

func importCredentialArmor(input io.Reader, output string) (credentialTransferResult, error) {
	if strings.TrimSpace(output) == "" {
		return credentialTransferResult{}, usageError("auth import requires --output")
	}
	limited := io.LimitReader(input, maxCredentialArmorSize+1)
	armor, err := io.ReadAll(limited)
	if err != nil {
		return credentialTransferResult{}, err
	}
	if len(armor) > maxCredentialArmorSize {
		return credentialTransferResult{}, credentialTransferError("armor_too_large", "credential armor exceeds the supported size")
	}
	envelope, data, err := decodeCredentialArmor(armor)
	if err != nil {
		return credentialTransferResult{}, err
	}
	credential, err := parseRoleCredential(data)
	if err != nil {
		return credentialTransferResult{}, err
	}
	absolute, err := filepath.Abs(output)
	if err != nil {
		return credentialTransferResult{}, err
	}
	if err := os.MkdirAll(filepath.Dir(absolute), 0o700); err != nil {
		return credentialTransferResult{}, err
	}
	if _, err := os.Lstat(absolute); err == nil {
		return credentialTransferResult{}, &CLIError{Code: "CHS-AUTH-CREDENTIAL-EXISTS", Message: "refusing to overwrite an existing credential file", Diagnostic: "output_exists", ExitCode: 10}
	} else if !os.IsNotExist(err) {
		return credentialTransferResult{}, err
	}
	if err := writeAtomic(absolute, data, 0o600); err != nil {
		return credentialTransferResult{}, err
	}
	return credentialTransferResult{ID: credential.ID, Actor: credential.Actor, Role: credential.Role, Digest: envelope.Digest, Path: absolute, Imported: true}, nil
}

func decodeCredentialArmor(armor []byte) (credentialEnvelope, []byte, error) {
	text := strings.ReplaceAll(string(armor), "\r\n", "\n")
	lines := strings.Split(strings.TrimSpace(text), "\n")
	if len(lines) != 3 || lines[0] != credentialArmorBegin || lines[2] != credentialArmorEnd || strings.TrimSpace(lines[1]) != lines[1] || lines[1] == "" {
		return credentialEnvelope{}, nil, credentialTransferError("armor_format_invalid", "credential armor must contain exactly one header, one base64 payload line, and one footer")
	}
	decoded, err := base64.StdEncoding.Strict().DecodeString(lines[1])
	if err != nil {
		return credentialEnvelope{}, nil, credentialTransferError("armor_base64_invalid", "credential armor payload is not valid base64")
	}
	var envelope credentialEnvelope
	decoder := json.NewDecoder(bytes.NewReader(decoded))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&envelope); err != nil {
		return credentialEnvelope{}, nil, credentialTransferError("envelope_invalid", "credential envelope is invalid")
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return credentialEnvelope{}, nil, credentialTransferError("envelope_invalid", "credential envelope contains trailing data")
	}
	if envelope.Kind != credentialEnvelopeKind || envelope.Version != credentialEnvelopeV1 {
		return credentialEnvelope{}, nil, credentialTransferError("envelope_version_unsupported", "credential envelope kind or version is unsupported")
	}
	data := []byte(envelope.Payload)
	if envelope.Digest == "" || digestBytes(data) != envelope.Digest {
		return credentialEnvelope{}, nil, credentialTransferError("digest_mismatch", "credential envelope digest does not match its payload")
	}
	return envelope, data, nil
}

func parseRoleCredential(data []byte) (Credential, error) {
	var header struct {
		Kind string `yaml:"kind"`
	}
	if err := yaml.Unmarshal(data, &header); err != nil {
		return Credential{}, credentialTransferError("credential_malformed", "credential YAML is invalid")
	}
	if header.Kind == "chassiss-master-root" {
		return Credential{}, credentialTransferError("master_root_export_denied", "Master Root credentials cannot be exported or imported as role credentials")
	}
	var credential Credential
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(&credential); err != nil {
		return Credential{}, credentialTransferError("credential_malformed", "credential YAML is invalid")
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return Credential{}, credentialTransferError("credential_malformed", "credential YAML must contain exactly one document")
	}
	if credential.Kind != "chassiss-role-credential" || credential.Version != CredentialVersion || credential.ID == "" || credential.ProjectID == "" || credential.RootFingerprint == "" || !validActor(credential.Actor) || credential.IssuedAt.IsZero() {
		return Credential{}, credentialTransferError("credential_metadata_invalid", "credential metadata is incomplete or unsupported")
	}
	allowed := stringSet(actionsForRole(credential.Role))
	if len(allowed) == 0 || len(credential.Actions) == 0 {
		return Credential{}, credentialTransferError("credential_role_invalid", "credential role or action grant is invalid")
	}
	for _, action := range credential.Actions {
		if _, ok := allowed[action]; !ok {
			return Credential{}, credentialTransferError("credential_action_invalid", "credential contains an action not allowed for its role")
		}
	}
	private, err := base64.RawStdEncoding.DecodeString(credential.PrivateKey)
	if err != nil || len(private) != ed25519.PrivateKeySize {
		return Credential{}, credentialTransferError("key_invalid", "credential private key is invalid")
	}
	return credential, nil
}

func credentialTransferError(category, message string) error {
	return &CLIError{Code: "CHS-AUTH-TRANSFER", Message: message, Diagnostic: category, ExitCode: 11}
}

func defaultChassissDirectory() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".chassiss"), nil
}

func defaultMasterRootPath() (string, error) {
	directory, err := defaultChassissDirectory()
	if err != nil {
		return "", err
	}
	return filepath.Join(directory, "master-root.yaml"), nil
}

func defaultCredentialPath(actor string) (string, error) {
	directory, err := defaultChassissDirectory()
	if err != nil {
		return "", err
	}
	var name strings.Builder
	lastDash := false
	for _, value := range actor {
		allowed := unicode.IsLetter(value) || unicode.IsDigit(value) || strings.ContainsRune("._-", value)
		if allowed {
			name.WriteRune(value)
			lastDash = false
			continue
		}
		if !lastDash {
			name.WriteByte('-')
			lastDash = true
		}
	}
	safe := strings.Trim(name.String(), ".-")
	if safe == "" {
		safe = "agent"
	}
	return filepath.Join(directory, "cred-"+safe+".yaml"), nil
}

func discoverMasterRoot(rootFingerprint string) (string, error) {
	directory, err := defaultChassissDirectory()
	if err != nil {
		return "", err
	}
	candidates := []string{filepath.Join(directory, "master-root.yaml")}
	rootsDirectory := filepath.Join(directory, "roots")
	if entries, readErr := os.ReadDir(rootsDirectory); readErr == nil {
		for _, entry := range entries {
			if !entry.IsDir() && (filepath.Ext(entry.Name()) == ".yaml" || filepath.Ext(entry.Name()) == ".yml") {
				candidates = append(candidates, filepath.Join(rootsDirectory, entry.Name()))
			}
		}
	}
	sort.Strings(candidates[1:])
	matches := []string{}
	for _, candidate := range candidates {
		_, public, _, loadErr := loadRoot(candidate)
		if loadErr == nil && keyFingerprint(public) == rootFingerprint {
			matches = append(matches, candidate)
		}
	}
	if len(matches) == 1 {
		return matches[0], nil
	}
	if len(matches) == 0 {
		return "", &CLIError{
			Code: "CHS-AUTH-ROOT-NOT-FOUND", Message: "no local Master Root matches this project",
			Diagnostic: "matching_root_not_found", ExitCode: 11,
			Remedy: []string{"store the matching root at ~/.chassiss/master-root.yaml or pass --master-root to auth issue"},
		}
	}
	return "", &CLIError{
		Code: "CHS-AUTH-ROOT-AMBIGUOUS", Message: fmt.Sprintf("multiple local Master Roots match this project: %s", strings.Join(matches, ", ")),
		Diagnostic: "matching_root_ambiguous", ExitCode: 11,
		Remedy: []string{"pass --master-root to auth issue explicitly"},
	}
}
