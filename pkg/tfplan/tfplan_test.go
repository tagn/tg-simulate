package tfplan_test

import (
	"encoding/json"
	"testing"

	tfjson "github.com/hashicorp/terraform-json"

	"github.com/tagn/tg-simulate/pkg/tfplan"
)

// minimalPlanJSON builds a minimal valid plan JSON for testing.
func minimalPlanJSON(t *testing.T, resourceChanges []*tfjson.ResourceChange, outputChanges map[string]*tfjson.Change) []byte {
	t.Helper()
	p := tfjson.Plan{
		FormatVersion:   "1.2", // required by tfjson unmarshal validation
		ResourceChanges: resourceChanges,
		OutputChanges:   outputChanges,
	}
	b, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestParse_ValidJSON(t *testing.T) {
	raw := minimalPlanJSON(t, nil, nil)
	p, err := tfplan.Parse(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p == nil {
		t.Fatal("expected non-nil Plan")
	}
}

func TestParse_InvalidJSON(t *testing.T) {
	_, err := tfplan.Parse([]byte(`not json`))
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestSummarize_Mixed(t *testing.T) {
	changes := []*tfjson.ResourceChange{
		{Change: &tfjson.Change{Actions: tfjson.Actions{"create"}}},
		{Change: &tfjson.Change{Actions: tfjson.Actions{"create"}}},
		{Change: &tfjson.Change{Actions: tfjson.Actions{"update"}}},
		{Change: &tfjson.Change{Actions: tfjson.Actions{"delete"}}},
		{Change: &tfjson.Change{Actions: tfjson.Actions{"delete", "create"}}}, // replace
		{Change: &tfjson.Change{Actions: tfjson.Actions{"no-op"}}},
		{Change: &tfjson.Change{Actions: tfjson.Actions{"read"}}},
	}
	raw := minimalPlanJSON(t, changes, nil)
	p, _ := tfplan.Parse(raw)
	s := tfplan.Summarize(p)

	// 2 creates + 1 replace-add = 3 add; 1 update; 1 delete + 1 replace-destroy = 2 destroy
	if s.Add != 3 {
		t.Errorf("Add: want 3, got %d", s.Add)
	}
	if s.Change != 1 {
		t.Errorf("Change: want 1, got %d", s.Change)
	}
	if s.Destroy != 2 {
		t.Errorf("Destroy: want 2, got %d", s.Destroy)
	}
}

func TestSummarize_Empty(t *testing.T) {
	raw := minimalPlanJSON(t, nil, nil)
	p, _ := tfplan.Parse(raw)
	s := tfplan.Summarize(p)
	if s.Add != 0 || s.Change != 0 || s.Destroy != 0 {
		t.Errorf("empty plan should have zero counts, got %+v", s)
	}
}

func TestIsOutputUnknown(t *testing.T) {
	cases := []struct {
		name         string
		afterUnknown interface{}
		want         bool
	}{
		{"nil", nil, false},
		{"false bool", false, false},
		{"true bool", true, true},
		{"map (partial unknown)", map[string]interface{}{"id": true}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ch := &tfjson.Change{AfterUnknown: c.afterUnknown}
			if got := tfplan.IsOutputUnknown(ch); got != c.want {
				t.Errorf("IsOutputUnknown = %v, want %v", got, c.want)
			}
		})
	}
}

func TestIsOutputSensitive(t *testing.T) {
	cases := []struct {
		name          string
		afterSensitive interface{}
		want           bool
	}{
		{"nil", nil, false},
		{"false bool", false, false},
		{"true bool", true, true},
		{"map (not a bool)", map[string]interface{}{"key": true}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ch := &tfjson.Change{AfterSensitive: c.afterSensitive}
			if got := tfplan.IsOutputSensitive(ch); got != c.want {
				t.Errorf("IsOutputSensitive = %v, want %v", got, c.want)
			}
		})
	}
}
