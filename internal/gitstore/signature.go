package gitstore

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/ExplodeCode6324/chassiss/internal/cryptoutil"
)

type CommitIdentity struct {
	Name  string
	Email string
}

func (runner Runner) CommitTree(ctx context.Context, tree string, parents []string, message, signingKey string, identity CommitIdentity) (string, error) {
	if signingKey == "" {
		return "", fmt.Errorf("signing key handle is required")
	}
	if identity.Name == "" {
		identity.Name = "CHASSISS"
	}
	if identity.Email == "" {
		identity.Email = "chassiss@local.invalid"
	}
	args := []string{
		"-c", "gpg.format=ssh",
		"-c", "user.signingkey=" + signingKey,
		"commit-tree", tree,
	}
	for _, parent := range parents {
		args = append(args, "-p", parent)
	}
	args = append(args, "-S", "-F", "-")
	extra := map[string]string{
		"GIT_AUTHOR_NAME":     identity.Name,
		"GIT_AUTHOR_EMAIL":    identity.Email,
		"GIT_COMMITTER_NAME":  identity.Name,
		"GIT_COMMITTER_EMAIL": identity.Email,
	}
	result, err := runner.RunWithInput(ctx, []byte(message), extra, args...)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(result.Stdout)), nil
}

func (runner Runner) VerifyCommitSSH(ctx context.Context, commitOID, publicKey string) error {
	if _, err := cryptoutil.ParseEd25519PublicKey(publicKey); err != nil {
		return err
	}
	file, err := createOwnerOnlyTemp("", "chassiss-signers-")
	if err != nil {
		return err
	}
	path := file.Name()
	defer os.Remove(path)
	content := "chassiss " + publicKey + "\n"
	if _, err := file.WriteString(content); err != nil {
		file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	result, err := runner.Run(ctx,
		"-c", "gpg.format=ssh",
		"-c", "gpg.ssh.allowedSignersFile="+path,
		"verify-commit", "--raw", commitOID,
	)
	if err != nil {
		return err
	}
	output := string(result.Stdout) + string(result.Stderr)
	if !strings.Contains(output, "Good \"git\" signature for chassiss") {
		return fmt.Errorf("Git did not report a valid CHASSISS SSH signature")
	}
	return nil
}

func (runner Runner) UpdateRefCAS(ctx context.Context, ref, newOID, expectedOldOID, message string) error {
	if message == "" {
		message = "chassiss update"
	}
	_, err := runner.Run(ctx, "update-ref", "-m", message, ref, newOID, expectedOldOID)
	return err
}

type RefUpdate struct {
	Ref         string
	NewOID      string
	ExpectedOID string
	CreateOnly  bool
}

func (runner Runner) UpdateRefsAtomic(ctx context.Context, updates []RefUpdate, message string) error {
	var input strings.Builder
	input.WriteString("start\n")
	for _, update := range updates {
		if update.CreateOnly {
			fmt.Fprintf(&input, "create %s %s\n", update.Ref, update.NewOID)
		} else {
			fmt.Fprintf(&input, "update %s %s %s\n", update.Ref, update.NewOID, update.ExpectedOID)
		}
	}
	input.WriteString("prepare\ncommit\n")
	_, err := runner.RunWithInput(ctx, []byte(input.String()), nil, "update-ref", "--stdin", "-m", message)
	return err
}

func (runner Runner) IsAncestor(ctx context.Context, ancestor, descendant string) (bool, error) {
	_, err := runner.Run(ctx, "merge-base", "--is-ancestor", ancestor, descendant)
	if err == nil {
		return true, nil
	}
	if commandError, ok := err.(*CommandError); ok {
		if exitError, ok := commandError.Cause.(*os.PathError); ok {
			_ = exitError
		}
		if code := commandExitCode(commandError); code == 1 {
			return false, nil
		}
	}
	return false, err
}

func commandExitCode(err *CommandError) int {
	type exitCoder interface{ ExitCode() int }
	if value, ok := err.Cause.(exitCoder); ok {
		return value.ExitCode()
	}
	return -1
}

func ParentCountAllowed(action string, parents int) bool {
	switch action {
	case "project.genesis", "project.bootstrap":
		return parents == 0
	case "integration.applied":
		return parents == 2
	default:
		return parents == 1
	}
}

func attemptString(value int) string { return strconv.Itoa(value) }
