//go:build !darwin && !linux && !windows

package autostart

import "errors"

func Supported() bool             { return false }
func Enabled() bool               { return false }
func Enable(dataDir string) error { return errors.New("not supported on this system") }
func Disable() error              { return nil }
