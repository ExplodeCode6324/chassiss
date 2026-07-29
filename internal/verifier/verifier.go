package verifier

import (
	"bytes"
	"context"
	"fmt"
	"reflect"
	"sort"
	"strings"

	"github.com/ExplodeCode6324/chassiss/internal/contracts"
	"github.com/ExplodeCode6324/chassiss/internal/cryptoutil"
	"github.com/ExplodeCode6324/chassiss/internal/gitstore"
	"github.com/ExplodeCode6324/chassiss/internal/protocol"
	"github.com/ExplodeCode6324/chassiss/internal/state"
)

type Options struct {
	ExpectedProject         string
	ExpectedRootFingerprint string
	MinimumCheckpoint       string
	Full                    bool
}

type Result struct {
	Architecture    *contracts.Architecture
	Genesis         string
	Head            string
	ObjectFormat    string
	RootFingerprint string
	State           *state.State
	StateDigest     string
	Taskbook        *contracts.Taskbook
	Transitions     []Transition
}

type Transition struct {
	Action          string
	Commit          string
	OperationDigest string
	OperationID     string
	StateDigest     string
	Target          string
}

type verifier struct {
	runner          gitstore.Runner
	format          string
	knownKeys       map[string]map[string]struct{}
	seenOperations  map[string]string
	seenResources   map[string]struct{}
	seenTaskbookIDs map[string]struct{}
	architectureID  string
}

func Verify(ctx context.Context, runner gitstore.Runner, target string, options Options) (*Result, error) {
	format, err := runner.ObjectFormat(ctx)
	if err != nil {
		return nil, err
	}
	targetOID, err := runner.Resolve(ctx, target)
	if err != nil {
		return nil, err
	}
	if options.MinimumCheckpoint != "" {
		descendant, err := runner.IsAncestor(ctx, options.MinimumCheckpoint, targetOID)
		if err != nil {
			return nil, err
		}
		if !descendant {
			return nil, protocol.NewError(protocol.ErrMainlineRollback, protocol.CategoryTrust, "Target main is below the minimum trusted checkpoint.")
		}
	}
	chain, err := firstParentChain(ctx, runner, targetOID)
	if err != nil {
		return nil, err
	}
	engine := verifier{
		runner: runner, format: format, knownKeys: map[string]map[string]struct{}{},
		seenOperations: map[string]string{}, seenResources: map[string]struct{}{},
		seenTaskbookIDs: map[string]struct{}{},
	}
	result := &Result{Head: targetOID, ObjectFormat: format, Transitions: make([]Transition, 0, len(chain))}
	var parentState *state.State
	var parentCommit gitstore.Commit
	var parentTree gitstore.TreeMap
	for index, commit := range chain {
		tree, stateBytes, parsedState, err := engine.readState(ctx, commit)
		if err != nil {
			return nil, fmt.Errorf("commit %s: %w", commit.OID, err)
		}
		message, err := protocol.ParseTransitionMessage(commit.Message, format)
		if err != nil {
			return nil, protocol.WrapError(protocol.ErrTrailerInvalid, protocol.CategoryProtocol, "Transition message is invalid.", err)
		}
		if !gitstore.ParentCountAllowed(message.Operation.Action, len(commit.Parents)) {
			return nil, protocol.NewError(protocol.ErrMainlineNonLinear, protocol.CategoryProtocol, "Transition parent topology is invalid.")
		}
		if index > 0 && (len(commit.Parents) == 0 || commit.Parents[0] != parentCommit.OID) {
			return nil, protocol.NewError(protocol.ErrMainlineNonLinear, protocol.CategoryProtocol, "First-parent chain is not contiguous.")
		}
		stateDigest := protocol.DigestBytes(stateBytes)
		if message.Trailers.StateDigest != stateDigest {
			return nil, protocol.NewError(protocol.ErrStateDigestMismatch, protocol.CategoryProtocol, "Transition State digest does not match the exact State blob.")
		}
		var reduced *state.State
		var architecture *contracts.Architecture
		var taskbook *contracts.Taskbook
		if index == 0 {
			reduced, architecture, taskbook, err = engine.verifyGenesis(ctx, commit, tree, parsedState, message, options)
			if err != nil {
				return nil, err
			}
			result.Genesis = commit.OID
			result.RootFingerprint, _ = cryptoutil.Fingerprint(reduced.Authority.Root.PublicKey)
		} else {
			publicKey, err := authorityPublicKey(parentState, message.Operation.Authority)
			if err != nil {
				return nil, err
			}
			if err := runner.VerifyCommitSSH(ctx, commit.OID, publicKey); err != nil {
				return nil, protocol.WrapError(protocol.ErrCommitSignatureInvalid, protocol.CategoryTrust, "Transition Git SSH signature is invalid.", err)
			}
			facts, currentArchitecture, currentTaskbook, err := engine.verifyFacts(
				ctx, parentCommit, parentTree, parentState, commit, tree, message,
			)
			if err != nil {
				return nil, err
			}
			facts.SignerFingerprint, _ = cryptoutil.Fingerprint(publicKey)
			reduced, err = state.Reduce(parentState, message.Operation, message.Evidence, facts)
			if err != nil {
				return nil, protocol.WrapError(protocol.ErrReducerMismatch, protocol.CategoryProtocol, "Transition reducer rejected the commit.", err)
			}
			architecture, taskbook, err = engine.contractsForState(ctx, reduced)
			if err != nil {
				return nil, err
			}
			if currentArchitecture == nil {
				currentArchitecture = architecture
			}
			if err := engine.verifyWhitelist(ctx, parentTree, tree, parentState, commit, message, currentArchitecture, currentTaskbook); err != nil {
				return nil, err
			}
		}
		expectedBytes, err := state.Encode(reduced, format)
		if err != nil {
			return nil, err
		}
		if !bytes.Equal(expectedBytes, stateBytes) || !reflect.DeepEqual(reduced, parsedState) {
			return nil, protocol.NewError(protocol.ErrReducerMismatch, protocol.CategoryProtocol, "Reducer output does not equal the exact next State blob.")
		}
		engine.addKnownKeys(reduced)
		if architecture != nil {
			if engine.architectureID == "" {
				engine.architectureID = architecture.ID
			}
			for id := range architecture.Resources() {
				engine.seenResources[id] = struct{}{}
			}
		}
		if taskbook != nil {
			engine.seenTaskbookIDs[taskbook.ID] = struct{}{}
		}
		opDigest, _ := protocol.ObjectDigest("operation", message.Operation)
		if previous, exists := engine.seenOperations[message.Operation.OperationID]; exists {
			return nil, &protocol.Error{
				Code: protocol.ErrOperationIDCollision, Category: protocol.CategoryProtocol,
				Message: "Operation ID appears more than once in verified history.",
				Details: map[string]any{
					"current_digest": opDigest, "operation_id": message.Operation.OperationID,
					"previous_digest": previous,
				},
			}
		}
		engine.seenOperations[message.Operation.OperationID] = opDigest
		result.Transitions = append(result.Transitions, Transition{
			Action: message.Operation.Action, Commit: commit.OID,
			OperationDigest: opDigest, OperationID: message.Operation.OperationID,
			StateDigest: stateDigest, Target: message.Operation.Target,
		})
		parentState, parentCommit, parentTree = reduced, commit, tree
		result.State, result.StateDigest = reduced, stateDigest
		result.Architecture, result.Taskbook = architecture, taskbook
	}
	if options.ExpectedProject != "" && result.State.Project.ID != options.ExpectedProject {
		return nil, protocol.NewError(protocol.ErrProjectIDMismatch, protocol.CategoryTrust, "Verified Project ID does not match the trusted Project ID.")
	}
	if options.ExpectedRootFingerprint != "" && result.RootFingerprint != options.ExpectedRootFingerprint {
		return nil, protocol.NewError(protocol.ErrRootFingerprintMismatch, protocol.CategoryTrust, "Verified Root fingerprint does not match the trust anchor.")
	}
	if target == "refs/heads/main" || target == "refs/remotes/origin/main" {
		if err := verifyCurrentWorkRefs(ctx, runner, result.State); err != nil {
			return nil, err
		}
	}
	return result, nil
}

func verifyCurrentWorkRefs(ctx context.Context, runner gitstore.Runner, current *state.State) error {
	for taskID, task := range current.Tasks {
		if (task.Phase != "submitted" && task.Phase != "approved") || task.Attempt == nil {
			continue
		}
		newSuffix := "chassiss/work/" + taskID + "/" + task.Base[:12] + "/" + task.Actor
		legacySuffix := "chassiss/work/" + taskID + "/" + task.Actor
		refs := []string{
			"refs/heads/" + newSuffix,
			"refs/remotes/origin/" + newSuffix,
			"refs/heads/" + legacySuffix,
			"refs/remotes/origin/" + legacySuffix,
		}
		retained := false
		for _, ref := range refs {
			oid, err := runner.Resolve(ctx, ref)
			if err == nil && oid == task.Attempt.Head {
				retained = true
				break
			}
		}
		if !retained {
			return &protocol.Error{
				Code: protocol.ErrAttemptUnreachable, Category: protocol.CategoryProtocol,
				Message: "Current submitted or approved Attempt is not retained by its exact Work Ref.",
				Details: map[string]any{
					"expected_head": task.Attempt.Head,
					"refs":          refs,
					"task":          taskID,
				},
			}
		}
	}
	return nil
}

func firstParentChain(ctx context.Context, runner gitstore.Runner, target string) ([]gitstore.Commit, error) {
	reversed := make([]gitstore.Commit, 0)
	seen := make(map[string]struct{})
	current := target
	for len(reversed) < 100_000 {
		if _, exists := seen[current]; exists {
			return nil, fmt.Errorf("first-parent history contains a cycle")
		}
		seen[current] = struct{}{}
		commit, err := runner.ReadCommit(ctx, current)
		if err != nil {
			return nil, err
		}
		reversed = append(reversed, commit)
		if len(commit.Parents) == 0 {
			break
		}
		current = commit.Parents[0]
	}
	if len(reversed) == 100_000 {
		return nil, fmt.Errorf("first-parent history exceeds verifier limit")
	}
	chain := make([]gitstore.Commit, len(reversed))
	for index := range reversed {
		chain[len(reversed)-1-index] = reversed[index]
	}
	return chain, nil
}

func (engine verifier) readState(ctx context.Context, commit gitstore.Commit) (gitstore.TreeMap, []byte, *state.State, error) {
	tree, err := engine.runner.ReadTree(ctx, commit.Tree)
	if err != nil {
		return nil, nil, nil, err
	}
	entry, exists := tree[".chassiss/state.json"]
	if !exists || entry.Mode != "100644" {
		return nil, nil, nil, fmt.Errorf("Transition tree lacks regular .chassiss/state.json")
	}
	data, err := engine.runner.ReadBlob(ctx, entry.OID)
	if err != nil {
		return nil, nil, nil, err
	}
	parsed, err := state.Parse(data, engine.format)
	if err != nil {
		return nil, nil, nil, err
	}
	return tree, data, parsed, nil
}

func (engine verifier) verifyGenesis(
	ctx context.Context,
	commit gitstore.Commit,
	tree gitstore.TreeMap,
	parsed *state.State,
	message protocol.TransitionMessage,
	options Options,
) (*state.State, *contracts.Architecture, *contracts.Taskbook, error) {
	action := message.Operation.Action
	if (action != "project.genesis" && action != "project.bootstrap") || len(commit.Parents) != 0 ||
		message.Evidence.Parent != nil {
		return nil, nil, nil, protocol.NewError(protocol.ErrGenesisInvalid, protocol.CategoryTrust, "Genesis topology or Action is invalid.")
	}
	if stringFact(message.Evidence.Facts, "initial_tree") != commit.Tree {
		return nil, nil, nil, protocol.NewError(protocol.ErrGenesisInvalid, protocol.CategoryProtocol, "Genesis Evidence does not bind the exact initial tree.")
	}
	if err := engine.runner.VerifyCommitSSH(ctx, commit.OID, parsed.Authority.Root.PublicKey); err != nil {
		return nil, nil, nil, protocol.WrapError(protocol.ErrCommitSignatureInvalid, protocol.CategoryTrust, "Genesis Root self-signature is invalid.", err)
	}
	architecture, taskbook, err := engine.contractsForState(ctx, parsed)
	if err != nil {
		return nil, nil, nil, err
	}
	if action == "project.genesis" && taskbook == nil {
		return nil, nil, nil, protocol.NewError(protocol.ErrGenesisInvalid, protocol.CategoryProtocol, "Genesis requires an initial Taskbook.")
	}
	if action == "project.bootstrap" {
		if architecture != nil || taskbook != nil || parsed.Project.Source == nil {
			return nil, nil, nil, protocol.NewError(protocol.ErrGenesisInvalid, protocol.CategoryProtocol, "Source bootstrap cannot contain Architecture or Taskbook.")
		}
		source := parsed.Project.Source
		entry, exists := tree[source.HistoryPath]
		if !exists || entry.Mode != "100644" || entry.OID != source.HistoryBlob {
			return nil, nil, nil, protocol.NewError(protocol.ErrGenesisInvalid, protocol.CategoryProtocol, "Source bootstrap history document is missing or does not match State.")
		}
	}
	ready := make([]string, 0)
	taskbookID := ""
	if taskbook != nil {
		taskbookID = taskbook.ID
		ready = make([]string, 0, len(taskbook.Tasks))
		for id := range taskbook.Tasks {
			ready = append(ready, id)
		}
	}
	sort.Strings(ready)
	reduced, err := state.Reduce(nil, message.Operation, message.Evidence, state.ReduceFacts{
		ObjectFormat: engine.format, GenesisTaskbookID: taskbookID, GenesisReadyTasks: ready,
	})
	if err != nil {
		return nil, nil, nil, err
	}
	if options.ExpectedProject != "" && reduced.Project.ID != options.ExpectedProject {
		return nil, nil, nil, protocol.NewError(protocol.ErrProjectIDMismatch, protocol.CategoryTrust, "Genesis Project ID does not match the trust anchor.")
	}
	fingerprint, _ := cryptoutil.Fingerprint(reduced.Authority.Root.PublicKey)
	if options.ExpectedRootFingerprint != "" && fingerprint != options.ExpectedRootFingerprint {
		return nil, nil, nil, protocol.NewError(protocol.ErrRootFingerprintMismatch, protocol.CategoryTrust, "Genesis Root fingerprint does not match the trust anchor.")
	}
	return reduced, architecture, taskbook, nil
}

func (engine verifier) contractsForState(ctx context.Context, current *state.State) (*contracts.Architecture, *contracts.Taskbook, error) {
	if current.Project.Architecture == nil {
		if current.Project.Taskbook != nil || len(current.Tasks) != 0 {
			return nil, nil, fmt.Errorf("bootstrap State cannot contain Taskbook data")
		}
		return nil, nil, nil
	}
	architectureData, err := engine.runner.ReadBlob(ctx, current.Project.Architecture.BlobOID)
	if err != nil {
		return nil, nil, err
	}
	architecture, err := contracts.ParseArchitecture(architectureData)
	if err != nil {
		return nil, nil, err
	}
	if current.Project.Taskbook == nil {
		return architecture, nil, nil
	}
	taskbookData, err := engine.runner.ReadBlob(ctx, current.Project.Taskbook.BlobOID)
	if err != nil {
		return nil, nil, err
	}
	taskbook, err := contracts.ParseTaskbook(taskbookData, architecture)
	if err != nil {
		return nil, nil, err
	}
	if taskbook.ID != current.Project.Taskbook.ID {
		return nil, nil, fmt.Errorf("State Taskbook ID does not match the Taskbook blob")
	}
	if len(taskbook.Tasks) != len(current.Tasks) {
		return nil, nil, fmt.Errorf("State does not project every active Taskbook Task")
	}
	for id := range taskbook.Tasks {
		if _, exists := current.Tasks[id]; !exists {
			return nil, nil, fmt.Errorf("State lacks Taskbook Task %s", id)
		}
	}
	return architecture, taskbook, nil
}

func authorityPublicKey(parent *state.State, authority string) (string, error) {
	if strings.HasPrefix(authority, "root:") {
		if strings.TrimPrefix(authority, "root:") != parent.Authority.Root.KeyID {
			return "", protocol.NewError(protocol.ErrKeyMismatch, protocol.CategoryAuthorization, "Transition selects a non-current Root key.")
		}
		return parent.Authority.Root.PublicKey, nil
	}
	grantID := strings.TrimPrefix(authority, "grant:")
	grant, exists := parent.Authority.Grants[grantID]
	if !exists {
		return "", protocol.NewError(protocol.ErrGrantNotFound, protocol.CategoryAuthorization, "Transition selects a non-current Grant.")
	}
	return grant.PublicKey, nil
}

func (engine *verifier) addKnownKeys(current *state.State) {
	for _, grant := range current.Authority.Grants {
		if engine.knownKeys[grant.Actor] == nil {
			engine.knownKeys[grant.Actor] = map[string]struct{}{}
		}
		engine.knownKeys[grant.Actor][grant.PublicKey] = struct{}{}
	}
}

func stringFact(object map[string]any, key string) string {
	value, _ := object[key].(string)
	return value
}
