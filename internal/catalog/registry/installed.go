package registry

import "os/exec"

// Installed reports whether the tool's binary (the first word of Command) is
// on PATH. exec.LookPath applies PATHEXT on Windows, so a stub or shim with
// .exe, .cmd or .bat is found. A tool with no command is not installed.
func Installed(t Tool) bool {
	if len(t.Command) == 0 || t.Command[0] == "" {
		return false
	}
	_, err := exec.LookPath(t.Command[0])
	return err == nil
}

// InstalledMap reports Installed for every tool, by id.
func (r *Registry) InstalledMap() map[string]bool {
	out := make(map[string]bool, len(r.Tools))
	for _, t := range r.Tools {
		out[t.ID] = Installed(t)
	}
	return out
}
