package app

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCredentialArmorRoundTripAndIntegrity(t *testing.T) {
	directory := t.TempDir()
	rootPath := filepath.Join(directory, "master-root.yaml")
	if _, err := createRoot(rootPath); err != nil {
		t.Fatal(err)
	}
	project := filepath.Join(directory, "project")
	if _, _, err := initializeProject(project, rootPath, false); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(directory, "developer.yaml")
	if _, err := issueCredential(project, rootPath, "codex", "developer", source, nil); err != nil {
		t.Fatal(err)
	}
	exported, err := exportCredentialArmor(source)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(exported.Armor, credentialArmorBegin+"\n") || strings.Count(strings.TrimSpace(exported.Armor), "\n") != 2 {
		t.Fatalf("armor is not a three-line block:\n%s", exported.Armor)
	}
	output := filepath.Join(directory, "imported", "credential.yaml")
	imported, err := importCredentialArmor(strings.NewReader(exported.Armor), output)
	if err != nil {
		t.Fatal(err)
	}
	sourceData, _ := os.ReadFile(source)
	outputData, _ := os.ReadFile(output)
	if !bytes.Equal(sourceData, outputData) || imported.Digest != digestBytes(sourceData) {
		t.Fatal("credential import did not preserve the exact YAML bytes")
	}
	info, err := os.Stat(output)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("imported credential mode = %v, %v", info.Mode().Perm(), err)
	}
	if _, err := importCredentialArmor(strings.NewReader(exported.Armor), output); err == nil {
		t.Fatal("credential import overwrote an existing file")
	}

	lines := strings.Split(strings.TrimSpace(exported.Armor), "\n")
	rawEnvelope, _ := base64.StdEncoding.DecodeString(lines[1])
	var envelope credentialEnvelope
	if err := json.Unmarshal(rawEnvelope, &envelope); err != nil {
		t.Fatal(err)
	}
	envelope.Payload += "\n"
	rawEnvelope, _ = json.Marshal(envelope)
	lines[1] = base64.StdEncoding.EncodeToString(rawEnvelope)
	tampered := strings.Join(lines, "\n") + "\n"
	if _, err := importCredentialArmor(strings.NewReader(tampered), filepath.Join(directory, "tampered.yaml")); err == nil {
		t.Fatal("credential import accepted a digest mismatch")
	} else if typed, ok := err.(*CLIError); !ok || typed.Diagnostic != "digest_mismatch" {
		t.Fatalf("tampered armor error = %#v", err)
	}
	if _, err := exportCredentialArmor(rootPath); err == nil {
		t.Fatal("Master Root was exported as a role credential")
	}
}

func TestConvenientCredentialIssueExportImportCLI(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	rootPath := filepath.Join(home, ".chassiss", "master-root.yaml")
	var stdout, stderr bytes.Buffer
	if exit := Run([]string{"auth", "master-init"}, &stdout, &stderr); exit != 0 {
		t.Fatalf("master-init exit=%d stdout=%s stderr=%s", exit, stdout.String(), stderr.String())
	}
	if _, err := os.Stat(rootPath); err != nil {
		t.Fatal(err)
	}
	project := filepath.Join(t.TempDir(), "project")
	if _, _, err := initializeProject(project, rootPath, false); err != nil {
		t.Fatal(err)
	}
	stdout.Reset()
	stderr.Reset()
	if exit := Run([]string{"--root", project, "auth", "issue", "--actor", "codex", "--role", "developer"}, &stdout, &stderr); exit != 0 {
		t.Fatalf("auth issue exit=%d stdout=%s stderr=%s", exit, stdout.String(), stderr.String())
	}
	issued := filepath.Join(home, ".chassiss", "cred-codex.yaml")
	if _, err := os.Stat(issued); err != nil {
		t.Fatal(err)
	}
	stdout.Reset()
	stderr.Reset()
	if exit := Run([]string{"auth", "export", issued}, &stdout, &stderr); exit != 0 {
		t.Fatalf("auth export exit=%d stdout=%s stderr=%s", exit, stdout.String(), stderr.String())
	}
	armor := stdout.String()
	imported := filepath.Join(home, ".chassiss", "my-cred.yaml")
	stdout.Reset()
	stderr.Reset()
	if exit := RunWithInput([]string{"auth", "import", "--output", imported}, strings.NewReader(armor), &stdout, &stderr); exit != 0 {
		t.Fatalf("auth import exit=%d stdout=%s stderr=%s", exit, stdout.String(), stderr.String())
	}
	if _, err := loadPrincipal(project, imported, "work.open"); err != nil {
		t.Fatalf("imported credential did not bootstrap against project trust: %v", err)
	}
}

func TestCredentialDiagnosticCategories(t *testing.T) {
	directory := t.TempDir()
	rootPath := filepath.Join(directory, "master-root.yaml")
	if _, err := createRoot(rootPath); err != nil {
		t.Fatal(err)
	}
	project := filepath.Join(directory, "project")
	if _, _, err := initializeProject(project, rootPath, false); err != nil {
		t.Fatal(err)
	}
	credentialPath := filepath.Join(directory, "developer.yaml")
	credential, err := issueCredential(project, rootPath, "codex", "developer", credentialPath, nil)
	if err != nil {
		t.Fatal(err)
	}
	tampered := *credential
	tampered.Actor = "other"
	tamperedPath := filepath.Join(directory, "tampered.yaml")
	if err := writeYAMLAtomic(tamperedPath, &tampered, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadPrincipal(project, tamperedPath, "work.open"); err == nil {
		t.Fatal("tampered credential was accepted")
	} else if typed, ok := err.(*CLIError); !ok || typed.Diagnostic != "metadata_mismatch" {
		t.Fatalf("tampered credential diagnostic = %#v", err)
	}
	var stdout, stderr bytes.Buffer
	if exit := Run([]string{"--json", "--root", project, "--credential", tamperedPath, "bootstrap"}, &stdout, &stderr); exit == 0 {
		t.Fatal("tampered credential bootstrap succeeded")
	}
	var failure Response
	if err := json.Unmarshal(stderr.Bytes(), &failure); err != nil || failure.Error == nil || failure.Error.DiagnosticCategory != "metadata_mismatch" {
		t.Fatalf("JSON credential diagnostic = %#v, parse error = %v", failure.Error, err)
	}
	if err := revokeCredential(project, rootPath, credential.ID, "test"); err != nil {
		t.Fatal(err)
	}
	if _, err := loadPrincipal(project, credentialPath, "work.open"); err == nil {
		t.Fatal("revoked credential was accepted")
	} else if typed, ok := err.(*CLIError); !ok || typed.Code != "CHS-AUTH-REVOKED" || typed.Diagnostic != "revoked" {
		t.Fatalf("revoked credential diagnostic = %#v", err)
	}
	config, trust, _, err := loadProject(project)
	if err != nil {
		t.Fatal(err)
	}
	trust.Signature = "invalid"
	if err := verifyTrust(config, trust); err == nil {
		t.Fatal("invalid trust signature was accepted")
	} else if typed, ok := err.(*CLIError); !ok || typed.Diagnostic != "signature_invalid" {
		t.Fatalf("trust signature diagnostic = %#v", err)
	}
}
