// Package version says which build of GOtome is running, for the log line that
// opens the process and for the version subcommand.
package version

import "runtime/debug"

// Set by the linker from the repository's VERSION file (see mk/go.mk). A build
// made without them falls back to what the Go toolchain recorded.
var (
	Version string
	Commit  string
)

// Info is one build.
type Info struct {
	// Version is the release, or "dev" when the linker was not told one.
	Version string `json:"version"`
	// Commit is the git hash the binary was built from, when known.
	Commit string `json:"commit,omitempty"`
}

// Current is the running build.
func Current() Info {
	info := Info{Version: Version, Commit: Commit}
	if bi, ok := debug.ReadBuildInfo(); ok && info.Commit == "" {
		for _, s := range bi.Settings {
			if s.Key == "vcs.revision" {
				info.Commit = s.Value
			}
		}
	}
	if info.Version == "" {
		info.Version = "dev"
	}
	return info
}

// String is the build on one line, as the version subcommand prints it.
func (i Info) String() string {
	if i.Commit == "" {
		return i.Version
	}
	return i.Version + " (" + i.Commit + ")"
}
