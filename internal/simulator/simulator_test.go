package simulator

import (
	"encoding/json"
	"testing"

	tfjson "github.com/hashicorp/terraform-json"

	"github.com/tagn/tg-simulate/internal/graph"
	"github.com/tagn/tg-simulate/internal/planner"
)

func planResult(t *testing.T, outputChanges map[string]*tfjson.Change) *planner.PlanResult {
	t.Helper()
	p := tfjson.Plan{FormatVersion: "1.2", OutputChanges: outputChanges}
	b, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	return &planner.PlanResult{
		Unit:     &graph.Unit{Path: "/unit/a"},
		PlanJSON: b,
	}
}

func TestExtractDeltas_KnownOutput(t *testing.T) {
	deltas, err := ExtractDeltas(planResult(t, map[string]*tfjson.Change{
		"vnet_id": {
			Actions:      tfjson.Actions{"create"},
			After:        "sim-/subscriptions/abc/vnet",
			AfterUnknown: false,
		},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if len(deltas) != 1 {
		t.Fatalf("expected 1 delta, got %d", len(deltas))
	}
	d := deltas[0]
	if d.Name != "vnet_id" {
		t.Errorf("Name = %q, want %q", d.Name, "vnet_id")
	}
	if d.Action != "create" {
		t.Errorf("Action = %q, want create", d.Action)
	}
	if d.IsUnknown {
		t.Error("expected IsUnknown = false")
	}
	if d.IsSensitive {
		t.Error("expected IsSensitive = false")
	}
	if d.KnownValue != "sim-/subscriptions/abc/vnet" {
		t.Errorf("KnownValue = %v", d.KnownValue)
	}
}

func TestExtractDeltas_UnknownOutput(t *testing.T) {
	deltas, err := ExtractDeltas(planResult(t, map[string]*tfjson.Change{
		"connection_string": {
			Actions:      tfjson.Actions{"create"},
			AfterUnknown: true,
		},
	}))
	if err != nil {
		t.Fatal(err)
	}
	d := deltas[0]
	if !d.IsUnknown {
		t.Error("expected IsUnknown = true")
	}
	if d.KnownValue != nil {
		t.Errorf("KnownValue must be nil for unknown output, got %v", d.KnownValue)
	}
}

func TestExtractDeltas_SensitiveOutput(t *testing.T) {
	deltas, err := ExtractDeltas(planResult(t, map[string]*tfjson.Change{
		"password": {
			Actions:        tfjson.Actions{"create"},
			After:          "hunter2",
			AfterSensitive: true,
		},
	}))
	if err != nil {
		t.Fatal(err)
	}
	d := deltas[0]
	if !d.IsSensitive {
		t.Error("expected IsSensitive = true")
	}
	if d.KnownValue != nil {
		t.Errorf("KnownValue must be nil for sensitive output, got %v", d.KnownValue)
	}
}

func TestExtractDeltas_Actions(t *testing.T) {
	cases := []struct {
		actions tfjson.Actions
		want    string
	}{
		{tfjson.Actions{"no-op"}, "no-op"},
		{tfjson.Actions{"create"}, "create"},
		{tfjson.Actions{"update"}, "update"},
		{tfjson.Actions{"delete"}, "delete"},
		{tfjson.Actions{"delete", "create"}, "replace"},
		{tfjson.Actions{"create", "delete"}, "replace"},
	}
	for _, c := range cases {
		t.Run(c.want, func(t *testing.T) {
			deltas, err := ExtractDeltas(planResult(t, map[string]*tfjson.Change{
				"out": {Actions: c.actions},
			}))
			if err != nil {
				t.Fatal(err)
			}
			if deltas[0].Action != c.want {
				t.Errorf("action = %q, want %q", deltas[0].Action, c.want)
			}
		})
	}
}

func TestExtractDeltas_NoPlanJSON(t *testing.T) {
	result := &planner.PlanResult{Unit: &graph.Unit{Path: "/unit/a"}}
	deltas, err := ExtractDeltas(result)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if deltas != nil {
		t.Errorf("expected nil deltas for empty PlanJSON, got %v", deltas)
	}
}

func TestExtractDeltas_NoOutputChanges(t *testing.T) {
	deltas, err := ExtractDeltas(planResult(t, nil))
	if err != nil {
		t.Fatal(err)
	}
	if len(deltas) != 0 {
		t.Errorf("expected 0 deltas, got %d", len(deltas))
	}
}

func TestSimulation_SetAndGet(t *testing.T) {
	sim := NewSimulation()
	outputs := map[string]SimulatedOutput{
		"vnet_id": {Value: "sim-vnet", Source: SourceSynthetic, Confidence: 0.3},
	}
	sim.SetOutputs("/unit/a", outputs)

	got, ok := sim.UnitOutputs["/unit/a"]
	if !ok {
		t.Fatal("expected outputs for /unit/a")
	}
	if got["vnet_id"].Value != "sim-vnet" {
		t.Errorf("unexpected value: %v", got["vnet_id"].Value)
	}
}
