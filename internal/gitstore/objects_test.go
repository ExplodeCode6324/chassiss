package gitstore

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"
)

func initRepository(t *testing.T) Runner {
	t.Helper()
	directory := t.TempDir()
	command := New("")
	if _, err := command.Run(context.Background(), "init", "-b", "main", directory); err != nil {
		t.Fatal(err)
	}
	return New(directory)
}

func TestTreeRoundTripAndOverlay(t *testing.T) {
	ctx := context.Background()
	runner := initRepository(t)
	a, err := runner.HashBlob(ctx, []byte("a\n"))
	if err != nil {
		t.Fatal(err)
	}
	b, err := runner.HashBlob(ctx, []byte("b\n"))
	if err != nil {
		t.Fatal(err)
	}
	c, err := runner.HashBlob(ctx, []byte("c\n"))
	if err != nil {
		t.Fatal(err)
	}
	base := TreeMap{
		"keep.txt": {Mode: "100644", OID: a},
		"work.txt": {Mode: "100644", OID: a},
	}
	work := TreeMap{
		"keep.txt": {Mode: "100644", OID: a},
		"work.txt": {Mode: "100644", OID: b},
		"new.txt":  {Mode: "100644", OID: c},
	}
	main := TreeMap{
		"keep.txt":  {Mode: "100644", OID: a},
		"work.txt":  {Mode: "100644", OID: a},
		"drift.txt": {Mode: "100644", OID: b},
	}
	overlay, changed, err := Overlay(base, work, main)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(changed, ",") != "new.txt,work.txt" {
		t.Fatalf("unexpected changed paths %v", changed)
	}
	if overlay["drift.txt"].OID != b || overlay["work.txt"].OID != b {
		t.Fatal("overlay lost main drift or Work change")
	}
	tree, err := runner.WriteTree(ctx, overlay)
	if err != nil {
		t.Fatal(err)
	}
	roundTrip, err := runner.ReadTree(ctx, tree)
	if err != nil {
		t.Fatal(err)
	}
	if got := ChangedPaths(overlay, roundTrip); len(got) != 0 {
		t.Fatalf("tree mapping changed after round trip: %v", got)
	}
	pureOID, err := TreeOID(overlay, "sha1")
	if err != nil {
		t.Fatal(err)
	}
	if pureOID != tree {
		t.Fatalf("pure tree OID %s differs from Git %s", pureOID, tree)
	}
}

func TestOverlayRejectsPrefixCollision(t *testing.T) {
	base := TreeMap{}
	work := TreeMap{
		"a":   {Mode: "100644", OID: strings.Repeat("a", 40)},
		"a/b": {Mode: "100644", OID: strings.Repeat("b", 40)},
	}
	if _, _, err := Overlay(base, work, TreeMap{}); err == nil {
		t.Fatal("expected prefix collision")
	}
}

func TestReadCommit(t *testing.T) {
	ctx := context.Background()
	runner := initRepository(t)
	if err := os.WriteFile(filepath.Join(runner.Repo, "README.md"), []byte("test\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := runner.Run(ctx, "add", "README.md"); err != nil {
		t.Fatal(err)
	}
	if _, err := runner.RunWithInput(ctx, nil, map[string]string{
		"GIT_AUTHOR_NAME": "Test", "GIT_AUTHOR_EMAIL": "test@example.invalid",
		"GIT_COMMITTER_NAME": "Test", "GIT_COMMITTER_EMAIL": "test@example.invalid",
	}, "commit", "-m", "initial"); err != nil {
		t.Fatal(err)
	}
	oid, err := runner.Resolve(ctx, "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	commit, err := runner.ReadCommit(ctx, oid)
	if err != nil {
		t.Fatal(err)
	}
	if len(commit.Parents) != 0 || commit.Message != "initial\n" {
		t.Fatalf("unexpected commit %#v", commit)
	}
}

func TestSignedCommitRoundTrip(t *testing.T) {
	ctx := context.Background()
	runner := initRepository(t)
	keyPath := filepath.Join(t.TempDir(), "signing-key")
	command := exec.Command("ssh-keygen", "-q", "-t", "ed25519", "-N", "", "-f", keyPath)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("ssh-keygen: %v: %s", err, output)
	}
	publicData, err := os.ReadFile(keyPath + ".pub")
	if err != nil {
		t.Fatal(err)
	}
	public, _, _, _, err := ssh.ParseAuthorizedKey(publicData)
	if err != nil {
		t.Fatal(err)
	}
	canonicalPublic := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(public)))
	blob, err := runner.HashBlob(ctx, []byte("signed\n"))
	if err != nil {
		t.Fatal(err)
	}
	tree, err := runner.WriteTree(ctx, TreeMap{
		"README.md": {Mode: "100644", OID: blob},
	})
	if err != nil {
		t.Fatal(err)
	}
	commit, err := runner.CommitTree(ctx, tree, nil, "signed test\n", keyPath, CommitIdentity{})
	if err != nil {
		t.Fatal(err)
	}
	if err := runner.VerifyCommitSSH(ctx, commit, canonicalPublic); err != nil {
		t.Fatal(err)
	}
	parsed, err := runner.ReadCommit(ctx, commit)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Signature == "" {
		t.Fatal("signed commit has no gpgsig header")
	}
}
