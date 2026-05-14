package simulator

// ResolveOutput converts an OutputDelta into a SimulatedOutput.
//
// mockHint is the existing mock_outputs value for this output name from any
// downstream unit's dependency config — it provides the strongest type hint for
// synthetic generation. Pass nil when no hint is available.
func ResolveOutput(outputName string, delta OutputDelta, mockHint interface{}) SimulatedOutput {
	switch {
	case delta.IsSensitive:
		// Never attempt to recover or log a sensitive value.
		return SimulatedOutput{
			Value:      "sim-sensitive-redacted",
			Source:     SourceSynthetic,
			Confidence: 0.3,
		}

	case delta.IsUnknown:
		hint := TypeHint{OutputName: outputName, ExistingMock: mockHint}
		return SimulatedOutput{
			Value:      Default.Generate(hint),
			Source:     SourceSynthetic,
			Confidence: 0.3,
		}

	case delta.Action == "no-op":
		// Value is unchanged from state — highest confidence.
		return SimulatedOutput{
			Value:      delta.KnownValue,
			Source:     SourceRealState,
			Confidence: 1.0,
		}

	default:
		// Known planned value (create / update / replace / delete).
		return SimulatedOutput{
			Value:      delta.KnownValue,
			Source:     SourcePlannedKnown,
			Confidence: 0.7,
		}
	}
}

// PopulateFromDeltas resolves every OutputDelta for a unit and stores the
// results in the Simulation, ready to be injected into downstream units.
//
// mockHints is keyed by output name and sourced from downstream units'
// dependency mock_outputs declarations. Pass nil when no hints are available.
func (s *Simulation) PopulateFromDeltas(unitPath string, deltas []OutputDelta, mockHints map[string]interface{}) {
	outputs := make(map[string]SimulatedOutput, len(deltas))
	for _, delta := range deltas {
		var hint interface{}
		if mockHints != nil {
			hint = mockHints[delta.Name]
		}
		outputs[delta.Name] = ResolveOutput(delta.Name, delta, hint)
	}
	s.SetOutputs(unitPath, outputs)
}
