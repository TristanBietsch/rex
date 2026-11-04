// Package version holds the release label rex and rex-daemon share.
//
// It has no dependencies so any package (the wire client, the daemon, the
// CLI) can import it without an import cycle.
package version

// Version is the release label both binaries print (rex version,
// rex-daemon -version) and exchange in the Hello handshake: the client sends
// it as Hello.client_version and the daemon answers with it as
// Snapshot.daemon_version.
const Version = "v1"
