package report

import (
	"fmt"
	"math"
	"strings"
)

// svgChart generates a filled-area SVG line chart from a series of values.
// The chart is sized to fit a flexible-width container with a fixed height.
// The fill uses a gradient that fades from the line color to transparent.

type chartSeries struct {
	values []float64
	color  string // hex, e.g. "#4a7fff"
}

// svgFilledChart builds an SVG with one or more filled line series.
// width and height are the viewBox dimensions; the SVG scales to container width.
func svgFilledChart(series []chartSeries, width, height int) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf(`<svg class="chart-svg" viewBox="0 0 %d %d" preserveAspectRatio="none" xmlns="http://www.w3.org/2000/svg">`, width, height))

	max := maxAcrossSeries(series)
	if max == 0 {
		max = 1
	}

	for i, s := range series {
		renderSeries(&b, s, i+1, width, height, max)
	}

	b.WriteString(`</svg>`)
	return b.String()
}

func maxAcrossSeries(series []chartSeries) float64 {
	max := 0.0
	for _, s := range series {
		for _, v := range s.values {
			if v > max {
				max = v
			}
		}
	}
	return max
}

func renderSeries(b *strings.Builder, s chartSeries, gradID, width, height int, max float64) {
	n := len(s.values)
	if n == 0 {
		return
	}

	pad := 2.0
	chartH := float64(height) - pad*2
	chartW := float64(width)
	gradIDStr := fmt.Sprintf("grad-%d", gradID)

	linePath, fillPath := buildPaths(s.values, n, chartW, chartH, pad, max)

	writeGradient(b, gradIDStr, s.color)
	b.WriteString(fmt.Sprintf(`<path d="%s" fill="url(#%s)"/>`, fillPath.String(), gradIDStr))
	b.WriteString(fmt.Sprintf(`<path d="%s" fill="none" stroke="%s" stroke-width="1.5" stroke-linejoin="round" stroke-linecap="round"/>`, linePath.String(), s.color))
	writeDots(b, s.values, n, chartW, chartH, pad, max, s.color)
}

func buildPaths(values []float64, n int, chartW, chartH, pad, max float64) (linePath, fillPath strings.Builder) {
	for i, v := range values {
		x := float64(i) / math.Max(float64(n-1), 1) * chartW
		y := pad + chartH - (v/max)*chartH
		if i == 0 {
			linePath.WriteString(fmt.Sprintf("M %.1f %.1f", x, y))
			fillPath.WriteString(fmt.Sprintf("M %.1f %.1f", x, y))
		} else {
			linePath.WriteString(fmt.Sprintf(" L %.1f %.1f", x, y))
			fillPath.WriteString(fmt.Sprintf(" L %.1f %.1f", x, y))
		}
	}
	fillPath.WriteString(fmt.Sprintf(" L %.1f %.1f L 0 %.1f Z", chartW, pad+chartH, pad+chartH))
	return linePath, fillPath
}

func writeGradient(b *strings.Builder, id, color string) {
	b.WriteString(fmt.Sprintf(`<defs><linearGradient id="%s" x1="0" y1="0" x2="0" y2="1">`, id))
	b.WriteString(fmt.Sprintf(`<stop offset="0%%" stop-color="%s" stop-opacity="0.25"/>`, color))
	b.WriteString(fmt.Sprintf(`<stop offset="100%%" stop-color="%s" stop-opacity="0.02"/>`, color))
	b.WriteString(`</linearGradient></defs>`)
}

func writeDots(b *strings.Builder, values []float64, n int, chartW, chartH, pad, max float64, color string) {
	for i, v := range values {
		x := float64(i) / math.Max(float64(n-1), 1) * chartW
		y := pad + chartH - (v/max)*chartH
		b.WriteString(fmt.Sprintf(`<circle cx="%.1f" cy="%.1f" r="2" fill="%s"/>`, x, y, color))
	}
}
