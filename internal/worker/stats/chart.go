package stats

import (
	"bytes"
	"fmt"
	"time"

	"github.com/robalyx/rotector/internal/database/types"
	"github.com/wcharczuk/go-chart/v2"
	"github.com/wcharczuk/go-chart/v2/drawing"
)

// Chart dimensions and styling constants control the visual appearance
// of the statistics chart.
const (
	// hoursToShow is the number of x-axis ticks to show in the chart.
	hoursToShow = 24

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

// ChartBuilder creates statistical charts for the dashboard.
type ChartBuilder struct {
	stats []*types.HourlyStats
}

// NewChartBuilder loads hourly statistics to create a new chart builder.
func NewChartBuilder(stats []*types.HourlyStats) *ChartBuilder {
	return &ChartBuilder{
		stats: stats,
	}
}

// Build creates both user and group statistics charts.
func (b *ChartBuilder) Build() (*bytes.Buffer, *bytes.Buffer, error) {
	userBuffer, err := b.buildUserChart()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to build user chart: %w", err)
	}

	groupBuffer, err := b.buildGroupChart()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to build group chart: %w", err)
	}

	return userBuffer, groupBuffer, nil
}

// buildUserChart creates a chart showing user-related statistics.
func (b *ChartBuilder) buildUserChart() (*bytes.Buffer, error) {
	// Extract data points for user series
	xValues, confirmedSeries, flaggedSeries, clearedSeries, bannedSeries := b.prepareUserDataSeries()

	// Configure and create the chart
	graph := &chart.Chart{
		Title:      "User Statistics (24h)",
		TitleStyle: b.getTitleStyle(),
		Background: b.getBackgroundStyle(),
		XAxis:      b.getXAxis(b.prepareGridLinesAndTicks()),
		YAxis:      b.getYAxis(),
		Series: []chart.Series{
			b.createSeries("Confirmed", xValues, confirmedSeries, chart.ColorRed),
			b.createSeries("Flagged", xValues, flaggedSeries, chart.ColorOrange),
			b.createSeries("Cleared", xValues, clearedSeries, chart.ColorGreen),
			b.createSeries("Banned", xValues, bannedSeries, chart.ColorBlue),
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

// buildGroupChart creates a chart showing group-related statistics.
func (b *ChartBuilder) buildGroupChart() (*bytes.Buffer, error) {
	// Extract data points for group series
	xValues, confirmedSeries, flaggedSeries, mixedSeries, lockedSeries := b.prepareGroupDataSeries()

	// Configure and create the chart
	graph := &chart.Chart{
		Title:      "Group Statistics (24h)",
		TitleStyle: b.getTitleStyle(),
		Background: b.getBackgroundStyle(),
		XAxis:      b.getXAxis(b.prepareGridLinesAndTicks()),
		YAxis:      b.getYAxis(),
		Series: []chart.Series{
			b.createSeries("Confirmed", xValues, confirmedSeries, chart.ColorRed),
			b.createSeries("Flagged", xValues, flaggedSeries, chart.ColorOrange),
			b.createSeries("Mixed", xValues, mixedSeries, chart.ColorGreen),
			b.createSeries("Locked", xValues, lockedSeries, chart.ColorBlue),
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

// prepareUserDataSeries extracts user-related data points from hourly statistics.
func (b *ChartBuilder) prepareUserDataSeries() ([]float64, []float64, []float64, []float64, []float64) {
	xValues := make([]float64, hoursToShow)
	confirmedSeries := make([]float64, hoursToShow)
	flaggedSeries := make([]float64, hoursToShow)
	clearedSeries := make([]float64, hoursToShow)
	bannedSeries := make([]float64, hoursToShow)

	// Create a map of truncated timestamps to stats for lookup
	statsMap := make(map[time.Time]*types.HourlyStats)
	for _, stat := range b.stats {
		truncatedTime := stat.Timestamp.Truncate(time.Hour)
		statsMap[truncatedTime] = stat
	}

	// Fill in data points for each hour
	now := time.Now().UTC().Truncate(time.Hour)

	for i := range hoursToShow {
		xValues[i] = float64(i)
		timestamp := now.Add(time.Duration(-i) * time.Hour)

		if stat, exists := statsMap[timestamp]; exists {
			idx := hoursToShow - 1 - i
			confirmedSeries[idx] = float64(stat.UsersConfirmed)
			flaggedSeries[idx] = float64(stat.UsersFlagged)
			clearedSeries[idx] = float64(stat.UsersCleared)
			bannedSeries[idx] = float64(stat.UsersBanned)
		}
	}

	return xValues, confirmedSeries, flaggedSeries, clearedSeries, bannedSeries
}

// prepareGroupDataSeries extracts group-related data points from hourly statistics.
func (b *ChartBuilder) prepareGroupDataSeries() ([]float64, []float64, []float64, []float64, []float64) {
	xValues := make([]float64, hoursToShow)
	confirmedSeries := make([]float64, hoursToShow)
	flaggedSeries := make([]float64, hoursToShow)
	mixedSeries := make([]float64, hoursToShow)
	lockedSeries := make([]float64, hoursToShow)

	// Create a map of truncated timestamps to stats for lookup
	statsMap := make(map[time.Time]*types.HourlyStats)
	for _, stat := range b.stats {
		truncatedTime := stat.Timestamp.Truncate(time.Hour)
		statsMap[truncatedTime] = stat
	}

	// Fill in data points for each hour
	now := time.Now().UTC().Truncate(time.Hour)

	for i := range hoursToShow {
		xValues[i] = float64(i)
		timestamp := now.Add(time.Duration(-i) * time.Hour)

		if stat, exists := statsMap[timestamp]; exists {
			idx := hoursToShow - 1 - i
			confirmedSeries[idx] = float64(stat.GroupsConfirmed)
			flaggedSeries[idx] = float64(stat.GroupsFlagged)
			mixedSeries[idx] = float64(stat.GroupsMixed)
			lockedSeries[idx] = float64(stat.GroupsLocked)
		}
	}

	return xValues, confirmedSeries, flaggedSeries, mixedSeries, lockedSeries
}

// prepareGridLinesAndTicks creates grid lines and x-axis labels.
func (b *ChartBuilder) prepareGridLinesAndTicks() ([]chart.GridLine, []chart.Tick) {
	gridLines := make([]chart.GridLine, hoursToShow)
	ticks := make([]chart.Tick, hoursToShow)

	for i := range hoursToShow {
		gridLines[i] = chart.GridLine{Value: float64(i)}

		// Format as hours ago
		hoursAgo := hoursToShow - i
		label := fmt.Sprintf("%dh ago", hoursAgo)

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

// getXAxis returns configuration for the x-axis.
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

// getYAxis returns configuration for the y-axis.
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
		ValueFormatter: func(v any) string {
			if f, ok := v.(float64); ok {
				return fmt.Sprintf("%.0f", f)
			}
			return ""
		},
	}
}

// createSeries builds a line series for the chart.
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
