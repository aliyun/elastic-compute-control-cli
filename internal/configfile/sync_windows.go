//go:build windows

package configfile

// Windows does not provide a portable directory fsync through os.File. The
// temporary file itself is synced before the atomic rename.
func syncDirectory(string) error { return nil }
