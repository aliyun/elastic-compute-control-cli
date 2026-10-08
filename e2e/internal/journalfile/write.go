package journalfile

import (
	"os"
	"path/filepath"
)

// WriteDurable installs a private journal after syncing its contents and the
// platform's rename durability barrier. Callers hold the journal lock.
func WriteDurable(path string, data []byte) error {
	return writeDurable(path, data, true)
}

// WriteExclusiveDurable reserves an immutable recovery intent. An existing
// intent is never replaced, even after an uncertain durability error.
func WriteExclusiveDurable(path string, data []byte) error {
	return writeDurable(path, data, false)
}

func writeDurable(path string, data []byte, replace bool) error {
	file, err := os.CreateTemp(filepath.Dir(path), ".journal-*")
	if err != nil {
		return err
	}
	tmp := file.Name()
	defer os.Remove(tmp)
	if _, err = file.Write(data); err == nil {
		err = file.Sync()
	}
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return installDurable(tmp, path, replace)
}
