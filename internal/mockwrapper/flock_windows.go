//go:build windows

package mockwrapper

import (
	"errors"
	"os"
)

// errWindowsNotSupported is returned unconditionally: this repo has no
// Windows CI coverage to validate a golang.org/x/sys/windows.LockFileEx
// implementation against. appendBinaryCall's caller treats a lockFile error
// as "proceed unlocked" rather than a hard failure, so concurrent mock
// invocations on Windows rely on NTFS's own append-write atomicity instead
// of an explicit lock.
var errWindowsNotSupported = errors.New("mock wrapper file locking is not supported on Windows")

func lockFile(file *os.File) error {
	return errWindowsNotSupported
}

func unlockFile(file *os.File) error {
	return errWindowsNotSupported
}
