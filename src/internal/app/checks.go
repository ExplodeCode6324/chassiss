package app

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

func checkSpecDigest(spec CheckSpec) string {
	data, _ := canonicalJSON(spec)
	return digestBytes(data)
}

func validateCheckSpec(spec CheckSpec) error {
	if spec.ID == "" || len(spec.Argv) == 0 || strings.TrimSpace(spec.Argv[0]) == "" {
		return &CLIError{Code: "CHS-ARTIFACT-CHECK", Message: "acceptance checks require id and non-empty argv", ExitCode: 10}
	}
	if spec.Cwd == "" || filepath.IsAbs(spec.Cwd) {
		return &CLIError{Code: "CHS-ARTIFACT-CHECK", Message: "acceptance check cwd must be project-relative", ExitCode: 10}
	}
	if spec.TimeoutSeconds < 1 || spec.TimeoutSeconds > 86400 {
		return &CLIError{Code: "CHS-ARTIFACT-CHECK", Message: "acceptance check timeout_seconds must be between 1 and 86400", ExitCode: 10}
	}
	if spec.Env == nil {
		return &CLIError{Code: "CHS-ARTIFACT-CHECK", Message: "acceptance check env must be an explicit map", ExitCode: 10}
	}
	if spec.Shell && len(spec.Argv) != 1 {
		return &CLIError{Code: "CHS-ARTIFACT-CHECK", Message: "shell checks require exactly one argv script string", ExitCode: 10}
	}
	for _, argument := range spec.Argv {
		if strings.ContainsRune(argument, '\x00') {
			return &CLIError{Code: "CHS-ARTIFACT-CHECK", Message: "acceptance check argv contains a null byte", ExitCode: 10}
		}
	}
	for key := range spec.Env {
		if key == "" || strings.ContainsAny(key, "=\x00") {
			return &CLIError{Code: "CHS-ARTIFACT-CHECK", Message: "acceptance check contains an invalid environment name", ExitCode: 10}
		}
		if strings.ContainsRune(spec.Env[key], '\x00') {
			return &CLIError{Code: "CHS-ARTIFACT-CHECK", Message: "acceptance check environment contains a null byte", ExitCode: 10}
		}
	}
	return nil
}

func runCheckSpec(worktreeRoot string, spec CheckSpec) CheckResult {
	result := CheckResult{ID: spec.ID, SpecDigest: checkSpecDigest(spec), CheckedAt: timeNow()}
	if err := validateCheckSpec(spec); err != nil {
		result.ExitCode = 2
		result.Output = err.Error()
		return result
	}
	cwd, err := safeCheckCwd(worktreeRoot, spec.Cwd)
	if err != nil {
		result.ExitCode = 2
		result.Output = err.Error()
		return result
	}
	runtimeEnvironment, cleanup, err := checkEnvironment(worktreeRoot, spec.Env)
	if err != nil {
		result.ExitCode = 2
		result.Output = err.Error()
		return result
	}
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(spec.TimeoutSeconds)*time.Second)
	defer cancel()
	name, arguments := spec.Argv[0], spec.Argv[1:]
	if spec.Shell {
		name, arguments = "/bin/sh", []string{"-c", spec.Argv[0]}
	}
	command := exec.CommandContext(ctx, name, arguments...)
	command.Dir = cwd
	command.Env = runtimeEnvironment
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	runErr := command.Run()
	combined := strings.TrimSpace(strings.Join([]string{stdout.String(), stderr.String()}, "\n"))
	result.Output = trimOutput(combined)
	if ctx.Err() == context.DeadlineExceeded {
		result.ExitCode = 124
		result.Output = fmt.Sprintf("check timed out after %d seconds", spec.TimeoutSeconds)
		return result
	}
	if runErr != nil {
		var exitError *exec.ExitError
		if errors.As(runErr, &exitError) {
			result.ExitCode = exitError.ExitCode()
		} else {
			result.ExitCode = 1
			if result.Output == "" {
				result.Output = runErr.Error()
			}
		}
		return result
	}
	result.Passed = true
	return result
}

func safeCheckCwd(worktreeRoot, relative string) (string, error) {
	candidate, err := pathWithin(worktreeRoot, relative)
	if err != nil {
		return "", err
	}
	resolvedRoot, err := filepath.EvalSymlinks(worktreeRoot)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(candidate)
	if err != nil {
		return "", &CLIError{Code: "CHS-CHECK-CWD", Message: "acceptance check cwd does not exist", ExitCode: 10}
	}
	relativeResolved, err := filepath.Rel(resolvedRoot, resolved)
	if err != nil || relativeResolved == ".." || strings.HasPrefix(relativeResolved, ".."+string(os.PathSeparator)) {
		return "", &CLIError{Code: "CHS-CHECK-CWD", Message: "acceptance check cwd escapes the task worktree", ExitCode: 10}
	}
	info, err := os.Stat(resolved)
	if err != nil || !info.IsDir() {
		return "", &CLIError{Code: "CHS-CHECK-CWD", Message: "acceptance check cwd is not a directory", ExitCode: 10}
	}
	return resolved, nil
}

func checkEnvironment(worktreeRoot string, declared map[string]string) ([]string, func(), error) {
	commonDir, err := git(worktreeRoot, "rev-parse", "--absolute-git-dir")
	if err != nil {
		return nil, func() {}, err
	}
	commonDir, err = filepath.EvalSymlinks(commonDir)
	if err != nil {
		return nil, func() {}, err
	}
	for filepath.Base(commonDir) != ".git" {
		parent := filepath.Dir(commonDir)
		if parent == commonDir {
			break
		}
		commonDir = parent
	}
	cacheRoot := filepath.Join(commonDir, "chassiss-check-cache")
	for _, directory := range []string{"home", "tmp", "go-build", "go-mod"} {
		if err := os.MkdirAll(filepath.Join(cacheRoot, directory), 0o700); err != nil {
			return nil, func() {}, err
		}
	}
	values := map[string]string{
		"PATH":       os.Getenv("PATH"),
		"HOME":       filepath.Join(cacheRoot, "home"),
		"TMPDIR":     filepath.Join(cacheRoot, "tmp"),
		"GOCACHE":    filepath.Join(cacheRoot, "go-build"),
		"GOMODCACHE": filepath.Join(cacheRoot, "go-mod"),
		"LANG":       "C.UTF-8",
	}
	for key, value := range declared {
		values[key] = value
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	environment := make([]string, 0, len(keys))
	for _, key := range keys {
		environment = append(environment, key+"="+values[key])
	}
	return environment, func() {}, nil
}
