package cryptoutil

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

func SignSSHSIG(handle, namespace string, message []byte) (string, error) {
	keyPath, err := ResolveFileHandle(handle)
	if err != nil {
		return "", err
	}
	directory, err := os.MkdirTemp("", "chassiss-sshsig-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(directory)
	messagePath := filepath.Join(directory, "message")
	if err := os.WriteFile(messagePath, message, 0o600); err != nil {
		return "", err
	}
	command := exec.Command("ssh-keygen", "-Y", "sign", "-f", keyPath, "-n", namespace, messagePath)
	var stderr bytes.Buffer
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		return "", fmt.Errorf("ssh-keygen SSHSIG sign failed: %s: %w", stderr.String(), err)
	}
	signature, err := os.ReadFile(messagePath + ".sig")
	if err != nil {
		return "", err
	}
	return string(signature), nil
}

func VerifySSHSIG(publicKey, namespace, signature string, message []byte) error {
	if _, err := ParseEd25519PublicKey(publicKey); err != nil {
		return err
	}
	directory, err := os.MkdirTemp("", "chassiss-sshsig-verify-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(directory)
	allowedPath := filepath.Join(directory, "allowed-signers")
	signaturePath := filepath.Join(directory, "signature")
	if err := os.WriteFile(allowedPath, []byte("chassiss "+publicKey+"\n"), 0o600); err != nil {
		return err
	}
	if err := os.WriteFile(signaturePath, []byte(signature), 0o600); err != nil {
		return err
	}
	command := exec.Command("ssh-keygen", "-Y", "verify",
		"-f", allowedPath, "-I", "chassiss", "-n", namespace, "-s", signaturePath,
	)
	command.Stdin = bytes.NewReader(message)
	var stderr bytes.Buffer
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		return fmt.Errorf("SSHSIG verification failed: %s: %w", stderr.String(), err)
	}
	return nil
}
