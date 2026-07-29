package localstate

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

type Paths struct {
	Data    string
	Cache   string
	Runtime string
	Keys    string
	State   string
}

func DefaultPaths() (Paths, error) {
	if override := os.Getenv("CHASSISS_DATA_DIR"); override != "" {
		if !filepath.IsAbs(override) {
			return Paths{}, fmt.Errorf("CHASSISS_DATA_DIR must be absolute")
		}
		return pathsFromData(override, cacheOverride(override)), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return Paths{}, err
	}
	switch runtime.GOOS {
	case "darwin":
		data := filepath.Join(home, "Library", "Application Support", "CHASSISS")
		cache := filepath.Join(home, "Library", "Caches", "CHASSISS")
		return pathsFromData(data, cache), nil
	case "windows":
		base := os.Getenv("LOCALAPPDATA")
		if base == "" {
			base = filepath.Join(home, "AppData", "Local")
		}
		data := filepath.Join(base, "CHASSISS", "Data")
		cache := filepath.Join(base, "CHASSISS", "Cache")
		return pathsFromData(data, cache), nil
	default:
		dataBase := os.Getenv("XDG_DATA_HOME")
		if dataBase == "" {
			dataBase = filepath.Join(home, ".local", "share")
		}
		cacheBase := os.Getenv("XDG_CACHE_HOME")
		if cacheBase == "" {
			cacheBase = filepath.Join(home, ".cache")
		}
		paths := pathsFromData(filepath.Join(dataBase, "chassiss"), filepath.Join(cacheBase, "chassiss"))
		if runtimeBase := os.Getenv("XDG_RUNTIME_DIR"); runtimeBase != "" {
			paths.Runtime = filepath.Join(runtimeBase, "chassiss")
		}
		return paths, nil
	}
}

func cacheOverride(data string) string {
	if value := os.Getenv("CHASSISS_CACHE_DIR"); value != "" && filepath.IsAbs(value) {
		return value
	}
	return filepath.Join(data, "cache")
}

func pathsFromData(data, cache string) Paths {
	return Paths{
		Data: data, Cache: cache, Runtime: filepath.Join(data, "runtime"),
		Keys: filepath.Join(data, "keys"), State: filepath.Join(data, "local-state.json"),
	}
}
