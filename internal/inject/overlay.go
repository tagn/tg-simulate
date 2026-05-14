package inject

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclwrite"
	"github.com/zclconf/go-cty/cty"

	"github.com/tagn/tg-simulate/internal/graph"
	"github.com/tagn/tg-simulate/internal/simulator"
)

// GenerateOverlay copies the unit's source directory into a scratch subdirectory
// and patches the copied terragrunt.hcl in place. Every dependency config_path
// is made absolute (so it resolves correctly from the scratch location), and for
// dependencies whose upstream outputs have been simulated, mock_outputs are
// replaced with the simulated values.
//
// The returned path is <scratchDir>/<unitDirName>/terragrunt.hcl. Callers should
// run terragrunt from the returned file's parent directory so that TG picks up
// the patched config naturally (no --config flag needed).
//
// This avoids the old `include "original"` pattern, which broke in TG 1.0.x
// because get_terragrunt_dir() inside an included file returns the child's
// (overlay's) directory rather than the included file's own directory, causing
// remote_state backend path conflicts.
func GenerateOverlay(unit *graph.Unit, sim *simulator.Simulation, scratchDir, mergeStrategy string) (string, error) {
	if mergeStrategy == "" {
		mergeStrategy = "shallow"
	}

	overlayDir := filepath.Join(scratchDir, unitDirName(unit.Path))
	if err := os.MkdirAll(overlayDir, 0o755); err != nil {
		return "", fmt.Errorf("creating overlay dir: %w", err)
	}

	if err := copyUnitFiles(unit.Path, overlayDir); err != nil {
		return "", fmt.Errorf("unit %q: copying files: %w", unit.Path, err)
	}

	overlayPath := filepath.Join(overlayDir, "terragrunt.hcl")
	if err := patchTerragruntHCL(overlayPath, unit, sim, mergeStrategy); err != nil {
		return "", fmt.Errorf("unit %q: patching HCL: %w", unit.Path, err)
	}

	return overlayPath, nil
}

// copyUnitFiles copies non-generated files from src to dst (flat, no recursion
// into subdirectories). Skips .terragrunt-cache, .terraform, state files, and
// plan binaries.
func copyUnitFiles(src, dst string) error {
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if e.IsDir() || skipFile(e.Name()) {
			continue
		}
		if err := copyFile(filepath.Join(src, e.Name()), filepath.Join(dst, e.Name())); err != nil {
			return fmt.Errorf("copying %s: %w", e.Name(), err)
		}
	}
	return nil
}

func skipFile(name string) bool {
	switch name {
	case ".terragrunt-cache", ".terraform", "terraform.tfstate.backup":
		return true
	}
	return strings.HasSuffix(name, ".tfplan") || strings.HasSuffix(name, ".binary")
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	if _, err = io.Copy(out, in); err != nil {
		return err
	}
	return out.Close()
}

// patchTerragruntHCL reads the HCL file at path, absolutizes all dependency
// config_paths (so they resolve from the scratch dir), and for dependencies
// with simulated outputs injects mock_outputs, mock_outputs_allowed_terraform_commands,
// and mock_outputs_merge_strategy_with_state.
func patchTerragruntHCL(path string, unit *graph.Unit, sim *simulator.Simulation, mergeStrategy string) error {
	src, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	f, diags := hclwrite.ParseConfig(src, path, hcl.Pos{Line: 1, Column: 1})
	if diags.HasErrors() {
		return fmt.Errorf("parsing HCL: %s", diags.Error())
	}

	if unit.Config == nil {
		return nil
	}

	for _, block := range f.Body().Blocks() {
		if block.Type() != "dependency" || len(block.Labels()) == 0 {
			continue
		}
		label := block.Labels()[0]
		depCfg, ok := unit.Config.Dependencies[label]
		if !ok {
			continue
		}

		// Make config_path absolute so it resolves from any working directory.
		block.Body().SetAttributeValue("config_path", cty.StringVal(depCfg.ConfigPath))

		unitOutputs, hasOutputs := sim.UnitOutputs[depCfg.ConfigPath]
		if !hasOutputs {
			continue
		}

		mockCty, err := outputsToCty(unitOutputs)
		if err != nil {
			return fmt.Errorf("dependency %q: %w", label, err)
		}
		block.Body().SetAttributeValue("mock_outputs", mockCty)
		block.Body().SetAttributeValue("mock_outputs_allowed_terraform_commands",
			cty.ListVal([]cty.Value{cty.StringVal("plan"), cty.StringVal("validate")}))
		// Always use no_merge so the simulated (future) values override any real
		// current state. The whole point of the simulation is to predict what WILL
		// happen after upstream changes are applied, so the simulated values must
		// win over the current (pre-apply) state.
		block.Body().SetAttributeValue("mock_outputs_merge_strategy_with_state",
			cty.StringVal("no_merge"))
	}

	return os.WriteFile(path, hclwrite.Format(f.Bytes()), 0o644)
}

// outputsToCty converts a map of SimulatedOutputs to a cty object value
// suitable for use as mock_outputs in an HCL dependency block.
func outputsToCty(outputs map[string]simulator.SimulatedOutput) (cty.Value, error) {
	if len(outputs) == 0 {
		return cty.EmptyObjectVal, nil
	}
	attrs := make(map[string]cty.Value, len(outputs))
	for name, out := range outputs {
		v, err := toCtyValue(out.Value)
		if err != nil {
			return cty.NilVal, fmt.Errorf("output %q: %w", name, err)
		}
		attrs[name] = v
	}
	return cty.ObjectVal(attrs), nil
}

// toCtyValue converts a Go value produced by the simulator into a cty.Value.
// Handles strings, bools, float64 (JSON numbers), lists, and maps.
func toCtyValue(v interface{}) (cty.Value, error) {
	if v == nil {
		return cty.StringVal(""), nil
	}
	switch val := v.(type) {
	case string:
		return cty.StringVal(val), nil
	case bool:
		return cty.BoolVal(val), nil
	case float64:
		return cty.NumberFloatVal(val), nil
	case int:
		return cty.NumberIntVal(int64(val)), nil
	case int64:
		return cty.NumberIntVal(val), nil
	case []interface{}:
		return listToCty(val)
	case map[string]interface{}:
		return mapToCty(val)
	default:
		return cty.StringVal(fmt.Sprintf("%v", v)), nil
	}
}

func listToCty(items []interface{}) (cty.Value, error) {
	if len(items) == 0 {
		return cty.ListValEmpty(cty.String), nil
	}
	elems := make([]cty.Value, len(items))
	for i, item := range items {
		v, err := toCtyValue(item)
		if err != nil {
			return cty.NilVal, err
		}
		elems[i] = v
	}
	first := elems[0].Type()
	uniform := true
	for _, e := range elems[1:] {
		if !e.Type().Equals(first) {
			uniform = false
			break
		}
	}
	if uniform {
		return cty.ListVal(elems), nil
	}
	return cty.TupleVal(elems), nil
}

func mapToCty(m map[string]interface{}) (cty.Value, error) {
	if len(m) == 0 {
		return cty.EmptyObjectVal, nil
	}
	attrs := make(map[string]cty.Value, len(m))
	for k, v := range m {
		cv, err := toCtyValue(v)
		if err != nil {
			return cty.NilVal, err
		}
		attrs[k] = cv
	}
	return cty.ObjectVal(attrs), nil
}

// unitDirName produces a filesystem-safe name for a unit's scratch subdirectory.
// It strips the leading path separator and replaces all others with underscores.
func unitDirName(unitPath string) string {
	clean := filepath.Clean(unitPath)
	clean = strings.TrimPrefix(clean, string(filepath.Separator))
	return strings.ReplaceAll(clean, string(filepath.Separator), "_")
}
