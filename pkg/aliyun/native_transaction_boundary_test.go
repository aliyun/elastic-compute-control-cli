package aliyun

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aliyun/elastic-compute-control-cli/internal/configfile"
)

func nativeTransactionFixture() nativeOAuthTransactionRecord {
	return nativeOAuthTransactionRecord{Version: nativeOAuthTransactionVersion, ProfileName: "test", ConfigPath: "config.json", ResolvedConfigPath: "config.json", OldAuthGeneration: strings.Repeat("a", 64), NewAuthGeneration: strings.Repeat("b", 64), NewLoginGeneration: "login-new", HadPrevious: true,
		Previous: credentialCacheEntry{Mode: credentialModeOAuth, SourceGeneration: "previous", OAuthRefreshToken: "old"}, Next: credentialCacheEntry{Mode: credentialModeOAuth, SourceGeneration: "next", OAuthRefreshToken: "new"}}
}

func TestNativeTransactionRejectsMalformedAndOversizedJournals(t *testing.T) {
	ctx := context.Background()
	cachePath := filepath.Join(t.TempDir(), "private", "entry")
	if err := configfile.PreparePrivateDirectory(filepath.Dir(cachePath)); err != nil {
		t.Fatal(err)
	}
	path := nativeOAuthTransactionPath(cachePath)
	for _, raw := range []string{"invalid", `{}`, `{} {}`, strings.Repeat("x", nativeOAuthTransactionMaxBytes+1)} {
		if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, found, err := loadNativeOAuthTransaction(ctx, cachePath); err == nil || found {
			t.Fatalf("malformed journal accepted, found=%t err=%v", found, err)
		}
	}
	if _, found, err := loadNativeOAuthTransaction(ctx, ""); found || err != nil {
		t.Fatalf("empty journal path=%t, %v", found, err)
	}
	if _, err := beginNativeOAuthTransactionWrite(""); err == nil {
		t.Fatal("missing transaction path accepted")
	}
	if err := commitPreparedNativeOAuthTransaction(ctx, nil, nativeTransactionFixture()); err == nil {
		t.Fatal("missing prepared transaction accepted")
	}
	for _, mutate := range []func(*nativeOAuthTransactionRecord){
		func(r *nativeOAuthTransactionRecord) { r.NewAuthGeneration = "invalid" },
		func(r *nativeOAuthTransactionRecord) { r.Previous.Mode = "" },
		func(r *nativeOAuthTransactionRecord) {
			r.Next.OAuthRefreshToken = strings.Repeat("x", nativeOAuthTransactionMaxBytes)
		},
	} {
		record := nativeTransactionFixture()
		mutate(&record)
		if _, err := marshalNativeOAuthTransaction(record); err == nil {
			t.Fatal("invalid transaction record marshaled")
		}
	}
}

func TestNativeTransactionRecoveryPreservesUnrelatedCacheOwners(t *testing.T) {
	ctx := context.Background()
	for _, operation := range []string{"install", "restore"} {
		for _, owner := range []string{"missing", "previous", "next", "unrelated", "wrong mode"} {
			t.Run(operation+"/"+owner, func(t *testing.T) {
				path := filepath.Join(t.TempDir(), "private", "entry")
				record := nativeTransactionFixture()
				if err := configfile.PreparePrivateDirectory(filepath.Dir(path)); err != nil {
					t.Fatal(err)
				}
				var active credentialCacheEntry
				switch owner {
				case "previous":
					active = record.Previous
				case "next":
					active = record.Next
				case "unrelated":
					active = credentialCacheEntry{Mode: credentialModeOAuth, SourceGeneration: "other"}
				case "wrong mode":
					active = record.Previous
					if operation == "install" {
						active = record.Next
					}
					active.Mode = credentialModeCloudSSO
				}
				if owner != "missing" {
					if err := storeCredentialCacheEntry(ctx, path, active); err != nil {
						t.Fatal(err)
					}
				}
				var err error
				want := record.Previous
				if operation == "install" {
					err = installNativeOAuthTransactionNext(ctx, path, record)
					want = record.Next
				} else {
					err = restoreNativeOAuthTransactionPrevious(ctx, path, record)
				}
				conflict := owner == "unrelated" || owner == "wrong mode"
				if conflict {
					if !errors.Is(err, ErrCredentialProfileChanged) {
						t.Fatalf("owner conflict=%v", err)
					}
					want = active
				} else if err != nil {
					t.Fatal(err)
				}
				entry, found, err := loadAnyCredentialCacheEntry(ctx, path)
				if err != nil || !found || entry.Mode != want.Mode || entry.SourceGeneration != want.SourceGeneration {
					t.Fatalf("recovered entry=%+v found=%t err=%v", entry, found, err)
				}
			})
		}
	}
}

func TestNativeTransactionWithoutPreviousRetiresOnlyItsOwnEntry(t *testing.T) {
	ctx := context.Background()
	record := nativeTransactionFixture()
	record.HadPrevious = false
	path := filepath.Join(t.TempDir(), "private", "entry")
	if err := configfile.PreparePrivateDirectory(filepath.Dir(path)); err != nil {
		t.Fatal(err)
	}
	if err := restoreNativeOAuthTransactionPrevious(ctx, path, record); err != nil {
		t.Fatal(err)
	}
	if err := storeCredentialCacheEntry(ctx, path, record.Next); err != nil {
		t.Fatal(err)
	}
	if err := restoreNativeOAuthTransactionPrevious(ctx, path, record); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("new entry was not retired: %v", err)
	}
}
