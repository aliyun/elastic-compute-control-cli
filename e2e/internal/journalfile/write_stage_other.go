//go:build !darwin

package journalfile

import "os"

func createDurableStagedTemp(path string) (*os.File, string, func(), func(string, bool) error, error) {
	file, err := createDurableTemp(path)
	if err != nil {
		return nil, "", nil, nil, err
	}
	return file, path, func() { _ = os.Remove(path) }, nil, nil
}

func createDurableRootStagedTemp(root *os.Root, name string) (*os.File, string, func(), func(string, bool) error, error) {
	file, err := createDurableRootTemp(root, name)
	if err != nil {
		return nil, "", nil, nil, err
	}
	return file, name, func() { _ = root.Remove(name) }, nil, nil
}
