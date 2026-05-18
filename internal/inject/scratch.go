package inject

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"
)

// NewScratchDir creates a temporary directory for a single simulation run.
// The caller is responsible for calling Cleanup when the run is complete.
func NewScratchDir() (string, error) {
	dir, err := os.MkdirTemp("", "tg-simulate-*")
	if err != nil {
		return "", fmt.Errorf("creating scratch dir: %w", err)
	}
	return dir, nil
}

// NewInPlaceScratchDir creates a scratch directory inside workingDir rather
// than in the OS temp directory. This keeps the overlay files within the
// repository, so Terragrunt commands that rely on git context (e.g.
// run_cmd("git", "rev-parse", "--show-toplevel")) continue to work correctly.
//
// The created directory matches .tg-simulate-* so callers can add that pattern
// to .gitignore to avoid accidentally committing overlays.
func NewInPlaceScratchDir(workingDir string) (string, error) {
	if err := os.MkdirAll(workingDir, 0o755); err != nil {
		return "", fmt.Errorf("ensuring working dir exists: %w", err)
	}
	dir, err := os.MkdirTemp(workingDir, ".tg-simulate-*")
	if err != nil {
		return "", fmt.Errorf("creating in-place scratch dir: %w", err)
	}
	return dir, nil
}

// Cleanup removes the scratch directory and all overlay files within it.
// Safe to call multiple times; errors are silently ignored after the first removal.
func Cleanup(scratchDir string) {
	os.RemoveAll(scratchDir)
}

// RegisterCleanup installs a signal handler that removes scratchDir before the
// process exits on SIGINT or SIGTERM. It returns a cancel function that
// deregisters the handler when called (useful in tests and normal exit paths).
func RegisterCleanup(scratchDir string) (cancel func()) {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		if _, ok := <-ch; ok {
			Cleanup(scratchDir)
			os.Exit(1)
		}
	}()

	return func() {
		signal.Stop(ch)
		close(ch)
	}
}
