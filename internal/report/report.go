// Package report aggregates per-unit plan results into a human/machine-readable report.
package report

import (
	"io"

	"github.com/tagn/tg-simulate/internal/graph"
	"github.com/tagn/tg-simulate/internal/planner"
	"github.com/tagn/tg-simulate/internal/simulator"
)

// Confidence indicates how much of a unit's inputs came from real vs. simulated data.
type Confidence int

const (
	ConfidenceReal      Confidence = iota // 🟢 all inputs real or planned-known
	ConfidencePartial                     // 🟡 mix of real and synthetic inputs
	ConfidenceSynthetic                   // 🔴 all inputs synthetic
)

// UnitReport is the aggregated result for a single unit.
type UnitReport struct {
	Unit            *graph.Unit
	PlanResult      *planner.PlanResult  // nil when Err is set
	OutputDeltas    []simulator.OutputDelta
	Confidence      Confidence
	SimulatedInputs []string // "label.outputName" for each synthetic upstream input
	Err             error    // set when this unit could not be planned
}

// Report is the top-level output of a simulation run.
type Report struct {
	Units      []*UnitReport
	errorCount int
}

func NewReport() *Report {
	return &Report{}
}

// AddUnit records a successfully planned unit.
func (r *Report) AddUnit(
	unit *graph.Unit,
	result *planner.PlanResult,
	deltas []simulator.OutputDelta,
	confidence Confidence,
	simulatedInputs []string,
) {
	r.Units = append(r.Units, &UnitReport{
		Unit:            unit,
		PlanResult:      result,
		OutputDeltas:    deltas,
		Confidence:      confidence,
		SimulatedInputs: simulatedInputs,
	})
}

// AddError records a unit that failed to plan.
func (r *Report) AddError(unit *graph.Unit, err error) {
	r.errorCount++
	r.Units = append(r.Units, &UnitReport{
		Unit: unit,
		Err:  err,
	})
}

// HasErrors reports whether any unit in the report encountered an error.
func (r *Report) HasErrors() bool {
	return r.errorCount > 0
}

// Format controls the report output format.
type Format string

const (
	FormatText     Format = "text"
	FormatMarkdown Format = "markdown"
	FormatJSON     Format = "json"
)

// Render writes the report to w in the given format.
func (r *Report) Render(w io.Writer, format Format) error {
	switch format {
	case FormatMarkdown:
		return renderMarkdown(w, r)
	case FormatJSON:
		return renderJSON(w, r)
	default:
		return renderText(w, r)
	}
}
