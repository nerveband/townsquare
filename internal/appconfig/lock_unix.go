//go:build !windows

package appconfig

import (
	"os"
	"path/filepath"
	"syscall"
)

// Lock makes sure only one Townsquare server uses a data folder (two would both
// send every post). It returns false when another server holds it. The lock is
// released when the process exits, including on exec into an update.
func Lock(dataDir string) (bool, error) {
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return false, err
	}
	f, err := os.OpenFile(filepath.Join(dataDir, "serve.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return false, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		return false, nil
	}
	// Close the lock on exec, so a restarted (updated) process can take it again.
	syscall.CloseOnExec(int(f.Fd()))
	lockFile = f // keep it open for the life of the process
	return true, nil
}

var lockFile *os.File

// Unlock releases the lock (tests; a server keeps it until it exits).
func Unlock() {
	if lockFile != nil {
		lockFile.Close()
		lockFile = nil
	}
}
