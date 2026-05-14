package graph

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"sync"
)

// TerragruntConfig holds the parts of a unit's evaluated terragrunt config that
// tg-simulate needs: the dependency blocks and their existing mock_outputs.
type TerragruntConfig struct {
	// Dependencies is keyed by the dependency label used in the HCL file (e.g. "vnet").
	Dependencies map[string]*DependencyConfig
}

// DependencyConfig represents a single `dependency` block from a terragrunt.hcl.
type DependencyConfig struct {
	// ConfigPath is the absolute path to the upstream unit's directory.
	ConfigPath string
	// MockOutputs are the existing mock values declared in the HCL, if any.
	// They provide the strongest type hint for synthetic value generation.
	MockOutputs map[string]interface{}
}

// renderedConfig mirrors the JSON structure produced by `terragrunt render-json`.
// Only the fields tg-simulate needs are decoded; everything else is ignored.
type renderedConfig struct {
	Dependency map[string]renderedDependency `json:"dependency"`
}

type renderedDependency struct {
	ConfigPath  string                 `json:"config_path"`
	MockOutputs map[string]interface{} `json:"mock_outputs"`
}

// parseRenderedJSON parses the bytes from `terragrunt render-json` into a TerragruntConfig.
// unitDir is the absolute path of the unit — used to resolve relative config_path values.
func parseRenderedJSON(data []byte, unitDir string) (*TerragruntConfig, error) {
	var raw renderedConfig
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("decoding rendered config: %w", err)
	}

	cfg := &TerragruntConfig{
		Dependencies: make(map[string]*DependencyConfig, len(raw.Dependency)),
	}

	for label, dep := range raw.Dependency {
		absPath := dep.ConfigPath
		if !filepath.IsAbs(absPath) {
			absPath = filepath.Clean(filepath.Join(unitDir, dep.ConfigPath))
		}
		// Resolve symlinks so paths match what resolvePath() produces for graph nodes.
		if resolved, err := filepath.EvalSymlinks(absPath); err == nil {
			absPath = resolved
		}
		mockOutputs := dep.MockOutputs
		if mockOutputs == nil {
			mockOutputs = map[string]interface{}{}
		}
		cfg.Dependencies[label] = &DependencyConfig{
			ConfigPath:  absPath,
			MockOutputs: mockOutputs,
		}
	}

	return cfg, nil
}

// configCache holds render-json results for a single Load() call.
// Safe for concurrent use via its embedded mutex.
type configCache struct {
	mu    sync.Mutex
	cache map[string]*TerragruntConfig
}

func newConfigCache() *configCache {
	return &configCache{cache: make(map[string]*TerragruntConfig)}
}

// renderJSONFunc is the function used to invoke terragrunt render-json.
// Replaced in tests to avoid shelling out.
var renderJSONFunc = func(ctx context.Context, unitDir string) ([]byte, error) {
	return renderJSON(ctx, unitDir)
}

// loadConfig runs `terragrunt render-json` for unitDir, caching the result.
// Safe for concurrent callers: duplicate renders for the same path are benign
// (last writer wins, results are identical).
func loadConfig(ctx context.Context, unitDir string, cache *configCache) (*TerragruntConfig, error) {
	cache.mu.Lock()
	if cfg, ok := cache.cache[unitDir]; ok {
		cache.mu.Unlock()
		return cfg, nil
	}
	cache.mu.Unlock()

	data, err := renderJSONFunc(ctx, unitDir)
	if err != nil {
		return nil, fmt.Errorf("unit %q: render-json: %w", unitDir, err)
	}

	cfg, err := parseRenderedJSON(data, unitDir)
	if err != nil {
		return nil, fmt.Errorf("unit %q: %w", unitDir, err)
	}

	cache.mu.Lock()
	cache.cache[unitDir] = cfg
	cache.mu.Unlock()
	return cfg, nil
}

// renderJSON shells out to `terragrunt render --json` and returns stdout.
// Terragrunt 1.0.x writes the evaluated config JSON to stdout; log output
// goes to stderr and is discarded here.
func renderJSON(ctx context.Context, unitDir string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "terragrunt", "render", "--json",
		"--working-dir", unitDir,
	)
	out, err := cmd.Output() // Output() captures stdout only
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf("exit %d: %s", exitErr.ExitCode(), exitErr.Stderr)
		}
		return nil, err
	}
	return out, nil
}
