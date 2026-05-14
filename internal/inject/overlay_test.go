package inject

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/zclconf/go-cty/cty"

	"github.com/tagn/tg-simulate/internal/graph"
	"github.com/tagn/tg-simulate/internal/simulator"
)

// makeUnit creates a unit directory with a real terragrunt.hcl that contains
// dependency blocks matching deps. This ensures the copy-and-patch logic in
// GenerateOverlay has actual blocks to find and modify.
func makeUnit(t *testing.T, name string, deps map[string]*graph.DependencyConfig) *graph.Unit {
	t.Helper()
	dir := filepath.Join(t.TempDir(), name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "terragrunt.hcl"), []byte(buildHCL(deps)), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := &graph.TerragruntConfig{Dependencies: deps}
	if deps == nil {
		cfg.Dependencies = map[string]*graph.DependencyConfig{}
	}
	return &graph.Unit{Path: dir, Config: cfg}
}

// buildHCL generates minimal HCL content for a unit with the given dependency
// configs. Labels are sorted so the output is deterministic.
func buildHCL(deps map[string]*graph.DependencyConfig) string {
	if len(deps) == 0 {
		return "# no dependencies\n"
	}
	labels := make([]string, 0, len(deps))
	for l := range deps {
		labels = append(labels, l)
	}
	sort.Strings(labels)

	var sb strings.Builder
	for _, label := range labels {
		dep := deps[label]
		fmt.Fprintf(&sb, "dependency %q {\n  config_path = %q\n  mock_outputs = {}\n}\n\n", label, dep.ConfigPath)
	}
	return sb.String()
}

// simWith builds a Simulation with one unit's outputs pre-set.
func simWith(path string, outputs map[string]simulator.SimulatedOutput) *simulator.Simulation {
	s := simulator.NewSimulation()
	s.SetOutputs(path, outputs)
	return s
}

// parseHCL validates that content is well-formed HCL and returns parsed body.
func parseHCL(t *testing.T, src []byte) *hclsyntax.Body {
	t.Helper()
	f, diags := hclsyntax.ParseConfig(src, "overlay.hcl", hcl.Pos{Line: 1, Column: 1})
	if diags.HasErrors() {
		t.Fatalf("overlay is not valid HCL:\n%s\ncontent:\n%s", diags.Error(), src)
	}
	return f.Body.(*hclsyntax.Body)
}

func TestGenerateOverlay_NoDependencies(t *testing.T) {
	unit := makeUnit(t, "solo", nil)
	sim := simulator.NewSimulation()

	overlayPath, err := GenerateOverlay(unit, sim, t.TempDir(), "shallow", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	content, err := os.ReadFile(overlayPath)
	if err != nil {
		t.Fatal(err)
	}

	body := parseHCL(t, content)
	for _, b := range body.Blocks {
		if b.Type == "dependency" {
			t.Errorf("unexpected dependency block in no-dep overlay\n%s", content)
		}
	}
}

func TestGenerateOverlay_WithSimulatedDependency(t *testing.T) {
	vnetDir := t.TempDir()

	unit := makeUnit(t, "app", map[string]*graph.DependencyConfig{
		"vnet": {ConfigPath: vnetDir, MockOutputs: map[string]interface{}{}},
	})

	sim := simWith(vnetDir, map[string]simulator.SimulatedOutput{
		"vnet_id":    {Value: "sim-vnet-123", Source: simulator.SourceSynthetic, Confidence: 0.3},
		"subnet_ids": {Value: []interface{}{"sim-subnet-a", "sim-subnet-b"}, Source: simulator.SourceSynthetic, Confidence: 0.3},
	})

	overlayPath, err := GenerateOverlay(unit, sim, t.TempDir(), "shallow", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	content, err := os.ReadFile(overlayPath)
	if err != nil {
		t.Fatal(err)
	}

	body := parseHCL(t, content)

	var depBlock *hclsyntax.Block
	for _, b := range body.Blocks {
		if b.Type == "dependency" && len(b.Labels) > 0 && b.Labels[0] == "vnet" {
			depBlock = b
		}
	}
	if depBlock == nil {
		t.Fatalf("no dependency \"vnet\" block found\n%s", content)
	}

	if _, ok := depBlock.Body.Attributes["config_path"]; !ok {
		t.Error("missing config_path attribute")
	}
	if !filepath.IsAbs(vnetDir) {
		t.Error("vnetDir not absolute — test setup issue")
	}

	contentStr := string(content)
	if !strings.Contains(contentStr, "mock_outputs_allowed_terraform_commands") {
		t.Error("missing mock_outputs_allowed_terraform_commands")
	}
	if !strings.Contains(contentStr, "mock_outputs_merge_strategy_with_state") {
		t.Error("missing mock_outputs_merge_strategy_with_state")
	}
	// Injected deps always use no_merge so simulated future values win over real state.
	if !strings.Contains(contentStr, `"no_merge"`) {
		t.Errorf("expected no_merge strategy for injected dep\n%s", content)
	}
}

func TestGenerateOverlay_InjectedDepAlwaysNoMerge(t *testing.T) {
	depDir := t.TempDir()
	unit := makeUnit(t, "app", map[string]*graph.DependencyConfig{
		"db": {ConfigPath: depDir},
	})
	sim := simWith(depDir, map[string]simulator.SimulatedOutput{
		"endpoint": {Value: "sim-db.example.com", Source: simulator.SourceSynthetic},
	})

	// The mergeStrategy arg is irrelevant for injected deps — they always use no_merge.
	for _, strategy := range []string{"", "shallow", "no_merge"} {
		overlayPath, err := GenerateOverlay(unit, sim, t.TempDir(), strategy, "")
		if err != nil {
			t.Fatalf("strategy=%q: %v", strategy, err)
		}
		content, _ := os.ReadFile(overlayPath)
		if !strings.Contains(string(content), `"no_merge"`) {
			t.Errorf("strategy=%q: expected no_merge for injected dep\n%s", strategy, content)
		}
	}
}

func TestGenerateOverlay_SkipsDependencyWithNoSimOutputs(t *testing.T) {
	depDir := t.TempDir()
	unit := makeUnit(t, "app", map[string]*graph.DependencyConfig{
		"unplanned": {ConfigPath: depDir},
	})
	// Simulation has no outputs for depDir.
	sim := simulator.NewSimulation()

	overlayPath, err := GenerateOverlay(unit, sim, t.TempDir(), "shallow", "")
	if err != nil {
		t.Fatal(err)
	}
	content, _ := os.ReadFile(overlayPath)
	contentStr := string(content)

	// The dependency block exists (copied from original) but mock_outputs should
	// NOT have been replaced with injected commands/strategy.
	if strings.Contains(contentStr, "mock_outputs_allowed_terraform_commands") {
		t.Errorf("should not inject mock commands for unplanned dep\n%s", content)
	}
	// config_path should be the absolute path.
	if !strings.Contains(contentStr, depDir) {
		t.Errorf("expected absolute config_path %q in overlay\n%s", depDir, content)
	}
}

func TestGenerateOverlay_CopiesSourceFiles(t *testing.T) {
	unit := makeUnit(t, "myunit", nil)
	// Add a .tf file to the source dir.
	if err := os.WriteFile(filepath.Join(unit.Path, "main.tf"), []byte("# main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sim := simulator.NewSimulation()

	overlayPath, err := GenerateOverlay(unit, sim, t.TempDir(), "shallow", "")
	if err != nil {
		t.Fatal(err)
	}
	overlayDir := filepath.Dir(overlayPath)

	if _, err := os.Stat(filepath.Join(overlayDir, "main.tf")); os.IsNotExist(err) {
		t.Error("main.tf was not copied into the overlay dir")
	}
	if _, err := os.Stat(overlayPath); os.IsNotExist(err) {
		t.Error("terragrunt.hcl not found in overlay dir")
	}
}

func TestGenerateOverlay_OutputPathInScratchDir(t *testing.T) {
	scratchDir := t.TempDir()
	unit := makeUnit(t, "app", nil)
	sim := simulator.NewSimulation()

	overlayPath, err := GenerateOverlay(unit, sim, scratchDir, "shallow", "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(overlayPath, scratchDir) {
		t.Errorf("overlay %q is not inside scratchDir %q", overlayPath, scratchDir)
	}
}

func TestGenerateOverlay_MultipleDepsDeterministic(t *testing.T) {
	depA, depB, depC := t.TempDir(), t.TempDir(), t.TempDir()
	unit := makeUnit(t, "app", map[string]*graph.DependencyConfig{
		"zzz": {ConfigPath: depC},
		"aaa": {ConfigPath: depA},
		"mmm": {ConfigPath: depB},
	})
	sim := simulator.NewSimulation()
	for _, p := range []string{depA, depB, depC} {
		sim.SetOutputs(p, map[string]simulator.SimulatedOutput{
			"out": {Value: "sim-val", Source: simulator.SourceSynthetic},
		})
	}

	content1, _ := os.ReadFile(mustOverlay(t, unit, sim, t.TempDir(), "shallow"))
	content2, _ := os.ReadFile(mustOverlay(t, unit, sim, t.TempDir(), "shallow"))

	if string(content1) != string(content2) {
		t.Error("overlay output is non-deterministic across calls")
	}

	// buildHCL writes deps in sorted order, so aaa < mmm < zzz in the source.
	s := string(content1)
	posA := strings.Index(s, `dependency "aaa"`)
	posM := strings.Index(s, `dependency "mmm"`)
	posZ := strings.Index(s, `dependency "zzz"`)
	if posA < 0 || posM < 0 || posZ < 0 {
		t.Fatalf("one or more dependency blocks not found\n%s", s)
	}
	if posA >= posM || posM >= posZ {
		t.Errorf("dependency blocks not in expected order aaa < mmm < zzz\n%s", s)
	}
}

func mustOverlay(t *testing.T, unit *graph.Unit, sim *simulator.Simulation, scratchDir, strategy string) string {
	t.Helper()
	p, err := GenerateOverlay(unit, sim, scratchDir, strategy, "")
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// --- toCtyValue ---

func TestToCtyValue_Types(t *testing.T) {
	cases := []struct {
		name  string
		input interface{}
	}{
		{"nil", nil},
		{"string", "hello"},
		{"bool", true},
		{"float64", float64(3.14)},
		{"int", 42},
		{"int64", int64(99)},
		{"list of strings", []interface{}{"a", "b"}},
		{"empty list", []interface{}{}},
		{"map", map[string]interface{}{"k": "v"}},
		{"empty map", map[string]interface{}{}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			v, err := toCtyValue(c.input)
			if err != nil {
				t.Errorf("toCtyValue(%v): unexpected error: %v", c.input, err)
			}
			if v == cty.NilVal {
				t.Errorf("toCtyValue(%v): returned NilVal", c.input)
			}
		})
	}
}

// TestGenerateOverlay_ExplicitStackHierarchy exercises the ancestor-HCL-copy
// and relative-overlay-path behaviour needed for explicit stacks.
//
// It sets up a miniature stack layout:
//
//	workingDir/
//	  root.hcl        ← shared config (find_in_parent_folders target)
//	  .terragrunt-stack/
//	    a/
//	      terragrunt.hcl
//
// and asserts that after GenerateOverlay the scratch directory mirrors this
// layout so that root.hcl lands at <scratch>/root.hcl (accessible via
// find_in_parent_folders from <scratch>/.terragrunt-stack/a/).
func TestGenerateOverlay_ExplicitStackHierarchy(t *testing.T) {
	workingDir := t.TempDir()

	// Create root.hcl in the working dir (simulates the shared stack config).
	rootHCL := filepath.Join(workingDir, "root.hcl")
	if err := os.WriteFile(rootHCL, []byte("# root\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Create the unit inside .terragrunt-stack/a/.
	unitDir := filepath.Join(workingDir, ".terragrunt-stack", "a")
	if err := os.MkdirAll(unitDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(unitDir, "terragrunt.hcl"), []byte("# no deps\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	unit := &graph.Unit{
		Path:   unitDir,
		Config: &graph.TerragruntConfig{Dependencies: map[string]*graph.DependencyConfig{}},
	}
	sim := simulator.NewSimulation()
	scratchDir := t.TempDir()

	overlayPath, err := GenerateOverlay(unit, sim, scratchDir, "shallow", workingDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Overlay should be at <scratch>/.terragrunt-stack/a/terragrunt.hcl.
	wantOverlay := filepath.Join(scratchDir, ".terragrunt-stack", "a", "terragrunt.hcl")
	if overlayPath != wantOverlay {
		t.Errorf("overlay path = %q, want %q", overlayPath, wantOverlay)
	}

	// root.hcl must have been copied to the scratch root so find_in_parent_folders works.
	if _, err := os.Stat(filepath.Join(scratchDir, "root.hcl")); os.IsNotExist(err) {
		t.Error("root.hcl was not copied to scratch root (find_in_parent_folders would fail)")
	}
}

func TestUnitDirName(t *testing.T) {
	cases := []struct{ path, want string }{
		{"/a/b/c", "a_b_c"},
		{"/single", "single"},
		{"relative/path", "relative_path"},
	}
	for _, c := range cases {
		got := unitDirName(c.path)
		if got != c.want {
			t.Errorf("unitDirName(%q) = %q, want %q", c.path, got, c.want)
		}
	}
}
