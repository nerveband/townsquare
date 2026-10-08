//go:build windows

package update

import (
	"os"
	"os/exec"
)

// run starts p with the same console and arguments, waits for it, and exits
// with its exit code. Windows can't replace a running process in place.
func run(p string, args []string) error {
	cmd := exec.Command(p, args...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	cmd.Env = append(os.Environ(), handoffEnv+"=1", "TOWNSQUARE_INSTALLED="+installed())
	if err := cmd.Start(); err != nil {
		return err
	}
	err := cmd.Wait()
	if ee, ok := err.(*exec.ExitError); ok {
		os.Exit(ee.ExitCode())
	}
	if err != nil {
		os.Exit(1)
	}
	os.Exit(0)
	return nil
}
