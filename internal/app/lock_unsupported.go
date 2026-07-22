//go:build !darwin && !dragonfly && !freebsd && !linux && !netbsd && !openbsd

package app

import (
	"os"
)

func tryAdvisoryLock(_ *os.File) (bool, error) {
	return false, &CLIError{Code: "CHS-LOCK-UNSUPPORTED", Message: "advisory project locks are not supported on this operating system", ExitCode: 20}
}

func unlockAdvisoryLock(_ *os.File) error { return nil }
