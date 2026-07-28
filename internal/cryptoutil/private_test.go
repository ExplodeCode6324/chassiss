package cryptoutil

import (
	"os"
	"runtime"
	"strings"
	"testing"
)

func TestGenerateFileKey(t *testing.T) {
	generated, err := GenerateFileKey(t.TempDir(), "KEY-TEST-01")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(generated.Handle, FileHandlePrefix) {
		t.Fatalf("unexpected handle %s", generated.Handle)
	}
	path, err := ResolveFileHandle(generated.Handle)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Fatalf("private key permissions are %o", info.Mode().Perm())
	}
	public, fingerprint, err := PublicFromHandle(generated.Handle)
	if err != nil {
		t.Fatal(err)
	}
	if public != generated.PublicKey || fingerprint != generated.Fingerprint {
		t.Fatal("generated public identity mismatch")
	}
}
