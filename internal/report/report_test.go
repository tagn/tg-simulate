package report

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/tagn/tg-simulate/internal/graph"
	"github.com/tagn/tg-simulate/internal/planner"
	"github.com/tagn/tg-simulate/internal/simulator"
)

// minPlanJSON builds a minimal valid plan JSON for testing renderers.
func minPlanJSON(t *testing.T) []byte {
	t.Helper()
	// language=json
	return []byte(`{"format_version":"1.2","resource_changes":[{"address":"null_resource.this","type":"null_resource","change":{"actions":["create"],"before":null,"after":{}}}],"output_changes":{"resource_id":{"actions":["create"],"before":null,"after":"sim-abc123"}}}`)
}

func makeUnitReport(t *testing.T, path string, confidence Confidence, simulatedInputs []string, deltas []simulator.OutputDelta, err error) *UnitReport {
	t.Helper()
	u := &UnitReport{
		Unit:            &graph.Unit{Path: path},
		Confidence:      confidence,
		SimulatedInputs: simulatedInputs,
		OutputDeltas:    deltas,
		Err:             err,
	}
	if err == nil {
		u.PlanResult = &planner.PlanResult{
			Unit:       u.Unit,
			PlanJSON:   minPlanJSON(t),
			HasChanges: true,
		}
	}
	return u
}

func sampleReport(t *testing.T) *Report {
	t.Helper()
	r := NewReport()
	r.Units = []*UnitReport{
		makeUnitReport(t, "/stack/a", ConfidenceReal, nil, []simulator.OutputDelta{
			{Name: "resource_id", Action: "create", KnownValue: "real-abc"},
		}, nil),
		makeUnitReport(t, "/stack/b", ConfidencePartial, []string{"a.resource_id"}, []simulator.OutputDelta{
			{Name: "resource_id", Action: "create", IsUnknown: true},
			{Name: "secret", Action: "create", IsSensitive: true},
		}, nil),
		makeUnitReport(t, "/stack/c", ConfidenceSynthetic, []string{"b.resource_id"}, nil, nil),
	}
	return r
}

// --- text renderer ---

func TestRenderText_SummaryLine(t *testing.T) {
	r := sampleReport(t)
	var buf bytes.Buffer
	if err := r.Render(&buf, FormatText); err != nil {
		t.Fatalf("Render: %v", err)
	}
	out := buf.String()

	if !strings.Contains(out, "3 unit(s) planned") {
		t.Errorf("expected '3 unit(s) planned' in text output\n%s", out)
	}
	if !strings.Contains(out, "🟢 real: 1") {
		t.Errorf("expected real count in text output\n%s", out)
	}
	if !strings.Contains(out, "🟡 partial: 1") {
		t.Errorf("expected partial count in text output\n%s", out)
	}
	if !strings.Contains(out, "🔴 synthetic: 1") {
		t.Errorf("expected synthetic count in text output\n%s", out)
	}
}

func TestRenderText_PerUnitSections(t *testing.T) {
	r := sampleReport(t)
	var buf bytes.Buffer
	if err := r.Render(&buf, FormatText); err != nil {
		t.Fatal(err)
	}
	out := buf.String()

	if !strings.Contains(out, "stack/a") {
		t.Errorf("expected unit 'a' section\n%s", out)
	}
	if !strings.Contains(out, "🟢 real") {
		t.Errorf("expected real confidence label\n%s", out)
	}
	if !strings.Contains(out, "🟡 partially simulated") {
		t.Errorf("expected partial confidence label\n%s", out)
	}
	if !strings.Contains(out, "🔴 fully synthetic") {
		t.Errorf("expected synthetic confidence label\n%s", out)
	}
}

func TestRenderText_ResourceCounts(t *testing.T) {
	r := sampleReport(t)
	var buf bytes.Buffer
	if err := r.Render(&buf, FormatText); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	// The minPlanJSON has one create, so we expect +1 ~0 -0.
	if !strings.Contains(out, "+1") {
		t.Errorf("expected +1 in resource counts\n%s", out)
	}
}

func TestRenderText_ResourceDetails(t *testing.T) {
	r := sampleReport(t)
	var buf bytes.Buffer
	if err := r.Render(&buf, FormatText); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	// minPlanJSON has address "null_resource.this" with action "create" → "+".
	if !strings.Contains(out, "+ null_resource.this") {
		t.Errorf("expected '+ null_resource.this' in text output\n%s", out)
	}
}

func TestRenderText_OutputDeltas(t *testing.T) {
	r := sampleReport(t)
	var buf bytes.Buffer
	if err := r.Render(&buf, FormatText); err != nil {
		t.Fatal(err)
	}
	out := buf.String()

	if !strings.Contains(out, "resource_id") {
		t.Errorf("expected output name in text\n%s", out)
	}
	if !strings.Contains(out, "(unknown)") {
		t.Errorf("expected (unknown) for unknown output\n%s", out)
	}
	if !strings.Contains(out, "(sensitive)") {
		t.Errorf("expected (sensitive) for sensitive output\n%s", out)
	}
}

func TestRenderText_SimulatedInputsWarning(t *testing.T) {
	r := sampleReport(t)
	var buf bytes.Buffer
	if err := r.Render(&buf, FormatText); err != nil {
		t.Fatal(err)
	}
	out := buf.String()

	if !strings.Contains(out, "a.resource_id") {
		t.Errorf("expected simulated input listed\n%s", out)
	}
	if !strings.Contains(out, "⚠️") {
		t.Errorf("expected warning symbol for synthetic inputs\n%s", out)
	}
}

func TestRenderText_ErrorUnit(t *testing.T) {
	r := NewReport()
	r.AddError(&graph.Unit{Path: "/stack/broken"}, errors.New("provider crashed"))
	var buf bytes.Buffer
	if err := r.Render(&buf, FormatText); err != nil {
		t.Fatal(err)
	}
	out := buf.String()

	if !strings.Contains(out, "ERROR") {
		t.Errorf("expected ERROR marker for failed unit\n%s", out)
	}
	if !strings.Contains(out, "provider crashed") {
		t.Errorf("expected error message in text\n%s", out)
	}
	if !strings.Contains(out, "1 error(s)") {
		t.Errorf("expected error count in summary\n%s", out)
	}
}

// --- markdown renderer ---

func TestRenderMarkdown_Headings(t *testing.T) {
	r := sampleReport(t)
	var buf bytes.Buffer
	if err := r.Render(&buf, FormatMarkdown); err != nil {
		t.Fatal(err)
	}
	out := buf.String()

	if !strings.Contains(out, "## tg-simulate Report") {
		t.Errorf("expected top-level heading\n%s", out)
	}
	if !strings.Contains(out, "### Summary") {
		t.Errorf("expected Summary heading\n%s", out)
	}
}

func TestRenderMarkdown_CollapsibleDetails(t *testing.T) {
	r := sampleReport(t)
	var buf bytes.Buffer
	if err := r.Render(&buf, FormatMarkdown); err != nil {
		t.Fatal(err)
	}
	out := buf.String()

	if !strings.Contains(out, "<details>") {
		t.Errorf("expected <details> blocks\n%s", out)
	}
	if !strings.Contains(out, "</details>") {
		t.Errorf("expected </details> closing tags\n%s", out)
	}
	if !strings.Contains(out, "<summary>") {
		t.Errorf("expected <summary> elements\n%s", out)
	}
}

func TestRenderMarkdown_ResourceTable(t *testing.T) {
	r := sampleReport(t)
	var buf bytes.Buffer
	if err := r.Render(&buf, FormatMarkdown); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "| Action | Resource |") {
		t.Errorf("expected resource table header\n%s", out)
	}
	if !strings.Contains(out, "null_resource.this") {
		t.Errorf("expected resource address in table\n%s", out)
	}
	if !strings.Contains(out, "`add`") {
		t.Errorf("expected 'add' action in resource table\n%s", out)
	}
}

func TestRenderMarkdown_OutputTable(t *testing.T) {
	r := sampleReport(t)
	var buf bytes.Buffer
	if err := r.Render(&buf, FormatMarkdown); err != nil {
		t.Fatal(err)
	}
	out := buf.String()

	if !strings.Contains(out, "| Output | Action | Value |") {
		t.Errorf("expected output table header\n%s", out)
	}
	if !strings.Contains(out, "resource_id") {
		t.Errorf("expected output name in table\n%s", out)
	}
}

func TestRenderMarkdown_SyntheticWarning(t *testing.T) {
	r := sampleReport(t)
	var buf bytes.Buffer
	if err := r.Render(&buf, FormatMarkdown); err != nil {
		t.Fatal(err)
	}
	out := buf.String()

	if !strings.Contains(out, "synthetic upstream values") {
		t.Errorf("expected synthetic warning in markdown\n%s", out)
	}
	if !strings.Contains(out, "a.resource_id") {
		t.Errorf("expected simulated input listed\n%s", out)
	}
}

func TestRenderMarkdown_ErrorUnit(t *testing.T) {
	r := NewReport()
	r.AddError(&graph.Unit{Path: "/stack/broken"}, errors.New("network timeout"))
	var buf bytes.Buffer
	if err := r.Render(&buf, FormatMarkdown); err != nil {
		t.Fatal(err)
	}
	out := buf.String()

	if !strings.Contains(out, "❌") {
		t.Errorf("expected ❌ for error unit\n%s", out)
	}
	if !strings.Contains(out, "network timeout") {
		t.Errorf("expected error message in markdown\n%s", out)
	}
}

func TestRenderMarkdown_ConfidenceIcons(t *testing.T) {
	r := sampleReport(t)
	var buf bytes.Buffer
	if err := r.Render(&buf, FormatMarkdown); err != nil {
		t.Fatal(err)
	}
	out := buf.String()

	if !strings.Contains(out, "🟢") {
		t.Errorf("expected 🟢 for real unit\n%s", out)
	}
	if !strings.Contains(out, "🟡") {
		t.Errorf("expected 🟡 for partial unit\n%s", out)
	}
	if !strings.Contains(out, "🔴") {
		t.Errorf("expected 🔴 for synthetic unit\n%s", out)
	}
}

// --- JSON renderer ---

func TestRenderJSON_ValidJSON(t *testing.T) {
	r := sampleReport(t)
	var buf bytes.Buffer
	if err := r.Render(&buf, FormatJSON); err != nil {
		t.Fatal(err)
	}
	var out map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("JSON output is not valid: %v\n%s", err, buf.String())
	}
}

func TestRenderJSON_Structure(t *testing.T) {
	r := sampleReport(t)
	var buf bytes.Buffer
	if err := r.Render(&buf, FormatJSON); err != nil {
		t.Fatal(err)
	}

	var out map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatal(err)
	}

	summary, ok := out["summary"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected 'summary' object\n%s", buf.String())
	}
	if total := summary["total"].(float64); total != 3 {
		t.Errorf("summary.total = %v, want 3", total)
	}
	if real := summary["real"].(float64); real != 1 {
		t.Errorf("summary.real = %v, want 1", real)
	}
	if partial := summary["partial"].(float64); partial != 1 {
		t.Errorf("summary.partial = %v, want 1", partial)
	}
	if synthetic := summary["synthetic"].(float64); synthetic != 1 {
		t.Errorf("summary.synthetic = %v, want 1", synthetic)
	}

	units, ok := out["units"].([]interface{})
	if !ok || len(units) != 3 {
		t.Fatalf("expected 3 units in JSON, got %v", out["units"])
	}
}

func TestRenderJSON_ConfidenceLabels(t *testing.T) {
	r := sampleReport(t)
	var buf bytes.Buffer
	if err := r.Render(&buf, FormatJSON); err != nil {
		t.Fatal(err)
	}

	var out struct {
		Units []struct {
			Path       string `json:"path"`
			Confidence string `json:"confidence"`
		} `json:"units"`
	}
	if err := json.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatal(err)
	}

	want := map[string]string{
		"/stack/a": "real",
		"/stack/b": "partial",
		"/stack/c": "synthetic",
	}
	for _, u := range out.Units {
		if got, ok := want[u.Path]; ok {
			if u.Confidence != got {
				t.Errorf("unit %q: confidence = %q, want %q", u.Path, u.Confidence, got)
			}
		}
	}
}

func TestRenderJSON_NoSensitiveValues(t *testing.T) {
	r := NewReport()
	r.Units = []*UnitReport{
		{
			Unit:       &graph.Unit{Path: "/stack/a"},
			Confidence: ConfidenceReal,
			PlanResult: &planner.PlanResult{Unit: &graph.Unit{Path: "/stack/a"}, PlanJSON: minPlanJSON(t)},
			OutputDeltas: []simulator.OutputDelta{
				{Name: "password", Action: "create", IsSensitive: true, KnownValue: "hunter2"},
			},
		},
	}

	var buf bytes.Buffer
	if err := r.Render(&buf, FormatJSON); err != nil {
		t.Fatal(err)
	}

	// The sensitive value "hunter2" must never appear in the output.
	if strings.Contains(buf.String(), "hunter2") {
		t.Errorf("sensitive value leaked into JSON output\n%s", buf.String())
	}
}

func TestRenderJSON_UnknownOutputOmitsValue(t *testing.T) {
	r := NewReport()
	r.Units = []*UnitReport{
		{
			Unit:       &graph.Unit{Path: "/stack/a"},
			Confidence: ConfidenceReal,
			PlanResult: &planner.PlanResult{Unit: &graph.Unit{Path: "/stack/a"}, PlanJSON: minPlanJSON(t)},
			OutputDeltas: []simulator.OutputDelta{
				{Name: "conn_str", Action: "create", IsUnknown: true, KnownValue: nil},
			},
		},
	}

	var buf bytes.Buffer
	if err := r.Render(&buf, FormatJSON); err != nil {
		t.Fatal(err)
	}

	var out struct {
		Units []struct {
			OutputDeltas []struct {
				Name      string      `json:"name"`
				IsUnknown bool        `json:"unknown"`
				Value     interface{} `json:"value"`
			} `json:"output_deltas"`
		} `json:"units"`
	}
	if err := json.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatal(err)
	}

	d := out.Units[0].OutputDeltas[0]
	if !d.IsUnknown {
		t.Error("expected unknown=true")
	}
	if d.Value != nil {
		t.Errorf("expected value to be omitted (nil), got %v", d.Value)
	}
}

func TestRenderJSON_ResourceChanges(t *testing.T) {
	r := sampleReport(t)
	var buf bytes.Buffer
	if err := r.Render(&buf, FormatJSON); err != nil {
		t.Fatal(err)
	}

	var out struct {
		Units []struct {
			Resources *struct {
				Add     int `json:"add"`
				Changes []struct {
					Address string `json:"address"`
					Action  string `json:"action"`
				} `json:"changes"`
			} `json:"resources"`
		} `json:"units"`
	}
	if err := json.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatal(err)
	}

	// All three units have minPlanJSON with one create resource.
	for i, u := range out.Units {
		if u.Resources == nil {
			t.Errorf("unit[%d]: expected resources field", i)
			continue
		}
		if len(u.Resources.Changes) == 0 {
			t.Errorf("unit[%d]: expected at least one resource change", i)
			continue
		}
		rc := u.Resources.Changes[0]
		if rc.Address != "null_resource.this" {
			t.Errorf("unit[%d]: address = %q, want 'null_resource.this'", i, rc.Address)
		}
		if rc.Action != "add" {
			t.Errorf("unit[%d]: action = %q, want 'add'", i, rc.Action)
		}
	}
}

func TestRenderJSON_ErrorUnit(t *testing.T) {
	r := NewReport()
	r.AddError(&graph.Unit{Path: "/stack/broken"}, errors.New("init failed"))

	var buf bytes.Buffer
	if err := r.Render(&buf, FormatJSON); err != nil {
		t.Fatal(err)
	}

	var out struct {
		Summary struct {
			Errors int `json:"errors"`
			Total  int `json:"total"`
		} `json:"summary"`
		Units []struct {
			Error string `json:"error"`
		} `json:"units"`
	}
	if err := json.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatal(err)
	}

	if out.Summary.Errors != 1 {
		t.Errorf("summary.errors = %d, want 1", out.Summary.Errors)
	}
	if out.Summary.Total != 0 {
		t.Errorf("summary.total = %d, want 0 (error units don't count)", out.Summary.Total)
	}
	if out.Units[0].Error != "init failed" {
		t.Errorf("unit error = %q, want 'init failed'", out.Units[0].Error)
	}
}

// --- Render dispatch ---

func TestRender_UnknownFormatFallsBackToText(t *testing.T) {
	r := sampleReport(t)
	var buf bytes.Buffer
	// An unrecognized format falls through to text.
	if err := r.Render(&buf, Format("bogus")); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "=== tg-simulate Report ===") {
		t.Errorf("expected text fallback for unknown format\n%s", buf.String())
	}
}

func TestRender_EmptyReport(t *testing.T) {
	r := NewReport()
	for _, format := range []Format{FormatText, FormatMarkdown, FormatJSON} {
		var buf bytes.Buffer
		if err := r.Render(&buf, format); err != nil {
			t.Errorf("Render(%s) on empty report: %v", format, err)
		}
		if buf.Len() == 0 {
			t.Errorf("Render(%s) on empty report produced empty output", format)
		}
	}
}

// --- HasErrors ---

func TestReport_HasErrors(t *testing.T) {
	r := NewReport()
	if r.HasErrors() {
		t.Error("empty report should not have errors")
	}

	r.AddError(&graph.Unit{Path: "/stack/a"}, errors.New("oops"))
	if !r.HasErrors() {
		t.Error("report with error unit should return HasErrors=true")
	}
}

// --- unitDisplayName ---

func TestUnitDisplayName(t *testing.T) {
	cases := []struct{ path, want string }{
		{"/a/b/c", "b/c"},
		{"/a/b", "a/b"},
		// For a path with only one directory below root the function returns
		// the last two slash-split components, which includes the leading empty
		// string from the absolute path prefix.
		{"/single", "/single"},
	}
	for _, c := range cases {
		got := unitDisplayName(c.path)
		if got != c.want {
			t.Errorf("unitDisplayName(%q) = %q, want %q", c.path, got, c.want)
		}
	}
}

// --- formatDeltaValue ---

func TestFormatDeltaValue(t *testing.T) {
	cases := []struct {
		delta simulator.OutputDelta
		want  string
	}{
		{simulator.OutputDelta{IsSensitive: true, KnownValue: "secret"}, "(sensitive)"},
		{simulator.OutputDelta{IsUnknown: true}, "(unknown)"},
		{simulator.OutputDelta{KnownValue: nil}, "null"},
		{simulator.OutputDelta{KnownValue: "hello"}, "hello"},
		{simulator.OutputDelta{KnownValue: float64(42)}, "42"},
	}
	for _, c := range cases {
		if got := formatDeltaValue(c.delta); got != c.want {
			t.Errorf("formatDeltaValue(%+v) = %q, want %q", c.delta, got, c.want)
		}
	}
}
