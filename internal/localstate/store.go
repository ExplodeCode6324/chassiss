package localstate

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
)

var storeWriteMutex sync.Mutex

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
	return store.loadUnlocked()
}

func (store Store) loadUnlocked() (*State, error) {
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
	return store.withExclusiveWriteLock(func() error {
		return store.saveUnlocked(state)
	})
}

func (store Store) saveUnlocked(state *State) error {
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
	return store.withExclusiveWriteLock(func() error {
		state, err := store.loadUnlocked()
		if err != nil {
			return err
		}
		if err := mutator(state); err != nil {
			return err
		}
		return store.saveUnlocked(state)
	})
}

func (store Store) withExclusiveWriteLock(action func() error) error {
	storeWriteMutex.Lock()
	defer storeWriteMutex.Unlock()

	if err := os.MkdirAll(store.Paths.Data, 0o700); err != nil {
		return err
	}
	if err := os.Chmod(store.Paths.Data, 0o700); err != nil {
		return err
	}
	lockPath := filepath.Join(store.Paths.Data, "local-state.lock")
	lock, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	if err := lock.Chmod(0o600); err != nil {
		_ = lock.Close()
		return err
	}
	if err := acquireStoreFileLock(lock); err != nil {
		_ = lock.Close()
		return fmt.Errorf("lock local State: %w", err)
	}
	actionErr := action()
	unlockErr := releaseStoreFileLock(lock)
	closeErr := lock.Close()
	if actionErr != nil {
		return actionErr
	}
	if unlockErr != nil {
		return fmt.Errorf("unlock local State: %w", unlockErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close local State lock: %w", closeErr)
	}
	return nil
}
