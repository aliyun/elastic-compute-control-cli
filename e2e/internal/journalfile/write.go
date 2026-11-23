package journalfile

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"

	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/report"
)

var syncStagedParentDirectory = func(parent *os.File) error { return parent.Sync() }

// WriteDurable installs a private journal after syncing its contents and the
// platform's rename durability barrier. Callers hold the journal lock.
func WriteDurable(path string, data []byte) error {
	return writeDurable(path, data, true)
}

// WriteDurableRoot replaces a file through an already opened directory. A
// renamed journal parent cannot redirect the replacement into a new directory.
func WriteDurableRoot(root *os.Root, name string, data []byte) error {
	if root == nil || name == "" || name == "." || filepath.Base(name) != name {
		return fmt.Errorf("durable rooted write requires one file name")
	}
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return err
	}
	tmp := ".journal-" + hex.EncodeToString(nonce[:])
	file, tmp, cleanupStage, publish, err := createDurableRootStagedTemp(root, tmp)
	if err != nil {
		return err
	}
	defer cleanupStage()
	if _, err = file.Write(data); err == nil {
		err = file.Sync()
	}
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if publish != nil {
		err = publish(name, true)
	} else {
		err = root.Rename(tmp, name)
	}
	if err != nil {
		return err
	}
	if err := syncAndVerifyDurableRoot(root, name); err != nil {
		return err
	}
	installed, err := root.ReadFile(name)
	if err != nil || !bytes.Equal(installed, data) {
		return fmt.Errorf("durable rooted write changed after barrier: %v", err)
	}
	return nil
}

// WriteExclusiveDurableRoot installs a private immutable receipt through the
// caller's already verified parent. An existing receipt is never replaced.
func WriteExclusiveDurableRoot(root *os.Root, name string, data []byte) error {
	if root == nil || name == "" || name == "." || filepath.Base(name) != name {
		return fmt.Errorf("exclusive durable rooted write requires one file name")
	}
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return err
	}
	tmp := ".journal-" + hex.EncodeToString(nonce[:])
	file, tmp, cleanupStage, publish, err := createDurableRootStagedTemp(root, tmp)
	if err != nil {
		return err
	}
	defer cleanupStage()
	if _, err = file.Write(data); err == nil {
		err = file.Sync()
	}
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if publish != nil {
		err = publish(name, false)
	} else {
		err = root.Link(tmp, name)
		if err == nil {
			err = root.Remove(tmp)
		}
	}
	if err != nil {
		return err
	}
	if err := syncAndVerifyDurableRoot(root, name); err != nil {
		return err
	}
	installed, err := root.ReadFile(name)
	if err != nil || !bytes.Equal(installed, data) {
		return fmt.Errorf("exclusive durable rooted write changed after barrier: %v", err)
	}
	return nil
}

func syncAndVerifyDurableRoot(root *os.Root, name string) error {
	barrier := "."
	if runtime.GOOS == "windows" {
		barrier = name
	}
	var target *os.File
	var err error
	if runtime.GOOS == "windows" {
		target, err = root.OpenFile(barrier, os.O_RDWR, 0)
	} else {
		target, err = root.Open(barrier)
	}
	if err != nil {
		return err
	}
	defer target.Close()
	if err := target.Sync(); err != nil {
		return err
	}
	installed, err := root.Open(name)
	if err != nil {
		return err
	}
	defer installed.Close()
	return verifyPrivateDurableFile(installed)
}

// EnsureRecoveryDirectory applies the platform's recovery directory barrier
// before an allocation depends on an ordinary journal reservation.
func EnsureRecoveryDirectory(path string) error {
	return ensureRecoveryDirectory(filepath.Dir(path))
}

// WriteExclusiveDurable reserves an immutable recovery intent. An existing
// intent is never replaced, even after an uncertain durability error.
func WriteExclusiveDurable(path string, data []byte) error {
	if err := EnsureRecoveryDirectory(path); err != nil {
		return err
	}
	return writeDurable(path, data, false)
}

func writeDurable(path string, data []byte, replace bool) error {
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return err
	}
	tmp := filepath.Join(filepath.Dir(path), ".journal-"+hex.EncodeToString(nonce[:]))
	file, tmp, cleanupStage, publish, err := createDurableStagedTemp(tmp)
	if err != nil {
		return err
	}
	defer cleanupStage()
	if _, err = file.Write(data); err == nil {
		err = file.Sync()
	}
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if publish != nil {
		err = publish(filepath.Base(path), replace)
	} else {
		err = installDurable(tmp, path, replace)
	}
	if err != nil {
		return err
	}
	installed, err := os.Open(path)
	if err != nil {
		return err
	}
	defer installed.Close()
	return verifyPrivateDurableFile(installed)
}

// ReaffirmDurable repeats the platform's replacement barrier for bytes that
// were installed before a prior durability error. The caller holds the journal
// lock and verifies its ownership again after this call.
func ReaffirmDurable(path string, expected []byte) error {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() {
		return fmt.Errorf("durable file is unavailable: %v", err)
	}
	before, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(before, expected) {
		return fmt.Errorf("durable file changed before barrier: %v", err)
	}
	if err := WriteDurable(path, expected); err != nil {
		return err
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(after, expected) {
		return fmt.Errorf("durable file changed after barrier: %v", err)
	}
	return nil
}

// ReaffirmDurableRoot repeats a replacement barrier through the directory
// already verified by the caller. It never resolves a replaced parent path.
func ReaffirmDurableRoot(root *os.Root, name string, expected []byte) error {
	info, err := root.Lstat(name)
	if err != nil || !info.Mode().IsRegular() {
		return fmt.Errorf("durable rooted file is unavailable: %v", err)
	}
	before, err := root.ReadFile(name)
	if err != nil || !bytes.Equal(before, expected) {
		return fmt.Errorf("durable rooted file changed before barrier: %v", err)
	}
	if err := WriteDurableRoot(root, name, expected); err != nil {
		return err
	}
	after, err := root.ReadFile(name)
	if err != nil || !bytes.Equal(after, expected) {
		return fmt.Errorf("durable rooted file changed after barrier: %v", err)
	}
	return nil
}

// RestoreUninvokedMarkerRoot retires only the exact marker installed before a
// provider call, through the same verified directory used for the marker.
func RestoreUninvokedMarkerRoot(root *os.Root, name string, marked, original []byte) error {
	installed, err := root.ReadFile(name)
	if err != nil || !bytes.Equal(installed, marked) {
		return fmt.Errorf("unused rooted marker is not installed exactly: %v", err)
	}
	if err := WriteDurableRoot(root, name, original); err != nil {
		if barrierErr := ReaffirmDurableRoot(root, name, original); barrierErr != nil {
			return errors.Join(err, barrierErr)
		}
	}
	installed, err = root.ReadFile(name)
	if err != nil || !bytes.Equal(installed, original) {
		return fmt.Errorf("unused rooted marker restoration changed: %v", err)
	}
	return nil
}

// RestoreUninvokedMarker is called under the journal lock only after a
// prelaunch marker write failed. It removes an exactly installed marker before
// any provider call can occur; a different installed file remains fenced.
func RestoreUninvokedMarker(path string, marked, original []byte) error {
	installed, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(installed, marked) {
		return fmt.Errorf("unused marker is not installed exactly: %v", err)
	}
	if err := WriteDurable(path, original); err != nil {
		if barrierErr := ReaffirmDurable(path, original); barrierErr != nil {
			return errors.Join(err, barrierErr)
		}
	}
	installed, err = os.ReadFile(path)
	if err != nil || !bytes.Equal(installed, original) {
		return fmt.Errorf("unused marker restoration changed: %v", err)
	}
	return nil
}

// PrivateRecoverySidecar applies the platform's owner-only policy to native
// recovery receipts before a restart command accepts them.
func PrivateRecoverySidecar(path string) bool { return quotaNativeNewFilePrivate(path) }

// ReadRecoveryFile ties the platform privacy check to the opened file used
// for recovery decisions. The caller decides whether legacy public files may
// be accepted when private is false.
func ReadRecoveryFile(file *os.File, path string) ([]byte, bool, error) {
	return readRecoveryFileNamed(file, func() (os.FileInfo, error) { return os.Lstat(path) })
}

// ReadRecoveryFileRoot checks the name through the same directory handle that
// supplied the opened file and is used for subsequent recovery transitions.
func ReadRecoveryFileRoot(root *os.Root, file *os.File, name string) ([]byte, bool, error) {
	if root == nil || name == "" || name == "." || filepath.Base(name) != name {
		return nil, false, fmt.Errorf("recovery file requires one rooted name")
	}
	return readRecoveryFileNamed(file, func() (os.FileInfo, error) { return root.Lstat(name) })
}

func readRecoveryFileNamed(file *os.File, namedStat func() (os.FileInfo, error)) ([]byte, bool, error) {
	if file == nil {
		return nil, false, fmt.Errorf("recovery file is unavailable")
	}
	private := func() bool {
		opened, openErr := file.Stat()
		named, nameErr := namedStat()
		return openErr == nil && nameErr == nil && opened.Mode().IsRegular() &&
			os.SameFile(opened, named) && privateRecoveryDescriptor(file)
	}
	before := private()
	body, err := io.ReadAll(file)
	if err != nil {
		return nil, false, err
	}
	return body, before && private(), nil
}

// ProtectedRecoveryJournal identifies metadata whose bytes can authorize
// protected teardown, including a migrated relative Team selector.
func ProtectedRecoveryJournal(journal report.CleanupJournal) bool {
	if journal.Version != 2 {
		return false
	}
	if journal.EcctlBinSHA256 != "" || journal.QuotaBinding != nil || journal.VolumeFixtureBinding != nil || len(journal.NativeIntentPaths) != 0 || len(journal.VolumeTerminalReceipts) != 0 ||
		len(journal.VolumeScopeBindings) != 0 || len(journal.VolumeTeamBindings) != 0 || journal.JournalPath != "" && !filepath.IsAbs(journal.EcctlBin) {
		return true
	}
	for _, entry := range journal.Entries {
		if entry.TeamCreateConfirmed || entry.TeamReplayAttempted || entry.QuotaBound || entry.QuotaReserved || entry.QuotaUpdatePending ||
			entry.VolumeTeamBound || entry.VolumeTransferPending || entry.VolumeDeleteAttempted || entry.VolumeFixtureKind != "" ||
			entry.VolumeFixturePending || IsNativeVolumeDelete(entry.Teardown) {
			return true
		}
	}
	return false
}
