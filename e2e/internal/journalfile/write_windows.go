package journalfile

import (
	"fmt"
	"os"
	"path/filepath"
	"unsafe"

	"golang.org/x/sys/windows"
)

func openedWindowsPath(file *os.File) (string, error) {
	buf := make([]uint16, 256)
	for {
		n, err := windows.GetFinalPathNameByHandle(windows.Handle(file.Fd()), &buf[0], uint32(len(buf)), 0)
		if err != nil {
			return "", err
		}
		if n < uint32(len(buf)) {
			return windows.UTF16ToString(buf[:n]), nil
		}
		if n > 32768 {
			return "", fmt.Errorf("durable temporary file path is too long")
		}
		buf = make([]uint16, n+1)
	}
}

func createDurableTemp(path string) (*os.File, error) {
	return createPrivateDurableFile(path)
}

func createDurableRootTemp(root *os.Root, name string) (*os.File, error) {
	dir, err := root.Open(".")
	if err != nil {
		return nil, err
	}
	path, err := openedWindowsPath(dir)
	dir.Close()
	if err != nil {
		return nil, err
	}
	path = filepath.Join(path, name)
	file, err := createPrivateDurableFile(path)
	if err != nil {
		return nil, err
	}
	opened, openErr := file.Stat()
	rooted, rootErr := root.Lstat(name)
	if openErr != nil || rootErr != nil || !os.SameFile(opened, rooted) {
		file.Close()
		return nil, fmt.Errorf("durable temporary file changed before rooted write: %v %v", openErr, rootErr)
	}
	return file, nil
}

func createPrivateDurableFile(path string) (*os.File, error) {
	token, err := windows.OpenCurrentProcessToken()
	if err != nil {
		return nil, err
	}
	defer token.Close()
	user, err := token.GetTokenUser()
	if err != nil {
		return nil, err
	}
	system, err := windows.CreateWellKnownSid(windows.WinLocalSystemSid)
	if err != nil {
		return nil, err
	}
	entries := []windows.EXPLICIT_ACCESS{
		{AccessPermissions: windows.GENERIC_ALL, AccessMode: windows.GRANT_ACCESS, Trustee: windows.TRUSTEE{TrusteeForm: windows.TRUSTEE_IS_SID, TrusteeType: windows.TRUSTEE_IS_USER, TrusteeValue: windows.TrusteeValueFromSID(user.User.Sid)}},
	}
	if !user.User.Sid.Equals(system) {
		entries = append(entries, windows.EXPLICIT_ACCESS{AccessPermissions: windows.GENERIC_ALL, AccessMode: windows.GRANT_ACCESS, Trustee: windows.TRUSTEE{TrusteeForm: windows.TRUSTEE_IS_SID, TrusteeType: windows.TRUSTEE_IS_USER, TrusteeValue: windows.TrusteeValueFromSID(system)}})
	}
	acl, err := windows.ACLFromEntries(entries, nil)
	if err != nil {
		return nil, err
	}
	sd, err := windows.NewSecurityDescriptor()
	if err != nil {
		return nil, err
	}
	if err := sd.SetOwner(user.User.Sid, false); err != nil {
		return nil, err
	}
	if err := sd.SetDACL(acl, true, false); err != nil {
		return nil, err
	}
	if err := sd.SetControl(windows.SE_DACL_PROTECTED, windows.SE_DACL_PROTECTED); err != nil {
		return nil, err
	}
	sa := windows.SecurityAttributes{Length: uint32(unsafe.Sizeof(windows.SecurityAttributes{})), SecurityDescriptor: sd}
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	handle, err := windows.CreateFile(name, windows.GENERIC_READ|windows.GENERIC_WRITE, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, &sa, windows.CREATE_NEW, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(handle), path)
	if err := verifyPrivateDurableFile(file); err != nil {
		file.Close()
		return nil, err
	}
	return file, nil
}

func verifyPrivateDurableFile(file *os.File) error {
	path, err := openedWindowsPath(file)
	if err != nil {
		return err
	}
	opened, err := file.Stat()
	if err != nil {
		return err
	}
	for i := 0; i < 2; i++ {
		named, err := os.Lstat(path)
		if err != nil || !os.SameFile(opened, named) || !privateRecoveryDescriptor(file) {
			return fmt.Errorf("durable recovery file lacks a protected private ACL: %v", err)
		}
	}
	return nil
}

func ensureRecoveryDirectory(dir string) error { return os.MkdirAll(dir, 0o755) }

func installDurable(tmp, path string, replace bool) error {
	source, err := windows.UTF16PtrFromString(tmp)
	if err != nil {
		return err
	}
	target, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	flags := uint32(windows.MOVEFILE_WRITE_THROUGH)
	if replace {
		flags |= windows.MOVEFILE_REPLACE_EXISTING
	}
	return windows.MoveFileEx(source, target, flags)
}
