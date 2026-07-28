package gitstore

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type Runner struct {
	Repo      string
	GitBinary string
	Timeout   time.Duration
}

type Result struct {
	Stdout []byte
	Stderr []byte
}

func New(repo string) Runner {
	return Runner{Repo: repo, GitBinary: "git", Timeout: 60 * time.Second}
}

func (runner Runner) Run(ctx context.Context, args ...string) (Result, error) {
	return runner.runInput(ctx, nil, nil, args...)
}

func (runner Runner) RunWithInput(ctx context.Context, input []byte, extraEnv map[string]string, args ...string) (Result, error) {
	return runner.runInput(ctx, input, extraEnv, args...)
}

func (runner Runner) runInput(ctx context.Context, input []byte, extraEnv map[string]string, args ...string) (Result, error) {
	if runner.GitBinary == "" {
		runner.GitBinary = "git"
	}
	if runner.Timeout <= 0 {
		runner.Timeout = 60 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, runner.Timeout)
	defer cancel()
	hooks, err := os.MkdirTemp("", "chassiss-hooks-")
	if err != nil {
		return Result{}, err
	}
	defer os.RemoveAll(hooks)

	argv := []string{
		"-c", "core.hooksPath=" + hooks,
		"-c", "alias.chassiss-disabled=!false",
		"-c", "protocol.file.allow=never",
	}
	if runner.Repo != "" {
		argv = append(argv, "-C", runner.Repo)
	}
	argv = append(argv, args...)
	command := exec.CommandContext(ctx, runner.GitBinary, argv...)
	command.Env = isolatedEnvironment(extraEnv)
	command.Stdin = bytes.NewReader(input)
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		if ctx.Err() != nil {
			return Result{Stdout: stdout.Bytes(), Stderr: stderr.Bytes()}, fmt.Errorf("git command timed out: %w", ctx.Err())
		}
		return Result{Stdout: stdout.Bytes(), Stderr: stderr.Bytes()}, &CommandError{
			Args: args, Stderr: strings.TrimSpace(stderr.String()), Cause: err,
		}
	}
	return Result{Stdout: stdout.Bytes(), Stderr: stderr.Bytes()}, nil
}

type CommandError struct {
	Args   []string
	Stderr string
	Cause  error
}

func (err *CommandError) Error() string {
	if err.Stderr == "" {
		return fmt.Sprintf("git %s: %v", strings.Join(err.Args, " "), err.Cause)
	}
	return fmt.Sprintf("git %s: %s", strings.Join(err.Args, " "), err.Stderr)
}

func (err *CommandError) Unwrap() error { return err.Cause }

func isolatedEnvironment(extra map[string]string) []string {
	blockedPrefixes := []string{
		"GIT_DIR=", "GIT_WORK_TREE=", "GIT_INDEX_FILE=", "GIT_OBJECT_DIRECTORY=",
		"GIT_ALTERNATE_OBJECT_DIRECTORIES=", "GIT_CONFIG=", "GIT_CONFIG_COUNT=",
		"GIT_AUTHOR_", "GIT_COMMITTER_",
	}
	environment := make([]string, 0, len(os.Environ())+12)
	for _, entry := range os.Environ() {
		blocked := false
		for _, prefix := range blockedPrefixes {
			if strings.HasPrefix(entry, prefix) {
				blocked = true
				break
			}
		}
		if !blocked && !strings.HasPrefix(entry, "GIT_CONFIG_KEY_") &&
			!strings.HasPrefix(entry, "GIT_CONFIG_VALUE_") {
			environment = append(environment, entry)
		}
	}
	nullConfig := "/dev/null"
	if os.PathSeparator == '\\' {
		nullConfig = "NUL"
	}
	environment = append(environment,
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_CONFIG_GLOBAL="+nullConfig,
		"GIT_TERMINAL_PROMPT=0",
		"GCM_INTERACTIVE=Never",
		"LC_ALL=C",
	)
	for key, value := range extra {
		environment = append(environment, key+"="+value)
	}
	return environment
}

func repositoryGitDir(ctx context.Context, runner Runner) (string, error) {
	result, err := runner.Run(ctx, "rev-parse", "--absolute-git-dir")
	if err != nil {
		return "", err
	}
	path := strings.TrimSpace(string(result.Stdout))
	if !filepath.IsAbs(path) {
		return "", fmt.Errorf("Git returned a non-absolute git-dir")
	}
	return path, nil
}
