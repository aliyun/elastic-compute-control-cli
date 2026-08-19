package specsync

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

type fileUpdate struct {
	path string
	data []byte
	mode fs.FileMode
}

type stagedFileUpdate struct {
	fileUpdate
	temporary    string
	original     []byte
	originalMode fs.FileMode
	existed      bool
}

func commitFileUpdates(updates []fileUpdate) error {
	return commitFileUpdatesWithRename(updates, os.Rename)
}

func commitFileUpdatesWithRename(updates []fileUpdate, rename func(string, string) error) error {
	staged, err := stageFileUpdates(updates)
	if err != nil {
		return err
	}
	defer func() {
		for _, update := range staged {
			_ = os.Remove(update.temporary)
		}
	}()

	committed := make([]stagedFileUpdate, 0, len(staged))
	for _, update := range staged {
		if err := rename(update.temporary, update.path); err != nil {
			rollbackErr := rollbackFileUpdates(committed, rename)
			return errors.Join(fmt.Errorf("replace %s: %w", update.path, err), rollbackErr)
		}
		committed = append(committed, update)
	}
	return nil
}

func stageFileUpdates(updates []fileUpdate) ([]stagedFileUpdate, error) {
	seenPaths := map[string]bool{}
	var seenFiles []fs.FileInfo
	staged := make([]stagedFileUpdate, 0, len(updates))
	for _, update := range updates {
		path, info, exists, err := canonicalOutputPath(update.path)
		if err != nil {
			cleanupStagedUpdates(staged)
			return nil, err
		}
		duplicate := seenPaths[path]
		if exists {
			for _, previous := range seenFiles {
				if os.SameFile(previous, info) {
					duplicate = true
					break
				}
			}
		}
		if duplicate {
			cleanupStagedUpdates(staged)
			return nil, fmt.Errorf("duplicate output path %s", path)
		}
		seenPaths[path] = true
		if exists {
			seenFiles = append(seenFiles, info)
		}
		update.path = path

		item := stagedFileUpdate{fileUpdate: update}
		original, err := os.ReadFile(path)
		switch {
		case err == nil:
			item.original = original
			item.existed = true
			info, statErr := os.Stat(path)
			if statErr != nil {
				cleanupStagedUpdates(staged)
				return nil, fmt.Errorf("stat output %s: %w", path, statErr)
			}
			item.originalMode = info.Mode().Perm()
		case os.IsNotExist(err):
			item.originalMode = update.mode.Perm()
		default:
			cleanupStagedUpdates(staged)
			return nil, fmt.Errorf("read output %s: %w", path, err)
		}

		temporary, err := writeSiblingTemp(path, update.data, update.mode)
		if err != nil {
			cleanupStagedUpdates(staged)
			return nil, err
		}
		item.temporary = temporary
		staged = append(staged, item)
	}
	return staged, nil
}

func canonicalOutputPath(path string) (string, fs.FileInfo, bool, error) {
	if path == "" || filepath.Clean(path) == "." {
		return "", nil, false, errors.New("output path is required")
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", nil, false, fmt.Errorf("resolve output path %s: %w", path, err)
	}
	parent, err := filepath.EvalSymlinks(filepath.Dir(absolute))
	if err != nil {
		return "", nil, false, fmt.Errorf("resolve output directory for %s: %w", path, err)
	}
	canonical := filepath.Join(parent, filepath.Base(absolute))
	linkInfo, err := os.Lstat(canonical)
	if err == nil && linkInfo.Mode()&os.ModeSymlink != 0 {
		return "", nil, false, fmt.Errorf("output path must not be a symbolic link: %s", canonical)
	}
	if err != nil && !os.IsNotExist(err) {
		return "", nil, false, fmt.Errorf("inspect output %s: %w", canonical, err)
	}
	info, err := os.Stat(canonical)
	if err == nil {
		return canonical, info, true, nil
	}
	if os.IsNotExist(err) {
		return canonical, nil, false, nil
	}
	return "", nil, false, fmt.Errorf("stat output %s: %w", canonical, err)
}

func writeSiblingTemp(path string, data []byte, mode fs.FileMode) (string, error) {
	directory := filepath.Dir(path)
	file, err := os.CreateTemp(directory, "."+filepath.Base(path)+".specsync-*")
	if err != nil {
		return "", fmt.Errorf("create temporary output for %s: %w", path, err)
	}
	temporary := file.Name()
	remove := true
	defer func() {
		_ = file.Close()
		if remove {
			_ = os.Remove(temporary)
		}
	}()
	if _, err := file.Write(data); err != nil {
		return "", fmt.Errorf("write temporary output for %s: %w", path, err)
	}
	if err := file.Chmod(mode.Perm()); err != nil {
		return "", fmt.Errorf("chmod temporary output for %s: %w", path, err)
	}
	if err := file.Sync(); err != nil {
		return "", fmt.Errorf("sync temporary output for %s: %w", path, err)
	}
	if err := file.Close(); err != nil {
		return "", fmt.Errorf("close temporary output for %s: %w", path, err)
	}
	remove = false
	return temporary, nil
}

func rollbackFileUpdates(committed []stagedFileUpdate, rename func(string, string) error) error {
	var rollbackErrors []error
	for index := len(committed) - 1; index >= 0; index-- {
		update := committed[index]
		if !update.existed {
			if err := os.Remove(update.path); err != nil && !os.IsNotExist(err) {
				rollbackErrors = append(rollbackErrors, fmt.Errorf("remove new output %s: %w", update.path, err))
			}
			continue
		}
		temporary, err := writeSiblingTemp(update.path, update.original, update.originalMode)
		if err != nil {
			rollbackErrors = append(rollbackErrors, err)
			continue
		}
		if err := rename(temporary, update.path); err != nil {
			_ = os.Remove(temporary)
			rollbackErrors = append(rollbackErrors, fmt.Errorf("restore output %s: %w", update.path, err))
		}
	}
	return errors.Join(rollbackErrors...)
}

func cleanupStagedUpdates(staged []stagedFileUpdate) {
	for _, update := range staged {
		_ = os.Remove(update.temporary)
	}
}
