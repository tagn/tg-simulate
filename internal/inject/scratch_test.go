package inject

import (
	"os"
	"testing"
)

func TestNewScratchDir_CreatesDirectory(t *testing.T) {
	dir, err := NewScratchDir()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer Cleanup(dir)

	if _, err := os.Stat(dir); err != nil {
		t.Errorf("scratch dir does not exist: %v", err)
	}
}

func TestCleanup_RemovesDirectory(t *testing.T) {
	dir, err := NewScratchDir()
	if err != nil {
		t.Fatal(err)
	}

	// Write a file inside to confirm non-empty removal works.
	f, err := os.CreateTemp(dir, "test-*")
	if err != nil {
		t.Fatal(err)
	}
	f.Close()

	Cleanup(dir)

	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("expected dir to be removed, but Stat returned: %v", err)
	}
}

func TestCleanup_IdempotentOnMissingDir(t *testing.T) {
	// Should not panic or error when called on a non-existent path.
	Cleanup("/tmp/tg-simulate-nonexistent-dir-that-does-not-exist")
}
