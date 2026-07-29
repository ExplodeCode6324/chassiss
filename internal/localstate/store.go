package localstate

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

type Store struct {
	Paths Paths
}

func OpenDefault() (Store, error) {
	paths, err := DefaultPaths()
	if err != nil {
		return Store{}, err
	}
	return Store{Paths: paths}, nil
}

func (store Store) Load() (*State, error) {
	data, err := os.ReadFile(store.Paths.State)
	if os.IsNotExist(err) {
		return New(), nil
	}
	if err != nil {
		return nil, err
	}
	var state State
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&state); err != nil {
		return nil, fmt.Errorf("local State: %w", err)
	}
	if err := state.Validate(); err != nil {
		return nil, err
	}
	return &state, nil
}

func (store Store) Save(state *State) error {
	if err := state.Validate(); err != nil {
		return err
	}
	if err := os.MkdirAll(store.Paths.Data, 0o700); err != nil {
		return err
	}
	if err := os.Chmod(store.Paths.Data, 0o700); err != nil {
		return err
	}
	// Marshal without indentation so the canonical JSON bytes held by
	// PendingOperation RawMessages are not rewritten by encoding/json.
	data, err := json.Marshal(state)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	file, err := os.CreateTemp(store.Paths.Data, "local-state-")
	if err != nil {
		return err
	}
	tempPath := file.Name()
	defer os.Remove(tempPath)
	if err := file.Chmod(0o600); err != nil {
		file.Close()
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
	if err := file.Close(); err != nil {
		return err
	}
	if err := os.Rename(tempPath, store.Paths.State); err != nil {
		return err
	}
	if runtime.GOOS == "windows" {
		return nil
	}
	directory, err := os.Open(filepath.Dir(store.Paths.State))
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}

func (store Store) Update(mutator func(*State) error) error {
	state, err := store.Load()
	if err != nil {
		return err
	}
	if err := mutator(state); err != nil {
		return err
	}
	return store.Save(state)
}
