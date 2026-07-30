//go:build darwin || linux || freebsd || netbsd || openbsd

package localstate

import (
	"os"

	"golang.org/x/sys/unix"
)

func acquireStoreFileLock(file *os.File) error {
	return unix.Flock(int(file.Fd()), unix.LOCK_EX)
}

func releaseStoreFileLock(file *os.File) error {
	return unix.Flock(int(file.Fd()), unix.LOCK_UN)
}
