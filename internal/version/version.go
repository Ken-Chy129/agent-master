// Package version carries the build version, overridden at build time via
// -ldflags "-X .../internal/version.Version=<v>".
package version

// Version is the semantic version of this build.
var Version = "0.0.1-dev"
