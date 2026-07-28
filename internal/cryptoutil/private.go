package cryptoutil

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/ExplodeCode6324/chassiss/internal/protocol"
	"golang.org/x/crypto/ssh"
)

const FileHandlePrefix = "file:"

type GeneratedKey struct {
	Handle      string
	PublicKey   string
	Fingerprint string
}

// GenerateFileKey creates a project-local-secret-store key file. The file is
// owner-only and created with O_EXCL. Only its opaque file: handle belongs in
// local state; private bytes must never enter a CLI response.
func GenerateFileKey(directory, keyID string) (GeneratedKey, error) {
	if err := protocol.ValidateID(protocol.IDKey, keyID); err != nil {
		return GeneratedKey{}, err
	}
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return GeneratedKey{}, err
	}
	if err := os.Chmod(directory, 0o700); err != nil {
		return GeneratedKey{}, err
	}
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return GeneratedKey{}, err
	}
	block, err := ssh.MarshalPrivateKey(private, "CHASSISS "+keyID)
	if err != nil {
		return GeneratedKey{}, err
	}
	path, err := filepath.Abs(filepath.Join(directory, keyID))
	if err != nil {
		return GeneratedKey{}, err
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return GeneratedKey{}, err
	}
	data := pem.EncodeToMemory(block)
	if _, err := file.Write(data); err != nil {
		file.Close()
		os.Remove(path)
		return GeneratedKey{}, err
	}
	if err := file.Sync(); err != nil {
		file.Close()
		os.Remove(path)
		return GeneratedKey{}, err
	}
	if err := file.Close(); err != nil {
		os.Remove(path)
		return GeneratedKey{}, err
	}
	if runtime.GOOS == "windows" {
		sshPublic, err := ssh.NewPublicKey(public)
		if err != nil {
			return GeneratedKey{}, err
		}
		publicLine := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(sshPublic)))
		return GeneratedKey{
			Handle: FileHandlePrefix + path, PublicKey: publicLine,
			Fingerprint: ssh.FingerprintSHA256(sshPublic),
		}, nil
	}
	parent, err := os.Open(directory)
	if err != nil {
		os.Remove(path)
		return GeneratedKey{}, err
	}
	if err := parent.Sync(); err != nil {
		parent.Close()
		os.Remove(path)
		return GeneratedKey{}, err
	}
	if err := parent.Close(); err != nil {
		os.Remove(path)
		return GeneratedKey{}, err
	}
	sshPublic, err := ssh.NewPublicKey(public)
	if err != nil {
		return GeneratedKey{}, err
	}
	publicLine := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(sshPublic)))
	return GeneratedKey{
		Handle: FileHandlePrefix + path, PublicKey: publicLine,
		Fingerprint: ssh.FingerprintSHA256(sshPublic),
	}, nil
}

func ResolveFileHandle(handle string) (string, error) {
	if !strings.HasPrefix(handle, FileHandlePrefix) {
		return "", fmt.Errorf("unsupported private-key handle kind")
	}
	path := strings.TrimPrefix(handle, FileHandlePrefix)
	if !filepath.IsAbs(path) {
		return "", fmt.Errorf("file key handle must contain an absolute path")
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("key handle does not reference a regular file")
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0 {
		return "", fmt.Errorf("private key file permissions are not owner-only")
	}
	return path, nil
}

func PublicFromHandle(handle string) (string, string, error) {
	path, err := ResolveFileHandle(handle)
	if err != nil {
		return "", "", err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", "", err
	}
	raw, err := ssh.ParseRawPrivateKey(data)
	if err != nil {
		return "", "", err
	}
	signer, err := ssh.NewSignerFromKey(raw)
	if err != nil {
		return "", "", err
	}
	if signer.PublicKey().Type() != ssh.KeyAlgoED25519 {
		return "", "", fmt.Errorf("v1 private key must be Ed25519")
	}
	public := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(signer.PublicKey())))
	return public, ssh.FingerprintSHA256(signer.PublicKey()), nil
}
