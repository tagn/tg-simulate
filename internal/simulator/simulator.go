// Package simulator extracts output deltas from plan results and tracks simulated values.
package simulator

import (
	"fmt"

	"github.com/tagn/tg-simulate/internal/planner"
	"github.com/tagn/tg-simulate/pkg/tfplan"
)

// OutputSource describes how a simulated output value was obtained.
type OutputSource int

const (
	SourceRealState    OutputSource = iota // value from current Terraform state
	SourcePlannedKnown                     // known value from the plan output
	SourceSynthetic                        // heuristically generated value
)

// SimulatedOutput is the resolved value for a single unit output.
type SimulatedOutput struct {
	Value      interface{}
	Source     OutputSource
	Confidence float64 // 1.0 real, 0.7 planned-known, 0.3 synthetic
}

// Simulation tracks all resolved outputs keyed by unit path.
// Not safe for concurrent use — v1 is single-threaded.
type Simulation struct {
	UnitOutputs map[string]map[string]SimulatedOutput
}

func NewSimulation() *Simulation {
	return &Simulation{UnitOutputs: make(map[string]map[string]SimulatedOutput)}
}

func (s *Simulation) SetOutputs(unitPath string, outputs map[string]SimulatedOutput) {
	s.UnitOutputs[unitPath] = outputs
}

// MergeFrom copies all unit outputs from src into s without overwriting
// entries that already exist in s. This is used to seed a downstream stack
// simulation with outputs produced by an upstream stack.
func (s *Simulation) MergeFrom(src *Simulation) {
	for unitPath, outputs := range src.UnitOutputs {
		if _, exists := s.UnitOutputs[unitPath]; !exists {
			s.UnitOutputs[unitPath] = outputs
		}
	}
}

// OutputDelta describes how a single output changes in a plan.
type OutputDelta struct {
	Name        string
	Action      string      // create | update | delete | replace | no-op
	KnownValue  interface{} // nil when IsUnknown or IsSensitive
	IsUnknown   bool
	IsSensitive bool
}

// ExtractDeltas parses the plan JSON in result and returns one OutputDelta per
// output that appears in the plan's output_changes map.
func ExtractDeltas(result *planner.PlanResult) ([]OutputDelta, error) {
	if len(result.PlanJSON) == 0 {
		return nil, nil
	}

	plan, err := tfplan.Parse(result.PlanJSON)
	if err != nil {
		return nil, fmt.Errorf("unit %q: %w", result.Unit.Path, err)
	}

	deltas := make([]OutputDelta, 0, len(plan.OutputChanges))
	for name, change := range plan.OutputChanges {
		delta := OutputDelta{
			Name:        name,
			Action:      actionString(change),
			IsUnknown:   tfplan.IsOutputUnknown(change),
			IsSensitive: tfplan.IsOutputSensitive(change),
		}
		// Never surface the value for sensitive or unknown outputs.
		if !delta.IsUnknown && !delta.IsSensitive {
			delta.KnownValue = change.After
		}
		deltas = append(deltas, delta)
	}

	return deltas, nil
}

// actionString converts a tfjson Actions slice to a simple label.
func actionString(c *tfplan.Change) string {
	switch {
	case c.Actions.NoOp():
		return "no-op"
	case c.Actions.Create():
		return "create"
	case c.Actions.Update():
		return "update"
	case c.Actions.Delete() || c.Actions.Forget():
		return "delete"
	case c.Actions.Replace():
		return "replace"
	default:
		return "unknown"
	}
}
