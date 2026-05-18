package report

import (
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/tagn/tg-simulate/internal/simulator"
	"github.com/tagn/tg-simulate/pkg/tfplan"
)

func renderText(w io.Writer, r *Report) error {
	renderTextHeader(w)
	renderTextSummary(w, r)
	for _, u := range r.Units {
		renderUnitText(w, u)
	}
	return nil
}

func renderTextHeader(w io.Writer) {
	fmt.Fprintln(w, "=== tg-simulate Report ===")
	fmt.Fprintln(w)
}

func renderTextSummary(w io.Writer, r *Report) {
	var nReal, nPartial, nSynthetic, nErr int
	for _, u := range r.Units {
		if u.Err != nil {
			nErr++
			continue
		}
		switch u.Confidence {
		case ConfidenceReal:
			nReal++
		case ConfidencePartial:
			nPartial++
		case ConfidenceSynthetic:
			nSynthetic++
		}
	}
	fmt.Fprintf(w, "Summary: %d unit(s) planned", len(r.Units)-nErr)
	if nErr > 0 {
		fmt.Fprintf(w, ", %d error(s)", nErr)
	}
	fmt.Fprintln(w)
	fmt.Fprintf(w, "  🟢 real: %d  🟡 partial: %d  🔴 synthetic: %d\n", nReal, nPartial, nSynthetic)
	fmt.Fprintln(w)
}

func renderUnitText(w io.Writer, u *UnitReport) {
	displayName := unitDisplayName(u.Unit.Path)
	confidenceLabel := confidenceText(u.Confidence)

	fmt.Fprintf(w, "--- %s (%s) ---\n", displayName, confidenceLabel)

	if u.Err != nil {
		fmt.Fprintf(w, "  ERROR: %v\n\n", u.Err)
		return
	}

	if u.PlanResult != nil && len(u.PlanResult.PlanJSON) > 0 {
		plan, err := tfplan.Parse(u.PlanResult.PlanJSON)
		if err == nil {
			s := tfplan.Summarize(plan)
			fmt.Fprintf(w, "  Resources: +%d ~%d -%d\n", s.Add, s.Change, s.Destroy)
			for _, rd := range tfplan.ResourceDetails(plan) {
				fmt.Fprintf(w, "    %s %s\n", resourceActionPrefix(rd.Action), rd.Address)
			}
		}
	}

	if len(u.OutputDeltas) > 0 {
		fmt.Fprintln(w, "  Outputs:")
		for _, d := range u.OutputDeltas {
			fmt.Fprintf(w, "    %-30s [%-7s]  %s\n", d.Name, d.Action, formatDeltaValue(d))
		}
	}

	if len(u.SimulatedInputs) > 0 {
		fmt.Fprintf(w, "  Simulated inputs: %s\n", strings.Join(u.SimulatedInputs, ", "))
		fmt.Fprintln(w, "  ⚠️  Uses synthetic upstream values — some diffs may resolve to no-op at apply")
	}

	fmt.Fprintln(w)
}

func unitDisplayName(absPath string) string {
	parts := strings.Split(filepath.ToSlash(absPath), "/")
	if len(parts) >= 2 {
		return strings.Join(parts[len(parts)-2:], "/")
	}
	return filepath.Base(absPath)
}

func confidenceText(c Confidence) string {
	switch c {
	case ConfidenceReal:
		return "🟢 real"
	case ConfidencePartial:
		return "🟡 partially simulated"
	case ConfidenceSynthetic:
		return "🔴 fully synthetic"
	default:
		return "unknown"
	}
}

func resourceActionPrefix(action string) string {
	switch action {
	case "add":
		return "+"
	case "change":
		return "~"
	case "destroy":
		return "-"
	case "replace":
		return "±"
	default:
		return "?"
	}
}

func formatDeltaValue(d simulator.OutputDelta) string {
	if d.IsSensitive {
		return "(sensitive)"
	}
	if d.IsUnknown {
		return "(unknown)"
	}
	if d.KnownValue == nil {
		return "null"
	}
	return fmt.Sprintf("%v", d.KnownValue)
}
