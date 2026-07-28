package workflow

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/ExplodeCode6324/chassiss/internal/contracts"
	"github.com/ExplodeCode6324/chassiss/internal/protocol"
)

type CheckLog struct {
	CheckID string
	Stdout  []byte
	Stderr  []byte
}

const maxCheckLogBytes = 1 << 20

type cappedBuffer struct {
	bytes.Buffer
	limit int
}

func (buffer *cappedBuffer) Write(data []byte) (int, error) {
	original := len(data)
	remaining := buffer.limit - buffer.Len()
	if remaining > 0 {
		if len(data) > remaining {
			data = data[:remaining]
		}
		_, _ = buffer.Buffer.Write(data)
	}
	return original, nil
}

func RunChecks(ctx context.Context, repositoryRoot string, specs []contracts.CheckSpec, binding CheckBinding) ([]CheckResult, []CheckLog, error) {
	root, err := filepath.Abs(repositoryRoot)
	if err != nil {
		return nil, nil, err
	}
	results := make([]CheckResult, 0, len(specs))
	logs := make([]CheckLog, 0, len(specs))
	for _, spec := range specs {
		checkContext, err := BuildCheckContext(spec, binding)
		if err != nil {
			return nil, logs, err
		}
		contextDigest, err := protocol.ObjectDigest("check-execution-context", checkContext)
		if err != nil {
			return nil, logs, err
		}
		cwd := root
		if spec.Cwd != "." {
			cwd = filepath.Join(root, filepath.FromSlash(spec.Cwd))
		}
		resolvedRoot, err := filepath.EvalSymlinks(root)
		if err != nil {
			return nil, logs, err
		}
		resolvedCWD, err := filepath.EvalSymlinks(cwd)
		if err != nil {
			return nil, logs, err
		}
		relative, err := filepath.Rel(resolvedRoot, resolvedCWD)
		if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return nil, logs, fmt.Errorf("Check %s cwd escapes the repository", spec.ID)
		}

		checkCtx, cancel := context.WithTimeout(ctx, time.Duration(spec.TimeoutSeconds)*time.Second)
		command := exec.CommandContext(checkCtx, spec.Argv[0], spec.Argv[1:]...)
		command.Dir = resolvedCWD
		command.Env = os.Environ()
		stdout := cappedBuffer{limit: maxCheckLogBytes}
		stderr := cappedBuffer{limit: maxCheckLogBytes}
		command.Stdout = &stdout
		command.Stderr = &stderr
		runErr := command.Run()
		cancel()
		result := CheckResult{CheckID: spec.ID, ContextDigest: contextDigest}
		switch {
		case checkCtx.Err() == context.DeadlineExceeded:
			result.Status = "error"
		case runErr == nil:
			code := int64(0)
			result.Status = "pass"
			result.ExitCode = &code
		case isExitError(runErr):
			code := int64(runErr.(*exec.ExitError).ExitCode())
			result.Status = "fail"
			result.ExitCode = &code
		default:
			result.Status = "error"
		}
		results = append(results, result)
		logs = append(logs, CheckLog{CheckID: spec.ID, Stdout: stdout.Bytes(), Stderr: stderr.Bytes()})
	}
	if err := ValidateCheckResults(specs, results, binding, false); err != nil {
		return nil, logs, err
	}
	return results, logs, nil
}

func isExitError(err error) bool {
	_, ok := err.(*exec.ExitError)
	return ok
}
