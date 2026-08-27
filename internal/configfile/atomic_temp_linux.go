//go:build linux

package configfile

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"strings"

	"golang.org/x/sys/unix"
)

type linuxFileMetadata struct {
	uid    int
	gid    int
	mode   uint32
	xattrs map[string][]byte
}

type replacementMetadata struct {
	exists    bool
	requested os.FileMode
	linux     linuxFileMetadata
}

func createAtomicTemp(dir, pattern, target string, requested os.FileMode) (*os.File, replacementMetadata, error) {
	metadata := replacementMetadata{requested: requested}
	targetFD, err := unix.Open(target, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err == nil {
		metadata.exists = true
		metadata.linux, err = readLinuxFileMetadata(targetFD)
		_ = unix.Close(targetFD)
		if err != nil {
			return nil, replacementMetadata{}, err
		}
	} else if !errors.Is(err, unix.ENOENT) {
		return nil, replacementMetadata{}, err
	}
	file, err := CreateSensitiveTemp(dir, pattern)
	return file, metadata, err
}

func prepareReplacementBeforeWrite(temp *os.File, metadata replacementMetadata) error {
	if !metadata.exists {
		return nil
	}
	tempFD := int(temp.Fd())
	if err := unix.Fchown(tempFD, metadata.linux.uid, metadata.linux.gid); err != nil {
		return err
	}
	// fchown can clear mode bits. Restore the private write-time mode, not the
	// target's potentially group-readable mode, before credential bytes exist.
	return unix.Fchmod(tempFD, 0o600)
}

func finishReplacementAfterWrite(temp *os.File, metadata replacementMetadata) error {
	tempFD := int(temp.Fd())
	if !metadata.exists {
		return unix.Fchmod(tempFD, uint32(metadata.requested.Perm()))
	}
	if err := unix.Fchmod(tempFD, metadata.linux.mode); err != nil {
		return err
	}
	for name, value := range metadata.linux.xattrs {
		if !linuxReplayableXattr(name) {
			continue
		}
		if err := unix.Fsetxattr(tempFD, name, value, 0); err != nil {
			return fmt.Errorf("preserve xattr %s: %w", name, err)
		}
	}
	preserved, err := readLinuxFileMetadata(tempFD)
	if err != nil {
		return err
	}
	if preserved.uid != metadata.linux.uid || preserved.gid != metadata.linux.gid || preserved.mode != metadata.linux.mode || !equalLinuxXattrs(preserved.xattrs, metadata.linux.xattrs) {
		return errors.New("replacement file security metadata changed")
	}
	return nil
}

func linuxReplayableXattr(name string) bool {
	return strings.HasPrefix(name, "user.") || name == "system.posix_acl_access"
}

func readLinuxFileMetadata(fd int) (linuxFileMetadata, error) {
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		return linuxFileMetadata{}, err
	}
	xattrs, err := readLinuxXattrs(fd)
	if err != nil {
		return linuxFileMetadata{}, err
	}
	return linuxFileMetadata{uid: int(stat.Uid), gid: int(stat.Gid), mode: stat.Mode & 0o7777, xattrs: xattrs}, nil
}

func readLinuxXattrs(fd int) (map[string][]byte, error) {
	size, err := unix.Flistxattr(fd, nil)
	if errors.Is(err, unix.ENOTSUP) {
		return map[string][]byte{}, nil
	}
	if err != nil {
		return nil, err
	}
	if size == 0 {
		return map[string][]byte{}, nil
	}
	raw := make([]byte, size)
	read, err := unix.Flistxattr(fd, raw)
	if err != nil {
		return nil, err
	}
	out := map[string][]byte{}
	for _, name := range strings.Split(string(raw[:read]), "\x00") {
		if name == "" {
			continue
		}
		valueSize, err := unix.Fgetxattr(fd, name, nil)
		if err != nil {
			return nil, err
		}
		value := make([]byte, valueSize)
		if valueSize > 0 {
			actual, getErr := unix.Fgetxattr(fd, name, value)
			if getErr != nil {
				return nil, getErr
			}
			value = value[:actual]
		}
		out[name] = value
	}
	return out, nil
}

func equalLinuxXattrs(left, right map[string][]byte) bool {
	if len(left) != len(right) {
		return false
	}
	for name, value := range left {
		rightValue, ok := right[name]
		if !ok || !bytes.Equal(value, rightValue) {
			return false
		}
	}
	return true
}
