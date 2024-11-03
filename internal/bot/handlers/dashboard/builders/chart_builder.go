package builders

import (
	"bytes"
	"fmt"

	"github.com/rotector/rotector/internal/common/statistics"
	"github.com/wcharczuk/go-chart/v2"
	"github.com/wcharczuk/go-chart/v2/drawing"
)

// Chart dimensions and styling constants control the visual appearance
// of the statistics chart.
const (
	// titleFontSize sets the size of the chart title text.
	titleFontSize = 12.0
	// xAxisFontSize sets the size of x-axis labels.
	xAxisFontSize = 10.0
	// yAxisFontSize sets the size of y-axis labels.
	yAxisFontSize = 12.0
	// xAxisRotation angles x-axis labels to prevent overlap.
	xAxisRotation = 45.0
	// gridLineWidth controls the thickness of grid lines.
	gridLineWidth = 1.0
	// seriesLineWidth controls the thickness of data lines.
	seriesLineWidth = 3.0
	// seriesDotWidth controls the size of data points.
	seriesDotWidth = 4.0
	// paddingTop adds space above the chart.
	paddingTop = 30
	// paddingBottom adds space below the chart.
	paddingBottom = 30
	// paddingLeft adds space to the left of the chart.
	paddingLeft = 20
	// paddingRight adds space to the right of the chart.
	paddingRight = 20
)

// ChartBuilder creates statistical charts for the dashboard by combining
// hourly data points into line graphs.
type ChartBuilder struct {
	stats statistics.HourlyStats
}

// NewChartBuilder loads hourly statistics to create a new chart builder.
func NewChartBuilder(stats statistics.HourlyStats) *ChartBuilder {
	return &ChartBuilder{
		stats: stats,
	}
}

// Build creates a PNG image showing:
// - Three line series (confirmed, flagged, cleared users)
// - Grid lines for easier reading
// - Hour labels on x-axis
// - Count labels on y-axis
// - Legend identifying each line.
func (b *ChartBuilder) Build() (*bytes.Buffer, error) {
	// Extract data points for each series
	xValues, confirmedSeries, flaggedSeries, clearedSeries := b.prepareDataSeries()
	gridLines, ticks := b.prepareGridLinesAndTicks()

	// Configure and create the chart
	graph := &chart.Chart{
		Title:      "User Statistics",
		TitleStyle: b.getTitleStyle(),
		Background: b.getBackgroundStyle(),
		XAxis:      b.getXAxis(gridLines, ticks),
		YAxis:      b.getYAxis(),
		Series: []chart.Series{
			b.createSeries("Confirmed", xValues, confirmedSeries, chart.ColorBlue),
			b.createSeries("Flagged", xValues, flaggedSeries, chart.ColorRed),
			b.createSeries("Cleared", xValues, clearedSeries, chart.ColorGreen),
		},
	}

	// Add legend below the chart
	graph.Elements = []chart.Renderable{
		chart.Legend(graph),
	}

	// Render chart to PNG format
	buf := new(bytes.Buffer)
	if err := graph.Render(chart.PNG, buf); err != nil {
		return nil, err
	}

	return buf, nil
}

// prepareDataSeries extracts data points from hourly statistics into
// separate slices for x-axis values and each data series.
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

// prepareGridLinesAndTicks creates grid lines and x-axis labels showing
// how many hours ago each data point represents.
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

// getTitleStyle returns styling for the chart title.
func (b *ChartBuilder) getTitleStyle() chart.Style {
	return chart.Style{
		FontSize: titleFontSize,
	}
}

// getBackgroundStyle returns styling for the chart background,
// including padding around all edges.
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

// getXAxis returns configuration for the x-axis including:
// - Rotated labels to prevent overlap
// - Grid lines for easier reading
// - Custom tick marks and labels.
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

// getYAxis returns configuration for the y-axis including:
// - Grid lines for easier reading
// - Number formatting for count labels.
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

// createSeries builds a line series for the chart with:
// - Custom name for the legend
// - Data points from x and y values
// - Specified line color and thickness
// - Dots at each data point.
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
