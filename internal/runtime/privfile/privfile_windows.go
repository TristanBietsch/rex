//go:build windows

package privfile

import (
	"os"
	"path/filepath"
)

// open opens dir/name through an os.Root, which refuses a symlink that
// leaves dir: on Windows a link must stay inside the directory.
func open(dir, name string) (*os.File, error) {
	root, err := os.OpenRoot(filepath.Clean(dir))
	if err != nil {
		return nil, err
	}
	defer func() { _ = root.Close() }()
	return root.Open(filepath.FromSlash(name))
}

// Windows file modes carry no group or other bits and ownership is an ACL
// matter, so there is nothing to check here; SECURITY.md documents ACLs as
// out of scope.
func checkFile(os.FileInfo) error { return nil }

func checkDir(os.FileInfo, string) error { return nil }
