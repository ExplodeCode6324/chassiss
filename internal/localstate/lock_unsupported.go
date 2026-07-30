//go:build !darwin && !linux && !freebsd && !netbsd && !openbsd && !windows

package localstate

import (
	"fmt"
	"os"
	"runtime"
)

func acquireStoreFileLock(_ *os.File) error {
	return fmt.Errorf("cross-process local State locking is unsupported on %s", runtime.GOOS)
}

func releaseStoreFileLock(_ *os.File) error {
	return nil
}
