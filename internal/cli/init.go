package cli

import (
	"bytes"
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ExplodeCode6324/chassiss/internal/contracts"
	"github.com/ExplodeCode6324/chassiss/internal/cryptoutil"
	"github.com/ExplodeCode6324/chassiss/internal/gitstore"
	"github.com/ExplodeCode6324/chassiss/internal/localstate"
	"github.com/ExplodeCode6324/chassiss/internal/protocol"
	"github.com/ExplodeCode6324/chassiss/internal/state"
)

func initCommand(ctx context.Context, invocation invocation) (Envelope, error) {
	projectID := invocation.Value("project")
	if err := protocol.ValidateID(protocol.IDProject, projectID); err != nil {
		return Envelope{}, usageError(err.Error())
	}
	architectureData, err := os.ReadFile(invocation.Value("architecture"))
	if err != nil {
		return Envelope{}, protocol.WrapError(protocol.ErrPathNotFound, protocol.CategoryLocal, "Cannot read Architecture candidate.", err)
	}
	architecture, err := contracts.ParseArchitecture(architectureData)
	if err != nil {
		return Envelope{}, protocol.WrapError(protocol.ErrArchitectureInvalid, protocol.CategoryValidation, "Architecture candidate is invalid.", err)
	}
	taskbookData, err := os.ReadFile(invocation.Value("taskbook"))
	if err != nil {
		return Envelope{}, protocol.WrapError(protocol.ErrPathNotFound, protocol.CategoryLocal, "Cannot read Taskbook candidate.", err)
	}
	taskbook, err := contracts.ParseTaskbook(taskbookData, architecture)
	if err != nil {
		return Envelope{}, protocol.WrapError(protocol.ErrTaskbookInvalid, protocol.CategoryValidation, "Taskbook candidate is invalid.", err)
	}
	store, err := localstate.OpenDefault()
	if err != nil {
		return Envelope{}, err
	}
	rootHandle, err := resolveKeyHandle(invocation.Value("root-key"), store.Paths)
	if err != nil {
		return Envelope{}, protocol.WrapError(protocol.ErrSecretKeyNotFound, protocol.CategoryLocal, "Root private-key handle cannot be resolved.", err)
	}
	rootPublic, rootFingerprint, err := cryptoutil.PublicFromHandle(rootHandle)
	if err != nil {
		return Envelope{}, protocol.WrapError(protocol.ErrSecretKeyNotFound, protocol.CategoryLocal, "Root private key cannot be loaded.", err)
	}
	rootPath, err := cryptoutil.ResolveFileHandle(rootHandle)
	if err != nil {
		return Envelope{}, err
	}
	repository, err := filepath.Abs(".")
	if err != nil {
		return Envelope{}, err
	}
	if pathWithin(repository, rootPath) {
		return Envelope{}, protocol.NewError(protocol.ErrUsageInvalid, protocol.CategoryUsage, "Root private key must be stored outside the Project repository.")
	}
	gitMarker := filepath.Join(repository, ".git")
	runner := gitstore.New(repository)
	if _, err := os.Stat(gitMarker); err == nil {
		if _, err := runner.Run(ctx, "rev-parse", "--verify", "HEAD^{commit}"); err == nil {
			return Envelope{}, protocol.NewError(protocol.ErrProjectAlreadyRegistered, protocol.CategoryLocal, "Target directory already has Git history.")
		}
	} else if os.IsNotExist(err) {
		bootstrap := gitstore.New("")
		if _, err := bootstrap.Run(ctx, "init", "-b", "main", repository); err != nil {
			return Envelope{}, err
		}
	} else if err != nil {
		return Envelope{}, err
	}
	runner = gitstore.New(repository)
	format, err := runner.ObjectFormat(ctx)
	if err != nil {
		return Envelope{}, err
	}
	architectureBlob, err := runner.HashBlob(ctx, architectureData)
	if err != nil {
		return Envelope{}, err
	}
	taskbookBlob, err := runner.HashBlob(ctx, taskbookData)
	if err != nil {
		return Envelope{}, err
	}
	mapping, err := scanInitialTree(ctx, runner, repository)
	if err != nil {
		return Envelope{}, err
	}
	mapping["docs/architecture.yaml"] = gitstore.Entry{Mode: "100644", OID: architectureBlob}
	mapping["docs/taskbook.yaml"] = gitstore.Entry{Mode: "100644", OID: taskbookBlob}
	operationID, err := operationID(invocation)
	if err != nil {
		return Envelope{}, err
	}
	operation := protocol.Operation{
		Schema: protocol.OperationSchema, OperationID: operationID,
		Action: "project.genesis", Project: projectID,
		Authority: "root:" + invocationRootKeyID(rootHandle, rootPublic, invocation),
		Target:    projectID, Preconditions: map[string]any{},
		Payload: map[string]any{
			"architecture_blob": architectureBlob, "project_id": projectID,
			"root_key_id":     invocationRootKeyID(rootHandle, rootPublic, invocation),
			"root_public_key": rootPublic, "taskbook_blob": taskbookBlob,
		},
	}
	operationDigest, err := protocol.ObjectDigest("operation", operation)
	if err != nil {
		return Envelope{}, err
	}
	evidence := protocol.ExecutionEvidence{
		Schema: protocol.EvidenceSchema, OperationDigest: operationDigest,
		Action: operation.Action, Attempt: 1, Parent: nil,
		Facts: map[string]any{
			"architecture_blob": architectureBlob,
			"initial_tree":      strings.Repeat("0", map[bool]int{true: 64, false: 40}[format == "sha256"]),
			"taskbook_blob":     taskbookBlob,
		},
	}
	ready := make([]string, 0, len(taskbook.Tasks))
	for id := range taskbook.Tasks {
		ready = append(ready, id)
	}
	sort.Strings(ready)
	initialState, err := state.Reduce(nil, operation, evidence, state.ReduceFacts{
		ObjectFormat: format, GenesisTaskbookID: taskbook.ID, GenesisReadyTasks: ready,
	})
	if err != nil {
		return Envelope{}, err
	}
	stateData, err := state.Encode(initialState, format)
	if err != nil {
		return Envelope{}, err
	}
	stateBlob, err := runner.HashBlob(ctx, stateData)
	if err != nil {
		return Envelope{}, err
	}
	mapping[".chassiss/state.json"] = gitstore.Entry{Mode: "100644", OID: stateBlob}
	tree, err := runner.WriteTree(ctx, mapping)
	if err != nil {
		return Envelope{}, err
	}
	evidence.Facts["initial_tree"] = tree
	stateDigest := protocol.DigestBytes(stateData)
	message, err := protocol.BuildTransitionMessage(operation, evidence, stateDigest, format)
	if err != nil {
		return Envelope{}, err
	}
	rootKeyPath, _ := cryptoutil.ResolveFileHandle(rootHandle)
	commit, err := runner.CommitTree(ctx, tree, nil, message, rootKeyPath, gitstore.CommitIdentity{})
	if err != nil {
		return Envelope{}, protocol.WrapError(protocol.ErrCommitSignatureInvalid, protocol.CategoryLocal, "Cannot create signed Genesis commit.", err)
	}
	zeroOID := strings.Repeat("0", map[bool]int{true: 64, false: 40}[format == "sha256"])
	if err := runner.UpdateRefCAS(ctx, "refs/heads/main", commit, zeroOID, "CHASSISS Genesis"); err != nil {
		return Envelope{}, err
	}
	if _, err := runner.Run(ctx, "symbolic-ref", "HEAD", "refs/heads/main"); err != nil {
		return Envelope{}, err
	}
	if _, err := runner.Run(ctx, "read-tree", "refs/heads/main"); err != nil {
		return Envelope{}, err
	}
	if err := writeProjectFile(repository, ".chassiss/state.json", stateData, 0o644); err != nil {
		return Envelope{}, err
	}
	if err := writeProjectFile(repository, "docs/architecture.yaml", architectureData, 0o644); err != nil {
		return Envelope{}, err
	}
	if err := writeProjectFile(repository, "docs/taskbook.yaml", taskbookData, 0o644); err != nil {
		return Envelope{}, err
	}
	gitDirResult, err := runner.Run(ctx, "rev-parse", "--absolute-git-dir")
	if err != nil {
		return Envelope{}, err
	}
	remoteURL := invocation.Value("remote")
	if remoteURL != "" {
		if err := validateRemoteURL(remoteURL); err != nil {
			return Envelope{}, usageError(err.Error())
		}
		if _, err := runner.Run(ctx, "remote", "add", "origin", remoteURL); err != nil {
			return Envelope{}, err
		}
		if _, pushErr := runner.Run(ctx, "push", "--porcelain", "--atomic", "origin", commit+":refs/heads/main"); pushErr != nil {
			actual, exists, reconcileErr := readRemoteRef(ctx, runner, "origin", "refs/heads/main")
			if reconcileErr != nil {
				failure := protocol.WrapError(
					protocol.ErrPushResultUnknown, protocol.CategoryNetwork,
					"Genesis push result requires reconciliation.", pushErr,
				)
				failure.Retryable = true
				failure.Details["reconciliation_error"] = reconcileErr.Error()
				return Envelope{}, failure
			}
			if !exists || actual != commit {
				failure := protocol.WrapError(
					protocol.ErrRemoteUnreachable, protocol.CategoryNetwork,
					"Remote verification confirmed that Genesis was not published.", pushErr,
				)
				failure.Details["actual_head"] = actual
				failure.Details["expected_head"] = commit
				return Envelope{}, failure
			}
		}
	}
	remoteFingerprint := ""
	if remoteURL != "" {
		remoteFingerprint = protocol.DigestBytes([]byte(remoteURL))
	}
	if err := store.Update(func(local *localstate.State) error {
		if _, exists := local.Projects[projectID]; exists {
			return protocol.NewError(protocol.ErrProjectAlreadyRegistered, protocol.CategoryLocal, "Project ID is already registered locally.")
		}
		local.Projects[projectID] = localstate.Project{
			GenesisCommit: commit,
			Identity: localstate.Identity{
				Keys: map[string]localstate.Key{
					invocationRootKeyID(rootHandle, rootPublic, invocation): {PrivateKeyHandle: rootHandle},
				},
				SelectedKeyID: invocationRootKeyID(rootHandle, rootPublic, invocation),
			},
			MinimumCheckpoint: localstate.Checkpoint{Commit: commit, StateDigest: stateDigest},
			PendingOperations: map[string]localstate.PendingOperation{},
			Remote: localstate.Remote{
				URL: remoteURL, URLFingerprint: remoteFingerprint,
			},
			RepoInstances: []localstate.RepoInstance{{
				CreatedByCLI: true, GitDir: stringTrimSpace(gitDirResult.Stdout),
				LastVerifiedHead: commit, RepoPath: repository,
			}},
			RootFingerprint: rootFingerprint, Worktrees: map[string]localstate.Worktree{},
		}
		return nil
	}); err != nil {
		return Envelope{}, err
	}
	envelope := baseEnvelope("init")
	envelope.Project = &ProjectBody{ID: projectID, Protocol: protocol.ProtocolID, RootFingerprint: rootFingerprint}
	architectureValue := architectureBlob
	taskbookValue := taskbookBlob
	envelope.Snapshot = &SnapshotBody{
		ArchitectureBlob: &architectureValue, MainCommit: commit, Offline: false,
		StateDigest: stateDigest, TaskbookBlob: &taskbookValue, Trust: "verified",
	}
	evidenceDigest, _ := protocol.ObjectDigest("execution-evidence", evidence)
	envelope.Operation = &OperationBody{
		Commit: commit, EvidenceAttempt: 1, EvidenceDigest: evidenceDigest,
		OperationDigest: operationDigest, OperationID: operationID,
		Signer: SignerBody{
			Actor: "root", Authority: operation.Authority,
			KeyFingerprint: rootFingerprint,
			KeyID:          invocationRootKeyID(rootHandle, rootPublic, invocation),
			Root:           true,
		},
		Status: "published",
	}
	envelope.Result = map[string]any{"genesis": commit, "repository": repository}
	return envelope, nil
}

func scanInitialTree(ctx context.Context, runner gitstore.Runner, repository string) (gitstore.TreeMap, error) {
	result, err := runner.Run(ctx, "ls-files", "--cached", "--others", "--exclude-standard", "-z")
	if err != nil {
		return nil, err
	}
	mapping := gitstore.TreeMap{}
	for _, raw := range bytes.Split(result.Stdout, []byte{0}) {
		if len(raw) == 0 {
			continue
		}
		path := string(raw)
		if filepath.Base(path) == ".DS_Store" {
			continue
		}
		if err := contracts.ValidateRepoPath(path); err != nil {
			return nil, protocol.WrapError(protocol.ErrPathEncodingInvalid, protocol.CategoryValidation, "Initial tree contains an invalid path.", err)
		}
		if strings.HasPrefix(path, ".chassiss/") || contracts.IsProtectedPath(path) {
			failure := protocol.NewError(protocol.ErrDirectGitStateDetected, protocol.CategoryValidation, "Existing CHASSISS protected data is prohibited before init.")
			failure.Details["path"] = path
			return nil, failure
		}
		absolute := filepath.Join(repository, filepath.FromSlash(path))
		info, err := os.Lstat(absolute)
		if err != nil {
			return nil, err
		}
		var data []byte
		mode := "100644"
		switch {
		case info.Mode()&os.ModeSymlink != 0:
			target, err := os.Readlink(absolute)
			if err != nil {
				return nil, err
			}
			if symlinkTargetEscapesRepository(target) {
				return nil, protocol.NewError(protocol.ErrScopeViolation, protocol.CategoryValidation, "Initial tree contains a symlink that escapes the repository.")
			}
			data, mode = []byte(target), "120000"
		case info.Mode().IsRegular():
			data, err = os.ReadFile(absolute)
			if err != nil {
				return nil, err
			}
			if info.Mode()&0o111 != 0 {
				mode = "100755"
			}
		default:
			return nil, protocol.NewError(protocol.ErrScopeViolation, protocol.CategoryValidation, "Initial tree contains an unsupported filesystem entry.")
		}
		oid, err := runner.HashBlob(ctx, data)
		if err != nil {
			return nil, err
		}
		mapping[path] = gitstore.Entry{Mode: mode, OID: oid}
	}
	return mapping, nil
}

func writeProjectFile(root, relative string, data []byte, mode os.FileMode) error {
	path := filepath.Join(root, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(path), "chassiss-write-")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	if err := temp.Chmod(mode); err != nil {
		temp.Close()
		return err
	}
	if _, err := temp.Write(data); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Sync(); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return os.Rename(tempPath, path)
}

func operationID(invocation invocation) (string, error) {
	if value := invocation.Value("operation-id"); value != "" {
		if err := protocol.ValidateOperationID(value); err != nil {
			return "", usageError(err.Error())
		}
		return value, nil
	}
	return protocol.NewOperationID()
}

func invocationRootKeyID(handle, public string, invocation invocation) string {
	value := invocation.Value("root-key")
	if strings.HasPrefix(value, "KEY-") {
		return value
	}
	base := filepath.Base(strings.TrimPrefix(handle, cryptoutil.FileHandlePrefix))
	if strings.HasPrefix(base, "KEY-") {
		return base
	}
	// init requires a stable ID even for externally managed handles. The
	// fingerprint-derived ID is deterministic but intentionally public-only.
	fingerprint, _ := cryptoutil.Fingerprint(public)
	token := strings.NewReplacer("SHA256:", "", "/", "-", "+", "-").Replace(fingerprint)
	token = strings.ToUpper(token)
	if len(token) > 32 {
		token = token[:32]
	}
	return "KEY-" + token
}

func resolveKeyHandle(value string, paths localstate.Paths) (string, error) {
	switch {
	case strings.HasPrefix(value, cryptoutil.FileHandlePrefix):
		_, err := cryptoutil.ResolveFileHandle(value)
		return value, err
	case strings.HasPrefix(value, "KEY-"):
		handle := cryptoutil.FileHandlePrefix + filepath.Join(paths.Keys, value)
		_, err := cryptoutil.ResolveFileHandle(handle)
		return handle, err
	case filepath.IsAbs(value):
		handle := cryptoutil.FileHandlePrefix + value
		_, err := cryptoutil.ResolveFileHandle(handle)
		return handle, err
	default:
		return "", fmt.Errorf("key must be a KEY ID, absolute path, or file: handle")
	}
}

func pathWithin(root, path string) bool {
	relative, err := filepath.Rel(root, path)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func validateRemoteURL(value string) error {
	if value == "" || strings.TrimSpace(value) != value ||
		strings.ContainsAny(value, "\r\n\x00") {
		return fmt.Errorf("remote URL is empty or contains prohibited whitespace")
	}
	if filepath.IsAbs(value) || strings.HasPrefix(value, "./") ||
		strings.HasPrefix(value, "../") || strings.HasPrefix(value, "file:") {
		return fmt.Errorf("local/file Git transports are prohibited")
	}
	if strings.Contains(value, "://") {
		parsed, err := url.Parse(value)
		if err != nil {
			return err
		}
		switch parsed.Scheme {
		case "https", "ssh", "git":
		default:
			return fmt.Errorf("remote URL scheme must be https, ssh, or git")
		}
		if parsed.Host == "" || parsed.Path == "" {
			return fmt.Errorf("remote URL must contain a host and repository path")
		}
		if parsed.RawQuery != "" || parsed.Fragment != "" {
			return fmt.Errorf("remote URL query and fragment are prohibited")
		}
		if parsed.User != nil {
			if _, hasPassword := parsed.User.Password(); hasPassword || parsed.Scheme != "ssh" {
				return fmt.Errorf("remote URL must not contain embedded credentials")
			}
		}
		return nil
	}
	colon := strings.IndexByte(value, ':')
	if colon <= 0 || colon == len(value)-1 {
		return fmt.Errorf("remote URL must be an absolute network URL or SSH scp-style location")
	}
	host, repository := value[:colon], value[colon+1:]
	if strings.ContainsAny(host, "/\\") || strings.Contains(repository, "\\") ||
		strings.Contains(repository, ":") ||
		strings.Contains(host, "@") && strings.HasPrefix(host, "@") {
		return fmt.Errorf("SSH scp-style remote URL is invalid")
	}
	return nil
}
