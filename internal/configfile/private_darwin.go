//go:build darwin

package configfile

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
)

func normalizePrivatePath(path string, _ bool) error {
	command := exec.Command("/bin/chmod", "-N", path)
	command.Env = []string{"LC_ALL=C", "PATH=/usr/bin:/bin"}
	if output, err := command.CombinedOutput(); err != nil {
		return errors.New("remove private file ACL: " + string(bytes.TrimSpace(output)))
	}
	return nil
}

func normalizePrivateOpenFile(file *os.File) error {
	if file == nil {
		return errors.New("private temporary file is unavailable")
	}
	return normalizePrivatePath(file.Name(), false)
}

func validatePrivatePathACL(path string, _ bool) error {
	command := exec.Command("/bin/ls", "-lde", path)
	command.Env = []string{"LC_ALL=C", "PATH=/usr/bin:/bin"}
	output, err := command.Output()
	if err != nil {
		return err
	}
	lines := bytes.Split(bytes.TrimSpace(output), []byte{'\n'})
	if len(lines) > 1 {
		return errors.New("private file ACL is unsafe")
	}
	return nil
}

func validatePrivateOpenFileACL(file *os.File) error {
	if file == nil {
		return errors.New("private temporary file is unavailable")
	}
	return validatePrivatePathACL(file.Name(), false)
}
