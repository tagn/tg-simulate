package report

import (
	"fmt"
	"io"
	"strings"

	"github.com/tagn/tg-simulate/pkg/tfplan"
)

func renderMarkdown(w io.Writer, r *Report) error {
	renderMarkdownHeader(w)
	renderMarkdownSummary(w, r)
	for _, u := range r.Units {
		renderUnitMarkdown(w, u)
	}
	return nil
}

func renderMarkdownHeader(w io.Writer) {
	_, _ = fmt.Fprintln(w, "## tg-simulate Report")
	_, _ = fmt.Fprintln(w)
}

func renderMarkdownSummary(w io.Writer, r *Report) {
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
	_, _ = fmt.Fprintf(w, "### Summary\n\n")
	_, _ = fmt.Fprintf(w, "- %d unit(s) planned", planned)
	if nErr > 0 {
		_, _ = fmt.Fprintf(w, " (%d error(s))", nErr)
	}
	_, _ = fmt.Fprintln(w)
	_, _ = fmt.Fprintf(w, "- 🟢 real: %d &nbsp; 🟡 partial: %d &nbsp; 🔴 synthetic: %d\n", nReal, nPartial, nSynthetic)
	_, _ = fmt.Fprintln(w)
}

func renderUnitMarkdown(w io.Writer, u *UnitReport) {
	name := unitDisplayName(u.Unit.Path)
	icon := confidenceIcon(u.Confidence)

	if u.Err != nil {
		_, _ = fmt.Fprintf(w, "<details><summary>❌ %s — error</summary>\n\n", name)
		_, _ = fmt.Fprintf(w, "```\n%v\n```\n\n</details>\n\n", u.Err)
		return
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

	_, _ = fmt.Fprintf(w, "<details><summary>%s %s (%s) — %s</summary>\n\n",
		icon, name, confidenceText(u.Confidence), summaryLine)

	if len(resourceDetails) > 0 {
		_, _ = fmt.Fprintf(w, "**Resource changes**\n\n")
		_, _ = fmt.Fprintln(w, "| Action | Resource |")
		_, _ = fmt.Fprintln(w, "|--------|----------|")
		for _, rd := range resourceDetails {
			_, _ = fmt.Fprintf(w, "| `%s` | `%s` |\n", rd.Action, rd.Address)
		}
		_, _ = fmt.Fprintln(w)
	}

	if len(u.OutputDeltas) > 0 {
		_, _ = fmt.Fprintf(w, "**Output changes**\n\n")
		_, _ = fmt.Fprintln(w, "| Output | Action | Value |")
		_, _ = fmt.Fprintln(w, "|--------|--------|-------|")
		for _, d := range u.OutputDeltas {
			val := formatDeltaValue(d)
			val = strings.ReplaceAll(val, "|", "\\|")
			_, _ = fmt.Fprintf(w, "| `%s` | %s | %s |\n", d.Name, d.Action, val)
		}
		_, _ = fmt.Fprintln(w)
	}

	if len(u.SimulatedInputs) > 0 {
		_, _ = fmt.Fprintf(w, "**Simulated inputs**\n\n")
		for _, inp := range u.SimulatedInputs {
			_, _ = fmt.Fprintf(w, "- `%s`\n", inp)
		}
		_, _ = fmt.Fprintln(w)
		_, _ = fmt.Fprintf(w, "> ⚠️ Uses synthetic upstream values — some diffs may resolve to no-op at apply\n\n")
	}

	_, _ = fmt.Fprintf(w, "</details>\n\n")
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
