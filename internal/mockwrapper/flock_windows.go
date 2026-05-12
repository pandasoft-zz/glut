//go:build windows

package mockwrapper

import (
	"errors"
	"os"
)

var errWindowsNotSupported = errors.New("mock wrapper file locking is not supported on Windows")

func lockFile(file *os.File) error {
	return errWindowsNotSupported
}

func unlockFile(file *os.File) error {
	return errWindowsNotSupported
}
