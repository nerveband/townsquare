//go:build windows

package appconfig

import (
	"os"
	"path/filepath"
)

var lockFile *os.File

// Lock makes sure only one Townsquare server uses a data folder. Windows doesn't
// let a second process open a file another has open without sharing, so an
// exclusive create-or-open of serve.lock works as the lock.
func Lock(dataDir string) (bool, error) {
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return false, err
	}
	p := filepath.Join(dataDir, "serve.lock")
	_ = os.Remove(p) // fails while another server holds it open
	f, err := os.OpenFile(p, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o600)
	if err != nil {
		return false, nil
	}
	lockFile = f
	return true, nil
}

// Unlock releases the lock (tests; a server keeps it until it exits).
func Unlock() {
	if lockFile != nil {
		lockFile.Close()
		lockFile = nil
	}
}
