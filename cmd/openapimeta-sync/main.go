// Command openapimeta-sync prepares ignored build data or explicitly updates its lock.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/gofrs/flock"
)

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintf(os.Stderr, "openapimeta-sync: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("openapimeta-sync", flag.ContinueOnError)
	flags.SetOutput(stderr)
	archive := flags.String("archive", "", "local upstream tar.gz; optional for -prepare")
	revision := flags.String("revision", "", "full upstream Git SHA; only for an explicit lock update")
	out := flags.String("out", "internal/openapimeta", "metadata package directory")
	prepare := flags.Bool("prepare", false, "prepare ignored data from the existing lock; never change tracked files")
	check := flags.Bool("check", false, "validate prepared data offline")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if flags.NArg() != 0 || *out == "" || (*prepare && *check) ||
		((*prepare || *check) && *revision != "") || (*check && *archive != "") {
		return errors.New("use -prepare [-archive local.tar.gz], -check, or -archive local.tar.gz -revision <40hex>, with optional -out")
	}
	if !*prepare && !*check {
		if *archive == "" {
			return errors.New("explicit lock update requires -archive and -revision")
		}
		if _, err := normalizeRevision(*revision); err != nil {
			return err
		}
	}
	root, err := filepath.Abs(*out)
	if err != nil {
		return err
	}
	if err := requireDirectory(root, true); err != nil {
		return err
	}
	lockPath := filepath.Join(root, ".prepare.lock")
	if err := requireRegularOrMissing(lockPath, nil); err != nil {
		return err
	}
	lock := flock.New(lockPath)
	if err := lock.Lock(); err != nil {
		return err
	}
	defer lock.Close()
	if *check {
		if err := checkPrepared(root); err != nil {
			return fmt.Errorf("metadata is not prepared: %w; run make metadata-prepare", err)
		}
		_, err = fmt.Fprintln(stdout, "prepared metadata verified offline")
		return err
	}
	if *prepare {
		return prepareLocked(root, *archive, stdout)
	}
	return updateLock(root, *archive, *revision, stdout)
}
