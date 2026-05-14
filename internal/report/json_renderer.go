package report

import (
	"encoding/json"
	"io"

	"github.com/tagn/tg-simulate/pkg/tfplan"
)

// jsonReport is the wire format for the JSON renderer.
type jsonReport struct {
	Summary jsonSummary  `json:"summary"`
	Units   []jsonUnit   `json:"units"`
}

type jsonSummary struct {
	Total     int `json:"total"`
	Real      int `json:"real"`
	Partial   int `json:"partial"`
	Synthetic int `json:"synthetic"`
	Errors    int `json:"errors"`
}

type jsonUnit struct {
	Path            string            `json:"path"`
	Confidence      string            `json:"confidence"`
	Resources       *jsonResources    `json:"resources,omitempty"`
	OutputDeltas    []jsonOutputDelta `json:"output_deltas,omitempty"`
	SimulatedInputs []string          `json:"simulated_inputs,omitempty"`
	Error           string            `json:"error,omitempty"`
}

type jsonResources struct {
	Add     int                  `json:"add"`
	Change  int                  `json:"change"`
	Destroy int                  `json:"destroy"`
	Changes []jsonResourceChange `json:"changes,omitempty"`
}

type jsonResourceChange struct {
	Address string `json:"address"`
	Action  string `json:"action"`
}

type jsonOutputDelta struct {
	Name        string      `json:"name"`
	Action      string      `json:"action"`
	Value       interface{} `json:"value,omitempty"`
	IsUnknown   bool        `json:"unknown,omitempty"`
	IsSensitive bool        `json:"sensitive,omitempty"`
}

func renderJSON(w io.Writer, r *Report) error {
	out := jsonReport{}

	for _, u := range r.Units {
		if u.Err != nil {
			out.Summary.Errors++
			out.Units = append(out.Units, jsonUnit{
				Path:  u.Unit.Path,
				Error: u.Err.Error(),
			})
			continue
		}

		out.Summary.Total++
		switch u.Confidence {
		case ConfidenceReal:
			out.Summary.Real++
		case ConfidencePartial:
			out.Summary.Partial++
		case ConfidenceSynthetic:
			out.Summary.Synthetic++
		}

		ju := jsonUnit{
			Path:            u.Unit.Path,
			Confidence:      confidenceJSONLabel(u.Confidence),
			SimulatedInputs: u.SimulatedInputs,
		}

		if u.PlanResult != nil && len(u.PlanResult.PlanJSON) > 0 {
			plan, err := tfplan.Parse(u.PlanResult.PlanJSON)
			if err == nil {
				s := tfplan.Summarize(plan)
				jr := &jsonResources{Add: s.Add, Change: s.Change, Destroy: s.Destroy}
				for _, rd := range tfplan.ResourceDetails(plan) {
					jr.Changes = append(jr.Changes, jsonResourceChange{Address: rd.Address, Action: rd.Action})
				}
				ju.Resources = jr
			}
		}

		for _, d := range u.OutputDeltas {
			jd := jsonOutputDelta{
				Name:        d.Name,
				Action:      d.Action,
				IsUnknown:   d.IsUnknown,
				IsSensitive: d.IsSensitive,
			}
			if !d.IsUnknown && !d.IsSensitive {
				jd.Value = d.KnownValue
			}
			ju.OutputDeltas = append(ju.OutputDeltas, jd)
		}

		out.Units = append(out.Units, ju)
	}

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}

func confidenceJSONLabel(c Confidence) string {
	switch c {
	case ConfidenceReal:
		return "real"
	case ConfidencePartial:
		return "partial"
	case ConfidenceSynthetic:
		return "synthetic"
	default:
		return "unknown"
	}
}
