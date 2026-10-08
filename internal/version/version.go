// Package version reports the build version. Release builds set these with
// -ldflags "-X github.com/nerveband/townsquare/internal/version.Version=v1.2.3 ...".
package version

var (
	Version = "dev"
	Commit  = "unknown"
	Date    = "unknown"
)

// String is "v1.2.3 (abc1234, 2026-10-06)".
func String() string { return Version + " (" + Commit + ", " + Date + ")" }
