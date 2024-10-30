package builders

import (
	"bytes"
	"fmt"

	"github.com/rotector/rotector/internal/common/statistics"
	"github.com/wcharczuk/go-chart/v2"
	"github.com/wcharczuk/go-chart/v2/drawing"
)

const (
	// Chart dimensions and styling.
	titleFontSize   = 12.0
	xAxisFontSize   = 10.0
	yAxisFontSize   = 12.0
	xAxisRotation   = 45.0
	gridLineWidth   = 1.0
	seriesLineWidth = 3.0
	seriesDotWidth  = 4.0
	paddingTop      = 30
	paddingBottom   = 30
	paddingLeft     = 20
	paddingRight    = 20
)

// ChartBuilder builds charts for the dashboard.
type ChartBuilder struct {
	stats []statistics.HourlyStats
}

// NewChartBuilder creates a new ChartBuilder.
func NewChartBuilder(stats []statistics.HourlyStats) *ChartBuilder {
	return &ChartBuilder{
		stats: stats,
	}
}

// Build creates the chart image.
func (b *ChartBuilder) Build() (*bytes.Buffer, error) {
	xValues, confirmedSeries, flaggedSeries, clearedSeries := b.prepareDataSeries()
	gridLines, ticks := b.prepareGridLinesAndTicks()

	graph := &chart.Chart{
		Title:      "User Statistics",
		TitleStyle: b.getTitleStyle(),
		Background: b.getBackgroundStyle(),
		XAxis:      b.getXAxis(gridLines, ticks),
		YAxis:      b.getYAxis(),
		Series: []chart.Series{
			b.createSeries("Confirmed", xValues, confirmedSeries, chart.ColorGreen),
			b.createSeries("Flagged", xValues, flaggedSeries, chart.ColorRed),
			b.createSeries("Cleared", xValues, clearedSeries, chart.ColorBlue),
		},
	}

	// Add legend
	graph.Elements = []chart.Renderable{
		chart.Legend(graph),
	}

	// Render to buffer
	buf := new(bytes.Buffer)
	if err := graph.Render(chart.PNG, buf); err != nil {
		return nil, err
	}

	return buf, nil
}

// prepareDataSeries prepares the data series for the chart.
func (b *ChartBuilder) prepareDataSeries() ([]float64, []float64, []float64, []float64) {
	xValues := make([]float64, len(b.stats))
	confirmedSeries := make([]float64, len(b.stats))
	flaggedSeries := make([]float64, len(b.stats))
	clearedSeries := make([]float64, len(b.stats))

	for i, stat := range b.stats {
		xValues[i] = float64(i)
		confirmedSeries[i] = float64(stat.Confirmed)
		flaggedSeries[i] = float64(stat.Flagged)
		clearedSeries[i] = float64(stat.Cleared)
	}

	return xValues, confirmedSeries, flaggedSeries, clearedSeries
}

// prepareGridLinesAndTicks prepares the grid lines and ticks for the chart.
func (b *ChartBuilder) prepareGridLinesAndTicks() ([]chart.GridLine, []chart.Tick) {
	gridLines := make([]chart.GridLine, len(b.stats))
	ticks := make([]chart.Tick, len(b.stats))
	for i := range b.stats {
		gridLines[i] = chart.GridLine{Value: float64(i)}

		// Format as hours ago
		hoursAgo := len(b.stats) - 1 - i
		label := "now"
		if hoursAgo > 0 {
			label = fmt.Sprintf("%dh ago", hoursAgo)
		}

		ticks[i] = chart.Tick{
			Value: float64(i),
			Label: label,
		}
	}
	return gridLines, ticks
}

// getTitleStyle returns the style for the chart title.
func (b *ChartBuilder) getTitleStyle() chart.Style {
	return chart.Style{
		FontSize: titleFontSize,
	}
}

// getBackgroundStyle returns the style for the chart background.
func (b *ChartBuilder) getBackgroundStyle() chart.Style {
	return chart.Style{
		Padding: chart.Box{
			Top:    paddingTop,
			Left:   paddingLeft,
			Right:  paddingRight,
			Bottom: paddingBottom,
		},
	}
}

// getXAxis returns the configuration for the X axis.
func (b *ChartBuilder) getXAxis(gridLines []chart.GridLine, ticks []chart.Tick) chart.XAxis {
	return chart.XAxis{
		Style: chart.Style{
			FontSize:            xAxisFontSize,
			TextRotationDegrees: xAxisRotation,
		},
		GridMajorStyle: chart.Style{
			StrokeColor: chart.ColorAlternateGray,
			StrokeWidth: gridLineWidth,
		},
		GridLines:    gridLines,
		Ticks:        ticks,
		TickPosition: chart.TickPositionUnderTick,
	}
}

// getYAxis returns the configuration for the Y axis.
func (b *ChartBuilder) getYAxis() chart.YAxis {
	return chart.YAxis{
		Style: chart.Style{
			FontSize:            yAxisFontSize,
			TextRotationDegrees: 0.0,
		},
		GridMajorStyle: chart.Style{
			StrokeColor: chart.ColorAlternateGray,
			StrokeWidth: gridLineWidth,
		},
		ValueFormatter: func(v interface{}) string {
			if f, ok := v.(float64); ok {
				return fmt.Sprintf("%.0f", f)
			}
			return ""
		},
	}
}

// createSeries creates a new series for the chart.
func (b *ChartBuilder) createSeries(name string, xValues, yValues []float64, color drawing.Color) chart.Series {
	return chart.ContinuousSeries{
		Name:    name,
		XValues: xValues,
		YValues: yValues,
		Style: chart.Style{
			StrokeColor: color,
			StrokeWidth: seriesLineWidth,
			DotColor:    color,
			DotWidth:    seriesDotWidth,
		},
	}
}
