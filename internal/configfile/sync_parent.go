package configfile

import (
	"errors"
)

var syncReplacement = syncReplacementPlatform

// SyncReplacement establishes a durability barrier for an atomic replacement
// that is already visible at path. Unix persists the containing directory;
// Windows flushes the installed file and its replacement metadata.
func SyncReplacement(path string) error {
	if path == "" {
		return errors.New("path is unavailable for replacement sync")
	}
	return syncReplacement(path)
}
