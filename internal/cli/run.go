package cli

import (
	"context"
	"io"
	"time"

	"github.com/ExplodeCode6324/chassiss/internal/protocol"
)

var (
	Version         = "0.1.0-dev"
	BuildDigest     = "development"
	ReleaseIdentity = "local"
)

type runtime struct {
	stdin  io.Reader
	stdout io.Writer
	stderr io.Writer
}

func Run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	parsed, err := parseInvocation(args)
	if err != nil {
		envelope, _ := errorEnvelope(commandHint(args), err)
		return emit(envelope, parsed.JSON || containsJSON(args), stdout, stderr)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	envelope, err := dispatch(ctx, runtime{stdin: stdin, stdout: stdout, stderr: stderr}, parsed)
	if err != nil {
		failure, _ := errorEnvelope(parsed.Definition.Path, err)
		return emit(failure, parsed.JSON, stdout, stderr)
	}
	return emit(envelope, parsed.JSON, stdout, stderr)
}

func dispatch(ctx context.Context, rt runtime, invocation invocation) (Envelope, error) {
	switch invocation.Definition.Path {
	case "version":
		envelope := baseEnvelope("version")
		envelope.Result = map[string]any{
			"build_digest": BuildDigest, "protocols": []string{protocol.ProtocolID},
			"release_identity": ReleaseIdentity, "version": Version,
		}
		return envelope, nil
	case "help":
		return helpCommand(invocation)
	case "init":
		return initCommand(ctx, invocation)
	case "clone":
		return cloneCommand(ctx, invocation)
	case "sync":
		return syncCommand(ctx, invocation)
	case "verify":
		return verifyCommand(ctx, invocation)
	case "status":
		return statusCommand(ctx, invocation)
	case "context", "log", "file show", "remote show", "remote set", "task list", "task show":
		return browseCommand(ctx, invocation)
	case "architecture validate":
		return architectureValidateCommand(ctx, invocation)
	case "taskbook validate":
		return taskbookValidateCommand(ctx, invocation)
	case "taskbook show", "taskbook draft", "taskbook diff", "taskbook open", "taskbook update", "taskbook archive",
		"architecture show", "architecture draft", "architecture diff", "architecture requires",
		"architecture required-by", "architecture impact", "architecture update":
		return contractCommand(ctx, invocation)
	case "key generate", "key list", "key show", "key remove":
		return keyCommand(ctx, invocation)
	case "task start", "task release", "task block", "task resume", "task cancel", "task supersede":
		return taskMutationCommand(ctx, invocation)
	case "grant request", "grant list", "grant show", "grant add", "grant revoke":
		return grantCommand(ctx, invocation)
	case "work open", "work status", "work diff", "work log", "work commit", "work restore", "work remove", "check", "submit":
		return workCommand(ctx, invocation)
	case "review":
		return reviewCommand(ctx, invocation)
	case "integrate":
		return integrateCommand(ctx, invocation)
	case "transition inspect", "transition publish":
		return transitionCommand(ctx, invocation)
	case "owner apply", "export", "cache clean":
		return utilityCommand(ctx, invocation)
	default:
		panic("unreachable command definition")
	}
}

func helpCommand(invocation invocation) (Envelope, error) {
	envelope := baseEnvelope("help")
	definitions := sortedDefinitions()
	if len(invocation.Positionals) == 1 {
		target := invocation.Positionals[0]
		filtered := make([]CommandDefinition, 0)
		for _, definition := range definitions {
			if definition.Path == target || len(target) > 0 && len(definition.Path) > len(target) &&
				definition.Path[:len(target)] == target && definition.Path[len(target)] == ' ' {
				filtered = append(filtered, definition)
			}
		}
		if len(filtered) == 0 {
			return Envelope{}, usageError("unknown help command path")
		}
		definitions = filtered
	}
	envelope.Result = map[string]any{
		"commands": definitions, "schema": "chassiss.command-schema/v1",
	}
	return envelope, nil
}

func commandHint(args []string) string {
	if len(args) == 0 {
		return "help"
	}
	if args[0] == "--json" && len(args) > 1 {
		return args[1]
	}
	return args[0]
}

func containsJSON(args []string) bool {
	for _, value := range args {
		if value == "--json" {
			return true
		}
	}
	return false
}
