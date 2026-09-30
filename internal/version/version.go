// Package version contains the application identity injected by the build.
package version

const Name = "GlitchScope"

// Version is overridden by the release build with -ldflags -X.
var Version = "1.1"

// UserAgent returns the application identity used for outbound Go requests.
func UserAgent() string { return Name + "/" + Version }

// WindowTitle returns the title shown by the SDL window.
func WindowTitle() string { return Name + " " + Version }
