package report

import (
	"fmt"
	"io"
	"strings"

	"github.com/tagn/tg-simulate/pkg/tfplan"
)

func renderMarkdown(w io.Writer, r *Report) error {
	fmt.Fprintln(w, "## tg-simulate Report")
	fmt.Fprintln(w)

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

	planned := len(r.Units) - nErr
	fmt.Fprintf(w, "### Summary\n\n")
	fmt.Fprintf(w, "- %d unit(s) planned", planned)
	if nErr > 0 {
		fmt.Fprintf(w, " (%d error(s))", nErr)
	}
	fmt.Fprintln(w)
	fmt.Fprintf(w, "- 🟢 real: %d &nbsp; 🟡 partial: %d &nbsp; 🔴 synthetic: %d\n", nReal, nPartial, nSynthetic)
	fmt.Fprintln(w)

	for _, u := range r.Units {
		name := unitDisplayName(u.Unit.Path)
		icon := confidenceIcon(u.Confidence)

		if u.Err != nil {
			fmt.Fprintf(w, "<details><summary>❌ %s — error</summary>\n\n", name)
			fmt.Fprintf(w, "```\n%v\n```\n\n</details>\n\n", u.Err)
			continue
		}

		var summaryLine string
		var resourceDetails []tfplan.ResourceDetail
		if u.PlanResult != nil && len(u.PlanResult.PlanJSON) > 0 {
			plan, err := tfplan.Parse(u.PlanResult.PlanJSON)
			if err == nil {
				s := tfplan.Summarize(plan)
				summaryLine = fmt.Sprintf("+%d ~%d -%d resources", s.Add, s.Change, s.Destroy)
				resourceDetails = tfplan.ResourceDetails(plan)
			}
		}
		if summaryLine == "" {
			summaryLine = "no resource changes"
		}

		fmt.Fprintf(w, "<details><summary>%s %s (%s) — %s</summary>\n\n",
			icon, name, confidenceText(u.Confidence), summaryLine)

		if len(resourceDetails) > 0 {
			fmt.Fprintf(w, "**Resource changes**\n\n")
			fmt.Fprintln(w, "| Action | Resource |")
			fmt.Fprintln(w, "|--------|----------|")
			for _, rd := range resourceDetails {
				fmt.Fprintf(w, "| `%s` | `%s` |\n", rd.Action, rd.Address)
			}
			fmt.Fprintln(w)
		}

		if len(u.OutputDeltas) > 0 {
			fmt.Fprintf(w, "**Output changes**\n\n")
			fmt.Fprintln(w, "| Output | Action | Value |")
			fmt.Fprintln(w, "|--------|--------|-------|")
			for _, d := range u.OutputDeltas {
				val := formatDeltaValue(d)
				val = strings.ReplaceAll(val, "|", "\\|")
				fmt.Fprintf(w, "| `%s` | %s | %s |\n", d.Name, d.Action, val)
			}
			fmt.Fprintln(w)
		}

		if len(u.SimulatedInputs) > 0 {
			fmt.Fprintf(w, "**Simulated inputs**\n\n")
			for _, inp := range u.SimulatedInputs {
				fmt.Fprintf(w, "- `%s`\n", inp)
			}
			fmt.Fprintln(w)
			fmt.Fprintf(w, "> ⚠️ Uses synthetic upstream values — some diffs may resolve to no-op at apply\n\n")
		}

		fmt.Fprintf(w, "</details>\n\n")
	}

	return nil
}

func confidenceIcon(c Confidence) string {
	switch c {
	case ConfidenceReal:
		return "🟢"
	case ConfidencePartial:
		return "🟡"
	case ConfidenceSynthetic:
		return "🔴"
	default:
		return "⬜"
	}
}
