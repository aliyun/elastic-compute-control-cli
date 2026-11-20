//go:build darwin

package journalfile

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"

	"golang.org/x/sys/unix"

	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/privatefs"
)

// A parent ACL can expose a child before the child's ACL is cleared. Open the
// staging directory first, remove its inherited ACL, and create through that
// handle so a path replacement cannot redirect the secret write.
func createDurableStagedTemp(path string) (*os.File, string, func(), func(string, bool) error, error) {
	parent := filepath.Dir(path)
	root, err := os.OpenRoot(parent)
	if err != nil {
		return nil, "", nil, nil, err
	}
	file, name, cleanup, publish, err := createDurableRootStagedTemp(root, filepath.Base(path))
	if err != nil {
		_ = root.Close()
		return nil, "", nil, nil, err
	}
	return file, filepath.Join(parent, name), func() { cleanup(); _ = root.Close() }, publish, nil
}

func createDurableRootStagedTemp(root *os.Root, name string) (*os.File, string, func(), func(string, bool) error, error) {
	stage := name + ".private"
	if err := root.Mkdir(stage, 0o700); err != nil {
		return nil, "", nil, nil, err
	}
	createdStage, err := root.Lstat(stage)
	if err != nil || !createdStage.IsDir() {
		return nil, "", nil, nil, fmt.Errorf("private recovery staging directory changed after creation: %v", err)
	}
	removeStage := func() {
		if named, err := root.Lstat(stage); err == nil && os.SameFile(named, createdStage) {
			_ = root.Remove(stage)
		}
	}
	parentDirectory, err := root.Open(".")
	if err != nil {
		removeStage()
		return nil, "", nil, nil, err
	}
	stageFD, err := unix.Openat(int(parentDirectory.Fd()), stage, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		_ = parentDirectory.Close()
		removeStage()
		return nil, "", nil, nil, err
	}
	directory := os.NewFile(uintptr(stageFD), stage)
	opened, statErr := directory.Stat()
	named, nameErr := root.Lstat(stage)
	var owner *syscall.Stat_t
	ownerOK := false
	if statErr == nil {
		owner, ownerOK = opened.Sys().(*syscall.Stat_t)
	}
	if statErr != nil || nameErr != nil || !os.SameFile(opened, createdStage) || !os.SameFile(opened, named) || !opened.IsDir() || !ownerOK || owner.Uid != uint32(os.Geteuid()) {
		_ = directory.Close()
		_ = parentDirectory.Close()
		removeStage()
		return nil, "", nil, nil, fmt.Errorf("private recovery staging directory identity or owner changed: %v %v", statErr, nameErr)
	}
	stageRoot, err := root.OpenRoot(stage)
	if err != nil {
		_ = directory.Close()
		_ = parentDirectory.Close()
		removeStage()
		return nil, "", nil, nil, err
	}
	anchored, anchorErr := stageRoot.Stat(".")
	cleanup := func() {
		_ = stageRoot.Remove(name)
		_ = stageRoot.Close()
		_ = directory.Close()
		if named, err := root.Lstat(stage); err == nil && os.SameFile(named, createdStage) {
			_ = unix.Unlinkat(int(parentDirectory.Fd()), stage, unix.AT_REMOVEDIR)
		}
		_ = parentDirectory.Close()
	}
	if anchorErr != nil || !os.SameFile(opened, anchored) {
		cleanup()
		return nil, "", nil, nil, fmt.Errorf("private recovery staging directory changed before preparation: %v", anchorErr)
	}
	if err := preparePrivateStage(directory); err != nil {
		cleanup()
		return nil, "", nil, nil, err
	}
	file, err := createDurableRootTemp(stageRoot, name)
	if err != nil {
		cleanup()
		return nil, "", nil, nil, err
	}
	created, err := file.Stat()
	if err != nil {
		_ = file.Close()
		cleanup()
		return nil, "", nil, nil, err
	}
	stageDirectory, err := stageRoot.Open(".")
	if err != nil {
		_ = file.Close()
		cleanup()
		return nil, "", nil, nil, err
	}
	finish := func() {
		_ = stageDirectory.Close()
		cleanup()
	}
	publish := func(destination string, replace bool) error {
		if destination == "" || filepath.Base(destination) != destination {
			return fmt.Errorf("private recovery destination is invalid")
		}
		staged, err := stageRoot.Lstat(name)
		if err != nil || !os.SameFile(staged, created) {
			return fmt.Errorf("private recovery stage entry changed before publish: %v", err)
		}
		if replace {
			err = unix.Renameat(int(stageDirectory.Fd()), name, int(parentDirectory.Fd()), destination)
		} else {
			err = unix.Linkat(int(stageDirectory.Fd()), name, int(parentDirectory.Fd()), destination, 0)
			if err == nil {
				err = stageRoot.Remove(name)
			}
		}
		if err != nil {
			return err
		}
		installed, err := root.Lstat(destination)
		if err != nil || !os.SameFile(installed, created) {
			return fmt.Errorf("private recovery installed identity changed: %v", err)
		}
		return syncStagedParentDirectory(parentDirectory)
	}
	return file, stage + "/" + name, finish, publish, nil
}

func preparePrivateStage(file *os.File) error {
	if err := file.Chmod(0o700); err != nil {
		return err
	}
	if err := privatefs.ClearInheritedACL(file); err != nil {
		return err
	}
	info, err := file.Stat()
	if err != nil || !info.IsDir() || info.Mode().Perm() != 0o700 || !privatefs.NoExtendedACLFile(file) {
		return fmt.Errorf("recovery staging directory is not private: %v", err)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != uint32(os.Geteuid()) {
		return fmt.Errorf("recovery staging directory owner differs")
	}
	return file.Sync()
}
