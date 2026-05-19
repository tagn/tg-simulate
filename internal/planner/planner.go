// Package planner runs per-unit terragrunt plans and captures the plan JSON.
package planner

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"

	"github.com/tagn/tg-simulate/internal/graph"
)

// PlanOptions controls how a unit is planned.
type PlanOptions struct {
	// OverlayPath is the path to a generated terragrunt overlay config.
	// When empty, the unit's own terragrunt.hcl is used directly.
	OverlayPath   string
	MergeStrategy string // mock_outputs_merge_strategy_with_state value
}

// PlanResult holds the parsed plan data and metadata for a single unit.
type PlanResult struct {
	Unit       *graph.Unit
	PlanJSON   []byte // raw output of `terraform show -json`
	HasChanges bool   // true when terragrunt plan exits with code 2
}

// execTerragruntPlan and execTerraformShow are package-level vars so tests can
// replace them without shelling out to real binaries.
var execTerragruntPlan = runTerragruntPlan
var execTerraformShow = runTerraformShow

// PlanUnit runs `terragrunt plan` followed by `terraform show -json` for the
// given unit, using the overlay config when one is provided.
//
// When an overlay is provided, both the plan and show steps run from the
// overlay's directory so that provider plugins are installed and read from the
// same location.
func PlanUnit(ctx context.Context, unit *graph.Unit, opts PlanOptions) (*PlanResult, error) {
	tmpDir, err := os.MkdirTemp("", "tg-simulate-plan-*")
	if err != nil {
		return nil, fmt.Errorf("unit %q: creating temp dir: %w", unit.Path, err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	planFile := filepath.Join(tmpDir, "tfplan.binary")

	// When an overlay is provided, run from its directory. TG will find the
	// patched terragrunt.hcl there, and provider plugins will be installed
	// under .terragrunt-cache in the same dir.
	workingDir := unit.Path
	if opts.OverlayPath != "" {
		workingDir = filepath.Dir(opts.OverlayPath)
	}

	hasChanges, err := execTerragruntPlan(ctx, workingDir, planFile, opts)
	if err != nil {
		return nil, fmt.Errorf("unit %q: %w", unit.Path, err)
	}

	planJSON, err := execTerraformShow(ctx, workingDir, planFile)
	if err != nil {
		return nil, fmt.Errorf("unit %q: terraform show: %w", unit.Path, err)
	}

	return &PlanResult{
		Unit:       unit,
		PlanJSON:   planJSON,
		HasChanges: hasChanges,
	}, nil
}

var (
	tfBinaryOnce   sync.Once
	tfBinaryCached string
)

// tfBinary returns the terraform/tofu binary to use for `show -json`.
// Resolution order: TF_BINARY env var → "terraform" → "tofu".
// Result is cached after the first call.
func tfBinary() string {
	tfBinaryOnce.Do(func() {
		if bin := os.Getenv("TF_BINARY"); bin != "" {
			tfBinaryCached = bin
			return
		}
		if _, err := exec.LookPath("terraform"); err == nil {
			tfBinaryCached = "terraform"
			return
		}
		tfBinaryCached = "tofu"
	})
	return tfBinaryCached
}

// runTerragruntPlan shells out to `terragrunt plan`.
// Returns (hasChanges, error): hasChanges is true on exit code 2, false on 0.
// Any other exit code is treated as a hard error.
//
// workingDir is either the original unit dir (no overlay) or the overlay dir
// (with overlay). TG discovers terragrunt.hcl from cmd.Dir automatically.
func runTerragruntPlan(ctx context.Context, workingDir, planFile string, opts PlanOptions) (bool, error) {
	args := []string{
		"plan",
		"-out=" + planFile,
		"-no-color",
		"-detailed-exitcode",
	}

	cmd := exec.CommandContext(ctx, "terragrunt", args...)
	cmd.Dir = workingDir
	// TG_TF_PATH tells TG 1.0.x which terraform/tofu binary to call internally,
	// ensuring it matches the binary we use for `show -json`.
	cmd.Env = append(os.Environ(), "TG_TF_PATH="+tfBinary())

	out, err := cmd.CombinedOutput()
	if err == nil {
		return false, nil // exit 0: no changes
	}

	exitErr, ok := err.(*exec.ExitError)
	if !ok {
		return false, fmt.Errorf("terragrunt plan: %w\n%s", err, out)
	}

	switch exitErr.ExitCode() {
	case 2:
		return true, nil // exit 2: changes present, not an error
	default:
		return false, fmt.Errorf("terragrunt plan exit %d: %s", exitErr.ExitCode(), out)
	}
}

// runTerraformShow shells out to `terraform show -json` (or tofu if terraform
// is not on PATH) and returns stdout.
//
// It runs from the deepest directory inside workingDir/.terragrunt-cache that
// contains a .terraform subdirectory — i.e. the directory where TG ran `init`
// and where provider plugins are installed. Falling back to workingDir itself
// when the cache is not found (e.g. unit has no dependencies and TG skipped
// caching).
func runTerraformShow(ctx context.Context, workingDir, planFile string) ([]byte, error) {
	showDir := findTerraformCacheDir(workingDir)
	cmd := exec.CommandContext(ctx, tfBinary(), "show", "-json", planFile)
	cmd.Dir = showDir

	out, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf("exit %d: %s", exitErr.ExitCode(), exitErr.Stderr)
		}
		return nil, err
	}
	return out, nil
}

// findTerraformCacheDir walks workingDir/.terragrunt-cache looking for a
// directory that contains a ".terraform" subdirectory (where TG ran init).
// Returns workingDir itself when none is found.
func findTerraformCacheDir(workingDir string) string {
	cacheRoot := filepath.Join(workingDir, ".terragrunt-cache")
	var found string
	_ = filepath.Walk(cacheRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil || !info.IsDir() {
			return nil
		}
		if _, err := os.Stat(filepath.Join(path, ".terraform")); err == nil {
			found = path
		}
		return nil
	})
	if found != "" {
		return found
	}
	return workingDir
}
