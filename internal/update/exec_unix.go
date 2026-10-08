//go:build !windows

package update

import (
	"os"
	"syscall"
)

// run replaces this process with p, keeping the process id (so launchd and
// systemd keep tracking it).
func run(p string, args []string) error {
	env := append(os.Environ(), handoffEnv+"=1", "TOWNSQUARE_INSTALLED="+installed())
	return syscall.Exec(p, append([]string{p}, args...), env)
}
