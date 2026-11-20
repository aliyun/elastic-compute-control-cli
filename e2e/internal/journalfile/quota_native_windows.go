//go:build windows

package journalfile

import (
	"os"
	"unsafe"

	"golang.org/x/sys/windows"
)

const quotaWindowsFileAllAccess = windows.STANDARD_RIGHTS_REQUIRED | windows.SYNCHRONIZE | 0x1ff

// Windows os.FileInfo.Mode reports 0666 for writable files regardless of
// their ACL. Check the installed file's protected owner-only DACL instead.
func quotaNativeNewFilePrivate(path string) bool {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() {
		return false
	}
	file, err := os.Open(path)
	if err != nil {
		return false
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !os.SameFile(info, opened) || !privateRecoveryDescriptor(file) {
		return false
	}
	after, err := os.Lstat(path)
	return err == nil && os.SameFile(opened, after)
}

func privateRecoveryDescriptor(file *os.File) bool {
	if file == nil {
		return false
	}
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return false
	}
	descriptor, err := windows.GetSecurityInfo(windows.Handle(file.Fd()), windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		return false
	}
	control, _, err := descriptor.Control()
	if err != nil || control&windows.SE_DACL_PROTECTED == 0 {
		return false
	}
	token, err := windows.OpenCurrentProcessToken()
	if err != nil {
		return false
	}
	defer token.Close()
	user, err := token.GetTokenUser()
	if err != nil {
		return false
	}
	system, err := windows.CreateWellKnownSid(windows.WinLocalSystemSid)
	if err != nil {
		return false
	}
	owner, _, err := descriptor.Owner()
	if err != nil || owner == nil || !owner.Equals(user.User.Sid) {
		return false
	}
	dacl, _, err := descriptor.DACL()
	wantCount := uint16(2)
	if user.User.Sid.Equals(system) {
		wantCount = 1
	}
	if err != nil || dacl == nil || dacl.AceCount != wantCount {
		return false
	}
	foundUser, foundSystem := false, false
	for index := uint16(0); index < dacl.AceCount; index++ {
		var ace *windows.ACCESS_ALLOWED_ACE
		if windows.GetAce(dacl, uint32(index), &ace) != nil || ace == nil || ace.Header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE || ace.Mask&windows.GENERIC_ALL == 0 && ace.Mask&quotaWindowsFileAllAccess != quotaWindowsFileAllAccess {
			return false
		}
		sid := (*windows.SID)(unsafe.Pointer(&ace.SidStart))
		switch {
		case sid.Equals(user.User.Sid):
			if foundUser {
				return false
			}
			foundUser = true
		case sid.Equals(system):
			if foundSystem {
				return false
			}
			foundSystem = true
		default:
			return false
		}
	}
	return foundUser && (foundSystem || user.User.Sid.Equals(system))
}
