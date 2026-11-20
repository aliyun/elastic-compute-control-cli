//go:build !windows

package journalfile

import "os"

func createDurableTemp(path string) (*os.File, error) {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return nil, err
	}
	if err := prepareDurableTemp(file); err != nil {
		file.Close()
		os.Remove(path)
		return nil, err
	}
	return file, nil
}

func createDurableRootTemp(root *os.Root, name string) (*os.File, error) {
	file, err := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return nil, err
	}
	if err := prepareDurableTemp(file); err != nil {
		file.Close()
		root.Remove(name)
		return nil, err
	}
	return file, nil
}
