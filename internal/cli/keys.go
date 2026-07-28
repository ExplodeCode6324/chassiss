package cli

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	goruntime "runtime"
	"sort"
	"strings"

	"github.com/ExplodeCode6324/chassiss/internal/cryptoutil"
	"github.com/ExplodeCode6324/chassiss/internal/gitstore"
	"github.com/ExplodeCode6324/chassiss/internal/localstate"
	"github.com/ExplodeCode6324/chassiss/internal/protocol"
	"github.com/ExplodeCode6324/chassiss/internal/verifier"
)

func keyCommand(ctx context.Context, invocation invocation) (Envelope, error) {
	store, err := localstate.OpenDefault()
	if err != nil {
		return Envelope{}, err
	}
	switch invocation.Definition.Path {
	case "key generate":
		keyID := invocation.Value("id")
		if err := protocol.ValidateID(protocol.IDKey, keyID); err != nil {
			return Envelope{}, usageError(err.Error())
		}
		if err := protocol.ValidateActor(invocation.Value("actor")); err != nil {
			return Envelope{}, usageError(err.Error())
		}
		if kind := invocation.Value("store"); kind != "" && kind != "file" {
			return Envelope{}, protocol.NewError(protocol.ErrFeatureNotInV1, protocol.CategoryUnsupported, "This build supports the owner-only local file secret store.")
		}
		generated, err := cryptoutil.GenerateFileKey(store.Paths.Keys, keyID)
		if err != nil {
			return Envelope{}, protocol.WrapError(protocol.ErrLocalStateCorrupt, protocol.CategoryLocal, "Cannot generate local key.", err)
		}
		metadata := keyMetadata{Actor: invocation.Value("actor"), KeyID: keyID}
		metadataData, _ := json.Marshal(metadata)
		metadataPath := filepath.Join(store.Paths.Keys, keyID+".meta.json")
		if err := writeNewOwnerOnly(metadataPath, append(metadataData, '\n')); err != nil {
			if keyPath, resolveErr := cryptoutil.ResolveFileHandle(generated.Handle); resolveErr == nil {
				_ = os.Remove(keyPath)
			}
			return Envelope{}, err
		}
		// If invoked inside a registered Project, register only the opaque
		// handle. No Grant identity is cached.
		if project, loadErr := loadProject(ctx, "", false); loadErr == nil {
			_ = project.Store.Update(func(local *localstate.State) error {
				value := local.Projects[project.Verified.State.Project.ID]
				value.Identity.Keys[keyID] = localstate.Key{PrivateKeyHandle: generated.Handle}
				if value.Identity.SelectedKeyID == "" {
					value.Identity.SelectedKeyID = keyID
				}
				local.Projects[project.Verified.State.Project.ID] = value
				return nil
			})
		}
		envelope := baseEnvelope("key generate")
		envelope.Result = map[string]any{
			"actor": invocation.Value("actor"), "fingerprint": generated.Fingerprint,
			"key_id": keyID, "public_key": generated.PublicKey, "store": "file",
		}
		return envelope, nil
	case "key list":
		entries, err := os.ReadDir(store.Paths.Keys)
		if os.IsNotExist(err) {
			entries = nil
		} else if err != nil {
			return Envelope{}, err
		}
		keys := make([]map[string]any, 0)
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasPrefix(entry.Name(), "KEY-") || strings.HasSuffix(entry.Name(), ".meta.json") {
				continue
			}
			handle := cryptoutil.FileHandlePrefix + filepath.Join(store.Paths.Keys, entry.Name())
			public, fingerprint, err := cryptoutil.PublicFromHandle(handle)
			if err != nil {
				continue
			}
			metadata, _ := readKeyMetadata(store.Paths, entry.Name())
			keys = append(keys, map[string]any{
				"actor": metadata.Actor, "fingerprint": fingerprint,
				"key_id": entry.Name(), "public_key": public,
			})
		}
		sort.Slice(keys, func(i, j int) bool { return keys[i]["key_id"].(string) < keys[j]["key_id"].(string) })
		envelope := baseEnvelope("key list")
		envelope.Result = map[string]any{"keys": keys}
		return envelope, nil
	case "key show":
		keyID := invocation.Positionals[0]
		handle, err := resolveKeyHandle(keyID, store.Paths)
		if err != nil {
			return Envelope{}, protocol.WrapError(protocol.ErrSecretKeyNotFound, protocol.CategoryLocal, "Local key does not exist.", err)
		}
		public, fingerprint, err := cryptoutil.PublicFromHandle(handle)
		if err != nil {
			return Envelope{}, err
		}
		envelope := baseEnvelope("key show")
		metadata, _ := readKeyMetadata(store.Paths, keyID)
		envelope.Result = map[string]any{"actor": metadata.Actor, "fingerprint": fingerprint, "key_id": keyID, "public_key": public}
		return envelope, nil
	case "key remove":
		if !invocation.Flags["yes"] {
			return Envelope{}, protocol.NewError(protocol.ErrUsageInvalid, protocol.CategoryUsage, "key remove requires --yes after exact key inspection.")
		}
		keyID := invocation.Positionals[0]
		local, err := store.Load()
		if err != nil {
			return Envelope{}, err
		}
		for projectID, project := range local.Projects {
			verified, verifyErr := verifiedProjectAtAnyInstance(ctx, project)
			if verifyErr != nil {
				if _, registered := project.Identity.Keys[keyID]; registered {
					return Envelope{}, protocol.WrapError(protocol.ErrLocalStateCorrupt, protocol.CategoryLocal, "Cannot prove that the registered key is safe to remove.", verifyErr)
				}
				continue
			}
			if keyID == verified.State.Authority.Root.KeyID {
				return Envelope{}, &protocol.Error{
					Code: protocol.ErrCapabilityDenied, Category: protocol.CategoryAuthorization,
					Message: "A current Project Root key cannot be removed by the v1 CLI.",
					Details: map[string]any{"project": projectID},
				}
			}
			for grantID, grant := range verified.State.Authority.Grants {
				if grant.KeyID == keyID && !invocation.Flags["orphan-grant"] {
					return Envelope{}, &protocol.Error{
						Code: protocol.ErrCapabilityDenied, Category: protocol.CategoryAuthorization,
						Message: "A key referenced by a current Grant requires --orphan-grant for removal.",
						Details: map[string]any{"grant_id": grantID, "project": projectID},
					}
				}
			}
		}
		handle, err := resolveKeyHandle(keyID, store.Paths)
		if err != nil {
			return Envelope{}, protocol.WrapError(protocol.ErrSecretKeyNotFound, protocol.CategoryLocal, "Local key does not exist.", err)
		}
		path, _ := cryptoutil.ResolveFileHandle(handle)
		staged, err := stageKeyRemoval(path)
		if err != nil {
			return Envelope{}, err
		}
		metadataPath := filepath.Join(store.Paths.Keys, keyID+".meta.json")
		stagedMetadata, metadataErr := stageOptionalRemoval(metadataPath)
		if metadataErr != nil {
			_ = os.Rename(staged, path)
			return Envelope{}, metadataErr
		}
		if err := store.Update(func(local *localstate.State) error {
			for projectID, project := range local.Projects {
				delete(project.Identity.Keys, keyID)
				if project.Identity.SelectedKeyID == keyID {
					project.Identity.SelectedKeyID = ""
				}
				local.Projects[projectID] = project
			}
			return nil
		}); err != nil {
			_ = os.Rename(staged, path)
			if stagedMetadata != "" {
				_ = os.Rename(stagedMetadata, metadataPath)
			}
			return Envelope{}, err
		}
		if err := os.Remove(staged); err != nil {
			return Envelope{}, err
		}
		if stagedMetadata != "" {
			if err := os.Remove(stagedMetadata); err != nil {
				return Envelope{}, err
			}
		}
		envelope := baseEnvelope("key remove")
		envelope.Result = map[string]any{"key_id": keyID, "removed": true}
		return envelope, nil
	default:
		panic("unreachable")
	}
}

type keyMetadata struct {
	Actor string `json:"actor"`
	KeyID string `json:"key_id"`
}

func readKeyMetadata(paths localstate.Paths, keyID string) (keyMetadata, error) {
	data, err := os.ReadFile(filepath.Join(paths.Keys, keyID+".meta.json"))
	if err != nil {
		return keyMetadata{}, err
	}
	var metadata keyMetadata
	if err := json.Unmarshal(data, &metadata); err != nil {
		return keyMetadata{}, err
	}
	if metadata.KeyID != keyID {
		return keyMetadata{}, protocol.NewError(protocol.ErrLocalStateCorrupt, protocol.CategoryLocal, "Key metadata ID does not match its filename.")
	}
	return metadata, nil
}

func writeNewOwnerOnly(path string, data []byte) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		_ = os.Remove(path)
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		_ = os.Remove(path)
		return err
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(path)
		return err
	}
	if goruntime.GOOS == "windows" {
		return nil
	}
	directory, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}

func stageKeyRemoval(path string) (string, error) {
	file, err := os.CreateTemp(filepath.Dir(path), ".chassiss-key-remove-")
	if err != nil {
		return "", err
	}
	staged := file.Name()
	if err := file.Close(); err != nil {
		_ = os.Remove(staged)
		return "", err
	}
	if err := os.Remove(staged); err != nil {
		return "", err
	}
	if err := os.Rename(path, staged); err != nil {
		return "", err
	}
	return staged, nil
}

func stageOptionalRemoval(path string) (string, error) {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return "", nil
	} else if err != nil {
		return "", err
	}
	return stageKeyRemoval(path)
}

func verifiedProjectAtAnyInstance(ctx context.Context, project localstate.Project) (*verifier.Result, error) {
	var lastErr error
	for _, instance := range project.RepoInstances {
		runner := gitstore.New(instance.RepoPath)
		result, err := verifier.Verify(ctx, runner, "refs/heads/main", verifier.Options{
			ExpectedRootFingerprint: project.RootFingerprint,
			MinimumCheckpoint:       project.MinimumCheckpoint.Commit,
		})
		if err == nil {
			return result, nil
		}
		lastErr = err
	}
	if lastErr == nil {
		lastErr = protocol.NewError(protocol.ErrProjectNotFound, protocol.CategoryLocal, "Project has no registered repository instance.")
	}
	return nil, lastErr
}
