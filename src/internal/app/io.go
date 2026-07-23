package app

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"time"

	"gopkg.in/yaml.v3"
)

func newID(prefix string) (string, error) {
	data := make([]byte, 12)
	if _, err := rand.Read(data); err != nil {
		return "", err
	}
	return prefix + "-" + hex.EncodeToString(data), nil
}

func digestBytes(data []byte) string {
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// canonicalJSON is the frozen Event V4 / Trust V1 signing codec. Changing its
// byte output requires a new protocol version and a version-selective verifier.
func canonicalJSON(value any) ([]byte, error) {
	return json.Marshal(value)
}

func loadYAML(path string, output any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := yaml.Unmarshal(data, output); err != nil {
		return fmt.Errorf("parse %s: %w", path, err)
	}
	return nil
}

func writeYAMLAtomic(path string, value any, mode os.FileMode) error {
	data, err := yaml.Marshal(value)
	if err != nil {
		return err
	}
	return writeAtomic(path, data, mode)
}

func writeJSONAtomic(path string, value any, mode os.FileMode) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return writeAtomic(path, data, mode)
}

func writeAtomic(path string, data []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(path), ".chassiss-write-*")
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
	if err := os.Rename(tempPath, path); err != nil {
		return err
	}
	directory, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}

func writeEventAtomic(eventsDir string, event Event) error {
	if event.Sequence < 1 || event.ID == "" {
		return &CLIError{Code: "CHS-INTEGRITY-EVENTS", Message: "event file identity is invalid", ExitCode: 40}
	}
	name := fmt.Sprintf("%020d-%s.json", event.Sequence, event.ID)
	path := filepath.Join(eventsDir, name)
	if _, err := os.Stat(path); err == nil {
		return &CLIError{Code: "CHS-INTEGRITY-EVENTS", Message: "event file already exists", ExitCode: 40}
	} else if !os.IsNotExist(err) {
		return err
	}
	return writeJSONAtomic(path, event, 0o644)
}

func readEvents(path string) ([]Event, error) {
	entries, err := os.ReadDir(path)
	if os.IsNotExist(err) {
		return []Event{}, nil
	}
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		names = append(names, entry.Name())
	}
	sort.Strings(names)
	var events []Event
	for _, name := range names {
		data, err := os.ReadFile(filepath.Join(path, name))
		if err != nil {
			return nil, err
		}
		var event Event
		decoder := json.NewDecoder(bytes.NewReader(data))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&event); err != nil {
			return nil, fmt.Errorf("parse event %s: %w", name, err)
		}
		if err := decoder.Decode(&struct{}{}); err != io.EOF {
			return nil, fmt.Errorf("parse event %s: trailing data", name)
		}
		events = append(events, event)
	}
	return events, nil
}

type projectLock struct {
	path string
	file *os.File
}

func acquireLock(root string) (*projectLock, error) {
	path := filepath.Join(root, ".chassis", "lock")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	locked, err := tryAdvisoryLock(file)
	if err != nil {
		_ = file.Close()
		return nil, err
	}
	if !locked {
		_ = file.Close()
		return nil, &CLIError{Code: "CHS-CONFLICT-LOCKED", Message: "project has an active write lock", ExitCode: 12, Retryable: true, Remedy: []string{"retry after the active command completes", "run chassiss doctor if the writer crashed"}}
	}
	if err := file.Truncate(0); err != nil {
		_ = unlockAdvisoryLock(file)
		_ = file.Close()
		return nil, err
	}
	if _, err := file.Seek(0, 0); err != nil {
		_ = unlockAdvisoryLock(file)
		_ = file.Close()
		return nil, err
	}
	if _, err := fmt.Fprintf(file, "pid=%d\nacquired_at=%s\n", os.Getpid(), time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		_ = unlockAdvisoryLock(file)
		_ = file.Close()
		return nil, err
	}
	if err := file.Sync(); err != nil {
		_ = unlockAdvisoryLock(file)
		_ = file.Close()
		return nil, err
	}
	return &projectLock{path: path, file: file}, nil
}

func (lock *projectLock) release() {
	if lock == nil || lock.file == nil {
		return
	}
	_ = unlockAdvisoryLock(lock.file)
	_ = lock.file.Close()
	lock.file = nil
}

func copyStream(dst io.Writer, src io.Reader) error {
	_, err := io.Copy(dst, src)
	return err
}
