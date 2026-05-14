package simulator

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveOutput_NoOp(t *testing.T) {
	delta := OutputDelta{Name: "vnet_id", Action: "no-op", KnownValue: "real-vnet-id"}
	out := ResolveOutput("vnet_id", delta, nil)
	if out.Source != SourceRealState {
		t.Errorf("no-op should be SourceRealState, got %v", out.Source)
	}
	if out.Confidence != 1.0 {
		t.Errorf("no-op confidence = %v, want 1.0", out.Confidence)
	}
	if out.Value != "real-vnet-id" {
		t.Errorf("no-op value = %v, want real-vnet-id", out.Value)
	}
}

func TestResolveOutput_KnownChange(t *testing.T) {
	delta := OutputDelta{Name: "vnet_id", Action: "update", KnownValue: "new-vnet-id"}
	out := ResolveOutput("vnet_id", delta, nil)
	if out.Source != SourcePlannedKnown {
		t.Errorf("known change should be SourcePlannedKnown, got %v", out.Source)
	}
	if out.Confidence != 0.7 {
		t.Errorf("planned-known confidence = %v, want 0.7", out.Confidence)
	}
	if out.Value != "new-vnet-id" {
		t.Errorf("value = %v, want new-vnet-id", out.Value)
	}
}

func TestResolveOutput_Unknown(t *testing.T) {
	delta := OutputDelta{Name: "connection_string", Action: "create", IsUnknown: true}
	out := ResolveOutput("connection_string", delta, nil)
	if out.Source != SourceSynthetic {
		t.Errorf("unknown should be SourceSynthetic, got %v", out.Source)
	}
	if out.Confidence != 0.3 {
		t.Errorf("synthetic confidence = %v, want 0.3", out.Confidence)
	}
	s, ok := out.Value.(string)
	if !ok {
		t.Fatalf("synthetic value should be string, got %T", out.Value)
	}
	if !strings.Contains(strings.ToLower(s), "sim") {
		t.Errorf("synthetic value should contain 'sim', got %q", s)
	}
}

func TestResolveOutput_UnknownWithMockHint(t *testing.T) {
	hint := "arn:aws:s3:::prod-bucket"
	delta := OutputDelta{Name: "bucket_arn", Action: "create", IsUnknown: true}
	out := ResolveOutput("bucket_arn", delta, hint)
	s, ok := out.Value.(string)
	if !ok {
		t.Fatalf("expected string, got %T", out.Value)
	}
	if !strings.HasPrefix(s, "arn:aws:s3:") {
		t.Errorf("mock hint not used: got %q, expected ARN", s)
	}
}

func TestResolveOutput_Sensitive(t *testing.T) {
	delta := OutputDelta{Name: "password", Action: "create", IsSensitive: true, KnownValue: "hunter2"}
	out := ResolveOutput("password", delta, nil)
	if out.Source != SourceSynthetic {
		t.Errorf("sensitive should be SourceSynthetic, got %v", out.Source)
	}
	// Value must not be the real value.
	if out.Value == "hunter2" {
		t.Error("sensitive value was leaked into SimulatedOutput")
	}
	s, ok := out.Value.(string)
	if !ok || !strings.Contains(strings.ToLower(s), "sim") {
		t.Errorf("sensitive placeholder should contain 'sim', got %v", out.Value)
	}
}

func TestPopulateFromDeltas_StoresAll(t *testing.T) {
	sim := NewSimulation()
	deltas := []OutputDelta{
		{Name: "vnet_id", Action: "create", KnownValue: "new-id"},
		{Name: "subnet_ids", Action: "create", IsUnknown: true},
	}
	hints := map[string]interface{}{
		"subnet_ids": []interface{}{"subnet-a"},
	}
	sim.PopulateFromDeltas("/unit/a", deltas, hints)

	unitOutputs, ok := sim.UnitOutputs["/unit/a"]
	if !ok {
		t.Fatal("expected outputs stored for /unit/a")
	}
	if len(unitOutputs) != 2 {
		t.Fatalf("expected 2 outputs, got %d", len(unitOutputs))
	}
	if unitOutputs["vnet_id"].Source != SourcePlannedKnown {
		t.Errorf("vnet_id source = %v, want SourcePlannedKnown", unitOutputs["vnet_id"].Source)
	}
	if unitOutputs["subnet_ids"].Source != SourceSynthetic {
		t.Errorf("subnet_ids source = %v, want SourceSynthetic", unitOutputs["subnet_ids"].Source)
	}
}

func TestPopulateFromDeltas_NilHints(t *testing.T) {
	sim := NewSimulation()
	deltas := []OutputDelta{{Name: "out", Action: "create", IsUnknown: true}}
	// Must not panic when mockHints is nil.
	sim.PopulateFromDeltas("/unit/b", deltas, nil)
	if _, ok := sim.UnitOutputs["/unit/b"]; !ok {
		t.Fatal("expected outputs stored even with nil hints")
	}
}

// --- persistence tests ---

func TestSimulation_SaveLoad_RoundTrip(t *testing.T) {
	sim := NewSimulation()
	sim.SetOutputs("/unit/a", map[string]SimulatedOutput{
		"vnet_id": {Value: "real-vnet", Source: SourceRealState, Confidence: 1.0},
		"subnet_ids": {
			Value:      []interface{}{"sim-subnet-1", "sim-subnet-2"},
			Source:     SourceSynthetic,
			Confidence: 0.3,
		},
	})

	path := filepath.Join(t.TempDir(), "state", "simulation.json")
	if err := sim.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("file not created: %v", err)
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	unitOutputs := loaded.UnitOutputs["/unit/a"]
	if len(unitOutputs) != 2 {
		t.Fatalf("expected 2 outputs after round-trip, got %d", len(unitOutputs))
	}
	if unitOutputs["vnet_id"].Confidence != 1.0 {
		t.Errorf("confidence not preserved: got %v", unitOutputs["vnet_id"].Confidence)
	}
	if unitOutputs["vnet_id"].Source != SourceRealState {
		t.Errorf("source not preserved: got %v", unitOutputs["vnet_id"].Source)
	}
}

func TestSimulation_Save_CreatesDir(t *testing.T) {
	sim := NewSimulation()
	nested := filepath.Join(t.TempDir(), "a", "b", "c", "sim.json")
	if err := sim.Save(nested); err != nil {
		t.Fatalf("Save with nested dir: %v", err)
	}
}

func TestLoad_MissingFile(t *testing.T) {
	_, err := Load("/nonexistent/path/simulation.json")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}
