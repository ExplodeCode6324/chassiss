package cryptoutil

import (
	"bytes"
	"fmt"
	"strings"

	"golang.org/x/crypto/ssh"
)

func ParseEd25519PublicKey(value string) (ssh.PublicKey, error) {
	if strings.TrimSpace(value) != value || strings.ContainsAny(value, "\r\n\t") {
		return nil, fmt.Errorf("public key must be a single canonical OpenSSH line")
	}
	key, comment, options, rest, err := ssh.ParseAuthorizedKey([]byte(value))
	if err != nil {
		return nil, fmt.Errorf("malformed OpenSSH public key: %w", err)
	}
	if len(rest) != 0 || comment != "" || len(options) != 0 {
		return nil, fmt.Errorf("public key must not contain options, comments, or trailing data")
	}
	if key.Type() != ssh.KeyAlgoED25519 {
		return nil, fmt.Errorf("v1 authority keys must use ssh-ed25519")
	}
	canonical := bytes.TrimSuffix(ssh.MarshalAuthorizedKey(key), []byte{'\n'})
	if !bytes.Equal(canonical, []byte(value)) {
		return nil, fmt.Errorf("public key is not in canonical OpenSSH form")
	}
	return key, nil
}

func Fingerprint(value string) (string, error) {
	key, err := ParseEd25519PublicKey(value)
	if err != nil {
		return "", err
	}
	return ssh.FingerprintSHA256(key), nil
}
