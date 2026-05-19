package simulator

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Save writes the full simulation state to a JSON file at path.
// The directory is created if it does not exist.
// This file is read by the `tg-simulate explain` command to trace value provenance.
// The write is atomic: data goes to a .tmp file first, then renamed into place.
func (s *Simulation) Save(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("creating simulation state dir: %w", err)
	}

	tmp := path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return fmt.Errorf("creating simulation state file: %w", err)
	}

	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	encErr := enc.Encode(s)
	closeErr := f.Close()

	if encErr != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("encoding simulation state: %w", encErr)
	}
	if closeErr != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("closing simulation state file: %w", closeErr)
	}

	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("committing simulation state: %w", err)
	}
	return nil
}

// Load reads a previously saved Simulation from a JSON file.
func Load(path string) (*Simulation, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("opening simulation state: %w", err)
	}
	defer func() { _ = f.Close() }()

	var s Simulation
	if err := json.NewDecoder(f).Decode(&s); err != nil {
		return nil, fmt.Errorf("decoding simulation state: %w", err)
	}
	return &s, nil
}
