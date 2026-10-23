package store

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// recoveryAction is the fix S11 requires the error to name. It is written out
// here instead of referencing the constant, so the test fails if the text drifts
// into something a user cannot act on.
const recoveryAction = "move or delete the swa-mcp/ directory and reindex"

// garbageStoreFile writes a non-database file where the store lives and returns
// its directory and path.
func garbageStoreFile(t *testing.T) (dir, path string, content []byte) {
	t.Helper()
	dir = filepath.Join(t.TempDir(), DirName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("create store dir: %v", err)
	}
	content = []byte("this file used to be a database, then a checkout went wrong\n")
	path = filepath.Join(dir, DBFileName)
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatalf("write garbage store: %v", err)
	}
	return dir, path, content
}

// assertRecoveryMessage checks the two things S11 asks of the message: it names
// the store path and it names the action, instead of stopping at driver text.
func assertRecoveryMessage(t *testing.T, err error, path string) {
	t.Helper()
	if err == nil {
		t.Fatal("a store file that is not a database was accepted")
	}
	if !errors.Is(err, ErrUnusable) {
		t.Errorf("error %v does not wrap ErrUnusable", err)
	}
	msg := err.Error()
	if !strings.Contains(msg, path) {
		t.Errorf("error %q does not name the store path %q", msg, path)
	}
	if !strings.Contains(msg, recoveryAction) {
		t.Errorf("error %q does not name the recovery action %q", msg, recoveryAction)
	}
}

// TestOpenReadOnlyReportsRecoveryForANonDatabase covers S11 on the query path: a
// file that is not a database names the path and the fix, and nothing about
// reading it may touch the bytes.
func TestOpenReadOnlyReportsRecoveryForANonDatabase(t *testing.T) {
	dir, path, content := garbageStoreFile(t)

	_, err := OpenReadOnly(dir)
	assertRecoveryMessage(t, err, path)

	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read store after the failed query open: %v", err)
	}
	if !bytes.Equal(after, content) {
		t.Errorf("the query path rewrote the store file:\n--- before\n%q\n--- after\n%q", content, after)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read store dir: %v", err)
	}
	if len(entries) != 1 {
		names := make([]string, 0, len(entries))
		for _, entry := range entries {
			names = append(names, entry.Name())
		}
		t.Errorf("the query path left %v in the store directory, want only %s", names, DBFileName)
	}
}

// TestOpenReadOnlyReportsRecoveryForATruncatedStore covers the other shape of
// damage: a real store whose pages were cut off. SQLite sees a valid header and
// missing pages, which is a different driver error than "not a database", and
// must still end in the same actionable message.
func TestOpenReadOnlyReportsRecoveryForATruncatedStore(t *testing.T) {
	dir := filepath.Join(t.TempDir(), DirName)
	st, err := Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	mustPut(t, st, newRecord(t, "petstore", "testdata/petstore.json"))
	if err := st.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	path := filepath.Join(dir, DBFileName)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the intact store: %v", err)
	}
	// Keep the SQLite header, drop the pages it points at.
	truncated := raw[:100]
	if err := os.WriteFile(path, truncated, 0o644); err != nil {
		t.Fatalf("truncate store: %v", err)
	}

	_, err = OpenReadOnly(dir)
	assertRecoveryMessage(t, err, path)

	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read store after the failed query open: %v", err)
	}
	if !bytes.Equal(after, truncated) {
		t.Error("the query path rewrote the truncated store file")
	}
}

// TestOpenReportsRecoveryForANonDatabase covers S11 on the writable path: the
// index and unindex commands open the store for writing, and they must explain
// the same recovery instead of surfacing a raw driver message.
func TestOpenReportsRecoveryForANonDatabase(t *testing.T) {
	dir, path, content := garbageStoreFile(t)

	_, err := Open(dir)
	assertRecoveryMessage(t, err, path)

	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read store after the failed write open: %v", err)
	}
	if !bytes.Equal(after, content) {
		t.Errorf("opening a non-database for writing changed its bytes: %q then %q", content, after)
	}
}
