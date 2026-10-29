//go:build !linux && !windows

package daemonctl

// sunPathLen is sizeof(sockaddr_un.sun_path) on macOS and the BSDs.
const sunPathLen = 104
