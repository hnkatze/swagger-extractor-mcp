package store

import (
	"os"
	"path/filepath"
	"testing"
)

// TestDirForDefaultsToWorkingDirectory covers the index command's "create the
// store in the current working directory" rule when nothing exists above it.
func TestDirForDefaultsToWorkingDirectory(t *testing.T) {
	t.Setenv(EnvDir, "")
	work := t.TempDir()

	got, err := DirFor(work)
	if err != nil {
		t.Fatalf("DirFor(%q): %v", work, err)
	}
	want := filepath.Join(work, DirName)
	if got != want {
		t.Fatalf("DirFor(%q) = %q, want %q", work, got, want)
	}
}

// TestDirForWalksUpToExistingStore covers discovery: a nested working
// directory resolves to the project root that already owns a store.
func TestDirForWalksUpToExistingStore(t *testing.T) {
	t.Setenv(EnvDir, "")
	root := t.TempDir()
	storeDir := filepath.Join(root, DirName)
	if err := os.MkdirAll(storeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	nested := filepath.Join(root, "pkg", "internal", "deep")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}

	got, err := DirFor(nested)
	if err != nil {
		t.Fatalf("DirFor(%q): %v", nested, err)
	}
	if got != storeDir {
		t.Fatalf("DirFor(%q) = %q, want %q", nested, got, storeDir)
	}

	// FindRoot reports the project root, not the store directory.
	storeRoot, ok := FindRoot(nested)
	if !ok {
		t.Fatalf("FindRoot(%q) found nothing", nested)
	}
	if storeRoot != root {
		t.Fatalf("FindRoot(%q) = %q, want %q", nested, storeRoot, root)
	}
}

// TestDirForHonoursEnvOverride covers the SWA_MCP_DIR override winning over
// discovery, as S2 requires.
func TestDirForHonoursEnvOverride(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, DirName), 0o755); err != nil {
		t.Fatal(err)
	}
	override := filepath.Join(t.TempDir(), "elsewhere")
	t.Setenv(EnvDir, override)

	got, err := DirFor(root)
	if err != nil {
		t.Fatalf("DirFor(%q): %v", root, err)
	}
	if got != override {
		t.Fatalf("DirFor(%q) = %q, want env override %q", root, got, override)
	}
}

// TestFindRootReportsAbsence keeps discovery honest: no store above the working
// directory must be reported as "not found", never as a bogus parent.
func TestFindRootReportsAbsence(t *testing.T) {
	t.Setenv(EnvDir, "")
	// A temp directory nested deep enough that no swa-mcp dir can be an
	// ancestor of the innermost level unless it exists in the temp tree.
	root := t.TempDir()
	nested := filepath.Join(root, "a", "b")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	if got, ok := FindRoot(nested); ok {
		t.Fatalf("FindRoot(%q) = %q, true; want false", nested, got)
	}
}
