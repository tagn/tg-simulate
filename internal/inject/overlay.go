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
// replaced with the simulated values. Local relative terraform source paths are
// also made absolute so TG can locate modules when running from the overlay dir.
//
// workingDir is the root of the Terragrunt stack (the directory passed to
// tg-simulate --working-dir). When the unit lives inside workingDir, the overlay
// is placed at <scratchDir>/<rel-path-from-workingDir>/ so that the directory
// hierarchy is preserved. This is required for explicit stacks where units inside
// .terragrunt-stack/ use find_in_parent_folders() to locate shared config files
// (e.g. root.hcl) that live outside the generated stack directory. When workingDir
// is empty or unitPath is not under workingDir, a flat sanitized name is used
// (legacy behavior, safe for test helpers that create units in isolated temp dirs).
//
// The returned path is the overlay terragrunt.hcl. Callers should run terragrunt
// from the returned file's parent directory so that TG picks up the patched config
// naturally (no --config flag needed).
//
// This avoids the old `include "original"` pattern, which broke in TG 1.0.x
// because get_terragrunt_dir() inside an included file returns the child's
// (overlay's) directory rather than the included file's own directory, causing
// remote_state backend path conflicts.
func GenerateOverlay(unit *graph.Unit, sim *simulator.Simulation, scratchDir, mergeStrategy, workingDir string) (string, error) {
	if mergeStrategy == "" {
		mergeStrategy = "shallow"
	}

	// Resolve workingDir to absolute once so all path comparisons below work
	// correctly when the caller passes a relative path (e.g. "./testdata/foo").
	if workingDir != "" {
		if abs, err := filepath.Abs(workingDir); err == nil {
			workingDir = abs
		}
	}

	overlayDir := filepath.Join(scratchDir, overlayRelPath(unit.Path, workingDir))
	if err := os.MkdirAll(overlayDir, 0o755); err != nil {
		return "", fmt.Errorf("creating overlay dir: %w", err)
	}

	// For units inside an explicit stack (.terragrunt-stack/), copy HCL files
	// from each ancestor directory so find_in_parent_folders() resolves correctly
	// from the overlay directory.
	if workingDir != "" {
		if err := copyAncestorHCLFiles(workingDir, unit.Path, scratchDir); err != nil {
			return "", fmt.Errorf("unit %q: copying ancestor HCL files: %w", unit.Path, err)
		}
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

// overlayRelPath returns the path for the overlay subdirectory within scratchDir.
// When unitPath is under workingDir, the relative path is used so the directory
// hierarchy (and find_in_parent_folders resolution) is preserved. Otherwise falls
// back to a flat sanitized name derived from the absolute unit path.
func overlayRelPath(unitPath, workingDir string) string {
	if workingDir != "" {
		// Resolve workingDir to absolute so filepath.Rel works correctly when the
		// caller passes a relative path (e.g. "./testdata/foo") but unitPath is
		// already absolute (as produced by graph.Load → EvalSymlinks).
		if absWD, err := filepath.Abs(workingDir); err == nil {
			workingDir = absWD
		}
		if rel, err := filepath.Rel(workingDir, unitPath); err == nil && !strings.HasPrefix(rel, "..") {
			return rel
		}
	}
	return unitDirName(unitPath)
}

// copyAncestorHCLFiles copies .hcl files from every directory in the chain
// from workingDir down to (but not including) unitPath, placing them in the
// corresponding locations under scratchDir. This makes find_in_parent_folders()
// work correctly when terragrunt runs from the overlay unit directory.
func copyAncestorHCLFiles(workingDir, unitPath, scratchDir string) error {
	rel, err := filepath.Rel(workingDir, unitPath)
	if err != nil || strings.HasPrefix(rel, "..") {
		return nil // unitPath not under workingDir
	}

	// Split the relative path into directory components. We copy HCL files at
	// each level between workingDir and the unit's own directory.
	parts := strings.Split(filepath.Dir(rel), string(filepath.Separator))

	srcDir := workingDir
	dstDir := scratchDir

	for _, part := range parts {
		entries, err := os.ReadDir(srcDir)
		if err != nil {
			return err
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".hcl") {
				continue
			}
			dst := filepath.Join(dstDir, e.Name())
			// Don't overwrite a file already placed there (e.g. a patched unit HCL).
			if _, statErr := os.Stat(dst); statErr == nil {
				continue
			}
			if copyErr := copyFile(filepath.Join(srcDir, e.Name()), dst); copyErr != nil {
				return copyErr
			}
		}
		if part == "." {
			break
		}
		srcDir = filepath.Join(srcDir, part)
		dstDir = filepath.Join(dstDir, part)
		if err := os.MkdirAll(dstDir, 0o755); err != nil {
			return err
		}
	}
	return nil
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

// patchTerragruntHCL reads the HCL file at path and applies two kinds of patches:
//
//  1. dependency blocks: config_path is made absolute; for dependencies with
//     simulated outputs, mock_outputs/mock_outputs_allowed_terraform_commands/
//     mock_outputs_merge_strategy_with_state are injected.
//
//  2. terraform block: if source is a local relative path (starts with ./ or ../),
//     it is resolved to an absolute path relative to unit.Path. This ensures TG
//     can locate the module source when running from the overlay directory, which
//     may be in a different location than the original unit directory.
func patchTerragruntHCL(path string, unit *graph.Unit, sim *simulator.Simulation, mergeStrategy string) error {
	src, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	f, diags := hclwrite.ParseConfig(src, path, hcl.Pos{Line: 1, Column: 1})
	if diags.HasErrors() {
		return fmt.Errorf("parsing HCL: %s", diags.Error())
	}

	for _, block := range f.Body().Blocks() {
		switch block.Type() {
		case "dependency":
			if len(block.Labels()) == 0 {
				continue
			}
			if unit.Config == nil {
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
			// skip_outputs prevents TG from reading the upstream dependency's real state.
			// Without this, TG reads real state even when mock_outputs are set, and the
			// merge strategy ends up using stale pre-apply values instead of the
			// simulated future values.
			block.Body().SetAttributeValue("skip_outputs", cty.BoolVal(true))
			block.Body().SetAttributeValue("mock_outputs", mockCty)
			block.Body().SetAttributeValue("mock_outputs_allowed_terraform_commands",
				cty.ListVal([]cty.Value{cty.StringVal("plan"), cty.StringVal("validate")}))
			block.Body().SetAttributeValue("mock_outputs_merge_strategy_with_state",
				cty.StringVal(mergeStrategy))

		case "terraform":
			patchTerraformSource(block.Body(), unit.Path)
		}
	}

	return os.WriteFile(path, hclwrite.Format(f.Bytes()), 0o644)
}

// patchTerraformSource absolutizes a local relative source path in a terraform
// block so TG can locate the module when running from the overlay directory.
// Remote sources (git URLs, registry addresses) and absolute paths are left alone.
func patchTerraformSource(body *hclwrite.Body, unitPath string) {
	sourceAttr := body.GetAttribute("source")
	if sourceAttr == nil {
		return
	}

	// hclwrite expressions are represented as raw token bytes; for a simple
	// string literal they look like `"../../modules/a"` (including the quotes).
	rawBytes := sourceAttr.Expr().BuildTokens(nil).Bytes()
	raw := strings.TrimSpace(string(rawBytes))

	if len(raw) < 2 || raw[0] != '"' || raw[len(raw)-1] != '"' {
		return // not a simple quoted string (function call, var ref, etc.)
	}
	srcVal := raw[1 : len(raw)-1]

	// Only handle local relative paths; remote sources start with a scheme,
	// a registry address, or github.com and should be left as-is.
	if !strings.HasPrefix(srcVal, "./") && !strings.HasPrefix(srcVal, "../") {
		return
	}

	// TG uses "//" as a subdir separator within the source path, e.g.
	// "../../shared//modules/vpc" means: checkout "../../shared", use subdir "modules/vpc".
	base, sub, hasSub := strings.Cut(srcVal, "//")
	absBase := filepath.Clean(filepath.Join(unitPath, base))
	absSource := absBase
	if hasSub {
		absSource = absBase + "//" + sub
	}

	body.SetAttributeValue("source", cty.StringVal(absSource))
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
