//go:build windows

package configfile

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

func TestSyncReplacementPlatformFlushesInstalledWindowsFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte("contents"), 0o600); err != nil {
		t.Fatal(err)
	}
	originalFlush := flushFileBuffers
	flushCalls := 0
	flushFileBuffers = func(windows.Handle) error {
		flushCalls++
		return nil
	}
	t.Cleanup(func() { flushFileBuffers = originalFlush })
	if err := syncReplacementPlatform(path); err != nil {
		t.Fatal(err)
	}
	if flushCalls != 1 {
		t.Fatalf("FlushFileBuffers calls = %d, want 1", flushCalls)
	}

	flushErr := errors.New("flush failed")
	flushFileBuffers = func(windows.Handle) error { return flushErr }
	if err := syncReplacementPlatform(path); !errors.Is(err, flushErr) {
		t.Fatalf("replacement sync error = %v, want %v", err, flushErr)
	}
}

func TestCreateSensitiveTempUsesRestrictedWindowsDACL(t *testing.T) {
	file, err := CreateSensitiveTemp(t.TempDir(), "credential-*.json")
	if err != nil {
		t.Fatal(err)
	}
	path := file.Name()
	t.Cleanup(func() {
		_ = file.Close()
		_ = os.Remove(path)
	})
	descriptor, err := windows.GetSecurityInfo(
		windows.Handle(file.Fd()),
		windows.SE_FILE_OBJECT,
		windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION,
	)
	if err != nil {
		t.Fatal(err)
	}
	control, _, err := descriptor.Control()
	if err != nil || control&windows.SE_DACL_PROTECTED == 0 {
		t.Fatalf("sensitive temporary file DACL is not protected: %s, err=%v", descriptor.String(), err)
	}
	token, err := windows.OpenCurrentProcessToken()
	if err != nil {
		t.Fatal(err)
	}
	defer token.Close()
	user, err := token.GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	requireWindowsDACLPrincipals(t, descriptor, user.User.Sid)
	owner, _, err := descriptor.Owner()
	if err != nil || owner == nil || !owner.Equals(user.User.Sid) {
		t.Fatalf("sensitive temporary file owner = %v, want %s, err=%v", owner, user.User.Sid, err)
	}
}

func requireWindowsDACLPrincipals(t *testing.T, descriptor *windows.SECURITY_DESCRIPTOR, currentUser *windows.SID) {
	t.Helper()
	system, err := windows.CreateWellKnownSid(windows.WinLocalSystemSid)
	if err != nil {
		t.Fatal(err)
	}
	dacl, _, err := descriptor.DACL()
	if err != nil || dacl == nil || dacl.AceCount != 2 {
		t.Fatalf("DACL = %v, err=%v, want exactly two ACEs", dacl, err)
	}
	foundUser, foundSystem := false, false
	for index := uint16(0); index < dacl.AceCount; index++ {
		var ace *windows.ACCESS_ALLOWED_ACE
		if err := windows.GetAce(dacl, uint32(index), &ace); err != nil {
			t.Fatal(err)
		}
		if ace == nil || ace.Header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE {
			t.Fatalf("DACL contains a non-allow ACE: %s", descriptor.String())
		}
		sid := (*windows.SID)(unsafe.Pointer(&ace.SidStart))
		switch {
		case sid.Equals(currentUser):
			foundUser = true
		case sid.Equals(system):
			foundSystem = true
		default:
			t.Fatalf("DACL grants an unexpected principal %s: %s", sid, descriptor.String())
		}
	}
	if !foundUser || !foundSystem {
		t.Fatalf("DACL does not contain current user and SYSTEM: %s", descriptor.String())
	}
}

func TestWindowsFullControlAcceptsGenericAndMappedFileMasks(t *testing.T) {
	if !windowsACEGrantsFullControl(windows.GENERIC_ALL) {
		t.Fatal("GENERIC_ALL was not recognized as full control")
	}
	if !windowsACEGrantsFullControl(windowsFileAllAccess) {
		t.Fatal("mapped FILE_ALL_ACCESS mask was not recognized as full control")
	}
	if windowsACEGrantsFullControl(windows.FILE_GENERIC_READ | windows.FILE_GENERIC_WRITE | windows.FILE_GENERIC_EXECUTE) {
		t.Fatal("read/write/execute without ownership and DACL control was accepted as full control")
	}
}

func TestAtomicWritePreservesExistingWindowsDACL(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte("before"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := restrictFileToCurrentUserAndSystem(path); err != nil {
		t.Fatal(err)
	}
	before := windowsFileDACL(t, path)
	target, err := Resolve(path, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := target.AtomicWrite([]byte("after"), 0o600); err != nil {
		t.Fatal(err)
	}
	after := windowsFileDACL(t, path)
	if before != after {
		t.Fatalf("Windows DACL changed across replacement:\nbefore=%s\nafter=%s", before, after)
	}
}

func TestAtomicWriteCreatesRestrictedWindowsDACL(t *testing.T) {
	path := filepath.Join(t.TempDir(), "new-config.json")
	target, err := Resolve(path, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := target.AtomicWrite([]byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	dacl := windowsFileDACL(t, path)
	for _, forbidden := range []string{";;;WD)", ";;;BU)", ";;;BG)"} {
		if strings.Contains(dacl, forbidden) {
			t.Fatalf("new credential file DACL grants a broad principal: %s", dacl)
		}
	}
}

func TestValidatePrivateFileRejectsSpecificAdditionalWindowsPrincipal(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "credentials-v2")
	if err := PreparePrivateDirectory(dir); err != nil {
		t.Fatal(err)
	}
	file, err := CreateSensitiveTemp(dir, "entry-*.json")
	if err != nil {
		t.Fatal(err)
	}
	path := file.Name()
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	token, err := windows.OpenCurrentProcessToken()
	if err != nil {
		t.Fatal(err)
	}
	defer token.Close()
	user, err := token.GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	system, err := windows.CreateWellKnownSid(windows.WinLocalSystemSid)
	if err != nil {
		t.Fatal(err)
	}
	authenticatedUsers, err := windows.CreateWellKnownSid(windows.WinAuthenticatedUserSid)
	if err != nil {
		t.Fatal(err)
	}
	entries := make([]windows.EXPLICIT_ACCESS, 0, 3)
	for _, sid := range []*windows.SID{user.User.Sid, system, authenticatedUsers} {
		entries = append(entries, windows.EXPLICIT_ACCESS{
			AccessPermissions: windows.GENERIC_ALL,
			AccessMode:        windows.GRANT_ACCESS,
			Trustee: windows.TRUSTEE{
				TrusteeForm: windows.TRUSTEE_IS_SID, TrusteeType: windows.TRUSTEE_IS_USER,
				TrusteeValue: windows.TrusteeValueFromSID(sid),
			},
		})
	}
	acl, err := windows.ACLFromEntries(entries, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, acl, nil); err != nil {
		t.Fatal(err)
	}
	if err := ValidatePrivateFile(path); err == nil {
		t.Fatal("private file with an additional Windows principal was accepted")
	}
}

func windowsFileDACL(t *testing.T, path string) string {
	t.Helper()
	descriptor, err := windows.GetNamedSecurityInfo(
		path,
		windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION,
	)
	if err != nil {
		t.Fatal(err)
	}
	return descriptor.String()
}
