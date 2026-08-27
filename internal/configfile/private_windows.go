//go:build windows

package configfile

import (
	"errors"
	"os"
	"path/filepath"
	"unsafe"

	"golang.org/x/sys/windows"
)

const windowsFileAllAccess = windows.STANDARD_RIGHTS_REQUIRED | windows.SYNCHRONIZE | 0x1ff

func ValidatePrivateFile(path string) error {
	parent := filepath.Dir(path)
	if err := validateWindowsPrivatePath(parent, true); err != nil {
		return err
	}
	if err := validateWindowsPrivatePathACL(parent); err != nil {
		return err
	}
	if err := validateWindowsPrivatePath(path, false); err != nil {
		return err
	}
	return validateWindowsPrivatePathACL(path)
}

func PreparePrivateDirectory(path string) error {
	if path == "" {
		return errors.New("private file directory is unavailable")
	}
	_, beforeErr := os.Lstat(path)
	created := errors.Is(beforeErr, os.ErrNotExist)
	if beforeErr != nil && !created {
		return beforeErr
	}
	if err := os.MkdirAll(path, 0o700); err != nil {
		return err
	}
	if err := validateWindowsPrivatePath(path, true); err != nil {
		return err
	}
	if created {
		if err := restrictFileToCurrentUserAndSystem(path); err != nil {
			return err
		}
	}
	return validateWindowsPrivatePathACL(path)
}

func prepareCreatedPrivateDirectory(path string) error {
	if err := validateWindowsPrivatePath(path, true); err != nil {
		return err
	}
	if err := restrictFileToCurrentUserAndSystem(path); err != nil {
		return err
	}
	return validateWindowsPrivatePathACL(path)
}

func validateSensitiveTempDirectory(path string) error {
	if err := validateWindowsPrivatePath(path, true); err != nil {
		return err
	}
	return validateWindowsPrivatePathACL(path)
}

func preparePrivateTemp(file *os.File) error {
	if file == nil {
		return errors.New("private temporary file is unavailable")
	}
	if err := restrictFileToCurrentUserAndSystem(file.Name()); err != nil {
		return err
	}
	return validatePrivateOpenFile(file)
}

func validatePrivateOpenFile(file *os.File) error {
	if file == nil {
		return errors.New("private temporary file is unavailable")
	}
	if info, err := file.Stat(); err != nil {
		return err
	} else if !info.Mode().IsRegular() {
		return errors.New("private temporary file is unsafe")
	}
	return ValidatePrivateFile(file.Name())
}

func validateWindowsPrivatePath(path string, wantDirectory bool) error {
	pathPtr, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	attributes, err := windows.GetFileAttributes(pathPtr)
	if err != nil {
		return err
	}
	if attributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
		return errors.New("private path is a reparse point")
	}
	isDirectory := attributes&windows.FILE_ATTRIBUTE_DIRECTORY != 0
	if isDirectory != wantDirectory {
		return errors.New("private path type is unsafe")
	}
	return nil
}

func validateWindowsPrivatePathACL(path string) error {
	descriptor, err := windows.GetNamedSecurityInfo(
		path,
		windows.SE_FILE_OBJECT,
		windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION,
	)
	if err != nil {
		return err
	}
	control, _, err := descriptor.Control()
	if err != nil {
		return err
	}
	if control&windows.SE_DACL_PROTECTED == 0 {
		return errors.New("private file DACL is not protected")
	}
	token, err := windows.OpenCurrentProcessToken()
	if err != nil {
		return err
	}
	defer token.Close()
	user, err := token.GetTokenUser()
	if err != nil {
		return err
	}
	system, err := windows.CreateWellKnownSid(windows.WinLocalSystemSid)
	if err != nil {
		return err
	}
	owner, _, err := descriptor.Owner()
	if err != nil || owner == nil || !owner.Equals(user.User.Sid) {
		return errors.New("private file owner is unsafe")
	}
	dacl, _, err := descriptor.DACL()
	if err != nil || dacl == nil || dacl.AceCount != 2 {
		return errors.New("private file DACL is not canonical")
	}
	foundUser, foundSystem := false, false
	for index := uint16(0); index < dacl.AceCount; index++ {
		var ace *windows.ACCESS_ALLOWED_ACE
		if err := windows.GetAce(dacl, uint32(index), &ace); err != nil {
			return err
		}
		if ace == nil || ace.Header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE || !windowsACEGrantsFullControl(ace.Mask) {
			return errors.New("private file DACL contains a non-canonical ACE")
		}
		sid := (*windows.SID)(unsafe.Pointer(&ace.SidStart))
		switch {
		case sid.Equals(user.User.Sid):
			if foundUser {
				return errors.New("private file DACL repeats the current user")
			}
			foundUser = true
		case sid.Equals(system):
			if foundSystem {
				return errors.New("private file DACL repeats SYSTEM")
			}
			foundSystem = true
		default:
			return errors.New("private file DACL grants another principal")
		}
	}
	if !foundUser || !foundSystem {
		return errors.New("private file DACL is incomplete")
	}
	return nil
}

func windowsACEGrantsFullControl(mask windows.ACCESS_MASK) bool {
	return mask&windows.GENERIC_ALL != 0 || mask&windowsFileAllAccess == windowsFileAllAccess
}
