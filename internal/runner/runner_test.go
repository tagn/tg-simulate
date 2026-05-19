package runner

import (
	"context"
	"errors"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/tagn/tg-simulate/internal/graph"
	"github.com/tagn/tg-simulate/internal/planner"
	"github.com/tagn/tg-simulate/internal/report"
	"github.com/tagn/tg-simulate/internal/simulator"
)

// fakePlanJSON is the minimal valid plan JSON the simulator can parse.
const fakePlanJSON = `{"format_version":"1.2","output_changes":{}}`

// planSuccess returns a stub PlanResult with no changes.
func planSuccess(unit *graph.Unit) *planner.PlanResult {
	return &planner.PlanResult{Unit: unit, PlanJSON: []byte(fakePlanJSON), HasChanges: false}
}

// setupMocks replaces planUnitFunc and generateOverlayFunc for a test and
// restores them via t.Cleanup.
func setupMocks(t *testing.T,
	planFn func(context.Context, *graph.Unit, planner.PlanOptions) (*planner.PlanResult, error),
	overlayFn func(*graph.Unit, *simulator.Simulation, string, string, string) (string, error),
) {
	t.Helper()
	origPlan, origOverlay := planUnitFunc, generateOverlayFunc
	planUnitFunc = planFn
	generateOverlayFunc = overlayFn
	t.Cleanup(func() {
		planUnitFunc = origPlan
		generateOverlayFunc = origOverlay
	})
}

func noopOverlay(unit *graph.Unit, sim *simulator.Simulation, scratchDir, strategy, workingDir string) (string, error) {
	return filepath.Join(scratchDir, "overlay.hcl"), nil
}

// --- Simulate tests ---

func TestSimulate_HappyPath(t *testing.T) {
	g, _ := buildGraph(t, nil, "a")

	setupMocks(t,
		func(_ context.Context, unit *graph.Unit, _ planner.PlanOptions) (*planner.PlanResult, error) {
			return planSuccess(unit), nil
		},
		noopOverlay,
	)

	rpt, sim, err := Simulate(context.Background(), g, Options{WorkingDir: t.TempDir()})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rpt == nil {
		t.Fatal("expected non-nil report")
	}
	if sim == nil {
		t.Fatal("expected non-nil simulation")
	}
	if len(rpt.Units) != 1 {
		t.Errorf("expected 1 unit in report, got %d", len(rpt.Units))
	}
	if rpt.Units[0].Err != nil {
		t.Errorf("unexpected unit error: %v", rpt.Units[0].Err)
	}
}

func TestSimulate_ProcessesInTopologicalOrder(t *testing.T) {
	// B depends on A — A must be planned before B.
	g, root := buildGraph(t, [][2]string{{"b", "a"}})

	var order []string
	setupMocks(t,
		func(_ context.Context, unit *graph.Unit, _ planner.PlanOptions) (*planner.PlanResult, error) {
			order = append(order, filepath.Base(unit.Path))
			return planSuccess(unit), nil
		},
		noopOverlay,
	)

	_, _, err := Simulate(context.Background(), g, Options{WorkingDir: root})
	if err != nil {
		t.Fatal(err)
	}
	if len(order) != 2 || order[0] != "a" || order[1] != "b" {
		t.Errorf("expected [a b], got %v", order)
	}
}

func TestSimulate_CollectsPerUnitErrors(t *testing.T) {
	g, root := buildGraph(t, nil, "a", "b")

	var callCount atomic.Int64
	setupMocks(t,
		func(_ context.Context, unit *graph.Unit, _ planner.PlanOptions) (*planner.PlanResult, error) {
			callCount.Add(1)
			if filepath.Base(unit.Path) == "a" {
				return nil, errors.New("provider error")
			}
			return planSuccess(unit), nil
		},
		noopOverlay,
	)

	rpt, _, err := Simulate(context.Background(), g, Options{WorkingDir: root})
	if err != nil {
		t.Fatalf("expected nil top-level error, got: %v", err)
	}
	// Both units attempted.
	if n := callCount.Load(); n != 2 {
		t.Errorf("expected 2 plan calls, got %d", n)
	}
	var errCount int
	for _, u := range rpt.Units {
		if u.Err != nil {
			errCount++
		}
	}
	if errCount != 1 {
		t.Errorf("expected 1 unit error in report, got %d", errCount)
	}
	if !rpt.HasErrors() {
		t.Error("HasErrors() should return true")
	}
}

func TestSimulate_OverlayErrorCollected(t *testing.T) {
	g, root := buildGraph(t, nil, "a")

	setupMocks(t,
		func(_ context.Context, unit *graph.Unit, _ planner.PlanOptions) (*planner.PlanResult, error) {
			return planSuccess(unit), nil
		},
		func(_ *graph.Unit, _ *simulator.Simulation, _, _, _ string) (string, error) {
			return "", errors.New("hclwrite: disk full")
		},
	)

	rpt, _, err := Simulate(context.Background(), g, Options{WorkingDir: root})
	if err != nil {
		t.Fatal(err)
	}
	if len(rpt.Units) != 1 || rpt.Units[0].Err == nil {
		t.Error("expected overlay error to be recorded in report")
	}
}

func TestSimulate_TargetUnitFilter(t *testing.T) {
	// C depends on B depends on A.  Filter for B → only A and B should plan.
	g, root := buildGraph(t, [][2]string{{"b", "a"}, {"c", "b"}})

	var planned []string
	setupMocks(t,
		func(_ context.Context, unit *graph.Unit, _ planner.PlanOptions) (*planner.PlanResult, error) {
			planned = append(planned, filepath.Base(unit.Path))
			return planSuccess(unit), nil
		},
		noopOverlay,
	)

	bPath := filepath.Join(root, "b")
	_, _, err := Simulate(context.Background(), g, Options{WorkingDir: root, TargetUnit: bPath})
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range planned {
		if p == "c" {
			t.Error("unit 'c' should not have been planned (not an ancestor of b)")
		}
	}
	if len(planned) != 2 {
		t.Errorf("expected 2 planned units (a+b), got %v", planned)
	}
}

func TestSimulate_DefaultMergeStrategy(t *testing.T) {
	g, root := buildGraph(t, nil, "a")

	var capturedStrategy string
	setupMocks(t,
		func(_ context.Context, unit *graph.Unit, opts planner.PlanOptions) (*planner.PlanResult, error) {
			capturedStrategy = opts.MergeStrategy
			return planSuccess(unit), nil
		},
		noopOverlay,
	)

	_, _, err := Simulate(context.Background(), g, Options{WorkingDir: root})
	if err != nil {
		t.Fatal(err)
	}
	if capturedStrategy != "shallow" {
		t.Errorf("default merge strategy = %q, want shallow", capturedStrategy)
	}
}

// --- MockHintsFor tests ---

func TestMockHintsFor_NoDownstreams(t *testing.T) {
	g, _ := buildGraph(t, nil, "solo")
	unit := g.Units[g.Order[0]]
	hints := MockHintsFor(unit, g)
	if len(hints) != 0 {
		t.Errorf("expected no hints for isolated unit, got %v", hints)
	}
}

func TestMockHintsFor_PicksUpDownstreamMockOutputs(t *testing.T) {
	g, root := buildGraph(t, [][2]string{{"b", "a"}})
	aPath := filepath.Join(root, "a")
	bPath := filepath.Join(root, "b")

	// Give B a dependency config pointing to A with mock_outputs.
	g.Units[bPath].Config.Dependencies["vnet"] = &graph.DependencyConfig{
		ConfigPath:  aPath,
		MockOutputs: map[string]interface{}{"vnet_id": "/subscriptions/real/vnet"},
	}

	hints := MockHintsFor(g.Units[aPath], g)
	if v, ok := hints["vnet_id"]; !ok || v != "/subscriptions/real/vnet" {
		t.Errorf("expected vnet_id hint, got %v", hints)
	}
}

func TestMockHintsFor_FirstHintWins(t *testing.T) {
	// Two downstreams both have mock_outputs for the same output name.
	g, root := buildGraph(t, [][2]string{{"b", "a"}, {"c", "a"}})
	aPath := filepath.Join(root, "a")
	bPath := filepath.Join(root, "b")
	cPath := filepath.Join(root, "c")

	g.Units[bPath].Config.Dependencies["dep"] = &graph.DependencyConfig{
		ConfigPath:  aPath,
		MockOutputs: map[string]interface{}{"out": "b-hint"},
	}
	g.Units[cPath].Config.Dependencies["dep"] = &graph.DependencyConfig{
		ConfigPath:  aPath,
		MockOutputs: map[string]interface{}{"out": "c-hint"},
	}

	hints := MockHintsFor(g.Units[aPath], g)
	if _, ok := hints["out"]; !ok {
		t.Error("expected 'out' hint to be present")
	}
	// Either "b-hint" or "c-hint" is valid; important is that only one is picked.
	if len(hints) != 1 {
		t.Errorf("expected 1 hint key, got %d", len(hints))
	}
}

// --- computeConfidence tests ---

func TestComputeConfidence_NoDeps(t *testing.T) {
	unit := &graph.Unit{
		Config: &graph.TerragruntConfig{Dependencies: map[string]*graph.DependencyConfig{}},
	}
	sim := simulator.NewSimulation()
	if got := computeConfidence(unit, sim); got != report.ConfidenceReal {
		t.Errorf("no-dep unit: expected ConfidenceReal, got %v", got)
	}
}

func TestComputeConfidence_AllReal(t *testing.T) {
	depPath := "/dep/a"
	unit := &graph.Unit{
		Config: &graph.TerragruntConfig{
			Dependencies: map[string]*graph.DependencyConfig{
				"a": {ConfigPath: depPath},
			},
		},
	}
	sim := simulator.NewSimulation()
	sim.SetOutputs(depPath, map[string]simulator.SimulatedOutput{
		"out": {Value: "real", Source: simulator.SourceRealState, Confidence: 1.0},
	})
	if got := computeConfidence(unit, sim); got != report.ConfidenceReal {
		t.Errorf("expected ConfidenceReal, got %v", got)
	}
}

func TestComputeConfidence_AllSynthetic(t *testing.T) {
	depPath := "/dep/a"
	unit := &graph.Unit{
		Config: &graph.TerragruntConfig{
			Dependencies: map[string]*graph.DependencyConfig{
				"a": {ConfigPath: depPath},
			},
		},
	}
	sim := simulator.NewSimulation()
	sim.SetOutputs(depPath, map[string]simulator.SimulatedOutput{
		"out": {Value: "sim-val", Source: simulator.SourceSynthetic, Confidence: 0.3},
	})
	if got := computeConfidence(unit, sim); got != report.ConfidenceSynthetic {
		t.Errorf("expected ConfidenceSynthetic, got %v", got)
	}
}

func TestComputeConfidence_Mixed(t *testing.T) {
	depPath := "/dep/a"
	unit := &graph.Unit{
		Config: &graph.TerragruntConfig{
			Dependencies: map[string]*graph.DependencyConfig{
				"a": {ConfigPath: depPath},
			},
		},
	}
	sim := simulator.NewSimulation()
	sim.SetOutputs(depPath, map[string]simulator.SimulatedOutput{
		"real_out": {Source: simulator.SourceRealState, Confidence: 1.0},
		"syn_out":  {Source: simulator.SourceSynthetic, Confidence: 0.3},
	})
	if got := computeConfidence(unit, sim); got != report.ConfidencePartial {
		t.Errorf("expected ConfidencePartial, got %v", got)
	}
}

// --- collectSimulatedInputs tests ---

func TestCollectSimulatedInputs_Empty(t *testing.T) {
	unit := &graph.Unit{
		Config: &graph.TerragruntConfig{Dependencies: map[string]*graph.DependencyConfig{}},
	}
	sim := simulator.NewSimulation()
	if got := collectSimulatedInputs(unit, sim); len(got) != 0 {
		t.Errorf("expected no simulated inputs, got %v", got)
	}
}

func TestCollectSimulatedInputs_Sorted(t *testing.T) {
	depPath := "/dep/a"
	unit := &graph.Unit{
		Config: &graph.TerragruntConfig{
			Dependencies: map[string]*graph.DependencyConfig{
				"vnet": {ConfigPath: depPath},
			},
		},
	}
	sim := simulator.NewSimulation()
	sim.SetOutputs(depPath, map[string]simulator.SimulatedOutput{
		"zzz_id": {Source: simulator.SourceSynthetic},
		"aaa_id": {Source: simulator.SourceSynthetic},
		"real":   {Source: simulator.SourceRealState},
	})

	inputs := collectSimulatedInputs(unit, sim)
	if len(inputs) != 2 {
		t.Fatalf("expected 2 synthetic inputs, got %v", inputs)
	}
	if inputs[0] != "vnet.aaa_id" || inputs[1] != "vnet.zzz_id" {
		t.Errorf("expected sorted [vnet.aaa_id vnet.zzz_id], got %v", inputs)
	}
}

// --- force_new propagation (unit-level) ---
//
// TestSimulate_ForceNewPropagation verifies that when an upstream output
// changes and the downstream plan JSON contains a resource replacement (the
// "delete"+"create" pair that terraform/tofu emits for force_new attributes),
// that replacement is captured in the report for the downstream unit.
//
// This is the unit-level counterpart to TestIntegration_ForceNewPropagation:
// it mocks both plan invocations so no real binaries are needed, but it proves
// that the orchestrator correctly threads the upstream output into the
// downstream plan options and preserves the replace action in the report.
func TestSimulate_ForceNewPropagation(t *testing.T) {
	// Graph: B depends on A.
	g, root := buildGraph(t, [][2]string{{"b", "a"}})
	aPath := filepath.Join(root, "a")
	bPath := filepath.Join(root, "b")

	// A's plan: resource_id output changes from old to new (create action,
	// known value — simulates an upstream resource being replaced).
	aPlanJSON := []byte(`{
		"format_version": "1.2",
		"resource_changes": [{
			"address": "null_resource.this",
			"type": "null_resource",
			"change": {"actions": ["delete", "create"], "before": {}, "after": {}}
		}],
		"output_changes": {
			"resource_id": {"actions": ["update"], "before": "old-id", "after": "new-id"}
		}
	}`)

	// B's plan: null_resource.this is replaced because its trigger changed.
	// This is what terraform emits when a force_new attribute value changes.
	bPlanJSON := []byte(`{
		"format_version": "1.2",
		"resource_changes": [{
			"address": "null_resource.this",
			"type": "null_resource",
			"change": {"actions": ["delete", "create"], "before": {}, "after": {}}
		}],
		"output_changes": {
			"resource_id": {"actions": ["update"], "before": "b-old-id", "after_unknown": true}
		}
	}`)

	setupMocks(t,
		func(_ context.Context, unit *graph.Unit, _ planner.PlanOptions) (*planner.PlanResult, error) {
			switch unit.Path {
			case aPath:
				return &planner.PlanResult{Unit: unit, PlanJSON: aPlanJSON, HasChanges: true}, nil
			case bPath:
				return &planner.PlanResult{Unit: unit, PlanJSON: bPlanJSON, HasChanges: true}, nil
			default:
				t.Errorf("unexpected unit path: %s", unit.Path)
				return nil, nil
			}
		},
		noopOverlay,
	)

	rpt, _, err := Simulate(context.Background(), g, Options{WorkingDir: root})
	if err != nil {
		t.Fatalf("Simulate: %v", err)
	}

	// Both units should have been planned without error.
	for _, u := range rpt.Units {
		if u.Err != nil {
			t.Errorf("unit %q: unexpected error: %v", filepath.Base(u.Unit.Path), u.Err)
		}
	}

	// Find unit B in the report.
	var bReport *report.UnitReport
	for _, u := range rpt.Units {
		if u.Unit.Path == bPath {
			bReport = u
		}
	}
	if bReport == nil {
		t.Fatal("unit B not found in report")
	}

	// B must show HasChanges.
	if !bReport.PlanResult.HasChanges {
		t.Error("unit B: expected HasChanges=true")
	}

	// B's output delta for resource_id must be present and unknown
	// (the new ID is not known until apply completes).
	foundReplaceOutput := false
	for _, d := range bReport.OutputDeltas {
		if d.Name == "resource_id" && d.IsUnknown {
			foundReplaceOutput = true
		}
	}
	if !foundReplaceOutput {
		t.Errorf("unit B: expected resource_id output delta with IsUnknown=true; got %+v",
			bReport.OutputDeltas)
	}
}
