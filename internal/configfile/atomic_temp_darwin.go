//go:build darwin

package configfile

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"

	"golang.org/x/sys/unix"
)

const cloneACL = 0x0004

var cloneDarwinFile = unix.Clonefile

type darwinFileMetadata struct {
	uid    uint32
	gid    uint32
	mode   os.FileMode
	acl    string
	xattrs map[string][]byte
}

type replacementMetadata struct {
	cloned    bool
	requested os.FileMode
	darwin    darwinFileMetadata
}

func createAtomicTemp(dir, pattern, target string, requested os.FileMode) (*os.File, replacementMetadata, error) {
	if _, err := os.Stat(target); errors.Is(err, os.ErrNotExist) {
		file, createErr := CreateSensitiveTemp(dir, pattern)
		return file, replacementMetadata{requested: requested}, createErr
	} else if err != nil {
		return nil, replacementMetadata{}, err
	}
	metadata, err := readDarwinFileMetadata(target)
	if err != nil {
		return nil, replacementMetadata{}, err
	}
	placeholder, err := CreateSensitiveTemp(dir, pattern)
	if err != nil {
		return nil, replacementMetadata{}, err
	}
	path := placeholder.Name()
	if err := placeholder.Close(); err != nil {
		_ = CleanupSensitiveTemp(path)
		return nil, replacementMetadata{}, err
	}
	if err := os.Remove(path); err != nil {
		_ = CleanupSensitiveTemp(path)
		return nil, replacementMetadata{}, err
	}
	if err := cloneDarwinFile(target, path, cloneACL); err != nil {
		if !errors.Is(err, unix.ENOTSUP) && !errors.Is(err, unix.EXDEV) && !errors.Is(err, unix.EINVAL) {
			_ = CleanupSensitiveTemp(path)
			return nil, replacementMetadata{}, err
		}
		command := exec.Command("/bin/cp", "-p", target, path)
		command.Env = []string{"LC_ALL=C", "PATH=/usr/bin:/bin"}
		if output, copyErr := command.CombinedOutput(); copyErr != nil {
			_ = CleanupSensitiveTemp(path)
			return nil, replacementMetadata{}, fmt.Errorf("copy Darwin replacement metadata: %w: %s", copyErr, bytes.TrimSpace(output))
		}
	}
	if err := verifyDarwinFileMetadata(path, metadata); err != nil {
		_ = CleanupSensitiveTemp(path)
		return nil, replacementMetadata{}, err
	}
	fd, err := unix.Open(path, unix.O_RDWR|unix.O_TRUNC|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		_ = CleanupSensitiveTemp(path)
		return nil, replacementMetadata{}, err
	}
	file := os.NewFile(uintptr(fd), path)
	if file == nil {
		_ = unix.Close(fd)
		_ = CleanupSensitiveTemp(path)
		return nil, replacementMetadata{}, errors.New("atomic clone file is unavailable")
	}
	return file, replacementMetadata{cloned: true, requested: requested, darwin: metadata}, nil
}

func prepareReplacementBeforeWrite(*os.File, replacementMetadata) error { return nil }

func finishReplacementAfterWrite(temp *os.File, metadata replacementMetadata) error {
	if metadata.cloned {
		return verifyDarwinFileMetadata(temp.Name(), metadata.darwin)
	}
	if err := temp.Chmod(metadata.requested.Perm()); err != nil {
		return err
	}
	info, err := temp.Stat()
	if err != nil {
		return err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || !info.Mode().IsRegular() || info.Mode().Perm() != metadata.requested.Perm() || int(stat.Uid) != os.Geteuid() {
		return errors.New("new replacement file metadata is unsafe")
	}
	return validatePrivateOpenFileACL(temp)
}

func readDarwinFileMetadata(path string) (darwinFileMetadata, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return darwinFileMetadata{}, err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || !info.Mode().IsRegular() {
		return darwinFileMetadata{}, errors.New("Darwin replacement target is unsafe")
	}
	acl, err := readDarwinACL(path)
	if err != nil {
		return darwinFileMetadata{}, err
	}
	xattrs, err := readDarwinXattrs(path)
	if err != nil {
		return darwinFileMetadata{}, err
	}
	return darwinFileMetadata{uid: stat.Uid, gid: stat.Gid, mode: info.Mode(), acl: acl, xattrs: xattrs}, nil
}

func verifyDarwinFileMetadata(path string, expected darwinFileMetadata) error {
	actual, err := readDarwinFileMetadata(path)
	if err != nil {
		return err
	}
	if actual.uid != expected.uid || actual.gid != expected.gid || actual.mode != expected.mode || actual.acl != expected.acl || !equalDarwinXattrs(actual.xattrs, expected.xattrs) {
		return errors.New("replacement file security metadata changed")
	}
	return nil
}

func readDarwinACL(path string) (string, error) {
	command := exec.Command("/bin/ls", "-lde", path)
	command.Env = []string{"LC_ALL=C", "PATH=/usr/bin:/bin"}
	output, err := command.Output()
	if err != nil {
		return "", err
	}
	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	if len(lines) <= 1 {
		return "", nil
	}
	for index := range lines[1:] {
		lines[index+1] = strings.TrimSpace(lines[index+1])
	}
	return strings.Join(lines[1:], "\n"), nil
}

func readDarwinXattrs(path string) (map[string][]byte, error) {
	size, err := unix.Listxattr(path, nil)
	if err != nil {
		return nil, err
	}
	if size == 0 {
		return map[string][]byte{}, nil
	}
	raw := make([]byte, size)
	read, err := unix.Listxattr(path, raw)
	if err != nil {
		return nil, err
	}
	out := map[string][]byte{}
	for _, name := range strings.Split(string(raw[:read]), "\x00") {
		if name == "" {
			continue
		}
		valueSize, err := unix.Getxattr(path, name, nil)
		if err != nil {
			return nil, err
		}
		value := make([]byte, valueSize)
		if valueSize > 0 {
			actual, getErr := unix.Getxattr(path, name, value)
			if getErr != nil {
				return nil, getErr
			}
			value = value[:actual]
		}
		out[name] = value
	}
	return out, nil
}

func equalDarwinXattrs(left, right map[string][]byte) bool {
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
