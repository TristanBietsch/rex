//go:build windows

package theme

import (
	"os"
	"path/filepath"
)

// openThemeFile opens dir/file through an os.Root, which refuses a symlink
// that leaves dir: on Windows a link must stay inside the themes directory.
func openThemeFile(dir, file string) (*os.File, error) {
	root, err := os.OpenRoot(filepath.Clean(dir))
	if err != nil {
		return nil, err
	}
	defer func() { _ = root.Close() }()
	return root.Open(file)
}

// Windows file modes carry no group or other bits and ownership is an ACL
// matter, so there is nothing to check here; SECURITY.md documents ACLs as
// out of scope.
func checkPrivateFile(os.FileInfo) error { return nil }

func checkPrivateDir(os.FileInfo, string) error { return nil }
