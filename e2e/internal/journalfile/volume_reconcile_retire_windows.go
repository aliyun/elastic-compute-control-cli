//go:build windows

package journalfile

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func readRootEntries(root *os.Root) ([]os.DirEntry, error) {
	dir, err := root.Open(".")
	if err != nil {
		return nil, err
	}
	defer dir.Close()
	return dir.ReadDir(-1)
}

func volumeReconcileRetirementPrefix(path string) string {
	digest := sha256.Sum256([]byte(filepath.Base(path)))
	return ".journal-retiring-" + hex.EncodeToString(digest[:8]) + "-"
}

func volumeReconcileRetiredPrefix(path string) string {
	digest := sha256.Sum256([]byte(filepath.Base(path)))
	return ".journal-retired-" + hex.EncodeToString(digest[:8]) + "-"
}

func checkRetiredVolumeReconcileRetirement(root *os.Root, path string) error {
	entries, err := readRootEntries(root)
	if err != nil {
		return err
	}
	prefix := volumeReconcileRetiredPrefix(path)
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), prefix) {
			continue
		}
		suffix := strings.TrimPrefix(entry.Name(), prefix)
		if len(suffix) != 64+1+32 || suffix[64] != '-' || !validHistoricalDigest(suffix[:64]) || !validRetirementNonce(suffix[65:]) || !entry.Type().IsRegular() {
			return fmt.Errorf("Volume reconciliation retired marker is invalid")
		}
		if _, err := readVolumeReconcileTransitionAt(root, path, entry.Name(), volumeReconcileTransitionName(path, suffix[:64])); err != nil {
			return fmt.Errorf("Volume reconciliation retired marker changed: %w", err)
		}
		body, err := root.ReadFile(entry.Name())
		if err != nil || len(body) == 0 || ReaffirmDurableRoot(root, entry.Name(), body) != nil {
			return fmt.Errorf("Volume reconciliation retired marker durability is uncertain: %v", err)
		}
	}
	return nil
}

// Replay and staging hold the anchored journal lock. Complete only a pending
// retirement whose current predecessor and archived transition still agree.
func recoverPendingVolumeReconcileRetirement(root *os.Root, path string) error {
	parent, err := root.Stat(".")
	if err != nil {
		return err
	}
	actual, err := os.Stat(filepath.Dir(path))
	if err != nil || !os.SameFile(parent, actual) {
		return fmt.Errorf("Volume reconciliation retirement parent changed: %v", err)
	}
	if err := checkRetiredVolumeReconcileRetirement(root, path); err != nil {
		return err
	}
	entries, err := readRootEntries(root)
	if err != nil {
		return err
	}
	prefix := volumeReconcileRetirementPrefix(path)
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), prefix) {
			continue
		}
		suffix := strings.TrimPrefix(entry.Name(), prefix)
		digest := suffix
		if len(suffix) > 64 {
			digest = suffix[:64]
			if len(suffix) != 64+len("-moved-")+32 || !strings.HasPrefix(suffix[64:], "-moved-") || !validRetirementNonce(suffix[64+len("-moved-"):]) {
				return fmt.Errorf("Volume reconciliation pending retirement name is invalid")
			}
		}
		if !validHistoricalDigest(digest) || !entry.Type().IsRegular() {
			return fmt.Errorf("Volume reconciliation pending retirement is invalid")
		}
		active := volumeReconcileTransitionName(path, digest)
		if _, err := root.Lstat(active); !os.IsNotExist(err) {
			return fmt.Errorf("Volume reconciliation pending retirement conflicts with an active edge: %v", err)
		}
		staged, err := readVolumeReconcileTransitionAt(root, path, entry.Name(), active)
		if err != nil {
			return err
		}
		pendingBody, err := root.ReadFile(entry.Name())
		if err != nil || len(pendingBody) == 0 {
			return fmt.Errorf("Volume reconciliation pending edge changed: %v", err)
		}
		before, err := root.ReadFile(filepath.Base(path))
		if err != nil || len(before) == 0 {
			return fmt.Errorf("Volume reconciliation pending predecessor is unavailable: %v", err)
		}
		if err := validateUnusedVolumeReconcileTransition(root, path, staged, before); err != nil {
			return err
		}
		if err := completePendingVolumeReconcileRetirement(root, path, entry.Name(), digest, pendingBody); err != nil {
			return err
		}
		installed, err := root.ReadFile(filepath.Base(path))
		if err != nil || !bytes.Equal(installed, before) {
			return fmt.Errorf("Volume reconciliation predecessor changed after pending retirement: %v", err)
		}
		if staged.SidecarPath != "" {
			want, err := readHistoricalOriginal(root, path, staged.SidecarStates[0])
			if err != nil {
				return err
			}
			installed, err := root.ReadFile(filepath.Base(staged.SidecarPath))
			if err != nil || !bytes.Equal(installed, want) {
				return fmt.Errorf("Volume reconciliation sidecar changed after pending retirement: %v", err)
			}
		}
	}
	return nil
}

func retireVolumeReconcileEdge(root *os.Root, path, name string) error {
	parent, err := root.Stat(".")
	if err != nil {
		return err
	}
	actual, err := os.Stat(filepath.Dir(path))
	if err != nil || !os.SameFile(parent, actual) {
		return fmt.Errorf("Volume reconciliation retirement parent changed: %v", err)
	}
	digest := strings.TrimSuffix(strings.TrimPrefix(name, filepath.Base(path)+".volume-reconcile-"), ".json")
	if !validHistoricalDigest(digest) || name != volumeReconcileTransitionName(path, digest) {
		return fmt.Errorf("Volume reconciliation retirement edge name is invalid")
	}
	pending := volumeReconcileRetirementPrefix(path) + digest
	if _, err := root.Lstat(pending); !os.IsNotExist(err) {
		return fmt.Errorf("Volume reconciliation pending retirement already exists: %v", err)
	}
	if err := root.Rename(name, pending); err != nil {
		return err
	}
	before, err := root.ReadFile(pending)
	if err != nil || len(before) == 0 {
		return fmt.Errorf("Volume reconciliation pending edge changed: %v", err)
	}
	return completePendingVolumeReconcileRetirement(root, path, pending, digest, before)
}

func validRetirementNonce(value string) bool {
	if len(value) != 32 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil && strings.ToLower(value) == value
}

func completePendingVolumeReconcileRetirement(root *os.Root, path, pending, digest string, expected []byte) error {
	parent, err := root.Stat(".")
	if err != nil {
		return err
	}
	actual, err := os.Stat(filepath.Dir(path))
	if err != nil || !os.SameFile(parent, actual) {
		return fmt.Errorf("Volume reconciliation retirement parent changed: %v", err)
	}
	active := volumeReconcileTransitionName(path, digest)
	prefix := volumeReconcileRetirementPrefix(path) + digest
	moved := pending
	var nonce string
	if pending == prefix {
		var random [16]byte
		if _, err := rand.Read(random[:]); err != nil {
			return err
		}
		nonce = hex.EncodeToString(random[:])
		moved = prefix + "-moved-" + nonce
		current, err := root.ReadFile(pending)
		if err != nil || len(expected) == 0 || !bytes.Equal(current, expected) {
			return fmt.Errorf("Volume reconciliation pending edge changed before retirement: %v", err)
		}
		if err := root.Rename(pending, moved); err != nil {
			// The moved name remains discoverable if the operation committed
			// despite an error. Recovery reconciles its actual contents.
			return err
		}
	} else if strings.HasPrefix(pending, prefix+"-moved-") {
		nonce = strings.TrimPrefix(pending, prefix+"-moved-")
		if !validRetirementNonce(nonce) {
			return fmt.Errorf("Volume reconciliation moved retirement name is invalid")
		}
	} else {
		return fmt.Errorf("Volume reconciliation pending retirement name is invalid")
	}
	if info, err := root.Lstat(moved); err != nil || !info.Mode().IsRegular() {
		return fmt.Errorf("Volume reconciliation moved tombstone is unavailable: %v", err)
	}
	if _, err := ReadPrivateRecoverySnapshotRoot(root, moved); err != nil {
		return err
	}
	body, err := root.ReadFile(moved)
	if err != nil || len(body) == 0 || !bytes.Equal(body, expected) || ReaffirmDurableRoot(root, moved, expected) != nil {
		return fmt.Errorf("Volume reconciliation moved tombstone durability is uncertain: %v", err)
	}
	actual, err = os.Stat(filepath.Dir(path))
	if err != nil || !os.SameFile(parent, actual) {
		return fmt.Errorf("Volume reconciliation retirement parent changed after barrier: %v", err)
	}
	if _, err := root.Lstat(active); !os.IsNotExist(err) {
		return fmt.Errorf("Volume reconciliation retired edge remains active: %v", err)
	}
	if _, err := root.Lstat(prefix); !os.IsNotExist(err) {
		return fmt.Errorf("Volume reconciliation pending edge remains active: %v", err)
	}
	retired := volumeReconcileRetiredPrefix(path) + digest + "-" + nonce
	if err := root.Rename(moved, retired); err != nil {
		// A failed write-through call may still have installed the final name.
		// Accept it only after the same private, exact-byte durability check.
		if info, statErr := root.Lstat(retired); statErr != nil || !info.Mode().IsRegular() {
			return err
		}
		if _, readErr := ReadPrivateRecoverySnapshotRoot(root, retired); readErr != nil {
			return err
		}
		if current, readErr := root.ReadFile(retired); readErr != nil || !bytes.Equal(current, expected) || ReaffirmDurableRoot(root, retired, expected) != nil {
			return err
		}
	}
	if snapshot, err := ReadPrivateRecoverySnapshotRoot(root, retired); err != nil || !bytes.Equal(snapshot.Body, expected) {
		return fmt.Errorf("Volume reconciliation retired tombstone changed: %v", err)
	}
	if err := ReaffirmDurableRoot(root, retired, expected); err != nil {
		return fmt.Errorf("Volume reconciliation retired tombstone barrier is uncertain: %w", err)
	}
	if _, err := root.Lstat(moved); !os.IsNotExist(err) {
		return fmt.Errorf("Volume reconciliation moved tombstone remains active: %v", err)
	}
	return nil
}
