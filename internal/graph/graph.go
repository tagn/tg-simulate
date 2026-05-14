// Package graph parses the Terragrunt dependency DAG and produces a topological order.
package graph

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sync/errgroup"
)

// Unit represents a single Terragrunt unit (directory with a terragrunt.hcl) in the stack.
type Unit struct {
	Path         string             // absolute path to the unit directory
	Dependencies []string           // absolute paths to upstream units (this unit depends on them)
	Dependents   []string           // absolute paths to downstream units (they depend on this unit)
	Config       *TerragruntConfig  // evaluated config from terragrunt render-json
}

// Graph is the parsed dependency DAG with a stable topological order.
type Graph struct {
	Units map[string]*Unit // keyed by absolute path
	Order []string         // topological order: dependencies before dependents
}

// Load extracts the Terragrunt DAG from workingDir and returns a topologically sorted Graph.
// It validates that each unit's terragrunt.hcl exists on disk.
func Load(ctx context.Context, workingDir string) (*Graph, error) {
	abs, err := filepath.Abs(workingDir)
	if err != nil {
		return nil, fmt.Errorf("resolving working dir: %w", err)
	}

	dot, err := extractDOT(ctx, abs)
	if err != nil {
		return nil, fmt.Errorf("extracting DAG: %w", err)
	}

	g, err := parseDOT(dot, abs)
	if err != nil {
		return nil, err
	}

	if err := validateUnits(g.Units); err != nil {
		return nil, err
	}

	cache := newConfigCache()
	eg, egCtx := errgroup.WithContext(ctx)
	for _, unit := range g.Units {
		unit := unit
		eg.Go(func() error {
			cfg, err := loadConfig(egCtx, unit.Path, cache)
			if err != nil {
				return err
			}
			unit.Config = cfg
			return nil
		})
	}
	if err := eg.Wait(); err != nil {
		return nil, err
	}

	return g, nil
}

// validateUnits checks that each unit's terragrunt.hcl exists on disk.
func validateUnits(units map[string]*Unit) error {
	for path := range units {
		hcl := filepath.Join(path, "terragrunt.hcl")
		if _, err := os.Stat(hcl); os.IsNotExist(err) {
			return fmt.Errorf("unit %q: terragrunt.hcl not found", path)
		}
	}
	return nil
}
