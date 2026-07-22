package app

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

const Version = "0.1.0-dev"

type globalOptions struct {
	root          string
	credential    string
	json          bool
	dryRun        bool
	expected      int64
	trustExpected int64
}

type commandArgs struct {
	positionals []string
	values      map[string]string
	flags       map[string]bool
}

var booleanCommandFlags = map[string]bool{
	"existing": true, "pending": true, "ready": true, "active": true,
	"blocked": true, "review": true, "all": true, "persistent": true,
}

// Run executes the CHASSISS command line and returns a process exit code.
func Run(args []string, stdout, stderr io.Writer) int {
	options, rest, err := parseGlobals(args)
	if err != nil {
		return emitFailure(stderr, options.json, "parse", err)
	}
	if len(rest) == 0 || rest[0] == "help" || rest[0] == "--help" || rest[0] == "-h" {
		fmt.Fprint(stdout, helpText)
		return 0
	}
	if rest[0] == "version" || rest[0] == "--version" {
		if options.json {
			_ = json.NewEncoder(stdout).Encode(Response{APIVersion: APIVersion, OK: true, Command: "version", Result: map[string]string{"version": Version}})
		} else {
			fmt.Fprintln(stdout, "chassiss "+Version)
		}
		return 0
	}
	command := commandName(rest)
	result, err := dispatch(options, rest)
	if err != nil {
		return emitFailure(stderr, options.json, command, err)
	}
	return emitSuccess(stdout, options.json, result)
}

func parseGlobals(args []string) (globalOptions, []string, error) {
	options := globalOptions{expected: -1, trustExpected: -1}
	for len(args) > 0 {
		switch args[0] {
		case "--root":
			if len(args) < 2 {
				return options, nil, usageError("--root requires a path")
			}
			options.root, args = args[1], args[2:]
		case "--credential":
			if len(args) < 2 {
				return options, nil, usageError("--credential requires a path")
			}
			options.credential, args = args[1], args[2:]
		case "--json":
			options.json, args = true, args[1:]
		case "--dry-run":
			options.dryRun, args = true, args[1:]
		case "--expect-revision":
			if len(args) < 2 {
				return options, nil, usageError("--expect-revision requires an integer")
			}
			value, err := strconv.ParseInt(args[1], 10, 64)
			if err != nil || value < 1 {
				return options, nil, usageError("--expect-revision must be a positive integer")
			}
			options.expected, args = value, args[2:]
		case "--expect-trust-revision":
			if len(args) < 2 {
				return options, nil, usageError("--expect-trust-revision requires an integer")
			}
			value, err := strconv.ParseInt(args[1], 10, 64)
			if err != nil || value < 1 {
				return options, nil, usageError("--expect-trust-revision must be a positive integer")
			}
			options.trustExpected, args = value, args[2:]
		default:
			return options, args, nil
		}
	}
	return options, args, nil
}

func parseCommandArgs(args []string) (commandArgs, error) {
	parsed := commandArgs{values: map[string]string{}, flags: map[string]bool{}}
	for len(args) > 0 {
		argument := args[0]
		if !strings.HasPrefix(argument, "--") {
			parsed.positionals = append(parsed.positionals, argument)
			args = args[1:]
			continue
		}
		name := strings.TrimPrefix(argument, "--")
		if booleanCommandFlags[name] {
			parsed.flags[name] = true
			args = args[1:]
			continue
		}
		if len(args) < 2 || strings.HasPrefix(args[1], "--") {
			return parsed, usageError("--" + name + " requires a value")
		}
		if _, exists := parsed.values[name]; exists {
			return parsed, usageError("duplicate option --" + name)
		}
		parsed.values[name], args = args[1], args[2:]
	}
	return parsed, nil
}

func validateCommandOptions(command string, parsed commandArgs) error {
	type allowedOptions struct {
		values []string
		flags  []string
	}
	allowed := map[string]allowedOptions{
		"auth master-init":          {values: []string{"output"}},
		"auth issue":                {values: []string{"master-root", "actor", "role", "output", "actions", "not-before", "expires-at", "ttl-seconds", "projects", "missions", "tasks", "submissions", "submission-digests", "heads", "baselines"}, flags: []string{"persistent"}},
		"auth revoke":               {values: []string{"master-root", "id", "reason"}},
		"project init":              {values: []string{"master-root"}, flags: []string{"existing"}},
		"next":                      {values: []string{"role", "actor"}},
		"template get":              {values: []string{"id", "output"}},
		"artifact list":             {flags: []string{"pending"}},
		"artifact reject":           {values: []string{"reason"}},
		"mission block":             {values: []string{"reason"}},
		"mission submit-acceptance": {values: []string{"evidence"}},
		"task list":                 {flags: []string{"ready", "active", "blocked", "review"}},
		"task assign":               {values: []string{"owner"}},
		"task block":                {values: []string{"reason"}},
		"task cancel":               {values: []string{"reason"}},
		"task supersede":            {values: []string{"replacement"}},
		"work check":                {values: []string{"id"}, flags: []string{"all"}},
		"work checkpoint":           {values: []string{"file"}},
		"work submit":               {values: []string{"file"}},
		"work block":                {values: []string{"reason"}},
		"review approve":            {values: []string{"report"}},
		"review request-changes":    {values: []string{"report"}},
		"publish check":             {values: []string{"target", "remote", "branch"}},
		"publish apply":             {values: []string{"target", "remote", "branch"}},
	}
	rules := allowed[command]
	valueSet, flagSet := stringSet(rules.values), stringSet(rules.flags)
	for name := range parsed.values {
		if _, ok := valueSet[name]; !ok {
			return usageError("unknown option for " + command + ": --" + name)
		}
	}
	for name := range parsed.flags {
		if _, ok := flagSet[name]; !ok {
			return usageError("unknown flag for " + command + ": --" + name)
		}
	}
	return nil
}

func dispatch(options globalOptions, words []string) (Response, error) {
	command := commandName(words)
	if options.dryRun && isWriteCommand(command) {
		return Response{}, &CLIError{Code: "CHS-DRY-RUN-UNSUPPORTED", Message: "this preview does not yet implement transactional dry-run", ExitCode: 20}
	}
	parsed, err := parseCommandArgs(words[commandWordCount(words):])
	if err != nil {
		return Response{}, err
	}
	if err := validateCommandOptions(command, parsed); err != nil {
		return Response{}, err
	}

	if command == "auth master-init" {
		output := parsed.values["output"]
		if output == "" {
			return Response{}, usageError("auth master-init requires --output")
		}
		if info, statErr := os.Stat(output); statErr == nil && info.IsDir() {
			output = filepath.Join(output, "master-root.yaml")
		}
		root, err := createRoot(output)
		if err != nil {
			return Response{}, err
		}
		return response(command, "", 0, 0, map[string]any{"id": root.ID, "path": output, "fingerprint": keyFingerprint(mustDecodePublic(root.PublicKey)), "created_at": root.CreatedAt}), nil
	}
	if command == "project init" {
		if len(parsed.positionals) != 1 {
			return Response{}, usageError("project init requires exactly one project path")
		}
		rootKey := firstNonEmpty(parsed.values["master-root"], options.credential)
		if rootKey == "" {
			return Response{}, usageError("project init requires --master-root or global --credential")
		}
		config, state, err := initializeProject(parsed.positionals[0], rootKey, parsed.flags["existing"])
		if err != nil {
			return Response{}, err
		}
		return response(command, config.ProjectID, 0, state.Revision, map[string]any{"root": absolutePath(parsed.positionals[0]), "mode": config.Mode, "default_branch": config.DefaultBranch}), nil
	}
	if command == "auth inspect" {
		path := options.credential
		if len(parsed.positionals) == 1 {
			path = parsed.positionals[0]
		}
		if path == "" {
			return Response{}, usageError("auth inspect requires a credential path")
		}
		value, err := inspectCredential(path)
		if err != nil {
			return Response{}, err
		}
		return response(command, "", 0, 0, value), nil
	}

	root, err := resolveRoot(options.root)
	if err != nil {
		return Response{}, err
	}
	if command == "recover" {
		configPath, _, statePath, _ := projectPaths(root)
		var config Config
		if err := loadYAML(configPath, &config); err != nil {
			return Response{}, err
		}
		var projected State
		_ = loadYAML(statePath, &projected)
		recovered, err := recoverProject(root)
		if err != nil {
			return Response{}, err
		}
		return response(command, config.ProjectID, projected.Revision, recovered.Revision, map[string]any{"recovered": true, "revision": recovered.Revision}), nil
	}
	config, trust, state, err := loadProject(root)
	if err != nil {
		return Response{}, err
	}
	state, err = verifyProject(root)
	if err != nil {
		return Response{}, err
	}
	readResponse := func(value any) Response {
		return response(command, config.ProjectID, state.Revision, state.Revision, value)
	}
	mutatingResponse := func(previous, next State, value any, principal Principal) Response {
		item := response(command, config.ProjectID, previous.Revision, next.Revision, value)
		item.Next = nextActions(next, principal.Role, principal.Actor)
		return item
	}
	principalFor := func(action string) (Principal, error) {
		return loadPrincipal(root, options.credential, action)
	}

	switch command {
	case "auth issue":
		rootKey := firstNonEmpty(parsed.values["master-root"], options.credential)
		actor, role, output := parsed.values["actor"], parsed.values["role"], parsed.values["output"]
		if rootKey == "" || actor == "" || role == "" || output == "" {
			return Response{}, usageError("auth issue requires --actor, --role, --output, and a Master Root credential")
		}
		policy, err := credentialPolicyFromArgs(parsed)
		if err != nil {
			return Response{}, err
		}
		credential, err := issueCredentialWithPolicy(root, rootKey, actor, role, output, commaList(parsed.values["actions"]), options.trustExpected, policy)
		if err != nil {
			return Response{}, err
		}
		_, updatedTrust, _, err := loadProject(root)
		if err != nil {
			return Response{}, err
		}
		return readResponse(map[string]any{"id": credential.ID, "actor": credential.Actor, "role": credential.Role, "actions": credential.Actions, "path": absolutePath(output), "persistent": credential.ExpiresAt == nil, "not_before": credential.NotBefore, "expires_at": credential.ExpiresAt, "resources": credential.Resources, "trust_revision": updatedTrust.Revision}), nil
	case "auth revoke":
		rootKey := firstNonEmpty(parsed.values["master-root"], options.credential)
		credentialID := parsed.values["id"]
		if len(parsed.positionals) == 1 {
			credentialID = parsed.positionals[0]
		}
		if rootKey == "" || credentialID == "" {
			return Response{}, usageError("auth revoke requires a credential ID and Master Root credential")
		}
		if err := revokeCredentialExpected(root, rootKey, credentialID, parsed.values["reason"], options.trustExpected); err != nil {
			return Response{}, err
		}
		_, trust, current, err := loadProject(root)
		if err != nil {
			return Response{}, err
		}
		state, _ = current, trust
		return readResponse(map[string]any{"credential_id": credentialID, "revoked": true, "trust_version": trust.Version, "trust_revision": trust.Revision}), nil

	case "status":
		result := stateSummary(state)
		result["root"] = root
		result["mode"] = config.Mode
		result["trust_revision"] = trust.Revision
		return readResponse(result), nil
	case "next":
		role := parsed.values["role"]
		if role == "" {
			return Response{}, usageError("next requires --role")
		}
		actions := nextActions(state, role, parsed.values["actor"])
		item := readResponse(map[string]any{"role": role, "actions": actions})
		item.Next = actions
		return item, nil
	case "doctor", "verify":
		verified, err := verifyProject(root)
		if err != nil {
			return Response{}, err
		}
		clean, detail, gitErr := gitClean(root)
		if gitErr != nil {
			return Response{}, gitErr
		}
		result := map[string]any{"integrity": "valid", "event_revision": verified.Revision, "git_clean": clean, "git_status": detail, "root_fingerprint": config.RootFingerprint}
		if options.credential != "" {
			anchor, err := loadPrincipal(root, options.credential, "")
			if err != nil {
				return Response{}, err
			}
			result["credential_anchor"] = map[string]any{"valid": true, "id": anchor.ID, "actor": anchor.Actor, "role": anchor.Role}
		}
		return readResponse(result), nil
	case "explain":
		if len(parsed.positionals) != 1 {
			return Response{}, usageError("explain requires an error code")
		}
		return readResponse(explainCode(parsed.positionals[0])), nil

	case "template list":
		return readResponse(map[string]any{"kinds": templateKinds()}), nil
	case "template get":
		if len(parsed.positionals) != 1 {
			return Response{}, usageError("template get requires a template kind")
		}
		data, canonical, err := renderTemplate(root, parsed.positionals[0], parsed.values["id"])
		if err != nil {
			return Response{}, err
		}
		if output := parsed.values["output"]; output != "" {
			absolute, err := pathWithin(root, output)
			if err != nil {
				return Response{}, err
			}
			if _, err := os.Stat(absolute); err == nil {
				return Response{}, &CLIError{Code: "CHS-TEMPLATE-EXISTS", Message: "refusing to overwrite existing file: " + output, ExitCode: 10}
			} else if !os.IsNotExist(err) {
				return Response{}, err
			}
			if err := writeAtomic(absolute, data, 0o644); err != nil {
				return Response{}, err
			}
			return readResponse(map[string]any{"kind": parsed.positionals[0], "path": filepath.ToSlash(output), "canonical_path": canonical}), nil
		}
		return readResponse(map[string]any{"kind": parsed.positionals[0], "canonical_path": canonical, "content": string(data)}), nil
	case "artifact check":
		path, err := exactlyOne(parsed.positionals, "artifact check requires a path")
		if err != nil {
			return Response{}, err
		}
		document, err := parseArtifact(root, path)
		if err == nil {
			err = validateArtifactAgainstState(document, state)
		}
		if err != nil {
			return Response{}, err
		}
		return readResponse(map[string]any{"valid": true, "kind": document.Metadata.Kind, "id": document.Metadata.ID, "path": document.Path, "digest": document.Digest}), nil
	case "artifact submit":
		path, err := exactlyOne(parsed.positionals, "artifact submit requires a path")
		if err != nil {
			return Response{}, err
		}
		principal, err := principalFor("artifact.submit")
		if err != nil {
			return Response{}, err
		}
		previous, next, artifact, err := submitArtifact(root, path, principal, options.expected)
		if err != nil {
			return Response{}, err
		}
		return mutatingResponse(previous, next, artifact, principal), nil
	case "artifact accept":
		id, err := exactlyOne(parsed.positionals, "artifact accept requires a submission ID")
		if err != nil {
			return Response{}, err
		}
		principal, err := principalFor("artifact.accept")
		if err != nil {
			return Response{}, err
		}
		previous, next, artifact, err := acceptArtifact(root, id, principal, options.expected)
		if err != nil {
			return Response{}, err
		}
		return mutatingResponse(previous, next, artifact, principal), nil
	case "artifact reject":
		id, err := exactlyOne(parsed.positionals, "artifact reject requires a submission ID")
		if err != nil {
			return Response{}, err
		}
		principal, err := principalFor("artifact.reject")
		if err != nil {
			return Response{}, err
		}
		previous, next, artifact, err := rejectArtifact(root, id, parsed.values["reason"], principal, options.expected)
		if err != nil {
			return Response{}, err
		}
		return mutatingResponse(previous, next, artifact, principal), nil
	case "artifact list":
		artifacts := make([]ArtifactState, 0, len(state.Artifacts))
		for _, id := range sortedArtifactIDs(state.Artifacts) {
			artifact := state.Artifacts[id]
			if !parsed.flags["pending"] || artifact.Status == "submitted" {
				artifacts = append(artifacts, artifact)
			}
		}
		return readResponse(map[string]any{"artifacts": artifacts}), nil
	case "artifact context":
		id, err := exactlyOne(parsed.positionals, "artifact context requires a submission ID")
		if err != nil {
			return Response{}, err
		}
		artifact, found := artifactBySubmission(state, id)
		if !found {
			return Response{}, notFound("artifact submission")
		}
		content, err := readTextFile(root, artifact.Path)
		if err != nil {
			return Response{}, err
		}
		return readResponse(map[string]any{"artifact": artifact, "content": content}), nil

	case "mission list":
		return readResponse(map[string]any{"missions": sortedMissions(state)}), nil
	case "mission context":
		id, err := exactlyOne(parsed.positionals, "mission context requires a mission ID")
		if err != nil {
			return Response{}, err
		}
		mission, ok := state.Missions[id]
		if !ok {
			return Response{}, notFound("mission")
		}
		return readResponse(map[string]any{"mission": mission, "tasks": tasksForMission(state, id)}), nil
	case "mission activate":
		id, err := exactlyOne(parsed.positionals, "mission activate requires a mission ID")
		if err != nil {
			return Response{}, err
		}
		principal, err := principalFor("mission.activate")
		if err != nil {
			return Response{}, err
		}
		previous, next, err := activateMission(root, id, principal, options.expected)
		if err != nil {
			return Response{}, err
		}
		return mutatingResponse(previous, next, next.Missions[id], principal), nil
	case "mission block":
		id, err := exactlyOne(parsed.positionals, "mission block requires a mission ID")
		if err != nil {
			return Response{}, err
		}
		principal, err := principalFor("mission.block")
		if err != nil {
			return Response{}, err
		}
		previous, next, mission, err := missionBlock(root, id, parsed.values["reason"], principal, options.expected)
		if err != nil {
			return Response{}, err
		}
		return mutatingResponse(previous, next, mission, principal), nil
	case "mission resume":
		id, err := exactlyOne(parsed.positionals, "mission resume requires a mission ID")
		if err != nil {
			return Response{}, err
		}
		principal, err := principalFor("mission.resume")
		if err != nil {
			return Response{}, err
		}
		previous, next, mission, err := missionResume(root, id, principal, options.expected)
		if err != nil {
			return Response{}, err
		}
		return mutatingResponse(previous, next, mission, principal), nil
	case "mission submit-acceptance":
		id, err := exactlyOne(parsed.positionals, "mission submit-acceptance requires a mission ID")
		if err != nil {
			return Response{}, err
		}
		principal, err := principalFor("mission.submit-acceptance")
		if err != nil {
			return Response{}, err
		}
		evidence, err := optionText(root, parsed, "evidence")
		if err != nil || evidence == "" {
			if err == nil {
				err = usageError("mission submit-acceptance requires --evidence")
			}
			return Response{}, err
		}
		previous, next, mission, err := submitMissionAcceptance(root, id, evidence, principal, options.expected)
		if err != nil {
			return Response{}, err
		}
		return mutatingResponse(previous, next, mission, principal), nil
	case "mission accept":
		id, err := exactlyOne(parsed.positionals, "mission accept requires a mission ID")
		if err != nil {
			return Response{}, err
		}
		principal, err := principalFor("mission.accept")
		if err != nil {
			return Response{}, err
		}
		previous, next, mission, err := acceptMission(root, id, principal, options.expected)
		if err != nil {
			return Response{}, err
		}
		return mutatingResponse(previous, next, mission, principal), nil

	case "task list":
		statusFilter := taskStatusFilter(parsed)
		tasks := []TaskState{}
		for _, id := range sortedTaskIDs(state.Tasks) {
			task := state.Tasks[id]
			if statusFilter == "" || statusMatchesFilter(task.Status, statusFilter) {
				tasks = append(tasks, task)
			}
		}
		return readResponse(map[string]any{"tasks": tasks}), nil
	case "task context", "work context", "work status":
		id, err := exactlyOne(parsed.positionals, command+" requires a task ID")
		if err != nil {
			return Response{}, err
		}
		task, ok := state.Tasks[id]
		if !ok {
			return Response{}, notFound("task")
		}
		result := map[string]any{"task": task, "mission": state.Missions[task.MissionID]}
		if artifact, ok := state.Artifacts[id]; ok {
			content, _ := readTextFile(root, artifact.Path)
			result["contract"] = content
		}
		return readResponse(result), nil
	case "task claim", "task assign":
		id, err := exactlyOne(parsed.positionals, command+" requires a task ID")
		if err != nil {
			return Response{}, err
		}
		action := strings.ReplaceAll(command, " ", ".")
		principal, err := principalFor(action)
		if err != nil {
			return Response{}, err
		}
		owner := parsed.values["owner"]
		if command == "task assign" && owner == "" {
			return Response{}, usageError("task assign requires --owner")
		}
		previous, next, task, err := taskClaimOrAssign(root, id, owner, principal, options.expected, command == "task assign")
		if err != nil {
			return Response{}, err
		}
		return mutatingResponse(previous, next, task, principal), nil
	case "task block", "work block":
		id, err := exactlyOne(parsed.positionals, command+" requires a task ID")
		if err != nil {
			return Response{}, err
		}
		action := strings.ReplaceAll(command, " ", ".")
		principal, err := principalFor(action)
		if err != nil {
			return Response{}, err
		}
		reason := parsed.values["reason"]
		if reason == "" {
			return Response{}, usageError(command + " requires --reason")
		}
		previous, next, task, err := taskBlock(root, id, reason, principal, options.expected, action+"ed")
		if err != nil {
			return Response{}, err
		}
		return mutatingResponse(previous, next, task, principal), nil
	case "task resume":
		id, err := exactlyOne(parsed.positionals, "task resume requires a task ID")
		if err != nil {
			return Response{}, err
		}
		principal, err := principalFor("task.resume")
		if err != nil {
			return Response{}, err
		}
		previous, next, task, err := taskResume(root, id, principal, options.expected)
		if err != nil {
			return Response{}, err
		}
		return mutatingResponse(previous, next, task, principal), nil
	case "task release":
		id, err := exactlyOne(parsed.positionals, "task release requires a task ID")
		if err != nil {
			return Response{}, err
		}
		principal, err := principalFor("task.release")
		if err != nil {
			return Response{}, err
		}
		previous, next, task, err := taskRelease(root, id, principal, options.expected)
		if err != nil {
			return Response{}, err
		}
		return mutatingResponse(previous, next, task, principal), nil
	case "task cancel":
		id, err := exactlyOne(parsed.positionals, "task cancel requires a task ID")
		if err != nil {
			return Response{}, err
		}
		reason := parsed.values["reason"]
		if reason == "" {
			return Response{}, usageError("task cancel requires --reason")
		}
		principal, err := principalFor("task.cancel")
		if err != nil {
			return Response{}, err
		}
		previous, next, task, err := taskCancel(root, id, reason, principal, options.expected)
		if err != nil {
			return Response{}, err
		}
		return mutatingResponse(previous, next, task, principal), nil
	case "task supersede":
		id, err := exactlyOne(parsed.positionals, "task supersede requires a task ID")
		if err != nil {
			return Response{}, err
		}
		replacement := parsed.values["replacement"]
		if replacement == "" {
			return Response{}, usageError("task supersede requires --replacement")
		}
		principal, err := principalFor("task.supersede")
		if err != nil {
			return Response{}, err
		}
		previous, next, task, err := taskSupersede(root, id, replacement, principal, options.expected)
		if err != nil {
			return Response{}, err
		}
		return mutatingResponse(previous, next, task, principal), nil

	case "work open":
		id, err := exactlyOne(parsed.positionals, "work open requires a task ID")
		if err != nil {
			return Response{}, err
		}
		principal, err := principalFor("work.open")
		if err != nil {
			return Response{}, err
		}
		previous, next, task, err := workOpen(root, id, principal, options.expected)
		if err != nil {
			return Response{}, err
		}
		return mutatingResponse(previous, next, task, principal), nil
	case "work diff":
		id, err := exactlyOne(parsed.positionals, "work diff requires a task ID")
		if err != nil {
			return Response{}, err
		}
		task, ok := state.Tasks[id]
		if !ok || task.Baseline == "" {
			return Response{}, notFound("opened task")
		}
		worktreeRoot, err := taskWorktreeRoot(root, task)
		if err != nil {
			return Response{}, err
		}
		files, err := gitWorkingFiles(worktreeRoot)
		if err != nil {
			return Response{}, err
		}
		diff, err := gitWorkingDiff(worktreeRoot)
		if err != nil {
			return Response{}, err
		}
		return readResponse(map[string]any{"task_id": id, "baseline": task.Baseline, "files": files, "diff": diff}), nil
	case "work check":
		id, err := exactlyOne(parsed.positionals, "work check requires a task ID")
		if err != nil {
			return Response{}, err
		}
		principal, err := principalFor("work.check")
		if err != nil {
			return Response{}, err
		}
		all := parsed.flags["all"]
		checkID := parsed.values["id"]
		if !all && checkID == "" {
			return Response{}, usageError("work check requires --all or --id")
		}
		previous, next, checks, err := runTaskCheck(root, id, checkID, all, principal, options.expected)
		if err != nil {
			return Response{}, err
		}
		return mutatingResponse(previous, next, map[string]any{"task_id": id, "checks": checks}, principal), nil
	case "work checkpoint", "work submit":
		id, err := exactlyOne(parsed.positionals, command+" requires a task ID")
		if err != nil {
			return Response{}, err
		}
		action := strings.ReplaceAll(command, " ", ".")
		principal, err := principalFor(action)
		if err != nil {
			return Response{}, err
		}
		text, err := optionText(root, parsed, "file")
		if err != nil || text == "" {
			if err == nil {
				err = usageError(command + " requires --file")
			}
			return Response{}, err
		}
		if command == "work checkpoint" {
			previous, next, task, err := workCheckpoint(root, id, text, principal, options.expected)
			if err != nil {
				return Response{}, err
			}
			return mutatingResponse(previous, next, task, principal), nil
		}
		previous, next, submission, err := workSubmit(root, id, text, principal, options.expected)
		if err != nil {
			return Response{}, err
		}
		return mutatingResponse(previous, next, submission, principal), nil

	case "review list":
		items := []Submission{}
		for _, submission := range state.Submissions {
			if submission.Status == "review_pending" {
				items = append(items, submission)
			}
		}
		sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
		return readResponse(map[string]any{"submissions": items}), nil
	case "review context":
		id, err := exactlyOne(parsed.positionals, "review context requires a submission ID")
		if err != nil {
			return Response{}, err
		}
		submission, task, files, err := reviewCheck(root, id)
		if err != nil {
			return Response{}, err
		}
		diff, err := git(root, "diff", submission.BaseCommit+".."+submission.HeadCommit, "--")
		if err != nil {
			return Response{}, err
		}
		return readResponse(map[string]any{"submission": submission, "task": task, "files": files, "diff": diff}), nil
	case "review check", "integrate check":
		id, err := exactlyOne(parsed.positionals, command+" requires a submission ID")
		if err != nil {
			return Response{}, err
		}
		submission, task, files, err := reviewCheck(root, id)
		if err != nil {
			return Response{}, err
		}
		if command == "integrate check" && submission.Status != "approved" {
			return Response{}, &CLIError{Code: "CHS-INTEGRATION-NOT-APPROVED", Message: "submission is not approved", ExitCode: 10}
		}
		return readResponse(map[string]any{"valid": true, "submission": submission, "task": task, "files": files}), nil
	case "review approve", "review request-changes":
		id, err := exactlyOne(parsed.positionals, command+" requires a submission ID")
		if err != nil {
			return Response{}, err
		}
		action := strings.ReplaceAll(command, " ", ".")
		principal, err := principalFor(action)
		if err != nil {
			return Response{}, err
		}
		report, err := optionText(root, parsed, "report")
		if err != nil || report == "" {
			if err == nil {
				err = usageError(command + " requires --report")
			}
			return Response{}, err
		}
		verdict := "approve"
		if command == "review request-changes" {
			verdict = "request_changes"
		}
		previous, next, review, err := recordReview(root, id, verdict, report, principal, options.expected)
		if err != nil {
			return Response{}, err
		}
		return mutatingResponse(previous, next, review, principal), nil
	case "integrate apply":
		id, err := exactlyOne(parsed.positionals, "integrate apply requires a submission ID")
		if err != nil {
			return Response{}, err
		}
		principal, err := principalFor("integrate.apply")
		if err != nil {
			return Response{}, err
		}
		previous, next, integration, err := integrateSubmission(root, id, principal, options.expected)
		if err != nil {
			return Response{}, err
		}
		return mutatingResponse(previous, next, integration, principal), nil
	case "publish check":
		if len(parsed.positionals) != 0 || parsed.values["target"] == "" {
			return Response{}, usageError("publish check requires --target and no positional arguments")
		}
		check, err := publishCheck(root, parsed.values["target"], parsed.values["remote"], parsed.values["branch"])
		if err != nil {
			return Response{}, err
		}
		return readResponse(check), nil
	case "publish apply":
		if len(parsed.positionals) != 0 || parsed.values["target"] == "" {
			return Response{}, usageError("publish apply requires --target and no positional arguments")
		}
		principal, err := principalFor("publish.apply")
		if err != nil {
			return Response{}, err
		}
		previous, next, publication, err := publishApply(root, parsed.values["target"], parsed.values["remote"], parsed.values["branch"], principal, options.expected)
		if err != nil {
			return Response{}, err
		}
		return mutatingResponse(previous, next, publication, principal), nil
	default:
		return Response{}, &CLIError{Code: "CHS-COMMAND-UNKNOWN", Message: "unknown command: " + command, ExitCode: 20, Remedy: []string{"run chassiss help"}}
	}
}

func commandName(words []string) string {
	if len(words) == 0 {
		return ""
	}
	if containsString([]string{"status", "next", "doctor", "verify", "recover", "explain", "version"}, words[0]) || len(words) == 1 {
		return words[0]
	}
	return words[0] + " " + words[1]
}

func commandWordCount(words []string) int {
	if strings.Contains(commandName(words), " ") {
		return 2
	}
	return 1
}

func isWriteCommand(command string) bool {
	return containsString([]string{
		"auth master-init", "auth issue", "auth revoke", "project init", "recover", "template get",
		"artifact submit", "artifact accept", "artifact reject", "mission activate", "mission block", "mission resume", "mission submit-acceptance", "mission accept",
		"task claim", "task assign", "task block", "task resume", "task release", "task cancel", "task supersede", "work open", "work check", "work checkpoint", "work submit", "work block",
		"review approve", "review request-changes", "integrate apply", "publish apply",
	}, command)
}

func response(command, projectID string, before, after int64, result any) Response {
	return Response{APIVersion: APIVersion, OK: true, Command: command, ProjectID: projectID, RevisionBefore: before, RevisionAfter: after, Result: result}
}

func emitSuccess(writer io.Writer, asJSON bool, value Response) int {
	if asJSON {
		encoder := json.NewEncoder(writer)
		encoder.SetEscapeHTML(false)
		if err := encoder.Encode(value); err != nil {
			return 1
		}
		return 0
	}
	fmt.Fprintln(writer, "OK", value.Command)
	if value.ProjectID != "" {
		fmt.Fprintln(writer, "project:", value.ProjectID)
	}
	if value.RevisionAfter != 0 {
		fmt.Fprintf(writer, "revision: %d -> %d\n", value.RevisionBefore, value.RevisionAfter)
	}
	data, _ := json.MarshalIndent(value.Result, "", "  ")
	if len(data) > 0 && string(data) != "null" {
		fmt.Fprintln(writer, string(data))
	}
	return 0
}

func emitFailure(writer io.Writer, asJSON bool, command string, err error) int {
	cliError := &CLIError{Code: "CHS-INTERNAL", Message: err.Error(), ExitCode: 1}
	var typed *CLIError
	if errors.As(err, &typed) {
		cliError = typed
	}
	if asJSON {
		_ = json.NewEncoder(writer).Encode(Response{APIVersion: APIVersion, OK: false, Command: command, Error: &ResponseError{Code: cliError.Code, Message: cliError.Message, Retryable: cliError.Retryable, Remediation: cliError.Remedy}})
	} else {
		fmt.Fprintf(writer, "ERROR %s: %s\n", cliError.Code, cliError.Message)
		for _, remedy := range cliError.Remedy {
			fmt.Fprintln(writer, "-", remedy)
		}
	}
	if cliError.ExitCode <= 0 {
		return 1
	}
	return cliError.ExitCode
}

func resolveRoot(explicit string) (string, error) {
	start := explicit
	if start == "" {
		var err error
		start, err = os.Getwd()
		if err != nil {
			return "", err
		}
	}
	absolute, err := filepath.Abs(start)
	if err != nil {
		return "", err
	}
	for current := filepath.Clean(absolute); ; current = filepath.Dir(current) {
		if info, err := os.Stat(filepath.Join(current, ".chassis")); err == nil && info.IsDir() {
			return current, nil
		}
		parent := filepath.Dir(current)
		if parent == current || explicit != "" {
			break
		}
	}
	return "", &CLIError{Code: "CHS-PROJECT-NOT-FOUND", Message: "no .chassis project found", ExitCode: 10, Remedy: []string{"run inside a CHASSISS project or pass --root"}}
}

func inspectCredential(path string) (any, error) {
	var header struct {
		Kind string `yaml:"kind"`
	}
	if err := loadYAML(path, &header); err != nil {
		return nil, err
	}
	if header.Kind == "chassiss-master-root" {
		root, public, _, err := loadRoot(path)
		if err != nil {
			return nil, err
		}
		return map[string]any{"kind": root.Kind, "version": root.Version, "id": root.ID, "fingerprint": keyFingerprint(public), "created_at": root.CreatedAt}, nil
	}
	var credential Credential
	if err := loadYAML(path, &credential); err != nil {
		return nil, err
	}
	if credential.Kind != "chassiss-role-credential" {
		return nil, &CLIError{Code: "CHS-AUTH-CREDENTIAL", Message: "unsupported credential kind", ExitCode: 11}
	}
	return map[string]any{"kind": credential.Kind, "version": credential.Version, "id": credential.ID, "project_id": credential.ProjectID, "actor": credential.Actor, "role": credential.Role, "actions": credential.Actions, "issued_at": credential.IssuedAt, "not_before": credential.NotBefore, "expires_at": credential.ExpiresAt, "resources": credential.Resources, "persistent": credential.ExpiresAt == nil}, nil
}

func credentialPolicyFromArgs(parsed commandArgs) (CredentialPolicy, error) {
	var policy CredentialPolicy
	if value := parsed.values["not-before"]; value != "" {
		parsedTime, err := time.Parse(time.RFC3339, value)
		if err != nil {
			return policy, usageError("--not-before must be RFC3339")
		}
		policy.NotBefore = &parsedTime
	}
	if parsed.values["expires-at"] != "" && parsed.values["ttl-seconds"] != "" {
		return policy, usageError("use only one of --expires-at or --ttl-seconds")
	}
	if value := parsed.values["expires-at"]; value != "" {
		parsedTime, err := time.Parse(time.RFC3339, value)
		if err != nil {
			return policy, usageError("--expires-at must be RFC3339")
		}
		policy.ExpiresAt = &parsedTime
	}
	if value := parsed.values["ttl-seconds"]; value != "" {
		seconds, err := strconv.ParseInt(value, 10, 64)
		if err != nil || seconds < 1 || seconds > 315360000 {
			return policy, usageError("--ttl-seconds must be between 1 and 315360000")
		}
		expires := timeNow().Add(time.Duration(seconds) * time.Second)
		policy.ExpiresAt = &expires
	}
	policy.Resources = ResourceScope{
		Projects: commaList(parsed.values["projects"]), Missions: commaList(parsed.values["missions"]), Tasks: commaList(parsed.values["tasks"]),
		Submissions: commaList(parsed.values["submissions"]), SubmissionDigests: commaList(parsed.values["submission-digests"]),
		Heads: commaList(parsed.values["heads"]), Baselines: commaList(parsed.values["baselines"]),
	}
	return policy, nil
}

func mustDecodePublic(value string) []byte {
	decoded, _ := base64.RawStdEncoding.DecodeString(value)
	return decoded
}

func exactlyOne(values []string, message string) (string, error) {
	if len(values) != 1 {
		return "", usageError(message)
	}
	return values[0], nil
}

func usageError(message string) error {
	return &CLIError{Code: "CHS-USAGE", Message: message, ExitCode: 20, Remedy: []string{"run chassiss help"}}
}

func notFound(kind string) error {
	return &CLIError{Code: "CHS-NOT-FOUND", Message: kind + " not found", ExitCode: 10}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func commaList(value string) []string {
	if value == "" {
		return nil
	}
	return strings.Split(value, ",")
}

func absolutePath(path string) string {
	absolute, _ := filepath.Abs(path)
	return absolute
}

func optionText(root string, parsed commandArgs, name string) (string, error) {
	value := parsed.values[name]
	if value == "" {
		return "", nil
	}
	if name == "reason" {
		return value, nil
	}
	if info, err := os.Stat(filepath.Join(root, value)); err == nil && !info.IsDir() {
		return readTextFile(root, value)
	}
	return value, nil
}

func artifactBySubmission(state State, id string) (ArtifactState, bool) {
	for _, artifact := range state.Artifacts {
		if artifact.SubmissionID == id {
			return artifact, true
		}
	}
	return ArtifactState{}, false
}

func sortedMissions(state State) []MissionState {
	ids := make([]string, 0, len(state.Missions))
	for id := range state.Missions {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	result := make([]MissionState, 0, len(ids))
	for _, id := range ids {
		result = append(result, state.Missions[id])
	}
	return result
}

func tasksForMission(state State, missionID string) []TaskState {
	result := []TaskState{}
	for _, id := range sortedTaskIDs(state.Tasks) {
		if state.Tasks[id].MissionID == missionID {
			result = append(result, state.Tasks[id])
		}
	}
	return result
}

func taskStatusFilter(parsed commandArgs) string {
	for _, value := range []string{"ready", "active", "blocked", "review"} {
		if parsed.flags[value] {
			return value
		}
	}
	return ""
}

func statusMatchesFilter(status, filter string) bool {
	switch filter {
	case "active":
		return containsString([]string{"claimed", "in_progress", "changes_requested", "approved"}, status)
	case "review":
		return status == "review_pending"
	default:
		return status == filter
	}
}

func explainCode(code string) map[string]any {
	explanations := map[string]string{
		"CHS-CONFLICT-REVISION":       "State changed since it was read. Refresh status and reconsider the action.",
		"CHS-CONFLICT-TRUST-REVISION": "Trust grants or revocations changed since they were read. Reload trust metadata and reconsider the authorization action.",
		"CHS-AUTH-REVOKED":            "The selected long-lived credential was explicitly revoked by Master.",
		"CHS-WORK-SCOPE":              "At least one changed file is outside the Task allowed_paths contract.",
		"CHS-REVIEW-INDEPENDENCE":     "The same actor identity cannot author and approve a submission.",
		"CHS-INTEGRITY-EVENTS":        "The signed event log is missing, reordered, modified, or otherwise invalid.",
	}
	message := explanations[code]
	if message == "" {
		message = "No detailed explanation is registered for this code. The error response remains authoritative."
	}
	return map[string]any{"code": code, "explanation": message}
}

const helpText = `CHASSISS ` + Version + `

Usage:
  chassiss [--root PATH] [--credential FILE] [--json]
           [--expect-revision N] [--expect-trust-revision N]
           <group> <action> [arguments]

Core commands:
  auth master-init|issue|inspect|revoke
  project init
  status | next | doctor | verify | recover | explain
  template list|get
  artifact check|submit|list|context|accept|reject
  mission list|context|activate|block|resume|submit-acceptance|accept
  task list|context|claim|assign|block|resume|release|cancel|supersede
  work open|context|status|diff|check|checkpoint|submit|block
  review list|context|check|approve|request-changes
  integrate check|apply
  publish check|apply

Run commands with --json for stable agent-readable envelopes.
`
