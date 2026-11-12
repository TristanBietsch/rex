//go:build !windows

package winlaunch

// Active is true where CreateProcess hands batch files to cmd.exe.
const Active = false
