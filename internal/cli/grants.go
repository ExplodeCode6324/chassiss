package cli

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strconv"

	"github.com/ExplodeCode6324/chassiss/internal/authority"
	"github.com/ExplodeCode6324/chassiss/internal/cryptoutil"
	"github.com/ExplodeCode6324/chassiss/internal/localstate"
	"github.com/ExplodeCode6324/chassiss/internal/protocol"
	"github.com/ExplodeCode6324/chassiss/internal/state"
)

func grantCommand(ctx context.Context, invocation invocation) (Envelope, error) {
	switch invocation.Definition.Path {
	case "grant request":
		return grantRequestCommand(invocation)
	case "grant list", "grant show":
		return grantReadCommand(ctx, invocation)
	case "grant add":
		return grantAddCommand(ctx, invocation)
	case "grant revoke":
		return grantRevokeCommand(ctx, invocation)
	default:
		panic("unreachable")
	}
}

func grantRequestCommand(invocation invocation) (Envelope, error) {
	projectID := invocation.Value("project")
	if err := protocol.ValidateID(protocol.IDProject, projectID); err != nil {
		return Envelope{}, usageError(err.Error())
	}
	store, err := localstate.OpenDefault()
	if err != nil {
		return Envelope{}, err
	}
	keyID := invocation.Value("key")
	if err := protocol.ValidateID(protocol.IDKey, keyID); err != nil {
		return Envelope{}, usageError(err.Error())
	}
	handle, err := resolveKeyHandle(keyID, store.Paths)
	if err != nil {
		return Envelope{}, protocol.WrapError(protocol.ErrSecretKeyNotFound, protocol.CategoryLocal, "Request key does not exist.", err)
	}
	public, fingerprint, err := cryptoutil.PublicFromHandle(handle)
	if err != nil {
		return Envelope{}, err
	}
	metadata, err := readKeyMetadata(store.Paths, keyID)
	if err != nil {
		return Envelope{}, protocol.WrapError(protocol.ErrLocalStateCorrupt, protocol.CategoryLocal, "Request key actor metadata is missing.", err)
	}
	limits, err := limitsFromInvocation(invocation)
	if err != nil {
		return Envelope{}, err
	}
	capabilities, limits, err := authority.ApplyProfile(invocation.Value("profile"), invocation.Values["capability"], limits)
	if err != nil {
		return Envelope{}, usageError(err.Error())
	}
	request, err := authority.NewRequest(
		projectID, metadata.Actor, keyID, public, handle, capabilities,
		state.Scope{
			Tasks: invocation.Values["task-scope"], Resources: invocation.Values["resource-scope"],
		},
		limits,
	)
	if err != nil {
		return Envelope{}, protocol.WrapError(protocol.ErrSchemaInvalid, protocol.CategoryValidation, "Grant Request is invalid.", err)
	}
	data, err := authority.EncodeRequest(*request)
	if err != nil {
		return Envelope{}, err
	}
	output := invocation.Value("output")
	local, err := store.Load()
	if err != nil {
		return Envelope{}, err
	}
	projectIDs := make([]string, 0, len(local.Projects))
	for registeredProjectID := range local.Projects {
		projectIDs = append(projectIDs, registeredProjectID)
	}
	sort.Strings(projectIDs)
	for _, registeredProjectID := range projectIDs {
		if err := requireOutsideRegisteredProject(local.Projects[registeredProjectID], output); err != nil {
			return Envelope{}, err
		}
	}
	if err := writeExternalFile(output, data); err != nil {
		return Envelope{}, err
	}
	digest, _ := request.Digest()
	envelope := baseEnvelope("grant request")
	envelope.Result = map[string]any{
		"actor": metadata.Actor, "fingerprint": fingerprint, "key_id": keyID,
		"output": output, "request_digest": digest,
	}
	return envelope, nil
}

func grantReadCommand(ctx context.Context, invocation invocation) (Envelope, error) {
	project, err := loadProject(ctx, "", false)
	if err != nil {
		return Envelope{}, err
	}
	envelope := projectEnvelope(invocation.Definition.Path, project)
	if invocation.Definition.Path == "grant show" {
		id := invocation.Positionals[0]
		grant, exists := project.Verified.State.Authority.Grants[id]
		if !exists {
			return Envelope{}, protocol.NewError(protocol.ErrGrantNotFound, protocol.CategoryAuthorization, "Grant is not current.")
		}
		envelope.Result = map[string]any{"grant": grant, "grant_id": id}
		return envelope, nil
	}
	ids := make([]string, 0)
	for id, grant := range project.Verified.State.Authority.Grants {
		if actor := invocation.Value("actor"); actor == "" || actor == grant.Actor {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	grants := make([]map[string]any, 0, len(ids))
	for _, id := range ids {
		grants = append(grants, map[string]any{"grant": project.Verified.State.Authority.Grants[id], "grant_id": id})
	}
	envelope.Result = map[string]any{"grants": grants}
	return envelope, nil
}

func grantAddCommand(ctx context.Context, invocation invocation) (Envelope, error) {
	project, err := loadMutableProject(ctx)
	if err != nil {
		return Envelope{}, err
	}
	requestData, err := os.ReadFile(invocation.Value("request"))
	if err != nil {
		return Envelope{}, protocol.WrapError(protocol.ErrPathNotFound, protocol.CategoryLocal, "Cannot read Grant Request.", err)
	}
	request, err := authority.DecodeRequest(requestData)
	if err != nil {
		return Envelope{}, protocol.WrapError(protocol.ErrSchemaInvalid, protocol.CategoryValidation, "Grant Request proof or schema is invalid.", err)
	}
	if request.ProjectID != project.Verified.State.Project.ID {
		return Envelope{}, protocol.NewError(protocol.ErrProjectIDMismatch, protocol.CategoryValidation, "Grant Request targets a different Project.")
	}
	grantID := invocation.Value("grant-id")
	if err := protocol.ValidateID(protocol.IDGrant, grantID); err != nil {
		return Envelope{}, usageError(err.Error())
	}
	if _, exists := project.Verified.State.Authority.Grants[grantID]; exists {
		return Envelope{}, protocol.NewError(protocol.ErrOperationInvalid, protocol.CategoryValidation, "Grant ID is already current.")
	}
	if len(invocation.Values["capability"]) == 0 && invocation.Value("profile") == "" {
		return Envelope{}, usageError("grant add requires explicit --capability or --profile approval")
	}
	if len(invocation.Values["task-scope"]) == 0 || len(invocation.Values["resource-scope"]) == 0 ||
		invocation.Value("limits") == "" {
		return Envelope{}, usageError("grant add requires explicit Task scope, Resource scope, and limits")
	}
	limits, err := limitsFromInvocation(invocation)
	if err != nil {
		return Envelope{}, err
	}
	capabilities, limits, err := authority.ApplyProfile(invocation.Value("profile"), invocation.Values["capability"], limits)
	if err != nil {
		return Envelope{}, usageError(err.Error())
	}
	sort.Strings(capabilities)
	taskScope := sortedCopy(invocation.Values["task-scope"])
	resourceScope := sortedCopy(invocation.Values["resource-scope"])
	grant := state.Grant{
		Actor: request.Actor, Capabilities: capabilities, KeyID: request.KeyID,
		Limits: limits, PublicKey: request.PublicKey,
		Scope: state.Scope{Tasks: taskScope, Resources: resourceScope},
	}
	if err := grant.Validate(); err != nil {
		return Envelope{}, protocol.WrapError(protocol.ErrSchemaInvalid, protocol.CategoryValidation, "Approved Grant is invalid.", err)
	}
	root, err := selectRoot(project, invocation.Value("root-key"))
	if err != nil {
		return Envelope{}, err
	}
	requestDigest, _ := request.Digest()
	grantObject, err := objectMap(grant)
	if err != nil {
		return Envelope{}, err
	}
	operationID, err := operationID(invocation)
	if err != nil {
		return Envelope{}, err
	}
	operation := protocol.Operation{
		Schema: protocol.OperationSchema, OperationID: operationID,
		Action: "authority.grant-added", Project: project.Verified.State.Project.ID,
		Authority: root.Reference, Target: grantID,
		Preconditions: map[string]any{
			"grant_absent": true, "root_key_id": project.Verified.State.Authority.Root.KeyID,
		},
		Payload: map[string]any{
			"grant": grantObject, "grant_id": grantID, "request_digest": requestDigest,
		},
	}
	return publishAuthorityTransition(ctx, project, root, operation, map[string]any{}, map[string]any{"grant_id": grantID}, invocation.Value("proposal"))
}

func grantRevokeCommand(ctx context.Context, invocation invocation) (Envelope, error) {
	project, err := loadMutableProject(ctx)
	if err != nil {
		return Envelope{}, err
	}
	grantID := invocation.Positionals[0]
	if _, exists := project.Verified.State.Authority.Grants[grantID]; !exists {
		return Envelope{}, protocol.NewError(protocol.ErrGrantNotFound, protocol.CategoryAuthorization, "Grant is not current.")
	}
	root, err := selectRoot(project, invocation.Value("root-key"))
	if err != nil {
		return Envelope{}, err
	}
	operationID, err := operationID(invocation)
	if err != nil {
		return Envelope{}, err
	}
	operation := protocol.Operation{
		Schema: protocol.OperationSchema, OperationID: operationID,
		Action: "authority.grant-revoked", Project: project.Verified.State.Project.ID,
		Authority: root.Reference, Target: grantID,
		Preconditions: map[string]any{
			"grant_id": grantID, "root_key_id": project.Verified.State.Authority.Root.KeyID,
		},
		Payload: map[string]any{"grant_id": grantID, "reason": invocation.Value("reason")},
	}
	return publishAuthorityTransition(ctx, project, root, operation, map[string]any{}, map[string]any{"grant_id": grantID}, invocation.Value("proposal"))
}

func publishAuthorityTransition(ctx context.Context, project *projectContext, root selectedAuthority, operation protocol.Operation, evidenceFacts map[string]any, result map[string]any, proposal string) (Envelope, error) {
	operationDigest, _ := protocol.ObjectDigest("operation", operation)
	parent := project.Verified.Head
	evidence := protocol.ExecutionEvidence{
		Schema: protocol.EvidenceSchema, OperationDigest: operationDigest,
		Action: operation.Action, Attempt: 1, Parent: &parent, Facts: evidenceFacts,
	}
	reduceFacts := state.ReduceFacts{
		ObjectFormat: project.Verified.ObjectFormat, ParentCommit: parent,
		SignerFingerprint: root.Fingerprint,
	}
	next, err := state.Reduce(project.Verified.State, operation, evidence, reduceFacts)
	if err != nil {
		return Envelope{}, err
	}
	tree, err := currentTree(ctx, project)
	if err != nil {
		return Envelope{}, err
	}
	plan := transitionPlan{
		Operation: operation, Evidence: evidence, NextState: next,
		Tree: tree, ReduceFacts: reduceFacts, Authority: root, Result: result,
	}
	if proposal != "" {
		return createProposal(ctx, project, plan, proposal)
	}
	return publishTransition(ctx, project, plan)
}

func limitsFromInvocation(invocation invocation) (state.Limits, error) {
	limits := state.Limits{Mode: invocation.Value("limits")}
	if limits.Mode != "bounded" && limits.Mode != "unbounded" {
		return state.Limits{}, usageError("--limits must be bounded or unbounded")
	}
	parse := func(name string) (*int64, error) {
		if invocation.Value(name) == "" {
			return nil, nil
		}
		value, err := strconv.ParseInt(invocation.Value(name), 10, 64)
		if err != nil || value < 1 || value > 2147483647 {
			return nil, usageError("--" + name + " must be in [1,2147483647]")
		}
		return &value, nil
	}
	var err error
	limits.MaxActiveTasks, err = parse("max-active-tasks")
	if err != nil {
		return state.Limits{}, err
	}
	limits.MaxChangedPaths, err = parse("max-changed-paths")
	if err != nil {
		return state.Limits{}, err
	}
	if limits.Mode == "unbounded" && (limits.MaxActiveTasks != nil || limits.MaxChangedPaths != nil) {
		return state.Limits{}, usageError("unbounded limits cannot include numeric limits")
	}
	if limits.Mode == "bounded" && limits.MaxActiveTasks == nil && limits.MaxChangedPaths == nil &&
		invocation.Value("profile") != "developer" {
		return state.Limits{}, usageError("bounded limits require at least one numeric limit")
	}
	return limits, nil
}

func objectMap(value any) (map[string]any, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var result map[string]any
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func sortedCopy(values []string) []string {
	result := append([]string(nil), values...)
	sort.Strings(result)
	return result
}

func writeExternalFile(path string, data []byte) error {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(absolute), 0o700); err != nil {
		return err
	}
	file, err := os.OpenFile(absolute, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	if _, err := file.Write(data); err != nil {
		file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return err
	}
	return file.Close()
}
