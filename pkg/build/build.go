// Package build carries the metadata of the binary.
//
// The values are set at link time with -ldflags "-X github.com/gnrfan/dns-change-monitor/pkg/build.Version=..."
// so that a released binary reports where it came from.
package build

// Version is the release version of the binary.
var Version = "dev"

// Commit is the short hash of the commit the binary was built from.
var Commit = "none"

// Date is the UTC date of the build.
var Date = "unknown"

// String returns a one line description of the build.
func String() string {
	return Version + " (" + Commit + ", " + Date + ")"
}
