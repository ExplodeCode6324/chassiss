package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"testing"

	"github.com/ExplodeCode6324/chassiss/internal/gitstore"
	"github.com/ExplodeCode6324/chassiss/internal/protocol"
)

func TestSymlinkTargetEscapesRepositoryPortable(t *testing.T) {
	tests := []struct {
		name    string
		target  string
		escapes bool
	}{
		{name: "plain relative", target: "file.txt"},
		{name: "POSIX relative", target: "dir/file.txt"},
		{name: "Windows relative", target: `dir\file.txt`},
		{name: "explicit relative", target: "./dir/file.txt"},
		{name: "colon not drive prefix", target: "name:value"},
		{name: "POSIX parent", target: "../secret", escapes: true},
		{name: "Windows parent", target: `..\secret`, escapes: true},
		{name: "POSIX nested parent", target: "dir/../secret", escapes: true},
		{name: "Windows nested parent", target: `dir\..\secret`, escapes: true},
		{name: "mixed parent one", target: `dir\../secret`, escapes: true},
		{name: "mixed parent two", target: `dir/..\secret`, escapes: true},
		{name: "POSIX absolute", target: "/secret", escapes: true},
		{name: "Windows rooted", target: `\secret`, escapes: true},
		{name: "Windows drive absolute backslash", target: `C:\secret`, escapes: true},
		{name: "Windows drive absolute slash", target: "C:/secret", escapes: true},
		{name: "Windows drive relative", target: `C:secret`, escapes: true},
		{name: "Windows lowercase drive", target: `c:\secret`, escapes: true},
		{name: "Windows UNC", target: `\\server\share\secret`, escapes: true},
		{name: "Windows extended device", target: `\\?\C:\secret`, escapes: true},
		{name: "Windows device", target: `\\.\device`, escapes: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := symlinkTargetEscapesRepository(test.target); got != test.escapes {
				t.Fatalf("symlinkTargetEscapesRepository(%q) = %v, want %v", test.target, got, test.escapes)
			}
		})
	}
}

func TestBootstrapRejectsPortableSymlinkEscapesBeforeInitializingTarget(t *testing.T) {
	tests := []struct {
		name   string
		target string
	}{
		{name: "Windows parent", target: `..\secret`},
		{name: "Windows nested parent", target: `dir\..\secret`},
		{name: "Windows drive", target: `C:\secret`},
		{name: "Windows UNC", target: `\\server\share\secret`},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			source := t.TempDir()
			runTestGit(t, source, "init", "-b", "main")
			blobCommand := exec.Command("git", "-C", source, "hash-object", "-w", "--stdin")
			blobCommand.Stdin = strings.NewReader(test.target)
			blobOutput, err := blobCommand.CombinedOutput()
			if err != nil {
				t.Fatalf("hash symlink target: %v\n%s", err, blobOutput)
			}
			blob := strings.TrimSpace(string(blobOutput))
			runTestGit(t, source, "update-index", "--add", "--cacheinfo", "120000,"+blob+",escape-link")
			runTestGit(t, source, "-c", "user.name=Legacy", "-c", "user.email=legacy@example.invalid", "commit", "-m", "portable symlink fixture")
			sourceCommit := strings.TrimSpace(runTestGit(t, source, "rev-parse", "HEAD"))

			target := t.TempDir()
			dataDirectory := filepath.Join(t.TempDir(), "local-data")
			t.Setenv("CHASSISS_DATA_DIR", dataDirectory)
			previous, err := os.Getwd()
			if err != nil {
				t.Fatal(err)
			}
			if err := os.Chdir(target); err != nil {
				t.Fatal(err)
			}
			defer os.Chdir(previous)

			var stdout, stderr bytes.Buffer
			exit := Run([]string{
				"bootstrap", "--project", "PRJ-SYMLINK-REJECT", "--source", source,
				"--ref", sourceCommit, "--root-key", "KEY-MISSING", "--json",
			}, bytes.NewReader(nil), &stdout, &stderr)
			if exit != 6 {
				t.Fatalf("bootstrap exit %d\nstdout %s\nstderr %s", exit, stdout.String(), stderr.String())
			}
			var envelope Envelope
			if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
				t.Fatal(err)
			}
			if envelope.Error == nil || envelope.Error.Code != protocol.ErrScopeViolation {
				t.Fatalf("unexpected symlink refusal: %#v", envelope.Error)
			}
			if envelope.Operation != nil {
				t.Fatalf("refused bootstrap produced an Operation: %#v", envelope.Operation)
			}
			if entries, err := os.ReadDir(target); err != nil || len(entries) != 0 {
				t.Fatalf("refused bootstrap initialized target: entries=%v err=%v", entries, err)
			}
			if _, err := os.Stat(dataDirectory); !os.IsNotExist(err) {
				t.Fatalf("refused bootstrap created local state: %v", err)
			}
		})
	}
}

func TestSourceTreeValidationAndImportRejectPortableSymlinkEscape(t *testing.T) {
	source := t.TempDir()
	target := t.TempDir()
	runTestGit(t, source, "init", "-b", "main")
	runTestGit(t, target, "init", "-b", "main")
	blobCommand := exec.Command("git", "-C", source, "hash-object", "-w", "--stdin")
	blobCommand.Stdin = strings.NewReader(`..\secret`)
	blobOutput, err := blobCommand.CombinedOutput()
	if err != nil {
		t.Fatalf("hash symlink target: %v\n%s", err, blobOutput)
	}
	tree := gitstore.TreeMap{
		"escape-link": {Mode: "120000", OID: strings.TrimSpace(string(blobOutput))},
	}
	sourceRunner := gitstore.New(source)
	targetRunner := gitstore.New(target)
	if err := validateSourceTree(context.Background(), sourceRunner, tree); err == nil {
		t.Fatal("source pre-validation accepted a cross-platform escaping symlink")
	}
	if _, err := importSourceTree(context.Background(), sourceRunner, targetRunner, tree); err == nil {
		t.Fatal("source import accepted a cross-platform escaping symlink")
	}
}

func TestInitAndManagedWorkRejectPortableSymlinkEscape(t *testing.T) {
	repository := t.TempDir()
	runTestGit(t, repository, "init", "-b", "main")
	link := filepath.Join(repository, "escape-link")
	if err := os.Symlink(`..\secret`, link); err != nil {
		if goruntime.GOOS == "windows" {
			t.Skipf("symlink creation is unavailable: %v", err)
		}
		t.Fatal(err)
	}

	if _, err := scanInitialTree(context.Background(), gitstore.New(repository), repository); err == nil {
		t.Fatal("initial tree accepted a cross-platform escaping symlink")
	}
	if err := validateWorkSymlink(repository, "escape-link"); err == nil {
		t.Fatal("managed work accepted a cross-platform escaping symlink")
	}
}

func TestInitAndManagedWorkAllowPortableRelativeSymlink(t *testing.T) {
	repository := t.TempDir()
	runTestGit(t, repository, "init", "-b", "main")
	link := filepath.Join(repository, "internal-link")
	if err := os.Symlink("dir/file.txt", link); err != nil {
		if goruntime.GOOS == "windows" {
			t.Skipf("symlink creation is unavailable: %v", err)
		}
		t.Fatal(err)
	}

	mapping, err := scanInitialTree(context.Background(), gitstore.New(repository), repository)
	if err != nil {
		t.Fatalf("initial tree refused an internal relative symlink: %v", err)
	}
	if entry := mapping["internal-link"]; entry.Mode != "120000" {
		t.Fatalf("initial tree did not preserve symlink mode: %#v", entry)
	}
	if err := validateWorkSymlink(repository, "internal-link"); err != nil {
		t.Fatalf("managed work refused an internal relative symlink: %v", err)
	}
}
