package view

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/a-h/templ"
	"github.com/tylersriver/mend/internal/store"
)

func ptURL(seg string, id int64) string { return "/pt/" + seg + "/" + strconv.FormatInt(id, 10) }

// --- nullable number rendering ---------------------------------------------

// dash renders a nullable int/float as text, or "—" when not recorded.
func ptInt(p *int64) string {
	if p == nil {
		return "—"
	}
	return strconv.FormatInt(*p, 10)
}

func ptNum(p *float64) string {
	if p == nil {
		return "—"
	}
	return strconv.FormatFloat(*p, 'f', -1, 64)
}

// *Val variants return "" (empty input) instead of a dash, for form fields.
func ptIntVal(p *int64) string {
	if p == nil {
		return ""
	}
	return strconv.FormatInt(*p, 10)
}

func ptNumVal(p *float64) string {
	if p == nil {
		return ""
	}
	return strconv.FormatFloat(*p, 'f', -1, 64)
}

// setSummary builds a compact human description of a logged set from whichever
// metric fields were recorded.
func setSummary(s store.Set) string {
	var parts []string
	if s.Reps != nil {
		parts = append(parts, fmt.Sprintf("%d reps", *s.Reps))
	}
	if s.Load != nil {
		parts = append(parts, fmt.Sprintf("@ %s lb", ptNum(s.Load)))
	}
	if s.Resistance != "" {
		parts = append(parts, "band "+s.Resistance)
	}
	if s.DurationSec != nil {
		parts = append(parts, fmt.Sprintf("%ds hold", *s.DurationSec))
	}
	if s.ROMDegrees != nil {
		parts = append(parts, fmt.Sprintf("ROM %s°", ptNum(s.ROMDegrees)))
	}
	if len(parts) == 0 {
		parts = append(parts, "logged")
	}
	out := strings.Join(parts, " ")
	var tail []string
	if s.RPE != nil {
		tail = append(tail, "RPE "+ptNum(s.RPE))
	}
	if s.Pain != nil {
		tail = append(tail, fmt.Sprintf("pain %d", *s.Pain))
	}
	if len(tail) > 0 {
		out += " · " + strings.Join(tail, ", ")
	}
	return out
}

func itoaInt(n int) string { return strconv.Itoa(n) }

func archivedCls(archived bool) string {
	if archived {
		return "opacity-60"
	}
	return ""
}

// toggleArchived returns the value to POST to flip the current archive state.
func toggleArchived(archived bool) string {
	if archived {
		return "0"
	}
	return "1"
}

func sideSuffix(side string) string {
	if side == "" {
		return ""
	}
	return "(" + side + ")"
}

// measureValue renders a measurement's value with its unit/side for the log table.
func measureValue(m store.Measurement) string {
	v := strconv.FormatFloat(m.Value, 'f', -1, 64)
	if m.Unit != "" {
		v += " " + m.Unit
	}
	return v
}

// latestValue summarizes the most recent point of a series for the chart header.
func latestValue(points []store.Measurement) string {
	if len(points) == 0 {
		return ""
	}
	return measureValue(points[len(points)-1])
}

// --- measurement charts -----------------------------------------------------

// sparkline renders a small inline SVG line chart for a metric's value series.
// Falls back to an empty component for fewer than two points.
func sparkline(points []store.Measurement) templ.Component {
	if len(points) < 2 {
		return templ.NopComponent
	}
	const w, h, pad = 320.0, 60.0, 6.0
	min, max := points[0].Value, points[0].Value
	for _, p := range points {
		if p.Value < min {
			min = p.Value
		}
		if p.Value > max {
			max = p.Value
		}
	}
	span := max - min
	if span == 0 {
		span = 1
	}
	n := len(points)
	var b strings.Builder
	for i, p := range points {
		x := pad + (w-2*pad)*float64(i)/float64(n-1)
		y := h - pad - (h-2*pad)*(p.Value-min)/span
		if i > 0 {
			b.WriteByte(' ')
		}
		fmt.Fprintf(&b, "%.1f,%.1f", x, y)
	}
	svg := fmt.Sprintf(
		`<svg viewBox="0 0 %.0f %.0f" class="w-full h-16" preserveAspectRatio="none" role="img">`+
			`<polyline fill="none" stroke="#0f766e" stroke-width="2" points="%s"/></svg>`,
		w, h, b.String())
	return templ.Raw(svg)
}
