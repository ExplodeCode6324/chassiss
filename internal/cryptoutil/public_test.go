package cryptoutil

import (
	"crypto/ed25519"
	"crypto/rand"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"
)

func fixturePublicKey(t *testing.T) string {
	t.Helper()
	public, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	key, err := ssh.NewPublicKey(public)
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSuffix(string(ssh.MarshalAuthorizedKey(key)), "\n")
}

func TestCanonicalEd25519PublicKey(t *testing.T) {
	value := fixturePublicKey(t)
	if _, err := ParseEd25519PublicKey(value); err != nil {
		t.Fatal(err)
	}
	fingerprint, err := Fingerprint(value)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(fingerprint, "SHA256:") {
		t.Fatalf("unexpected fingerprint %s", fingerprint)
	}
	for _, invalid := range []string{
		value + " comment",
		value + "\n",
		"ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAABAQC7",
	} {
		if _, err := ParseEd25519PublicKey(invalid); err == nil {
			t.Fatalf("expected rejection for %q", invalid)
		}
	}
}
