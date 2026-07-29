package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/ExplodeCode6324/chassiss/internal/contracts"
	"github.com/ExplodeCode6324/chassiss/internal/cryptoutil"
	"github.com/ExplodeCode6324/chassiss/internal/gitstore"
	"github.com/ExplodeCode6324/chassiss/internal/localstate"
	"github.com/ExplodeCode6324/chassiss/internal/protocol"
	"github.com/ExplodeCode6324/chassiss/internal/state"
)

const sourceHistoryPath = "docs/chassiss/onboarding/source-history.md"
const maxSourceHistoryNotes = 1 << 20

func bootstrapCommand(ctx context.Context, invocation invocation) (Envelope, error) {
	projectID := invocation.Value("project")
	if err := protocol.ValidateID(protocol.IDProject, projectID); err != nil {
		return Envelope{}, usageError(err.Error())
	}
	repository, err := filepath.Abs(".")
	if err != nil {
		return Envelope{}, err
	}
	entries, err := os.ReadDir(repository)
	if err != nil {
		return Envelope{}, err
	}
	if len(entries) != 0 {
		return Envelope{}, protocol.NewError(protocol.ErrProjectAlreadyRegistered, protocol.CategoryLocal, "Bootstrap target directory must be empty.")
	}
	sourceRepository, err := filepath.Abs(invocation.Value("source"))
	if err != nil {
		return Envelope{}, err
	}
	if samePath(repository, sourceRepository) || pathWithin(sourceRepository, repository) ||
		pathWithin(repository, sourceRepository) {
		return Envelope{}, usageError("bootstrap source and target must be separate directories")
	}
	sourceRunner := gitstore.New(sourceRepository)
	sourceFormat, err := sourceRunner.ObjectFormat(ctx)
	if err != nil {
		return Envelope{}, protocol.WrapError(protocol.ErrProjectNotFound, protocol.CategoryLocal, "Bootstrap source is not a readable Git repository.", err)
	}
	sourceCommitOID := invocation.Value("ref")
	if err := protocol.ValidateOID(sourceCommitOID, sourceFormat); err != nil {
		return Envelope{}, usageError("--ref must be a full source commit OID")
	}
	sourceCommit, err := sourceRunner.ReadCommit(ctx, sourceCommitOID)
	if err != nil {
		return Envelope{}, protocol.WrapError(protocol.ErrPathNotFound, protocol.CategoryLocal, "Bootstrap source commit cannot be read.", err)
	}
	sourceTree, err := sourceRunner.ReadTree(ctx, sourceCommit.Tree)
	if err != nil {
		return Envelope{}, err
	}
	if err := validateSourceTree(ctx, sourceRunner, sourceTree); err != nil {
		return Envelope{}, err
	}
	store, err := localstate.OpenDefault()
	if err != nil {
		return Envelope{}, err
	}
	local, err := store.Load()
	if err != nil {
		return Envelope{}, err
	}
	if _, exists := local.Projects[projectID]; exists {
		return Envelope{}, protocol.NewError(protocol.ErrProjectAlreadyRegistered, protocol.CategoryLocal, "Project ID is already registered locally.")
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
	if pathWithin(repository, rootPath) {
		return Envelope{}, usageError("Root private key must be stored outside the Project repository")
	}
	if remoteURL := invocation.Value("remote"); remoteURL != "" {
		if err := validateRemoteURL(remoteURL); err != nil {
			return Envelope{}, usageError(err.Error())
		}
	}
	historyNotes, err := readHistoryNotes(invocation.Value("history"))
	if err != nil {
		return Envelope{}, err
	}
	if _, err := gitstore.New("").Run(ctx, "init", "-b", "main", repository); err != nil {
		return Envelope{}, err
	}
	runner := gitstore.New(repository)
	format, err := runner.ObjectFormat(ctx)
	if err != nil {
		return Envelope{}, err
	}
	mapping, err := importSourceTree(ctx, sourceRunner, runner, sourceTree)
	if err != nil {
		return Envelope{}, err
	}
	historyData := sourceHistoryDocument(sourceCommitOID, sourceCommit.Tree, sourceFormat, len(mapping), historyNotes)
	historyBlob, err := runner.HashBlob(ctx, historyData)
	if err != nil {
		return Envelope{}, err
	}
	mapping[sourceHistoryPath] = gitstore.Entry{Mode: "100644", OID: historyBlob}
	rootKeyID := invocationRootKeyID(rootHandle, rootPublic, invocation)
	operationID, err := operationID(invocation)
	if err != nil {
		return Envelope{}, err
	}
	operation := protocol.Operation{
		Schema: protocol.OperationSchema, OperationID: operationID,
		Action: "project.bootstrap", Project: projectID,
		Authority: "root:" + rootKeyID, Target: projectID,
		Preconditions: map[string]any{},
		Payload: map[string]any{
			"project_id": projectID, "root_key_id": rootKeyID,
			"root_public_key": rootPublic, "source_commit": sourceCommitOID,
			"source_history_blob": historyBlob, "source_object_format": sourceFormat,
			"source_tree": sourceCommit.Tree,
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
			"initial_tree":  strings.Repeat("0", map[bool]int{true: 64, false: 40}[format == "sha256"]),
			"source_commit": sourceCommitOID, "source_history_blob": historyBlob,
			"source_tree": sourceCommit.Tree,
		},
	}
	initialState, err := state.Reduce(nil, operation, evidence, state.ReduceFacts{ObjectFormat: format})
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
		return Envelope{}, protocol.WrapError(protocol.ErrCommitSignatureInvalid, protocol.CategoryLocal, "Cannot create signed bootstrap commit.", err)
	}
	zeroOID := strings.Repeat("0", map[bool]int{true: 64, false: 40}[format == "sha256"])
	if err := runner.UpdateRefCAS(ctx, "refs/heads/main", commit, zeroOID, "CHASSISS source bootstrap"); err != nil {
		return Envelope{}, err
	}
	if _, err := runner.Run(ctx, "symbolic-ref", "HEAD", "refs/heads/main"); err != nil {
		return Envelope{}, err
	}
	if _, err := runner.Run(ctx, "read-tree", "--reset", "-u", "refs/heads/main"); err != nil {
		return Envelope{}, err
	}
	remoteURL := invocation.Value("remote")
	if remoteURL != "" {
		if _, err := runner.Run(ctx, "remote", "add", "origin", remoteURL); err != nil {
			return Envelope{}, err
		}
		if _, pushErr := runner.Run(ctx, "push", "--porcelain", "--atomic", "origin", commit+":refs/heads/main"); pushErr != nil {
			actual, exists, reconcileErr := readRemoteRef(ctx, runner, "origin", "refs/heads/main")
			if reconcileErr != nil {
				failure := protocol.WrapError(protocol.ErrPushResultUnknown, protocol.CategoryNetwork, "Bootstrap push result requires reconciliation.", pushErr)
				failure.Retryable = true
				failure.Details["reconciliation_error"] = reconcileErr.Error()
				return Envelope{}, failure
			}
			if !exists || actual != commit {
				failure := protocol.WrapError(protocol.ErrRemoteUnreachable, protocol.CategoryNetwork, "Remote verification confirmed that bootstrap was not published.", pushErr)
				failure.Details["actual_head"] = actual
				failure.Details["expected_head"] = commit
				return Envelope{}, failure
			}
		}
	}
	gitDirResult, err := runner.Run(ctx, "rev-parse", "--absolute-git-dir")
	if err != nil {
		return Envelope{}, err
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
				Keys:          map[string]localstate.Key{rootKeyID: {PrivateKeyHandle: rootHandle}},
				SelectedKeyID: rootKeyID,
			},
			MinimumCheckpoint: localstate.Checkpoint{Commit: commit, StateDigest: stateDigest},
			PendingOperations: map[string]localstate.PendingOperation{},
			Remote:            localstate.Remote{URL: remoteURL, URLFingerprint: remoteFingerprint},
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
	envelope := baseEnvelope("bootstrap")
	envelope.Project = &ProjectBody{ID: projectID, Protocol: protocol.ProtocolID, RootFingerprint: rootFingerprint}
	envelope.Snapshot = &SnapshotBody{
		ArchitectureBlob: nil, MainCommit: commit, Offline: false,
		StateDigest: stateDigest, TaskbookBlob: nil, Trust: "verified",
	}
	evidenceDigest, _ := protocol.ObjectDigest("execution-evidence", evidence)
	envelope.Operation = &OperationBody{
		Commit: commit, EvidenceAttempt: 1, EvidenceDigest: evidenceDigest,
		OperationDigest: operationDigest, OperationID: operationID,
		Signer: SignerBody{
			Actor: "root", Authority: operation.Authority, KeyFingerprint: rootFingerprint,
			KeyID: rootKeyID, Root: true,
		},
		Status: "published",
	}
	envelope.Result = map[string]any{
		"bootstrap": commit, "history": sourceHistoryPath, "repository": repository,
		"source_commit": sourceCommitOID, "source_tree": sourceCommit.Tree,
	}
	return envelope, nil
}

func importSourceTree(
	ctx context.Context,
	source, target gitstore.Runner,
	sourceTree gitstore.TreeMap,
) (gitstore.TreeMap, error) {
	mapping := make(gitstore.TreeMap, len(sourceTree)+2)
	for path, entry := range sourceTree {
		if err := contracts.ValidateRepoPath(path); err != nil {
			return nil, protocol.WrapError(protocol.ErrPathEncodingInvalid, protocol.CategoryValidation, "Source tree contains an invalid path.", err)
		}
		if strings.HasPrefix(path, ".chassiss/") || contracts.IsProtectedPath(path) {
			failure := protocol.NewError(protocol.ErrProtectedPathChanged, protocol.CategoryValidation, "Source tree collides with a CHASSISS protected path.")
			failure.Details["path"] = path
			return nil, failure
		}
		if entry.Mode == "160000" {
			failure := protocol.NewError(protocol.ErrFeatureNotInV1, protocol.CategoryUnsupported, "Source tree contains a submodule; flatten or remove it before bootstrap.")
			failure.Details["path"] = path
			return nil, failure
		}
		if entry.Mode != "100644" && entry.Mode != "100755" && entry.Mode != "120000" {
			return nil, protocol.NewError(protocol.ErrScopeViolation, protocol.CategoryValidation, "Source tree contains an unsupported Git mode.")
		}
		data, err := source.ReadBlob(ctx, entry.OID)
		if err != nil {
			return nil, err
		}
		if entry.Mode == "120000" {
			link := string(data)
			if filepath.IsAbs(link) || containsParentSegment(filepath.ToSlash(link)) {
				return nil, protocol.NewError(protocol.ErrScopeViolation, protocol.CategoryValidation, "Source tree contains a symlink that escapes the repository.")
			}
		}
		oid, err := target.HashBlob(ctx, data)
		if err != nil {
			return nil, err
		}
		mapping[path] = gitstore.Entry{Mode: entry.Mode, OID: oid}
	}
	return mapping, nil
}

func validateSourceTree(ctx context.Context, source gitstore.Runner, sourceTree gitstore.TreeMap) error {
	for path, entry := range sourceTree {
		if err := contracts.ValidateRepoPath(path); err != nil {
			return protocol.WrapError(protocol.ErrPathEncodingInvalid, protocol.CategoryValidation, "Source tree contains an invalid path.", err)
		}
		if strings.HasPrefix(path, ".chassiss/") || contracts.IsProtectedPath(path) {
			failure := protocol.NewError(protocol.ErrProtectedPathChanged, protocol.CategoryValidation, "Source tree collides with a CHASSISS protected path.")
			failure.Details["path"] = path
			return failure
		}
		if entry.Mode == "160000" {
			failure := protocol.NewError(protocol.ErrFeatureNotInV1, protocol.CategoryUnsupported, "Source tree contains a submodule; flatten or remove it before bootstrap.")
			failure.Details["path"] = path
			return failure
		}
		if entry.Mode != "100644" && entry.Mode != "100755" && entry.Mode != "120000" {
			return protocol.NewError(protocol.ErrScopeViolation, protocol.CategoryValidation, "Source tree contains an unsupported Git mode.")
		}
		if entry.Mode == "120000" {
			data, err := source.ReadBlob(ctx, entry.OID)
			if err != nil {
				return err
			}
			link := string(data)
			if filepath.IsAbs(link) || containsParentSegment(filepath.ToSlash(link)) {
				return protocol.NewError(protocol.ErrScopeViolation, protocol.CategoryValidation, "Source tree contains a symlink that escapes the repository.")
			}
		}
	}
	return nil
}

func readHistoryNotes(path string) ([]byte, error) {
	if path == "" {
		return nil, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, protocol.WrapError(protocol.ErrPathNotFound, protocol.CategoryLocal, "Cannot read source history notes.", err)
	}
	if !utf8.Valid(data) || strings.IndexByte(string(data), 0) >= 0 {
		return nil, protocol.NewError(protocol.ErrPathEncodingInvalid, protocol.CategoryValidation, "Source history notes must be UTF-8 text without NUL.")
	}
	if len(data) > maxSourceHistoryNotes {
		return nil, protocol.NewError(protocol.ErrScopeViolation, protocol.CategoryValidation, "Source history notes must not exceed 1 MiB.")
	}
	return data, nil
}

func sourceHistoryDocument(commit, tree, format string, paths int, notes []byte) []byte {
	document := fmt.Sprintf(`# Source history reference

This document records the source snapshot adopted into CHASSISS. The source
repository and its earlier commits are non-authoritative references: they are
not CHASSISS Transitions and do not grant authority.

- Source commit: %s
- Source tree: %s
- Source object format: %s
- Imported paths: %d

The Root-signed bootstrap commit binds this document and the imported ordinary
tree. Future protocol verification starts at that bootstrap commit.
`, commit, tree, format, paths)
	if len(notes) != 0 {
		document += "\n## Human-curated history notes\n\n" + strings.TrimSpace(string(notes)) + "\n"
	}
	return []byte(document)
}
