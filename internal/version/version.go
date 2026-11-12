// Package version holds the release label rex and rex-daemon share.
//
// It has no dependencies on other rex packages so any package (the wire
// client, the daemon, the CLI) can import it without an import cycle.
package version

import "runtime/debug"

// Version is the release label both binaries print (rex version,
// rex-daemon -version) and exchange in the Hello handshake: the client sends
// it as Hello.client_version and the daemon answers with it as
// Snapshot.daemon_version.
//
// Release builds set it from the git tag:
//
//	go build -ldflags "-X github.com/tristanbietsch/rex/internal/version.Version=v2.0.0"
//
// Without that flag it is the module version Go stamped into the binary
// (`go install …@latest` gives a pseudo-version such as
// v0.0.0-20261112093000-abcdef123456), and "dev" when there is none (a
// plain `go build` or `go test` in a checkout).
var Version = "dev"

func init() {
	Version = resolve(Version, buildInfoVersion())
}

// resolve keeps a version set with -ldflags and otherwise falls back to the
// module version from the build info.
func resolve(linked, module string) string {
	if linked != "" && linked != "dev" {
		return linked
	}
	if module != "" && module != "(devel)" {
		return module
	}
	return "dev"
}

func buildInfoVersion() string {
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return ""
	}
	return bi.Main.Version
}
