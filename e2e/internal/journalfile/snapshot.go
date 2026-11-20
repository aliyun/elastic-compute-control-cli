package journalfile

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// RecoverySnapshot binds an opened private journal's bytes to its canonical
// pathname and parent directory. Reconciliation hashes and decodes Body once,
// then verifies the same snapshot under its commit lock.
type RecoverySnapshot struct {
	Body   []byte
	path   string
	parent os.FileInfo
}

func ReadPrivateRecoverySnapshot(path string) (RecoverySnapshot, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return RecoverySnapshot{}, fmt.Errorf("recovery snapshot requires a canonical absolute path")
	}
	parentPath := filepath.Dir(path)
	root, err := os.OpenRoot(parentPath)
	if err != nil {
		return RecoverySnapshot{}, err
	}
	defer root.Close()
	parent, err := root.Stat(".")
	if err != nil || parent == nil || !parent.IsDir() {
		return RecoverySnapshot{}, fmt.Errorf("recovery snapshot parent is unavailable: %v", err)
	}
	file, err := root.Open(filepath.Base(path))
	if err != nil {
		return RecoverySnapshot{}, err
	}
	body, private, readErr := ReadRecoveryFileRoot(root, file, filepath.Base(path))
	if err := errors.Join(readErr, file.Close()); err != nil {
		return RecoverySnapshot{}, err
	}
	current, err := os.Stat(parentPath)
	if err != nil || !os.SameFile(parent, current) || !private {
		return RecoverySnapshot{}, fmt.Errorf("recovery snapshot lost its private file or parent binding: %v", err)
	}
	return RecoverySnapshot{Body: body, path: path, parent: parent}, nil
}

// ReadPrivateRecoverySnapshotRoot reads through the same directory handle the
// caller uses for later receipt and journal writes.
func ReadPrivateRecoverySnapshotRoot(root *os.Root, name string) (RecoverySnapshot, error) {
	if root == nil || name == "" || name == "." || filepath.Base(name) != name {
		return RecoverySnapshot{}, fmt.Errorf("recovery snapshot requires one rooted file name")
	}
	parent, err := root.Stat(".")
	if err != nil || parent == nil || !parent.IsDir() {
		return RecoverySnapshot{}, fmt.Errorf("recovery snapshot parent is unavailable: %v", err)
	}
	file, err := root.Open(name)
	if err != nil {
		return RecoverySnapshot{}, err
	}
	path := filepath.Join(root.Name(), name)
	body, private, readErr := ReadRecoveryFileRoot(root, file, name)
	if err := errors.Join(readErr, file.Close()); err != nil {
		return RecoverySnapshot{}, err
	}
	current, err := root.Stat(".")
	if err != nil || !os.SameFile(parent, current) || !private {
		return RecoverySnapshot{}, fmt.Errorf("recovery snapshot lost its private file or parent binding: %v", err)
	}
	return RecoverySnapshot{Body: body, path: path, parent: parent}, nil
}

func (snapshot RecoverySnapshot) VerifyRoot(root *os.Root, name string) error {
	if root == nil || snapshot.parent == nil || snapshot.path != filepath.Join(root.Name(), name) {
		return fmt.Errorf("recovery snapshot rooted path changed")
	}
	current, err := ReadPrivateRecoverySnapshotRoot(root, name)
	if err != nil {
		return err
	}
	if !os.SameFile(snapshot.parent, current.parent) || !bytes.Equal(snapshot.Body, current.Body) {
		return fmt.Errorf("recovery snapshot bytes or rooted parent changed")
	}
	return nil
}

func (snapshot RecoverySnapshot) VerifyCurrent(path string) error {
	if snapshot.path == "" || path != snapshot.path || snapshot.parent == nil {
		return fmt.Errorf("recovery snapshot path changed")
	}
	current, err := ReadPrivateRecoverySnapshot(path)
	if err != nil {
		return err
	}
	if !os.SameFile(snapshot.parent, current.parent) || !bytes.Equal(snapshot.Body, current.Body) {
		return fmt.Errorf("recovery snapshot bytes or parent changed")
	}
	return nil
}
