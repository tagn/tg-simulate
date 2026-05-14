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
