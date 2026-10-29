//go:build linux || windows

package daemonctl

// sunPathLen is sizeof(sockaddr_un.sun_path).
const sunPathLen = 108
