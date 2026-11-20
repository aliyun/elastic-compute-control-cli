package runner

import (
	"fmt"
	"os"
	"path/filepath"
)

// journalParentIdentity captures the file ID while the directory handle is
// open. Windows path Stat can defer this lookup until a later SameFile call.
func journalParentIdentity(path string) (os.FileInfo, error) {
	parent := filepath.Dir(path)
	dir, err := os.Open(parent)
	if err != nil {
		return nil, err
	}
	defer dir.Close()
	opened, err := dir.Stat()
	if err != nil || !opened.IsDir() {
		return nil, fmt.Errorf("journal parent is unavailable: %v", err)
	}
	current, err := os.Stat(parent)
	if err != nil || !os.SameFile(opened, current) {
		return nil, fmt.Errorf("journal parent changed while being observed: %v", err)
	}
	return opened, nil
}
