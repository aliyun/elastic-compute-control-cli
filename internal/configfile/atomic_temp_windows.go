//go:build windows

package configfile

import "os"

type replacementMetadata struct{ requested os.FileMode }

func createAtomicTemp(dir, pattern, _ string, requested os.FileMode) (*os.File, replacementMetadata, error) {
	file, err := CreateSensitiveTemp(dir, pattern)
	return file, replacementMetadata{requested: requested}, err
}

func prepareReplacementBeforeWrite(*os.File, replacementMetadata) error { return nil }

func finishReplacementAfterWrite(temp *os.File, metadata replacementMetadata) error {
	return temp.Chmod(metadata.requested.Perm())
}
