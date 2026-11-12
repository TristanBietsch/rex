package daemonctl

import "runtime"

// HotSupported reports whether this platform can hot restart the daemon
// (Windows has no execve).
var HotSupported = runtime.GOOS != "windows"
