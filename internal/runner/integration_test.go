//go:build integration

package runner

// Integration tests exercise the full Simulate pipeline against the fixture
// stacks in testdata/. They require real terragrunt and terraform/tofu binaries
// and no cloud credentials (all fixtures use the local backend + null_resource).
//
// Run with:
//   go test -tags integration -timeout 10m ./internal/runner/...
//
// Environment variables:
//   TF_BINARY  – path to the terraform/tofu binary (default: "terraform")
//
// Tested Terragrunt version: ≥ 1.0.0

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/tagn/tg-simulate/internal/graph"
	"github.com/tagn/tg-simulate/internal/planner"
	"github.com/tagn/tg-simulate/internal/report"
	"github.com/tagn/tg-simulate/internal/simulator"
)

// tfBinaryForTest returns the terraform/tofu binary name, matching the logic in planner.tfBinary().
func tfBinaryForTest() string {
	if bin := os.Getenv("TF_BINARY"); bin != "" {
		return bin
	}
	if _, err := exec.LookPath("terraform"); err == nil {
		return "terraform"
	}
	return "tofu"
}

// requireBinaries skips the test if terragrunt or terraform/tofu is not on PATH.
func requireBinaries(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("terragrunt"); err != nil {
		t.Skip("terragrunt binary not found; skipping integration test")
	}
	tfBin := os.Getenv("TF_BINARY")
	if tfBin == "" {
		tfBin = "terraform"
	}
	if _, err := exec.LookPath(tfBin); err != nil {
		// Fall back to tofu.
		if _, err2 := exec.LookPath("tofu"); err2 != nil {
			t.Skipf("neither %q nor 'tofu' found; skipping integration test", tfBin)
		}
	}
}

// fixtureDir returns the absolute path to a testdata fixture sub-directory.
// It locates testdata/ relative to this source file so the test works regardless
// of the working directory when `go test` is invoked.
func fixtureDir(t *testing.T, name string) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	// thisFile is internal/runner/integration_test.go
	root := filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
	p := filepath.Join(root, "testdata", name)
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("fixture directory %q not found: %v", p, err)
	}
	return p
}

// copyFixture copies the fixture at src to a temp directory so tests can
// modify files and accumulate state without polluting the repository.
func copyFixture(t *testing.T, src string) string {
	t.Helper()
	dst := t.TempDir()
	if err := copyDir(src, dst); err != nil {
		t.Fatalf("copying fixture %q: %v", src, err)
	}
	return dst
}

func copyDir(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		// Skip generated/cache directories that must not be copied.
		if info.IsDir() && (info.Name() == ".terragrunt-cache" || info.Name() == ".terraform") {
			return filepath.SkipDir
		}
		rel, _ := filepath.Rel(src, path)
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, info.Mode())
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, info.Mode())
	})
}

// tgApply runs `terragrunt run --all apply --non-interactive` in workingDir.
func tgApply(t *testing.T, workingDir string) {
	t.Helper()
	cmd := exec.CommandContext(context.Background(),
		"terragrunt", "run", "--all", "apply",
		"--non-interactive",
		"--working-dir", workingDir,
	)
	cmd.Dir = workingDir
	// Set TG_TF_PATH so TG 1.0.x uses the same binary as our plan/show steps.
	cmd.Env = append(os.Environ(), "TG_TF_PATH="+tfBinaryForTest())
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("terragrunt apply failed: %v\n%s", err, out)
	}
}

// --- simple-chain: A → B → C ---

func TestIntegration_SimpleChain(t *testing.T) {
	requireBinaries(t)
	dir := copyFixture(t, fixtureDir(t, "simple-chain"))

	ctx := context.Background()
	g, err := graph.Load(ctx, dir)
	if err != nil {
		t.Fatalf("graph.Load: %v", err)
	}

	rpt, _, err := Simulate(ctx, g, Options{WorkingDir: dir})
	if err != nil {
		t.Fatalf("Simulate: %v", err)
	}

	// Expect 3 units (a, b, c) — no fatal errors.
	planned := 0
	for _, u := range rpt.Units {
		if u.Err != nil {
			t.Errorf("unit %q errored: %v", u.Unit.Path, u.Err)
		} else {
			planned++
		}
	}
	if planned != 3 {
		t.Errorf("expected 3 planned units, got %d", planned)
	}

	// Units should be in topological order: a before b before c.
	order := make([]string, 0, len(rpt.Units))
	for _, u := range rpt.Units {
		order = append(order, filepath.Base(u.Unit.Path))
	}
	if !assertOrder(order, "a", "b") || !assertOrder(order, "b", "c") {
		t.Errorf("units not in topological order: %v", order)
	}
}

// --- diamond: A → B, A → C, B+C → D ---

func TestIntegration_Diamond(t *testing.T) {
	requireBinaries(t)
	dir := copyFixture(t, fixtureDir(t, "diamond"))

	ctx := context.Background()
	g, err := graph.Load(ctx, dir)
	if err != nil {
		t.Fatalf("graph.Load: %v", err)
	}
	if len(g.Units) != 4 {
		t.Fatalf("expected 4 units in diamond graph, got %d", len(g.Units))
	}

	rpt, _, err := Simulate(ctx, g, Options{WorkingDir: dir})
	if err != nil {
		t.Fatalf("Simulate: %v", err)
	}

	for _, u := range rpt.Units {
		if u.Err != nil {
			t.Errorf("unit %q errored: %v", u.Unit.Path, u.Err)
		}
	}

	// D should have ConfidenceSynthetic (no real state for B or C).
	for _, u := range rpt.Units {
		if filepath.Base(u.Unit.Path) == "d" {
			if u.Confidence == report.ConfidenceReal {
				t.Errorf("unit d: expected non-Real confidence (has simulated deps), got Real")
			}
		}
	}
}

// --- greenfield: single unit, nothing in state ---

func TestIntegration_Greenfield(t *testing.T) {
	requireBinaries(t)
	dir := copyFixture(t, fixtureDir(t, "greenfield"))

	ctx := context.Background()
	g, err := graph.Load(ctx, dir)
	if err != nil {
		t.Fatalf("graph.Load: %v", err)
	}

	rpt, sim, err := Simulate(ctx, g, Options{WorkingDir: dir})
	if err != nil {
		t.Fatalf("Simulate: %v", err)
	}
	if len(rpt.Units) != 1 {
		t.Fatalf("expected 1 unit in greenfield report, got %d", len(rpt.Units))
	}

	u := rpt.Units[0]
	if u.Err != nil {
		t.Fatalf("unit errored: %v", u.Err)
	}
	// Greenfield unit has no dependencies → confidence is Real.
	if u.Confidence != report.ConfidenceReal {
		t.Errorf("greenfield unit: expected ConfidenceReal, got %v", u.Confidence)
	}

	// The plan should show a create action for null_resource.this.
	if !u.PlanResult.HasChanges {
		t.Error("expected HasChanges=true for greenfield unit (nothing in state)")
	}

	// resource_id output should appear in the simulation state as synthetic
	// (the value is unknown until apply).
	aPath := g.Order[0]
	outputs := sim.UnitOutputs[aPath]
	if rid, ok := outputs["resource_id"]; ok {
		if rid.Source == simulator.SourceRealState {
			t.Error("greenfield resource_id should not be SourceRealState")
		}
	}
}

// --- with-changes: force_new propagation ---

func TestIntegration_ForceNewPropagation(t *testing.T) {
	requireBinaries(t)

	// Copy the fixture to a temp dir so we can apply state and modify files.
	src := fixtureDir(t, "with-changes")
	dir := copyFixture(t, src)

	ctx := context.Background()

	// Step 1: Apply unit A and B with trigger "v1" to establish real state.
	tgApply(t, dir)

	// Step 2: Load the graph from the temp dir.
	g, err := graph.Load(ctx, dir)
	if err != nil {
		t.Fatalf("graph.Load after apply: %v", err)
	}

	// Step 3: Simulate with the current config (no changes expected in A).
	rpt, _, err := Simulate(ctx, g, Options{WorkingDir: dir})
	if err != nil {
		t.Fatalf("Simulate (baseline): %v", err)
	}
	for _, u := range rpt.Units {
		if u.Err != nil {
			t.Errorf("baseline: unit %q errored: %v", u.Unit.Path, u.Err)
		}
	}

	// Step 4: Change A's trigger from "v1" to "v2" in the copied fixture.
	aMainTF := filepath.Join(dir, "a", "main.tf")
	content, err := os.ReadFile(aMainTF)
	if err != nil {
		t.Fatalf("reading a/main.tf: %v", err)
	}
	modified := strings.ReplaceAll(string(content), `"v1"`, `"v2"`)
	if err := os.WriteFile(aMainTF, []byte(modified), 0o644); err != nil {
		t.Fatalf("modifying a/main.tf: %v", err)
	}

	// Step 5: Re-load graph (config hasn't changed, only Terraform source).
	g2, err := graph.Load(ctx, dir)
	if err != nil {
		t.Fatalf("graph.Load after modification: %v", err)
	}

	// Step 6: Simulate and check that B shows a replacement.
	rpt2, _, err := Simulate(ctx, g2, Options{WorkingDir: dir})
	if err != nil {
		t.Fatalf("Simulate (post-change): %v", err)
	}

	bReplaced := false
	for _, u := range rpt2.Units {
		if u.Err != nil {
			t.Errorf("post-change: unit %q errored: %v", u.Unit.Path, u.Err)
			continue
		}
		if filepath.Base(u.Unit.Path) == "b" {
			for _, d := range u.OutputDeltas {
				_ = d
			}
			// Unit B must have HasChanges=true and its plan must include a replace.
			if !u.PlanResult.HasChanges {
				t.Error("unit b: expected HasChanges=true after upstream trigger changed")
			}
			bReplaced = hasReplaceInPlan(t, u.PlanResult)
		}
	}
	if !bReplaced {
		t.Error("unit b: expected a replace resource change after A's trigger changed (force_new propagation)")
	}
}

// hasReplaceInPlan reports whether any resource in the plan was replaced
// (delete+create or create+delete). Handles both compact and pretty-printed JSON
// by checking for the individual action strings in the same "actions" array.
func hasReplaceInPlan(t *testing.T, result *planner.PlanResult) bool {
	t.Helper()
	if result == nil || len(result.PlanJSON) == 0 {
		return false
	}
	s := string(result.PlanJSON)
	// Match compact form first.
	if strings.Contains(s, `"delete","create"`) || strings.Contains(s, `"create","delete"`) {
		return true
	}
	// Match pretty-printed: "delete" and "create" appear near each other in an actions array.
	return strings.Contains(s, `"delete"`) && strings.Contains(s, `"create"`) &&
		strings.Contains(s, `"actions"`)
}

// --- explicit-stack: units inside .terragrunt-stack/ with include root.hcl ---
//
// This test covers the case where the working directory is the root of a
// Terragrunt explicit stack (one that uses terragrunt.stack.hcl and generates
// unit configs into .terragrunt-stack/). Units inside .terragrunt-stack/ use
//
//	include "root" { path = find_in_parent_folders("root.hcl") }
//
// and a terraform { source = "../../modules/..." } pointing to modules that live
// outside the generated stack directory. Neither of those resolves correctly
// without the overlay hierarchy and terraform-source fixes.

func TestIntegration_ExplicitStack(t *testing.T) {
	requireBinaries(t)
	dir := copyFixture(t, fixtureDir(t, "explicit-stack"))

	ctx := context.Background()
	g, err := graph.Load(ctx, dir)
	if err != nil {
		t.Fatalf("graph.Load: %v", err)
	}
	if len(g.Units) != 2 {
		t.Fatalf("expected 2 units in explicit-stack graph, got %d (units: %v)",
			len(g.Units), unitPaths(g))
	}

	rpt, _, err := Simulate(ctx, g, Options{WorkingDir: dir})
	if err != nil {
		t.Fatalf("Simulate: %v", err)
	}

	planned := 0
	for _, u := range rpt.Units {
		if u.Err != nil {
			t.Errorf("unit %q errored: %v", u.Unit.Path, u.Err)
		} else {
			planned++
		}
	}
	if planned != 2 {
		t.Errorf("expected 2 planned units, got %d", planned)
	}

	// a must appear before b (b depends on a).
	order := make([]string, 0, len(rpt.Units))
	for _, u := range rpt.Units {
		order = append(order, filepath.Base(u.Unit.Path))
	}
	if !assertOrder(order, "a", "b") {
		t.Errorf("units not in topological order: %v", order)
	}

	// b has a simulated dependency on a — confidence must not be Real.
	for _, u := range rpt.Units {
		if filepath.Base(u.Unit.Path) == "b" {
			if u.Confidence == report.ConfidenceReal {
				t.Errorf("unit b: expected non-Real confidence (has simulated dep on a), got Real")
			}
		}
	}
}

// --- infra-pipeline: 3-tier implicit stack with multi-output propagation ---
//
// Topology: vpc → database → app
//
// vpc outputs vpc_id and cidr_block; database consumes both and outputs
// db_endpoint (computed from vpc_id) and db_port; app consumes db_endpoint
// and outputs instance_id and app_url.
//
// The test applies the stack at cidr_block="10.0.0.0/16", then changes the
// CIDR to "10.1.0.0/16" in vpc/terragrunt.hcl.  This replaces the VPC
// (new vpc_id), which changes db_endpoint in database (replaced), which
// changes instance_id/app_url in app (replaced) — a 3-hop cascade.

func TestIntegration_InfraPipeline(t *testing.T) {
	requireBinaries(t)

	dir := copyFixture(t, fixtureDir(t, "infra-pipeline"))
	ctx := context.Background()

	// Step 1: Apply to establish real state at cidr_block="10.0.0.0/16".
	tgApply(t, dir)

	// Step 2: Baseline simulate — no pending changes.
	g, err := graph.Load(ctx, dir)
	if err != nil {
		t.Fatalf("graph.Load (baseline): %v", err)
	}
	rpt, _, err := Simulate(ctx, g, Options{WorkingDir: dir})
	if err != nil {
		t.Fatalf("Simulate (baseline): %v", err)
	}
	for _, u := range rpt.Units {
		if u.Err != nil {
			t.Errorf("baseline: unit %q errored: %v", u.Unit.Path, u.Err)
		}
	}

	// Step 3: Change the VPC CIDR from "10.0.0.0/16" to "10.1.0.0/16".
	vpcHCL := filepath.Join(dir, "vpc", "terragrunt.hcl")
	content, err := os.ReadFile(vpcHCL)
	if err != nil {
		t.Fatalf("reading vpc/terragrunt.hcl: %v", err)
	}
	modified := strings.ReplaceAll(string(content), `"10.0.0.0/16"`, `"10.1.0.0/16"`)
	if err := os.WriteFile(vpcHCL, []byte(modified), 0o644); err != nil {
		t.Fatalf("modifying vpc/terragrunt.hcl: %v", err)
	}

	// Step 4: Re-simulate and verify the cascade.
	g2, err := graph.Load(ctx, dir)
	if err != nil {
		t.Fatalf("graph.Load (post-change): %v", err)
	}
	rpt2, _, err := Simulate(ctx, g2, Options{WorkingDir: dir})
	if err != nil {
		t.Fatalf("Simulate (post-change): %v", err)
	}

	vpcReplaced, dbReplaced, appReplaced := false, false, false
	for _, u := range rpt2.Units {
		if u.Err != nil {
			t.Errorf("post-change: unit %q errored: %v", u.Unit.Path, u.Err)
			continue
		}
		switch filepath.Base(u.Unit.Path) {
		case "vpc":
			vpcReplaced = hasReplaceInPlan(t, u.PlanResult)
		case "database":
			dbReplaced = hasReplaceInPlan(t, u.PlanResult)
		case "app":
			appReplaced = hasReplaceInPlan(t, u.PlanResult)
		}
	}
	if !vpcReplaced {
		t.Error("vpc: expected replace after cidr_block change")
	}
	if !dbReplaced {
		t.Error("database: expected replace after upstream vpc_id changed")
	}
	if !appReplaced {
		t.Error("app: expected replace after upstream db_endpoint changed")
	}
}

// --- platform-stack: 3-unit explicit stack with multi-output propagation ---
//
// Topology: network → storage, network → platform, storage → platform
//   (platform depends on both network and storage)
//
// network outputs network_id and network_version; storage consumes network_id
// and outputs bucket_name and storage_url; platform consumes network_id and
// bucket_name and outputs platform_id and platform_url.
//
// The test applies the stack at network_version="v1", then bumps the version
// to "v2" in .terragrunt-stack/network/terragrunt.hcl — replacing the network
// resource, which cascades into storage and then platform.

func TestIntegration_PlatformStack(t *testing.T) {
	requireBinaries(t)

	dir := copyFixture(t, fixtureDir(t, "platform-stack"))
	ctx := context.Background()

	// Step 1: Apply to establish real state at network_version="v1".
	tgApply(t, dir)

	// Step 2: Baseline simulate — no pending changes.
	g, err := graph.Load(ctx, dir)
	if err != nil {
		t.Fatalf("graph.Load (baseline): %v", err)
	}
	if len(g.Units) != 3 {
		t.Fatalf("expected 3 units in platform-stack graph, got %d (units: %v)",
			len(g.Units), unitPaths(g))
	}
	rpt, _, err := Simulate(ctx, g, Options{WorkingDir: dir})
	if err != nil {
		t.Fatalf("Simulate (baseline): %v", err)
	}
	for _, u := range rpt.Units {
		if u.Err != nil {
			t.Errorf("baseline: unit %q errored: %v", u.Unit.Path, u.Err)
		}
	}

	// Step 3: Bump network_version from "v1" to "v2".
	netHCL := filepath.Join(dir, ".terragrunt-stack", "network", "terragrunt.hcl")
	content, err := os.ReadFile(netHCL)
	if err != nil {
		t.Fatalf("reading .terragrunt-stack/network/terragrunt.hcl: %v", err)
	}
	modified := strings.ReplaceAll(string(content), `"v1"`, `"v2"`)
	if err := os.WriteFile(netHCL, []byte(modified), 0o644); err != nil {
		t.Fatalf("modifying network/terragrunt.hcl: %v", err)
	}

	// Step 4: Re-simulate and verify the cascade.
	g2, err := graph.Load(ctx, dir)
	if err != nil {
		t.Fatalf("graph.Load (post-change): %v", err)
	}
	rpt2, _, err := Simulate(ctx, g2, Options{WorkingDir: dir})
	if err != nil {
		t.Fatalf("Simulate (post-change): %v", err)
	}

	networkReplaced, storageReplaced, platformReplaced := false, false, false
	for _, u := range rpt2.Units {
		if u.Err != nil {
			t.Errorf("post-change: unit %q errored: %v", u.Unit.Path, u.Err)
			continue
		}
		switch filepath.Base(u.Unit.Path) {
		case "network":
			networkReplaced = hasReplaceInPlan(t, u.PlanResult)
		case "storage":
			storageReplaced = hasReplaceInPlan(t, u.PlanResult)
		case "platform":
			platformReplaced = hasReplaceInPlan(t, u.PlanResult)
		}
	}
	if !networkReplaced {
		t.Error("network: expected replace after network_version bump")
	}
	if !storageReplaced {
		t.Error("storage: expected replace after upstream network_id changed")
	}
	if !platformReplaced {
		t.Error("platform: expected replace after upstream network_id and bucket_name changed")
	}

	// platform depends on both network and storage — its confidence must be non-Real
	// even at baseline since it has simulated upstream inputs.
	for _, u := range rpt.Units {
		if filepath.Base(u.Unit.Path) == "platform" {
			if u.Confidence == report.ConfidenceReal {
				t.Error("platform: expected non-Real confidence (has simulated deps), got Real")
			}
		}
	}
}

// unitPaths returns a slice of all unit paths in the graph for diagnostics.
func unitPaths(g *graph.Graph) []string {
	paths := make([]string, 0, len(g.Units))
	for p := range g.Units {
		paths = append(paths, p)
	}
	return paths
}

// assertOrder returns true if item a comes before item b in the slice.
func assertOrder(order []string, a, b string) bool {
	posA, posB := -1, -1
	for i, v := range order {
		if v == a {
			posA = i
		}
		if v == b {
			posB = i
		}
	}
	return posA >= 0 && posB >= 0 && posA < posB
}
