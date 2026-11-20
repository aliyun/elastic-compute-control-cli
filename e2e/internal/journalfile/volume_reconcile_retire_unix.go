//go:build !windows

package journalfile

import "os"

func recoverPendingVolumeReconcileRetirement(*os.Root, string) error { return nil }

func retireVolumeReconcileEdge(root *os.Root, _, name string) error {
	if err := root.Remove(name); err != nil {
		return err
	}
	directory, err := root.Open(".")
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}
